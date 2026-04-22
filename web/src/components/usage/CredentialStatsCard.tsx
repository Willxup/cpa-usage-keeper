import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { formatCompactNumber } from '@/utils/usage';
import type { UsageCredential } from '@/lib/types';
import styles from '@/pages/UsagePage.module.scss';

export interface CredentialStatsCardProps {
  credentials: UsageCredential[];
  loading: boolean;
}

interface CredentialRow {
  key: string;
  displayName: string;
  type: string;
  success: number;
  failure: number;
  total: number;
  successRate: number;
}

function CredentialStatsTitle({ title, subtitle, eyebrow }: { title: string; subtitle: string; eyebrow: string }) {
  return (
    <div className={styles.sectionTitleBlock}>
      <span className={styles.sectionEyebrow}>{eyebrow}</span>
      <h3 className={styles.sectionTitle}>{title}</h3>
      <p className={styles.sectionSubtitle}>{subtitle}</p>
    </div>
  );
}

export function CredentialStatsCard({
  credentials,
  loading,
}: CredentialStatsCardProps) {
  const { t } = useTranslation();

  const rows = useMemo((): CredentialRow[] => {
    return credentials
      .map((credential) => {
        const displayName = String(credential.source ?? '').trim() || '-';
        const sourceType = String(credential.source_type ?? '').trim();
        const key = String(credential.source_key ?? '').trim() || displayName;
        const success = Number(credential.success_count) || 0;
        const failure = Number(credential.failure_count) || 0;
        const total = Number(credential.total_count) || success + failure;
        return {
          key,
          displayName,
          type: sourceType,
          success,
          failure,
          total,
          successRate: total > 0 ? (success / total) * 100 : 100,
        };
      })
      .sort((a, b) => b.total - a.total);
  }, [credentials]);

  return (
    <Card
      title={
        <CredentialStatsTitle
          eyebrow={t('usage_stats.credential_stats_eyebrow')}
          title={t('usage_stats.credential_stats_title')}
          subtitle={t('usage_stats.credential_stats_subtitle')}
        />
      }
      className={styles.detailsFixedCard}
    >
      {loading ? (
        <div className={styles.hint}>{t('common.loading')}</div>
      ) : rows.length > 0 ? (
        <div className={styles.detailsScroll}>
        <div className={styles.tableWrapper}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th>{t('usage_stats.credential_name')}</th>
                <th>{t('usage_stats.requests_count')}</th>
                <th>{t('usage_stats.success_rate')}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.key}>
                  <td className={styles.modelCell}>
                    <span>{row.displayName}</span>
                    {row.type && <span className={styles.credentialType}>{row.type}</span>}
                  </td>
                  <td>
                    <span className={styles.requestCountCell}>
                      <span>{formatCompactNumber(row.total)}</span>
                      <span className={styles.requestBreakdown}>
                        (<span className={styles.statSuccess}>{row.success.toLocaleString()}</span>{' '}
                        <span className={styles.statFailure}>{row.failure.toLocaleString()}</span>)
                      </span>
                    </span>
                  </td>
                  <td>
                    <span
                      className={
                        row.successRate >= 95
                          ? styles.statSuccess
                          : row.successRate >= 80
                            ? styles.statNeutral
                            : styles.statFailure
                      }
                    >
                      {row.successRate.toFixed(1)}%
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        </div>
      ) : (
        <div className={styles.hint}>{t('usage_stats.no_data')}</div>
      )}
    </Card>
  );
}
