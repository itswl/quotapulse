// One notification — the API calls it a delivery — as a row, and its message
// roughly as it was sent. The notification log uses the full row; under an
// event the same row is shorter: the event says what the message was about.
import { Ban, Check, ChevronDown, ChevronUp, CircleX, Clock, Moon, RotateCw, type LucideIcon } from 'lucide-preact';
import { useState } from 'preact/hooks';
import { retryDelivery } from '../../api/endpoints';
import type { Delivery, DeliveryEvent, Message } from '../../api/types';
import { Button, Chip, cx, IconButton, toast, type Tone } from '../../components';
import { errorText } from '../../errors';
import { formatClock, formatDateTime, formatTime, toMs } from '../../format';
import { t, type TKey } from '../../i18n';
import { Link } from '../../router';
import { now } from '../../state/clock';
import { boldRuns, deliveryState, dropReason, isLocalPath, type DeliveryState, type DropReason } from './logic';

// A state is an icon and a word; the colour only backs them up.
const STATE: Record<DeliveryState, { icon: LucideIcon; tone: Tone; label: TKey }> = {
  sent: { icon: Check, tone: 'ok', label: 'activity.status.sent' },
  failed: { icon: CircleX, tone: 'critical', label: 'activity.status.failed' },
  dropped: { icon: Ban, tone: 'neutral', label: 'activity.status.dropped' },
  held: { icon: Moon, tone: 'neutral', label: 'activity.status.held' },
  retrying: { icon: RotateCw, tone: 'warning', label: 'activity.status.retrying' },
  queued: { icon: Clock, tone: 'neutral', label: 'activity.status.queued' },
};

const EVENT: Record<DeliveryEvent, TKey> = {
  opened: 'activity.event.opened',
  escalated: 'activity.event.escalated',
  reminder: 'activity.event.reminder',
  resolved: 'activity.event.resolved',
  digest: 'activity.event.digest',
  test: 'activity.event.test',
};

const DROPPED: Record<DropReason, TKey> = {
  muted: 'activity.drop.muted',
  resolved: 'activity.drop.resolved',
  removed: 'activity.drop.removed',
  channelDeleted: 'activity.drop.channelDeleted',
  channelOff: 'activity.drop.channelOff',
};

/** What there is to say about a notification beyond its state: a sentence of
 *  ours, and the error as the platform or the server put it. */
function explain(delivery: Delivery, state: DeliveryState): { note: string; error: string } {
  switch (state) {
    case 'held':
      return { note: t('activity.note.held', { time: formatClock(delivery.not_before) }), error: '' };
    case 'retrying':
      return { note: t('activity.note.retrying', { n: delivery.attempts, time: formatClock(delivery.not_before) }), error: delivery.last_error };
    case 'failed':
      return { note: delivery.attempts > 0 ? t('activity.note.failed', { n: delivery.attempts }) : '', error: delivery.last_error };
    case 'dropped': {
      const reason = dropReason(delivery.last_error);
      return reason ? { note: t(DROPPED[reason]), error: '' } : { note: t('activity.note.dropped'), error: delivery.last_error };
    }
    case 'sent':
      // Queued in quiet hours and sent in the morning: the row's time is
      // when it was written, so say when it actually left.
      if (delivery.sent_at && toMs(delivery.sent_at) - toMs(delivery.created_at) > 90_000) {
        return { note: t('activity.note.sentAt', { time: formatClock(delivery.sent_at) }), error: '' };
      }
      return { note: '', error: '' };
    default:
      return { note: '', error: '' };
  }
}

export interface DeliveryItemProps {
  delivery: Delivery;
  /** In the log a row carries the message's title and opens to show the
   *  message, and its day stands above it. Under an event the row is the
   *  short form, and its time says the day when that is not today. */
  context: 'log' | 'event';
  /** A retry was accepted: this is the notification as it stands now. */
  onChange: (next: Delivery) => void;
}

/** Render inside a <ul class="ac-log">. */
export function DeliveryItem({ delivery, context, onChange }: DeliveryItemProps) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const state = deliveryState(delivery, now.value);
  const look = STATE[state];
  // The message is stored JSON; an old or foreign row may lack any part of it.
  const message: Partial<Message> = delivery.message ?? {};
  const title = message.title || t('activity.untitled');
  const at = toMs(delivery.created_at);
  const event = EVENT[delivery.event] as TKey | undefined;
  const { note, error } = explain(delivery, state);
  const panel = `delivery-${delivery.id}`;

  const again = async () => {
    setBusy(true);
    try {
      onChange(await retryDelivery(delivery.id));
      toast(t('activity.retry.done', { channel: delivery.channel }));
    } catch (cause) {
      toast.error(errorText(cause));
    } finally {
      setBusy(false);
    }
  };

  return (
    <li class="ac-delivery">
      <time class="ac-delivery__time num" dateTime={delivery.created_at} title={formatDateTime(at)}>
        {context === 'log' ? formatTime(at) : formatClock(at)}
      </time>
      <span class="ac-delivery__channel truncate" title={delivery.channel}>
        {delivery.channel}
      </span>
      {context === 'log' && (
        <span class="ac-delivery__title truncate" title={title}>
          {title}
        </span>
      )}
      <span class="ac-delivery__chips">
        <span class="ac-delivery__event">
          <Chip>{event ? t(event) : delivery.event}</Chip>
        </span>
        <span class="ac-delivery__status">
          <Chip tone={look.tone} icon={look.icon}>
            {t(look.label)}
          </Chip>
        </span>
      </span>
      {/* Always there, even when empty: the rows share one grid. */}
      <span class="ac-delivery__actions">
        {state === 'failed' && (
          <Button size="sm" icon={RotateCw} busy={busy} aria-label={t('activity.retry.label', { title, channel: delivery.channel })} onClick={again}>
            {t('activity.retry')}
          </Button>
        )}
        {context === 'log' && (
          <IconButton
            size="sm"
            icon={open ? ChevronUp : ChevronDown}
            label={t(open ? 'activity.message.hide' : 'activity.message.show', { title })}
            aria-expanded={open}
            aria-controls={open ? panel : undefined}
            onClick={() => setOpen(!open)}
          />
        )}
      </span>
      {(note || error) && (
        <p class={cx('ac-delivery__note', state === 'failed' && 'ac-delivery__note--failed')}>
          {note}
          {error && <span class="ac-delivery__error">{error}</span>}
        </p>
      )}
      {open && (
        <div class="ac-delivery__message" id={panel}>
          <MessagePreview message={message} />
        </div>
      )}
    </li>
  );
}

/** A message roughly as it was delivered: title, body, facts, and the
 *  sections of a digest. It is in the language notifications are written in,
 *  which need not be the dashboard's. */
export function MessagePreview({ message }: { message: Partial<Message> }) {
  const facts = message.facts ?? [];
  const sections = message.sections ?? [];
  const path = isLocalPath(message.path) ? message.path : null;
  return (
    <div class="ac-msg">
      <p class="ac-msg__title">{message.title || t('activity.untitled')}</p>
      {message.body && <p class="ac-msg__body">{message.body}</p>}
      {facts.length > 0 && (
        <dl class="ac-msg__facts">
          {facts.map((fact) => (
            <div key={fact.label}>
              <dt>{fact.label}</dt>
              <dd class="num">{fact.value}</dd>
            </div>
          ))}
        </dl>
      )}
      {sections.map((section) => (
        <div key={section.title} class="ac-msg__section">
          <p class="ac-msg__heading">{section.title}</p>
          <ul>
            {(section.lines ?? []).map((line, index) => (
              <li key={index}>{boldRuns(line).map((run, at) => (run.bold ? <strong key={at}>{run.text}</strong> : run.text))}</li>
            ))}
          </ul>
        </div>
      ))}
      {(message.footer || path) && (
        <p class="ac-msg__foot">
          {message.footer && <span>{message.footer}</span>}
          {path && <Link href={path}>{t('activity.message.open')}</Link>}
        </p>
      )}
    </div>
  );
}
