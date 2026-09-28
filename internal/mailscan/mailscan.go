// Package mailscan provides the package implementation.
//
// Implementation note.
// Implementation note.
//
// Implementation note.
// Implementation note.
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

// Implementation note.
const (
	maxMailboxWorkers = 5    // operation
	batchSize         = 100  // operation IMAP FETCH operation
	defaultMaxEmails  = 1000 // MaxEmails operation,operation MAX_EMAILS_TO_SCAN operation

	// Implementation note.
	// Implementation note.
	connectAttempts   = 3
	connectRetryDelay = 4 * time.Second
)

// Implementation note.
type Scanner struct {
	Store     store.Store
	Notifier  notify.Notifier
	Log       *slog.Logger
	Keywords  []string      // operation DefaultAlertKeywords
	MaxEmails int           // operation,operation,operation defaultMaxEmails
	Timeout   time.Duration // operation,operation

	// Implementation note.
	OnNotify func(kind string, ok bool)

	// Implementation note.
	dial      dialFunc
	retryWait time.Duration
}

// Implementation note.
type scanState struct {
	matcher *matcher
	seen    seenSet
	days    int
	dryRun  bool
}

// Implementation note.
//
// Implementation note.
// Implementation note.
func (s *Scanner) Scan(ctx context.Context, mailboxes []model.Mailbox, days int, dryRun bool) model.ScanResult {
	result := model.ScanResult{
		Days:      days,
		DryRun:    dryRun,
		Mailboxes: []model.MailboxResult{},
		Alerts:    []model.EmailAlert{},
	}
	if len(mailboxes) == 0 {
		s.log().Error("operation")
		return result
	}

	s.log().Info("Starting mailbox scan", "mailboxes", len(mailboxes), "days", days, "dry_run", dryRun)
	state := &scanState{matcher: newMatcher(s.keywords()), days: days, dryRun: dryRun}

	// Implementation note.
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

// Implementation note.
// Implementation note.
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
		s.log().Error("operation", "mailbox", name, "error", err)
		if !state.dryRun {
			s.sendMailboxError(ctx, name, m.Host, err.Error())
		}
	}
	return out, alerts
}

// Implementation note.
// Implementation note.
func (s *Scanner) scanInbox(ctx context.Context, m model.Mailbox, name string, state *scanState) (int, []model.EmailAlert, error) {
	conn, err := s.connect(ctx, m)
	if err != nil {
		return 0, nil, err
	}
	defer func() {
		if err := conn.Close(); err != nil {
			s.log().Warn("operation", "mailbox", name, "error", err)
		}
	}()

	// Implementation note.
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
		// Implementation note.
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

// Implementation note.
// Implementation note.
func (s *Scanner) fetchBatch(conn mailConn, nums []uint32, name string) [][]byte {
	raws, err := conn.Fetch(nums)
	if err == nil {
		return raws
	}
	s.log().Warn("operation,operation", "mailbox", name, "error", err)

	raws = make([][]byte, 0, len(nums))
	for _, num := range nums {
		one, err := conn.Fetch([]uint32{num})
		if err != nil {
			s.log().Warn("operation", "mailbox", name, "seq", num, "error", err)
			continue
		}
		raws = append(raws, one...)
	}
	return raws
}

// Implementation note.
func (s *Scanner) inspect(ctx context.Context, raw []byte, mailbox string, state *scanState) (model.EmailAlert, bool) {
	msg, err := parseMessage(raw)
	if err != nil {
		// Implementation note.
		s.log().Warn("operation,operation", "mailbox", mailbox, "error", err)
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
		// Implementation note.
		alert.Duplicate = true
		s.log().Info("Email alertoperation,operation", "mailbox", mailbox, "subject", msg.Subject)
	default:
		alert.AlertSent = s.send(ctx, alert)
	}
	return alert, true
}

// Implementation note.
// Implementation note.
func (s *Scanner) duplicated(ctx context.Context, alert model.EmailAlert, days int) bool {
	recent, err := s.Store.HasRecentEmailAlert(ctx, alert.Mailbox, alert.Sender, alert.Subject, alert.Date, max(days, 1))
	if err != nil {
		s.log().Warn("operationEmail alertoperation,operation", "mailbox", alert.Mailbox, "error", err)
		return false
	}
	return recent
}

// Implementation note.
// Implementation note.
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

// Implementation note.
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
		s.log().Error("operation", "mailbox", mailbox, "error", err)
	}
}

// Implementation note.
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

// Implementation note.
type seenSet struct {
	mu  sync.Mutex
	ids map[string]struct{}
}

// Implementation note.
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

// Implementation note.
func (s *Scanner) keywords() []string {
	if len(s.Keywords) > 0 {
		return s.Keywords
	}
	return DefaultAlertKeywords
}

// Implementation note.
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
	s.log().Info("operation",
		"mailboxes", len(result.Mailboxes), "total_emails", total,
		"total_alerts", len(result.Alerts), "alerts_sent", sent)
}

func (s *Scanner) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// Implementation note.
func displayName(m model.Mailbox) string {
	if m.Name != "" {
		return m.Name
	}
	if m.Username != "" {
		return m.Username
	}
	return "(Unnamed)"
}

// Implementation note.
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
