// Package metrics translates every check round into Prometheus metrics.
//
// Metric and label names are external contracts (see grafana/README.md):
// existing panels, alert rules and scrape configs hardcode these names — one
// changed character turns live dashboards into No Data, so no "drive-by
// polishing" of the string literals here.
//
// Metrics update only in the process running the scheduled jobs; results from
// manual command-line runs never enter them — otherwise one human
// investigation would leave a fake check spike on the curves.
package metrics

import (
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/itswl/quotapulse/internal/model"
)

// Label order equals the argument order of WithLabelValues / DeleteLabelValues; swapping writes series to wrong slots.
var (
	balanceLabels      = []string{"project", "provider", "type"}
	subscriptionLabels = []string{"name", "cycle_type"}
	mailboxLabels      = []string{"mailbox"}
	taskLabels         = []string{"task"}
)

// Series groups: Gauges in one group share a label set and a lifecycle and
// are pruned together.
const (
	groupBalance      = "balance" // balance, threshold, ratio, status, spend, runway
	groupCheck        = "check"   // check success/failure, a separate group: a failed project updates it while the balance family stays as-is
	groupSubscription = "subscription"
	groupMailbox      = "mailbox"
)

// Label count per group. labelValues is a fixed array; the first n are the group's real values.
var groupArity = map[string]int{
	groupBalance:      len(balanceLabels),
	groupCheck:        len(balanceLabels),
	groupSubscription: len(subscriptionLabels),
	groupMailbox:      len(mailboxLabels),
}

// labelValues is a fixed-size array, not a slice: slices are not comparable and
// cannot be map keys, and each group's label count is fixed (balance 3,
// subscription 2, mailbox 1); spare slots stay empty.
type labelValues [3]string

// Fallback for missing label values, avoiding series like project="" that no panel could identify.
const unknown = "unknown"

// Series groups: Gauges in one group share a label set and a lifecycle and
// are pruned together.
const (
	statusSuccess = "success"
	statusFailed  = "failed"
)

// Collector owns all metrics and remembers each group's live label combinations,
// so stale series disappear after a project / subscription / mailbox is deleted
// or renamed.
type Collector struct {
	// balances
	balance     *prometheus.GaugeVec
	threshold   *prometheus.GaugeVec
	ratio       *prometheus.GaugeVec
	status      *prometheus.GaugeVec
	checkStatus *prometheus.GaugeVec
	burnRate    *prometheus.GaugeVec
	runwayDays  *prometheus.GaugeVec

	// subscription renewals
	subDays   *prometheus.GaugeVec
	subAmount *prometheus.GaugeVec
	subStatus *prometheus.GaugeVec

	// mailbox scans
	mailboxStatus  *prometheus.GaugeVec
	lastScanEmails *prometheus.GaugeVec
	lastScanAlerts *prometheus.GaugeVec
	scanTotal      *prometheus.CounterVec
	alertsTotal    *prometheus.CounterVec

	// scheduled jobs
	jobLastRun      *prometheus.GaugeVec
	jobLastSuccess  *prometheus.GaugeVec
	jobLastStatus   *prometheus.GaugeVec
	jobLastDuration *prometheus.GaugeVec
	jobRuns         *prometheus.CounterVec

	// last-update timestamps for notifications and each check kind
	notifications *prometheus.CounterVec
	lastCheck     *prometheus.GaugeVec

	// HTTP layer, filled by Middleware
	httpRequests *prometheus.CounterVec
	httpDuration *prometheus.HistogramVec

	gatherer prometheus.Gatherer

	// The Vecs are concurrency-safe; this lock only guards the series registry
	// below: the scheduler thread and HTTP handlers both update, and the registry
	// is a plain map needing serialized access.
	mu     sync.Mutex
	series map[string]map[labelValues]struct{}

	now func() time.Time // replaced with a fixed clock in tests
}

// New registers every metric. Pass nil reg for the default registry.
func New(reg prometheus.Registerer) *Collector {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	f := promauto.With(reg)
	gauge := func(name, help string, labels []string) *prometheus.GaugeVec {
		return f.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: help}, labels)
	}
	counter := func(name, help string, labels []string) *prometheus.CounterVec {
		return f.NewCounterVec(prometheus.CounterOpts{Name: name, Help: help}, labels)
	}

	histogram := func(name, help string, labels []string) *prometheus.HistogramVec {
		return f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    name,
			Help:    help,
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, labels)
	}

	c := &Collector{
		balance:   gauge("quotapulse_balance", "Current balance or credits", balanceLabels),
		threshold: gauge("quotapulse_threshold", "Alert threshold", balanceLabels),
		ratio:     gauge("quotapulse_ratio", "Balance to threshold ratio", balanceLabels),
		status:    gauge("quotapulse_status", "Balance status (1=ok, 0=alert)", balanceLabels),
		checkStatus: gauge("quotapulse_check_status",
			"Result of the last balance check (1=success, 0=failed; balance keeps its last good value)", balanceLabels),
		burnRate: gauge("quotapulse_burn_rate_per_day",
			"Average daily consumption over the burn-rate window", balanceLabels),
		runwayDays: gauge("quotapulse_runway_days",
			"Days of runway left at the current burn rate", balanceLabels),

		subDays:   gauge("quotapulse_subscription_days", "Days until subscription renewal", subscriptionLabels),
		subAmount: gauge("quotapulse_subscription_amount", "Subscription renewal amount", subscriptionLabels),
		subStatus: gauge("quotapulse_subscription_status",
			"Subscription status (1=normal, 0=needs_renewal, -1=renewed_in_cycle)", subscriptionLabels),

		mailboxStatus: gauge("quotapulse_email_mailbox_status",
			"Mailbox status in the last scan (1=ok, 0=failed)", mailboxLabels),
		lastScanEmails: gauge("quotapulse_email_last_scan_emails", "Emails scanned in the last scan", mailboxLabels),
		lastScanAlerts: gauge("quotapulse_email_last_scan_alerts", "Alert emails found in the last scan", mailboxLabels),
		scanTotal:      counter("quotapulse_email_scan_total", "Total emails scanned", mailboxLabels),
		alertsTotal:    counter("quotapulse_email_alerts_total", "Total alert emails found", mailboxLabels),

		// The label is task, not job: at scrape time Prometheus renames a label colliding with its built-in job label to exported_job.
		jobLastRun:      gauge("quotapulse_job_last_run_timestamp", "Unix time of the last run", taskLabels),
		jobLastSuccess:  gauge("quotapulse_job_last_success_timestamp", "Unix time of the last successful run", taskLabels),
		jobLastStatus:   gauge("quotapulse_job_last_status", "Result of the last run (1=success, 0=failed)", taskLabels),
		jobLastDuration: gauge("quotapulse_job_last_duration_seconds", "Duration of the last run in seconds", taskLabels),
		jobRuns:         counter("quotapulse_job_runs_total", "Job runs by result", []string{"task", "status"}),

		notifications: counter("quotapulse_notifications_total",
			"Webhook notifications by kind and result", []string{"kind", "status"}),
		lastCheck: gauge("quotapulse_last_check_timestamp", "Timestamp of last check", []string{"check_type"}),

		httpRequests: counter("quotapulse_http_requests_total",
			"HTTP requests by matched route, method and status code", httpRequestLabels),
		httpDuration: histogram("quotapulse_http_request_duration_seconds",
			"HTTP request latency by matched route and method", httpDurationLabels),

		series: map[string]map[labelValues]struct{}{},
		now:    time.Now,
	}

	// Handler wants a Gatherer. The default registry and *prometheus.Registry both
	// qualify; for a wrapper that only implements Registerer, fall back to the
	// default so /metrics still comes up.
	if g, ok := reg.(prometheus.Gatherer); ok {
		c.gatherer = g
	} else {
		c.gatherer = prometheus.DefaultGatherer
	}
	return c
}

// Handler exposes /metrics.
func (c *Collector) Handler() http.Handler {
	return promhttp.HandlerFor(c.gatherer, promhttp.HandlerOpts{})
}

// set writes a Gauge and records the label combination in the group's registry for the next prune. Callers must hold the lock.
func (c *Collector) set(vec *prometheus.GaugeVec, group string, lv labelValues, value float64) {
	vec.WithLabelValues(lv[:groupArity[group]]...).Set(value)
	set, ok := c.series[group]
	if !ok {
		set = map[labelValues]struct{}{}
		c.series[group] = set
	}
	set[lv] = struct{}{}
}

// prune deletes from the group's Gauges the label combinations missing from
// keep — after a project is renamed or retired the panel must not keep a
// series that no longer exists, or statistics like count() stay inflated
// forever.
//
// Vec.Reset() cannot be used here: Reset clears the whole Vec including
// series that were not updated this round but must stay — the last-known
// balance kept on a failed check is exactly that kind. So delete by exact
// labels. Cumulative Counters (mailbox scan totals and friends) are not
// deleted either: restarting from 0 would show increase() a false reset.
func (c *Collector) prune(group string, keep map[labelValues]struct{}, vecs ...*prometheus.GaugeVec) {
	n := groupArity[group]
	for lv := range c.series[group] {
		if _, ok := keep[lv]; ok {
			continue
		}
		for _, vec := range vecs {
			vec.DeleteLabelValues(lv[:n]...)
		}
	}
	c.series[group] = keep
}

// UpdateBalance updates the balance metric family. An empty slice means "no projects at all" and clears every old series.
func (c *Collector) UpdateBalance(results []model.CheckResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	keepBalance := map[labelValues]struct{}{}
	keepCheck := map[labelValues]struct{}{}

	for _, r := range results {
		lv := labelValues{orUnknown(r.Project), orUnknown(r.Provider), orUnknown(r.Type)}
		keepCheck[lv] = struct{}{}

		if r.Success {
			c.recordBalance(lv, r)
			keepBalance[lv] = struct{}{}
			continue
		}

		// when the balance is unknown, only check_status drops to 0 and the balance
		// family stays byte-for-byte: the panel should show "last known balance + this
		// check failed", not a balance vanishing into thin air. Matching by
		// project+provider prefix because a failed result's type need not equal the
		// last successful one.
		c.set(c.checkStatus, groupCheck, lv, 0)
		for old := range c.series[groupBalance] {
			if old[0] == lv[0] && old[1] == lv[1] {
				keepBalance[old] = struct{}{}
			}
		}
	}

	c.prune(groupBalance, keepBalance,
		c.balance, c.threshold, c.ratio, c.status, c.burnRate, c.runwayDays)
	c.prune(groupCheck, keepCheck, c.checkStatus)
	c.lastCheck.WithLabelValues("balance").Set(unixSeconds(c.now()))
}

func (c *Collector) recordBalance(lv labelValues, r model.CheckResult) {
	credits := deref(r.Credits)
	threshold := deref(r.Threshold)

	c.set(c.checkStatus, groupCheck, lv, 1)
	c.set(c.balance, groupBalance, lv, credits)
	c.set(c.threshold, groupBalance, lv, threshold)

	// a zero threshold writes ratio 0: division by zero yields +Inf and blows up gauge panels
	ratio := 0.0
	if threshold > 0 {
		ratio = credits / threshold
	}
	c.set(c.ratio, groupBalance, lv, ratio)

	ok := 1.0
	if r.NeedAlarm {
		ok = 0
	}
	c.set(c.status, groupBalance, lv, ok)

	// spend profiles need enough history; without it these two are simply not
	// written, which renders as "no data" rather than 0 — 0 would trip
	// runway_days < 3 style alerts as "about to run out".
	if r.Runway != nil {
		if r.Runway.BurnPerDay != nil {
			c.set(c.burnRate, groupBalance, lv, *r.Runway.BurnPerDay)
		}
		if r.Runway.RunwayDays != nil {
			c.set(c.runwayDays, groupBalance, lv, *r.Runway.RunwayDays)
		}
	}
}

// UpdateSubscriptions updates renewal metrics. When the feature is off the caller passes an empty slice and old series are cleared.
func (c *Collector) UpdateSubscriptions(results []model.SubscriptionResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	keep := map[labelValues]struct{}{}
	for _, r := range results {
		lv := labelValues{orUnknown(r.Name), orDefault(r.CycleType, model.CycleMonthly)}

		// -1 means "renewed this cycle", distinct from 0 (due now): neither is 1 on
		// the panel, but a renewed one must not alert again.
		status := 1.0
		switch {
		case r.AlreadyRenewed:
			status = -1
		case r.NeedAlert:
			status = 0
		}

		c.set(c.subDays, groupSubscription, lv, float64(r.DaysUntilRenewal))
		c.set(c.subAmount, groupSubscription, lv, r.Amount)
		c.set(c.subStatus, groupSubscription, lv, status)
		keep[lv] = struct{}{}
	}

	c.prune(groupSubscription, keep, c.subDays, c.subAmount, c.subStatus)
	c.lastCheck.WithLabelValues("subscription").Set(unixSeconds(c.now()))
}

// UpdateEmailScan updates mailbox metrics: Gauges reflect the last scan, Counters only grow.
func (c *Collector) UpdateEmailScan(result model.ScanResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	keep := map[labelValues]struct{}{}
	for _, m := range result.Mailboxes {
		lv := labelValues{orUnknown(m.Name)}

		ok := 0.0
		if m.Success && m.Error == nil {
			ok = 1
		}
		c.set(c.mailboxStatus, groupMailbox, lv, ok)
		c.set(c.lastScanEmails, groupMailbox, lv, float64(m.TotalEmails))
		c.set(c.lastScanAlerts, groupMailbox, lv, float64(m.AlertCount))

		c.scanTotal.WithLabelValues(lv[0]).Add(float64(m.TotalEmails))
		c.alertsTotal.WithLabelValues(lv[0]).Add(float64(m.AlertCount))
		keep[lv] = struct{}{}
	}

	c.prune(groupMailbox, keep, c.mailboxStatus, c.lastScanEmails, c.lastScanAlerts)
	c.lastCheck.WithLabelValues("email").Set(unixSeconds(c.now()))
}

// ---------- Scheduled jobs / notifications ----------

// RecordJobRun records one task run. A zero startedAt counts as now.
//
// The job family does no series pruning: the task list is hardcoded, unlike
// projects, which get deleted or renamed.
func (c *Collector) RecordJobRun(task string, success bool, startedAt time.Time, duration time.Duration) {
	ts := unixSeconds(startedAt)
	if startedAt.IsZero() {
		ts = unixSeconds(c.now())
	}

	c.jobLastRun.WithLabelValues(task).Set(ts)
	c.jobLastDuration.WithLabelValues(task).Set(duration.Seconds())

	status := statusFailed
	result := 0.0
	if success {
		status, result = statusSuccess, 1
	}
	c.jobLastStatus.WithLabelValues(task).Set(result)
	c.jobRuns.WithLabelValues(task, status).Inc()

	// failures leave last_success untouched: alert rules rely on time() - last_success to flag stale jobs
	if success {
		c.jobLastSuccess.WithLabelValues(task).Set(ts)
	}
}

// RecordNotification records one webhook notification. kind values are
// documented in grafana/README.md: balance / subscription / email /
// mailbox_error / runway / spend_spike / weekly_report.
// balance / subscription / email / mailbox_error / runway / spend_spike / weekly_report。
func (c *Collector) RecordNotification(kind string, ok bool) {
	status := statusFailed
	if ok {
		status = statusSuccess
	}
	c.notifications.WithLabelValues(kind, status).Inc()
}

// ---------- Small helpers ----------

func orUnknown(s string) string { return orDefault(s, unknown) }

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// deref folds "no value" into 0: balance metrics must be numbers; pointer semantics only exist in API responses.
func deref(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

// unixSeconds converts to fractional Unix seconds. Sub-second precision matters
// because a check round often takes under a second; integer seconds would make
// "time since last check" panels all zero.
func unixSeconds(t time.Time) float64 {
	return float64(t.Unix()) + float64(t.Nanosecond())/1e9
}
