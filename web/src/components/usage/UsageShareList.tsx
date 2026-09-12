import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { formatCompactNumber, formatFixedTwoDecimals, formatUsd } from '@/utils/usage';
import styles from '@/pages/UsagePage.module.scss';

export interface UsageShareItem {
  key: string;
  label: string;
  tokens: number;
  requests: number;
  share: number | null;
  cost?: number | null;
  cacheRate?: number | null;
}

function UsageMetaPill({ label, value }: { label: string; value: string }) {
  return (
    <span className={styles.overviewRealtimeUsageMetaPill}>
      <span className={styles.overviewRealtimeUsageMetaLabel}>{label}</span>
      <span className={styles.overviewRealtimeUsageMetaValue}>{value}</span>
    </span>
  );
}

// Overview 与 Realtime 共用展示，数据来源与时间范围由各自页面负责。
export function UsageShareList({ items, loading, emptyContent }: { items: readonly UsageShareItem[]; loading: boolean; emptyContent?: ReactNode }) {
  const { t } = useTranslation();
  return (
    <div className={styles.overviewRealtimeUsageList} aria-busy={loading}>
      {items.length === 0 ? (
        <div className={styles.overviewRealtimeEmpty}>{emptyContent ?? t('usage_stats.overview_realtime_usage_empty')}</div>
      ) : items.map((item) => {
        const share = item.share === null ? null : Number.isFinite(item.share) ? item.share : 0;
        return (
          <div key={item.key} className={styles.overviewRealtimeUsageItem} data-usage-share-item={item.key}>
            <div className={styles.overviewRealtimeUsageTopline}>
              <span className={styles.overviewRealtimeUsageLabel} title={item.label}>{item.label}</span>
              <span className={styles.overviewRealtimeUsageShare}>{share === null ? '—' : `${formatFixedTwoDecimals(share)}%`}</span>
            </div>
            <div className={styles.overviewRealtimeUsageTrack} aria-hidden="true">
              {share !== null && share > 0 && (
                <span className={styles.overviewRealtimeUsageBar} style={{ width: `${Math.max(0, Math.min(100, share))}%` }} />
              )}
            </div>
            <div className={styles.overviewRealtimeUsageMeta}>
              <UsageMetaPill label={t('usage_stats.overview_realtime_tokens_label')} value={formatCompactNumber(item.tokens)} />
              <UsageMetaPill label={t('usage_stats.overview_realtime_requests_label')} value={item.requests.toLocaleString()} />
              {item.cost !== undefined && <UsageMetaPill label={t('usage_stats.overview_realtime_cost_label')} value={item.cost === null ? '—' : formatUsd(item.cost)} />}
              {item.cacheRate !== undefined && <UsageMetaPill label={t('usage_stats.overview_realtime_cache_rate')} value={item.cacheRate === null ? '—' : `${formatFixedTwoDecimals(item.cacheRate)}%`} />}
            </div>
          </div>
        );
      })}
    </div>
  );
}
