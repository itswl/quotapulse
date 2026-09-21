/* Implementation note. */

import { getTrend } from '../api/endpoints.js';
import type { TrendData, TrendResponse } from '../api/types.js';
import { LineChart, type ChartTheme } from '../chart/line-chart.js';
import { byId, requireById } from '../dom.js';
import { escapeHTML, formatCurrency } from '../format.js';
import { ICON_INFO_CIRCLE } from '../ui/icons.js';
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
  if (title) title.textContent = `Balance trend - ${projectName}`;

  setLoading(true);
  openModal(MODAL_ID);

  try {
    const { response, data } = await getTrend(provider, projectName, TREND_DAYS);

    // Implementation note.
    if (!response.ok || !data || data.status !== 'success') {
      destroyChart();
      if (statsContainer) {
        const message = data && 'message' in data && data.message ? data.message : 'No historical data yet';
        statsContainer.innerHTML = `
            <div class="trend-empty">
                ${ICON_INFO_CIRCLE}
                <p>${escapeHTML(message)}</p>
                <p class="hint">Trend charts need balance history. Set ENABLE_DATABASE=true and ENABLE_HISTORY_API=true, then restart the service.</p>
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
                <p>Load failed: ${escapeHTML(message)}</p>
            </div>
        `;
    }
  } finally {
    setLoading(false);
  }
}

/* Implementation note. */
export function trendDirection(change: number | undefined): 'up' | 'down' | 'stable' {
  if (change !== undefined && change > 0) return 'up';
  if (change !== undefined && change < 0) return 'down';
  return 'stable';
}

export function renderTrendStats(trendData: TrendData): string {
  const direction = trendDirection(trendData.change);
  const stats = [
    { label: 'Current balance', value: formatCurrency(trendData.current_balance), cls: '' },
    { label: 'Average balance', value: formatCurrency(trendData.avg_balance), cls: '' },
    { label: 'Maximum balance', value: formatCurrency(trendData.max_balance), cls: '' },
    { label: 'Minimum balance', value: formatCurrency(trendData.min_balance), cls: '' },
    {
      label: 'Trend',
      value: direction === 'up' ? '↑ Up' : direction === 'down' ? '↓ Down' : '→ Stable',
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

function renderTrendChart(trendData: TrendData): void {
  const canvas = requireById<HTMLCanvasElement>('trend-chart');
  const history = trendData.history || [];
  const labels = history.map((h) => new Date(h.timestamp).toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' }));
  const dark = document.documentElement.getAttribute('data-theme') === 'dark';

  const options = {
    labels,
    dark,
    theme: chartTheme(dark),
    formatValue: (value: number): string => formatCurrency(value),
    series: [
      {
        label: 'Balance',
        values: history.map((h) => h.balance),
        color: cssVar('--accent', '#3358d4'),
        fill: cssVar('--chart-fill', 'rgba(51, 88, 212, 0.14)'),
      },
      {
        label: 'Alert threshold',
        values: labels.map(() => trendData.threshold),
        color: cssVar('--danger', '#d64545'),
        dashed: true,
        showPoints: false,
      },
    ],
  };

  // Implementation note.
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
