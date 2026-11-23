// Choosing the password of an install that has none: the first thing anyone
// sees of a new install, and where `quotapulse reset-password` leads back to.
//
// A new install is open to whoever opens it within an hour of its start.
// Later than that, or with something already in it, it takes the one-time
// code the server writes to its log, which arrives in the link as
// #setup=<code> (internal/api/access.go). A new install is also asked its
// time zone and language here, so that nothing about it has to be set on the
// server.
import { Eye, EyeOff, KeyRound } from 'lucide-preact';
import type { TargetedEvent } from 'preact';
import { useEffect, useMemo, useState } from 'preact/hooks';
import { ApiError } from '../../api/client';
import type { SessionInfo } from '../../api/types';
import { Button, Field, IconButton, Input, Segmented } from '../../components';
import { errorText } from '../../errors';
import { lang, setLang, t, type Lang } from '../../i18n';
import { checkSession, setUp } from '../../state/session';
import { browserZone, ZoneSelect } from '../settings/zone-select';

/** The shortest password the server takes (internal/api/access.go). */
const MIN_PASSWORD = 8;

/** The one-time code a setup link carries in its fragment, which never
 *  reaches a server's or a proxy's log. */
function codeFromLink(): string | undefined {
  return new URLSearchParams(location.hash.slice(1)).get('setup')?.trim() || undefined;
}

/** The command that prints a new setup link, as most installs run it. */
export const RESET_COMMAND = 'docker compose exec quotapulse /quotapulse reset-password';
/** Where the link the server logs while there is no password can be read. */
const LOG_COMMAND = 'docker compose logs quotapulse';

export function SetupForm({ info }: { info: SessionInfo }) {
  const code = useMemo(codeFromLink, []);
  const [password, setPassword] = useState('');
  const [shown, setShown] = useState(false);
  const [zone, setZone] = useState(browserZone);
  const [error, setError] = useState<string | undefined>(undefined);
  const [busy, setBusy] = useState(false);
  // Seconds left before the server will look at another attempt.
  const [wait, setWait] = useState(0);

  useEffect(() => {
    if (wait <= 0) return;
    const timer = setTimeout(() => setWait(wait - 1), 1000);
    return () => clearTimeout(timer);
  }, [wait]);

  if (info.setup === 'code' && !code) return <NeedsLink />;

  const submit = async (event: TargetedEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (busy || wait > 0) return;
    if ([...password].length < MIN_PASSWORD) {
      setError(t('common.setup.short', { n: MIN_PASSWORD }));
      return;
    }
    setBusy(true);
    setError(undefined);
    try {
      await setUp({ password, code, ...(info.fresh ? { timezone: zone, language: lang.value } : {}) });
      // The code is spent: keep it out of the address bar and the history.
      if (location.hash) history.replaceState(history.state, '', location.pathname + location.search);
    } catch (cause) {
      setBusy(false);
      if (cause instanceof ApiError && cause.code === 'password_short') setError(t('common.setup.short', { n: MIN_PASSWORD }));
      else if (cause instanceof ApiError && cause.code === 'setup_code') setError(t('common.setup.badCode'));
      else if (cause instanceof ApiError && cause.code === 'too_many_attempts') {
        setWait(Math.ceil(cause.retryAfter ?? 0));
        setError(t('common.login.tooMany'));
      } else if (cause instanceof ApiError && cause.code === 'password_set') {
        // Somebody chose it a moment ago: what is left is to sign in.
        await checkSession();
      } else setError(errorText(cause));
    }
  };

  const message = wait > 0 ? t('common.login.tooManyFor', { n: wait }) : error;

  return (
    <form class="login__form setup" onSubmit={submit}>
      <header class="setup__head">
        <h2 class="setup__title">{info.fresh ? t('common.setup.welcome') : t('common.setup.choose')}</h2>
        <p class="setup__body">{info.fresh ? t('common.setup.welcome.body') : t('common.setup.choose.body')}</p>
      </header>
      {/* Lets a password manager file the new password under a name. */}
      <input class="sr-only" type="text" name="username" autoComplete="username" value="quotapulse" tabIndex={-1} aria-hidden="true" readOnly />
      <Field label={t('common.setup.password')} help={t('common.setup.password.help', { n: MIN_PASSWORD })} error={message}>
        <Input
          type={shown ? 'text' : 'password'}
          name="new-password"
          autoComplete="new-password"
          value={password}
          onInput={(value) => {
            setPassword(value);
            if (wait === 0) setError(undefined);
          }}
          prefix={<KeyRound size={14} />}
          suffix={
            <IconButton
              class="setup__reveal"
              icon={shown ? EyeOff : Eye}
              label={shown ? t('common.setup.hide') : t('common.setup.show')}
              onClick={() => setShown(!shown)}
            />
          }
          autoFocus
          required
        />
      </Field>
      {info.fresh && (
        <>
          {/* Each language is named in itself, so someone who cannot read
              the current one can still find their own. */}
          <Field label={t('common.setup.language')} help={t('common.setup.language.help')}>
            <Segmented<Lang>
              value={lang.value}
              onChange={setLang}
              options={[
                { value: 'zh', label: '中文' },
                { value: 'en', label: 'English' },
              ]}
            />
          </Field>
          <Field label={t('common.setup.zone')} help={t('common.setup.zone.help')}>
            <ZoneSelect value={zone} onChange={setZone} />
          </Field>
        </>
      )}
      <Button type="submit" variant="primary" block busy={busy} disabled={wait > 0}>
        {info.fresh ? t('common.setup.submit') : t('common.setup.use')}
      </Button>
    </form>
  );
}

/** A setup that takes the one-time code, opened without it. */
function NeedsLink() {
  return (
    <div class="setup setup--link" role="status">
      <header class="setup__head">
        <h2 class="setup__title">{t('common.setup.link.title')}</h2>
        <p class="setup__body">{t('common.setup.link.body')}</p>
      </header>
      <pre class="setup__command">
        <code>{RESET_COMMAND}</code>
      </pre>
      <p class="setup__note">{t('common.setup.link.after')}</p>
      <pre class="setup__command">
        <code>{LOG_COMMAND}</code>
      </pre>
    </div>
  );
}
