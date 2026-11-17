// Channels: where notifications go. The card on top is the device at hand;
// the list under it is every channel, this device's among them.
import { Bell, BellOff, BellRing, MessageSquare, Pencil, Plus, Send, Smartphone, Trash2, Webhook, type LucideIcon } from 'lucide-preact';
import { useState } from 'preact/hooks';
import { deleteChannel, updateChannel } from '../../api/endpoints';
import type { ChannelView } from '../../api/types';
import { Button, Card, Chip, ConfirmDialog, cx, EmptyState, ErrorState, Menu, Section, Skeleton, StatusDot, Switch, toast } from '../../components';
import { errorText } from '../../errors';
import { formatAgo, formatDateTime } from '../../format';
import { useAction } from '../../hooks';
import { t } from '../../i18n';
import { useResource } from '../../state/resource';
import { ChannelDialog, severityLabel, TestResult, topicLabel, useChannelTest } from './channel-dialog';
import { PushError, turnOffPush, turnOnPush, usePush, type PushState } from './push';
import { channels, channelTypes } from './state';

export function ChannelsSection() {
  useResource(channels);
  useResource(channelTypes);
  const push = usePush();
  const [adding, setAdding] = useState(false);

  const list = channels.data.value;
  const mine = push.device ? list?.find((each) => each.type === 'webpush' && each.device === push.device) : undefined;
  const add = (
    <Button variant="primary" size="sm" icon={Plus} onClick={() => setAdding(true)}>
      {t('settings.channels.add')}
    </Button>
  );

  return (
    <>
      <p class="st-intro">{t('settings.channels.intro')}</p>
      <PushCard push={push} channel={mine} ready={list !== undefined} />

      <Section title={t('settings.channels.title')} meta={list && list.length > 0 ? <span class="num">{list.length}</span> : undefined} action={list && list.length > 0 ? add : undefined}>
        {!list ? (
          channels.error.value ? (
            <ErrorState compact error={channels.error.value} onRetry={() => void channels.load()} />
          ) : (
            <div class="st-list" aria-busy="true">
              {[0, 1].map((index) => (
                <Skeleton key={index} height="92px" />
              ))}
            </div>
          )
        ) : list.length === 0 ? (
          <Card>
            <EmptyState compact icon={Bell} title={t('settings.channels.empty.title')} action={add}>
              {t('settings.channels.empty.body')}
            </EmptyState>
          </Card>
        ) : (
          <ul class="st-list">
            {list.map((channel) => (
              <ChannelRow key={channel.id} channel={channel} mine={channel.id === mine?.id} onGone={push.refresh} />
            ))}
          </ul>
        )}
      </Section>
      <ChannelDialog open={adding} onClose={() => setAdding(false)} />
    </>
  );
}

// ---------- this device ----------

function PushCard({ push, channel, ready }: { push: PushState; channel: ChannelView | undefined; ready: boolean }) {
  const [problem, setProblem] = useState<PushError | string | undefined>(undefined);
  const [fresh, setFresh] = useState(false);
  const test = useChannelTest(channel?.id);

  const on = useAction(async () => {
    setProblem(undefined);
    try {
      const made = await turnOnPush();
      channels.set((list) => (list ? [...list.filter((each) => each.id !== made.id), made] : list));
      setFresh(true);
      toast(t('settings.push.turnedOn'));
    } catch (cause) {
      setProblem(cause instanceof PushError ? cause : errorText(cause));
    } finally {
      push.refresh();
      void channels.load();
    }
  });

  const off = useAction(async () => {
    setProblem(undefined);
    test.clear();
    try {
      await turnOffPush(channel);
      channels.set((list) => list?.filter((each) => each.id !== channel?.id));
      setFresh(false);
      toast(t('settings.push.turnedOff'));
    } catch (cause) {
      setProblem(errorText(cause));
    } finally {
      push.refresh();
      void channels.load();
    }
  });

  const title = t('settings.push.title');
  const blocked = push.permission === 'denied' || (problem instanceof PushError && problem.reason === 'denied');

  let state: 'unavailable' | 'blocked' | 'checking' | 'on' | 'off';
  if (push.support !== 'ok') state = 'unavailable';
  else if (channel) state = 'on';
  else if (blocked) state = 'blocked';
  else if (push.device === undefined || !ready) state = 'checking';
  else state = 'off';

  const Icon = state === 'on' ? BellRing : state === 'off' || state === 'checking' ? Bell : BellOff;

  return (
    <Card class={cx('st-push', `st-push--${state}`)}>
      <span class="st-push__icon">
        <Icon size={20} />
      </span>
      <div class="st-push__text">
        <h2 class="st-push__title">
          {title}
          {state === 'on' && <StatusDot status="ok" label={t('common.word.on')} />}
        </h2>

        {state === 'unavailable' && (
          <>
            <p class="st-push__body">{t(`settings.push.${push.support === 'ok' ? 'unsupported' : push.support}`)}</p>
            {push.support === 'ios-install' && <p class="st-push__steps">{t('settings.push.ios-install.steps')}</p>}
          </>
        )}

        {state === 'blocked' && (
          <>
            <p class="st-push__body">{t('settings.push.blocked')}</p>
            <p class="st-push__steps">{t('settings.push.blocked.steps')}</p>
          </>
        )}

        {state === 'checking' && <Skeleton width="min(320px, 100%)" height="1.2em" />}

        {state === 'off' && <p class="st-push__body">{t('settings.push.off')}</p>}

        {state === 'on' && channel && (
          <>
            <p class="st-push__body">{fresh ? t('settings.push.on.fresh', { name: channel.name }) : t('settings.push.on', { name: channel.name })}</p>
            {!channel.enabled && <p class="st-push__steps">{t('settings.push.on.disabled')}</p>}
            <TestResult outcome={test.outcome} />
          </>
        )}

        {problem !== undefined && !blocked && (
          <p class="form-error" role="alert">
            {problem instanceof PushError ? pushProblem(problem) : problem}
          </p>
        )}
      </div>

      <div class="st-push__actions">
        {state === 'off' && (
          <Button variant="primary" icon={BellRing} busy={on.busy} onClick={() => void on.run()}>
            {t('settings.push.turnOn')}
          </Button>
        )}
        {state === 'blocked' && <Button onClick={push.refresh}>{t('settings.push.blocked.recheck')}</Button>}
        {state === 'on' && (
          <>
            <Button icon={Send} variant={fresh && !test.outcome ? 'primary' : 'secondary'} busy={test.busy} disabled={off.busy} onClick={test.run}>
              {t('settings.channels.test')}
            </Button>
            <Button icon={BellOff} busy={off.busy} disabled={test.busy} onClick={() => void off.run()}>
              {t('settings.push.turnOff')}
            </Button>
          </>
        )}
      </div>
    </Card>
  );
}

function pushProblem(problem: PushError): string {
  switch (problem.reason) {
    case 'dismissed':
      return t('settings.push.problem.dismissed');
    case 'worker':
      return t('settings.push.problem.worker', { detail: problem.message });
    case 'subscribe':
      return t('settings.push.problem.subscribe', { detail: problem.message });
    default:
      return t('settings.push.blocked');
  }
}

// ---------- the list ----------

const ICONS: Record<string, LucideIcon> = { webpush: Smartphone, webhook: Webhook, ntfy: Bell, bark: Bell };

/** Where a channel points, from its masked settings: the address, or what
 *  stands in for one. A setting masked all the way says nothing. */
function targetOf(channel: ChannelView): string {
  const parts = ['url', 'endpoint', 'server', 'chat_id', 'topic'].map((key) => channel.config[key]).filter((value): value is string => Boolean(value) && value !== '••••');
  return parts.join(' · ');
}

function ChannelRow({ channel, mine, onGone }: { channel: ChannelView; mine: boolean; onGone: () => void }) {
  const [editing, setEditing] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const test = useChannelTest(channel.id);

  const replace = (next: ChannelView) => channels.set((list) => list?.map((each) => (each.id === next.id ? next : each)));

  const toggle = useAction(async (enabled: boolean) => {
    try {
      const next = await updateChannel(channel.id, { enabled });
      replace(next);
      toast(t(next.enabled ? 'settings.channels.turnedOn' : 'settings.channels.turnedOff', { name: next.name }));
    } catch (cause) {
      toast.error(errorText(cause));
    }
  });

  const remove = useAction(async () => {
    // Deleting this device's own channel is turning its notifications off:
    // the browser should forget its subscription too.
    if (mine) await turnOffPush(channel);
    else await deleteChannel(channel.id);
    channels.set((list) => list?.filter((each) => each.id !== channel.id));
    setConfirming(false);
    toast(t('settings.channels.deleted', { name: channel.name }));
    if (mine) onGone();
  });

  const Icon = ICONS[channel.type] ?? MessageSquare;
  const target = targetOf(channel);
  const { health } = channel;
  const topics = channel.topics.length > 0 ? channel.topics.map(topicLabel).join(t('common.list.separator')) : t('settings.channels.row.noTopics');

  return (
    <li class={cx('st-item', !channel.enabled && 'is-off')}>
      <span class="st-item__icon">
        <Icon size={18} />
      </span>
      <div class="st-item__text">
        <p class="st-item__name">
          <span class="st-item__title">{channel.name}</span>
          {mine && <Chip tone="accent">{t('settings.channels.row.thisDevice')}</Chip>}
        </p>
        <p class="st-item__meta">
          <span>{channel.type_name}</span>
          {target && <span class="st-item__target">{target}</span>}
        </p>
        <p class="st-item__meta">
          <span>{topics}</span>
          <span>{severityLabel(channel.min_severity)}</span>
        </p>
        <p class="st-item__health">
          {!channel.enabled ? (
            <StatusDot status="paused" label={t('settings.channels.row.off')} />
          ) : health.last_at === null ? (
            <StatusDot status="pending" label={t('settings.channels.health.none')} />
          ) : health.last_status === 'failed' ? (
            <>
              <StatusDot status="warning" label={t('settings.channels.health.failing')} />
              <time dateTime={health.last_at} title={formatDateTime(health.last_at)}>
                {t('settings.channels.health.failedAt', { ago: formatAgo(health.last_at) })}
              </time>
              {health.failed_in_a_row > 1 && <span>{t('settings.channels.health.streak', { n: health.failed_in_a_row })}</span>}
            </>
          ) : (
            <>
              <StatusDot status="ok" label={t('settings.channels.health.ok')} />
              <time dateTime={health.last_at} title={formatDateTime(health.last_at)}>
                {t('settings.channels.health.sentAt', { ago: formatAgo(health.last_at) })}
              </time>
            </>
          )}
        </p>
        {channel.enabled && health.last_status === 'failed' && health.last_error && <p class="st-item__error">{health.last_error}</p>}
        <TestResult outcome={test.outcome} />
      </div>
      <div class="st-item__actions">
        <Switch checked={channel.enabled} disabled={toggle.busy} aria-label={t('settings.channels.row.enabled', { name: channel.name })} onChange={(enabled) => void toggle.run(enabled)} />
        <Button size="sm" icon={Send} busy={test.busy} onClick={test.run}>
          {t('settings.channels.test')}
        </Button>
        <Menu
          label={t('settings.channels.row.actions', { name: channel.name })}
          entries={[
            { label: t('common.action.edit'), icon: Pencil, onSelect: () => setEditing(true) },
            { separator: true },
            {
              label: t('common.action.delete'),
              icon: Trash2,
              danger: true,
              onSelect: () => {
                remove.clear();
                setConfirming(true);
              },
            },
          ]}
        />
      </div>
      <ChannelDialog open={editing} onClose={() => setEditing(false)} channel={channel} />
      <ConfirmDialog
        open={confirming}
        onClose={() => setConfirming(false)}
        title={t('settings.channels.delete.title', { name: channel.name })}
        confirmLabel={t('settings.channels.delete')}
        busy={remove.busy}
        error={remove.error === undefined ? undefined : errorText(remove.error)}
        onConfirm={() => void remove.run()}
      >
        <p>{mine ? t('settings.channels.delete.bodyMine') : t('settings.channels.delete.body')}</p>
      </ConfirmDialog>
    </li>
  );
}
