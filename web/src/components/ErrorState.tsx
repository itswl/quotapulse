import { CloudOff, RefreshCw } from 'lucide-preact';
import type { ApiError } from '../api/client';
import { errorText } from '../errors';
import { t } from '../i18n';
import { Button } from './Button';
import { EmptyState } from './EmptyState';

/** What a page shows when its data could not be loaded at all. */
export function ErrorState({ error, onRetry, compact }: { error: ApiError | undefined; onRetry?: () => void; compact?: boolean }) {
  return (
    <EmptyState
      icon={CloudOff}
      title={t('common.state.error.title')}
      compact={compact ?? false}
      tone="critical"
      action={
        onRetry && (
          <Button icon={RefreshCw} onClick={onRetry}>
            {t('common.action.retry')}
          </Button>
        )
      }
    >
      {errorText(error)}
    </EmptyState>
  );
}
