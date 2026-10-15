#!/usr/bin/env python3
"""Report Claude Code usage to a QuotaPulse instance.

Walks ~/.claude/projects/**/*.jsonl, aggregates the current month per model,
and POSTs one batch to /api/usage/ingest. Re-running overwrites the same
day/model cells, so a cron job every hour is safe — reports are refreshes.

Usage:
  QUOTAPULSE_URL=http://quotapulse:8080 QUOTAPULSE_KEY=... \
      ./claude-code-usage.py [--project claude-code] [--since-days N] [--dry-run]

Cost: Claude Code writes costUSD per assistant message; those numbers are
summed directly. Older records without costUSD report tokens only (cost 0)
unless --price "MODEL:IN,OUT per Mtok" is given, e.g.
  --price "claude-sonnet-4-5:3,15"
"""

import argparse
import datetime
import json
import os
import pathlib
import sys
import urllib.request

CLAUDE_DIR = pathlib.Path.home() / ".claude" / "projects"


def collect(since: datetime.date):
    """Yield (date, model, cost, in_tokens, out_tokens) from JSONL logs."""
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
                    when = record.get("timestamp", "")
                    day = when[:10]
                    try:
                        if datetime.date.fromisoformat(day) < since:
                            continue
                    except ValueError:
                        continue
                    message = record.get("message") or {}
                    usage = message.get("usage") if isinstance(message, dict) else None
                    if not usage:
                        continue
                    model = record.get("model") or message.get("model") or "all"
                    if "/" in model:
                        model = model.rsplit("/", 1)[-1]
                    cost = record.get("costUSD") or 0.0
                    tin = int(usage.get("input_tokens") or 0)
                    tout = int(usage.get("output_tokens") or 0)
                    # <synthetic> keep-alives and empty turns carry no signal
                    if model.startswith("<") or (cost == 0 and tin == 0 and tout == 0):
                        continue
                    yield (day, model, float(cost), tin, tout)
        except OSError:
            continue


def price_for(model, prices):
    if not prices:
        return None
    for name, rates in prices.items():
        if name in model:
            return rates
    return None


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--url", default=os.environ.get("QUOTAPULSE_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--key", default=os.environ.get("QUOTAPULSE_KEY", ""))
    parser.add_argument("--source", default="claude-code")
    parser.add_argument("--project", default="claude-code")
    parser.add_argument("--currency", default="USD")
    parser.add_argument("--since-days", type=int, default=0, help="0 = month to date")
    parser.add_argument("--price", action="append", default=[],
                        help='MODEL:IN,OUT per million tokens, repeatable')
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()

    today = datetime.date.today()
    since = today.replace(day=1)
    if args.since_days:
        since = today - datetime.timedelta(days=args.since_days)

    prices = {}
    for spec in args.price:
        name, _, rates = spec.partition(":")
        tin, _, tout = rates.partition(",")
        try:
            prices[name] = (float(tin), float(tout))
        except ValueError:
            sys.exit(f"bad --price {spec!r}; want MODEL:3,15")

    cells = {}
    for day, model, cost, tin, tout in collect(since):
        cell = cells.setdefault((day, model), {"cost": 0.0, "in": 0, "out": 0, "n": 0})
        if cost == 0 and (tin or tout):  # token-only records priced when a table is given
            rates = price_for(model, prices)
            if rates:
                cost = (tin * rates[0] + tout * rates[1]) / 1_000_000
        cell["cost"] += cost
        cell["in"] += tin
        cell["out"] += tout
        cell["n"] += 1

    if not cells:
        print(f"no usage since {since} under {CLAUDE_DIR}")
        return

    entries = [
        {
            "date": day,
            "model": model,
            "cost": round(cell["cost"], 6),
            "currency": args.currency,
            "input_tokens": cell["in"],
            "output_tokens": cell["out"],
            "requests": cell["n"],
        }
        for (day, model), cell in sorted(cells.items())
    ]
    body = json.dumps({"source": args.source, "project": args.project,
                       "entries": entries}).encode()

    if args.dry_run:
        print(json.dumps({"source": args.source, "project": args.project,
                          "entries": entries}, indent=2))
        return
    if not args.key:
        sys.exit("QUOTAPULSE_KEY (or --key) is required")

    request = urllib.request.Request(
        args.url.rstrip("/") + "/api/usage/ingest", data=body,
        headers={"Content-Type": "application/json", "X-API-Key": args.key},
        method="POST")
    with urllib.request.urlopen(request, timeout=30) as response:
        print(response.status, json.loads(response.read()).get("message"))


if __name__ == "__main__":
    main()
