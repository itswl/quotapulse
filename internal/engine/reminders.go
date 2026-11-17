package engine

import (
	"context"
	"slices"
	"sort"
	"time"

	"quotapulse/internal/core"
	"quotapulse/internal/recur"
)

// Occurrence is the date a reminder is about right now.
type Occurrence struct {
	Date recur.Date
	// Days from today; 0 is today.
	Days int
	// InWindow is true once the earliest lead day has been reached.
	InWindow bool
}

// current finds the occurrence a reminder is about: the first one from today
// on that has not been marked done.
func current(r core.Reminder, today recur.Date) (Occurrence, bool) {
	from := today
	if r.DoneThrough != nil && !r.DoneThrough.Before(today) {
		from = r.DoneThrough.AddDays(1)
	}
	date, ok := r.Rule.Next(from)
	if !ok {
		return Occurrence{}, false
	}
	days := date.Sub(today)
	return Occurrence{Date: date, Days: days, InWindow: days <= widestLead(r)}, true
}

func widestLead(r core.Reminder) int {
	widest := 0
	for _, lead := range r.LeadDays {
		widest = max(widest, lead)
	}
	return widest
}

func snoozed(r core.Reminder, today recur.Date) bool {
	return r.SnoozedUntil != nil && !today.After(*r.SnoozedUntil)
}

// dueSeverity ranks a date reminder. Something that needs doing — paying,
// renewing — becomes a warning on its last day; something to remember stays
// information.
func dueSeverity(r core.Reminder, days int) core.Severity {
	if r.NeedsAction() && days <= 1 {
		return core.Warning
	}
	return core.Info
}

func dueData(r core.Reminder, occ Occurrence, sent []int) DueData {
	data := DueData{
		ReminderKind: r.Kind, Date: occ.Date, Days: occ.Days, Ordinal: r.Rule.Ordinal(occ.Date),
		Amount: r.Amount, Currency: r.Currency, Notes: r.Notes, Sent: sent,
	}
	if r.Rule.Calendar == recur.LunarCal {
		data.Lunar = recur.LunarOf(occ.Date).Label
	}
	if data.Sent == nil {
		data.Sent = []int{}
	}
	return data
}

// EvaluateReminders opens, updates and closes the incidents for dates that
// are near, and announces each lead day once.
//
// It is safe to call as often as you like: what has been announced for an
// occurrence is written down in its incident.
func (e *Engine) EvaluateReminders(ctx context.Context) error {
	settings, err := e.store.Settings(ctx)
	if err != nil {
		return err
	}
	reminders, err := e.store.Reminders(ctx)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	changed := false
	for _, r := range reminders {
		moved, err := e.judgeReminder(ctx, r, settings)
		if err != nil {
			return err
		}
		changed = changed || moved
	}
	if changed {
		e.hub.Publish(ChangedIncidents)
		e.hub.Publish(ChangedReminders)
	}
	return nil
}

// judgeReminder reconciles one reminder. Caller holds e.mu.
func (e *Engine) judgeReminder(ctx context.Context, r core.Reminder, s core.Settings) (bool, error) {
	loc := s.Location()
	now := e.now().In(loc)
	today := dateOf(now)
	sub := subject{Type: core.SubjectReminder, ID: r.ID, Title: r.Title}

	open, err := e.store.OpenIncidentsOf(ctx, core.SubjectReminder, r.ID)
	if err != nil {
		return false, err
	}
	occ, upcoming := current(r, today)
	active := r.Enabled && upcoming && occ.InWindow
	changed := false

	// Anything open for another occurrence — one that has passed, or was
	// marked done — is over. Nobody needs telling that a birthday went by.
	var incident *core.Incident
	for i := range open {
		if open[i].Kind != core.IncidentDue {
			continue
		}
		if active && open[i].Dedup == occ.Date.String() {
			incident = &open[i]
			continue
		}
		if err := e.resolve(ctx, &open[i], sub, s); err != nil {
			return changed, err
		}
		changed = true
	}
	if !active {
		return changed, nil
	}

	stamp := e.now().UTC().Truncate(time.Second)
	if incident == nil {
		incident = &core.Incident{
			ID: core.NewID("inc"), SubjectType: core.SubjectReminder, SubjectID: r.ID, Kind: core.IncidentDue,
			Dedup: occ.Date.String(), State: core.StateOpen, OpenedAt: stamp,
		}
		// Lead days that passed before this reminder existed in its
		// present form were never missed: mark them, so a reminder added
		// today for a date tomorrow does not announce last week's slot.
		data := dueData(r, occ, slotsBeforeEdit(r, occ, loc))
		incident.Data = toMap(data)
		changed = true
	}
	var data DueData
	fromMap(incident.Data, &data)
	sent := data.Sent
	fresh := dueData(r, occ, sent)

	severity := dueSeverity(r, occ.Days)
	moved := incident.Severity != severity || incident.Title != r.Title || !sameData(incident.Data, toMap(fresh))
	incident.Severity, incident.Title, incident.Data = severity, r.Title, toMap(fresh)

	// The slot in force is the nearest lead day already reached. If it has
	// not been announced — normally because its hour has just come, or
	// because the server was down when it did — announce it now.
	if slot, ok := slotInForce(r.LeadDays, occ.Days); ok && !slices.Contains(sent, slot) &&
		!snoozed(r, today) && pastReminderTime(now, s) {
		event := core.EventReminder
		if incident.NotifyCount == 0 {
			event = core.EventOpened
		}
		// Whether or not any channel takes it, the slot is spent: a
		// channel added tomorrow should not get today's reminder.
		e.announce(ctx, incident, event, sub, s)
		for _, lead := range r.LeadDays {
			if lead >= occ.Days && !slices.Contains(sent, lead) {
				sent = append(sent, lead)
			}
		}
		sort.Sort(sort.Reverse(sort.IntSlice(sent)))
		fresh.Sent = sent
		incident.Data = toMap(fresh)
		moved = true
	}
	if moved || changed {
		incident.UpdatedAt = stamp
		if err := e.store.SaveIncident(ctx, *incident); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}

// slotInForce picks, among the lead days, the smallest one that has been
// reached: with leads [7, 1, 0] and five days to go, the slot in force is 7.
func slotInForce(leads []int, days int) (int, bool) {
	slot, found := 0, false
	for _, lead := range leads {
		if lead >= days && (!found || lead < slot) {
			slot, found = lead, true
		}
	}
	return slot, found
}

// slotsBeforeEdit lists the lead days whose date fell before the reminder
// was last edited.
func slotsBeforeEdit(r core.Reminder, occ Occurrence, loc *time.Location) []int {
	edited := dateOf(r.UpdatedAt.In(loc))
	sent := []int{}
	for _, lead := range r.LeadDays {
		if occ.Date.AddDays(-lead).Before(edited) {
			sent = append(sent, lead)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(sent)))
	return sent
}

func pastReminderTime(now time.Time, s core.Settings) bool {
	clock, err := core.ParseClock(s.ReminderTime)
	if err != nil {
		return true
	}
	return !now.Before(clock.On(now))
}
