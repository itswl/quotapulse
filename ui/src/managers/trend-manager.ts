/** Balance trend dialog: stat cards plus the line chart; needs ENABLE_HISTORY_API. */

import { getTrend } from '../api/endpoints.js';
import type { Runway, TrendData, TrendPoint, TrendResponse } from '../api/types.js';
import { LineChart, type ChartTheme, type Series } from '../chart/line-chart.js';
import { byId, requireById } from '../dom.js';
import { escapeHTML, formatCurrency } from '../format.js';
import { t } from '../i18n/index.js';
import { ICON_INFO_CIRCLE } from '../ui/icons.js';
import { AppState } from '../state.js';
import { bindModalClose, closeModal, openModal } from '../ui/modal.js';
import { setLoading } from '../ui/loading.js';

const MODAL_ID = 'trend-modal';
const TREND_DAYS = 30;

let chart: LineChart | null = null;

function destroyChart(): void {
  chart?.destroy();
  chart = null;
}

export async function showProjectTrend(projectName: string, provider: string): Promise<void> {
  const title = byId('trend-modal-title');
  const statsContainer = byId('trend-stats-container');
  if (title) title.textContent = t('trend.title_of', { name: projectName });

  setLoading(true);
  openModal(MODAL_ID);

  try {
    const { response, data } = await getTrend(provider, projectName, TREND_DAYS);

    // database off or no history yet: the backend returns 404; not an exception here — show an explanation
    if (!response.ok || !data || data.status !== 'success') {
      destroyChart();
      if (statsContainer) {
        const message = data && 'message' in data && data.message ? data.message : t('trend.no_data');
        statsContainer.innerHTML = `
            <div class="trend-empty">
                ${ICON_INFO_CIRCLE}
                <p>${escapeHTML(message)}</p>
                <p class="hint">${escapeHTML(t('trend.need_history'))}</p>
            </div>
        `;
      }
      return;
    }

    const trendData = (data as TrendResponse).data;
    if (statsContainer) statsContainer.innerHTML = renderTrendStats(trendData);
    renderTrendChart(trendData);
  } catch (error) {
    destroyChart();
    console.error('Failed to load trend data:', error);
    if (statsContainer) {
      const message = error instanceof Error ? error.message : String(error);
      statsContainer.innerHTML = `
            <div class="trend-empty error">
                <p>${escapeHTML(t('trend.load_failed', { message }))}</p>
            </div>
        `;
    }
  } finally {
    setLoading(false);
  }
}

/** Fewer than two points means the backend sends no change; display as "stable" */
export function trendDirection(change: number | undefined): 'up' | 'down' | 'stable' {
  if (change !== undefined && change > 0) return 'up';
  if (change !== undefined && change < 0) return 'down';
  return 'stable';
}

export function renderTrendStats(trendData: TrendData): string {
  const direction = trendDirection(trendData.change);
  const stats = [
    { label: t('trend.current'), value: formatCurrency(trendData.current_balance), cls: '' },
    { label: t('trend.average'), value: formatCurrency(trendData.avg_balance), cls: '' },
    { label: t('trend.maximum'), value: formatCurrency(trendData.max_balance), cls: '' },
    { label: t('trend.minimum'), value: formatCurrency(trendData.min_balance), cls: '' },
    {
      label: t('trend.trend'),
      value: direction === 'up' ? t('trend.up') : direction === 'down' ? t('trend.down') : t('trend.stable'),
      cls: direction === 'up' ? 'positive' : direction === 'down' ? 'negative' : '',
    },
  ];

  return stats
    .map(
      (stat) => `
        <div class="trend-stat-card">
            <div class="trend-stat-label">${escapeHTML(stat.label)}</div>
            <div class="trend-stat-value ${stat.cls}">${escapeHTML(stat.value)}</div>
        </div>
    `,
    )
    .join('');
}

/* Read a design token so the chart uses the same palette and fonts as the page. */
function cssVar(name: string, fallback: string): string {
  if (typeof getComputedStyle !== 'function') return fallback;
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return value || fallback;
}

function chartTheme(dark: boolean): ChartTheme {
  return {
    text: cssVar('--text-2', dark ? '#a4acb8' : '#5b626e'),
    grid: cssVar('--border', dark ? '#252a33' : '#e5e7eb'),
    tooltipBg: cssVar('--surface', dark ? '#15181d' : '#ffffff'),
    tooltipBorder: cssVar('--border-strong', dark ? '#343b47' : '#cfd4dc'),
    font: cssVar('--font', 'sans-serif'),
    mono: cssVar('--mono', 'monospace'),
  };
}

const pad2 = (n: number): string => String(n).padStart(2, '0');
const dayKey = (d: Date): string => `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`;

/**
 * Chart points and labels for a trend. The store keeps every check, hourly by default, so
 * 30 days is about 720 snapshots; a history longer than two days is charted as one point
 * per local day (its last snapshot), and a shorter one keeps its hourly detail.
 */
export function trendPoints(history: TrendPoint[]): { points: TrendPoint[]; labels: string[] } {
  const byDay = new Map<string, TrendPoint>();
  for (const point of history) byDay.set(dayKey(new Date(point.timestamp)), point); // the day's last wins
  if (byDay.size > 2) {
    const points = [...byDay.values()];
    return { points, labels: points.map((p) => { const d = new Date(p.timestamp); return `${pad2(d.getMonth() + 1)}/${pad2(d.getDate())}`; }) };
  }
  return {
    points: history,
    labels: history.map((p) => {
      const d = new Date(p.timestamp);
      return `${pad2(d.getMonth() + 1)}/${pad2(d.getDate())} ${pad2(d.getHours())}:${pad2(d.getMinutes())}`;
    }),
  };
}

/* Projected balance at the current burn rate: a dashed line from the newest
   point to the depletion date (or 30 days out), clamped at zero. Returns null
   without a runway estimate — no history means nothing to extrapolate. */
export function projectionSeries(
  history: TrendPoint[],
  runway: Runway | null | undefined,
  labels: string[],
): Series | null {
  const last = history[history.length - 1];
  if (!runway?.burn_per_day || !(runway.burn_per_day > 0) || !last) return null;
  const lastDay = new Date(last.timestamp);
  const perStep = runway.burn_per_day / 24; // history steps are hourly
  const horizon = runway.depletion_date
    ? Math.min(30, Math.max(1, Math.round((new Date(runway.depletion_date).getTime() - lastDay.getTime()) / 86_400_000)))
    : 30;
  const projected: Array<number | null> = labels.map(() => null);
  let balance = last.balance;
  const stepsPerDay = 24;
  for (let day = 0; day <= horizon; day += 1) {
    for (let hour = 0; hour < stepsPerDay; hour += 1) {
      if (day === 0 && hour === 0) continue;
      balance = Math.max(0, balance - perStep);
    }
    projected.push(Math.round(balance * 100) / 100);
    labels.push(projectLabel(lastDay, day + 1));
  }
  return {
    label: t('trend.projection'),
    values: projected,
    color: cssVar('--warning', '#e0a03c'),
    dashed: true,
    showPoints: false,
  };
}

function projectLabel(from: Date, daysAhead: number): string {
  const d = new Date(from.getTime() + daysAhead * 86_400_000);
  return `${String(d.getMonth() + 1).padStart(2, '0')}/${String(d.getDate()).padStart(2, '0')}`;
}

function renderTrendChart(trendData: TrendData): void {
  const canvas = requireById<HTMLCanvasElement>('trend-chart');
  const { points: history, labels } = trendPoints(trendData.history || []);
  const dark = document.documentElement.getAttribute('data-theme') === 'dark';

  // the projection rides the project's runway from the dashboard state
  const project = (AppState.balanceData?.projects || []).find((p) => p.project === trendData.project_name);
  const projection = projectionSeries(history, project?.runway ?? null, labels);

  const series: Series[] = [
    {
      label: t('type.balance'),
      values: history.map((h) => h.balance),
      color: cssVar('--accent', '#3358d4'),
      fill: cssVar('--chart-fill', 'rgba(51, 88, 212, 0.14)'),
    },
    {
      label: t('projects.threshold'),
      values: labels.map(() => trendData.threshold),
      color: cssVar('--danger', '#d64545'),
      dashed: true,
      showPoints: false,
    },
  ];
  if (projection) series.splice(1, 0, projection);

  const options = {
    labels,
    dark,
    theme: chartTheme(dark),
    formatValue: (value: number): string => formatCurrency(value),
    series,
  };

  // one canvas toggled across dialog opens; reuse the instance so listeners do not pile up
  if (chart) chart.update(options);
  else chart = new LineChart(canvas, options);
}

export function bindTrendManager(): void {
  bindModalClose(MODAL_ID, '.js-close-trend-modal', destroyChart);
}

export function closeTrendModal(): void {
  closeModal(MODAL_ID);
  destroyChart();
}
