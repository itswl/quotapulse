// The sign-in screen. It is shown in place of whatever page was asked for,
// at that page's address — so after signing in, the user is where they were
// going, with no redirect to remember. An install with no password yet shows
// its setup here instead (setup.tsx).
import { KeyRound, RefreshCw } from 'lucide-preact';
import type { TargetedEvent } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { ApiError } from '../../api/client';
import { Button, Field, Input, Logo, Spinner } from '../../components';
import { errorText } from '../../errors';
import { useTitle } from '../../hooks';
import { t } from '../../i18n';
import { ShellTools } from '../../shell-tools';
import { checkSession, sessionInfo, sessionStatus, signIn } from '../../state/session';
import { RESET_COMMAND, SetupForm } from './setup';

/** The password of an instance started with -demo (internal/demo). */
const DEMO_PASSWORD = 'demo';

export default function LoginPage() {
  const info = sessionInfo.value;
  useTitle(info?.setup ? (info.fresh ? t('common.setup.welcome') : t('common.setup.choose')) : t('common.login.title'));
  const status = sessionStatus.value;
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | undefined>(undefined);
  const [busy, setBusy] = useState(false);
  // Seconds left before the server will look at another attempt.
  const [wait, setWait] = useState(0);
  const form = useRef<HTMLFormElement>(null);

  useEffect(() => {
    if (wait <= 0) return;
    const timer = setTimeout(() => setWait(wait - 1), 1000);
    return () => clearTimeout(timer);
  }, [wait]);

  const focusPassword = () => {
    const input = form.current?.querySelector<HTMLInputElement>('input[type="password"]');
    input?.focus();
    input?.select();
  };

  const submit = async (event: TargetedEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (busy || wait > 0 || password === '') return;
    setBusy(true);
    setError(undefined);
    try {
      await signIn(password);
    } catch (cause) {
      if (cause instanceof ApiError && cause.code === 'wrong_password') {
        setError(t('common.login.wrong'));
      } else if (cause instanceof ApiError && cause.code === 'too_many_attempts') {
        setWait(Math.ceil(cause.retryAfter ?? 0));
        setError(t('common.login.tooMany'));
      } else if (cause instanceof ApiError && cause.code === 'no_password') {
        await checkSession();
      } else {
        setError(errorText(cause));
      }
      setBusy(false);
      requestAnimationFrame(focusPassword);
    }
  };

  const message = wait > 0 ? t('common.login.tooManyFor', { n: wait }) : error;

  return (
    <div class="login">
      <div class="login__tools">
        <ShellTools signOut={false} />
      </div>
      <main class="login__card">
        <div class="login__brand">
          <Logo size={40} class="login__logo" />
          <h1 class="login__name">{t('common.app.name')}</h1>
          <p class="login__tagline">{t('common.app.tagline')}</p>
        </div>

        {status === 'checking' && (
          <p class="login__checking">
            <Spinner /> {t('common.state.loading')}
          </p>
        )}

        {status === 'unreachable' && (
          <div class="login__unreachable" role="alert">
            <h2>{t('common.login.unreachable')}</h2>
            <p>{t('common.error.network')}</p>
            <Button icon={RefreshCw} onClick={() => void checkSession()}>
              {t('common.action.retry')}
            </Button>
          </div>
        )}

        {status === 'signedOut' && info?.setup && <SetupForm info={info} />}

        {status === 'signedOut' && !info?.setup && (
          <form class="login__form" ref={form} onSubmit={submit}>
            {/* Lets a password manager file the password under a name. */}
            <input class="sr-only" type="text" name="username" autoComplete="username" value="quotapulse" tabIndex={-1} aria-hidden="true" readOnly />
            <Field label={t('common.login.password')} error={message}>
              <Input
                type="password"
                name="password"
                autoComplete="current-password"
                value={password}
                onInput={(value) => {
                  setPassword(value);
                  if (wait === 0) setError(undefined);
                }}
                prefix={<KeyRound size={14} />}
                autoFocus
                required
              />
            </Field>
            <Button type="submit" variant="primary" block busy={busy} disabled={wait > 0}>
              {t('common.login.submit')}
            </Button>
            {info?.demo ? (
              <p class="login__demo">
                {t('common.login.demo')} <code>{DEMO_PASSWORD}</code>
              </p>
            ) : (
              <details class="login__forgot">
                <summary>{t('common.login.forgot')}</summary>
                <p>{t('common.login.forgot.body')}</p>
                <pre>
                  <code>{RESET_COMMAND}</code>
                </pre>
              </details>
            )}
          </form>
        )}
      </main>
      {info?.version && <p class="login__version num">{t('common.shell.version', { v: info.version })}</p>}
    </div>
  );
}
