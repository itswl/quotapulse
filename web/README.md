# QuotaPulse dashboard

Preact 11 + TypeScript, `@preact/signals` for state, esbuild for the build,
`lucide-preact` for icons. No router library, no UI kit, no CSS framework, no
chart library — and no new dependency without a very good reason.

## Run

```sh
npm install
npm run build      # production build into dist/ (minified, no sourcemaps)
npm run watch      # rebuild on change, with sourcemaps
npm run typecheck  # tsc --noEmit, strict, for src/ and test/
npm test           # bundles test/*.test.ts and runs them with node --test
```

The Go server serves `dist/` — embedded in the binary (`embed.go`), or
straight from disk in a demo: build, reload the browser, done. `dist/` is
build output and is not committed; `dist/.gitkeep` must stay.

The server sends a strict Content-Security-Policy. Nothing may load from
another origin, and there is no inline `<script>`: fonts and icons are
bundled, and the theme is set by its own tiny file (`src/theme-boot.ts`)
before the first paint.

## Where things are

```
index.html            template; build.mjs fills in the hashed file names
public/               copied to dist/ as is: icons, manifest, sw.js
src/
  main.tsx            boot
  app.tsx             the shell: navigation, banners, toasts, the page outlet
  routes.ts           address -> page; every area is already registered
  router.ts           route, navigate(), setQuery(), <Link>, matchPath()
  hooks.ts            useTitle, useQuery, useAction
  format.ts           numbers, amounts, durations, dates
  errors.ts           errorText(): a failure as a sentence
  api/                client.ts (fetch), types.ts (the Go JSON), endpoints.ts
  state/              signal stores; resource.ts, live.ts, clock.ts, session.ts
  i18n/               index.ts (t, lang), areas/<area>.ts (the words)
  components/         shared components; import from '../../components'
  charts/             Sparkline, LineChart, BarChart, QuotaBar, BudgetBar, ChartFrame
  pages/<area>/       overview, meters, dates, spend, activity, settings, login
  styles/             tokens.css, base.css, components.css, charts.css, shell.css,
                      pages/<area>.css; index.css imports them all
test/                 unit tests for pure logic
```

## Adding a page

An area owns three places, all already wired into the router, the navigation,
the i18n index and the stylesheet:

    src/pages/<area>/   src/i18n/areas/<area>.ts   src/styles/pages/<area>.css

1. **The component.** `src/pages/<area>/index.tsx` default-exports the page;
   it receives `{ params }` (`PageProps`). Pages beneath it need no route:
   read `params.rest` (dates, spend, activity) or `params.section` and
   `params.rest` (settings) and decide what to render. Put further files
   next to `index.tsx`.
2. **The frame.** Start with `<PageHeader title=… actions=… />` (it holds the
   page's only `<h1>`) and call `useTitle(title)`.
3. **The words.** Add keys to both languages in `i18n/areas/<area>.ts` and
   use them as `t('<area>.<key>')`. The compiler rejects a key missing from
   one language, and a typo in a key.
4. **The data.** Every endpoint already has a typed function in
   `api/endpoints.ts` and its types in `api/types.ts`. See *Data* below.
5. **The styles.** `styles/pages/<area>.css`, every class prefixed with the
   area (`dates-…`). Tokens only; see *Styles*.
6. **Check.** `npm run typecheck && npm test && npm run build`, then look at
   it in light and dark, Chinese and English, at 1440, 820 and 390 px wide,
   with the browser console open.

Shared files — components, tokens, base and component CSS, router, shell,
hooks, format, api — are an interface other pages depend on. Add to them
when something is truly shared; do not rename, remove or restyle what exists.

## Text

- Every visible string goes through `t(key, params?)`. `{name}` is filled
  from `params`; `'one thing | {n} things'` picks by `params.n`.
- Write the Chinese as a native speaker would, not as a translation.
- Incidents arrive already written by the server in the dashboard's language
  (`headline`, `detail`, `facts`). Show them as given — `<IncidentRow>` does —
  and never rebuild those sentences here.
- Data that carries both languages (a provider's `name` / `name_zh`):
  `pick(en, zh)`.
- An error for the user: `errorText(error)`.

## Time and numbers

Use `format.ts` for all of it; never `toLocaleString`, never `new Date()` for
display.

- The API sends UTC instants and `YYYY-MM-DD` days. Everything is shown in
  the **user's configured zone** (`timezone` in `state/clock`), not the
  browser's; the functions in `format.ts` do that by default.
- "Now" is the **server's** clock: `now` in `state/clock` (epoch ms, ticks by
  itself). `formatAgo`, `formatIn` and `formatClock` use it by default.
- Money `1,234.56 CNY`, percent `38%`, other units `923 calls`:
  `formatAmount(value, unit)`, or `<Amount value unit />` to show the unit
  smaller and muted.
- Numbers that sit in columns or change get `class="num"`: digits of equal
  width, in the same face as the words. The mono face is for code and keys.

## Data

Reads come in two kinds.

**Shared, no parameters** — a *resource* (`state/resource.ts`): data, error
and loading as signals, cached across pages, refetched when one of its live
topics changes or the language does.

```ts
// state/reminders.ts
export const reminders = resource(listReminders, ['reminders', 'incidents', 'settings']);

// in a page
useResource(reminders);                 // fetch now, stay current while mounted
const list = reminders.data.value;      // undefined until the first answer
if (!list) return reminders.error.value ? <ErrorState error={reminders.error.value} onRetry={() => void reminders.load()} /> : <Skeleton … />;
```

`overview`, `meters`, `accounts`, `providers`, `settings` and `reminders`
exist. An area's own resources belong in its page directory.

**Keyed** — one meter's series, one month's spend — `useQuery` (`hooks.ts`):

```ts
const spend = useQuery(month, (signal) => getSpend(month, signal), ['meters', 'reminders', 'settings']);
// spend.data, spend.error, spend.loading, spend.settled, spend.reload(), spend.set(next)
```

While a new key loads, `data` is still the old key's answer and `settled` is
false: dim what is on screen rather than blanking it.

Live topics (`state/live.ts`) are what the server announces on
`/api/v1/stream`: `meters`, `reminders`, `incidents`, `activity`, `settings`.
Nothing polls.

**Writes.** Call the endpoint; put the answer on screen (`resource.set(…)`,
`query.set(…)`) or let the stream refresh it; confirm with `toast('Saved')`.
`useAction` gives `busy` and `error` for a button or a form. A 401 anywhere
shows the sign-in screen in place — do not handle it.

## Components

Import from `src/components`. Each file's header says how to use it.

| | |
|---|---|
| `Button`, `LinkButton`, `IconButton` | `variant`: primary (one per view), secondary, ghost, danger. `compact` folds the label away on a phone, for an icon that says it all. An icon-only button needs a `label`. |
| `Chip`, `FilterChip` | a small label with a `tone`; a toggle for a filter row |
| `Card`, `Section`, `PageHeader` | surface; titled part of a page; top of a page |
| `Dialog`, `ConfirmDialog` | native `<dialog>`: focus trap, Escape, focus returns. Content mounts only while open. |
| `Field`, `Input`, `Select`, `Switch`, `Segmented` | a control inside a `Field` takes its label, help and error from it |
| `Menu`, `Tabs` | a button with a list of actions; a row of tabs |
| `Tooltip`, `InfoTip` | explains its (focusable) child; never the only place a fact lives |
| `toast()`, `toast.error()` | passing confirmation, optional `action` (Undo) |
| `Banner` | a standing notice |
| `EmptyState`, `ErrorState`, `Skeleton` | nothing to show; failed to load; first load only |
| `StatusDot` | a state as icon **and** word, never colour alone |
| `Amount`, `Ago` | a number with its unit; "5 min ago" with the exact time on hover |
| `IncidentRow`, `MeterCard` | an open incident with acknowledge/undo; a meter at a glance |

Charts (`src/charts`) are hand-drawn SVG sized by their container. Wrap a
chart in `<ChartFrame title legend table>`: it adds the table view that every
chart must have. Marks take `--chart-1`; text stays in text colours.

## Styles

The look is ink on paper: text, buttons, selections and chart marks are one
ink on a warm ground, and the only hues are the four that mean something. A
colour on screen is always saying ok, warning, critical or info.

- **Tokens only** (`styles/tokens.css`): no raw colours, sizes off the scale
  or shadows of your own. Light is `:root`, dark is `[data-theme='dark']`;
  a new token needs both.
- Text colours: `--text`, `--text-2`, `--text-3` all pass WCAG AA on every
  surface. `--text-disabled` does not — disabled controls and decoration only.
- Colour carries meaning only: `--ok`, `--warning`, `--critical`, `--info`,
  each with a `-bg` tint. `--accent` is the ink itself — the one main action,
  what is selected, the focus ring — so a link in running text is told apart
  by its underline: any `<a>` without a class gets one.
- Type scale `--fs-11 … --fs-40`; space `--s-1 … --s-12` (4px grid); radii
  `--r-1/2/3` (a control, a card, a dialog). A surface has a hairline border
  and `--shadow-card`; only floating things get `--shadow-float`.
- Figures are in the sans face with digits of equal width (`.num`). A big
  figure that stands alone — a card's value — takes proportional ones
  (`font-variant-numeric: normal`). The mono face is for code and keys.
- Breakpoints: `max-width: 959px` (icon rail) and `max-width: 719px` (phone).
  Write the wide layout first. A phone gets a layout of its own rather than
  the wide one squeezed: a tab bar of five with Settings in the top bar, type
  a notch larger (the root size steps up, so `--fs-*` follow by themselves),
  40px controls, dialogs as sheets from the bottom edge, lists where the wide
  layout has a grid of cards. Stay clear of the notch and the home indicator
  with `env(safe-area-inset-*)`.
- Helpers in `base.css` and `components.css`: `.dotted` (the parts of a quiet
  line with a middle dot between them; it can wrap without a line starting
  on a dot), `.filter-row` (a row of FilterChips; scrolls sideways on a
  phone), `.truncate`, `.sr-only`.
- Class names: `block__part--variant`, prefixed with the area in page CSS.
- Motion only where it explains something; `prefers-reduced-motion` is
  already honoured globally.

## Accessibility

Real `<button>`s and links (`<Link href>` for in-app navigation); every input
inside a `Field` or with an `aria-label`; state never by colour alone;
dialogs through `Dialog`; the focus ring is global — do not remove outlines.

## Routing

`route.value` is `{ path, segments, query }`. `navigate('/dates')` pushes;
`setQuery({ filter: 'due' })` replaces — use it for filters and tabs so they
survive a reload without piling up behind the back button. The server
answers any non-file path with `index.html`, so every address can be opened
directly.
