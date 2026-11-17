// The account a meter is read from: what it is, how the last read went, and
// the handful of things that can be done to it.
import { ExternalLink, Pencil, Power, Trash2 } from 'lucide-preact';
import type { ComponentChildren } from 'preact';
import type { AccountView, MeterView } from '../../api/types';
import { Banner, Button, Card, StatusDot, type Status } from '../../components';
import { formatAgo, formatDateTime, formatIn, formatSpan, toMs } from '../../format';
import { pick, t } from '../../i18n';
import { navigate } from '../../router';
import { now } from '../../state/clock';
import { providers } from '../../state/providers';
import { useResource } from '../../state/resource';
import { useAccountManager } from './account-manage';

function Row({ label, children }: { label: string; children: ComponentChildren }) {
  return (
    <div class="mt-numbers__row">
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

export function MeterAccount({ meter, account }: { meter: MeterView; account: AccountView | undefined }) {
  useResource(providers);
  // Deleting the account deletes this meter with it: there is nothing left
  // to show here.
  const manage = useAccountManager(account, () => navigate('/meters', { replace: true }));

  if (!account) {
    return (
      <Card class="mt-account-card">
        <h2 class="mt-card-title">{t('meters.account.title')}</h2>
        <p class="mt-account-card__note">{meter.account_id === '' ? t('meters.account.pushed') : t('common.state.loading')}</p>
      </Card>
    );
  }

  const definition = providers.data.value?.find((each) => each.id === account.provider);
  const label = (key: string) => definition?.fields.find((field) => field.key === key)?.label || key;
  const status: Status = !account.enabled ? 'paused' : account.status;
  const span = formatSpan(account.effective_interval_seconds * 1000);
  const nextAhead = account.next_sample_at !== null && toMs(account.next_sample_at) > now.value;

  return (
    <Card class="mt-account-card">
      <div class="mt-account-card__head">
        <h2 class="mt-card-title">{t('meters.account.title')}</h2>
        <StatusDot status={status} label={account.enabled ? undefined : t('meters.account.off')} />
      </div>

      {account.credential_unreadable && (
        <Banner tone="critical" class="mt-numbers__note">
          {t('meters.account.unreadable')}
        </Banner>
      )}

      <dl class="mt-numbers__list mt-account-card__list">
        <Row label={t('meters.form.name')}>{account.name}</Row>
        <Row label={t('meters.account.provider')}>
          {account.provider_name || pick(definition?.name ?? account.provider, definition?.name_zh)}
          {definition?.homepage && (
            <a class="mt-account-card__site" href={definition.homepage} target="_blank" rel="noopener noreferrer" aria-label={t('meters.add.homepage')}>
              <ExternalLink size={12} />
            </a>
          )}
        </Row>
        {account.group && <Row label={t('meters.account.group')}>{account.group}</Row>}
        {Object.entries(account.credential).map(([key, value]) => (
          <Row key={key} label={label(key)}>
            <code class="mt-account-card__secret">{value}</code>
          </Row>
        ))}
        <Row label={t('meters.account.interval')}>{account.interval_seconds === 0 ? t('meters.account.interval.default', { span }) : span}</Row>
        <Row label={t('meters.account.lastRead')}>
          {account.sampled_at ? (
            <>
              <time dateTime={account.sampled_at} title={formatDateTime(account.sampled_at)}>
                {formatAgo(account.sampled_at)}
              </time>
              {account.latency_ms !== null && account.latency_ms > 0 && <span class="mt-numbers__per"> {t('meters.account.took', { ms: account.latency_ms })}</span>}
            </>
          ) : (
            t('meters.account.never')
          )}
        </Row>
        {account.enabled && nextAhead && account.next_sample_at && <Row label={t('meters.account.nextRead')}>{formatIn(account.next_sample_at)}</Row>}
        {account.last_error && (
          <Row label={t('meters.account.lastError')}>
            <span class="mt-account-card__error">{account.last_error}</span>
            {account.consecutive_failures > 1 && <span class="mt-numbers__per"> {t('meters.account.failures', { n: account.consecutive_failures })}</span>}
          </Row>
        )}
      </dl>

      <div class="mt-account-card__actions">
        <Button icon={Pencil} onClick={manage.edit}>
          {t('common.action.edit')}
        </Button>
        <Button icon={Power} busy={manage.toggling} onClick={manage.toggle}>
          {account.enabled ? t('meters.account.turnOff') : t('meters.account.turnOn')}
        </Button>
        <Button icon={Trash2} onClick={manage.remove}>
          {t('common.action.delete')}
        </Button>
      </div>
      {manage.dialogs}
    </Card>
  );
}
