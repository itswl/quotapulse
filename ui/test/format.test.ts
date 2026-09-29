/* Implementation note. */

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import type { Runway } from '../src/api/types.js';
import {
  cycleLabel,
  escapeHTML,
  formatBalance,
  formatCurrency,
  formatDate,
  formatRunway,
  getBalancePercentage,
  getBalanceStatus,
  pluralize,
  getRelativeTime,
  renewalUrgency,
  typeLabel,
} from '../src/format.js';

/* Implementation note. */
function runway(overrides: Partial<Runway>): Runway {
  return {
    project_id: 'id',
    project_name: 'p',
    provider: 'deepseek',
    balance_type: 'balance',
    current_balance: 100,
    window_days: 7,
    data_points: 168,
    span_hours: 167,
    consumed: 0,
    topped_up: 0,
    burn_per_day: null,
    runway_days: null,
    depletion_date: null,
    confidence: 'high',
    daily: [],
    today_consumed: null,
    baseline_consumed: null,
    spike_ratio: null,
    ...overrides,
  };
}

describe('formatCurrency', () => {
  it('保留两位小数，并按千位分组（与余额一致）', () => {
    assert.equal(formatCurrency(430.371), '430.37');
    assert.equal(formatCurrency(0), '0.00');
    assert.equal(formatCurrency(1420), '1,420.00');
  });

  it('拿不到数字时给 - 而不是 0', () => {
    assert.equal(formatCurrency(null), '-');
    assert.equal(formatCurrency(undefined), '-');
    assert.equal(formatCurrency('abc'), '-');
  });
});

describe('formatBalance', () => {
  it('Quota型带百分号单位', () => {
    assert.equal(formatBalance(42.35, 'quota'), '42.4<span class="unit">%</span>');
  });

  it('其余类型带千分位并保留两位', () => {
    assert.equal(formatBalance(1234.5, 'balance'), '1,234.50');
    assert.equal(formatBalance(1234.5, 'credits'), '1,234.50');
  });

  it('非数字给 -', () => {
    assert.equal(formatBalance(null, 'balance'), '-');
  });
});

describe('typeLabel', () => {
  it('已知类型翻成中文', () => {
    assert.equal(typeLabel('balance'), 'Balance');
    assert.equal(typeLabel('credits'), 'Credits');
    assert.equal(typeLabel('quota'), 'Quota');
  });

  it('Unknown或空类型退回「Balance」', () => {
    assert.equal(typeLabel(''), 'Balance');
    assert.equal(typeLabel(null), 'Balance');
    assert.equal(typeLabel('unknown'), 'unknown');
  });
});

describe('formatRunway', () => {
  it('没有跑道数据时说「Accumulating data」，绝不能显示成 0 天', () => {
    const result = formatRunway(null);
    assert.equal(result.text, 'Accumulating data');
    assert.equal(result.level, 'unknown');
    assert.match(result.hint, /Estimates appear/);
  });

  it('置信度 none 等同于没有数据', () => {
    assert.equal(formatRunway(runway({ confidence: 'none', burn_per_day: 10, runway_days: 5 })).text, 'Accumulating data');
  });

  it('说明为什么没有估算：配额不估算，没开数据库要提示开启，而不是永远「Accumulating data」', () => {
    const quota = formatRunway(null, { balanceType: 'quota', database: true });
    assert.equal(quota.text, 'Not estimated');
    assert.match(quota.hint, /Quota plans/);

    const noDatabase = formatRunway(null, { balanceType: 'balance', database: false });
    assert.equal(noDatabase.text, 'Needs history');
    assert.match(noDatabase.hint, /ENABLE_DATABASE=true/);

    // With the database on, a missing estimate really is a history still filling up.
    assert.equal(formatRunway(null, { balanceType: 'balance', database: true }).text, 'Accumulating data');
  });

  it('没有消耗时说「No spending」，不是「用不完」也不是 0', () => {
    const result = formatRunway(runway({ burn_per_day: 0, window_days: 7 }));
    assert.equal(result.text, 'No spending');
    assert.equal(result.level, 'normal');
    assert.equal(result.hint, 'Balance did not decrease in the last 7 days');
  });

  it('有Daily spend但算不出天数时给破折号', () => {
    const result = formatRunway(runway({ burn_per_day: 12, runway_days: null }));
    assert.equal(result.text, '—');
    assert.equal(result.level, 'unknown');
  });

  it('不足一天单独说明', () => {
    assert.equal(formatRunway(runway({ burn_per_day: 100, runway_days: 0.4 })).text, 'Less than 1 day');
  });

  it('十天以内保留一位小数，之后取整', () => {
    assert.equal(formatRunway(runway({ burn_per_day: 62.5, runway_days: 6.89 })).text, '6.9 days');
    assert.equal(formatRunway(runway({ burn_per_day: 1, runway_days: 12.4 })).text, '12 days');
  });

  it('超过一年不给具体数字', () => {
    assert.equal(formatRunway(runway({ burn_per_day: 0.01, runway_days: 400 })).text, 'More than 1 year');
  });

  it('颜色档位：3 天内红、7 天内黄、再往上Healthy', () => {
    assert.equal(formatRunway(runway({ burn_per_day: 1, runway_days: 3 })).level, 'danger');
    assert.equal(formatRunway(runway({ burn_per_day: 1, runway_days: 3.1 })).level, 'warning');
    assert.equal(formatRunway(runway({ burn_per_day: 1, runway_days: 7 })).level, 'warning');
    assert.equal(formatRunway(runway({ burn_per_day: 1, runway_days: 7.1 })).level, 'normal');
  });

  it('有预计耗尽日期时提示里带上日期和日均', () => {
    const result = formatRunway(runway({ burn_per_day: 62.5, runway_days: 6.89, depletion_date: '2026-09-21' }));
    assert.equal(result.hint, 'At an average daily spend of 62.50, estimated to deplete around 2026-09-21');
  });
});

describe('getBalanceStatus', () => {
  it('低于阈值即为红色，与卡片的 Alert 状态一致；接近阈值（1.5 倍以内）为黄色', () => {
    assert.equal(getBalanceStatus(100, 50), 'normal'); // 2x
    assert.equal(getBalanceStatus(75, 50), 'normal'); // exactly 1.5x
    assert.equal(getBalanceStatus(74, 50), 'warning'); // closing in
    assert.equal(getBalanceStatus(50, 50), 'warning'); // at the threshold, not below
    assert.equal(getBalanceStatus(49.99, 50), 'danger');
    // 96.50 against 100 is alerting; it used to get a nearly full green bar.
    assert.equal(getBalanceStatus(96.5, 100), 'danger');
    assert.equal(getBalanceStatus(9, 50), 'danger');
  });

  it('阈值为 0（不Alert）时一律算Healthy', () => {
    assert.equal(getBalancePercentage(0, 0), 100);
    assert.equal(getBalanceStatus(0, 0), 'normal');
  });
});

describe('escapeHTML', () => {
  it('Project名里的尖括号和引号不会变成标签', () => {
    assert.equal(escapeHTML('<img src=x onerror="alert(1)">'), '&lt;img src=x onerror=&quot;alert(1)&quot;&gt;');
    assert.equal(escapeHTML("it's"), 'it&#39;s');
  });

  it('null / undefined 变成空串', () => {
    assert.equal(escapeHTML(null), '');
    assert.equal(escapeHTML(undefined), '');
  });
});

describe('getRelativeTime', () => {
  const now = new Date('2026-09-14T12:00:00Z');

  it('按间隔给出不同粒度', () => {
    assert.equal(getRelativeTime('2026-09-14T11:59:30Z', now), 'Just now');
    assert.equal(getRelativeTime('2026-09-14T11:30:00Z', now), '30 min ago');
    assert.equal(getRelativeTime('2026-09-14T09:00:00Z', now), '3 hr ago');
    assert.equal(getRelativeTime('2026-09-12T12:00:00Z', now), '2 days ago');
  });

  it('超过一周退回绝对时间，「8天前」没有信息量', () => {
    assert.ok(!getRelativeTime('2026-09-01T12:00:00Z', now).endsWith('天前'));
  });

  it('没有时间戳时说「Unknown」', () => {
    assert.equal(getRelativeTime(null, now), 'Unknown');
  });
});

describe('Subscription展示', () => {
  it('周期翻成中文', () => {
    assert.equal(cycleLabel('monthly'), 'Monthly');
    assert.equal(cycleLabel('yearly'), 'Yearly');
    assert.equal(cycleLabel('weekly'), 'Weekly');
  });

  it('续费紧迫度：提醒窗口内红，14 天内黄，已续费和周付不标黄', () => {
    const sub = (days: number, extra: Partial<Parameters<typeof renewalUrgency>[0]> = {}) => ({
      days_until_renewal: days, need_alert: false, already_renewed: false, cycle_type: 'monthly' as const, ...extra,
    });
    assert.equal(renewalUrgency(sub(3, { need_alert: true })), 'danger');
    assert.equal(renewalUrgency(sub(10, { need_alert: true })), 'danger');
    assert.equal(renewalUrgency(sub(5)), 'warning');
    assert.equal(renewalUrgency(sub(14)), 'warning');
    assert.equal(renewalUrgency(sub(15)), '');
    // A renewed item is settled even when the next renewal is close.
    assert.equal(renewalUrgency(sub(3, { already_renewed: true })), '');
    // Weekly renewals are always under 14 days out; only the reminder window counts.
    assert.equal(renewalUrgency(sub(4, { cycle_type: 'weekly' })), '');
    assert.equal(renewalUrgency(sub(1, { cycle_type: 'weekly', need_alert: true })), 'danger');
  });
});

describe('pluralize', () => {
  it('恰好 1 时用单数，其余用复数，界面上不再出现「1 days」', () => {
    assert.equal(pluralize(1, 'day'), '1 day');
    assert.equal(pluralize(0, 'day'), '0 days');
    assert.equal(pluralize(7, 'day'), '7 days');
    assert.equal(pluralize(1, 'match', 'matches'), '1 match');
    assert.equal(getRelativeTime(new Date(Date.now() - 26 * 3600_000).toISOString()), '1 day ago');
  });
});

describe('formatDate', () => {
  it('本地时间按 ISO 顺序显示，与页面上其他日期一致；无效输入给 -', () => {
    assert.equal(formatDate(new Date(2026, 8, 28, 6, 5).toISOString()), '2026-09-28 06:05');
    assert.equal(formatDate(null), '-');
    assert.equal(formatDate('not a date'), '-');
  });
});
