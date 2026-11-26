# Reading a platform

QuotaPulse reads a platform through a **definition**: one HTTP request and one
[jq](https://jqlang.org/) program that turns the response into meters. Adding a
platform never means writing Go — the built-in catalog is made of exactly the
same definitions you can write under **Settings → Providers**.

## The built-in catalog

| ID | Platform | Meters | Checked |
| --- | --- | --- | --- |
| `deepseek` | DeepSeek | a balance per currency | live |
| `glm-coding` | Zhipu GLM Coding Plan | the token window (%, with its reset time) and the monthly tool quota | live |
| `openrouter` | OpenRouter | credits left | recorded |
| `siliconflow` | SiliconFlow | balance | docs |
| `moonshot` | Moonshot AI (Kimi) | balance | docs |
| `volcengine` | Volcengine | balance (signed request) | recorded |
| `aliyun` | Alibaba Cloud | balance (signed request) | recorded |
| `tikhub` | TikHub | balance | recorded |
| `uniapi` | UniAPI | credits | recorded |

*Checked* says how far an entry was verified, so nobody has to guess:
**live** — run against the real API; **recorded** — replays a response recorded
from the real API by an earlier version; **docs** — written from the platform's
documentation and not yet run against it. Every entry has a replay test in
`internal/provider/provider_test.go`. If a *docs* entry does not work for you, the
**Test** button on the account form shows the platform's actual response; save a
corrected copy under your own ID.

## Writing a definition

```jsonc
{
  "id": "my-relay",                      // lowercase letters, digits, dashes
  "name": "My relay",
  "fields": [                            // what the account form asks for
    {"key": "base_url", "label": "Base URL", "placeholder": "https://relay.example"},
    {"key": "api_key",  "label": "API Key", "secret": true}
  ],
  "request": {
    "method": "GET",                     // GET (default) or POST
    "url": "{{base_url}}/v1/dashboard/billing/subscription",
    "headers": {"X-Client": "quotapulse"},
    "query": {"since": "{{month_start_date}}"},
    "auth": {"type": "bearer"}           // see below
  },
  "extract": "{key: \"balance\", kind: \"balance\", unit: \"USD\", value: (.hard_limit_usd | num)}"
}
```

Save it with `PUT /api/v1/providers/my-relay` or from the dashboard, which also
lets you try it against a real credential before saving.

### Fields and templates

Each field becomes an input on the account form. `secret` fields are stored
encrypted, never shown again, and never visible to the jq program.

`{{field}}` in the URL, a header, a query value, the body or the auth value is
replaced by that field. Always available as well:

| | |
| --- | --- |
| `{{now}}` | the current Unix time |
| `{{today}}` | today, `YYYY-MM-DD`, in the configured time zone |
| `{{month_start}}`, `{{month_start_date}}` | the start of the month as a Unix time, and as a date |

### Auth

| `type` | Sends |
| --- | --- |
| `bearer` | `Authorization: Bearer <value>` |
| `header` | `<name>: <value>` |
| `query` | `?<name>=<value>` |
| `basic` | `Authorization: Basic base64(<value>)`, where value is `user:password` |
| `none` | nothing; put the credential in the URL, a header or the body with a template |
| `aliyun-rpc` | an Alibaba Cloud RPC signature over the query. Needs fields `access_key_id` and `access_key_secret` |
| `volc-v4` | a Volcengine V4 signature. Needs fields `access_key_id` and `secret_access_key`, and `region` and `service` in `auth` |

`value` is a template and defaults to `{{api_key}}`.

### The extract program

The program's input is the response body. Each object it outputs is a meter:

| Key | | |
| --- | --- | --- |
| `value` | required | The current value: what is left of a balance or quota, the total so far of a counter. A number, or a numeric string |
| `key` | default `main` | Identifies the meter within the account. Keep it stable: history is attached to it |
| `kind` | default `balance` | `balance`, `quota` or `counter` — see [how each is read](ARCHITECTURE.md#a-meter-has-a-kind-and-the-kind-decides-how-its-history-is-read) |
| `unit` | | A currency code (`USD`), `%`, or any label (`calls`). Only currency codes are added up in the spend view |
| `name`, `name_zh` | | What a new meter is called, in English and Chinese. Renaming it in the dashboard sticks |
| `limit` | | The size of a quota |
| `resets_at` | | When a quota resets: Unix seconds, Unix milliseconds or RFC 3339 |
| `window` | | The length of the quota's window in seconds. With `resets_at` it gives the pace: whether the quota will last until it resets |

Available inside the program:

- **`num`** — reads a number wherever a platform hid it: `547.67`, `"547.67"`,
  `"1,234.56"`, or `"剩余263419余额"`.
- **`error("…")`** — fails the sample with that message. Use it for an API that
  answers 200 with an error inside.
- **`$now`** (Unix seconds) and **`$fields`** (the account's non-secret fields).

A non-2xx response fails the sample before the program runs, with the platform's
own message when one can be found in the body.

### Examples

**Several meters from one response** — a balance per currency:

```jq
.balance_infos[]? | {
  key:  ("balance-" + (.currency | ascii_downcase)),
  kind: "balance", unit: .currency,
  value: (.total_balance | num)
}
```

**A difference of two fields**, behind an envelope that may or may not be there:

```jq
(.data // .) | {
  key: "credits", kind: "balance", unit: "USD",
  value: ((.total_credits | num) - (.total_usage | num))
}
```

**An error inside a 200**, and a number inside a sentence:

```jq
if .code != 0 then error(.msg // "the request was rejected") else {
  key: "credits", kind: "balance", unit: "credits",
  value: ((.msg // .data.score) | num)
} end
```

**A quota with a reset time and a window**, so its pace can be judged:

```jq
.data.limits[]? | select(.type == "TOKENS_LIMIT") | {
  key: "tokens-5h", name: "5-hour window", name_zh: "5 小时窗口",
  kind: "quota", unit: "%",
  value: (100 - (.percentage // 0)), limit: 100,
  resets_at: .nextResetTime, window: 18000
}
```

## What the fetcher will and will not do

- Retries a `GET` twice on a network error, 429 or 5xx. A `POST` is sent once.
- Gives each attempt 15 seconds, and the program 2.
- Does **not** follow redirects: a redirect to another host would take the
  credential with it. Point the URL at the final address.
- Removes the account's field values from every error message before it is stored,
  logged, shown or sent. Platforms like to echo the key they rejected.
- Reads at most 4 MB of response.

## When there is no API

Push the number instead. Anything that can make an HTTP request can report a
reading — see [Pushing readings](API.md#pushing-readings). A pushed meter gets the
same forecast, rules and incidents as a sampled one, plus a rule of its own: warn
when it has been silent for too long.
