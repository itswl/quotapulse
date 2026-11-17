// How numbers, durations and dates are written. The rules mirror the
// server's (internal/i18n), so these cases are the same sentences its tests use.
import assert from 'node:assert/strict';
import { beforeEach, describe, it } from 'node:test';
import {
  addDays,
  amountParts,
  compactNumber,
  dayOf,
  dayStart,
  formatAgo,
  formatAmount,
  formatClock,
  formatDay,
  formatDays,
  formatIn,
  formatMonth,
  formatNumber,
  formatPercent,
  formatSpan,
  formatStamp,
  formatUntil,
  isCurrency,
  weekdayOf,
  zonedParts,
  zonedTime,
} from '../src/format';
import { lang } from '../src/i18n';

const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;
const SHANGHAI = 'Asia/Shanghai';
const NEW_YORK = 'America/New_York';

beforeEach(() => {
  lang.value = 'en';
});

describe('numbers', () => {
  it('groups thousands and trims trailing zeros unless told to keep them', () => {
    assert.equal(formatNumber(1234.5), '1,234.5');
    assert.equal(formatNumber(1234.5, 2, true), '1,234.50');
    assert.equal(formatNumber(923, 2), '923');
    assert.equal(formatNumber(0.455, 1), '0.5');
  });

  it('never writes a negative zero, and writes a dash for what is not a number', () => {
    assert.equal(formatNumber(-0.001, 2), '0');
    assert.equal(formatNumber(-12.5, 2), '-12.5');
    assert.equal(formatNumber(Number.NaN), '—');
    assert.equal(formatNumber(Infinity), '—');
  });

  it('writes money with two decimals and the unit after', () => {
    assert.equal(formatAmount(1234.56, 'CNY'), '1,234.56 CNY');
    assert.equal(formatAmount(6.2, 'USD'), '6.20 USD');
    assert.deepEqual(amountParts(6.2, 'USD'), { number: '6.20', unit: 'USD', tight: false });
  });

  it('writes a percentage with at most one decimal and no space', () => {
    assert.equal(formatAmount(38, '%'), '38%');
    assert.equal(formatAmount(42.44, '%'), '42.4%');
    assert.equal(formatPercent(92.3), '92.3%');
    assert.equal(amountParts(38, '%').tight, true);
  });

  it('writes other units with at most two decimals, trailing zeros dropped', () => {
    assert.equal(formatAmount(923, 'calls'), '923 calls');
    assert.equal(formatAmount(12.5, 'credits'), '12.5 credits');
    assert.equal(formatAmount(7, ''), '7');
  });

  it('knows a currency code from a label', () => {
    assert.equal(isCurrency('USD'), true);
    assert.equal(isCurrency('usd'), false);
    assert.equal(isCurrency('calls'), false);
    assert.equal(isCurrency('%'), false);
  });

  it('shortens large numbers for an axis, in the way each language counts', () => {
    assert.equal(compactNumber(0), '0');
    assert.equal(compactNumber(2.5), '2.5');
    assert.equal(compactNumber(9999), '9,999');
    assert.equal(compactNumber(12_500), '12.5K');
    assert.equal(compactNumber(3_000_000), '3M');
    lang.value = 'zh';
    assert.equal(compactNumber(12_500), '1.25万');
    assert.equal(compactNumber(300_000_000), '3亿');
  });
});

describe('durations', () => {
  it('names a span by its two largest units', () => {
    assert.equal(formatSpan(20_000), 'under a minute');
    assert.equal(formatSpan(45 * MIN), '45 min');
    assert.equal(formatSpan(100 * MIN), '1 h 40 min');
    assert.equal(formatSpan(2 * HOUR), '2 h');
    assert.equal(formatSpan(13 * HOUR + 20 * MIN), '13 h');
    assert.equal(formatSpan(3 * DAY), '3 days');
    assert.equal(formatSpan(-45 * MIN), '45 min');
  });

  it('does the same in Chinese', () => {
    lang.value = 'zh';
    assert.equal(formatSpan(20_000), '不到 1 分钟');
    assert.equal(formatSpan(100 * MIN), '1 小时 40 分钟');
    assert.equal(formatSpan(3 * DAY), '3 天');
    assert.equal(formatIn(100 * MIN, 0), '1 小时 40 分钟后');
  });

  it('writes days with one decimal under ten and none above', () => {
    assert.equal(formatDays(5.847), '5.8 days');
    assert.equal(formatDays(13.6), '14 days');
    assert.equal(formatDays(1), '1 day');
    assert.equal(formatDays(400), 'over a year');
    assert.equal(formatDays(800), 'over 2 years');
  });

  it('says when a date is, relative to today', () => {
    assert.deepEqual([0, 1, 2, 5, -1, -3].map(formatUntil), ['today', 'tomorrow', 'in 2 days', 'in 5 days', 'yesterday', '3 days ago']);
    lang.value = 'zh';
    assert.deepEqual([0, 1, 2, 5, -1, -3].map(formatUntil), ['今天', '明天', '后天', '5 天后', '昨天', '3 天前']);
  });

  it('says how long ago something was, and falls back to the date', () => {
    const now = Date.UTC(2026, 10, 16, 6, 0);
    assert.equal(formatAgo(now - 10_000, now, SHANGHAI), 'just now');
    assert.equal(formatAgo(now - 5 * MIN, now, SHANGHAI), '5 min ago');
    assert.equal(formatAgo(now - 3 * DAY, now, SHANGHAI), '3 days ago');
    assert.equal(formatAgo(now - 40 * DAY, now, SHANGHAI), 'Oct 7');
    lang.value = 'zh';
    assert.equal(formatAgo(now - 5 * MIN, now, SHANGHAI), '5 分钟前');
  });
});

describe('the time zone', () => {
  it('reads an instant on the wall clock of a zone', () => {
    const parts = zonedParts(Date.UTC(2026, 10, 16, 16, 30), SHANGHAI);
    assert.deepEqual(parts, { year: 2026, month: 11, day: 17, hour: 0, minute: 30, second: 0, weekday: 2 });
  });

  it('finds the instant of a wall-clock time, across a clock change', () => {
    assert.equal(zonedTime(2026, 11, 17, 0, 0, SHANGHAI), Date.UTC(2026, 10, 16, 16));
    // New York: winter is UTC-5, summer UTC-4.
    assert.equal(zonedTime(2026, 1, 15, 12, 0, NEW_YORK), Date.UTC(2026, 0, 15, 17));
    assert.equal(zonedTime(2026, 7, 15, 12, 0, NEW_YORK), Date.UTC(2026, 6, 15, 16));
    // The day the clocks go forward is 23 hours long.
    assert.equal(dayStart('2026-03-09', NEW_YORK) - dayStart('2026-03-08', NEW_YORK), 23 * HOUR);
  });

  it('puts an instant on the calendar day it has in the zone, not in UTC', () => {
    const lateEvening = '2026-11-16T16:30:00Z';
    assert.equal(dayOf(lateEvening, SHANGHAI), '2026-11-17');
    assert.equal(dayOf(lateEvening, 'UTC'), '2026-11-16');
    assert.equal(dayOf(lateEvening, NEW_YORK), '2026-11-16');
  });
});

describe('calendar days', () => {
  it('steps days across months and years', () => {
    assert.equal(addDays('2026-12-31', 1), '2027-01-01');
    assert.equal(addDays('2028-03-01', -1), '2028-02-29');
    assert.equal(weekdayOf('2026-11-16'), 1);
    assert.equal(weekdayOf('2026-11-22'), 7);
  });

  it('writes a day the way the server does', () => {
    assert.equal(formatDay('2026-12-24'), 'Dec 24');
    assert.equal(formatDay('2026-12-24', { weekday: true }), 'Thu, Dec 24');
    assert.equal(formatDay('2027-12-24', { weekday: true, year: 2026 }), 'Fri, Dec 24, 2027');
    assert.equal(formatDay('2026-12-24', { year: 2026 }), 'Dec 24');
    assert.equal(formatMonth('2026-11'), 'November 2026');
    lang.value = 'zh';
    assert.equal(formatDay('2026-12-24', { weekday: true }), '12月24日 周四');
    assert.equal(formatDay('2027-12-24', { weekday: true, year: 2026 }), '2027年12月24日 周五');
    assert.equal(formatMonth('2026-11'), '2026年11月');
  });

  it('writes a moment with its date only when the date is not today', () => {
    const now = Date.UTC(2026, 10, 16, 6, 0); // 14:00 in Shanghai
    assert.equal(formatClock('2026-11-16T07:45:00Z', now, SHANGHAI), '15:45');
    assert.equal(formatClock('2026-11-22T02:26:00Z', now, SHANGHAI), 'Nov 22 10:26');
    assert.equal(formatClock('2027-01-29T00:58:00Z', now, SHANGHAI), 'Jan 29, 2027 08:58');
    assert.equal(formatStamp('2026-11-16T07:45:00Z', now, SHANGHAI), 'Nov 16 15:45');
  });
});
