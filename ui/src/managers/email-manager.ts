/**
 * Implementation note.
 *
 * Implementation note.
 * Implementation note.
 */

import { mutate } from '../api/client.js';
import { ENDPOINTS, getEmailHistory, getEmailScanState, getMailboxes, runEmailScan } from '../api/endpoints.js';
import type { EmailAlert, EmailAlertRecord, EmailScanState, MailboxConfig, MailboxPayload, MailboxResult } from '../api/types.js';
import { byId, inputById, inputValue, isChecked, onClick, setChecked, setInputValue, toggleDisplay } from '../dom.js';
import { escapeAttr, escapeHTML, formatCurrency, formatDate, getRelativeTime } from '../format.js';
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
  loaded: boolean;
}

interface AlertCardOptions {
  dryRun?: boolean;
  history?: boolean;
}

/* Implementation note. */
type AnyAlert = (EmailAlert | EmailAlertRecord) & { keywords?: string[]; matched_keywords?: string[]; timestamp?: string };

export const EmailManager = {
  state: {
    mailboxes: [],
    scan: null,
    history: [],
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
      showToast(error instanceof Error && error.message ? error.message : 'Failed to load mailbox scanning data', 'error');
    } finally {
      setLoading(false);
    }
  },

  async fetchAll(): Promise<void> {
    const [mailboxResult, scanState] = await Promise.all([getMailboxes(), getEmailScanState()]);
    this.state.mailboxes = mailboxResult.emails || [];
    this.state.scan = scanState ?? null;
    this.state.history = AppState.features.history ? await this.fetchHistory() : [];
    this.state.loaded = true;
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
      { label: 'Enabled mailboxes', value: enabledCount },
      { label: 'Scanned emails', value: scanned ? summary.total_emails ?? 0 : '-' },
      {
        label: 'Alert emails',
        value: scanned ? summary.total_alerts ?? 0 : '-',
        cls: (summary.total_alerts ?? 0) > 0 ? 'warning' : '',
      },
      { label: 'Notifications sent', value: scanned ? summary.alerts_sent ?? 0 : '-' },
      { label: 'Last scan', value: scanned ? getRelativeTime(scan?.last_update) : 'Not scanned yet' },
    ];

    if (scanned && scan) {
      chips.push({ label: 'Scan range', value: `Last ${scan.days ?? '-'} days` });
      chips.push({
        label: 'Alert mode',
        value: scan.dry_run ? 'Dry run; no notifications' : 'Send real notifications',
        cls: scan.dry_run ? '' : 'danger',
      });
      if ((summary.failed_mailboxes ?? 0) > 0) {
        chips.push({ label: 'Connection failed', value: summary.failed_mailboxes ?? 0, cls: 'danger' });
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
        'No mailboxes yet',
        AppState.features.dynamic_config
          ? 'Click "Add mailbox" to configure an IMAP mailbox'
          : 'Set EMAIL_HOST / EMAIL_USERNAME / EMAIL_PASSWORD, or enable ENABLE_DYNAMIC_CONFIG to add one here',
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
      container.innerHTML = emptyState('Not scanned yet', 'Choose a date range and click "Scan now" to see results here', 'mail', true);
      return;
    }

    const alerts = scan.alerts || [];
    if (alerts.length === 0) {
      container.innerHTML = emptyState(
        'No alert emails',
        `No billing or renewal keywords matched in the last ${scan.days} days`,
        'mail',
        true,
      );
      return;
    }

    container.innerHTML = alerts.map((alert) => renderAlertCard(alert, { dryRun: scan.dry_run ?? false })).join('');
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
        ? emptyState('No history yet', 'Emails alerted by scheduled or Web scans are stored in the database', 'mail', true)
        : history.map((record) => renderAlertCard(record, { history: true })).join('');
  },

  // Implementation note.

  async runScan(): Promise<void> {
    const btn = byId<HTMLButtonElement>('email-scan-btn');
    const days = Number.parseInt(byId<HTMLSelectElement>('email-scan-days')?.value ?? '', 10) || 1;

    if (!this.state.mailboxes.some((m) => m.enabled !== false)) {
      showToast('No mailboxes available; configure one before scanning', 'warning');
      return;
    }

    try {
      if (btn) btn.disabled = true;
      showToast(`Scanning the last ${days} days; connecting to mailboxes may take a few seconds...`, 'info');

      const result = await runEmailScan(days);
      await this.fetchAll();
      this.renderAll();

      const summary = result.summary ?? {};
      const alerts = summary.total_alerts ?? 0;
      showToast(
        `Scan complete: ${summary.total_emails ?? 0} emails, ${alerts} alerts${result.dry_run ? ' (dry run; no notifications)' : ''}`,
        alerts > 0 ? 'warning' : 'success',
      );
    } catch (error) {
      console.error('Mailbox scan failed:', error);
      showToast(error instanceof Error && error.message ? error.message : 'Scan failed; please try again', 'error');
    } finally {
      if (btn) btn.disabled = false;
    }
  },
};

// Implementation note.

export function renderMailboxCard(mailbox: MailboxConfig, stat: MailboxResult | undefined): string {
  const name = mailbox.name || mailbox.username || 'Unnamed';
  const nameAttr = escapeAttr(name);
  const port = mailbox.port || DEFAULT_PORT;
  const enabled = mailbox.enabled !== false;

  let statusHtml: string;
  if (!enabled) {
    statusHtml = '<span class="status-badge muted">Disabled</span>';
  } else if (!stat) {
    statusHtml = '<span class="status-badge muted">Not scanned</span>';
  } else if (stat.error) {
    statusHtml = `<span class="status-badge danger" title="${escapeAttr(stat.error)}">Connection failed</span>`;
  } else if (stat.alert_count > 0) {
    statusHtml = `<span class="status-badge warning">${escapeHTML(stat.alert_count)}  alerts</span>`;
  } else {
    statusHtml = '<span class="status-badge success">Healthy</span>';
  }

  const actions = AppState.features.dynamic_config
    ? `
            <div class="subscription-actions">
                <button class="action-icon-btn js-edit-email" data-name="${nameAttr}" title="Edit">
                    ${ICON_EDIT}
                </button>
                <button class="action-icon-btn danger js-delete-email" data-name="${nameAttr}" title="Delete">
                    ${ICON_DELETE}
                </button>
            </div>`
    : '';

  const scannedMeta =
    stat && !stat.error ? `<span class="meta-item"><span class="k">Last scan</span>${escapeHTML(stat.total_emails)} emails</span>` : '';
  const errorMeta = stat && stat.error ? `<span class="meta-item error-text">${escapeHTML(stat.error)}</span>` : '';

  return `
            <div class="subscription-card mailbox-card ${enabled ? '' : 'disabled'}">
                <div class="subscription-info">
                    <h3>${escapeHTML(name)}</h3>
                    <div class="subscription-meta">
                        <span class="meta-item">${escapeHTML(mailbox.username || '-')}</span>
                        <span class="meta-item">${escapeHTML(mailbox.host || '-')}:${escapeHTML(port)}</span>
                        <span class="meta-item">${mailbox.use_ssl === false ? 'Plaintext' : 'SSL'}</span>
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
    badge = '<span class="status-badge muted">Already notified; skipped</span>';
  } else if (alert.alert_sent) {
    badge = '<span class="status-badge success">Notification sent</span>';
  } else if (options.dryRun) {
    badge = '<span class="status-badge info">Dry run</span>';
  } else {
    badge = '<span class="status-badge danger">Notification not sent</span>';
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
                    <div class="email-alert-subject">${escapeHTML(alert.subject || '(No subject)')}</div>
                    <div class="subscription-meta">
                        <span class="meta-item project-meta">${escapeHTML(alert.mailbox || '-')}</span>
                        <span class="meta-item"><span class="k">Sender</span>${escapeHTML(alert.sender || '-')}</span>
                        <span class="meta-item">${escapeHTML(alert.date || '-')}</span>
                        ${serviceName ? `<span class="meta-item"><span class="k">Service</span>${escapeHTML(serviceName)}</span>` : ''}
                        ${hasAmount ? `<span class="meta-item"><span class="k">Amount</span>${formatCurrency(alert.amount)}</span>` : ''}
                    </div>
                    ${keywordTags ? `<div class="keyword-tags">${keywordTags}</div>` : ''}
                </div>
                <div class="email-alert-side">
                    ${badge}
                    ${options.history && alert.timestamp ? `<span>Recorded ${escapeHTML(formatDate(alert.timestamp))}</span>` : ''}
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
    if (title) title.textContent = 'Edit mailbox';
    setInputValue('email-edit-mode', 'true');
    nameInput.value = mailbox.name || '';
    nameInput.readOnly = true; // The name is the stable key; delete and recreate it to rename.
    setInputValue('email-host', mailbox.host || '');
    setInputValue('email-port', mailbox.port || DEFAULT_PORT);
    setInputValue('email-username', mailbox.username || '');
    setChecked('email-use-ssl', mailbox.use_ssl !== false);
    setChecked('email-enabled', mailbox.enabled !== false);
    if (passwordHint) passwordHint.textContent = 'Leave empty to keep the existing password';
  } else {
    if (title) title.textContent = 'Add mailbox';
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
    ['email-name', 'Display name is required'],
    ['email-host', 'IMAP server is required'],
    ['email-username', 'Email account is required'],
  ];
  if (!isEdit) required.push(['email-password', 'A password or app password is required for a new mailbox']);
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

  const result = await mutate(ENDPOINTS.saveEmail, data, { success: isEdit ? 'Mailbox updated' : 'Mailbox added', fail: 'Save failed' });
  if (result) {
    closeEmailModal();
    await EmailManager.load(true);
  }
}

export function editEmail(name: string): void {
  const mailbox = EmailManager.state.mailboxes.find((m) => m.name === name);
  if (!mailbox) {
    showToast('Mailbox not found', 'error');
    return;
  }
  openEmailModal(mailbox);
}

export async function deleteEmail(name: string): Promise<void> {
  const confirmed = await confirmDialog({
    title: `Delete mailbox "${name}"?`,
    message: 'This action cannot be undone.',
    confirmLabel: 'Delete mailbox',
  });
  if (!confirmed) return;
  if (await mutate(ENDPOINTS.deleteEmail, { name }, { success: 'Mailbox deleted', fail: 'Delete failed' })) {
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
