// The arithmetic of the Spend page: months, the pace a projection assumes,
// and the scale of the six-month history.
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { dayNumber, daysBetween, hasMetered, historyBars, isEmpty, isMonth, monthOf, monthSpan, paceToProjection, shiftMonth } from '../src/pages/spend/logic';

const HOUR = 3_600_000;

describe('months', () => {
  it('knows a month when it sees one', () => {
    assert.equal(isMonth('2026-11'), true);
    assert.equal(isMonth('2026-13'), false);
    assert.equal(isMonth('2026-1'), false);
    assert.equal(isMonth('2026-11-16'), false);
    assert.equal(isMonth(''), false);
    assert.equal(isMonth(null), false);
  });

  it('finds the month of a day', () => {
    assert.equal(monthOf('2026-11-16'), '2026-11');
  });

  it('moves across years in both directions', () => {
    assert.equal(shiftMonth('2026-11', 1), '2026-12');
    assert.equal(shiftMonth('2026-12', 1), '2027-01');
    assert.equal(shiftMonth('2026-01', -1), '2025-12');
    assert.equal(shiftMonth('2026-11', -5), '2026-06');
    assert.equal(shiftMonth('2026-11', -23), '2024-12');
    assert.equal(shiftMonth('2026-11', 0), '2026-11');
  });

  it('spans a month from midnight to midnight in the zone', () => {
    const { from, to } = monthSpan('2026-11', 'Asia/Shanghai');
    assert.equal(new Date(from).toISOString(), '2026-10-31T16:00:00.000Z');
    assert.equal(new Date(to).toISOString(), '2026-11-30T16:00:00.000Z');
  });

  it('spans a month that loses an hour to a clock change', () => {
    const { from, to } = monthSpan('2026-03', 'America/New_York');
    assert.equal((to - from) / HOUR, 31 * 24 - 1);
  });

  it('counts the days between two dates', () => {
    assert.equal(daysBetween('2026-11-16', '2026-11-19'), 3);
    assert.equal(daysBetween('2026-11-16', '2026-11-16'), 0);
    assert.equal(daysBetween('2026-11-16', '2026-11-03'), -13);
    assert.equal(daysBetween('2026-12-30', '2027-01-02'), 3);
  });

  it('names the day a month is on', () => {
    assert.equal(dayNumber({ days_elapsed: 15.8, days_in_month: 30 }), 16);
    assert.equal(dayNumber({ days_elapsed: 0, days_in_month: 30 }), 1);
    assert.equal(dayNumber({ days_elapsed: 30, days_in_month: 30 }), 30);
  });
});

describe('the pace a projection assumes', () => {
  const month = { is_current: true, total: 1310.49, projected: 2446.06, days_elapsed: 15.8, days_in_month: 30 };

  it('spreads the rest of the way over the days left', () => {
    const pace = paceToProjection(month);
    assert.ok(pace !== null);
    assert.equal(Math.round(pace * 100) / 100, 79.97);
  });

  it('has nothing to say about a month that is over', () => {
    assert.equal(paceToProjection({ ...month, is_current: false }), null);
  });

  it('has nothing to say in the last hours of a month', () => {
    assert.equal(paceToProjection({ ...month, days_elapsed: 29.9 }), null);
  });

  it('has nothing to say when the projection does not rise', () => {
    assert.equal(paceToProjection({ ...month, projected: month.total }), null);
  });
});

describe('what a month holds', () => {
  const row = { meter_id: 'm', display: 'M', unit: 'USD', spend: 0, projected: 0, budget: 100, base: 0, projected_base: 0 };
  const renewal = { reminder_id: 'r', title: 'R', date: '2026-11-03', amount: 10, currency: 'USD', base: 71.6 };

  it('is empty when nothing was spent and nothing is due', () => {
    assert.equal(isEmpty({ rows: [], renewals: [] }), true);
  });

  it('is still empty when all it has is a budget nobody spent against', () => {
    assert.equal(isEmpty({ rows: [row], renewals: [] }), true);
    assert.equal(hasMetered({ rows: [row] }), false);
  });

  it('is not empty with spend or with a renewal', () => {
    assert.equal(isEmpty({ rows: [{ ...row, spend: 1 }], renewals: [] }), false);
    assert.equal(isEmpty({ rows: [], renewals: [renewal] }), false);
    assert.equal(hasMetered({ rows: [{ ...row, projected: 2 }] }), true);
  });
});

describe('the six months', () => {
  const history = [
    { month: '2026-09', total: 300, fixed: 50 },
    { month: '2026-10', total: 2400, fixed: 50 },
    { month: '2026-11', total: 1200, fixed: 50 },
  ];

  it('scales to the tallest month', () => {
    const bars = historyBars(history, null);
    assert.deepEqual(
      bars.map((bar) => bar.share),
      [0.125, 1, 0.5],
    );
    assert.deepEqual(
      bars.map((bar) => bar.heading),
      [0, 0, 0],
    );
  });

  it('counts where the running month is heading into the scale', () => {
    const bars = historyBars(history, { month: '2026-11', projected: 3000 });
    assert.deepEqual(
      bars.map((bar) => bar.share),
      [0.1, 0.8, 0.4],
    );
    assert.deepEqual(
      bars.map((bar) => bar.heading),
      [0, 0, 1],
    );
  });

  it('adds nothing when the projection is no more than what is spent', () => {
    const bars = historyBars(history, { month: '2026-11', projected: 1200 });
    assert.equal(bars[2]?.heading, 0);
  });

  it('draws nothing when there is nothing', () => {
    const bars = historyBars([{ month: '2026-11', total: 0, fixed: 0 }], { month: '2026-11', projected: 0 });
    assert.deepEqual(bars, [{ month: '2026-11', total: 0, fixed: 0, share: 0, heading: 0 }]);
  });
});
