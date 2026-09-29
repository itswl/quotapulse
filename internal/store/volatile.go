package store

import (
	"context"
	"sync"
	"time"
)

// volatileRetention bounds what Volatile remembers. It outlasts the default cooldown and
// the longest email scan range (30 days) that de-duplication has to cover.
const volatileRetention = 31 * 24 * time.Hour

// Volatile returns the store used when the database is off. Like Null it keeps no
// configuration or history, but it remembers in memory which alerts went out, so alert
// cooldowns and email de-duplication still hold between checks. The memory does not
// survive a restart.
func Volatile() Store {
	return &volatileStore{sent: map[alertKey]time.Time{}, emails: map[emailKey]time.Time{}}
}

type alertKey struct{ id, alertType string }

type emailKey struct{ mailbox, sender, subject, date string }

type volatileStore struct {
	nullStore

	mu     sync.Mutex
	now    func() time.Time // nil means time.Now; tests move the clock
	sent   map[alertKey]time.Time
	emails map[emailKey]time.Time
}

// SaveAlert remembers successful sends only: like the database, a failed send must not
// start a cooldown, or a broken channel would silence the retry.
func (v *volatileStore) SaveAlert(_ context.Context, rec AlertRecord) error {
	if rec.Status != "" && rec.Status != "sent" {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	now := v.clock()
	v.sent[alertKey{rec.AlertID, rec.AlertType}] = now
	v.prune(now)
	return nil
}

func (v *volatileStore) LastSentAlert(_ context.Context, alertID, alertType string, within time.Duration) (*time.Time, error) {
	if within <= 0 {
		return nil, nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	at, ok := v.sent[alertKey{alertID, alertType}]
	if !ok || v.clock().Sub(at) > within {
		return nil, nil
	}
	return &at, nil
}

func (v *volatileStore) HasRecentAlert(ctx context.Context, alertID, alertType string, within time.Duration) (bool, error) {
	last, err := v.LastSentAlert(ctx, alertID, alertType, within)
	return last != nil, err
}

// SaveEmailAlert remembers emails whose notification went out, which is what
// HasRecentEmailAlert de-duplicates on.
func (v *volatileStore) SaveEmailAlert(_ context.Context, rec EmailAlertRecord) error {
	if !rec.AlertSent {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	now := v.clock()
	v.emails[emailKey{rec.Mailbox, rec.Sender, rec.Subject, rec.Date}] = now
	v.prune(now)
	return nil
}

func (v *volatileStore) HasRecentEmailAlert(_ context.Context, mailbox, sender, subject, date string, days int) (bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	at, ok := v.emails[emailKey{mailbox, sender, subject, date}]
	window := time.Duration(daysOf(days, defaultEmailDays)) * 24 * time.Hour
	return ok && v.clock().Sub(at) <= window, nil
}

func (v *volatileStore) clock() time.Time {
	if v.now != nil {
		return v.now()
	}
	return time.Now()
}

// prune drops entries older than volatileRetention. The caller holds mu.
func (v *volatileStore) prune(now time.Time) {
	for key, at := range v.sent {
		if now.Sub(at) > volatileRetention {
			delete(v.sent, key)
		}
	}
	for key, at := range v.emails {
		if now.Sub(at) > volatileRetention {
			delete(v.emails, key)
		}
	}
}
