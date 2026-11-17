package core

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultSettingsAreValid(t *testing.T) {
	if err := DefaultSettings().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsValidationReportsEverythingAtOnce(t *testing.T) {
	s := DefaultSettings()
	s.Timezone, s.Language, s.BaseCurrency = "Mars/Olympus", "fr", "dollars"
	s.FX = map[string]float64{"usd": 7, "EUR": -1}
	s.SampleSeconds, s.RunwayDays, s.ReminderTime = 5, -1, "9am"
	s.Quiet.Start, s.Digest.Weekday, s.Digest.Time = "25:00", 9, ""
	s.RenotifyHours, s.RetentionDays, s.UpcomingDays, s.PublicURL = -1, 1, 0, "pulse.example"
	err := s.Validate()
	if err == nil {
		t.Fatal("invalid settings passed")
	}
	for _, field := range []string{
		"timezone", "language", "base_currency", `fx: "usd"`, "fx: the rate for EUR", "sample_seconds", "runway_days",
		"reminder_time", "quiet.start", "digest.weekday", "digest.time", "renotify_hours", "retention_days",
		"upcoming_days", "public_url",
	} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("the error does not mention %s: %v", field, err)
		}
	}
	// A switched-off feature is not checked: nobody should have to fix a
	// time they are not using.
	off := DefaultSettings()
	off.Quiet = QuietHours{Enabled: false, Start: "nonsense", End: ""}
	off.Digest = Digest{Enabled: false, Time: "nonsense"}
	if err := off.Validate(); err != nil {
		t.Errorf("disabled features were validated: %v", err)
	}
}

func TestRates(t *testing.T) {
	s := DefaultSettings()
	s.BaseCurrency, s.FX = "CNY", map[string]float64{"USD": 7.16, "JPY": 0}
	if rate, ok := s.Rate("CNY"); !ok || rate != 1 {
		t.Errorf("the base is worth one of itself: %v %v", rate, ok)
	}
	if rate, ok := s.Rate("USD"); !ok || rate != 7.16 {
		t.Errorf("USD = %v %v", rate, ok)
	}
	for _, unit := range []string{"EUR", "JPY", "%", "credits"} {
		if _, ok := s.Rate(unit); ok {
			t.Errorf("%s has no usable rate", unit)
		}
	}
}

func TestParseClock(t *testing.T) {
	for text, want := range map[string]Clock{"00:00": 0, "09:00": 540, "9:05": 545, "23:59": 1439, " 08:30 ": 510} {
		if got, err := ParseClock(text); err != nil || got != want {
			t.Errorf("ParseClock(%q) = %v, %v", text, got, err)
		}
	}
	for _, text := range []string{"", "9", "24:00", "12:60", "12:5", "noon", "12:00:00", "-1:00"} {
		if _, err := ParseClock(text); err == nil {
			t.Errorf("ParseClock(%q) passed", text)
		}
	}
}

func TestQuietHours(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 11, day, hour, minute, 0, 0, loc) }
	night := QuietHours{Enabled: true, Start: "23:00", End: "08:00"}
	cases := []struct {
		now  time.Time
		want time.Time
	}{
		{at(16, 22, 59), time.Time{}},
		{at(16, 23, 0), at(17, 8, 0)}, // before midnight: ends tomorrow morning
		{at(17, 2, 30), at(17, 8, 0)}, // after midnight: ends this morning
		{at(17, 7, 59), at(17, 8, 0)},
		{at(17, 8, 0), time.Time{}}, // the end itself is outside
		{at(17, 12, 0), time.Time{}},
	}
	for _, c := range cases {
		if got := night.QuietUntil(c.now); !got.Equal(c.want) {
			t.Errorf("at %s: quiet until %v, want %v", c.now.Format("02 15:04"), got, c.want)
		}
	}
	// A window that does not cross midnight.
	siesta := QuietHours{Enabled: true, Start: "13:00", End: "14:30"}
	if got := siesta.QuietUntil(at(16, 13, 30)); !got.Equal(at(16, 14, 30)) {
		t.Errorf("siesta until %v", got)
	}
	if got := siesta.QuietUntil(at(16, 15, 0)); !got.IsZero() {
		t.Errorf("after the siesta: %v", got)
	}
	// Off, empty or nonsense means never quiet.
	for _, q := range []QuietHours{{Start: "23:00", End: "08:00"}, {Enabled: true, Start: "x", End: "08:00"}, {Enabled: true, Start: "08:00", End: "08:00"}} {
		if got := q.QuietUntil(at(17, 2, 0)); !got.IsZero() {
			t.Errorf("%+v: quiet until %v", q, got)
		}
	}
}

func TestIsCurrency(t *testing.T) {
	for _, unit := range []string{"USD", "CNY", "EUR"} {
		if !IsCurrency(unit) {
			t.Errorf("%s is a currency code", unit)
		}
	}
	for _, unit := range []string{"", "%", "usd", "Usd", "US", "USDT", "calls", "credits", "U$D", "元"} {
		if IsCurrency(unit) {
			t.Errorf("%q is not a currency code", unit)
		}
	}
}

func TestChannelWants(t *testing.T) {
	c := Channel{Enabled: true, Topics: []string{TopicMeters, TopicSystem}, MinSeverity: Warning}
	if !c.Wants(TopicMeters, Critical) || !c.Wants(TopicSystem, Warning) {
		t.Error("wanted topics at or above the floor")
	}
	if c.Wants(TopicMeters, Info) || c.Wants(TopicDates, Critical) {
		t.Error("below the floor, or a topic it did not ask for")
	}
	c.Enabled = false
	if c.Wants(TopicMeters, Critical) {
		t.Error("a disabled channel wants nothing")
	}
}

func TestTopics(t *testing.T) {
	for kind, want := range map[string]string{
		IncidentLevel: TopicMeters, IncidentBudget: TopicMeters, IncidentStale: TopicMeters,
		IncidentDue: TopicDates, IncidentFailing: TopicSystem, IncidentDelivery: TopicSystem,
	} {
		if got := TopicOf(kind); got != want {
			t.Errorf("TopicOf(%s) = %s, want %s", kind, got, want)
		}
	}
}

func TestTokens(t *testing.T) {
	now := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	reader := Token{Scopes: []string{ScopeRead}}
	if !reader.Allows(ScopeRead) || reader.Allows(ScopeAct) || reader.Expired(now) {
		t.Error("a read token reads, and a token without an expiry never expires")
	}
	admin := Token{Scopes: []string{ScopeAdmin}}
	for _, scope := range Scopes {
		if !admin.Allows(scope) {
			t.Errorf("admin lacks %s", scope)
		}
	}
	expiring := Token{ExpiresAt: &now}
	if !expiring.Expired(now) || expiring.Expired(now.Add(-time.Second)) {
		t.Error("a token expires at its expiry, not before")
	}
}

func TestSeverityAndKinds(t *testing.T) {
	if !(Critical.Rank() > Warning.Rank() && Warning.Rank() > Info.Rank() && Info.Rank() > Severity("loud").Rank()) {
		t.Error("severities are out of order")
	}
	if Severity("loud").Valid() || !Warning.Valid() {
		t.Error("Valid")
	}
	if !Balance.Valid() || !Quota.Valid() || !Counter.Valid() || Kind("gauge").Valid() {
		t.Error("kinds")
	}
	renewal, birthday := Reminder{Kind: ReminderRenewal}, Reminder{Kind: ReminderBirthday}
	if !renewal.NeedsAction() || !(Reminder{Kind: ReminderExpiry}).NeedsAction() || birthday.NeedsAction() {
		t.Error("a renewal or an expiry needs doing; a birthday needs remembering")
	}
}

func TestNewID(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := NewID("mtr")
		if !strings.HasPrefix(id, "mtr_") || len(id) != 17 || strings.ToLower(id) != id || seen[id] {
			t.Fatalf("id = %q", id)
		}
		seen[id] = true
	}
}
