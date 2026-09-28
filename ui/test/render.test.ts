/* Implementation note. */

import './stub-dom.js';

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import type { CheckResult, CreditsResponse, Features, Runway, SubscriptionResult } from '../src/api/types.js';
import { filterProjects, renderProjectCard, renderProjects } from '../src/ui/projects.js';
import { renderSubscriptionCard, sortSubscriptionsByNextDate } from '../src/ui/subscriptions.js';
import {
  monthlyCost,
  refreshOverview,
  shortestRunway,
  updateBadges,
  updateFailedHint,
  updateNavFreshness,
  updateStats,
  updateSubscriptionStats,
} from '../src/ui/stats.js';
import { AppState, parseRefreshMinutes } from '../src/state.js';
import { formatServerCadence } from '../src/format.js';
import { resetStubDom, stubElement } from './stub-dom.js';

const ALL_OFF: Features = { subscriptions: false, dynamic_config: false, history: false, email_scan: false };
const ALL_ON: Features = { subscriptions: true, dynamic_config: true, history: true, email_scan: true };

function project(overrides: Partial<CheckResult> = {}): CheckResult {
  return {
    project: 'deepseek',
    owner_project: null,
    provider: 'deepseek',
    type: 'balance',
    success: true,
    credits: 430.37,
    threshold: 50,
    need_alarm: false,
    alarm_sent: false,
    error: null,
    cached: false,
    ...overrides,
  };
}

function runway(overrides: Partial<Runway> = {}): Runway {
  return {
    project_id: 'id',
    project_name: 'deepseek',
    provider: 'deepseek',
    balance_type: 'balance',
    current_balance: 430.37,
    window_days: 7,
    data_points: 168,
    span_hours: 167,
    consumed: 437.5,
    topped_up: 0,
    burn_per_day: 62.5,
    runway_days: 6.89,
    depletion_date: '2026-09-21',
    confidence: 'high',
    daily: [],
    today_consumed: null,
    baseline_consumed: null,
    spike_ratio: null,
    ...overrides,
  };
}

describe('renderProjectCard', () => {
  it('把Project名、Provider、Balance和跑道都放进卡片', () => {
    const html = renderProjectCard(project({ runway: runway() }), ALL_OFF);
    assert.match(html, /<h3>deepseek<\/h3>/);
    assert.match(html, /class="project-provider">deepseek</);
    assert.match(html, />430\.37</);
    assert.match(html, />6\.9 days</);
    assert.match(html, /runway-warning/);
    assert.match(html, /estimated to deplete around 2026-09-21/);
  });

  it('没有跑道数据时显示「Accumulating data」而不是 0 天', () => {
    const html = renderProjectCard(project(), ALL_OFF);
    assert.match(html, />Accumulating data</);
    assert.match(html, /runway-unknown/);
    assert.match(html, /<span class="detail-value">—<\/span>/); // Daily spend
  });

  it('Project名里的 HTML 被转义，不会注入标签', () => {
    const html = renderProjectCard(project({ project: '<img src=x onerror=alert(1)>' }), ALL_OFF);
    assert.ok(!html.includes('<img src=x'));
    assert.match(html, /&lt;img src=x onerror=alert\(1\)&gt;/);
  });

  it('关掉动态配置就没有编辑 / 删除按钮，关掉历史就没有趋势入口', () => {
    const html = renderProjectCard(project(), ALL_OFF);
    assert.ok(!html.includes('js-edit-project'));
    assert.ok(!html.includes('js-delete-project'));
    assert.ok(!html.includes('js-show-trend'));
  });

  it('开关都打开时三个入口都在，并带上定位用的 data 属性', () => {
    const html = renderProjectCard(project(), ALL_ON);
    assert.match(html, /js-edit-project" data-project="deepseek"/);
    assert.match(html, /js-delete-project" data-project="deepseek"/);
    assert.match(html, /js-show-trend" data-project="deepseek" data-provider="deepseek"/);
  });

  it('Alerting projects的Status位和文案都切换', () => {
    const html = renderProjectCard(project({ need_alarm: true }), ALL_OFF);
    assert.match(html, /data-status="alert"/);
    assert.match(html, /status-text alert">Alert</);
  });

  it('Quota型显示百分比和「剩余Quota」', () => {
    const html = renderProjectCard(project({ type: 'quota', credits: 42.35, threshold: 10 }), ALL_OFF);
    assert.match(html, /Remaining quota/);
    assert.match(html, /42\.4<span class="unit">%<\/span>/);
  });
});

describe('UnavailableBalance的Project', () => {
  const failed = project({
    success: false,
    credits: null,
    threshold: null,
    error: 'HTTP 401: Unauthorized',
  });

  it('不把 null Balance渲染成 0.00', () => {
    const html = renderProjectCard(failed, ALL_OFF);
    assert.ok(html.includes('Unavailable'), '应该明说Unavailable');
    assert.ok(!html.includes('0.00'), '不能编造一个 0.00 的Balance');
  });

  it('不把失败标成Healthy', () => {
    const html = renderProjectCard(failed, ALL_OFF);
    assert.ok(html.includes('data-status="failed"'), 'Status应是 failed');
    assert.ok(!html.includes('>Healthy<'), '失败的Project不能显示成Healthy');
    assert.ok(!html.includes('balance-progress-bar'), '不该画一条满格的进度条');
  });

  it('把失败原因摆出来', () => {
    const html = renderProjectCard(failed, ALL_OFF);
    assert.ok(html.includes('HTTP 401: Unauthorized'), '错误原文要能看到');
  });

  it('错误消息同样转义，不能从这里注入', () => {
    const html = renderProjectCard(
      project({ success: false, credits: null, error: '<img src=x onerror=alert(1)>' }),
      ALL_OFF,
    );
    assert.ok(!html.includes('<img src=x'), '错误消息必须转义');
  });

  it('开了动态配置仍然能编辑删除，否则改不掉坏掉的密钥', () => {
    const html = renderProjectCard(failed, ALL_ON);
    assert.ok(html.includes('js-edit-project'), '要能编辑');
    assert.ok(html.includes('js-delete-project'), '要能删除');
  });
});

describe('filterProjects', () => {
  const projects = [
    project({ project: 'deepseek', provider: 'deepseek', need_alarm: false }),
    project({ project: '火山-主账号', provider: 'volcengine', need_alarm: true }),
    project({ project: 'openrouter', provider: 'openrouter', need_alarm: false }),
  ];

  it('搜索同时匹配Project名和Provider名，且不区分大小写', () => {
    assert.equal(filterProjects(projects, { search: 'DEEP', provider: 'all', alertsOnly: false }).length, 1);
    assert.equal(filterProjects(projects, { search: 'volcengine', provider: 'all', alertsOnly: false }).length, 1);
    assert.equal(filterProjects(projects, { search: '火山', provider: 'all', alertsOnly: false }).length, 1);
  });

  it('Provider筛选和「仅Alert」可以叠加', () => {
    assert.equal(filterProjects(projects, { search: '', provider: 'openrouter', alertsOnly: false }).length, 1);
    assert.equal(filterProjects(projects, { search: '', provider: 'all', alertsOnly: true }).length, 1);
    assert.equal(filterProjects(projects, { search: '', provider: 'openrouter', alertsOnly: true }).length, 0);
  });
});

describe('概览里的检查失败提示', () => {
  it('有失败时在Alert卡片标签上说明，总数才对得上', () => {
    resetStubDom();
    const label = stubElement('alert-projects-label');
    updateFailedHint([
      project(),
      project({ project: 'glm', success: false, credits: null, error: 'HTTP 401' }),
      project({ project: 'volc', success: false, credits: null, error: '超时' }),
    ]);
    assert.ok(label.textContent.includes('2 unavailable'), `Actual: ${label.textContent}`);
    assert.ok(label.title.includes('glm'), '悬停要能看到是哪几个');
  });

  it('全都Healthy时不留多余文字', () => {
    resetStubDom();
    const label = stubElement('alert-projects-label');
    updateFailedHint([project()]);
    assert.equal(label.textContent, 'Alerting projects');
  });
});

describe('shortestRunway', () => {
  it('挑出最先见底的那个账户', () => {
    const projects = [
      project({ project: 'a', runway: runway({ runway_days: 12 }) }),
      project({ project: 'b', runway: runway({ runway_days: 2.5 }) }),
      project({ project: 'c', runway: runway({ runway_days: 30 }) }),
    ];
    assert.equal(shortestRunway(projects)?.project, 'b');
  });

  it('失败的Project和没有估算的Project都不参与排序', () => {
    const projects = [
      project({ project: 'failed', success: false, runway: runway({ runway_days: 0.1 }) }),
      project({ project: 'no-estimate', runway: runway({ runway_days: null }) }),
      project({ project: 'ok', runway: runway({ runway_days: 9 }) }),
    ];
    assert.equal(shortestRunway(projects)?.project, 'ok');
  });

  it('一个都算不出来时返回 null', () => {
    assert.equal(shortestRunway([project()]), null);
  });
});

describe('renderSubscriptionCard', () => {
  function subscription(overrides: Partial<SubscriptionResult> = {}): SubscriptionResult {
    return {
      name: 'Netflix',
      owner_project: null,
      renewal_day: 15,
      cycle_type: 'monthly',
      days_until_renewal: 20,
      next_renewal_date: '2026-10-15',
      need_alert: false,
      alert_sent: false,
      amount: 99,
      already_renewed: false,
      last_renewed_date: null,
      ...overrides,
    };
  }

  it('显示金额、周期、下次续费日和剩余天数', () => {
    const html = renderSubscriptionCard(subscription());
    assert.match(html, /<h3>Netflix<\/h3>/);
    assert.match(html, />99\.00</);
    assert.match(html, />Monthly</);
    assert.match(html, />2026-10-15</);
    assert.match(html, /days-remaining ">20<span class="unit"> days<\/span>/);
  });

  it('剩余 7 天内标红，14 天内标黄', () => {
    assert.match(renderSubscriptionCard(subscription({ days_until_renewal: 5 })), /days-remaining danger/);
    assert.match(renderSubscriptionCard(subscription({ days_until_renewal: 10 })), /days-remaining warning/);
  });

  it('Renewed时换成「Clear renewal mark」按钮', () => {
    const html = renderSubscriptionCard(subscription({ already_renewed: true }));
    assert.match(html, /js-clear-renewed/);
    assert.ok(!html.includes('js-mark-renewed'));
    assert.match(html, /status-badge success">Renewed</);
  });

  it('未续费时是「标记Renewed」按钮', () => {
    const html = renderSubscriptionCard(subscription());
    assert.match(html, /js-mark-renewed/);
    assert.ok(!html.includes('js-clear-renewed'));
  });

  it('按下一次提醒日期从近到远排序，原数组不变', () => {
    const input = [
      subscription({ name: 'later', next_renewal_date: '2026-12-01', days_until_renewal: 72 }),
      subscription({ name: 'soon', next_renewal_date: '2026-10-02', days_until_renewal: 12 }),
      subscription({ name: 'middle', next_renewal_date: '2026-11-01', days_until_renewal: 42 }),
    ];
    assert.deepEqual(sortSubscriptionsByNextDate(input).map((item) => item.name), ['soon', 'middle', 'later']);
    assert.deepEqual(input.map((item) => item.name), ['later', 'soon', 'middle']);
  });
});

describe('桩 DOM 上的顶部概览', () => {
  it('统计数字与最短跑道都写进对应元素', () => {
    resetStubDom();
    const total = stubElement('total-projects');
    const normal = stubElement('normal-projects');
    const alert = stubElement('alert-projects');
    const lastUpdate = stubElement('last-update');
    const runwayValue = stubElement('shortest-runway');
    const runwayLabel = stubElement('shortest-runway-label');
    const runwayHint = stubElement('shortest-runway-hint');

    const data: CreditsResponse = {
      last_update: new Date().toISOString(),
      projects: [
        project({ project: 'a', runway: runway({ runway_days: 2 }) }),
        project({ project: 'b', need_alarm: true }),
        project({ project: 'c', success: false, credits: null, threshold: null }),
      ],
      summary: {},
    };
    updateStats(data);

    assert.equal(total.textContent, '3');
    assert.equal(normal.textContent, '1'); // 失败的和Alert的都不算Healthy
    assert.equal(alert.textContent, '1');
    assert.equal(lastUpdate.textContent, 'Just now');
    assert.equal(runwayValue.textContent, '2.0 days');
    assert.equal(runwayValue.className, 'stat-value runway-danger');
    assert.equal(runwayLabel.textContent, 'Shortest runway · a');
    assert.match(runwayHint.textContent, /estimated to deplete around/);
  });

  it('一个Project都估算不出跑道时退回破折号', () => {
    resetStubDom();
    stubElement('total-projects');
    stubElement('normal-projects');
    stubElement('alert-projects');
    stubElement('last-update');
    const runwayValue = stubElement('shortest-runway');
    const runwayLabel = stubElement('shortest-runway-label');

    updateStats({ last_update: null, projects: [project()], summary: {} });

    assert.equal(runwayValue.textContent, '—');
    assert.equal(runwayValue.className, 'stat-value');
    assert.equal(runwayLabel.textContent, 'Shortest runway');
  });
});

describe('桩 DOM 上的Project列表', () => {
  it('一个Project都没有时渲染「开始使用」空Status而不是空白', () => {
    resetStubDom();
    const container = stubElement('projects-container');
    renderProjects({ last_update: null, projects: [], summary: {} });

    assert.match(container.innerHTML, /empty-state/);
    assert.match(container.innerHTML, /No projects yet/);
    assert.ok(!container.innerHTML.includes('js-open-add-project'), '动态配置关闭时不给「Add project」按钮');
    assert.equal(container.className, 'projects-grid');
  });

  it('开了动态配置的空Status带「Add project」入口', () => {
    resetStubDom();
    const container = stubElement('projects-container');
    AppState.features = { ...AppState.features, dynamic_config: true };
    renderProjects({ last_update: null, projects: [], summary: {} });
    AppState.features = { ...AppState.features, dynamic_config: false };

    assert.match(container.innerHTML, /js-open-add-project/);
  });

  it('有Project但筛不出结果时提示清除筛选', () => {
    resetStubDom();
    const container = stubElement('projects-container');
    AppState.searchQuery = 'nothing-matches-this';
    renderProjects({ last_update: null, projects: [project()], summary: {} });
    AppState.searchQuery = '';

    assert.match(container.innerHTML, /No projects match the filters/);
    assert.match(container.innerHTML, /js-clear-filters/);
  });

  it('Alerts only 筛选打开且没有告警时说明一切正常', () => {
    resetStubDom();
    const container = stubElement('projects-container');
    AppState.alertsOnly = true;
    renderProjects({ last_update: null, projects: [project()], summary: {} });
    AppState.alertsOnly = false;

    assert.match(container.innerHTML, /No alerts/);
    assert.ok(!container.innerHTML.includes('js-clear-filters'));
  });

  it('Alerts only 筛选下有告警但被搜索滤掉时，提示清除筛选', () => {
    resetStubDom();
    const container = stubElement('projects-container');
    AppState.alertsOnly = true;
    AppState.searchQuery = 'nothing-matches-this';
    renderProjects({ last_update: null, projects: [project({ need_alarm: true })], summary: {} });
    AppState.searchQuery = '';
    AppState.alertsOnly = false;

    assert.match(container.innerHTML, /No projects match the filters/);
    assert.match(container.innerHTML, /js-clear-filters/);
  });

  it('首次渲染带 stagger 入场类，再次渲染不重复动画', () => {
    resetStubDom();
    const container = stubElement('projects-container');
    const data = { last_update: null, projects: [project({ project: 'a' })], summary: {} };

    renderProjects(data);
    assert.equal(container.className, 'projects-grid stagger');

    renderProjects(data);
    assert.equal(container.className, 'projects-grid');
  });

  it('有数据时每个Project一张卡片', () => {
    resetStubDom();
    const container = stubElement('projects-container');
    renderProjects({
      last_update: null,
      projects: [project({ project: 'a' }), project({ project: 'b' })],
      summary: {},
    });

    assert.equal(container.innerHTML.match(/class="project-card"/g)?.length, 2);
  });
});

describe('概览带上的订阅统计', () => {
  function sub(overrides: Partial<SubscriptionResult> = {}): SubscriptionResult {
    return {
      name: 'Copilot',
      owner_project: null,
      renewal_day: 26,
      cycle_type: 'monthly',
      days_until_renewal: 5,
      next_renewal_date: '2026-10-01',
      need_alert: true,
      alert_sent: false,
      amount: 10,
      already_renewed: false,
      last_renewed_date: null,
      ...overrides,
    };
  }

  function stubBand(): void {
    for (const id of ['sub-due-soon', 'sub-due-hint', 'sub-due-30', 'sub-renewed', 'sub-cost']) {
      stubElement(id);
    }
  }

  it('7 天内到期的计数标红，并预告最近的一条', () => {
    resetStubDom();
    stubBand();
    updateSubscriptionStats({
      last_update: null,
      subscriptions: [
        sub({ name: 'Copilot', days_until_renewal: 5 }),
        sub({ name: 'Server', days_until_renewal: 2, next_renewal_date: '2026-09-30', amount: 7 }),
        sub({ name: 'iCloud', days_until_renewal: 20, need_alert: false, amount: 21 }),
      ],
      summary: {},
    });

    const value = stubElement('sub-due-soon');
    assert.equal(value.textContent, '2');
    assert.equal(value.className, 'stat-value runway-danger');
    assert.match(stubElement('sub-due-hint').textContent, /Next: Server on 2026-09-30/);
    assert.equal(stubElement('sub-due-30').textContent, '3');
    assert.equal(stubElement('sub-due-30').className, 'stat-value runway-warning');
  });

  it('已续费的不再算到期，但照常计入月成本', () => {
    resetStubDom();
    stubBand();
    updateSubscriptionStats({
      last_update: null,
      subscriptions: [
        sub({ amount: 10 }),
        sub({ name: 'CVM', days_until_renewal: 3, amount: 1420, already_renewed: true, need_alert: false }),
        sub({ name: 'Domain', days_until_renewal: 95, cycle_type: 'yearly', need_alert: false, amount: 120 }),
        sub({ name: 'VPS', days_until_renewal: 40, cycle_type: 'weekly', need_alert: false, amount: 52 }),
      ],
      summary: {},
    });

    assert.equal(stubElement('sub-due-soon').textContent, '1');
    assert.equal(stubElement('sub-renewed').textContent, '1');
    assert.equal(stubElement('sub-cost').textContent, '1665.33');
  });

  it('全都不急时主指标平静为 0，并预告下一次续费', () => {
    resetStubDom();
    stubBand();
    updateSubscriptionStats({
      last_update: null,
      subscriptions: [sub({ days_until_renewal: 20, need_alert: false, next_renewal_date: '2026-10-18' })],
      summary: {},
    });

    const value = stubElement('sub-due-soon');
    assert.equal(value.textContent, '0');
    assert.equal(value.className, 'stat-value');
    assert.match(stubElement('sub-due-hint').textContent, /Nothing due this week; next is Copilot on 2026-10-18/);
  });

  it('订阅功能关闭时给说明而不是 0', () => {
    resetStubDom();
    stubBand();
    updateSubscriptionStats(null);
    assert.equal(stubElement('sub-due-soon').textContent, '—');
    assert.match(stubElement('sub-due-hint').textContent, /ENABLE_SUBSCRIPTIONS/);
    assert.equal(stubElement('sub-cost').textContent, '—');
  });

  it('monthlyCost 把周期折算成月费', () => {
    assert.equal(monthlyCost(sub({ amount: 21 })), 21);
    assert.equal(monthlyCost(sub({ amount: 52, cycle_type: 'weekly' })), (52 * 52) / 12);
    assert.equal(monthlyCost(sub({ amount: 120, cycle_type: 'yearly' })), 10);
    assert.equal(monthlyCost(sub({ amount: 120, cycle_type: 'lunar_yearly' })), 10);
  });
});

describe('切换器徽标', () => {
  function sub2(overrides: Partial<SubscriptionResult>): SubscriptionResult {
    return {
      name: 'Copilot',
      owner_project: null,
      renewal_day: 26,
      cycle_type: 'monthly',
      days_until_renewal: 5,
      next_renewal_date: '2026-10-01',
      need_alert: false,
      alert_sent: false,
      amount: 10,
      already_renewed: false,
      last_renewed_date: null,
      ...overrides,
    };
  }

  it('按 need_alarm 和 need_alert 计数，为 0 时隐藏', () => {
    resetStubDom();
    const alertBadge = stubElement('alerts-badge');
    const subBadge = stubElement('subs-badge');
    AppState.balanceData = {
      last_update: null,
      projects: [project({ need_alarm: true }), project({ project: 'b' })],
      summary: {},
    };
    AppState.subscriptionData = {
      last_update: null,
      subscriptions: [
        sub2({ need_alert: true }),
        sub2({ name: 'b', need_alert: false }),
        sub2({ name: 'c', need_alert: true }),
      ],
      summary: {},
    };
    AppState.features = { ...AppState.features, subscriptions: true };

    updateBadges();
    assert.equal(alertBadge.textContent, '1');
    assert.equal(alertBadge.hidden, false);
    assert.ok(alertBadge.classList.contains('danger'));
    assert.equal(subBadge.textContent, '2');
    assert.ok(subBadge.classList.contains('warning'));

    AppState.balanceData!.projects = [project()];
    AppState.features = { ...AppState.features, subscriptions: false };
    updateBadges();
    assert.equal(alertBadge.hidden, true);
    assert.equal(subBadge.hidden, true);
  });
});

describe('refreshOverview 跟随视图', () => {
  it('data-view 指向当前视图，订阅开关未开时退回 projects', () => {
    resetStubDom();
    const grid = stubElement('stats-grid');
    AppState.features = { subscriptions: true, dynamic_config: false, history: false, email_scan: true };
    AppState.subscriptionData = { last_update: null, subscriptions: [], summary: {} };

    AppState.currentView = 'subscriptions';
    refreshOverview();
    assert.equal(grid.dataset['view'], 'subscriptions');

    AppState.currentView = 'email';
    refreshOverview();
    assert.equal(grid.dataset['view'], 'email');

    AppState.currentView = 'all';
    AppState.balanceData = { last_update: null, projects: [], summary: {} };
    refreshOverview();
    assert.equal(grid.dataset['view'], 'projects');

    AppState.currentView = 'subscriptions';
    AppState.features = { ...AppState.features, subscriptions: false };
    refreshOverview();
    assert.equal(grid.dataset['view'], 'projects');
  });
});

describe('导航状态时间戳与状态点', () => {
  it('新数据绿点 + 相对时间，超时变黄，长期失联或失败变红', () => {
    resetStubDom();
    const stamp = stubElement('last-update');
    const dot = stubElement('nav-live-dot');

    updateNavFreshness({ last_update: new Date().toISOString(), projects: [], summary: {} });
    assert.equal(stamp.textContent, 'Just now');
    assert.equal(dot.className, 'live-dot');

    updateNavFreshness({ last_update: new Date(Date.now() - 2 * 3600_000).toISOString(), projects: [], summary: {} });
    assert.equal(stamp.textContent, '2 hr ago');
    assert.equal(dot.className, 'live-dot stale');

    updateNavFreshness({ last_update: new Date(Date.now() - 30 * 3600_000).toISOString(), projects: [], summary: {} });
    assert.equal(dot.className, 'live-dot down');

    updateNavFreshness(null, true);
    assert.equal(stamp.textContent, '—');
    assert.equal(dot.className, 'live-dot down');
  });
});

describe('parseRefreshMinutes', () => {
  it('四舍五入并夹在 1–1440，垃圾输入回退 5', () => {
    assert.equal(parseRefreshMinutes('5'), 5);
    assert.equal(parseRefreshMinutes('0'), 1);
    assert.equal(parseRefreshMinutes('-3'), 1);
    assert.equal(parseRefreshMinutes('9999'), 1440);
    assert.equal(parseRefreshMinutes('7.6'), 8);
    assert.equal(parseRefreshMinutes(''), 1);
    assert.equal(parseRefreshMinutes('abc'), 5);
  });
});

describe('订阅卡上的通知状态徽标', () => {
  function sub(overrides: Partial<SubscriptionResult> = {}): SubscriptionResult {
    return {
      name: 'Copilot', owner_project: null, renewal_day: 26, cycle_type: 'monthly',
      days_until_renewal: 5, next_renewal_date: '2026-10-01', need_alert: true,
      alert_sent: true, alert_state: 'sent', amount: 10, already_renewed: false,
      last_renewed_date: null, ...overrides,
    };
  }

  it('失败带原因悬浮提示，冷却带下一次时间，已发送有确认', () => {
    const failed = renderSubscriptionCard(sub({ need_alert: true, alert_state: 'failed', last_error: 'Webhook returned HTTP 500: boom' }));
    assert.match(failed, /status-badge danger[^>]*>Send failed</);
    assert.match(failed, /title="Webhook returned HTTP 500: boom"/);

    const cooldown = renderSubscriptionCard(sub({
      need_alert: true, alert_state: 'cooldown_skipped',
      next_eligible_at: new Date(Date.now() + 17 * 60_000).toISOString(),
    }));
    assert.match(cooldown, /Cooldown · next in 17 min/);
    const whole = renderSubscriptionCard(sub({ need_alert: true, alert_state: 'cooldown_skipped', next_eligible_at: new Date(Date.now() + 9 * 3600_000).toISOString() }));
    assert.match(whole, /next in 9 hr</);

    const sent = renderSubscriptionCard(sub({ need_alert: true, alert_state: 'sent' }));
    assert.match(sent, />Notified</);
  });

  it('need_alert 为假时不显示通知徽标', () => {
    const card = renderSubscriptionCard(sub({ need_alert: false, alert_state: 'cooldown_skipped' }));
    assert.ok(!card.includes('Cooldown'));
  });
});

describe('formatServerCadence', () => {
  it('把秒数人话化，并带上下次运行倒计时', () => {
    const next = new Date(Date.now() + 12 * 60_000).toISOString();
    assert.equal(formatServerCadence({ schedule: 'Every 3600 seconds', next_run: next }), 'Server checks every 1 hr · next in 12 min');
    assert.equal(formatServerCadence({ schedule: 'Every 1800 seconds', next_run: next }), 'Server checks every 30 min · next in 12 min');
    assert.equal(formatServerCadence({ schedule: 'Every 45 seconds', next_run: next }), 'Server checks every 45 s · next in 12 min');
    assert.equal(formatServerCadence({ schedule: 'At 09:00,15:00', next_run: next }), 'Server checks at 09:00,15:00 · next in 12 min');
    assert.equal(formatServerCadence({ schedule: 'Every 3600 seconds', next_run: null }), 'Server checks every 1 hr');
    assert.equal(formatServerCadence(null), null);
    assert.equal(formatServerCadence({ schedule: '', next_run: null }), null);
  });
});
