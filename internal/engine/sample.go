package engine

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"quotapulse/internal/core"
	"quotapulse/internal/forecast"
	"quotapulse/internal/provider"
	"quotapulse/internal/recur"
	"quotapulse/internal/store"
)

const (
	// fetchTimeout bounds one account's whole fetch, retries included.
	fetchTimeout = 60 * time.Second
	// maxConcurrentSamples keeps a restart with many accounts due at once
	// from opening a connection to every platform in the same second.
	maxConcurrentSamples = 4
)

func dateOf(t time.Time) recur.Date { return recur.DateOf(t) }

// historyStart is how far back a meter's readings are loaded for judging
// it: the forecast's lookback, or the start of the month if that is earlier,
// since the budget rule sums the month.
func historyStart(now time.Time, loc *time.Location) time.Time {
	local := now.In(loc)
	monthStart := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
	start := now.Add(-forecast.Lookback)
	if monthStart.Before(start) {
		start = monthStart
	}
	return start.Add(-time.Hour)
}

// SampleDue samples every account whose time has come, a few at a time, and
// returns how many it sampled.
func (e *Engine) SampleDue(ctx context.Context) int {
	accounts, err := e.store.DueAccounts(ctx, e.now())
	if err != nil {
		e.log.Error("could not list due accounts", "error", err)
		return 0
	}
	var wg sync.WaitGroup
	slots := make(chan struct{}, maxConcurrentSamples)
	sampled := 0
	for _, account := range accounts {
		if _, busy := e.inflight.LoadOrStore(account.ID, true); busy {
			continue
		}
		sampled++
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer func() {
				<-slots
				e.inflight.Delete(account.ID)
				wg.Done()
			}()
			if err := e.sample(ctx, account); err != nil {
				e.log.Error("sampling failed", "account", account.Name, "error", err)
			}
		}()
	}
	wg.Wait()
	return sampled
}

// SampleAccount samples one account now and waits for the result.
func (e *Engine) SampleAccount(ctx context.Context, id string) error {
	account, err := e.store.Account(ctx, id)
	if err != nil {
		return err
	}
	if _, busy := e.inflight.LoadOrStore(id, true); busy {
		return nil // already under way; its result is as fresh as ours would be
	}
	defer e.inflight.Delete(id)
	return e.sample(ctx, account)
}

// RequestSample makes accounts due immediately; the background loop picks
// them up. An empty id means every enabled account.
func (e *Engine) RequestSample(ctx context.Context, id string) error {
	now := e.now()
	if id != "" {
		if err := e.store.ScheduleAccount(ctx, id, now); err != nil {
			return err
		}
	} else {
		accounts, err := e.store.Accounts(ctx)
		if err != nil {
			return err
		}
		for _, account := range accounts {
			if account.Enabled {
				_ = e.store.ScheduleAccount(ctx, account.ID, now)
			}
		}
	}
	e.nudge()
	return nil
}

// sample fetches an account's meters, records the outcome, and judges what
// it means.
func (e *Engine) sample(ctx context.Context, account core.Account) error {
	settings, err := e.store.Settings(ctx)
	if err != nil {
		return err
	}
	var result provider.Result
	def, fetchErr := e.Definition(ctx, account.Provider)
	switch {
	case account.Unreadable:
		fetchErr = errors.New("the saved credential cannot be read: QUOTAPULSE_SECRET is not the one it was saved with. Enter the credential again")
	case errors.Is(fetchErr, store.ErrNotFound):
		fetchErr = errors.New("the provider “" + account.Provider + "” is no longer defined")
	case fetchErr == nil:
		fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
		client := *e.fetch
		client.Now, client.Location = e.now, settings.Location()
		result, fetchErr = client.Fetch(fetchCtx, def, account.Credential)
		cancel()
	}
	if ctx.Err() != nil {
		return ctx.Err() // shutting down: an interrupted fetch says nothing about the account
	}

	now := e.now()
	interval := time.Duration(settings.SampleSeconds) * time.Second
	if account.Interval > 0 {
		interval = time.Duration(account.Interval) * time.Second
	}
	outcome := store.Outcome{At: now, Latency: result.Latency, Language: settings.Language, NextAt: now.Add(interval)}
	if fetchErr != nil {
		outcome.Err = fetchErr.Error()
		outcome.NextAt = now.Add(retryDelay(account.Failures+1, interval))
		e.log.Warn("sample failed", "account", account.Name, "error", outcome.Err, "failures", account.Failures+1)
	} else {
		outcome.Samples = result.Samples
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	meters, err := e.store.SaveOutcome(ctx, account.ID, outcome)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil // deleted while we were fetching
		}
		return err
	}
	account, err = e.store.Account(ctx, account.ID)
	if err != nil {
		return err
	}
	if err := e.judgeAccount(ctx, account, settings); err != nil {
		return err
	}
	for _, meter := range meters {
		if err := e.judgeMeter(ctx, meter, settings); err != nil {
			return err
		}
	}
	e.hub.Publish(ChangedMeters)
	return nil
}

// retryDelay looks again soon after a failure — to confirm it, and to notice
// a recovery quickly — then falls back to the normal rhythm.
func retryDelay(failures int, interval time.Duration) time.Duration {
	switch failures {
	case 1:
		return min(time.Minute, interval)
	case 2:
		return min(5*time.Minute, interval)
	}
	return interval
}

// judgeAccount reconciles whether an account can be read. Caller holds e.mu.
func (e *Engine) judgeAccount(ctx context.Context, account core.Account, s core.Settings) error {
	sub := subject{Type: core.SubjectAccount, ID: account.ID, Title: account.Name}
	open, err := e.store.OpenIncidentsOf(ctx, core.SubjectAccount, account.ID)
	if err != nil {
		return err
	}
	var openedAt *time.Time
	for _, inc := range open {
		if inc.Kind == core.IncidentFailing {
			openedAt = &inc.OpenedAt
		}
	}
	changed, err := e.reconcile(ctx, sub, []string{core.IncidentFailing}, accountConditions(account, openedAt, e.now()), nil, s)
	if changed {
		e.hub.Publish(ChangedIncidents)
	}
	return err
}

// judgeMeter reconciles everything about one meter. Caller holds e.mu.
func (e *Engine) judgeMeter(ctx context.Context, meter core.Meter, s core.Settings) error {
	now := e.now()
	if meter.Hidden {
		// Hiding a meter says "I do not care about this one". Whatever was
		// open on it ends quietly; "recovered" would be a lie.
		return e.closeSubject(ctx, core.SubjectMeter, meter.ID)
	}
	open, err := e.store.OpenIncidentsOf(ctx, core.SubjectMeter, meter.ID)
	if err != nil {
		return err
	}
	byKind := map[string]core.Incident{}
	for _, inc := range open {
		byKind[inc.Kind] = inc
	}
	readings, err := e.store.Readings(ctx, meter.ID, historyStart(now, s.Location()))
	if err != nil {
		return err
	}
	title, err := e.meterTitle(ctx, meter)
	if err != nil {
		return err
	}
	conds := meterConditions(meterInput{Meter: meter, Readings: readings, Settings: s, Now: now, Open: byKind})
	sub := subject{
		Type: core.SubjectMeter, ID: meter.ID, Title: title,
		Muted: meter.MutedUntil != nil && meter.MutedUntil.After(now),
	}
	kinds := []string{core.IncidentLevel, core.IncidentBudget, core.IncidentStale}
	changed, err := e.reconcile(ctx, sub, kinds, conds, currentFacts(meter, readings, s, now), s)
	if changed {
		e.hub.Publish(ChangedIncidents)
	}
	return err
}

// currentFacts describes a meter as it is right now, for the message that
// says an incident is over.
func currentFacts(m core.Meter, readings []core.Reading, s core.Settings, now time.Time) map[string]map[string]any {
	if m.Value == nil {
		return nil
	}
	level := LevelData{MeterKind: m.Kind, Unit: m.Unit, Value: *m.Value, Limit: m.Limit, Low: m.Rules.Low}
	if m.Kind == core.Quota {
		q := forecast.ForQuota(*m.Value, m.Limit, m.ResetsAt, m.Window, now)
		level.RemainingPct, level.ResetsAt = &q.RemainingPct, m.ResetsAt
	} else {
		fc := forecast.ForBalance(readings, m.Kind)
		level.Burn, level.RunwayDays, level.DepletesAt, level.LeadDays = fc.Burn, fc.RunwayDays, fc.DepletesAt, lead(m, s)
	}
	return map[string]map[string]any{core.IncidentLevel: toMap(level)}
}

// meterTitle is what a meter is called in a message or a list: the account's
// name when that says it all, "account · meter" when the account has several.
func (e *Engine) meterTitle(ctx context.Context, meter core.Meter) (string, error) {
	if meter.AccountID == "" {
		return meter.Name, nil
	}
	account, err := e.store.Account(ctx, meter.AccountID)
	if err != nil {
		return meter.Name, err
	}
	siblings, err := e.store.MetersOf(ctx, meter.AccountID)
	if err != nil {
		return meter.Name, err
	}
	return displayName(meter, account.Name, visibleCount(siblings)), nil
}

func visibleCount(meters []core.Meter) int {
	n := 0
	for _, m := range meters {
		if !m.Hidden {
			n++
		}
	}
	return n
}

func displayName(meter core.Meter, accountName string, siblings int) string {
	// A name someone chose is the name; the rest is what to call a meter
	// nobody has named.
	if meter.Renamed || meter.AccountID == "" || accountName == "" {
		return meter.Name
	}
	if siblings <= 1 {
		return accountName
	}
	return accountName + " · " + meter.Name
}

// ---------- pushed readings ----------

// Push is a reading reported from outside, by a script or an agent.
type Push struct {
	// Meter is the key that identifies the pushed meter.
	Meter string `json:"meter"`
	// Name, Kind and Unit describe the meter. Kind is required the first
	// time a key is seen; after that all three are optional and, when
	// given, update the meter.
	Name  string    `json:"name,omitempty"`
	Kind  core.Kind `json:"kind,omitempty"`
	Unit  string    `json:"unit,omitempty"`
	Value *float64  `json:"value"`
	Limit *float64  `json:"limit,omitempty"`
	// ResetsAt and Window describe a quota's window.
	ResetsAt *time.Time `json:"resets_at,omitempty"`
	Window   *int64     `json:"window_seconds,omitempty"`
	// At is when the reading was taken; now when omitted.
	At *time.Time `json:"at,omitempty"`
}

var pushKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// A reading may be reported late, but not from the future: a wrong clock on
// the reporting machine would otherwise shadow every honest reading after it.
const maxFutureSkew = 5 * time.Minute

// Ingest records pushed readings and judges the meters they belong to.
func (e *Engine) Ingest(ctx context.Context, pushes []Push) ([]core.Meter, error) {
	if len(pushes) == 0 {
		return nil, invalid("no readings given")
	}
	if len(pushes) > 500 {
		return nil, invalid("at most 500 readings per request")
	}
	settings, err := e.store.Settings(ctx)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	now := e.now()
	touched := map[string]core.Meter{}
	var order []string
	for i, p := range pushes {
		key := strings.TrimSpace(p.Meter)
		switch {
		case !pushKeyPattern.MatchString(key):
			return nil, invalid("reading %d: meter must be lowercase letters, digits, dots, dashes or underscores", i+1)
		case p.Value == nil:
			return nil, invalid("reading %d (%s): value is required", i+1, key)
		case p.Kind != "" && !p.Kind.Valid():
			return nil, invalid("reading %d (%s): kind must be balance, quota or counter", i+1, key)
		case p.Limit != nil && *p.Limit < 0:
			return nil, invalid("reading %d (%s): limit cannot be negative", i+1, key)
		case p.Window != nil && *p.Window <= 0:
			return nil, invalid("reading %d (%s): window_seconds must be positive", i+1, key)
		}
		at := now
		if p.At != nil {
			if p.At.After(now.Add(maxFutureSkew)) {
				return nil, invalid("reading %d (%s): at is in the future", i+1, key)
			}
			at = *p.At
		}

		meter, known := touched[key]
		if !known {
			meter, err = e.store.PushedMeter(ctx, key)
			switch {
			case errors.Is(err, store.ErrNotFound):
				if p.Kind == "" {
					return nil, invalid("reading %d (%s): this meter is new, so kind is required: balance, quota or counter", i+1, key)
				}
				meter = core.Meter{Key: key, Name: orText(strings.TrimSpace(p.Name), key), Kind: p.Kind, Unit: strings.TrimSpace(p.Unit)}
				if err := e.store.CreateMeter(ctx, &meter, now); err != nil {
					return nil, err
				}
			case err != nil:
				return nil, err
			}
			order = append(order, key)
		}
		if described := describe(&meter, p); described {
			if err := e.store.UpdateMeter(ctx, meter, now); err != nil {
				return nil, err
			}
		}
		window := p.Window
		if window == nil {
			window = meter.Window
		}
		reading := core.Reading{At: at, Value: *p.Value, Limit: p.Limit, ResetsAt: p.ResetsAt}
		if err := e.store.Record(ctx, &meter, reading, window); err != nil {
			return nil, err
		}
		touched[key] = meter
	}

	meters := make([]core.Meter, 0, len(order))
	for _, key := range order {
		meter := touched[key]
		if err := e.judgeMeter(ctx, meter, settings); err != nil {
			return nil, err
		}
		meters = append(meters, meter)
	}
	e.hub.Publish(ChangedMeters)
	return meters, nil
}

// describe applies a push's optional description to a meter and reports
// whether anything changed.
func describe(meter *core.Meter, p Push) bool {
	changed := false
	if name := strings.TrimSpace(p.Name); name != "" && name != meter.Name && meter.Name == meter.Key {
		// Only fill in a name that was never set; a name chosen in the
		// dashboard is the user's.
		meter.Name, changed = name, true
	}
	if p.Kind != "" && p.Kind != meter.Kind {
		meter.Kind, changed = p.Kind, true
	}
	if unit := strings.TrimSpace(p.Unit); unit != "" && unit != meter.Unit {
		meter.Unit, changed = unit, true
	}
	return changed
}

// EvaluateStale judges the pushed meters that are expected to keep
// reporting. It runs on a timer: silence, by its nature, never triggers
// anything on its own.
func (e *Engine) EvaluateStale(ctx context.Context) error {
	settings, err := e.store.Settings(ctx)
	if err != nil {
		return err
	}
	meters, err := e.store.Meters(ctx)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, meter := range meters {
		if meter.Rules.StaleAfter == nil {
			continue
		}
		if err := e.judgeMeter(ctx, meter, settings); err != nil {
			return err
		}
	}
	return nil
}

// EvaluateAll judges every account and meter again. Changing a default —
// the lead time, say — can open or close incidents with no new reading.
func (e *Engine) EvaluateAll(ctx context.Context) error {
	settings, err := e.store.Settings(ctx)
	if err != nil {
		return err
	}
	accounts, err := e.store.Accounts(ctx)
	if err != nil {
		return err
	}
	meters, err := e.store.Meters(ctx)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, account := range accounts {
		if err := e.judgeAccount(ctx, account, settings); err != nil {
			return err
		}
	}
	for _, meter := range meters {
		if err := e.judgeMeter(ctx, meter, settings); err != nil {
			return err
		}
	}
	return nil
}
