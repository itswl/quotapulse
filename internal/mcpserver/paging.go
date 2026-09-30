package mcpserver

// Time ranges and cursors for the history tools. Like views.go, this file must not
// import internal packages.

import (
	"encoding/base64"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// historyReadLimit is the store's cap on one history read. Paging happens over what one
// read returns, so a range with more rows than this is cut at its oldest end.
const historyReadLimit = 1000

// timeRange is the window and paging input the history tools share.
type timeRange struct {
	Since  string `json:"since,omitempty" jsonschema:"only rows at or after this time: RFC 3339, or a YYYY-MM-DD date in the server timezone"`
	Until  string `json:"until,omitempty" jsonschema:"only rows before this time: RFC 3339, or a YYYY-MM-DD date, which includes that whole day"`
	Days   int    `json:"days,omitempty" jsonschema:"without since, how many days back to search, 1-365 (default 30)"`
	Cursor string `json:"cursor,omitempty" jsonschema:"next_cursor of the previous page, to continue there"`
}

// window is a parsed timeRange.
type window struct {
	since, until time.Time // zero when open
	days         int       // how many days back the store read reaches
	after        *position // the previous page's last row
}

// position is a row's place in a newest-first list: its time, then its ID.
type position struct {
	at time.Time
	id string
}

func (p position) before(q position) bool {
	if !p.at.Equal(q.at) {
		return p.at.After(q.at)
	}
	return p.id > q.id
}

// parse validates the range against now; dates are read in loc.
func (r timeRange) parse(now time.Time, loc *time.Location) (window, error) {
	w := window{days: bounded(r.Days, 30, 1, 365)}
	var err error
	if w.since, err = parseBound(r.Since, loc, false); err != nil {
		return w, fmt.Errorf("since %w", err)
	}
	if w.until, err = parseBound(r.Until, loc, true); err != nil {
		return w, fmt.Errorf("until %w", err)
	}
	if !w.since.IsZero() && !w.until.IsZero() && !w.since.Before(w.until) {
		return w, fmt.Errorf("since must be earlier than until")
	}
	if !w.since.IsZero() {
		// Reach back far enough to cover since, within the store's 365 days.
		w.days = min(365, max(1, int(math.Ceil(now.Sub(w.since).Hours()/24))+1))
	}
	if r.Cursor != "" {
		if w.after, err = decodeCursor(r.Cursor); err != nil {
			return w, err
		}
	}
	return w, nil
}

// parseBound reads an RFC 3339 time or a date; an end date means the end of that day.
func parseBound(raw string, loc *time.Location, end bool) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	if at, err := time.Parse(time.RFC3339, raw); err == nil {
		return at, nil
	}
	day, err := time.ParseInLocation("2006-01-02", raw, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("must be an RFC 3339 time or a YYYY-MM-DD date")
	}
	if end {
		day = day.AddDate(0, 0, 1)
	}
	return day, nil
}

func (w window) contains(at time.Time) bool {
	return (w.since.IsZero() || !at.Before(w.since)) && (w.until.IsZero() || at.Before(w.until))
}

func encodeCursor(p position) string {
	return base64.RawURLEncoding.EncodeToString([]byte(p.at.UTC().Format(time.RFC3339Nano) + "|" + p.id))
}

func decodeCursor(raw string) (*position, error) {
	invalid := fmt.Errorf("cursor is not a next_cursor this tool returned")
	body, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, invalid
	}
	at, id, found := strings.Cut(string(body), "|")
	if !found {
		return nil, invalid
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return nil, invalid
	}
	return &position{at: t, id: id}, nil
}

// entry is one history row with its place in the list.
type entry[T any] struct {
	position
	item T
}

func newEntry[T any](timestamp, id string, item T) entry[T] {
	at, _ := time.Parse(time.RFC3339Nano, timestamp)
	return entry[T]{position: position{at: at, id: id}, item: item}
}

// rowID makes numeric IDs sort as numbers inside a cursor.
func rowID(kind string, id int64) string { return fmt.Sprintf("%s:%020d", kind, id) }

// page is one page of a newest-first history list.
type page[T any] struct {
	items []T
	next  string // cursor for the next page; "" on the last
	// truncated is set on the last page when a read hit historyReadLimit: older rows
	// in the range may exist but can't be reached.
	truncated bool
}

// paginate keeps the entries inside w, continues after its cursor, and cuts one page of
// limit entries, newest first. capped says a read returned historyReadLimit rows.
func paginate[T any](entries []entry[T], w window, limit int, capped bool) page[T] {
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].before(entries[j].position) })
	p := page[T]{items: []T{}}
	var last position
	for _, e := range entries {
		if !w.contains(e.at) || (w.after != nil && !w.after.before(e.position)) {
			continue
		}
		if len(p.items) == limit {
			p.next = encodeCursor(last)
			return p
		}
		p.items = append(p.items, e.item)
		last = e.position
	}
	p.truncated = capped
	return p
}
