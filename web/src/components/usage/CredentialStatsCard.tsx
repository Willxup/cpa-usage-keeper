import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { collectUsageDetails, formatCompactNumber } from '@/utils/usage';
import type { GeminiKeyConfig, ProviderKeyConfig, OpenAIProviderConfig } from '@/types';
import type { UsagePayload } from './hooks/useUsageData';
import styles from '@/pages/UsagePage.module.scss';

export interface CredentialStatsCardProps {
  usage: UsagePayload | null;
  loading: boolean;
  geminiKeys: GeminiKeyConfig[];
  claudeConfigs: ProviderKeyConfig[];
  codexConfigs: ProviderKeyConfig[];
  vertexConfigs: ProviderKeyConfig[];
  openaiProviders: OpenAIProviderConfig[];
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
  usage,
  loading,
  geminiKeys,
  claudeConfigs,
  codexConfigs,
  vertexConfigs,
  openaiProviders,
}: CredentialStatsCardProps) {
  const { t } = useTranslation();

  const rows = useMemo((): CredentialRow[] => {
    if (!usage) return [];
    const details = collectUsageDetails(usage);
    const buckets = new Map<string, CredentialRow>();

    details.forEach((detail) => {
      const displayName = String(detail.source_display ?? detail.source ?? '').trim() || '-';
      const sourceType = String(detail.source_type ?? '').trim();
      const resolvedKey = String(detail.source_key ?? '').trim() || displayName;
      const existing = buckets.get(resolvedKey) ?? {
        key: resolvedKey,
        displayName,
        type: sourceType,
        success: 0,
        failure: 0,
        total: 0,
        successRate: 100,
      };
      if (detail.failed === true) {
        existing.failure += 1;
      } else {
        existing.success += 1;
      }
      existing.total = existing.success + existing.failure;
      existing.successRate = existing.total > 0 ? (existing.success / existing.total) * 100 : 100;
      buckets.set(resolvedKey, existing);
    });

    return Array.from(buckets.values()).sort((a, b) => b.total - a.total);
  }, [usage]);

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
