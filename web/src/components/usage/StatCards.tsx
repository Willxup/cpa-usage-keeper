import { useMemo, type CSSProperties, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Col, Row, Statistic } from 'antd';
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
import { getChartTheme } from '@/lib/chartTheme';
import type { UsageOverviewPayload, UsagePayload } from './hooks/useUsageData';
import { buildDailyAverageMetrics, formatDailyAverageCount, formatDailyAverageRangeDays } from './dailyAverage';
import styles from './UsageOverview.module.scss';

interface StatCardData {
  key: string;
  label: string;
  icon: ReactNode;
  accent: string;
  accentSoft: string;
  accentBorder: string;
  value: string;
  meta?: ReactNode;
  context?: ReactNode;
}

export interface StatCardsProps {
  usage: UsageOverviewPayload | null;
  loading: boolean;
  isDark?: boolean;
  dailyAverageUsage?: UsageOverviewPayload | null;
  showDailyAverages?: boolean;
}

interface StatCardMetrics {
  requestStats: { successRate: number | null };
  tokenBreakdown: { cacheReadTokens: number; cacheCreationTokens: number; reasoningTokens: number };
  rateStats: { rpm: number; tpm: number; windowMinutes: number; requestCount: number; tokenCount: number };
  cacheReadRateStats: { cacheReadRate: number | null; cacheReadTokens: number; inputTokens: number };
  totalCost: number;
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
      rateStats: { rpm: 0, tpm: 0, windowMinutes: 1, requestCount: 0, tokenCount: 0 },
      cacheReadRateStats: { cacheReadRate: null, cacheReadTokens: 0, inputTokens: 0 },
      totalCost: 0,
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
      windowMinutes: usage.summary.window_minutes ?? 1,
      requestCount: usage.summary.request_count ?? 0,
      tokenCount: usage.summary.token_count ?? 0,
    },
    cacheReadRateStats: {
      cacheReadRate: calculateCacheReadRate({ inputTokens, cacheReadTokens }),
      cacheReadTokens,
      inputTokens,
    },
    totalCost: usage.summary.total_cost ?? 0,
  };
}

export function StatCards({
  usage,
  loading,
  isDark = false,
  dailyAverageUsage = null,
  showDailyAverages = false,
}: StatCardsProps) {
  const { t } = useTranslation();
  const usageSnapshot = usage?.usage ?? null;
  const { requestStats, tokenBreakdown, rateStats, cacheReadRateStats, totalCost } = useMemo(
    () => buildStatCardMetrics({ usage }),
    [usage]
  );
  const dailyAverages = useMemo(() => buildDailyAverageMetrics(dailyAverageUsage), [dailyAverageUsage]);
  const accents = getChartTheme(isDark).series;
  const dailyAverageTitle = dailyAverages
    ? `${t('usage_stats.daily_average')} · ${t('usage_stats.daily_average_range', {
        days: formatDailyAverageRangeDays(dailyAverages.rangeDays),
      })}`
    : t('usage_stats.daily_average');
  const dailyAverageContext = (
    label: string,
    value: string,
  ) => showDailyAverages ? (
    <span title={dailyAverageTitle}>
      {label}: {value}
    </span>
  ) : null;

  const statsCards: StatCardData[] = [
    {
      key: 'requests',
      label: t('usage_stats.total_requests'),
      icon: <IconSatellite size={16} />,
      accent: accents.blue.stroke,
      accentSoft: accents.blue.fill,
      accentBorder: accents.blue.stroke,
      value: loading ? '-' : (usageSnapshot?.total_requests ?? 0).toLocaleString(),
      meta: (
        <>
          <span className={styles.statMetaItem}>
            <span className={styles.statMetaDot} style={{ backgroundColor: 'var(--success-color)' }} />
            {t('usage_stats.success_requests')}: {loading ? '-' : (usageSnapshot?.success_count ?? 0)}
          </span>
          <span className={styles.statMetaItem}>
            <span className={styles.statMetaDot} style={{ backgroundColor: 'var(--danger-color)' }} />
            {t('usage_stats.failed_requests')}: {loading ? '-' : (usageSnapshot?.failure_count ?? 0)}
          </span>
          <span className={styles.statMetaItem}>
            {t('usage_stats.success_rate')}:{' '}
            {loading || requestStats.successRate === null ? '-' : `${formatFixedTwoDecimals(requestStats.successRate)}%`}
          </span>
        </>
      ),
      context: dailyAverageContext(
        t('usage_stats.avg_requests'),
        loading || !dailyAverages ? '-' : formatDailyAverageCount(dailyAverages.requests),
      ),
    },
    {
      key: 'tokens',
      label: t('usage_stats.total_tokens'),
      icon: <IconDiamond size={16} />,
      accent: accents.violet.stroke,
      accentSoft: accents.violet.fill,
      accentBorder: accents.violet.stroke,
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
      context: dailyAverageContext(
        t('usage_stats.avg_tokens'),
        loading || !dailyAverages ? '-' : formatCompactNumber(dailyAverages.tokens),
      ),
    },
    {
      key: 'rpm',
      label: t('usage_stats.rpm'),
      icon: <IconTimer size={16} />,
      accent: accents.cyan.stroke,
      accentSoft: accents.cyan.fill,
      accentBorder: accents.cyan.stroke,
      value: loading ? '-' : formatPerMinuteValue(rateStats.rpm),
      meta: (
        <span className={styles.statMetaItem}>
          {t('usage_stats.total_requests')}:{' '}
          {loading ? '-' : rateStats.requestCount.toLocaleString()}
        </span>
      ),
    },
    {
      key: 'tpm',
      label: t('usage_stats.tpm'),
      icon: <IconTrendingUp size={16} />,
      accent: accents.indigo.stroke,
      accentSoft: accents.indigo.fill,
      accentBorder: accents.indigo.stroke,
      value: loading ? '-' : formatPerMinuteValue(rateStats.tpm),
      meta: (
        <span className={styles.statMetaItem}>
          {t('usage_stats.total_tokens')}:{' '}
          {loading ? '-' : formatCompactNumber(rateStats.tokenCount)}
        </span>
      ),
    },
    {
      key: 'cache-read-rate',
      label: t('usage_stats.cache_rate'),
      icon: <IconPercent size={16} />,
      accent: accents.teal.stroke,
      accentSoft: accents.teal.fill,
      accentBorder: accents.teal.stroke,
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
    },
    {
      key: 'cost',
      label: t('usage_stats.total_cost'),
      icon: <IconDollarSign size={16} />,
      accent: accents.orange.stroke,
      accentSoft: accents.orange.fill,
      accentBorder: accents.orange.stroke,
      value: loading ? '-' : formatUsd(totalCost),
      meta: (
        <span className={styles.statMetaItem}>
          {t('usage_stats.total_tokens')}:{' '}
          {loading ? '-' : formatCompactNumber(usageSnapshot?.total_tokens ?? 0)}
        </span>
      ),
      context: dailyAverageContext(
        t('usage_stats.avg_cost'),
        loading || !dailyAverages ? '-' : formatUsd(dailyAverages.cost),
      ),
    },
  ];

  return (
    <section className={styles.statsPanel} aria-label={t('usage_stats.tab_overview')}>
      <Row className={styles.statsRow} gutter={0}>
        {statsCards.map((card) => (
          <Col key={card.key} xs={24} sm={12} lg={8} xxl={4} className={styles.statColumn}>
            <div
              className={styles.statItem}
              style={{
                '--accent': card.accent,
                '--accent-soft': card.accentSoft,
                '--accent-border': card.accentBorder,
              } as CSSProperties}
            >
              <Statistic
                className={styles.statistic}
                loading={loading}
                title={(
                  <span className={styles.statTitle}>
                    <span className={styles.statIcon}>{card.icon}</span>
                    <span>{card.label}</span>
                  </span>
                )}
                value={card.value}
                formatter={() => card.value}
              />
              {card.meta && <div className={styles.statMetaRow}>{card.meta}</div>}
              <div className={styles.statContextRow}>{card.context}</div>
            </div>
          </Col>
        ))}
      </Row>
    </section>
  );
}
