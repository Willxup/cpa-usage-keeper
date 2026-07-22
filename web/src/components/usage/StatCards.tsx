import { useEffect, useMemo, useState, type CSSProperties, type ReactNode } from 'react';
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
  calculateCacheReadRate,
  formatCompactNumber,
  formatFixedTwoDecimals,
  formatPerMinuteValue,
  formatUsd,
} from '@/utils/usage';
import { sparklineOptions } from '@/utils/usage/chartConfig';
import type { UsageOverviewPayload, UsagePayload } from './hooks/useUsageData';
import type { SparklineBundle } from './hooks/useSparklines';
import { buildDailyAverageMetrics, DailyAverageCard } from './DailyAverageCard';
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
  dailyAverageUsage: UsageOverviewPayload | null;
  reserveDailyAverage: boolean;
  sparklines: {
    requests: SparklineBundle | null;
    tokens: SparklineBundle | null;
    rpm: SparklineBundle | null;
    tpm: SparklineBundle | null;
    cacheReadRate: SparklineBundle | null;
    cost: SparklineBundle | null;
  };
}

interface StatCardMetrics {
  requestStats: { successRate: number | null };
  tokenBreakdown: { cacheReadTokens: number; cacheCreationTokens: number; reasoningTokens: number };
  rateStats: { rpm: number; tpm: number; requestCount: number; tokenCount: number };
  cacheReadRateStats: { cacheReadRate: number | null; cacheReadTokens: number; inputTokens: number };
  totalCost: number;
  costAvailable: boolean;
}

const safeNumber = (value: unknown): number => {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
};

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
      tokenBreakdown: { cacheReadTokens: 0, cacheCreationTokens: 0, reasoningTokens: 0 },
      rateStats: { rpm: 0, tpm: 0, requestCount: 0, tokenCount: 0 },
      cacheReadRateStats: { cacheReadRate: null, cacheReadTokens: 0, inputTokens: 0 },
      totalCost: 0,
      costAvailable: false,
    };
  }

  const cacheReadTokens = Math.max(safeNumber(usage.summary.cache_read_tokens), 0);
  const cacheCreationTokens = Math.max(safeNumber(usage.summary.cache_creation_tokens), 0);
  const inputTokens = Math.max(safeNumber(usage.summary.input_tokens), 0);

  return {
    requestStats,
    tokenBreakdown: {
      cacheReadTokens,
      cacheCreationTokens,
      reasoningTokens: usage.summary.reasoning_tokens ?? 0,
    },
    rateStats: {
      rpm: usage.summary.rpm ?? 0,
      tpm: usage.summary.tpm ?? 0,
      requestCount: Math.max(safeNumber(usageSnapshot?.total_requests), 0),
      tokenCount: Math.max(safeNumber(usageSnapshot?.total_tokens), 0),
    },
    cacheReadRateStats: {
      cacheReadRate: calculateCacheReadRate({ inputTokens, cacheReadTokens }),
      cacheReadTokens,
      inputTokens,
    },
    totalCost: usage.summary.total_cost ?? 0,
    costAvailable: usage.summary.cost_available === true,
  };
}

export function StatCards({
  usage,
  loading,
  dailyAverageUsage,
  reserveDailyAverage,
  sparklines,
}: StatCardsProps) {
  const { t } = useTranslation();
  const usageSnapshot = usage?.usage ?? null;
  const shouldExpandDailyAverage = Boolean(buildDailyAverageMetrics(dailyAverageUsage)) || reserveDailyAverage;
  const [dailyAverageExpanded, setDailyAverageExpanded] = useState(false);
  const { requestStats, tokenBreakdown, rateStats, cacheReadRateStats, totalCost, costAvailable } = useMemo(
    () => buildStatCardMetrics({ usage }),
    [usage]
  );

  useEffect(() => {
    // 等浏览器完成当前布局后再切换展开态，让首次出现和范围切换都能触发卡片布局动画。
    const frame = window.requestAnimationFrame(() => setDailyAverageExpanded(shouldExpandDailyAverage));
    return () => window.cancelAnimationFrame(frame);
  }, [shouldExpandDailyAverage]);

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
            {t('usage_stats.cache_read_tokens')}:{' '}
            {loading ? '-' : formatCompactNumber(tokenBreakdown.cacheReadTokens)}
          </span>
          <span className={styles.statMetaItem}>
            {t('usage_stats.cache_creation_tokens')}:{' '}
            {loading ? '-' : formatCompactNumber(tokenBreakdown.cacheCreationTokens)}
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
      key: 'cache-read-rate',
      label: t('usage_stats.cache_rate'),
      icon: <IconPercent size={16} />,
      accent: '#14b8a6',
      accentSoft: 'rgba(20, 184, 166, 0.18)',
      accentBorder: 'rgba(20, 184, 166, 0.34)',
      value: loading || cacheReadRateStats.cacheReadRate === null ? '-' : `${formatFixedTwoDecimals(cacheReadRateStats.cacheReadRate)}%`,
      meta: (
        <>
          <span className={styles.statMetaItem}>
            {t('usage_stats.cache_read_tokens')}:{' '}
            {loading ? '-' : formatCompactNumber(cacheReadRateStats.cacheReadTokens)}
          </span>
          <span className={styles.statMetaItem}>
            {t('usage_stats.input_tokens')}:{' '}
            {loading ? '-' : formatCompactNumber(cacheReadRateStats.inputTokens)}
          </span>
        </>
      ),
      trend: sparklines.cacheReadRate,
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

  const primaryCards = statsCards.slice(0, 2);
  const secondaryCards = statsCards.slice(2);
  const renderStatCard = (card: StatCardData) => (
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
  );

  return (
    <div className={styles.statsSection}>
      <div
        className={`${styles.primaryStatsRow} ${dailyAverageExpanded ? styles.primaryStatsRowExpanded : ''}`.trim()}
      >
        <div className={styles.dailyAverageSlot} aria-hidden={!dailyAverageExpanded}>
          <DailyAverageCard usage={dailyAverageUsage} loading={loading} />
        </div>
        {primaryCards.map((card) => (
          <div key={card.key} className={styles.primaryStatSlot}>
            {renderStatCard(card)}
          </div>
        ))}
      </div>
      <div className={styles.secondaryStatsGrid}>
        {secondaryCards.map(renderStatCard)}
      </div>
    </div>
  );
}
