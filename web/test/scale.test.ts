// The arithmetic under the charts.
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { bucketDays, chooseBucket, linear, nearest, niceStep, niceTicks, timeTicks } from '../src/charts/scale';
import { dayOf, dayStart, zonedParts } from '../src/format';

const HOUR = 3_600_000;
const DAY = 24 * HOUR;
const SHANGHAI = 'Asia/Shanghai';

describe('value axis', () => {
  it('maps one interval onto another', () => {
    const y = linear(0, 100, 200, 0);
    assert.equal(y(0), 200);
    assert.equal(y(25), 150);
    assert.equal(y(100), 0);
    assert.equal(linear(5, 5, 0, 10)(5), 5);
  });

  it('steps by round numbers', () => {
    assert.equal(niceStep(87, 4), 25);
    assert.equal(niceStep(4.7, 2), 2.5);
    assert.equal(niceStep(1280, 4), 500);
    assert.equal(niceStep(0.3, 3), 0.1);
  });

  it('ticks from zero to the first round number that covers the data', () => {
    assert.deepEqual(niceTicks(87), [0, 25, 50, 75, 100]);
    assert.deepEqual(niceTicks(4.7, 2), [0, 2.5, 5]);
    assert.deepEqual(niceTicks(1280), [0, 500, 1000, 1500]);
    assert.deepEqual(niceTicks(100), [0, 25, 50, 75, 100]);
    assert.deepEqual(niceTicks(0.3, 3), [0, 0.1, 0.2, 0.3]);
  });

  it('still gives an axis when there is nothing to show', () => {
    assert.deepEqual(niceTicks(0), [0, 1]);
    assert.deepEqual(niceTicks(Number.NaN), [0, 1]);
  });

  it('finds the nearest of a sorted list', () => {
    const xs = [10, 20, 30, 40];
    assert.equal(nearest(xs, 5), 0);
    assert.equal(nearest(xs, 24), 1);
    assert.equal(nearest(xs, 26), 2);
    assert.equal(nearest(xs, 99), 3);
    assert.equal(nearest([], 1), -1);
    assert.equal(nearest([7], 100), 0);
  });
});

describe('time axis', () => {
  const now = Date.UTC(2026, 10, 16, 6, 8); // Mon Nov 16, 14:08 in Shanghai

  it('ticks a day on whole hours of the zone, midnight marked', () => {
    const ticks = timeTicks(now - DAY, now, 10, SHANGHAI);
    assert.equal(ticks.length, 8);
    assert.ok(ticks.every((tick) => tick.unit === 'hour'));
    assert.deepEqual(
      ticks.map((tick) => zonedParts(tick.t, SHANGHAI).hour),
      [15, 18, 21, 0, 3, 6, 9, 12],
    );
    assert.deepEqual(
      ticks.filter((tick) => tick.major).map((tick) => dayOf(tick.t, SHANGHAI)),
      ['2026-11-16'],
    );
  });

  it('ticks a month on Mondays, at midnight in the zone', () => {
    const ticks = timeTicks(now - 30 * DAY, now, 10, SHANGHAI);
    assert.ok(ticks.length >= 4 && ticks.length <= 5);
    for (const tick of ticks) {
      const parts = zonedParts(tick.t, SHANGHAI);
      assert.equal(tick.unit, 'day');
      assert.equal(parts.weekday, 1);
      assert.equal(parts.hour, 0);
    }
  });

  it('ticks a year on firsts of months, January marked', () => {
    const ticks = timeTicks(now - 365 * DAY, now, 12, SHANGHAI);
    assert.ok(ticks.every((tick) => tick.unit === 'month' && zonedParts(tick.t, SHANGHAI).day === 1));
    assert.deepEqual(
      ticks.filter((tick) => tick.major).map((tick) => dayOf(tick.t, SHANGHAI)),
      ['2026-01-01'],
    );
  });

  it('never returns more ticks than asked for, nor any outside the range', () => {
    for (const span of [DAY, 7 * DAY, 30 * DAY, 90 * DAY, 365 * DAY]) {
      for (const limit of [3, 6, 12]) {
        const ticks = timeTicks(now - span, now, limit, SHANGHAI);
        assert.ok(ticks.length <= limit + 1, `${span / DAY} days, limit ${limit}: ${ticks.length} ticks`);
        assert.ok(ticks.every((tick) => tick.t >= now - span && tick.t <= now));
      }
    }
    assert.deepEqual(timeTicks(now, now, 5, SHANGHAI), []);
  });
});

describe('spend buckets', () => {
  const now = Date.UTC(2026, 10, 16, 6, 8);
  const from = now - 9 * DAY;
  const days = Array.from({ length: 10 }, (_, index) => ({ date: dayOf(from + index * DAY, SHANGHAI), spend: index + 1 }));

  it('picks the finest size a day is wide enough for', () => {
    assert.equal(chooseBucket(22), 'day');
    assert.equal(chooseBucket(5), 'day');
    assert.equal(chooseBucket(2.7), 'week');
    assert.equal(chooseBucket(0.6), 'month');
  });

  it('keeps days as they are, cut short at both ends of the range', () => {
    const buckets = bucketDays(days, 'day', from, now, SHANGHAI);
    assert.equal(buckets.length, 10);
    const first = buckets[0]!;
    const last = buckets[9]!;
    assert.equal(first.start, from);
    assert.equal(first.partial, true);
    assert.equal(last.end, now);
    assert.equal(last.partial, true);
    assert.equal(last.current, true);
    const whole = buckets[4]!;
    assert.equal(whole.partial, false);
    assert.equal(whole.current, false);
    assert.equal(whole.end - whole.start, DAY);
    assert.equal(whole.start, dayStart(whole.firstDay, SHANGHAI));
  });

  it('adds days up into weeks that start on Monday', () => {
    const buckets = bucketDays(days, 'week', from, now, SHANGHAI);
    // Nov 7 (Sat) – Nov 16 (Mon): Sat–Sun, a whole week, and the Monday.
    assert.deepEqual(
      buckets.map((bucket) => [bucket.firstDay, bucket.lastDay, bucket.value, bucket.partial]),
      [
        ['2026-11-07', '2026-11-08', 3, true],
        ['2026-11-09', '2026-11-15', 42, false],
        ['2026-11-16', '2026-11-16', 10, true],
      ],
    );
    assert.equal(buckets[2]!.current, true);
  });

  it('adds days up into months', () => {
    const buckets = bucketDays(days, 'month', from, now, SHANGHAI);
    assert.equal(buckets.length, 1);
    assert.equal(buckets[0]!.value, 55);
    assert.equal(buckets[0]!.partial, true);
  });

  it('drops a period that ends before the range begins', () => {
    const late = dayStart('2026-11-12', SHANGHAI);
    const buckets = bucketDays(days, 'day', late, now, SHANGHAI);
    assert.equal(buckets[0]!.firstDay, '2026-11-12');
    assert.equal(buckets.length, 5);
  });
});
