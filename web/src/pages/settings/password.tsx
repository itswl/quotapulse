// The password the dashboard is signed in with. Chosen in the dashboard, it
// is changed here; set by QUOTAPULSE_PASSWORD on the server, it can only be
// changed there, and this says so (internal/api/access.go).
import { Eye, EyeOff } from 'lucide-preact';
import type { TargetedEvent } from 'preact';
import { useState } from 'preact/hooks';
import { ApiError } from '../../api/client';
import { changePassword } from '../../api/endpoints';
import { Button, Card, Field, IconButton, Input, toast } from '../../components';
import { errorText } from '../../errors';
import { t } from '../../i18n';
import { sessionInfo } from '../../state/session';
import { RESET_COMMAND } from '../login/setup';

/** The shortest password the server takes. */
const MIN_PASSWORD = 8;

export function PasswordSection() {
  const fromEnv = sessionInfo.value?.password_from_env === true;
  return (
    <>
      <p class="st-intro">{t('settings.password.intro')}</p>
      {fromEnv ? (
        <Card class="st-password">
          <h2 class="st-password__title">{t('settings.password.env.title')}</h2>
          <p class="st-password__body">{t('settings.password.env.body')}</p>
        </Card>
      ) : (
        <ChangeForm />
      )}
      {!fromEnv && (
        <div class="st-password__forgot">
          <p>{t('settings.password.forgot')}</p>
          <pre>
            <code>{RESET_COMMAND}</code>
          </pre>
        </div>
      )}
    </>
  );
}

function ChangeForm() {
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [shown, setShown] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<{ field: 'current' | 'next' | 'form'; text: string } | undefined>(undefined);

  const submit = async (event: TargetedEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (busy) return;
    if ([...next].length < MIN_PASSWORD) {
      setProblem({ field: 'next', text: t('settings.password.short', { n: MIN_PASSWORD }) });
      return;
    }
    setBusy(true);
    setProblem(undefined);
    try {
      await changePassword(current, next);
      setCurrent('');
      setNext('');
      toast(t('settings.password.changed'));
    } catch (cause) {
      if (cause instanceof ApiError && cause.code === 'wrong_password') setProblem({ field: 'current', text: t('settings.password.wrong') });
      else if (cause instanceof ApiError && cause.code === 'password_short') setProblem({ field: 'next', text: t('settings.password.short', { n: MIN_PASSWORD }) });
      else if (cause instanceof ApiError && cause.code === 'too_many_attempts') setProblem({ field: 'form', text: t('common.login.tooMany') });
      else setProblem({ field: 'form', text: errorText(cause) });
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card>
      <form class="st-password__form" onSubmit={submit}>
        {/* Lets a password manager update the password it keeps. */}
        <input class="sr-only" type="text" name="username" autoComplete="username" value="quotapulse" tabIndex={-1} aria-hidden="true" readOnly />
        <Field label={t('settings.password.current')} error={problem?.field === 'current' ? problem.text : undefined}>
          <Input type="password" name="current-password" autoComplete="current-password" value={current} onInput={setCurrent} required />
        </Field>
        <Field
          label={t('settings.password.new')}
          help={t('settings.password.new.help', { n: MIN_PASSWORD })}
          error={problem?.field === 'next' ? problem.text : undefined}
        >
          <Input
            type={shown ? 'text' : 'password'}
            name="new-password"
            autoComplete="new-password"
            value={next}
            onInput={setNext}
            suffix={
              <IconButton
                size="sm"
                icon={shown ? EyeOff : Eye}
                label={shown ? t('common.setup.hide') : t('common.setup.show')}
                onClick={() => setShown(!shown)}
              />
            }
            required
          />
        </Field>
        {problem?.field === 'form' && (
          <p class="form-error" role="alert">
            {problem.text}
          </p>
        )}
        <div class="st-password__foot">
          <Button type="submit" variant="primary" busy={busy}>
            {t('settings.password.submit')}
          </Button>
        </div>
      </form>
    </Card>
  );
}
