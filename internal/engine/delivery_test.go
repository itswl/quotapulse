package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/recur"
)

// empty opens a critical incident on a fresh pushed meter.
func (w *world) empty(key string) {
	w.t.Helper()
	w.seed(key, core.Balance, "CNY", 5, 3, func(int) float64 { return 0 })
	w.push(key, 0)
}

func TestDeliveryIsRetriedWithBackoffThenGivenUp(t *testing.T) {
	w := newWorld(t)
	w.hook.fail(503)
	w.empty("wallet")

	// The first attempt fails; the delivery stays in the outbox, due later.
	expect(t, w.dispatch())
	d := w.deliveries()[0]
	if d.Status != core.DeliveryPending || d.Attempts != 1 || d.NotBefore.Sub(w.now) != 30*time.Second || !strings.HasPrefix(d.LastError, "HTTP 503") {
		t.Fatalf("after one attempt = %+v", d)
	}
	// Not due yet: dispatching again does not hammer the channel.
	expect(t, w.dispatch())
	if w.deliveries()[0].Attempts != 1 {
		t.Fatal("retried before its time")
	}

	// It recovers before the retries run out: delivered, late.
	w.advance(31 * time.Second)
	expect(t, w.dispatch())
	w.advance(3 * time.Minute)
	w.hook.fail(200)
	expect(t, w.dispatch(), "余额已用尽 · wallet")
	d = w.deliveries()[0]
	if d.Status != core.DeliverySent || d.Attempts != 3 || d.SentAt == nil || d.LastError != "" {
		t.Fatalf("delivered = %+v", d)
	}
}

func TestDeliveryGivesUpAndCanBeRetriedByHand(t *testing.T) {
	w := newWorld(t)
	w.hook.fail(500)
	w.empty("wallet")
	for range len(deliveryBackoff) + 1 {
		w.dispatch()
		w.advance(3 * time.Hour)
	}
	d := w.deliveries()[len(w.deliveries())-1]
	if d.Status != core.DeliveryFailed || d.Attempts != len(deliveryBackoff)+1 {
		t.Fatalf("after every retry = %+v", d)
	}

	w.hook.fail(200)
	if _, err := w.e.RetryDelivery(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	expect(t, w.dispatch(), "余额已用尽 · wallet")
	if _, err := w.e.RetryDelivery(ctx, d.ID); !IsInvalid(err) {
		t.Errorf("retrying a delivered message: %v", err)
	}
}

func TestARefusalIsNotRetried(t *testing.T) {
	w := newWorld(t)
	w.hook.fail(404) // a wrong address will be just as wrong in thirty seconds
	w.empty("wallet")
	w.dispatch()
	if d := w.deliveries()[0]; d.Status != core.DeliveryFailed || d.Attempts != 1 {
		t.Fatalf("delivery = %+v", d)
	}
}

func TestAFailingChannelIsReportedThroughTheOthers(t *testing.T) {
	w := newWorld(t)
	backup := newHook(t)
	w.channel("backup", backup)
	w.hook.fail(404)

	// Three different messages fail on "ops".
	for _, key := range []string{"a", "b", "c"} {
		w.empty(key)
		w.e.Dispatch(ctx)
	}
	got := backup.take()
	var about []string
	for _, m := range got {
		if m.Topic == core.TopicSystem {
			about = append(about, m.Title)
		}
	}
	// The incident opens in the pass that sees the third failure and is
	// delivered in the next one.
	w.e.Dispatch(ctx)
	for _, m := range backup.take() {
		if m.Topic == core.TopicSystem {
			about = append(about, m.Title)
		}
	}
	if len(about) != 1 || about[0] != "通知渠道连续失败 · ops" {
		t.Fatalf("system messages on the backup channel = %q", about)
	}
	// The broken channel is never asked to report on itself.
	for _, d := range w.deliveries() {
		if d.Channel == "ops" && strings.Contains(string(d.Message), "通知渠道连续失败") {
			t.Fatal("the failing channel was asked to announce its own failure")
		}
	}

	// It works again: the next delivery closes the incident.
	w.hook.fail(200)
	w.advance(time.Minute)
	w.empty("d")
	w.e.Dispatch(ctx)
	w.e.Dispatch(ctx)
	for _, inc := range w.open() {
		if inc.Kind == core.IncidentDelivery {
			t.Fatal("the channel incident is still open after a delivery got through")
		}
	}
	recovered := false
	for _, m := range backup.take() {
		recovered = recovered || m.Title == "通知渠道已恢复 · ops"
	}
	if !recovered {
		t.Error("the recovery was not announced")
	}
}

func TestTestChannel(t *testing.T) {
	w := newWorld(t)
	if err := w.e.TestChannel(ctx, w.ops.ID); err != nil {
		t.Fatal(err)
	}
	got := w.hook.take()
	expect(t, got, "QuotaPulse 测试消息")
	contains(t, got[0].Text, "说明渠道「ops」工作正常")
	if d := w.deliveries()[0]; d.Event != core.EventTest || d.Status != core.DeliverySent {
		t.Fatalf("log = %+v", d)
	}

	// The person pressing the button gets the platform's answer directly.
	w.hook.fail(403)
	err := w.e.TestChannel(ctx, w.ops.ID)
	if !IsInvalid(err) || !strings.HasPrefix(err.Error(), "HTTP 403") {
		t.Fatalf("err = %v", err)
	}
	if d := w.deliveries()[0]; d.Status != core.DeliveryFailed {
		t.Fatalf("log = %+v", d)
	}
}

func TestDeletedOrDisabledChannelDropsItsQueue(t *testing.T) {
	w := newWorld(t)
	w.empty("wallet")
	_, err := w.e.UpdateChannel(ctx, w.ops.ID, ChannelInput{Enabled: ptr(false)}, "zh")
	w.must(err)
	expect(t, w.dispatch())
	if d := w.deliveries()[0]; d.Status != core.DeliveryDropped {
		t.Fatalf("delivery = %+v", d)
	}
}

func TestDigest(t *testing.T) {
	w := newWorld(t)
	w.settings(func(s *core.Settings) { s.Digest = core.Digest{Enabled: true, Weekday: 1, Time: "09:00"} })

	// Something to report: a meter, an open incident, a date, a renewal.
	w.seed("deepseek", core.Balance, "CNY", 10*24, 547.67, func(int) float64 { return 0.5 })
	w.push("deepseek", 547.67)
	w.empty("wallet")
	w.dispatch()
	_, err := w.e.CreateReminder(ctx, ReminderInput{
		Title: ptr("iCloud"), Kind: ptr(core.ReminderRenewal), Rule: &recur.Rule{Freq: recur.Monthly, Day: 20},
		LeadDays: &[]int{0}, Amount: ptr(68.0),
	}, "zh")
	w.must(err)

	// Monday 16 November, before nine: not yet.
	w.at(2026, 11, 16, 8, 59)
	if sent, _ := w.e.MaybeDigest(ctx); sent {
		t.Fatal("sent before its time")
	}
	w.at(2026, 11, 16, 9, 0)
	if sent, err := w.e.MaybeDigest(ctx); err != nil || !sent {
		t.Fatalf("sent = %v, err = %v", sent, err)
	}
	// The loop comes round every minute; once is enough.
	w.advance(time.Minute)
	if sent, _ := w.e.MaybeDigest(ctx); sent {
		t.Fatal("sent twice in one day")
	}
	got := w.dispatch()
	expect(t, got, "QuotaPulse 周报 · 11月10日 – 11月16日")
	contains(t, got[0].Text,
		"需要关注", "• 余额已用尽 · wallet",
		"额度", "deepseek 547.67 CNY · 约可用 46 天",
		"本月花费", "本月续费：68.00 CNY",
		"即将到来", "11月20日 周五 · iCloud · 4 天后 · 68.00 CNY")
	if got[0].Topic != core.TopicDigest || got[0].Event != core.EventDigest {
		t.Errorf("message = %+v", got[0])
	}

	// Tuesday: not the day.
	w.at(2026, 11, 17, 9, 0)
	if sent, _ := w.e.MaybeDigest(ctx); sent {
		t.Fatal("sent on the wrong weekday")
	}
	// Next Monday, with the server down at nine: it goes out when it is back.
	w.at(2026, 11, 23, 13, 0)
	if sent, _ := w.e.MaybeDigest(ctx); !sent {
		t.Fatal("a missed digest was not caught up the same day")
	}

	// Every day.
	w.settings(func(s *core.Settings) { s.Digest.Weekday = 0 })
	w.at(2026, 11, 24, 9, 0)
	if sent, _ := w.e.MaybeDigest(ctx); !sent {
		t.Fatal("daily digest not sent")
	}
	got = w.dispatch()
	if len(got) != 2 || got[1].Title != "QuotaPulse 日报 · 11月24日" {
		t.Fatalf("titles = %q", titles(got))
	}
}

// A digest can be late, but only for an hour it was already waiting for.
func TestDigestIsNotOwedForATimeBeforeItsSchedule(t *testing.T) {
	// A new install — or a migration — that starts on Monday afternoon has
	// not missed Monday morning's digest.
	w := newWorld(t)
	w.at(2026, 11, 16, 15, 0)
	w.settings(func(s *core.Settings) { s.Digest = core.Digest{Enabled: true, Weekday: 1, Time: "09:00"} })
	for range 3 {
		if sent, err := w.e.MaybeDigest(ctx); err != nil || sent {
			t.Fatalf("sent this morning's digest on arrival: %v %v", sent, err)
		}
		w.advance(time.Minute)
	}
	w.at(2026, 11, 23, 9, 0)
	if sent, _ := w.e.MaybeDigest(ctx); !sent {
		t.Fatal("the first real digest did not go out")
	}

	// Moving it to an hour that has passed today waits for tomorrow…
	w.at(2026, 11, 25, 14, 0)
	w.settings(func(s *core.Settings) { s.Digest = core.Digest{Enabled: true, Weekday: 0, Time: "10:00"} })
	if sent, _ := w.e.MaybeDigest(ctx); sent {
		t.Fatal("sent for an hour that had passed when the schedule was set")
	}
	w.at(2026, 11, 26, 10, 0)
	if sent, _ := w.e.MaybeDigest(ctx); !sent {
		t.Fatal("the moved digest did not go out the next day")
	}
	// …and so does switching it off and on again.
	w.settings(func(s *core.Settings) { s.Digest.Enabled = false })
	w.at(2026, 11, 27, 9, 0)
	if sent, _ := w.e.MaybeDigest(ctx); sent {
		t.Fatal("sent while switched off")
	}
	w.at(2026, 11, 27, 11, 0)
	w.settings(func(s *core.Settings) { s.Digest.Enabled = true })
	if sent, _ := w.e.MaybeDigest(ctx); sent {
		t.Fatal("sent on being switched on after its hour")
	}
	w.at(2026, 11, 28, 10, 0)
	if sent, _ := w.e.MaybeDigest(ctx); !sent {
		t.Fatal("did not resume the day after")
	}
}

func TestDigestWithNothingToSay(t *testing.T) {
	w := newWorld(t)
	w.must(w.e.SendDigest(ctx))
	got := w.dispatch()
	if len(got) != 1 {
		t.Fatalf("messages = %q", titles(got))
	}
	contains(t, got[0].Text, "需要关注", "没有，一切正常。")
	if strings.Contains(got[0].Text, "额度") || strings.Contains(got[0].Text, "即将到来") {
		t.Errorf("empty sections were printed: %q", got[0].Text)
	}
}
