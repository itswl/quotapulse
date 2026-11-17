// The three small controls that are always within reach: theme, language,
// sign out. The shell shows them in the rail or the top bar; the sign-in
// screen shows the first two.
import { LogOut, Moon, Sun } from 'lucide-preact';
import { IconButton } from './components';
import { lang, setLang, t } from './i18n';
import { signOut } from './state/session';
import { theme, toggleTheme } from './theme';

export function ShellTools({ signOut: withSignOut = true }: { signOut?: boolean }) {
  const dark = theme.value === 'dark';
  const toChinese = lang.value === 'en';
  return (
    <div class="tools">
      <IconButton
        icon={dark ? Sun : Moon}
        label={dark ? t('common.shell.theme.toLight') : t('common.shell.theme.toDark')}
        onClick={toggleTheme}
      />
      {/* Shows the language it switches to, named in that language, so
          someone who cannot read the current one can still find it. */}
      <button
        type="button"
        class="icon-btn tools__lang"
        lang={toChinese ? 'zh-CN' : 'en'}
        aria-label={toChinese ? '切换到中文' : 'Switch to English'}
        title={toChinese ? '切换到中文' : 'Switch to English'}
        onClick={() => setLang(toChinese ? 'zh' : 'en')}
      >
        {toChinese ? '中' : 'EN'}
      </button>
      {withSignOut && <IconButton icon={LogOut} label={t('common.shell.signOut')} onClick={() => void signOut().catch(() => undefined)} />}
    </div>
  );
}
