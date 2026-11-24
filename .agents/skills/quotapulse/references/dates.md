# Dates

A date is a title, a kind and a rule. The rule says which days it falls on.
`add_date` takes the rule's fields flat, next to the title. Over HTTP they go in a
nested `rule` object instead.

## Kinds

| `kind` | For | 中文 |
| --- | --- | --- |
| `renewal` | A subscription or plan that charges again. Give `amount` and `currency` so it counts towards the month's fixed costs | 续费、订阅 |
| `birthday` | A person. Give `since`, the birth year, and reminders say which birthday it is | 生日 |
| `anniversary` | | 纪念日 |
| `holiday` | | 节日 |
| `expiry` | A passport, a certificate, a domain | 到期 |
| `custom` | Anything else. This is the default | |

## Rules

| `freq` | Needs | Notes |
| --- | --- | --- |
| `once` | `date` (`YYYY-MM-DD`) | Always solar |
| `weekly` | `weekday` (1 is Monday, 7 is Sunday) | Always solar |
| `monthly` | `day` | On the solar calendar, the 31st lands on the last day of a shorter month |
| `yearly` | `month`, `day` | On the solar calendar, February 29 lands on the 28th in a common year |

The optional fields:

- **`calendar: "lunar"`** works only with `monthly` and `yearly`. It uses the
  Chinese lunisolar calendar.
  - The day is 1 to 30. A day past the end of a short month lands on its last
    day, so month 12, day 30 is New Year's Eve (除夕) every year.
  - A rule always names a regular month, never a leap one (闰月). Someone born in
    a leap month is reminded in the regular month of that number.
- **`interval`** makes a date repeat every N weeks, months or years, up to 100.
  - It needs a `date` to count from.
  - It works only on the solar calendar.
  - For `weekly`, the weekday of that `date` is used when `weekday` is not given.
- **`since`** is the year it started. It is used only to say which occurrence
  this is.
- **`lead_days`** says on which days to remind before the date, where 0 is the
  day itself.
  - The default is `[0]`.
  - Each value is 0 to 366, and there can be at most 12.
  - Reminders go out at the user's reminder time (09:00 unless they changed it),
    in their time zone.
- **`amount`** and **`currency`** are what a renewal costs. Without a currency,
  the user's base currency is assumed.

## From a request to a call

Work out relative days such as "next Friday" or "in three months" from `today`
in `list_dates`. That is today in the user's time zone, which may not be today
on your clock.

| The user says | `add_date` |
| --- | --- |
| ChatGPT renews on the 15th, 20 dollars, tell me three days before | `{"title": "ChatGPT", "kind": "renewal", "freq": "monthly", "day": 15, "amount": 20, "currency": "USD", "lead_days": [3, 0]}` |
| 妈妈农历十月初九生日，1962 年生 | `{"title": "妈妈", "kind": "birthday", "freq": "yearly", "calendar": "lunar", "month": 10, "day": 9, "since": 1962, "lead_days": [7, 1, 0]}` |
| 除夕提醒我 | `{"title": "除夕", "kind": "holiday", "freq": "yearly", "calendar": "lunar", "month": 12, "day": 30, "lead_days": [3, 0]}` |
| My passport expires on 2031-05-20 | `{"title": "Passport", "kind": "expiry", "freq": "once", "date": "2031-05-20", "lead_days": [180, 90, 30]}` |
| Payroll every other Friday, the next one is December 4th | `{"title": "Payroll", "freq": "weekly", "interval": 2, "date": "2026-12-04"}` |
| The domain renews every year on March 3 | `{"title": "Domain", "kind": "renewal", "freq": "yearly", "month": 3, "day": 3, "lead_days": [30, 7]}` |

Pick lead days that leave time to act. A birthday needs time to send something,
and a passport needs months. When the user gives lead days, use theirs.

## Pitfalls

- **A lunar birthday given as a solar date.** "农历生日" with a birth date like
  1962-11-05 needs that day's lunar month and day. Do not work it out in your
  head. Ask the user, since people who keep a lunar birthday usually know it, or
  convert with a proper calendar library.
- **A lunar one-off.** A `once` date is always solar. Convert the day first.
- **Checking the result.** After saving, compare `next` (and `lunar`) with what
  the user said. Over HTTP, `POST /api/v1/reminders/preview` shows where a rule
  lands before anything is saved.
