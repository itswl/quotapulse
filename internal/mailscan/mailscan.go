// Package mailscan scans IMAP mailboxes and turns "arrears / renewal /
// suspension" reminder mails into alerts.
//
// Many bills only reach the inbox, and by the time someone reads them the
// service is already down — so somebody has to read mail for the on-call.
// Mailboxes scan concurrently; one being unreachable does not affect the
// others: it records an error and sends its own system alert.
//
// Dedup has two layers: within one scan the same mail (Message-ID) is handled
// once, a set shared across mailboxes because one notification is often cc'd
// to several scanned inboxes; across scans the store is asked whether the same
// mail notified within recent days.
package mailscan

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/notify"
	"github.com/itswl/quotapulse/internal/store"
)

// A few caps for the scanning process.
const (
	maxMailboxWorkers = 5    // mailboxes scanned concurrently
	batchSize         = 100  // mails per IMAP FETCH
	defaultMaxEmails  = 1000 // cap when MaxEmails is unset; the MAX_EMAILS_TO_SCAN default

	// Connection retry cadence: three attempts with two 4-second waits.
	// Network blips and provider throttling are common; a retry usually passes.
	connectAttempts   = 3
	connectRetryDelay = 4 * time.Second
)

// Scanner scans a batch of mailboxes. The zero value is unusable: at minimum set Store (store.Null() without a database).
type Scanner struct {
	Store     store.Store
	Notifier  notify.Notifier
	Log       *slog.Logger
	Keywords  []string      // fallback keyword list when unset
	MaxEmails int           // mails per mailbox; zero means unset, use defaultMaxEmails
	Timeout   time.Duration // connect and login timeout; never caps a whole scan

	// OnNotify fires once per notification, for metrics. Optional.
	OnNotify func(kind string, ok bool)

	// The two below exist so tests can swap implementations; production uses the defaults.
	dial      dialFunc
	retryWait time.Duration
}

// scanState holds what one scan shares across mailboxes.
type scanState struct {
	matcher *matcher
	seen    seenSet
	days    int
	dryRun  bool
}

// Scan scans the given mailboxes, returning per-mailbox stats and alert mails.
//
// A single mailbox failure affects only its own MailboxResult; dry-run runs
// everything but neither dedupes nor notifies — the page's "try a scan"
// button relies on that.
func (s *Scanner) Scan(ctx context.Context, mailboxes []model.Mailbox, days int, dryRun bool) model.ScanResult {
	result := model.ScanResult{
		Days:      days,
		DryRun:    dryRun,
		Mailboxes: []model.MailboxResult{},
		Alerts:    []model.EmailAlert{},
	}
	if len(mailboxes) == 0 {
		s.log().Error("No mailboxes are configured, or all of them are disabled")
		return result
	}

	s.log().Info("Starting mailbox scan", "mailboxes", len(mailboxes), "days", days, "dry_run", dryRun)
	state := &scanState{matcher: newMatcher(s.keywords()), days: days, dryRun: dryRun}

	// results are written back in input order so the page's mailbox order never shuffles between scans
	results := make([]model.MailboxResult, len(mailboxes))
	alerts := make([][]model.EmailAlert, len(mailboxes))
	workers := min(len(mailboxes), maxMailboxWorkers)

	jobs := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i], alerts[i] = s.scanMailbox(ctx, mailboxes[i], state)
			}
		}()
	}
	for i := range mailboxes {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	result.Mailboxes = results
	for _, list := range alerts {
		result.Alerts = append(result.Alerts, list...)
	}
	s.logSummary(result)
	return result
}

// scanMailbox scans one mailbox. Any failure lands in MailboxResult.Error and is
// never thrown: mails and alerts already found are kept — a partial result beats
// losing it all.
func (s *Scanner) scanMailbox(ctx context.Context, m model.Mailbox, state *scanState) (model.MailboxResult, []model.EmailAlert) {
	name := displayName(m)
	out := model.MailboxResult{
		Name:     name,
		Host:     m.Host,
		Port:     mailboxPort(m),
		Username: m.Username,
		Success:  true,
	}
	if m.Host == "" || m.Username == "" || m.Password == "" {
		s.log().Warn("Incomplete mailbox configuration; skipping", "mailbox", name)
		out.Success = false
		out.Error = model.Ptr("Incomplete configuration; host, username, and password are required")
		return out, nil
	}

	total, alerts, err := s.scanInbox(ctx, m, name, state)
	out.TotalEmails = total
	out.AlertCount = len(alerts)
	if err != nil {
		out.Success = false
		out.Error = model.Ptr(err.Error())
		s.log().Error("Failed to scan mailbox", "mailbox", name, "error", err)
		if !state.dryRun {
			s.sendMailboxError(ctx, name, m.Host, err.Error())
		}
	}
	return out, alerts
}

// scanInbox connects and inspects mails from the last days in batches. On error
// the counts and alerts so far are returned so the caller can keep the partial
// result.
func (s *Scanner) scanInbox(ctx context.Context, m model.Mailbox, name string, state *scanState) (int, []model.EmailAlert, error) {
	conn, err := s.connect(ctx, m)
	if err != nil {
		return 0, nil, err
	}
	defer func() {
		if err := conn.Close(); err != nil {
			s.log().Warn("Error while disconnecting from the mailbox", "mailbox", name, "error", err)
		}
	}()

	// step back days in the local timezone; IMAP SINCE compares dates only
	since := time.Now().Add(-time.Duration(state.days) * 24 * time.Hour)
	nums, err := conn.Search(since)
	if err != nil {
		return 0, nil, err
	}
	if len(nums) == 0 {
		s.log().Info("No messages require checking", "mailbox", name)
		return 0, nil, nil
	}

	if limit := s.maxEmails(); len(nums) > limit {
		s.log().Warn("Message count exceeds limit; scanning newest only", "mailbox", name, "found", len(nums), "limit", limit)
		nums = nums[len(nums)-limit:]
	}
	total := len(nums)
	s.log().Info("Messages found", "mailbox", name, "count", total)

	var alerts []model.EmailAlert
	for start := 0; start < total; start += batchSize {
		// stop fetching when the page cancels the request; the rest waits for the next round
		if err := ctx.Err(); err != nil {
			return total, alerts, err
		}
		end := min(start+batchSize, total)
		for _, raw := range s.fetchBatch(conn, nums[start:end], name) {
			if alert, ok := s.inspect(ctx, raw, name, state); ok {
				alerts = append(alerts, alert)
			}
		}
		s.log().Info("Scan progress", "mailbox", name, "scanned", end, "total", total)
	}

	s.log().Info("Mailbox scan summary", "mailbox", name, "total_emails", total, "alert_count", len(alerts))
	return total, alerts, nil
}

// fetchBatch fetches a batch in bulk, degrading to per-mail fetches when the whole
// batch fails — one malformed mail must not cost the other 99.
func (s *Scanner) fetchBatch(conn mailConn, nums []uint32, name string) [][]byte {
	raws, err := conn.Fetch(nums)
	if err == nil {
		return raws
	}
	s.log().Warn("Batch fetch failed; fetching messages one at a time", "mailbox", name, "error", err)

	raws = make([][]byte, 0, len(nums))
	for _, num := range nums {
		one, err := conn.Fetch([]uint32{num})
		if err != nil {
			s.log().Warn("Failed to fetch message", "mailbox", name, "seq", num, "error", err)
			continue
		}
		raws = append(raws, one...)
	}
	return raws
}

// inspect decides whether a mail is an alert; if so it (optionally) notifies and returns the details.
func (s *Scanner) inspect(ctx context.Context, raw []byte, mailbox string, state *scanState) (model.EmailAlert, bool) {
	msg, err := parseMessage(raw)
	if err != nil {
		// mails that fail to parse are not dropped; scan the raw text and just log a note
		s.log().Warn("Malformed message; scanning the raw text", "mailbox", mailbox, "error", err)
	}
	if !state.seen.mark(msg.ID) {
		return model.EmailAlert{}, false
	}

	keywords := state.matcher.match(msg.Subject, msg.Body)
	if len(keywords) == 0 {
		return model.EmailAlert{}, false
	}

	service, amount := extractServiceInfo(msg.Subject, msg.Body)
	alert := model.EmailAlert{
		Mailbox:     mailbox,
		Subject:     msg.Subject,
		Sender:      msg.Sender,
		Date:        msg.Date,
		Keywords:    keywords,
		ServiceName: &service,
		Amount:      amount,
	}

	attrs := []any{
		"mailbox", mailbox, "sender", msg.Sender, "subject", msg.Subject,
		"date", msg.Date, "keywords", strings.Join(keywords, ", "), "service", service,
	}
	if amount != nil {
		attrs = append(attrs, "amount", *amount)
	}
	s.log().Warn("Alert email found", attrs...)

	switch {
	case state.dryRun:
		s.log().Info("Dry run; skipping alert delivery", "mailbox", mailbox, "subject", msg.Subject)
	case s.duplicated(ctx, alert, state.days):
		// flag it instead of skipping silently: the dashboard must distinguish "already notified" from "no match"
		alert.Duplicate = true
		s.log().Info("Email alert was already sent; skipping the duplicate", "mailbox", mailbox, "subject", msg.Subject)
	default:
		alert.AlertSent = s.send(ctx, alert)
	}
	return alert, true
}

// duplicated asks the database whether this mail notified recently. On lookup
// failure treat it as "not notified": better a duplicate alert than a missed
// billing warning because the database hiccuped.
func (s *Scanner) duplicated(ctx context.Context, alert model.EmailAlert, days int) bool {
	recent, err := s.Store.HasRecentEmailAlert(ctx, alert.Mailbox, alert.Sender, alert.Subject, alert.Date, max(days, 1))
	if err != nil {
		s.log().Warn("Failed to check for a duplicate email alert; treating it as not yet notified", "mailbox", alert.Mailbox, "error", err)
		return false
	}
	return recent
}

// send delivers a mail alert and records it. The record carries the send result:
// failed sends are recorded too, so next time we know this mail was handled and
// what happened.
func (s *Scanner) send(ctx context.Context, alert model.EmailAlert) bool {
	if s.Notifier == nil {
		s.log().Error("Webhook URL is not configured")
		return false
	}
	if s.suppressed(ctx, alert) {
		s.log().Info("Email alert suppressed as false positive", "mailbox", alert.Mailbox, "sender", alert.Sender)
		s.record(ctx, alert, false)
		return false
	}

	msg := notify.EmailAlert(alert.Mailbox, alert.Subject, alert.Sender, alert.Date,
		alert.Keywords, alert.ServiceName, alert.Amount)
	err := s.Notifier.Send(ctx, msg)
	if s.OnNotify != nil {
		s.OnNotify(msg.Kind, err == nil)
	}
	if err != nil {
		s.log().Error("Failed to send email alert", "mailbox", alert.Mailbox, "subject", alert.Subject, "error", err)
	}

	sent := err == nil
	s.record(ctx, alert, sent)
	return sent
}

// suppressed reports whether this mailbox+sender pair was muted as a false positive.
func (s *Scanner) suppressed(ctx context.Context, alert model.EmailAlert) bool {
	if s.Store == nil {
		return false
	}
	list, err := s.Store.ListEmailSuppressions(ctx)
	if err != nil {
		s.log().Warn("Failed to load email suppressions", "error", err)
		return false
	}
	for _, item := range list {
		if item.Mailbox == alert.Mailbox && item.Sender == alert.Sender {
			return true
		}
	}
	return false
}

// record persists an email alert row even when nothing was sent, so the history shows
// the full story.
func (s *Scanner) record(ctx context.Context, alert model.EmailAlert, sent bool) {
	if err := s.Store.SaveEmailAlert(ctx, store.EmailAlertRecord{
		Mailbox: alert.Mailbox, Sender: alert.Sender, Subject: alert.Subject, Date: alert.Date,
		ServiceName: alert.ServiceName, Amount: alert.Amount,
		Keywords: alert.Keywords, AlertSent: sent,
	}); err != nil {
		s.log().Warn("Failed to record email alert", "mailbox", alert.Mailbox, "error", err)
	}
}

// sendMailboxError pushes a system alert when a mailbox is unreachable — nobody watching the inbox is itself an incident.
func (s *Scanner) sendMailboxError(ctx context.Context, mailbox, host, reason string) {
	if s.Notifier == nil {
		return
	}
	msg := notify.MailboxError(mailbox, host, reason)
	err := s.Notifier.Send(ctx, msg)
	if s.OnNotify != nil {
		s.OnNotify(msg.Kind, err == nil)
	}
	if err != nil {
		s.log().Error("Failed to send the mailbox failure alert", "mailbox", mailbox, "error", err)
	}
}

// connect dials with retries per connectAttempts / connectRetryDelay.
func (s *Scanner) connect(ctx context.Context, m model.Mailbox) (mailConn, error) {
	dial := s.dial
	if dial == nil {
		dial = dialIMAP
	}
	wait := s.retryWait
	if wait <= 0 {
		wait = connectRetryDelay
	}

	for attempt := 1; ; attempt++ {
		conn, err := dial(ctx, m, s.Timeout)
		if err == nil {
			s.log().Info("Mailbox connected", "mailbox", displayName(m), "host", m.Host)
			return conn, nil
		}
		if attempt >= connectAttempts {
			return nil, err
		}
		s.log().Warn("Mailbox connection failed; retrying later", "mailbox", displayName(m), "attempt", attempt, "error", err)
		if waitErr := sleep(ctx, wait); waitErr != nil {
			return nil, err
		}
	}
}

// seenSet remembers mails already handled in this scan. Concurrent mailboxes share it, so lock.
type seenSet struct {
	mu  sync.Mutex
	ids map[string]struct{}
}

// mark returns true the first time a mail appears, false afterwards.
func (s *seenSet) mark(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.ids[id]; ok {
		return false
	}
	if s.ids == nil {
		s.ids = make(map[string]struct{})
	}
	s.ids[id] = struct{}{}
	return true
}

// keywords falls back to the default list when unset; configuring none would otherwise disable scanning entirely.
func (s *Scanner) keywords() []string {
	if len(s.Keywords) > 0 {
		return s.Keywords
	}
	return DefaultAlertKeywords
}

// maxEmails defaults when unset; a negative setting counts as 1 to avoid a negative fetch range.
func (s *Scanner) maxEmails() int {
	if s.MaxEmails == 0 {
		return defaultMaxEmails
	}
	return max(s.MaxEmails, 1)
}

func (s *Scanner) logSummary(result model.ScanResult) {
	total, sent := 0, 0
	for _, m := range result.Mailboxes {
		total += m.TotalEmails
	}
	for _, a := range result.Alerts {
		if a.AlertSent {
			sent++
		}
	}
	s.log().Info("Mailbox scan summary",
		"mailboxes", len(result.Mailboxes), "total_emails", total,
		"total_alerts", len(result.Alerts), "alerts_sent", sent)
}

func (s *Scanner) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// displayName picks the mailbox's display name for logs and alerts: fall back to the account when no name is set.
func displayName(m model.Mailbox) string {
	if m.Name != "" {
		return m.Name
	}
	if m.Username != "" {
		return m.Username
	}
	return "(Unnamed)"
}

// sleep waits, returning immediately if ctx is cancelled; a scan round must not stall in retries.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
