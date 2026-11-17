import { Check } from 'lucide-preact';
import { useEffect, useState } from 'preact/hooks';
import { ackIncident, unackIncident } from '../api/endpoints';
import type { IncidentView } from '../api/types';
import { errorText } from '../errors';
import { formatAgo, formatDateTime } from '../format';
import { t } from '../i18n';
import { Link } from '../router';
import { Button } from './Button';
import { cx } from './cx';
import { StatusDot } from './StatusDot';
import { toast } from './Toast';

export interface IncidentRowProps {
  incident: IncidentView;
  /** Link the headline to the incident's subject. Off on the subject's own page. */
  link?: boolean;
  /** List the numbers behind the sentence. */
  facts?: boolean;
}

/**
 * One open incident: what is wrong, since when, and a way to say "I know".
 * The headline and detail are the server's own sentences, in the language
 * the dashboard asked for; nothing here rewrites them.
 *
 * Render it inside a <ul class="incidents">.
 */
export function IncidentRow({ incident, link = true, facts = false }: IncidentRowProps) {
  // Acknowledging shows at once; the server's answer arrives with the next
  // refresh and replaces this guess.
  const [guess, setGuess] = useState<boolean | null>(null);
  useEffect(() => setGuess(null), [incident.acked_at]);
  const acked = guess ?? incident.acked_at !== null;

  const toggle = async () => {
    const next = !acked;
    setGuess(next);
    try {
      await (next ? ackIncident(incident.id) : unackIncident(incident.id));
    } catch (cause) {
      setGuess(null);
      toast.error(errorText(cause));
    }
  };

  return (
    <li class={cx('incident', acked && 'incident--acked')}>
      <StatusDot class="incident__mark" status={incident.severity} iconOnly size={18} />
      <div class="incident__text">
        <p class="incident__headline">{link ? <Link href={incident.path}>{incident.headline}</Link> : incident.headline}</p>
        {incident.detail && <p class="incident__detail">{incident.detail}</p>}
        {facts && incident.facts.length > 0 && (
          <dl class="incident__facts">
            {incident.facts.map((fact) => (
              <div key={fact.label}>
                <dt>{fact.label}</dt>
                <dd class="num">{fact.value}</dd>
              </div>
            ))}
          </dl>
        )}
        <p class="incident__meta">
          <time dateTime={incident.opened_at} title={formatDateTime(incident.opened_at)}>
            {t('common.incident.opened', { ago: formatAgo(incident.opened_at) })}
          </time>
        </p>
      </div>
      <div class="incident__actions">
        {acked ? (
          <>
            <span class="incident__acked">
              <Check size={14} />
              {t('common.action.acked')}
            </span>
            <Button variant="ghost" size="sm" onClick={toggle}>
              {t('common.action.undo')}
            </Button>
          </>
        ) : (
          <Button size="sm" icon={Check} onClick={toggle}>
            {t('common.action.ack')}
          </Button>
        )}
      </div>
    </li>
  );
}
