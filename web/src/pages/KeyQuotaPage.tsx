import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { KeyViewerShell } from '@/features/key-viewer/KeyViewerShell';
import type { KeyViewerPath } from '@/features/key-viewer/navigation';
import type { AuthSessionAPIKeySummary } from '@/lib/types';
import {
  ApiError,
  fetchKeyQuota,
  type ViewerQuotaAccount,
  type ViewerQuotaRow,
} from '@/lib/api';
import { Card } from '@/components/ui/Card';
import styles from './KeyQuotaPage.module.scss';
import { startQuotaPolling } from '@/lib/quotaPolling';

export function quotaRemaining(row: ViewerQuotaRow): string {
  if (row.remaining_percent != null)
    return `${Math.max(0, Math.min(100, row.remaining_percent)).toLocaleString(undefined, { maximumFractionDigits: 1 })}%`;
  if (row.remaining != null)
    return row.remaining.toLocaleString(undefined, {
      maximumFractionDigits: 2,
    });
  return '—';
}

export function KeyQuotaPage({
  apiKey,
  onNavigate,
  onAuthRequired,
}: {
  apiKey?: AuthSessionAPIKeySummary;
  onNavigate: (path: KeyViewerPath) => void;
  onAuthRequired?: () => void;
}) {
  const { t } = useTranslation();
  const [accounts, setAccounts] = useState<ViewerQuotaAccount[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(false);
  const inFlight = useRef(false);
  const refresh = useCallback(
    async (signal?: AbortSignal) => {
      if (inFlight.current) return;
      inFlight.current = true;
      setLoading(true);
      try {
        const data = await fetchKeyQuota(signal);
        if (!signal?.aborted) {
          setAccounts(data.accounts);
          setError(false);
        }
      } catch (err) {
        if (!signal?.aborted) {
          if (err instanceof ApiError && err.status === 401) onAuthRequired?.();
          setError(true);
        }
      } finally {
        inFlight.current = false;
        setLoading(false);
      }
    },
    [onAuthRequired],
  );
  useEffect(() => startQuotaPolling(refresh), [refresh]);
  return (
    <KeyViewerShell
      activePage="quota"
      apiKey={apiKey}
      onNavigate={onNavigate}
      onAuthRequired={onAuthRequired}
      onRefresh={() => void refresh()}
      refreshing={loading}
      refreshDisabled={loading}
    >
      <p>{t('key_quota.shared')}</p>
      {error && <p role="alert">{t('key_quota.error')}</p>}
      {!loading && !error && accounts.length === 0 && (
        <p>{t('key_quota.empty')}</p>
      )}
      <div className={styles.accounts}>
        {accounts.map((account) => (
          <Card key={account.label}>
            <h2>{account.label}</h2>
            <p
              className={
                account.status === 'available' ? undefined : styles.notice
              }
            >
              {t(`key_quota.${account.status}`)}
            </p>
            {account.updated_at && (
              <p>
                {t('key_quota.updated')}:{' '}
                {new Date(account.updated_at).toLocaleString()}
              </p>
            )}
            {account.rows.map((row, index) => (
              <div className={styles.row} key={index}>
                <div>
                  <strong>{row.label}</strong>
                  {row.metric && (
                    <span className={styles.metric}>{row.metric}</span>
                  )}
                </div>
                <div>
                  {t('key_quota.remaining')}:{' '}
                  <strong>{quotaRemaining(row)}</strong>
                  {row.remaining_percent == null &&
                    row.limit != null &&
                    ` / ${row.limit.toLocaleString()}`}
                </div>
                {row.remaining_percent != null && (
                  <progress
                    max={100}
                    value={Math.max(0, Math.min(100, row.remaining_percent))}
                    aria-label={`${row.label}: ${t('key_quota.remaining')}`}
                  />
                )}
                {row.limit_reached && (
                  <p className={styles.notice}>{t('key_quota.exhausted')}</p>
                )}
                {row.reset_at && (
                  <p>
                    {t('key_quota.resets')}:{' '}
                    {new Date(row.reset_at).toLocaleString()}
                  </p>
                )}
              </div>
            ))}
          </Card>
        ))}
      </div>
    </KeyViewerShell>
  );
}
