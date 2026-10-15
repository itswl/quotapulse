/**
 * Monthly cost breakdown: measured spend per project from balance history,
 * converted into the base currency, against per-project budgets.
 */

import type { CostSummary, ProjectSpend } from '../api/types.js';
import { t } from '../i18n/index.js';
import { closeModal, bindModalClose, openModal } from './modal.js';

const MODAL_ID = 'cost-modal';

function money(value: number, currency: string): string {
  const rounded = Math.round(value * 100) / 100;
  return `${rounded.toLocaleString()} ${currency}`;
}

function row(entry: ProjectSpend, base: string): string {
  const native = entry.currency ? money(entry.spend, entry.currency) : '—';
  const projected = entry.currency ? money(entry.projected, entry.currency) : '—';
  const budgetCell = entry.budget
    ? `<span class="${entry.over_budget ? 'cost-over' : ''}">${money(entry.budget, entry.currency || base)}</span>`
    : '—';
  return `<tr>
    <td>${escapeHtml(entry.project)}</td>
    <td>${native}</td>
    <td>${entry.currency ? money(entry.spend_base, base) : '—'}</td>
    <td>${projected}</td>
    <td>${budgetCell}</td>
  </tr>`;
}

function escapeHtml(text: string): string {
  return text.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c] || c);
}

/** Renders the breakdown table into the cost modal body and opens it. */
export function showCostModal(summary: CostSummary): void {
  const body = document.getElementById('cost-modal-body');
  if (!body) return;
  const title = document.getElementById('cost-modal-title');
  if (title) {
    title.textContent = t('costs.title_month', {
      month: summary.month,
      base: summary.base_currency,
    });
  }
  const unknown = summary.unknown_fx?.length
    ? `<p class="subsection-hint">${t('costs.unknown_fx', { currencies: summary.unknown_fx.join(', ') })}</p>`
    : '';
  body.innerHTML = `
    ${unknown}
    <table class="cost-table">
      <thead>
        <tr>
          <th>${t('costs.project')}</th>
          <th>${t('costs.spend')}</th>
          <th>${t('costs.spend_base', { base: summary.base_currency })}</th>
          <th>${t('costs.projected')}</th>
          <th>${t('costs.budget')}</th>
        </tr>
      </thead>
      <tbody>${summary.projects.map((p) => row(p, summary.base_currency)).join('')}</tbody>
      <tfoot>
        <tr>
          <td>${t('costs.total')}</td>
          <td></td>
          <td>${money(summary.total_base, summary.base_currency)}</td>
          <td colspan="2">${t('costs.day_of_month', { elapsed: String(summary.days_elapsed), total: String(summary.days_in_month) })}</td>
        </tr>
      </tfoot>
    </table>`;
  openModal(MODAL_ID);
}

export function bindCostModal(): void {
  bindModalClose(MODAL_ID, '.js-close-cost-modal');
}

export function closeCostModal(): void {
  closeModal(MODAL_ID);
}
