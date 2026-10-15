// Package report builds the weekly report: one week of balances, spend,
// runway, subscriptions and mailboxes summarized into a single card.
//
// Normally notifications only arrive when something breaks; the weekly
// report makes "all is well" perceivable too: what this week burned, which
// account runs out first, how much subscription spend the next month needs.
// All data comes from dashboard state and balance history — no extra
// upstream calls.
package report

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/itswl/quotapulse/internal/model"
)

const (
	// WindowDays is the weekly report's reporting span.
	WindowDays = 7
	// TopN is how many entries each chart takes.
	TopN = 3
	// UpcomingDays is the horizon for subscription spend estimates.
	UpcomingDays = 30
)

// SpendRow is one row of the spend chart.
type SpendRow struct {
	Project       string   `json:"project"`
	Consumed      float64  `json:"consumed"`
	BurnPerDay    *float64 `json:"burn_per_day"`
	RunwayDays    *float64 `json:"runway_days"`
	DepletionDate *string  `json:"depletion_date"`
	Balance       *float64 `json:"balance"`
}

// AlertingRow is one row of the balance alert list.
type AlertingRow struct {
	Project   string   `json:"project"`
	Balance   *float64 `json:"balance"`
	Threshold *float64 `json:"threshold"`
}

// FailedRow is one row of the failed-checks list.
type FailedRow struct {
	Project string `json:"project"`
	Error   string `json:"error"`
}

// Accounts is the account overview.
type Accounts struct {
	Total    int `json:"total"`
	Alerting int `json:"alerting"`
	Failed   int `json:"failed"`
}

// Period is the reporting window.
type Period struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// MailboxStats is the mailbox scan overview.
type MailboxStats struct {
	Total  int `json:"total"`
	Failed int `json:"failed"`
	Alerts int `json:"alerts"`
}

// Summary is one week's data, the intermediate structure before render.
type Summary struct {
	Period                Period                     `json:"period"`
	Accounts              Accounts                   `json:"accounts"`
	TotalConsumed         *float64                   `json:"total_consumed"`
	TopSpend              []SpendRow                 `json:"top_spend"`
	ShortestRunway        []SpendRow                 `json:"shortest_runway"`
	AlertingProjects      []AlertingRow              `json:"alerting_projects"`
	FailedProjects        []FailedRow                `json:"failed_projects"`
	UpcomingSubscriptions []model.SubscriptionResult `json:"upcoming_subscriptions"`
	UpcomingAmount        float64                    `json:"upcoming_amount"`
	Mailboxes             MailboxStats               `json:"mailboxes"`
}

// Build aggregates the week. Empty runways mean no history, in which case only the balance part reports.
func Build(results []model.CheckResult, subs []model.SubscriptionResult,
	mailboxes []model.MailboxResult, totalAlerts int, runways map[string]model.Runway, now time.Time) Summary {

	var succeeded []model.CheckResult
	var failed []model.CheckResult
	for _, r := range results {
		if r.Success {
			succeeded = append(succeeded, r)
		} else {
			failed = append(failed, r)
		}
	}

	spend := spendRows(succeeded, runways)
	upcoming := upcomingSubscriptions(subs)

	summary := Summary{
		Period: Period{
			Start: now.AddDate(0, 0, -WindowDays).Format("2006-01-02"),
			End:   now.Format("2006-01-02"),
		},
		Accounts: Accounts{
			Total:    len(results),
			Alerting: countAlerting(succeeded),
			Failed:   len(failed),
		},
		TopSpend:              topByConsumed(spend),
		ShortestRunway:        topByRunway(spend),
		AlertingProjects:      alertingRows(succeeded),
		FailedProjects:        failedRows(failed),
		UpcomingSubscriptions: upcoming,
		Mailboxes:             mailboxStats(mailboxes, totalAlerts),
	}

	if len(spend) > 0 {
		total := 0.0
		for _, row := range spend {
			total += row.Consumed
		}
		summary.TotalConsumed = model.Ptr(round2(total))
	}
	for _, sub := range upcoming {
		summary.UpcomingAmount += sub.Amount
	}
	summary.UpcomingAmount = round2(summary.UpcomingAmount)
	return summary
}

// spendRows turns spend profiles into chart rows; accounts without history stay off the chart.
func spendRows(results []model.CheckResult, runways map[string]model.Runway) []SpendRow {
	var rows []SpendRow
	for _, r := range results {
		profile, ok := runways[model.ProjectID(r.Provider, r.Project)]
		if !ok {
			continue
		}
		rows = append(rows, SpendRow{
			Project:       r.Project,
			Consumed:      profile.Consumed,
			BurnPerDay:    profile.BurnPerDay,
			RunwayDays:    profile.RunwayDays,
			DepletionDate: profile.DepletionDate,
			Balance:       profile.CurrentBalance,
		})
	}
	return rows
}

// upcomingSubscriptions lists subscriptions renewing within a month that were not renewed this cycle, sorted by urgency.
func upcomingSubscriptions(subs []model.SubscriptionResult) []model.SubscriptionResult {
	var due []model.SubscriptionResult
	for _, s := range subs {
		if !s.AlreadyRenewed && s.DaysUntilRenewal >= 0 && s.DaysUntilRenewal <= UpcomingDays {
			due = append(due, s)
		}
	}
	sort.SliceStable(due, func(i, j int) bool { return due[i].DaysUntilRenewal < due[j].DaysUntilRenewal })
	return due
}

func topByConsumed(rows []SpendRow) []SpendRow {
	sorted := append([]SpendRow(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Consumed > sorted[j].Consumed })
	return head(sorted)
}

func topByRunway(rows []SpendRow) []SpendRow {
	var withRunway []SpendRow
	for _, row := range rows {
		if row.RunwayDays != nil {
			withRunway = append(withRunway, row)
		}
	}
	sort.SliceStable(withRunway, func(i, j int) bool { return *withRunway[i].RunwayDays < *withRunway[j].RunwayDays })
	return head(withRunway)
}

func head(rows []SpendRow) []SpendRow {
	if len(rows) > TopN {
		return rows[:TopN]
	}
	return rows
}

func countAlerting(results []model.CheckResult) int {
	count := 0
	for _, r := range results {
		if r.NeedAlarm {
			count++
		}
	}
	return count
}

func alertingRows(results []model.CheckResult) []AlertingRow {
	var rows []AlertingRow
	for _, r := range results {
		if r.NeedAlarm {
			rows = append(rows, AlertingRow{Project: r.Project, Balance: r.Credits, Threshold: r.Threshold})
		}
	}
	return rows
}

func failedRows(results []model.CheckResult) []FailedRow {
	var rows []FailedRow
	for _, r := range results {
		message := ""
		if r.Error != nil {
			message = *r.Error
		}
		rows = append(rows, FailedRow{Project: r.Project, Error: message})
	}
	return rows
}

func mailboxStats(mailboxes []model.MailboxResult, totalAlerts int) MailboxStats {
	stats := MailboxStats{Total: len(mailboxes), Alerts: totalAlerts}
	for _, m := range mailboxes {
		if m.Error != nil {
			stats.Failed++
		}
	}
	return stats
}

// Render produces the Markdown body; per-platform message wrapping belongs to notify.
func Render(s Summary) string {
	lines := headline(s)
	lines = append(lines, section("Shortest runway", runwayLines(s.ShortestRunway))...)
	lines = append(lines, section("Highest spending", spendLines(s.TopSpend))...)
	lines = append(lines, section(
		fmt.Sprintf("Next %d days of subscription spending: %s", UpcomingDays, fmtNum(&s.UpcomingAmount)),
		subscriptionLines(s.UpcomingSubscriptions))...)
	lines = append(lines, section("Needs attention", problemLines(s))...)
	return strings.Join(lines, "\n")
}

func headline(s Summary) []string {
	state := ", all healthy"
	if s.Accounts.Alerting > 0 {
		state = ", " + model.Quantity(s.Accounts.Alerting, "balance alert", "balance alerts")
	}
	if s.Accounts.Failed > 0 {
		state += ", " + model.Quantity(s.Accounts.Failed, "failed check", "failed checks")
	}
	lines := []string{
		fmt.Sprintf("**Reporting period**: %s ~ %s", s.Period.Start, s.Period.End),
		fmt.Sprintf("**Accounts**: %d%s", s.Accounts.Total, state),
	}
	if s.TotalConsumed != nil {
		lines = append(lines, "**This week's spending**: "+fmtNum(s.TotalConsumed))
	}
	return lines
}

// section omits empty sections entirely so the card is not a pile of blank titles.
func section(title string, rows []string) []string {
	if len(rows) == 0 {
		return nil
	}
	return append([]string{"", "**" + title + "**"}, rows...)
}

func runwayLines(rows []SpendRow) []string {
	var out []string
	for _, row := range rows {
		line := fmt.Sprintf("- %s: %.1f days remaining", row.Project, *row.RunwayDays)
		if row.DepletionDate != nil {
			line += fmt.Sprintf(" (estimated depletion: %s)", *row.DepletionDate)
		}
		out = append(out, line+", balance "+fmtNum(row.Balance))
	}
	return out
}

func spendLines(rows []SpendRow) []string {
	var out []string
	for _, row := range rows {
		out = append(out, fmt.Sprintf("- %s: %s, daily average %s",
			row.Project, fmtNum(&row.Consumed), fmtNum(row.BurnPerDay)))
	}
	return out
}

func subscriptionLines(subs []model.SubscriptionResult) []string {
	var out []string
	for _, sub := range subs {
		amount := sub.Amount
		due := "reminder today"
		if sub.DaysUntilRenewal > 0 {
			due = "reminder in " + model.Quantity(sub.DaysUntilRenewal, "day", "days")
		}
		out = append(out, fmt.Sprintf("- %s: %s, %s", sub.Name, due, fmtNum(&amount)))
	}
	return out
}

func problemLines(s Summary) []string {
	var out []string
	for _, item := range s.AlertingProjects {
		out = append(out, fmt.Sprintf("- %s: balance %s below threshold %s",
			item.Project, fmtNum(item.Balance), fmtNum(item.Threshold)))
	}
	for _, item := range s.FailedProjects {
		out = append(out, fmt.Sprintf("- %s: Check failed, %s", item.Project, item.Error))
	}
	if s.Mailboxes.Failed > 0 {
		out = append(out, "- Mailboxes: "+model.Quantity(s.Mailboxes.Failed, "connection failure", "connection failures"))
	}
	return out
}

// fmtNum formats card numbers: empty shows a dash, the rest get thousands separators and two decimals.
func fmtNum(value *float64) string {
	if value == nil {
		return "-"
	}
	return thousands(*value, 2)
}

func thousands(value float64, decimals int) string {
	text := fmt.Sprintf("%.*f", decimals, value)
	sign := ""
	if strings.HasPrefix(text, "-") {
		sign, text = "-", text[1:]
	}
	intPart, frac, hasFrac := strings.Cut(text, ".")

	var grouped strings.Builder
	for i, digit := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(digit)
	}
	if !hasFrac {
		return sign + grouped.String()
	}
	return sign + grouped.String() + "." + frac
}

// round2 uses banker's rounding to match runway's convention: the report's totals
// must agree with the dashboard and history — different rounding on each side
// shows up as off-by-a-cent.
func round2(value float64) float64 {
	return math.RoundToEven(value*100) / 100
}
