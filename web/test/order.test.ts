// The order meters are listed in, and the direction a change of page goes.
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import type { MeterView } from '../src/api/types';
import { byUrgency, daysToTrouble, matchesMeter, needsLook } from '../src/pages/meters/order';
import { directionOf } from '../src/router';

const NOW = Date.parse('2026-11-18T02:00:00Z');
const DAY = 86_400_000;
const T = '2026-11-01T00:00:00Z';

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

const lasting = (days: number | null) => ({
  as_of: T,
  coverage_days: 30,
  confidence: 'high' as const,
  burn_per_day: 1,
  burn_7d: 1,
  burn_30d: 1,
  peak_per_day: 1,
  spend_24h: 1,
  runway_days: days,
  depletes_at: days === null ? null : new Date(NOW + days * DAY).toISOString(),
  runway_days_at_peak: days,
  last_top_up: null,
});

describe('the order of meters', () => {
  it('puts problems first, worst first, then the soonest to become one, then by name', () => {
    const list = [
      meter({ id: 'b-long', forecast: lasting(200) }),
      meter({ id: 'warning', status: 'warning', forecast: lasting(5) }),
      meter({ id: 'a-long', forecast: lasting(200) }),
      meter({ id: 'soon', forecast: lasting(20) }),
      meter({ id: 'critical', status: 'critical', forecast: lasting(13) }),
      meter({ id: 'unknown', forecast: lasting(null) }),
      meter({ id: 'paused', status: 'paused' }),
    ];
    assert.deepEqual(list.sort(byUrgency(NOW)).map((each) => each.id), ['critical', 'warning', 'soon', 'a-long', 'b-long', 'unknown', 'paused']);
  });

  it('counts a meter whose account cannot be read among the ones that need a look', () => {
    const unread = meter({ id: 'kimi', account: { id: 'acc', name: 'Kimi', provider: 'kimi', provider_name: 'Kimi', group: '', status: 'failing', last_error: 'HTTP 401' } });
    assert.equal(needsLook(unread), true);
    assert.equal(needsLook(meter({ id: 'fine' })), false);
  });

  it('knows how far off trouble is for each kind of meter', () => {
    assert.equal(daysToTrouble(meter({ id: 'empty', value: 0 }), NOW), 0);
    assert.equal(daysToTrouble(meter({ id: 'collecting', forecast: { ...lasting(9), confidence: 'none' } }), NOW), Infinity);
    const quota = { remaining_pct: 10, used_pct: 90, elapsed_pct: 40, projected_pct: 180, exhausts_at: new Date(NOW + DAY / 2).toISOString() };
    assert.equal(daysToTrouble(meter({ id: 'window', kind: 'quota', quota }), NOW), 0.5);
    const month = { month: '2026-11', spend: 120, projected: 150, days_elapsed: 18, days_in_month: 30 };
    const over = meter({ id: 'over', kind: 'counter', month, rules: { ...meter({ id: 'x' }).rules, budget: 100 } });
    assert.equal(daysToTrouble(over, NOW), 0, 'already past its budget');
  });

  it('finds a meter by its name, its platform or its group', () => {
    const silicon = meter({ id: 'sf', display: '硅基流动', account: { id: 'acc', name: '硅基流动', provider: 'siliconflow', provider_name: 'SiliconFlow', group: 'AI 平台', status: 'ok', last_error: '' } });
    assert.equal(matchesMeter(silicon, 'silicon'), true);
    assert.equal(matchesMeter(silicon, 'ai 平台'), true);
    assert.equal(matchesMeter(silicon, '硅基'), true);
    assert.equal(matchesMeter(silicon, 'deepseek'), false);
    assert.equal(matchesMeter(silicon, '  '), true);
  });
});

describe('the way a change of page goes', () => {
  it('goes forward into a meter, back out of it, and across between places', () => {
    assert.equal(directionOf('/meters', '/meters/mtr_1'), 'forward');
    assert.equal(directionOf('/meters/mtr_1', '/meters'), 'back');
    assert.equal(directionOf('/', '/meters'), 'forward');
    assert.equal(directionOf('/meters', '/spend'), 'swap');
    assert.equal(directionOf('/settings/channels', '/settings'), 'back');
  });
});
