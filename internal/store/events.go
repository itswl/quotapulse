package store

import "sort"

// EventRow unifies alert-history and email-alert rows into one queryable timeline.
type EventRow struct {
	Type    string `json:"type"`
	Source  string `json:"source,omitempty"`
	Status  string `json:"status,omitempty"`
	Message string `json:"message"`
	Time    string `json:"time"`
}

// MergeEvents interleaves alert-history and email-alert rows, newest first, trimmed to
// limit. Pure read-model: both feeds already exist, this only normalizes them.
func MergeEvents(alerts []AlertRow, emails []EmailAlertRow, limit int) []EventRow {
	events := make([]EventRow, 0, len(alerts)+len(emails))
	for _, row := range alerts {
		source := row.ProjectName
		if source == "" {
			source = row.ProjectID
		}
		events = append(events, EventRow{Type: row.AlertType, Source: source, Status: row.Status, Message: row.Message, Time: row.Timestamp})
	}
	for _, row := range emails {
		events = append(events, EventRow{Type: "email_alert", Source: row.Mailbox, Message: row.Subject, Time: row.Timestamp})
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Time > events[j].Time })
	if limit > 0 && len(events) > limit {
		events = events[:limit]
	}
	return events
}
