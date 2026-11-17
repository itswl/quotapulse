#!/usr/bin/env python3
"""Report what Claude Code has cost this month to a QuotaPulse instance.

Reads ~/.claude/projects/**/*.jsonl, adds up the current calendar month, and
pushes the total as one reading of a counter. Run it as often as you like —
an hourly cron job is typical: a reading is a statement of "the total is now
X", so repeating it changes nothing, and the counter starting over on the
first of the month is exactly what QuotaPulse expects of a counter.

    QUOTAPULSE_URL=https://pulse.example QUOTAPULSE_TOKEN=qp_... \\
        ./claude-code-usage.py

The token needs the `ingest` scope and nothing else (Settings -> Tokens).

Give the meter a monthly budget in the dashboard to be warned when the month
is on course to pass it, and a "silent for" limit to be told when this script
stops running.

Cost: Claude Code writes costUSD on some records; those are summed as they
are. Records that only carry token counts cost 0 unless you say what the
model costs per million tokens:

    --price "claude-sonnet-4-5:3,15"      # MODEL-SUBSTRING:INPUT,OUTPUT
"""

import argparse
import datetime
import json
import os
import pathlib
import sys
import urllib.error
import urllib.request

CLAUDE_DIR = pathlib.Path.home() / ".claude" / "projects"


def collect(since):
    """Yield (model, cost, input_tokens, output_tokens) for records on or after `since`."""
    if not CLAUDE_DIR.is_dir():
        return
    for path in sorted(CLAUDE_DIR.rglob("*.jsonl")):
        try:
            with path.open(encoding="utf-8", errors="replace") as handle:
                for line in handle:
                    try:
                        record = json.loads(line)
                    except json.JSONDecodeError:
                        continue
                    if not isinstance(record, dict):
                        continue
                    try:
                        if datetime.date.fromisoformat(str(record.get("timestamp", ""))[:10]) < since:
                            continue
                    except ValueError:
                        continue
                    message = record.get("message")
                    usage = message.get("usage") if isinstance(message, dict) else None
                    if not usage:
                        continue
                    model = str(record.get("model") or message.get("model") or "unknown")
                    model = model.rsplit("/", 1)[-1]
                    cost = float(record.get("costUSD") or 0.0)
                    tokens_in = int(usage.get("input_tokens") or 0)
                    tokens_out = int(usage.get("output_tokens") or 0)
                    # <synthetic> keep-alives and empty turns say nothing
                    if model.startswith("<") or (cost == 0 and tokens_in == 0 and tokens_out == 0):
                        continue
                    yield model, cost, tokens_in, tokens_out
        except OSError:
            continue


def parse_prices(specs):
    prices = {}
    for spec in specs:
        name, _, rates = spec.partition(":")
        rate_in, _, rate_out = rates.partition(",")
        try:
            prices[name] = (float(rate_in), float(rate_out))
        except ValueError:
            sys.exit(f"bad --price {spec!r}; want MODEL:3,15")
    return prices


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--url", default=os.environ.get("QUOTAPULSE_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--token", default=os.environ.get("QUOTAPULSE_TOKEN", ""),
                        help="an API token with the ingest scope (default: $QUOTAPULSE_TOKEN)")
    parser.add_argument("--meter", default="claude-code", help="the meter's key (default: claude-code)")
    parser.add_argument("--name", default="Claude Code", help="its display name, used when the meter is new")
    parser.add_argument("--currency", default="USD")
    parser.add_argument("--price", action="append", default=[],
                        help="MODEL:IN,OUT per million tokens, for records without a cost; repeatable")
    parser.add_argument("--dry-run", action="store_true", help="print what would be sent, and send nothing")
    args = parser.parse_args()

    prices = parse_prices(args.price)
    month_start = datetime.date.today().replace(day=1)

    total, records, unpriced = 0.0, 0, 0
    for model, cost, tokens_in, tokens_out in collect(month_start):
        if cost == 0:
            rates = next((rates for name, rates in prices.items() if name in model), None)
            if rates:
                cost = (tokens_in * rates[0] + tokens_out * rates[1]) / 1_000_000
            else:
                unpriced += 1
        total += cost
        records += 1

    reading = {
        "meter": args.meter, "name": args.name, "kind": "counter",
        "unit": args.currency, "value": round(total, 4),
    }
    if unpriced:
        print(f"note: {unpriced} of {records} records carry no cost and match no --price; they count as 0",
              file=sys.stderr)
    if args.dry_run:
        print(json.dumps(reading, indent=2))
        return
    if not args.token:
        sys.exit("QUOTAPULSE_TOKEN (or --token) is required")

    request = urllib.request.Request(
        args.url.rstrip("/") + "/api/v1/ingest", data=json.dumps(reading).encode(),
        headers={"Content-Type": "application/json", "Authorization": "Bearer " + args.token},
        method="POST")
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            response.read()
    except urllib.error.HTTPError as error:
        detail = error.read().decode(errors="replace")
        try:
            detail = json.loads(detail)["error"]["message"]
        except (ValueError, KeyError, TypeError):
            pass
        sys.exit(f"QuotaPulse refused the reading: HTTP {error.code}: {detail}")
    except urllib.error.URLError as error:
        sys.exit(f"could not reach {args.url}: {error.reason}")
    print(f"reported {reading['value']} {args.currency} for {month_start:%Y-%m} from {records} records")


if __name__ == "__main__":
    main()
