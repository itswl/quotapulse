# QuotaPulse in pictures

QuotaPulse is a single self-hosted binary that watches the paid API accounts your work
depends on. It checks balances across providers, estimates how long each balance will
last, reminds you before subscriptions renew, and reads billing emails straight from
your mailbox — then tells you before things run dry, not after.

This page walks through the project using screenshots. Everything shown runs on demo
data; the interface ships in light and dark themes and follows your system preference
until you toggle it. For installation and configuration, see the
[README](../README.md); for the icon system, see [icons.md](icons.md).

## One dashboard for every account

The landing view answers the only question that matters at a glance: how long until
something runs out. The overview band leads with the shortest runway across all
accounts — here `volc-1` has 1.2 days left — followed by the total, healthy, and
alerting project counters. The time of the last successful check sits in the top
navigation, next to the theme toggle, refresh, and settings.

![Dashboard, light theme, grid view](images/dashboard-light.png)

Each project card shows the current balance in a large monospace figure, the alert
threshold as a colored progress bar, daily spend, and the runway estimate. Accounts
below threshold are tinted red with a pulsing dot; an account whose key stopped working
says so plainly — `aliyun-ops` shows "Unavailable" with the exact API error inline,
instead of pretending the balance is zero.

## Know before you run dry

Runway is the core idea. QuotaPulse stores balance snapshots, measures how fast each
account actually burns, and projects a depletion date — "at 15.30 per day, this account
is empty around 2026-09-22". Estimates that lack enough history say so honestly instead
of guessing. When an account crosses its threshold, the card turns red and an alert goes
out through your webhook once, with a cooldown so a bad night does not spam the channel.

![Dashboard, dark theme](images/dashboard-dark.png)

## Two ways to read the same data

The grid view is for scanning; the list view is for comparing. The same numbers stay
aligned in a compact table, and the toggle remembers your choice.

![List view](images/project-list.png)

## Never miss a renewal

Subscriptions are sorted by next renewal date, with the days-remaining figure escalating
from amber (14 days) to red (7). Renewed something early? Mark it and QuotaPulse pushes
the next reminder out; the mark is visible until you clear it. Weekly, monthly,
Gregorian-yearly, and lunar-yearly cycles are all supported.

![Subscription reminders](images/subscriptions.png)

## Let the mailbox do the bookkeeping

Point QuotaPulse at an IMAP mailbox and it picks out billing and renewal emails by
keyword: balance warnings, invoices, top-up confirmations. Summary chips show what a
scan found, each mailbox reports its own connection status, and matched emails are
listed with the keywords that flagged them and whether a notification went out.

![Email scanning](images/email-scanning.png)

## History you can see

With the optional database enabled, every check becomes a data point. "View trend" on a
card opens a 30-day balance chart with the alert threshold as a dashed line and a hover
tooltip. The chart draws with the theme's own colors and fonts, in both modes.

![Balance trend dialog, dark theme](images/trend-modal.png)

## Safe to operate

Destructive actions get an in-page confirmation with the focus on Cancel — no browser
popups, no accidental deletes. Forms validate inline, right next to the field. When the
backend cannot be reached, the dashboard says so and offers a retry instead of leaving
skeletons on screen.

![Delete confirmation](images/delete-confirm.png)

![Load failure with retry](images/load-error.png)

## Good from the first minute

A fresh install shows a getting-started panel that names the exact environment variable
to set, rather than an empty grid. Filters that match nothing offer a one-click reset.

![Empty state](images/empty-state.png)

## Light, dark, and pocket-sized

Dark mode is a first-class theme, applied before the first paint so it never flashes.
On a phone the counters collapse into rows, the view switcher scrolls horizontally, and
card actions stay visible.

![Mobile layout](images/mobile.png)

## Under the hood

All of this ships as one Go binary with the frontend embedded: no separate static
directory, no external font or icon CDNs — the typefaces (Geist) and the icon sprite
travel inside the binary. Balance history, runway estimates, and spending-spike
detection run from snapshots in an optional database. The same data is exposed as
Prometheus metrics, through a documented HTTP API, and via a read-only MCP endpoint for
AI assistants. See [ARCHITECTURE.md](ARCHITECTURE.md) and [API.md](API.md).
