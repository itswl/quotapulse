// What is ahead: which problems ask for a person, what happens when, and
// that nothing is said twice.
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import type { IncidentView, MeterView, Overview, ReminderView } from '../src/api/types';
import { bucketOf, budgetCrossing, buildHorizon, verdictOf } from '../src/pages/now/horizon';

const ZONE = 'Asia/Shanghai';
const DAY = 86_400_000;
// 10:00 on Wednesday, November 18 2026, in Shanghai.
const NOW = Date.parse('2026-11-18T02:00:00Z');
const T = '2026-11-01T00:00:00Z';
const at = (days: number) => new Date(NOW + days * DAY).toISOString();

function meter(over: Partial<MeterView> & { id: string }): MeterView {
  return {
    account_id: 'acc',
    key: 'main',
    name: over.id,
    renamed: false,
    kind: 'balance',
    unit: 'USD',
    value: 10,
    limit: null,
    resets_at: null,
    window_seconds: null,
    sampled_at: T,
    rules: { low: null, runway_days: null, anomaly: null, budget: null, pace: false, stale_after_seconds: null },
    muted_until: null,
    hidden: false,
    sort: 0,
    created_at: T,
    updated_at: T,
    display: over.id,
    status: 'ok',
    muted: false,
    account: null,
    forecast: null,
    quota: null,
    month: null,
    effective: { runway_days: 7, anomaly: true },
    incidents: [],
    spark: [],
    ...over,
  };
}

function runsOutIn(days: number) {
  return {
    as_of: T,
    coverage_days: 30,
    confidence: 'high' as const,
    burn_per_day: 1,
    burn_7d: 1,
    burn_30d: 1,
    peak_per_day: 1,
    spend_24h: 1,
    runway_days: days,
    depletes_at: at(days),
    runway_days_at_peak: days,
    last_top_up: null,
  };
}

function reminder(over: Partial<ReminderView> & { id: string; days: number }): ReminderView {
  const next = new Date(NOW + over.days * DAY + 8 * 3_600_000).toISOString().slice(0, 10);
  return {
    title: over.id,
    kind: 'renewal',
    rule: { freq: 'monthly', day: 1 },
    lead_days: [3, 0],
    amount: null,
    currency: '',
    group: '',
    notes: '',
    done_through: null,
    snoozed_until: null,
    enabled: true,
    created_at: T,
    updated_at: T,
    next,
    lunar: null,
    ordinal: 0,
    state: over.days <= 3 ? 'soon' : 'upcoming',
    handled: null,
    following: [],
    monthly_amount: null,
    ...over,
  };
}

function incident(over: Partial<IncidentView> & { id: string; subject_id: string }): IncidentView {
  return {
    subject_type: 'meter',
    kind: 'level',
    dedup: over.id,
    severity: 'warning',
    state: 'open',
    reason: '',
    title: '',
    data: null,
    opened_at: T,
    updated_at: T,
    resolved_at: null,
    acked_at: null,
    notified_at: null,
    notify_count: 0,
    headline: over.id,
    detail: '',
    facts: [],
    path: '/',
    ...over,
  };
}

function overview(over: Partial<Overview>): Overview {
  return {
    now: new Date(NOW).toISOString(),
    timezone: ZONE,
    status: 'ok',
    attention: [],
    meters: [],
    accounts: [],
    upcoming: [],
    spend: { month: '2026-11', base: 'CNY', total: 0, projected: 0, fixed: 0, unconverted: [] },
    counts: { accounts: 0, meters: 0, reminders: 0, channels: 0 },
    muted: false,
    ...over,
  };
}

describe('the problems at the top', () => {
  it('puts what asks for a person first: unacknowledged, then the worst, then the oldest', () => {
    const data = overview({
      attention: [
        incident({ id: 'known', subject_id: 'm1', severity: 'critical', acked_at: T }),
        incident({ id: 'warn-old', subject_id: 'm2', opened_at: '2026-11-02T00:00:00Z' }),
        incident({ id: 'warn-new', subject_id: 'm3', opened_at: '2026-11-10T00:00:00Z' }),
        incident({ id: 'crit', subject_id: 'm4', severity: 'critical' }),
      ],
    });
    assert.deepEqual(
      buildHorizon(data, NOW, ZONE).now.map((each) => each.id),
      ['crit', 'warn-old', 'warn-new', 'known'],
    );
  });

  it('leaves a near date on its day, with its incident', () => {
    const data = overview({
      upcoming: [reminder({ id: 'icloud', days: 3 })],
      attention: [incident({ id: 'due', subject_type: 'reminder', subject_id: 'icloud', kind: 'due', severity: 'info' })],
    });
    const horizon = buildHorizon(data, NOW, ZONE);
    assert.deepEqual(horizon.now, []);
    const event = horizon.groups[0]!.events[0]!;
    assert.equal(event.reminder?.id, 'icloud');
    assert.equal(event.incident?.id, 'due');
    assert.equal(event.estimated, false);
  });

  it('does not repeat a meter further down once it is a problem, nor the meters of an account that cannot be read', () => {
    const data = overview({
      meters: [
        meter({ id: 'low', forecast: runsOutIn(5) }),
        meter({ id: 'unread', account_id: 'kimi', forecast: runsOutIn(9) }),
        meter({ id: 'fine', forecast: runsOutIn(20) }),
      ],
      attention: [
        incident({ id: 'level', subject_id: 'low' }),
        incident({ id: 'failing', subject_type: 'account', subject_id: 'kimi', kind: 'failing' }),
      ],
    });
    const horizon = buildHorizon(data, NOW, ZONE);
    const ahead = horizon.groups.flatMap((group) => group.events.map((event) => event.meter?.id));
    assert.deepEqual(ahead, ['fine']);
    assert.deepEqual(horizon.steady, []);
  });
});

describe('what happens next', () => {
  it('puts a balance on the day it runs out, and one that lasts past the horizon among the steady', () => {
    const data = overview({
      meters: [meter({ id: 'soon', forecast: runsOutIn(5.5) }), meter({ id: 'long', forecast: runsOutIn(70) })],
    });
    const horizon = buildHorizon(data, NOW, ZONE, 60);
    assert.equal(horizon.groups.length, 1);
    assert.equal(horizon.groups[0]!.bucket, 'week');
    const event = horizon.groups[0]!.events[0]!;
    assert.equal(event.kind, 'runout');
    assert.equal(event.day, '2026-11-23');
    assert.equal(event.days, 5);
    assert.equal(event.estimated, true);
    assert.deepEqual(horizon.steady.map((each) => each.id), ['long']);
  });

  it('notes a quota that runs out before it resets', () => {
    const quota = { remaining_pct: 10, used_pct: 90, elapsed_pct: 40, projected_pct: 180, exhausts_at: at(0.2) };
    const data = overview({ meters: [meter({ id: 'window', kind: 'quota', unit: '%', quota })] });
    const event = buildHorizon(data, NOW, ZONE).groups[0]!.events[0]!;
    assert.equal(event.kind, 'exhaust');
    assert.equal(event.days, 0);
  });

  it('works out when a month passes its budget', () => {
    const month = { month: '2026-11', spend: 75, projected: 132, days_elapsed: 18, days_in_month: 30 };
    const claude = meter({ id: 'claude', kind: 'counter', month, rules: { ...meter({ id: 'x' }).rules, budget: 100 } });
    // 57 more over 12 days is 4.75 a day; the 25 left of the budget lasts 25 / 4.75 days.
    assert.equal(budgetCrossing(claude, NOW), NOW + (25 / 4.75) * DAY);
    assert.equal(budgetCrossing({ ...claude, month: { ...month, spend: 120 } }, NOW), null, 'already over');
    assert.equal(budgetCrossing({ ...claude, month: { ...month, projected: 90 } }, NOW), null, 'stays under');
    assert.equal(buildHorizon(overview({ meters: [claude] }), NOW, ZONE).groups[0]!.events[0]!.kind, 'budget');
  });

  it('leaves out what is hidden or paused, and keeps the rest steady', () => {
    const data = overview({
      meters: [meter({ id: 'hidden', hidden: true, forecast: runsOutIn(3) }), meter({ id: 'paused', status: 'paused', forecast: runsOutIn(3) }), meter({ id: 'quota', kind: 'quota' })],
    });
    const horizon = buildHorizon(data, NOW, ZONE);
    assert.deepEqual(horizon.groups, []);
    assert.deepEqual(horizon.steady.map((each) => each.id), ['quota']);
    assert.equal(horizon.next, null);
  });

  it('orders by day, a date before a forecast on the same day, and groups by how far off it is', () => {
    const data = overview({
      meters: [meter({ id: 'runs-out', forecast: runsOutIn(3.2) })],
      upcoming: [reminder({ id: 'later', days: 40 }), reminder({ id: 'renews', days: 3 }), reminder({ id: 'birthday', days: 1 })],
    });
    const horizon = buildHorizon(data, NOW, ZONE);
    assert.deepEqual(
      horizon.groups.map((group) => [group.bucket, group.events.map((event) => event.key)]),
      [
        ['tomorrow', ['reminder:birthday']],
        ['week', ['reminder:renews', 'meter:runs-out']],
        ['later', ['reminder:later']],
      ],
    );
    assert.equal(horizon.next?.key, 'reminder:birthday');
  });

  it('names how far off a day is', () => {
    assert.deepEqual([0, 1, 2, 7, 8, 31, 32].map(bucketOf), ['today', 'tomorrow', 'week', 'week', 'month', 'month', 'later']);
  });
});

describe('the verdict', () => {
  it('counts what asks for a person, and what is known', () => {
    assert.deepEqual(verdictOf([]), { tone: 'ok', fresh: 0, known: 0 });
    assert.deepEqual(verdictOf([incident({ id: 'a', subject_id: 'x', acked_at: T })]), { tone: 'neutral', fresh: 0, known: 1 });
    assert.deepEqual(
      verdictOf([incident({ id: 'a', subject_id: 'x' }), incident({ id: 'b', subject_id: 'y', severity: 'critical' }), incident({ id: 'c', subject_id: 'z', acked_at: T })]),
      { tone: 'critical', fresh: 2, known: 1 },
    );
  });
});
