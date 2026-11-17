// Package notify delivers a message to wherever a person will see it.
//
// A Message says what happened in a form that belongs to no chat app. Each
// driver turns it into its platform's dialect — a Feishu card, a Telegram
// message, a plain webhook — and reports honestly whether the platform took
// it. "Honestly" matters: several of these platforms answer HTTP 200 with an
// error in the body, and a notifier that trusts the status code is silent
// exactly when it is needed.
//
// The package only sends. Whether to send, when, and what to do about a
// failure is decided by the caller, which owns the outbox.
package notify

import (
	"html"
	"strings"
	"time"

	"quotapulse/internal/core"
)

// Message is one notification.
type Message struct {
	Event    string        `json:"event"`
	Severity core.Severity `json:"severity"`
	Topic    string        `json:"topic"`
	Title    string        `json:"title"`
	// Body is a sentence or two of plain text.
	Body string `json:"body,omitempty"`
	// Facts are the numbers behind the sentence.
	Facts []Fact `json:"facts,omitempty"`
	// Sections structure a longer message such as the digest.
	Sections []Section `json:"sections,omitempty"`
	// Link opens the dashboard at the right place; LinkLabel names it. Path
	// is the same place without the host, for a notification that opens
	// inside the dashboard's own origin.
	Link      string    `json:"link,omitempty"`
	LinkLabel string    `json:"link_label,omitempty"`
	Path      string    `json:"path,omitempty"`
	Footer    string    `json:"footer,omitempty"`
	At        time.Time `json:"at"`
	// Subject gives a machine on the other end something to key on.
	Subject *Subject `json:"subject,omitempty"`
}

// Fact is one labelled value.
type Fact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Section is a titled group of lines.
type Section struct {
	Title string   `json:"title"`
	Lines []string `json:"lines"`
}

// Subject identifies what a message is about, for the generic webhook.
type Subject struct {
	IncidentID string `json:"incident_id,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Type       string `json:"type,omitempty"`
	ID         string `json:"id,omitempty"`
	Name       string `json:"name,omitempty"`
}

// Resolved reports whether the message is good news.
func (m Message) Resolved() bool { return m.Event == core.EventResolved }

// Markdown renders the body for platforms that take Markdown. The title is
// left out: each platform has its own place for it.
func (m Message) Markdown() string {
	var b strings.Builder
	if m.Body != "" {
		b.WriteString(m.Body)
		b.WriteString("\n")
	}
	if len(m.Facts) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		for _, f := range m.Facts {
			b.WriteString("**" + f.Label + "** " + f.Value + "\n")
		}
	}
	for _, s := range m.Sections {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("**" + s.Title + "**\n")
		for _, line := range s.Lines {
			b.WriteString(line + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// Plain renders the body without markup.
func (m Message) Plain() string {
	var b strings.Builder
	if m.Body != "" {
		b.WriteString(m.Body)
		b.WriteString("\n")
	}
	if len(m.Facts) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		for _, f := range m.Facts {
			b.WriteString(f.Label + ": " + f.Value + "\n")
		}
	}
	for _, s := range m.Sections {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(s.Title + "\n")
		for _, line := range s.Lines {
			b.WriteString(stripMarkdown(line) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// HTML renders title and body in the small subset of HTML Telegram accepts.
func (m Message) HTML() string {
	var b strings.Builder
	b.WriteString("<b>" + html.EscapeString(m.Title) + "</b>\n")
	if m.Body != "" {
		b.WriteString(html.EscapeString(m.Body) + "\n")
	}
	if len(m.Facts) > 0 {
		b.WriteString("\n")
		for _, f := range m.Facts {
			b.WriteString("<b>" + html.EscapeString(f.Label) + "</b> " + html.EscapeString(f.Value) + "\n")
		}
	}
	for _, s := range m.Sections {
		b.WriteString("\n<b>" + html.EscapeString(s.Title) + "</b>\n")
		for _, line := range s.Lines {
			b.WriteString(html.EscapeString(stripMarkdown(line)) + "\n")
		}
	}
	if m.Link != "" {
		label := m.LinkLabel
		if label == "" {
			label = m.Link
		}
		b.WriteString("\n<a href=\"" + html.EscapeString(m.Link) + "\">" + html.EscapeString(label) + "</a>")
	}
	return strings.TrimRight(b.String(), "\n")
}

// stripMarkdown removes the bold markers digest lines carry.
func stripMarkdown(s string) string { return strings.ReplaceAll(s, "**", "") }
