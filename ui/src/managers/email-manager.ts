/**
 * Implementation note.
 *
 * Implementation note.
 * Implementation note.
 */

import { mutate } from '../api/client.js';
import { ENDPOINTS, getEmailHistory, getEmailScanState, getEmailSuppressions, getMailboxes, runEmailScan } from '../api/endpoints.js';
import type {
  EmailAlert,
  EmailAlertRecord,
  EmailScanState,
  EmailSuppression,
  MailboxConfig,
  MailboxPayload,
  MailboxResult,
} from '../api/types.js';
import { byId, inputById, inputValue, isChecked, onClick, setChecked, setInputValue, toggleDisplay } from '../dom.js';
import { escapeAttr, escapeHTML, formatCurrency, formatDate, formatDays, getRelativeTime } from '../format.js';
import { t } from '../i18n/index.js';
import { AppState } from '../state.js';
import { confirmDialog } from '../ui/confirm.js';
import { emptyState } from '../ui/empty.js';
import { clearFieldErrors, requireFields } from '../ui/forms.js';
import { ICON_DELETE, ICON_EDIT } from '../ui/icons.js';
import { setLoading } from '../ui/loading.js';
import { bindModalClose, closeModal, openModal } from '../ui/modal.js';
import { showToast } from '../ui/toast.js';

const MODAL_ID = 'email-modal';
const DEFAULT_PORT = 993;

interface EmailManagerState {
  mailboxes: MailboxConfig[];
  scan: EmailScanState | null;
  history: EmailAlertRecord[];
  suppressions: EmailSuppression[];
  loaded: boolean;
}

interface AlertCardOptions {
  dryRun?: boolean;
  history?: boolean;
  /* Muting needs the History API (it is kept in the database next to the history). */
  canMute?: boolean;
  muted?: boolean;
}

/* Implementation note. */
type AnyAlert = (EmailAlert | EmailAlertRecord) & { keywords?: string[]; matched_keywords?: string[]; timestamp?: string };

export const EmailManager = {
  state: {
    mailboxes: [],
    scan: null,
    history: [],
    suppressions: [],
    loaded: false,
  } as EmailManagerState,

  async load(force = false): Promise<void> {
    if (this.state.loaded && !force) {
      this.renderAll();
      return;
    }
    try {
      setLoading(true);
      await this.fetchAll();
      this.renderAll();
    } catch (error) {
      console.error('Failed to load mailbox scanning data:', error);
      showToast(error instanceof Error && error.message ? error.message : t('email.load_failed'), 'error');
    } finally {
      setLoading(false);
    }
  },

  async fetchAll(): Promise<void> {
    const [mailboxResult, scanState] = await Promise.all([getMailboxes(), getEmailScanState()]);
    this.state.mailboxes = mailboxResult.emails || [];
    this.state.scan = scanState ?? null;
    const [history, suppressions] = AppState.features.history
      ? await Promise.all([this.fetchHistory(), this.fetchSuppressions()])
      : [[], []];
    this.state.history = history;
    this.state.suppressions = suppressions;
    this.state.loaded = true;
  },

  async fetchSuppressions(): Promise<EmailSuppression[]> {
    try {
      return (await getEmailSuppressions()).data || [];
    } catch (error) {
      console.warn('Muted senders unavailable:', error);
      return [];
    }
  },

  isMuted(alert: { mailbox?: string | null; sender?: string | null }): boolean {
    return this.state.suppressions.some((s) => s.mailbox === alert.mailbox && s.sender === alert.sender);
  },

  cardOptions(extra: AlertCardOptions, alert: AnyAlert): AlertCardOptions {
    return { ...extra, canMute: AppState.features.history, muted: this.isMuted(alert) };
  },

  async fetchHistory(): Promise<EmailAlertRecord[]> {
    try {
      const result = await getEmailHistory(30, 100);
      return result.data || [];
    } catch (error) {
      // Implementation note.
      console.warn('Email alert history unavailable:', error);
      return [];
    }
  },

  // Implementation note.

  renderAll(): void {
    this.renderSummary();
    this.renderMailboxes();
    this.renderAlerts();
    this.renderHistory();
    this.renderSuppressions();
    toggleDisplay('add-email-btn', AppState.features.dynamic_config);
  },

  renderSummary(): void {
    const container = byId('email-scan-summary');
    if (!container) return;

    const scan = this.state.scan;
    const scanned = Boolean(scan?.last_update);
    const summary = scan?.summary ?? {};
    const enabledCount = this.state.mailboxes.filter((m) => m.enabled !== false).length;

    const chips: Array<{ label: string; value: string | number; cls?: string }> = [
      { label: t('email.chip_enabled'), value: enabledCount },
      { label: t('email.chip_scanned'), value: scanned ? summary.total_emails ?? 0 : '-' },
      {
        label: t('email.chip_alerts'),
        value: scanned ? summary.total_alerts ?? 0 : '-',
        cls: (summary.total_alerts ?? 0) > 0 ? 'warning' : '',
      },
      { label: t('email.chip_sent'), value: scanned ? summary.alerts_sent ?? 0 : '-' },
      { label: t('email.chip_last_scan'), value: scanned ? getRelativeTime(scan?.last_update) : t('email.not_scanned') },
    ];

    if (scanned && scan) {
      chips.push({ label: t('email.chip_range'), value: scan.days != null ? t('count.last_days', { count: scan.days }) : '-' });
      chips.push({
        label: t('email.chip_mode'),
        value: scan.dry_run ? t('email.mode_dry_run') : t('email.mode_real'),
        cls: scan.dry_run ? '' : 'danger',
      });
      if ((summary.failed_mailboxes ?? 0) > 0) {
        chips.push({ label: t('email.chip_failed'), value: summary.failed_mailboxes ?? 0, cls: 'danger' });
      }
    }

    container.innerHTML = chips
      .map(
        (chip) => `
            <div class="summary-chip ${chip.cls || ''}">
                <span class="summary-chip-label">${escapeHTML(chip.label)}</span>
                <span class="summary-chip-value">${escapeHTML(chip.value)}</span>
            </div>
        `,
      )
      .join('');
  },

  renderMailboxes(): void {
    const container = byId('mailboxes-container');
    if (!container) return;

    const mailboxes = this.state.mailboxes;
    if (mailboxes.length === 0) {
      container.innerHTML = emptyState(
        t('email.no_mailboxes'),
        AppState.features.dynamic_config ? t('email.no_mailboxes_dynamic') : t('email.no_mailboxes_env'),
        'mail',
        true,
      );
      return;
    }

    const statsByName = new Map((this.state.scan?.mailboxes ?? []).map((m) => [m.name, m]));
    container.innerHTML = mailboxes.map((mailbox) => renderMailboxCard(mailbox, statsByName.get(mailbox.name))).join('');
  },

  renderAlerts(): void {
    const container = byId('email-alerts-container');
    if (!container) return;

    const scan = this.state.scan;
    if (!scan?.last_update) {
      container.innerHTML = emptyState(t('email.not_scanned'), t('email.not_scanned_text'), 'mail', true);
      return;
    }

    const alerts = scan.alerts || [];
    if (alerts.length === 0) {
      container.innerHTML = emptyState(
        t('email.no_alerts'),
        t('email.no_alerts_text', { period: formatDays(scan.days ?? 0) }),
        'mail',
        true,
      );
      return;
    }

    container.innerHTML = alerts
      .map((alert) => renderAlertCard(alert, this.cardOptions({ dryRun: scan.dry_run ?? false }, alert)))
      .join('');
  },

  renderHistory(): void {
    const block = byId('email-history-block');
    const container = byId('email-history-container');
    if (!block || !container) return;

    if (!AppState.features.history) {
      block.style.display = 'none';
      return;
    }
    block.style.display = 'block';

    const history = this.state.history;
    container.innerHTML =
      history.length === 0
        ? emptyState(t('email.no_history'), t('email.no_history_text'), 'mail', true)
        : history.map((record) => renderAlertCard(record, this.cardOptions({ history: true }, record))).join('');
  },

  /* Muted senders, so a mute can be undone from the dashboard. Hidden when there are none. */
  renderSuppressions(): void {
    const block = byId('email-suppressions-block');
    const container = byId('email-suppressions-container');
    if (!block || !container) return;

    const muted = AppState.features.history ? this.state.suppressions : [];
    block.style.display = muted.length > 0 ? 'block' : 'none';
    container.innerHTML = muted.map(renderSuppressionRow).join('');
  },

  // Implementation note.

  async runScan(): Promise<void> {
    const btn = byId<HTMLButtonElement>('email-scan-btn');
    const days = Number.parseInt(byId<HTMLSelectElement>('email-scan-days')?.value ?? '', 10) || 1;

    if (!this.state.mailboxes.some((m) => m.enabled !== false)) {
      showToast(t('email.no_mailbox_to_scan'), 'warning');
      return;
    }

    try {
      if (btn) btn.disabled = true;
      showToast(t('email.scanning', { period: formatDays(days) }), 'info');

      const result = await runEmailScan(days);
      await this.fetchAll();
      this.renderAll();

      const summary = result.summary ?? {};
      const alerts = summary.total_alerts ?? 0;
      showToast(
        t('email.scan_complete', {
          emails: t('count.emails', { count: summary.total_emails ?? 0 }),
          alerts: t('count.alerts', { count: alerts }),
          dryRun: result.dry_run ? t('email.scan_dry_run_suffix') : '',
        }),
        alerts > 0 ? 'warning' : 'success',
      );
    } catch (error) {
      console.error('Mailbox scan failed:', error);
      showToast(error instanceof Error && error.message ? error.message : t('email.scan_failed'), 'error');
    } finally {
      if (btn) btn.disabled = false;
    }
  },
};

// Implementation note.

export function renderMailboxCard(mailbox: MailboxConfig, stat: MailboxResult | undefined): string {
  const name = mailbox.name || mailbox.username || t('email.unnamed');
  const nameAttr = escapeAttr(name);
  const port = mailbox.port || DEFAULT_PORT;
  const enabled = mailbox.enabled !== false;

  let statusHtml: string;
  if (!enabled) {
    statusHtml = `<span class="status-badge muted">${escapeHTML(t('status.disabled'))}</span>`;
  } else if (!stat) {
    statusHtml = `<span class="status-badge muted">${escapeHTML(t('email.status_not_scanned'))}</span>`;
  } else if (stat.error) {
    statusHtml = `<span class="status-badge danger" title="${escapeAttr(stat.error)}">${escapeHTML(t('email.status_connection_failed'))}</span>`;
  } else if (stat.alert_count > 0) {
    statusHtml = `<span class="status-badge warning">${escapeHTML(t('count.alerts', { count: stat.alert_count }))}</span>`;
  } else {
    statusHtml = `<span class="status-badge success">${escapeHTML(t('status.healthy'))}</span>`;
  }

  const actions = AppState.features.dynamic_config
    ? `
            <div class="subscription-actions">
                <button class="action-icon-btn js-edit-email" data-name="${nameAttr}" title="${escapeAttr(t('common.edit'))}">
                    ${ICON_EDIT}
                </button>
                <button class="action-icon-btn danger js-delete-email" data-name="${nameAttr}" title="${escapeAttr(t('common.delete'))}">
                    ${ICON_DELETE}
                </button>
            </div>`
    : '';

  const scannedMeta =
    stat && !stat.error
      ? `<span class="meta-item"><span class="k">${t('email.chip_last_scan')}</span>${escapeHTML(t('count.emails', { count: stat.total_emails }))}</span>`
      : '';
  const errorMeta = stat && stat.error ? `<span class="meta-item error-text">${escapeHTML(stat.error)}</span>` : '';

  return `
            <div class="subscription-card mailbox-card ${enabled ? '' : 'disabled'}">
                <div class="subscription-info">
                    <h3>${escapeHTML(name)}</h3>
                    <div class="subscription-meta">
                        <span class="meta-item">${escapeHTML(mailbox.username || '-')}</span>
                        <span class="meta-item">${escapeHTML(mailbox.host || '-')}:${escapeHTML(port)}</span>
                        <span class="meta-item">${escapeHTML(mailbox.use_ssl === false ? t('email.plaintext') : t('email.ssl'))}</span>
                        ${scannedMeta}
                        ${errorMeta}
                    </div>
                </div>
                <div class="subscription-status">
                    ${actions}
                    ${statusHtml}
                </div>
            </div>
        `;
}

export function renderAlertCard(alert: AnyAlert, options: AlertCardOptions = {}): string {
  let badge: string;
  if ('duplicate' in alert && alert.duplicate) {
    badge = `<span class="status-badge muted">${escapeHTML(t('email.badge_duplicate'))}</span>`;
  } else if (alert.alert_sent) {
    badge = `<span class="status-badge success">${escapeHTML(t('email.badge_sent'))}</span>`;
  } else if (options.dryRun) {
    badge = `<span class="status-badge info">${escapeHTML(t('email.badge_dry_run'))}</span>`;
  } else {
    badge = `<span class="status-badge danger">${escapeHTML(t('email.badge_not_sent'))}</span>`;
  }

  // Implementation note.
  const keywords = alert.keywords ?? alert.matched_keywords ?? [];
  const keywordTags = (Array.isArray(keywords) ? keywords : [keywords])
    .map((kw) => `<span class="keyword-tag">${escapeHTML(kw)}</span>`)
    .join('');

  const hasAmount = alert.amount !== null && alert.amount !== undefined;
  const serviceName = alert.service_name && alert.service_name !== 'Unknown service' ? alert.service_name : '';

  return `
            <div class="email-alert-card ${options.history ? 'history' : ''}">
                <div class="email-alert-main">
                    <div class="email-alert-subject">${escapeHTML(alert.subject || t('email.no_subject'))}</div>
                    <div class="subscription-meta">
                        <span class="meta-item project-meta">${escapeHTML(alert.mailbox || '-')}</span>
                        <span class="meta-item"><span class="k">${t('email.sender')}</span>${escapeHTML(alert.sender || '-')}</span>
                        <span class="meta-item">${escapeHTML(alert.date || '-')}</span>
                        ${serviceName ? `<span class="meta-item"><span class="k">${t('email.service')}</span>${escapeHTML(serviceName)}</span>` : ''}
                        ${hasAmount ? `<span class="meta-item"><span class="k">${t('common.amount')}</span>${formatCurrency(alert.amount)}</span>` : ''}
                    </div>
                    ${keywordTags ? `<div class="keyword-tags">${keywordTags}</div>` : ''}
                </div>
                <div class="email-alert-side">
                    ${badge}
                    ${
                      options.muted
                        ? `<span class="status-badge muted" title="${escapeAttr(t('email.sender_muted_title'))}">${escapeHTML(t('email.sender_muted'))}</span>`
                        : options.canMute
                          ? `<button type="button" class="btn-link js-email-suppress" data-mailbox="${escapeAttr(alert.mailbox || '')}" data-sender="${escapeAttr(alert.sender || '')}" title="${escapeAttr(t('email.false_positive_title'))}">${escapeHTML(t('email.false_positive'))}</button>`
                          : ''
                    }
                    ${options.history && alert.timestamp ? `<span>${escapeHTML(t('email.recorded', { date: formatDate(alert.timestamp) }))}</span>` : ''}
                </div>
            </div>
        `;
}

// Implementation note.

export function openEmailModal(mailbox: MailboxConfig | null = null): void {
  byId<HTMLFormElement>('email-form')?.reset();
  clearFieldErrors('email-form');
  setInputValue('email-port', DEFAULT_PORT);
  setChecked('email-use-ssl', true);
  setChecked('email-enabled', true);

  const title = byId('email-modal-title');
  const nameInput = inputById('email-name');
  const passwordHint = byId('email-password-hint');

  if (mailbox) {
    if (title) title.textContent = t('email.edit_mailbox');
    setInputValue('email-edit-mode', 'true');
    nameInput.value = mailbox.name || '';
    nameInput.readOnly = true; // The name is the stable key; delete and recreate it to rename.
    setInputValue('email-host', mailbox.host || '');
    setInputValue('email-port', mailbox.port || DEFAULT_PORT);
    setInputValue('email-username', mailbox.username || '');
    setChecked('email-use-ssl', mailbox.use_ssl !== false);
    setChecked('email-enabled', mailbox.enabled !== false);
    if (passwordHint) passwordHint.textContent = t('mailbox.keep_password');
  } else {
    if (title) title.textContent = t('email.add_mailbox');
    setInputValue('email-edit-mode', 'false');
    nameInput.readOnly = false;
    if (passwordHint) passwordHint.textContent = '';
  }

  openModal(MODAL_ID);
}

function closeEmailModal(): void {
  closeModal(MODAL_ID);
}

async function saveEmail(event: Event): Promise<void> {
  event.preventDefault();

  const isEdit = inputById('email-edit-mode').value === 'true';

  clearFieldErrors('email-form');
  const required: Array<[string, string]> = [
    ['email-name', t('mailbox.name_required')],
    ['email-host', t('mailbox.host_required')],
    ['email-username', t('mailbox.username_required')],
  ];
  if (!isEdit) required.push(['email-password', t('mailbox.password_required')]);
  if (!requireFields(required)) return;

  const data: MailboxPayload = {
    name: inputValue('email-name'),
    host: inputValue('email-host'),
    port: Number.parseInt(inputById('email-port').value, 10) || DEFAULT_PORT,
    username: inputValue('email-username'),
    use_ssl: isChecked('email-use-ssl'),
    enabled: isChecked('email-enabled'),
  };
  // An empty password preserves the existing value.
  const password = inputById('email-password').value;
  if (password) data.password = password;

  const result = await mutate(ENDPOINTS.saveEmail, data, { success: isEdit ? t('mailbox.updated') : t('mailbox.added'), fail: t('common.save_failed') });
  if (result) {
    closeEmailModal();
    await EmailManager.load(true);
  }
}

export function editEmail(name: string): void {
  const mailbox = EmailManager.state.mailboxes.find((m) => m.name === name);
  if (!mailbox) {
    showToast(t('mailbox.not_found'), 'error');
    return;
  }
  openEmailModal(mailbox);
}

export async function deleteEmail(name: string): Promise<void> {
  const confirmed = await confirmDialog({
    title: t('mailbox.delete_title', { name }),
    message: t('common.irreversible'),
    confirmLabel: t('email.delete_mailbox'),
  });
  if (!confirmed) return;
  if (await mutate(ENDPOINTS.deleteEmail, { name }, { success: t('mailbox.deleted'), fail: t('common.delete_failed') })) {
    await EmailManager.load(true);
  }
}

function renderSuppressionRow(item: EmailSuppression): string {
  return `
            <div class="subscription-card suppression-row">
                <div class="subscription-info">
                    <h3>${escapeHTML(item.sender)}</h3>
                    <div class="subscription-meta"><span class="meta-item project-meta">${escapeHTML(item.mailbox)}</span></div>
                </div>
                <button type="button" class="btn-secondary js-email-unsuppress" data-mailbox="${escapeAttr(item.mailbox)}" data-sender="${escapeAttr(item.sender)}">${escapeHTML(t('email.unmute'))}</button>
            </div>
        `;
}

/** Mute a mailbox+sender pair so future billing emails from it stay silent. */
export async function suppressEmail(mailbox: string, sender: string): Promise<void> {
  if (!mailbox || !sender) {
    return;
  }
  const confirmed = await confirmDialog({
    title: t('email.mute_title', { sender }),
    message: t('email.mute_message', { mailbox }),
    confirmLabel: t('email.mute_confirm'),
  });
  if (!confirmed) return;
  if (await mutate(ENDPOINTS.emailSuppressionAdd, { mailbox, sender }, { success: t('email.muted_toast'), fail: t('email.mute_failed') })) {
    await EmailManager.load(true);
  }
}

/** Undo a mute so the sender notifies again. */
export async function unsuppressEmail(mailbox: string, sender: string): Promise<void> {
  if (!mailbox || !sender) {
    return;
  }
  if (await mutate(ENDPOINTS.emailSuppressionDelete, { mailbox, sender }, { success: t('email.unmuted_toast'), fail: t('email.unmute_failed') })) {
    await EmailManager.load(true);
  }
}

export function bindEmailManager(): void {
  onClick('email-scan-btn', () => void EmailManager.runScan());
  onClick('add-email-btn', () => openEmailModal());
  bindModalClose(MODAL_ID, '.js-close-email-modal');
  document.querySelector('.js-save-email')?.addEventListener('click', (e) => void saveEmail(e));
  byId('email-form')?.addEventListener('submit', (e) => void saveEmail(e));
}
