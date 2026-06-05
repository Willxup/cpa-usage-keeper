import { useMemo, type CSSProperties, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Line } from 'react-chartjs-2';
import {
  IconDiamond,
  IconDollarSign,
  IconPercent,
  IconSatellite,
  IconTimer,
  IconTrendingUp,
} from '@/components/ui/icons';
import {
  calculateCacheRate,
  formatCompactNumber,
  formatFixedTwoDecimals,
  formatPerMinuteValue,
  formatUsd,
} from '@/utils/usage';
import { sparklineOptions } from '@/utils/usage/chartConfig';
import type { UsageOverviewPayload, UsagePayload } from './hooks/useUsageData';
import type { SparklineBundle } from './hooks/useSparklines';
import styles from '@/pages/UsagePage.module.scss';

interface StatCardData {
  key: string;
  label: string;
  icon: ReactNode;
  accent: string;
  accentSoft: string;
  accentBorder: string;
  value: string;
  meta?: ReactNode;
  trend: SparklineBundle | null;
}

export interface StatCardsProps {
  usage: UsageOverviewPayload | null;
  loading: boolean;
  sparklines: {
    requests: SparklineBundle | null;
    tokens: SparklineBundle | null;
    rpm: SparklineBundle | null;
    tpm: SparklineBundle | null;
    cachedRate: SparklineBundle | null;
    cost: SparklineBundle | null;
  };
}

interface StatCardMetrics {
  requestStats: { successRate: number | null };
  tokenBreakdown: { cachedTokens: number; reasoningTokens: number };
  rateStats: { rpm: number; tpm: number; windowMinutes: number; requestCount: number; tokenCount: number };
  cacheRateStats: { cachedRate: number | null; cachedTokens: number; inputTokens: number };
  totalCost: number;
  costAvailable: boolean;
}

const safeNumber = (value: unknown): number => {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
};

const sumNumberRecord = (record?: Record<string, number>): number => (
  Object.values(record ?? {}).reduce((sum, value) => sum + Math.max(safeNumber(value), 0), 0)
);

const calculateSuccessRate = (usageSnapshot: UsagePayload | null): number | null => {
  const totalRequests = Math.max(safeNumber(usageSnapshot?.total_requests), 0);
  if (totalRequests <= 0) {
    return null;
  }
  return (Math.max(safeNumber(usageSnapshot?.success_count), 0) / totalRequests) * 100;
};

export function buildStatCardMetrics({ usage }: { usage: UsageOverviewPayload | null }): StatCardMetrics {
  // overview 运行态和旧测试夹具的 snapshot 位置不同，这里统一后再计算请求成功率。
  const usageSnapshot = (usage?.usage ?? usage) as UsagePayload | null;
  const requestStats = { successRate: calculateSuccessRate(usageSnapshot) };

  if (!usage?.summary) {
    return {
      requestStats,
      tokenBreakdown: { cachedTokens: 0, reasoningTokens: 0 },
      rateStats: { rpm: 0, tpm: 0, windowMinutes: 1, requestCount: 0, tokenCount: 0 },
      cacheRateStats: { cachedRate: null, cachedTokens: 0, inputTokens: 0 },
      totalCost: 0,
      costAvailable: false,
    };
  }

  const cachedTokens = Math.max(safeNumber(usage.summary.cached_tokens), 0);
  const inputTokens = sumNumberRecord(usage.series?.input_tokens);

  return {
    requestStats,
    tokenBreakdown: {
      cachedTokens,
      reasoningTokens: usage.summary.reasoning_tokens ?? 0,
    },
    rateStats: {
      rpm: usage.summary.rpm ?? 0,
      tpm: usage.summary.tpm ?? 0,
      windowMinutes: usage.summary.window_minutes ?? 1,
      requestCount: usage.summary.request_count ?? 0,
      tokenCount: usage.summary.token_count ?? 0,
    },
    cacheRateStats: {
      cachedRate: calculateCacheRate({ inputTokens, cachedTokens }),
      cachedTokens,
      inputTokens,
    },
    totalCost: usage.summary.total_cost ?? 0,
    costAvailable: usage.summary.cost_available === true,
  };
}

export function StatCards({ usage, loading, sparklines }: StatCardsProps) {
  const { t } = useTranslation();
  const usageSnapshot = usage?.usage ?? null;
  const { requestStats, tokenBreakdown, rateStats, cacheRateStats, totalCost, costAvailable } = useMemo(
    () => buildStatCardMetrics({ usage }),
    [usage]
  );

  const statsCards: StatCardData[] = [
    {
      key: 'requests',
      label: t('usage_stats.total_requests'),
      icon: <IconSatellite size={16} />,
      accent: '#3b82f6',
      accentSoft: 'rgba(59, 130, 246, 0.18)',
      accentBorder: 'rgba(59, 130, 246, 0.34)',
      value: loading ? '-' : (usageSnapshot?.total_requests ?? 0).toLocaleString(),
      meta: (
        <>
          <span className={styles.statMetaItem}>
            <span className={styles.statMetaDot} style={{ backgroundColor: '#10b981' }} />
            {t('usage_stats.success_requests')}: {loading ? '-' : (usageSnapshot?.success_count ?? 0)}
          </span>
          <span className={styles.statMetaItem}>
            <span className={styles.statMetaDot} style={{ backgroundColor: '#c65746' }} />
            {t('usage_stats.failed_requests')}: {loading ? '-' : (usageSnapshot?.failure_count ?? 0)}
          </span>
          <span className={styles.statMetaItem}>
            {t('usage_stats.success_rate')}:{' '}
            {loading || requestStats.successRate === null ? '-' : `${formatFixedTwoDecimals(requestStats.successRate)}%`}
          </span>
        </>
      ),
      trend: sparklines.requests,
    },
    {
      key: 'tokens',
      label: t('usage_stats.total_tokens'),
      icon: <IconDiamond size={16} />,
      accent: '#8b5cf6',
      accentSoft: 'rgba(139, 92, 246, 0.18)',
      accentBorder: 'rgba(139, 92, 246, 0.35)',
      value: loading ? '-' : formatCompactNumber(usageSnapshot?.total_tokens ?? 0),
      meta: (
        <>
          <span className={styles.statMetaItem}>
            {t('usage_stats.cached_tokens')}:{' '}
            {loading ? '-' : formatCompactNumber(tokenBreakdown.cachedTokens)}
          </span>
          <span className={styles.statMetaItem}>
            {t('usage_stats.reasoning_tokens')}:{' '}
            {loading ? '-' : formatCompactNumber(tokenBreakdown.reasoningTokens)}
          </span>
        </>
      ),
      trend: sparklines.tokens,
    },
    {
      key: 'rpm',
      label: t('usage_stats.rpm'),
      icon: <IconTimer size={16} />,
      accent: '#22c55e',
      accentSoft: 'rgba(34, 197, 94, 0.18)',
      accentBorder: 'rgba(34, 197, 94, 0.32)',
      value: loading ? '-' : formatPerMinuteValue(rateStats.rpm),
      meta: (
        <span className={styles.statMetaItem}>
          {t('usage_stats.total_requests')}:{' '}
          {loading ? '-' : rateStats.requestCount.toLocaleString()}
        </span>
      ),
      trend: sparklines.rpm,
    },
    {
      key: 'tpm',
      label: t('usage_stats.tpm'),
      icon: <IconTrendingUp size={16} />,
      accent: '#f97316',
      accentSoft: 'rgba(249, 115, 22, 0.18)',
      accentBorder: 'rgba(249, 115, 22, 0.32)',
      value: loading ? '-' : formatPerMinuteValue(rateStats.tpm),
      meta: (
        <span className={styles.statMetaItem}>
          {t('usage_stats.total_tokens')}:{' '}
          {loading ? '-' : formatCompactNumber(rateStats.tokenCount)}
        </span>
      ),
      trend: sparklines.tpm,
    },
    {
      key: 'cache-rate',
      label: t('usage_stats.cache_rate'),
      icon: <IconPercent size={16} />,
      accent: '#14b8a6',
      accentSoft: 'rgba(20, 184, 166, 0.18)',
      accentBorder: 'rgba(20, 184, 166, 0.34)',
      value: loading || cacheRateStats.cachedRate === null ? '-' : `${formatFixedTwoDecimals(cacheRateStats.cachedRate)}%`,
      meta: (
        <>
          <span className={styles.statMetaItem}>
            {t('usage_stats.cached_tokens')}:{' '}
            {loading ? '-' : formatCompactNumber(cacheRateStats.cachedTokens)}
          </span>
          <span className={styles.statMetaItem}>
            {t('usage_stats.input_tokens')}:{' '}
            {loading ? '-' : formatCompactNumber(cacheRateStats.inputTokens)}
          </span>
        </>
      ),
      trend: sparklines.cachedRate,
    },
    {
      key: 'cost',
      label: t('usage_stats.total_cost'),
      icon: <IconDollarSign size={16} />,
      accent: '#f59e0b',
      accentSoft: 'rgba(245, 158, 11, 0.18)',
      accentBorder: 'rgba(245, 158, 11, 0.32)',
      value: loading ? '-' : formatUsd(totalCost),
      meta: (
        <>
          <span className={styles.statMetaItem}>
            {t('usage_stats.total_tokens')}:{' '}
            {loading ? '-' : formatCompactNumber(usageSnapshot?.total_tokens ?? 0)}
          </span>
          {!costAvailable && (
            <span className={`${styles.statMetaItem} ${styles.statSubtle}`}>
              {t('usage_stats.cost_need_price')}
            </span>
          )}
        </>
      ),
      trend: sparklines.cost,
    },
  ];

  return (
    <div className={styles.statsGrid}>
      {statsCards.map((card) => (
        <div
          key={card.key}
          className={styles.statCard}
          style={
            {
              '--accent': card.accent,
              '--accent-soft': card.accentSoft,
              '--accent-border': card.accentBorder,
            } as CSSProperties
          }
        >
          <div className={styles.statCardHeader}>
            <div className={styles.statLabelGroup}>
              <span className={styles.statLabel}>{card.label}</span>
            </div>
            <span className={styles.statIconBadge}>{card.icon}</span>
          </div>
          <div className={styles.statValue}>{card.value}</div>
          {card.meta && <div className={styles.statMetaRow}>{card.meta}</div>}
          <div className={styles.statTrend}>
            {card.trend ? (
              <Line
                className={styles.sparkline}
                data={card.trend.data}
                options={sparklineOptions}
              />
            ) : (
              <div className={styles.statTrendPlaceholder}></div>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}
