// The Dates page's pure parts: a rule read out in words, the lunar
// calendar's names, the form's way to a rule and back, and how the list is
// grouped and summed up.
import assert from 'node:assert/strict';
import { beforeEach, describe, it } from 'node:test';
import type { RecurRule, ReminderView } from '../src/api/types';
import { lang } from '../src/i18n';
import { buildInput, buildRule, cleanLeads, draftOf, everyOf, lastDay, newDraft, parseAmount } from '../src/pages/dates/draft';
import {
  formatLongDay,
  groupByMonth,
  leadsText,
  lunarDayName,
  lunarMonthName,
  matchesTitle,
  monthTitle,
  ordinal,
  ordinalText,
  resumeDay,
  ruleText,
  snoozeUntil,
  soonestFirst,
  summarise,
} from '../src/pages/dates/words';

/** A date as the API returns it, with whatever the case at hand needs. */
function view(over: Partial<ReminderView> = {}): ReminderView {
  return {
    id: 'rem_1',
    title: 'Mom',
    kind: 'birthday',
    rule: { freq: 'yearly', month: 12, day: 24 },
    lead_days: [3, 0],
    amount: null,
    currency: '',
    group: '',
    notes: '',
    done_through: null,
    snoozed_until: null,
    enabled: true,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    next: '2026-12-24',
    days: 38,
    lunar: null,
    ordinal: 0,
    state: 'upcoming',
    handled: null,
    following: [],
    monthly_amount: null,
    ...over,
  };
}

beforeEach(() => {
  lang.value = 'en';
});

describe('the lunar calendar’s names', () => {
  it('names the days the traditional way in Chinese', () => {
    lang.value = 'zh';
    const days = Array.from({ length: 30 }, (_, index) => lunarDayName(index + 1));
    assert.deepEqual(days.slice(0, 11), ['初一', '初二', '初三', '初四', '初五', '初六', '初七', '初八', '初九', '初十', '十一']);
    assert.deepEqual(days.slice(18), ['十九', '二十', '廿一', '廿二', '廿三', '廿四', '廿五', '廿六', '廿七', '廿八', '廿九', '三十']);
    assert.equal(new Set(days).size, 30);
  });

  it('names the months the traditional way in Chinese', () => {
    lang.value = 'zh';
    const months = Array.from({ length: 12 }, (_, index) => lunarMonthName(index + 1));
    assert.deepEqual(months, ['正月', '二月', '三月', '四月', '五月', '六月', '七月', '八月', '九月', '十月', '冬月', '腊月']);
  });

  it('uses plain ordinals in English', () => {
    assert.equal(lunarDayName(9), '9th');
    assert.equal(lunarMonthName(10), '10th');
    assert.deepEqual([1, 2, 3, 4, 11, 12, 13, 21, 22, 23, 30].map(ordinal), ['1st', '2nd', '3rd', '4th', '11th', '12th', '13th', '21st', '22nd', '23rd', '30th']);
  });
});

describe('ruleText', () => {
  const cases: [RecurRule, string, string][] = [
    [{ freq: 'yearly', calendar: 'lunar', month: 10, day: 9 }, '每年农历十月初九', 'every year on the 9th day of the 10th lunar month'],
    [{ freq: 'yearly', calendar: 'lunar', month: 12, day: 30 }, '每年农历腊月三十', 'every year on the 30th day of the 12th lunar month'],
    [{ freq: 'yearly', calendar: 'lunar', month: 1, day: 1 }, '每年农历正月初一', 'every year on the 1st day of the 1st lunar month'],
    [{ freq: 'monthly', calendar: 'lunar', day: 15 }, '每月农历十五', 'every lunar month on the 15th day'],
    [{ freq: 'monthly', day: 19 }, '每月 19 日', 'every month on the 19th'],
    [{ freq: 'monthly', day: 15, interval: 3, date: '2026-12-15' }, '每 3 个月的 15 日', 'every 3 months on the 15th'],
    [{ freq: 'weekly', weekday: 5 }, '每周五', 'every Friday'],
    [{ freq: 'weekly', weekday: 7 }, '每周日', 'every Sunday'],
    [{ freq: 'weekly', weekday: 5, interval: 2, date: '2026-11-16' }, '每 2 周的周五', 'every 2 weeks on Friday'],
    // every N weeks may leave the weekday to its start date: 2026-11-16 is a Monday
    [{ freq: 'weekly', interval: 2, date: '2026-11-16' }, '每 2 周的周一', 'every 2 weeks on Monday'],
    [{ freq: 'yearly', month: 12, day: 24, since: 2021 }, '每年 12月24日', 'every year on Dec 24'],
    [{ freq: 'yearly', month: 2, day: 29 }, '每年 2月29日', 'every year on Feb 29'],
    [{ freq: 'yearly', month: 4, day: 6, interval: 2, date: '2026-01-01' }, '每 2 年的 4月6日', 'every 2 years on Apr 6'],
    [{ freq: 'once', date: '2031-05-01' }, '2031年5月1日（仅一次）', 'May 1, 2031 (once)'],
  ];

  it('reads a rule out in Chinese', () => {
    lang.value = 'zh';
    for (const [rule, zh] of cases) assert.equal(ruleText(rule), zh);
  });

  it('reads a rule out in English', () => {
    for (const [rule, , en] of cases) assert.equal(ruleText(rule), en);
  });

  it('treats an interval of one as no interval', () => {
    assert.equal(ruleText({ freq: 'monthly', day: 1, interval: 1 }), 'every month on the 1st');
  });
});

describe('leadsText', () => {
  it('says which days a date reminds on', () => {
    assert.equal(leadsText([3, 0]), '3 days before, on the day');
    assert.equal(leadsText([3, 1, 0]), '3 days and 1 day before, on the day');
    assert.equal(leadsText([30, 7, 1]), '30 days, 7 days and 1 day before');
    assert.equal(leadsText([0]), 'on the day');
    lang.value = 'zh';
    assert.equal(leadsText([3, 0]), '提前 3 天、当天');
    assert.equal(leadsText([3, 1, 0]), '提前 3 天、1 天、当天');
    assert.equal(leadsText([30, 7, 1]), '提前 30 天、7 天、1 天');
    assert.equal(leadsText([0]), '当天');
  });

  it('puts the widest lead first whatever order they come in', () => {
    assert.equal(leadsText([1, 7, 7]), '7 days and 1 day before');
  });
});

describe('ordinalText', () => {
  it('counts the years for birthdays and anniversaries only', () => {
    assert.equal(ordinalText('birthday', 30), '30th birthday');
    assert.equal(ordinalText('anniversary', 5), '5th anniversary');
    assert.equal(ordinalText('renewal', 3), null);
    assert.equal(ordinalText('birthday', 0), null);
    lang.value = 'zh';
    assert.equal(ordinalText('birthday', 30), '30 岁');
    assert.equal(ordinalText('anniversary', 5), '5 周年');
  });
});

describe('formatLongDay', () => {
  it('writes a day out in full', () => {
    assert.equal(formatLongDay('2026-12-24'), 'Thursday, December 24, 2026');
    lang.value = 'zh';
    assert.equal(formatLongDay('2026-12-24'), '2026年12月24日 周四');
  });
});

describe('the list', () => {
  const list = [
    view({ id: 'a', title: 'Mom', next: '2026-11-17', days: 1, state: 'soon' }),
    view({ id: 'b', title: 'iCloud+ 2TB', kind: 'renewal', next: '2026-11-19', days: 3, amount: 68, currency: 'CNY', monthly_amount: 68 }),
    view({ id: 'c', title: 'Claude Max', kind: 'renewal', next: '2026-11-25', days: 9, amount: 100, currency: 'USD', monthly_amount: 100 }),
    view({ id: 'd', title: 'Domain', kind: 'expiry', next: '2027-01-07', days: 52, amount: 68.5, currency: 'CNY', monthly_amount: 5.708333 }),
    view({ id: 'e', title: 'Old plan', kind: 'renewal', next: '2026-12-01', days: 15, amount: 9, currency: 'USD', monthly_amount: 9, enabled: false, state: 'disabled' }),
    view({ id: 'f', title: 'Passport', kind: 'expiry', next: null, days: null, state: 'past' }),
  ];

  it('groups by the month of the next occurrence, with nothing-ahead last', () => {
    const groups = groupByMonth(list);
    assert.deepEqual(
      groups.map((group) => [group.key, group.items.map((item) => item.id).join('')]),
      [
        ['2026-11', 'abc'],
        ['2026-12', 'e'],
        ['2027-01', 'd'],
        ['', 'f'],
      ],
    );
  });

  it('names this year’s months plainly and later ones with their year', () => {
    assert.equal(monthTitle('2026-11', 2026), 'November');
    assert.equal(monthTitle('2027-01', 2026), 'January 2027');
    assert.equal(monthTitle('', 2026), 'Nothing ahead');
    lang.value = 'zh';
    assert.equal(monthTitle('2026-11', 2026), '11月');
    assert.equal(monthTitle('2027-01', 2026), '2027年1月');
    assert.equal(monthTitle('', 2026), '没有下一次');
  });

  it('counts what is near and sums amounts per currency, never across', () => {
    const summary = summarise(list);
    assert.equal(summary.week, 2);
    assert.equal(summary.month, 3);
    assert.deepEqual(
      summary.monthly.map((each) => [each.currency, Math.round(each.amount * 100) / 100]),
      [
        ['CNY', 73.71],
        ['USD', 100],
      ],
    );
  });

  it('leaves out what is turned off', () => {
    assert.deepEqual(summarise([list[4]!]), { week: 0, month: 0, monthly: [] });
  });

  it('searches titles whatever the case', () => {
    assert.equal(matchesTitle(list[1]!, ' icloud '), true);
    assert.equal(matchesTitle(list[1]!, 'claude'), false);
    assert.equal(matchesTitle(list[1]!, ''), true);
  });

  it('keeps the soonest first and nothing-ahead last', () => {
    const shuffled = [list[5]!, list[3]!, list[0]!, list[2]!];
    assert.deepEqual(
      soonestFirst(shuffled).map((item) => item.id),
      ['a', 'c', 'd', 'f'],
    );
  });
});

describe('snoozing', () => {
  it('converts between the last quiet day and the day reminders come back', () => {
    assert.equal(resumeDay('2026-11-18'), '2026-11-19');
    assert.equal(snoozeUntil('2026-11-19'), '2026-11-18');
    assert.equal(resumeDay('2026-12-31'), '2027-01-01');
    assert.equal(snoozeUntil('2028-03-01'), '2028-02-29');
  });
});

describe('the form', () => {
  it('starts a new date from its kind’s defaults', () => {
    assert.deepEqual([newDraft('birthday').freq, newDraft('birthday').leads], ['yearly', [3, 0]]);
    assert.deepEqual([newDraft('renewal').freq, newDraft('renewal').leads], ['monthly', [3, 0]]);
    assert.deepEqual([newDraft('expiry').freq, newDraft('expiry').leads], ['once', [30, 7, 1]]);
    assert.equal(newDraft('birthday', 'lunar').calendar, 'lunar');
  });

  it('builds a rule with only the fields its frequency uses', () => {
    const draft = { ...newDraft('birthday'), date: '2030-05-01', start: '2026-12-15', weekday: '5', month: '10', day: '9', every: '3', since: '1962' };
    assert.deepEqual(buildRule({ ...draft, freq: 'once' }), { freq: 'once', date: '2030-05-01' });
    assert.deepEqual(buildRule({ ...draft, freq: 'weekly' }), { freq: 'weekly', date: '2026-12-15', weekday: 5, interval: 3 });
    assert.deepEqual(buildRule({ ...draft, freq: 'monthly' }), { freq: 'monthly', date: '2026-12-15', day: 9, interval: 3 });
    assert.deepEqual(buildRule({ ...draft, freq: 'yearly' }), { freq: 'yearly', date: '2026-12-15', month: 10, day: 9, interval: 3, since: 1962 });
    assert.deepEqual(buildRule({ ...draft, freq: 'yearly', every: '' }), { freq: 'yearly', month: 10, day: 9, since: 1962 });
  });

  it('never sends an interval with a lunar rule', () => {
    const draft = { ...newDraft('birthday', 'lunar'), start: '2026-12-15', month: '10', day: '9', every: '3' };
    assert.deepEqual(buildRule(draft), { freq: 'yearly', calendar: 'lunar', month: 10, day: 9 });
    assert.equal(everyOf(draft), 1);
    // the calendar only means something for a monthly or a yearly rule
    assert.deepEqual(buildRule({ ...draft, freq: 'weekly', weekday: '1', every: '' }), { freq: 'weekly', weekday: 1 });
  });

  it('leaves out what has not been filled in, for the server to ask for', () => {
    assert.deepEqual(buildRule(newDraft('expiry')), { freq: 'once' });
    assert.deepEqual(buildRule(newDraft('birthday')), { freq: 'yearly' });
    assert.deepEqual(buildRule({ ...newDraft('renewal'), every: '2' }), { freq: 'monthly', interval: 2 });
  });

  it('reads an existing date back into the same rule', () => {
    const rules: RecurRule[] = [
      { freq: 'yearly', calendar: 'lunar', month: 10, day: 9 },
      { freq: 'monthly', day: 19 },
      { freq: 'yearly', month: 12, day: 10, since: 1996 },
      { freq: 'once', date: '2030-05-01' },
      { freq: 'monthly', date: '2026-12-15', day: 15, interval: 3 },
      { freq: 'weekly', weekday: 5 },
      { freq: 'monthly', calendar: 'lunar', day: 30 },
    ];
    for (const rule of rules) assert.deepEqual(buildRule(draftOf(view({ rule }))), rule);
  });

  it('offers only the days a month can have', () => {
    const draft = newDraft('birthday');
    assert.equal(lastDay({ ...draft, month: '2' }), 29);
    assert.equal(lastDay({ ...draft, month: '4' }), 30);
    assert.equal(lastDay({ ...draft, month: '' }), 31);
    assert.equal(lastDay({ ...draft, freq: 'monthly', month: '2' }), 31);
    assert.equal(lastDay({ ...draft, calendar: 'lunar', month: '1' }), 30);
  });

  it('reads an amount as it was typed', () => {
    assert.equal(parseAmount(''), null);
    assert.equal(parseAmount(' 68 '), 68);
    assert.equal(parseAmount('1,200.5'), 1200.5);
    assert.equal(parseAmount('abc'), 'invalid');
    assert.equal(parseAmount('-1'), 'invalid');
  });

  it('sends everything the form owns for a new date', () => {
    const draft = { ...newDraft('renewal'), title: ' iCloud+ ', day: '19', amount: '68', currency: 'cny', leads: [0, 3, 3] };
    assert.deepEqual(buildInput(draft), {
      title: 'iCloud+',
      kind: 'renewal',
      rule: { freq: 'monthly', day: 19 },
      lead_days: [3, 0],
      amount: 68,
      currency: 'CNY',
      group: '',
      notes: '',
    });
    assert.deepEqual(cleanLeads([0, 30, 7, 30]), [30, 7, 0]);
  });

  it('sends only what changed for an edit, and the rule whole', () => {
    const original = draftOf(view({ title: 'Mom', rule: { freq: 'yearly', calendar: 'lunar', month: 10, day: 9 }, lead_days: [3, 1, 0] }));
    assert.deepEqual(buildInput(original, original), {});
    assert.deepEqual(buildInput({ ...original, title: 'Mum' }, original), { title: 'Mum' });
    assert.deepEqual(buildInput({ ...original, day: '10', since: '1962' }, original), { rule: { freq: 'yearly', calendar: 'lunar', month: 10, day: 10, since: 1962 } });
  });

  it('clears an amount by sending zero', () => {
    const original = draftOf(view({ kind: 'renewal', amount: 68, currency: 'CNY' }));
    assert.deepEqual(buildInput({ ...original, amount: '' }, original), { amount: 0 });
  });
});
