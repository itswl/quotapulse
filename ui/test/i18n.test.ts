/* The interface language: dictionaries, lookups, detection, and the static markup's keys. */

import './stub-dom.js';

import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { describe, it } from 'node:test';
import { fileURLToPath } from 'node:url';

import { en, type Message } from '../src/i18n/en.js';
import { zhCN } from '../src/i18n/zh-CN.js';
import { detectLocale, getLocale, setLocale, t, translateDocument, LOCALE_STORAGE_KEY } from '../src/i18n/index.js';
import { cycleLabel, formatRunway, formatServerCadence, getRelativeTime, typeLabel } from '../src/format.js';
import { renderProjectCard } from '../src/ui/projects.js';
import { renderSubscriptionCard } from '../src/ui/subscriptions.js';
import { renderAlertCard } from '../src/managers/email-manager.js';
import type { CheckResult, Features, SubscriptionResult } from '../src/api/types.js';
import { createStubElement } from './stub-dom.js';

const ALL_ON: Features = { subscriptions: true, dynamic_config: true, history: true, email_scan: true };

/* Run `fn` in Chinese and always come back to English, so the other test files stay English. */
function inChinese(fn: () => void): void {
  setLocale('zh-CN');
  try {
    fn();
  } finally {
    setLocale('en');
  }
}

function placeholders(message: Message): string[] {
  const forms = typeof message === 'string' ? [message] : [message.one, message.other];
  return [...new Set(forms.flatMap((form) => [...form.matchAll(/\{(\w+)\}/g)].map((m) => m[1] ?? '')))].sort();
}

describe('字典', () => {
  it('两种语言的键完全一致，占位符也一一对应', () => {
    const enKeys = Object.keys(en).sort();
    const zhKeys = Object.keys(zhCN).sort();
    assert.deepEqual(zhKeys, enKeys);
    for (const key of enKeys as Array<keyof typeof en>) {
      assert.deepEqual(placeholders(zhCN[key]), placeholders(en[key]), `placeholders differ for ${key}`);
    }
  });

  it('英文单复数都带 {count}；中文没有单复数', () => {
    for (const [key, message] of Object.entries(en)) {
      if (typeof message !== 'string') {
        assert.match(message.one, /\{count\}|^[a-z]+$/, `${key}.one`);
        assert.match(message.other, /\{count\}|^[a-z]+$/, `${key}.other`);
      }
    }
    for (const [key, message] of Object.entries(zhCN)) {
      assert.equal(typeof message, 'string', `${key} should be a plain string in zh-CN`);
    }
  });

  it('中文里不该残留英文句子（允许产品名、环境变量和占位符）', () => {
    const latinWords = /\b(?:the|and|for|with|not|is|are|to)\b/i;
    for (const [key, message] of Object.entries(zhCN)) {
      if (key.startsWith('locale.')) continue; // the toggle names the other language on purpose
      assert.ok(!latinWords.test(String(message)), `${key} looks untranslated: ${String(message)}`);
    }
  });
});

describe('t()', () => {
  it('填充占位符，英文按 count 选单复数，中文直接带数字', () => {
    assert.equal(t('count.days', { count: 1 }), '1 day');
    assert.equal(t('count.days', { count: 0 }), '0 days');
    assert.equal(t('count.days', { count: 7 }), '7 days');
    assert.equal(t('count.days', { count: '6.9' }), '6.9 days');
    assert.equal(t('stats.next_renewal', { name: 'Netflix', date: '2026-10-15' }), 'Next: Netflix on 2026-10-15');
    inChinese(() => {
      assert.equal(t('count.days', { count: 1 }), '1 天');
      assert.equal(t('count.days', { count: '6.9' }), '6.9 天');
      assert.equal(t('common.cancel'), '取消');
    });
  });

  it('缺少参数时保留占位符，而不是悄悄变成空白', () => {
    assert.equal(t('trend.title_of'), 'Balance trend - {name}');
  });

  it('默认英文；setLocale 不持久化，只有明确选择才写入存储', () => {
    assert.equal(getLocale(), 'en');
    setLocale('zh-CN');
    assert.equal(getLocale(), 'zh-CN');
    assert.equal(localStorage.getItem(LOCALE_STORAGE_KEY), null);
    setLocale('en', { persist: true });
    assert.equal(localStorage.getItem(LOCALE_STORAGE_KEY), 'en');
    localStorage.removeItem(LOCALE_STORAGE_KEY);
  });
});

describe('detectLocale', () => {
  const win = window as unknown as { navigator?: { language: string } };

  it('存储的选择优先；否则跟随浏览器语言，任何 zh 变体都落到简体中文；其余英文', () => {
    try {
      assert.equal(detectLocale(), 'en'); // the stub has no navigator
      win.navigator = { language: 'zh-CN' };
      assert.equal(detectLocale(), 'zh-CN');
      win.navigator = { language: 'zh-TW' };
      assert.equal(detectLocale(), 'zh-CN');
      win.navigator = { language: 'zh' };
      assert.equal(detectLocale(), 'zh-CN');
      win.navigator = { language: 'en-US' };
      assert.equal(detectLocale(), 'en');
      win.navigator = { language: 'ja-JP' };
      assert.equal(detectLocale(), 'en');
      win.navigator = { language: 'zh-CN' };
      localStorage.setItem(LOCALE_STORAGE_KEY, 'en');
      assert.equal(detectLocale(), 'en');
      localStorage.setItem(LOCALE_STORAGE_KEY, 'fr');
      assert.equal(detectLocale(), 'zh-CN', 'an unknown stored value is ignored');
    } finally {
      delete win.navigator;
      localStorage.removeItem(LOCALE_STORAGE_KEY);
    }
  });
});

describe('渲染模块跟随语言', () => {
  const project: CheckResult = {
    project: 'deepseek', owner_project: null, provider: 'deepseek', type: 'balance', success: true,
    credits: 430.37, threshold: 50, need_alarm: true, alarm_sent: false, error: null, cached: false,
  };
  const subscription: SubscriptionResult = {
    name: 'Netflix', owner_project: null, renewal_day: 15, cycle_type: 'monthly', days_until_renewal: 1,
    next_renewal_date: '2026-10-15', need_alert: true, alert_sent: true, alert_state: 'sent', amount: 99,
    already_renewed: false, last_renewed_date: null,
  };

  it('项目卡片、订阅卡片、邮件卡片和格式化函数都切到中文，切回后恢复英文', () => {
    inChinese(() => {
      const card = renderProjectCard(project, ALL_ON);
      assert.match(card, /告警阈值/);
      assert.match(card, /status-text alert">告警</);
      assert.match(card, /无所属项目/);
      assert.match(card, /查看趋势/);
      assert.match(card, /title="编辑项目"/);
      assert.ok(!/Alert threshold|Healthy|View trend/.test(card), card);

      const sub = renderSubscriptionCard(subscription);
      assert.match(sub, />已通知</);
      assert.match(sub, /每月/);
      assert.match(sub, /<span class="unit"> 天<\/span>/);

      const mail = renderAlertCard(
        { mailbox: 'Ops', sender: 'a@b.c', subject: '', date: '2026-09-30', keywords: [], service_name: null, amount: null, alert_sent: true },
        { canMute: true },
      );
      assert.match(mail, /（无主题）/);
      assert.match(mail, />误报</);

      assert.equal(typeLabel('credits'), '额度');
      assert.equal(cycleLabel('lunar_yearly'), '每年（农历）');
      assert.equal(formatRunway(null).text, '积累数据中');
      assert.equal(formatRunway({ ...runway(), burn_per_day: 62.5, runway_days: 6.89 }).text, '6.9 天');
      assert.equal(getRelativeTime(new Date(Date.now() - 3 * 3600_000).toISOString()), '3 小时前');
      assert.equal(
        formatServerCadence({ schedule: 'Every 3600 seconds', next_run: new Date(Date.now() + 12 * 60_000).toISOString() }),
        '服务端检查：每 1 小时 · 12 分钟后再次检查',
      );
    });
    assert.match(renderProjectCard(project, ALL_ON), /Alert threshold/);
    assert.equal(formatRunway(null).text, 'Accumulating data');
  });

  function runway() {
    return {
      project_id: 'id', project_name: 'deepseek', provider: 'deepseek', balance_type: 'balance' as const,
      current_balance: 430.37, window_days: 7, data_points: 168, span_hours: 167, consumed: 437.5, topped_up: 0,
      burn_per_day: null, runway_days: null, depletion_date: null, confidence: 'high' as const, daily: [],
      today_consumed: null, baseline_consumed: null, spike_ratio: null,
    };
  }
});

describe('translateDocument', () => {
  it('按 data-i18n* 填文本和属性，data-i18n-count 选单复数', () => {
    const text = createStubElement('span');
    text.dataset['i18n'] = 'common.cancel';
    const option = createStubElement('option');
    option.dataset['i18n'] = 'count.last_days';
    option.dataset['i18nCount'] = '3';
    const attrs: Record<string, string> = {};
    const input = Object.assign(createStubElement('input'), {
      dataset: { i18nPlaceholder: 'projects.search', i18nAriaLabel: 'projects.search' },
      setAttribute: (name: string, value: string) => {
        attrs[name] = value;
      },
    });
    const root = { querySelectorAll: () => [text, option, input] } as unknown as ParentNode;

    translateDocument(root);
    assert.equal(text.textContent, 'Cancel');
    assert.equal(option.textContent, 'Last 3 days');
    assert.deepEqual(attrs, { placeholder: 'Search projects', 'aria-label': 'Search projects' });

    inChinese(() => translateDocument(root));
    assert.equal(text.textContent, '取消');
    assert.equal(option.textContent, '最近 3 天');
    assert.equal(attrs['placeholder'], '搜索项目');
  });

  it('index.html 里每个 data-i18n* 键都在字典里，带 count 的键是单复数消息', () => {
    const html = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '../index.html'), 'utf8');
    const used = [...html.matchAll(/data-i18n(?:-title|-aria-label|-placeholder)?="([^"]+)"/g)].map((m) => m[1] ?? '');
    assert.ok(used.length > 100, `only ${used.length} keys found in index.html`);
    const unknown = used.filter((key) => !(key in en));
    assert.deepEqual(unknown, []);
    for (const [, key] of html.matchAll(/data-i18n="([^"]+)"[^>]*data-i18n-count=/g)) {
      assert.equal(typeof en[key as keyof typeof en], 'object', `${key} takes a count but has no plural forms`);
    }
    // An element that carries a text key must not also carry markup, or the translation wipes it.
    for (const [, tag, body] of html.matchAll(/<(\w+)[^>]*\sdata-i18n="[^"]+"[^>]*>([\s\S]*?)<\/\1>/g)) {
      assert.ok(!/<\w/.test(body ?? ''), `a data-i18n <${tag}> contains child markup: ${(body ?? '').trim().slice(0, 60)}`);
    }
  });
});
