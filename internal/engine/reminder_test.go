package engine

import (
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/recur"
)

// birthday adds a lunar birthday on the 16th of the 11th month, which in
// 2026 is December 24, with reminders two days ahead, the day before and on
// the day.
func (w *world) birthday(leads ...int) ReminderView {
	w.t.Helper()
	if leads == nil {
		leads = []int{2, 1, 0}
	}
	view, err := w.e.CreateReminder(ctx, ReminderInput{
		Title: ptr("Alex"), Kind: ptr(core.ReminderBirthday),
		Rule:     &recur.Rule{Freq: recur.Yearly, Calendar: recur.LunarCal, Month: 11, Day: 16},
		LeadDays: &leads,
	}, "zh")
	w.must(err)
	return view
}

func (w *world) remind() []hookMessage {
	w.t.Helper()
	w.must(w.e.EvaluateReminders(ctx))
	return w.dispatch()
}

func TestReminderIsPlacedOnTheCalendar(t *testing.T) {
	w := newWorld(t)
	view := w.birthday()
	if view.Next.String() != "2026-12-24" || *view.Days != 38 || view.State != reminderUpcoming {
		t.Fatalf("view = %+v", view)
	}
	if view.Lunar == nil || view.Lunar.Label != "冬月十六" {
		t.Errorf("lunar = %+v", view.Lunar)
	}
	if len(view.Following) != 3 || view.Following[0].Y != 2027 {
		t.Errorf("following = %v", view.Following)
	}
	for _, next := range view.Following {
		if recur.LunarOf(next).Label != "冬月十六" {
			t.Errorf("%s is not the same lunar day", next)
		}
	}
}

func TestEachLeadDayIsAnnouncedOnceAtTheReminderTime(t *testing.T) {
	w := newWorld(t)
	w.birthday()

	// Three days out: outside the window.
	w.at(2026, 12, 21, 12, 0)
	expect(t, w.remind())
	if len(w.open()) != 0 {
		t.Fatal("opened before the first lead day")
	}

	// Two days out, but before nine: it is on the dashboard, nothing is sent.
	w.at(2026, 12, 22, 8, 0)
	expect(t, w.remind())
	if open := w.open(); len(open) != 1 || open[0].Kind != core.IncidentDue || open[0].Dedup != "2026-12-24" {
		t.Fatalf("open = %+v", open)
	}

	w.at(2026, 12, 22, 9, 0)
	got := w.remind()
	expect(t, got, "🎂 Alex · 生日 · 后天")
	if got[0].Text != "12月24日 周四 · 农历冬月十六" || got[0].Topic != core.TopicDates || got[0].Severity != "info" {
		t.Errorf("message = %+v", got[0])
	}
	if got[0].Link != "https://pulse.example/dates" {
		t.Errorf("link = %q", got[0].Link)
	}

	// The loop runs every minute; the day's reminder went out once.
	for range 5 {
		w.advance(time.Minute)
		expect(t, w.remind())
	}

	w.at(2026, 12, 23, 9, 30)
	expect(t, w.remind(), "🎂 Alex · 生日 · 明天")
	w.at(2026, 12, 24, 9, 0)
	expect(t, w.remind(), "🎂 Alex · 生日 · 今天")

	// The day after, it is simply over, and the reminder looks to next year.
	w.at(2026, 12, 25, 9, 0)
	expect(t, w.remind())
	if len(w.open()) != 0 {
		t.Fatal("still open after the day")
	}
	views, _ := w.e.Reminders(ctx, "zh")
	if views[0].Next.Y != 2027 {
		t.Errorf("next = %s", views[0].Next)
	}
}

func TestDateRemindersIgnoreQuietHours(t *testing.T) {
	w := newWorld(t)
	// Someone who sets the reminder time inside their quiet hours means it.
	w.settings(func(s *core.Settings) {
		s.Quiet = core.QuietHours{Enabled: true, Start: "22:00", End: "10:00"}
		s.ReminderTime = "07:30"
	})
	w.birthday(0)
	w.at(2026, 12, 24, 7, 30)
	expect(t, w.remind(), "🎂 Alex · 生日 · 今天")
}

func TestDoneStopsTheRemainingReminders(t *testing.T) {
	w := newWorld(t)
	view := w.birthday()
	w.at(2026, 12, 22, 9, 0)
	expect(t, w.remind(), "🎂 Alex · 生日 · 后天")

	done, err := w.e.CompleteReminder(ctx, view.ID, "zh")
	w.must(err)
	if done.Next.Y != 2027 || done.Handled == nil || done.Handled.String() != "2026-12-24" {
		t.Fatalf("after done = %+v", done)
	}
	if len(w.open()) != 0 {
		t.Fatal("the incident outlived the mark")
	}
	w.at(2026, 12, 23, 9, 0)
	expect(t, w.remind())
	w.at(2026, 12, 24, 9, 0)
	expect(t, w.remind())

	// Taking the mark back brings it back, and the day's reminder with it.
	reopened, err := w.e.ReopenReminder(ctx, view.ID, "zh")
	w.must(err)
	if reopened.Next.String() != "2026-12-24" || reopened.Handled != nil {
		t.Fatalf("after reopen = %+v", reopened)
	}
	expect(t, w.dispatch(), "🎂 Alex · 生日 · 今天")
}

func TestDowntimeDoesNotLoseAReminderOrSendItThreeTimes(t *testing.T) {
	// Down on the 22nd and 23rd, back on the morning of the 24th: one
	// message, about today — not three, and not zero.
	w := newWorld(t)
	w.birthday()
	w.at(2026, 12, 24, 9, 10)
	expect(t, w.remind(), "🎂 Alex · 生日 · 今天")
	expect(t, w.remind())

	// Down at nine on the 22nd, back that afternoon: the day's reminder is late, not lost.
	w = newWorld(t)
	w.birthday()
	w.at(2026, 12, 22, 15, 0)
	expect(t, w.remind(), "🎂 Alex · 生日 · 后天")
	w.at(2026, 12, 23, 9, 0)
	expect(t, w.remind(), "🎂 Alex · 生日 · 明天")
}

func TestAReminderAddedInsideItsWindowDoesNotReplayThePast(t *testing.T) {
	w := newWorld(t)
	w.at(2026, 12, 22, 14, 0)
	// Two days out, with lead days of a week, a day, and the day itself.
	// The week-ahead slot passed before this reminder existed.
	view := w.birthday(7, 1, 0)
	expect(t, w.dispatch())
	if *view.Days != 2 || view.State != reminderSoon {
		t.Fatalf("view = %+v", view)
	}
	w.at(2026, 12, 23, 9, 0)
	expect(t, w.remind(), "🎂 Alex · 生日 · 明天")

	// Added on a lead day, after the reminder time: that day's reminder is due now.
	w = newWorld(t)
	w.at(2026, 12, 23, 14, 0)
	w.birthday(7, 1, 0)
	expect(t, w.dispatch(), "🎂 Alex · 生日 · 明天")
}

func TestSnooze(t *testing.T) {
	w := newWorld(t)
	view := w.birthday()
	snoozed, err := w.e.SnoozeReminder(ctx, view.ID, ptr(recur.NewDate(2026, 12, 23)), "zh")
	w.must(err)
	if snoozed.SnoozedUntil.String() != "2026-12-23" {
		t.Fatalf("view = %+v", snoozed)
	}

	w.at(2026, 12, 22, 9, 0)
	expect(t, w.remind())
	views, _ := w.e.Reminders(ctx, "zh")
	if views[0].State != reminderSnoozed {
		t.Errorf("state = %s", views[0].State)
	}
	w.at(2026, 12, 23, 9, 0)
	expect(t, w.remind())
	// Awake again: the reminder that is due is the one for today.
	w.at(2026, 12, 24, 9, 0)
	expect(t, w.remind(), "🎂 Alex · 生日 · 今天")

	if _, err := w.e.SnoozeReminder(ctx, view.ID, ptr(recur.NewDate(2020, 1, 1)), "zh"); !IsInvalid(err) {
		t.Errorf("snoozing into the past: %v", err)
	}
}

func TestRenewalWording(t *testing.T) {
	w := newWorld(t)
	leads := []int{3, 0}
	view, err := w.e.CreateReminder(ctx, ReminderInput{
		Title: ptr("iCloud 2TB"), Kind: ptr(core.ReminderRenewal),
		Rule:     &recur.Rule{Freq: recur.Monthly, Day: 20},
		LeadDays: &leads, Amount: ptr(68.0), Currency: ptr("cny"), Notes: ptr("家庭共享"),
	}, "zh")
	w.must(err)
	if view.Currency != "CNY" || *view.MonthlyAmount != 68 || view.Next.String() != "2026-11-20" {
		t.Fatalf("view = %+v", view)
	}

	w.at(2026, 11, 17, 9, 0)
	got := w.remind()
	expect(t, got, "💳 iCloud 2TB · 续费 · 3 天后")
	contains(t, got[0].Text, "11月20日 周五", "金额: 68.00 CNY", "备注: 家庭共享")
	if got[0].Severity != "info" {
		t.Errorf("three days ahead is information: %s", got[0].Severity)
	}

	w.at(2026, 11, 20, 9, 0)
	got = w.remind()
	expect(t, got, "💳 iCloud 2TB · 续费 · 今天")
	if got[0].Severity != "warning" {
		t.Errorf("a payment due today is a warning: %s", got[0].Severity)
	}

	// English reads as English.
	w.settings(func(s *core.Settings) { s.Language = "en" })
	w.at(2026, 12, 17, 9, 0)
	got = w.remind()
	expect(t, got, "💳 iCloud 2TB · renews · in 3 days")
	contains(t, got[0].Text, "Sun, Dec 20", "Amount: 68.00 CNY")
}

func TestOrdinalAndOtherKinds(t *testing.T) {
	w := newWorld(t)
	w.at(2026, 12, 24, 9, 0)
	zero := []int{0}
	add := func(title, kind string, rule recur.Rule) {
		t.Helper()
		_, err := w.e.CreateReminder(ctx, ReminderInput{Title: &title, Kind: &kind, Rule: &rule, LeadDays: &zero}, "zh")
		w.must(err)
	}
	add("Sam", core.ReminderBirthday, recur.Rule{Freq: recur.Yearly, Month: 12, Day: 24, Since: 1996})
	add("结婚", core.ReminderAnniversary, recur.Rule{Freq: recur.Yearly, Month: 12, Day: 24, Since: 2021})
	add("平安夜", core.ReminderHoliday, recur.Rule{Freq: recur.Yearly, Month: 12, Day: 24})
	add("example.com", core.ReminderExpiry, recur.Rule{Freq: recur.Once, Date: ptr(recur.NewDate(2026, 12, 24))})
	add("交材料", core.ReminderCustom, recur.Rule{Freq: recur.Once, Date: ptr(recur.NewDate(2026, 12, 24))})

	got := map[string]bool{}
	for _, m := range w.dispatch() {
		got[m.Title] = true
	}
	for _, want := range []string{
		"🎂 Sam · 30 岁生日 · 今天", "💝 结婚 · 5 周年 · 今天", "🎉 平安夜 · 今天",
		"⏳ example.com · 到期 · 今天", "📌 交材料 · 今天",
	} {
		if !got[want] {
			t.Errorf("missing %q in %v", want, got)
		}
	}

	// A one-off that has passed has nothing ahead.
	w.at(2026, 12, 25, 9, 0)
	w.must(w.e.EvaluateReminders(ctx))
	views, _ := w.e.Reminders(ctx, "zh")
	past := 0
	for _, v := range views {
		if v.State == reminderPast {
			past++
			if v.Next != nil {
				t.Errorf("%s: a past one-off has a next date", v.Title)
			}
		}
	}
	if past != 2 {
		t.Errorf("past = %d", past)
	}
	// The ones with a date come first, soonest first.
	if views[len(views)-1].Next != nil || views[0].Next == nil {
		t.Errorf("order = %+v", views)
	}
}

func TestEditingARuleForgetsTheOldDoneMark(t *testing.T) {
	w := newWorld(t)
	view := w.birthday()
	_, err := w.e.CompleteReminder(ctx, view.ID, "zh")
	w.must(err)

	// Only the origin year changes: the dates are the same, the mark stays.
	same := recur.Rule{Freq: recur.Yearly, Calendar: recur.LunarCal, Month: 11, Day: 16, Since: 1990}
	kept, err := w.e.UpdateReminder(ctx, view.ID, ReminderInput{Rule: &same}, "zh")
	w.must(err)
	if kept.DoneThrough == nil {
		t.Fatal("the mark was dropped for a change that moves no date")
	}

	moved := recur.Rule{Freq: recur.Yearly, Month: 12, Day: 1}
	changed, err := w.e.UpdateReminder(ctx, view.ID, ReminderInput{Rule: &moved}, "zh")
	w.must(err)
	if changed.DoneThrough != nil || changed.Next.String() != "2026-12-01" {
		t.Fatalf("after moving the date = %+v", changed)
	}
}

func TestDisablingOrDeletingAReminderClosesItQuietly(t *testing.T) {
	w := newWorld(t)
	view := w.birthday()
	w.at(2026, 12, 22, 9, 0)
	expect(t, w.remind(), "🎂 Alex · 生日 · 后天")

	off, err := w.e.UpdateReminder(ctx, view.ID, ReminderInput{Enabled: ptr(false)}, "zh")
	w.must(err)
	if off.State != reminderDisabled || len(w.open()) != 0 {
		t.Fatalf("state = %s, open = %d", off.State, len(w.open()))
	}
	expect(t, w.dispatch())

	_, err = w.e.UpdateReminder(ctx, view.ID, ReminderInput{Enabled: ptr(true)}, "zh")
	w.must(err)
	if len(w.open()) != 1 {
		t.Fatal("re-enabling did not reopen")
	}
	w.must(w.e.DeleteReminder(ctx, view.ID))
	if len(w.open()) != 0 {
		t.Fatal("deleting left an incident open")
	}
}

func TestReminderValidation(t *testing.T) {
	w := newWorld(t)
	good := recur.Rule{Freq: recur.Monthly, Day: 1}
	bad := []ReminderInput{
		{},
		{Title: ptr("x")},
		{Title: ptr(" "), Rule: &good},
		{Title: ptr("x"), Rule: &recur.Rule{Freq: "daily"}},
		{Title: ptr("x"), Rule: &good, Kind: ptr("party")},
		{Title: ptr("x"), Rule: &good, LeadDays: &[]int{-1}},
		{Title: ptr("x"), Rule: &good, LeadDays: &[]int{400}},
		{Title: ptr("x"), Rule: &good, Amount: ptr(-5.0)},
		{Title: ptr("x"), Rule: &good, Amount: ptr(5.0), Currency: ptr("yuan")},
	}
	for i, in := range bad {
		if _, err := w.e.CreateReminder(ctx, in, "zh"); !IsInvalid(err) {
			t.Errorf("input %d: err = %v", i, err)
		}
	}

	// Lead days are tidied: sorted, unique, and never empty.
	view, err := w.e.CreateReminder(ctx, ReminderInput{Title: ptr("x"), Rule: &good, LeadDays: &[]int{1, 7, 1, 0}}, "zh")
	w.must(err)
	if len(view.LeadDays) != 3 || view.LeadDays[0] != 7 || view.LeadDays[2] != 0 {
		t.Errorf("lead days = %v", view.LeadDays)
	}
	view, err = w.e.UpdateReminder(ctx, view.ID, ReminderInput{LeadDays: &[]int{}, Amount: ptr(9.9)}, "zh")
	w.must(err)
	if len(view.LeadDays) != 1 || view.LeadDays[0] != 0 || view.Currency != "CNY" {
		t.Errorf("view = %+v", view)
	}
	// Zero clears the amount.
	view, err = w.e.UpdateReminder(ctx, view.ID, ReminderInput{Amount: ptr(0.0)}, "zh")
	w.must(err)
	if view.Amount != nil || view.MonthlyAmount != nil {
		t.Errorf("amount = %v", view.Amount)
	}
}

func TestPreviewRule(t *testing.T) {
	w := newWorld(t)
	view, err := w.e.PreviewRule(ctx, recur.Rule{Freq: recur.Yearly, Calendar: recur.LunarCal, Month: 12, Day: 30}, "zh")
	w.must(err)
	// 腊月三十 next falls on 2027-02-05, New Year's Eve, though that 腊月 has 29 days.
	if view.Next.String() != "2027-02-05" || len(view.Following) != 3 {
		t.Fatalf("preview = %+v", view)
	}
	if _, err := w.e.PreviewRule(ctx, recur.Rule{Freq: recur.Monthly, Day: 40}, "zh"); !IsInvalid(err) {
		t.Errorf("err = %v", err)
	}
	if all, _ := w.e.Reminders(ctx, "zh"); len(all) != 0 {
		t.Error("a preview was saved")
	}
}

func TestMonthlyAmount(t *testing.T) {
	amount := func(rule recur.Rule) float64 {
		got := monthlyAmount(core.Reminder{Rule: rule, Amount: ptr(120.0)})
		if got == nil {
			return -1
		}
		return *got
	}
	if got := amount(recur.Rule{Freq: recur.Yearly}); got != 10 {
		t.Errorf("yearly = %v", got)
	}
	if got := amount(recur.Rule{Freq: recur.Monthly, Interval: 3}); got != 40 {
		t.Errorf("quarterly = %v", got)
	}
	if got := amount(recur.Rule{Freq: recur.Weekly}); got < 521 || got > 522 {
		t.Errorf("weekly = %v", got)
	}
	if got := amount(recur.Rule{Freq: recur.Once}); got != -1 {
		t.Errorf("a one-off is not a running cost: %v", got)
	}
}
