// This device: how the dashboard looks and which language it speaks here,
// and the way out. None of it is the server's business. It is kept in this
// browser and takes effect at once, so unlike the groups below there is
// nothing to save.
import { LogOut } from 'lucide-preact';
import { Button, Card, Field, Segmented } from '../../components';
import { lang, setLang, t, type Lang } from '../../i18n';
import { signOut } from '../../state/session';
import { setTheme, themeChoice, type ThemeChoice } from '../../theme';
import { Block } from './parts';

export function DeviceBlock() {
  return (
    <Block title={t('settings.device.title')} about={t('settings.device.about')}>
      <Card>
        <div class="st-prefs">
          <Field label={t('settings.device.theme')} help={t('settings.device.theme.help')}>
            <Segmented<ThemeChoice>
              value={themeChoice.value}
              onChange={setTheme}
              options={[
                { value: 'system', label: t('settings.device.theme.system') },
                { value: 'light', label: t('settings.device.theme.light') },
                { value: 'dark', label: t('settings.device.theme.dark') },
              ]}
            />
          </Field>
          {/* Each language is named in itself, so someone who cannot read
              the current one can still find their own. */}
          <Field label={t('settings.device.language')} help={t('settings.device.language.help')}>
            <Segmented<Lang>
              value={lang.value}
              onChange={setLang}
              options={[
                { value: 'zh', label: t('settings.prefs.language.zh') },
                { value: 'en', label: t('settings.prefs.language.en') },
              ]}
            />
          </Field>
          <div class="st-prefs__foot">
            <Button icon={LogOut} onClick={() => void signOut().catch(() => undefined)}>
              {t('common.shell.signOut')}
            </Button>
          </div>
        </div>
      </Card>
    </Block>
  );
}
