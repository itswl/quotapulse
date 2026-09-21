# Icon system

QuotaPulse ships its own icon set: 21 hand-drawn symbols that share one grid, one stroke
weight, and one color rule. They replace the generic stock-icon look with shapes picked
to match what each control actually does — the theme toggle is a half-filled disc rather
than a sun, settings are sliders rather than a gear, subscriptions are a renewal cycle
rather than a calendar.

For how the interface looks in practice, see [screenshots.md](screenshots.md).

## Where the icons live

- **The single source of truth is an SVG sprite** at the top of `ui/index.html`: every
  icon is a `<symbol id="i-*">` inside a hidden `<svg class="icon-sprite">`.
- **Static markup** references a symbol with `<svg class="icon"><use href="#i-search"></use></svg>`.
- **Rendered markup** (project cards, subscriptions, toasts, …) gets the same references
  from the constants in `ui/src/ui/icons.ts`, so both paths emit identical output.
- **The favicon** is the brand mark inlined as a data URI in the `<head>`; keep it in
  sync with `i-logo` when the mark changes.

## Drawing rules

Every symbol follows the same constraints, which is what keeps the set coherent:

| Rule | Value |
| --- | --- |
| Canvas | 24 × 24 grid |
| Stroke | 1.75 px, round caps and round joins (set globally in `base.css`) |
| Color | inherited via `currentColor`; icons never hard-code a palette color |
| Fill | none, except deliberate accents (the theme disc, the info dot) |
| Sizing | set by the surrounding context; `.icon` defaults to 16 px |

Because stroke and color come from CSS, one drawing serves every context: a 15 px icon in
a button, a 44 px one in an empty state, and the tinted hover states on destructive
actions all reuse the same paths.

## The set

| Icon | Symbol | Used for |
| --- | --- | --- |
| ![logo](images/icons/logo.svg) | `i-logo` | Brand mark — a quota ring with the tail of a Q; navigation tile and favicon |
| ![theme](images/icons/theme.svg) | `i-theme` | Theme toggle; the filled half swaps sides by rotating 180° in dark mode |
| ![refresh](images/icons/refresh.svg) | `i-refresh` | Refresh balances now |
| ![sliders](images/icons/sliders.svg) | `i-sliders` | Settings dialog |
| ![card](images/icons/card.svg) | `i-card` | All-projects view |
| ![flag](images/icons/flag.svg) | `i-flag` | Alerts-only view |
| ![cycle](images/icons/cycle.svg) | `i-cycle` | Subscriptions view and its empty state — recurring renewals |
| ![at](images/icons/at.svg) | `i-at` | Email-scanning view and mailbox empty states |
| ![grid](images/icons/grid.svg) | `i-grid` | Grid layout toggle |
| ![rows](images/icons/rows.svg) | `i-rows` | List layout toggle |
| ![search](images/icons/search.svg) | `i-search` | Search field and the no-match empty state |
| ![plus](images/icons/plus.svg) | `i-plus` | Add project / add subscription |
| ![close](images/icons/close.svg) | `i-close` | Dialogs, toasts |
| ![edit](images/icons/edit.svg) | `i-edit` | Edit actions on cards |
| ![trash](images/icons/trash.svg) | `i-trash` | Delete actions; also the confirmation dialog |
| ![check](images/icons/check.svg) | `i-check` | Mark a subscription as renewed |
| ![undo](images/icons/undo.svg) | `i-undo` | Clear a renewal mark |
| ![arrow-right](images/icons/arrow-right.svg) | `i-arrow-right` | "View trend" link on project cards |
| ![info](images/icons/info.svg) | `i-info` | Trend chart unavailable hint |
| ![check-circle](images/icons/check-circle.svg) | `i-check-circle` | "No alerts" empty state — everything is healthy |
| ![error](images/icons/error.svg) | `i-error` | Load-failure state |

## Adding an icon

1. Draw a new `<symbol id="i-name" viewBox="0 0 24 24">` in the sprite in `ui/index.html`.
   Keep the paths inside the 24-grid, leave fill and stroke unset (inherit from `.icon`),
   and use 1.75-weight-friendly geometry: rounded corners, no sub-pixel detail.
2. Reference it from markup with `<svg class="icon" aria-hidden="true" focusable="false"><use href="#i-name"></use></svg>`.
   Icons are decorative; the accessible name comes from the surrounding button or heading.
3. If the icon is used by JavaScript-rendered markup, add a constant in `ui/src/ui/icons.ts`
   instead of pasting SVG strings.
4. Rebuild with `npm --prefix ui run build` — the sprite is part of `index.html`, so the
   emitted `dist/index.html` picks it up automatically.
