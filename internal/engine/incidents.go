package engine

import (
	"context"
	"encoding/json"
	"reflect"
	"time"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/store"
)

// subject is what an evaluation was about.
type subject struct {
	Type, ID string
	// Title is the subject's display name right now.
	Title string
	// Muted keeps incidents visible on the dashboard but says nothing about them.
	Muted bool
	// SkipChannel is never notified: when a channel is the thing that is
	// broken, it cannot carry the news of its own failure.
	SkipChannel string
}

// mutedReason is recorded on a delivery that was held back by mute mode.
const mutedReason = "muted: recorded, not sent"

// reconcile brings a subject's open incidents in line with its conditions.
//
// kinds are the incident kinds this evaluation is the authority on: an open
// incident of one of them with no matching condition is resolved. fresh
// carries up-to-date facts per kind, so that a recovery message reports the
// value now rather than the last bad one.
//
// The caller must hold e.mu.
func (e *Engine) reconcile(ctx context.Context, sub subject, kinds []string, conds []condition,
	fresh map[string]map[string]any, s core.Settings) (bool, error) {
	open, err := e.store.OpenIncidentsOf(ctx, sub.Type, sub.ID)
	if err != nil {
		return false, err
	}
	now := e.now().UTC().Truncate(time.Second)
	changed := false
	matched := map[string]bool{}

	for _, c := range conds {
		var existing *core.Incident
		for i := range open {
			if open[i].Kind == c.Kind && open[i].Dedup == c.Dedup {
				existing = &open[i]
			}
		}
		if existing == nil {
			inc := core.Incident{
				ID: core.NewID("inc"), SubjectType: sub.Type, SubjectID: sub.ID, Kind: c.Kind, Dedup: c.Dedup,
				Severity: c.Severity, State: core.StateOpen, Reason: c.Reason, Title: sub.Title, Data: c.Data,
				OpenedAt: now, UpdatedAt: now,
			}
			e.announce(ctx, &inc, core.EventOpened, sub, s)
			if err := e.store.SaveIncident(ctx, inc); err != nil {
				return changed, err
			}
			e.log.Info("incident opened", "id", inc.ID, "subject", sub.Title, "kind", c.Kind, "reason", c.Reason, "severity", c.Severity)
			changed = true
			continue
		}

		matched[existing.ID] = true
		worse := c.Severity.Rank() > existing.Severity.Rank() ||
			(c.Severity == existing.Severity && reasonRank(c.Reason) > reasonRank(existing.Reason))
		moved := existing.Severity != c.Severity || existing.Reason != c.Reason ||
			existing.Title != sub.Title || !sameData(existing.Data, c.Data)
		existing.Severity, existing.Reason, existing.Data, existing.Title = c.Severity, c.Reason, c.Data, sub.Title

		announced := 0
		switch {
		case worse:
			// An acknowledgement covered the incident as it was. Now it is
			// something worse, and that is news again.
			existing.AckedAt = nil
			announced = e.announce(ctx, existing, core.EventEscalated, sub, s)
			e.log.Info("incident escalated", "id", existing.ID, "subject", sub.Title, "reason", c.Reason, "severity", c.Severity)
		case existing.NotifiedAt == nil:
			// Nobody has heard about it yet — it opened while muted, or
			// before any channel existed.
			announced = e.announce(ctx, existing, core.EventOpened, sub, s)
		case renotifyDue(*existing, s, now):
			announced = e.announce(ctx, existing, core.EventReminder, sub, s)
		}
		if moved || announced > 0 {
			existing.UpdatedAt = now
			if err := e.store.SaveIncident(ctx, *existing); err != nil {
				return changed, err
			}
			changed = true
		}
	}

	covered := map[string]bool{}
	for _, kind := range kinds {
		covered[kind] = true
	}
	for i := range open {
		inc := &open[i]
		if !covered[inc.Kind] || matched[inc.ID] {
			continue
		}
		if data, ok := fresh[inc.Kind]; ok {
			for key, value := range data {
				inc.Data[key] = value
			}
		}
		if err := e.resolve(ctx, inc, sub, s); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}

// renotifyDue reports whether an open incident is due for a nudge: it is
// critical, still holds, nobody acknowledged it, and the last message is a
// while ago.
//
// Only critical incidents nag. A warning is said once: if it gets worse it
// escalates, which is a message of its own, and until then repeating "six
// days left… five days left…" teaches people to stop reading.
func renotifyDue(inc core.Incident, s core.Settings, now time.Time) bool {
	if s.RenotifyHours <= 0 || inc.AckedAt != nil || inc.NotifiedAt == nil {
		return false
	}
	if inc.Severity != core.Critical || inc.Kind == core.IncidentDue {
		return false
	}
	return now.Sub(*inc.NotifiedAt) >= time.Duration(s.RenotifyHours)*time.Hour
}

// resolve closes an incident and, when someone was told it had opened, tells
// them it is over.
func (e *Engine) resolve(ctx context.Context, inc *core.Incident, sub subject, s core.Settings) error {
	now := e.now().UTC().Truncate(time.Second)
	inc.State, inc.ResolvedAt, inc.UpdatedAt = core.StateResolved, &now, now

	// A message still waiting in the outbox — held back by quiet hours —
	// would now announce a problem that no longer exists.
	if _, err := e.store.DropPendingFor(ctx, inc.ID, "resolved before it was sent"); err != nil {
		return err
	}
	if s.NotifyResolved && inc.Kind != core.IncidentDue {
		told, err := e.toldChannels(ctx, inc.ID)
		if err != nil {
			return err
		}
		if len(told) > 0 {
			e.announceTo(ctx, inc, core.EventResolved, sub, s, told)
		}
	}
	e.log.Info("incident resolved", "id", inc.ID, "subject", inc.Title, "kind", inc.Kind)
	return e.store.SaveIncident(ctx, *inc)
}

// toldChannels lists the channels that actually received a message about an
// incident. A recovery goes to them and nobody else: "it's fine again" means
// nothing to someone who never heard it was broken.
func (e *Engine) toldChannels(ctx context.Context, incidentID string) (map[string]bool, error) {
	deliveries, err := e.store.Deliveries(ctx, store.DeliveryFilter{IncidentID: incidentID, Limit: 500})
	if err != nil {
		return nil, err
	}
	told := map[string]bool{}
	for _, d := range deliveries {
		if d.Status == core.DeliverySent || (d.Status == core.DeliveryDropped && d.LastError == mutedReason) {
			told[d.ChannelID] = true
		}
	}
	return told, nil
}

// announce queues a message about an incident for every channel that wants
// it, and returns how many that was.
func (e *Engine) announce(ctx context.Context, inc *core.Incident, event string, sub subject, s core.Settings) int {
	return e.announceTo(ctx, inc, event, sub, s, nil)
}

// announceTo is announce restricted to a set of channels; nil means every
// channel that wants the incident's topic and severity.
func (e *Engine) announceTo(ctx context.Context, inc *core.Incident, event string, sub subject, s core.Settings, only map[string]bool) int {
	if sub.Muted {
		return 0
	}
	channels, err := e.store.Channels(ctx)
	if err != nil {
		e.log.Error("could not load channels", "error", err)
		return 0
	}
	now := e.now().UTC().Truncate(time.Second)
	raw, err := json.Marshal(e.compose(*inc, event, s))
	if err != nil {
		e.log.Error("could not encode a message", "error", err)
		return 0
	}

	// Quiet hours hold everything back except what cannot wait: a critical
	// problem, and a date reminder — whose hour the user picked themselves.
	notBefore := now
	urgent := inc.Severity == core.Critical && event != core.EventResolved
	if !urgent && inc.Kind != core.IncidentDue {
		if until := s.Quiet.QuietUntil(e.now().In(s.Location())); !until.IsZero() {
			notBefore = until.UTC()
		}
	}

	topic := core.TopicOf(inc.Kind)
	queued := 0
	for _, ch := range channels {
		if ch.ID == sub.SkipChannel || !ch.Enabled {
			continue
		}
		if only != nil {
			if !only[ch.ID] {
				continue
			}
		} else if !ch.Wants(topic, inc.Severity) {
			continue
		}
		d := core.Delivery{
			IncidentID: inc.ID, Event: event, ChannelID: ch.ID, Channel: ch.Name, Message: raw,
			Status: core.DeliveryPending, NotBefore: notBefore, CreatedAt: now,
		}
		if e.mute {
			d.Status, d.LastError = core.DeliveryDropped, mutedReason
		}
		if err := e.store.Enqueue(ctx, &d); err != nil {
			e.log.Error("could not queue a notification", "channel", ch.Name, "error", err)
			continue
		}
		queued++
	}
	if queued > 0 {
		inc.NotifiedAt = &now
		inc.NotifyCount++
		e.hub.Publish(ChangedActivity)
		e.nudge()
	}
	return queued
}

// sameData compares two incident payloads the way they will be stored: as JSON.
func sameData(a, b map[string]any) bool {
	return reflect.DeepEqual(toMap(a), toMap(b))
}

// Ack marks an incident as seen: it stays open, but stops nudging.
func (e *Engine) Ack(ctx context.Context, id string, acked bool) (core.Incident, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	inc, err := e.store.Incident(ctx, id)
	if err != nil {
		return inc, err
	}
	if inc.State != core.StateOpen {
		return inc, invalid("this incident is already resolved")
	}
	now := e.now().UTC().Truncate(time.Second)
	if acked {
		inc.AckedAt = &now
	} else {
		inc.AckedAt = nil
	}
	inc.UpdatedAt = now
	if err := e.store.SaveIncident(ctx, inc); err != nil {
		return inc, err
	}
	e.hub.Publish(ChangedIncidents)
	return inc, nil
}

// closeSubject resolves everything open on a subject that is going away,
// without announcing it: deleting an account is not a recovery.
func (e *Engine) closeSubject(ctx context.Context, subjectType, subjectID string) error {
	open, err := e.store.OpenIncidentsOf(ctx, subjectType, subjectID)
	if err != nil {
		return err
	}
	now := e.now().UTC().Truncate(time.Second)
	for _, inc := range open {
		inc.State, inc.ResolvedAt, inc.UpdatedAt = core.StateResolved, &now, now
		if _, err := e.store.DropPendingFor(ctx, inc.ID, "the subject was removed"); err != nil {
			return err
		}
		if err := e.store.SaveIncident(ctx, inc); err != nil {
			return err
		}
	}
	return nil
}
