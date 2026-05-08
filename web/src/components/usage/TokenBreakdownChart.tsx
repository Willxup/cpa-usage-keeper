import { useState, useMemo, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Line } from 'react-chartjs-2';
import type { ChartOptions } from 'chart.js';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { formatCompactTokenValue, type TokenCategory } from '@/utils/usage';
import { buildChartOptions, getHourChartMinWidth } from '@/utils/usage/chartConfig';
import type { UsageOverviewPayload } from './hooks/useUsageData';
import styles from '@/pages/UsagePage.module.scss';

const TOKEN_COLORS: Record<TokenCategory, { border: string; bg: string }> = {
  input: { border: '#8b8680', bg: 'rgba(139, 134, 128, 0.25)' },
  output: { border: '#22c55e', bg: 'rgba(34, 197, 94, 0.25)' },
  cached: { border: '#f59e0b', bg: 'rgba(245, 158, 11, 0.25)' },
  reasoning: { border: '#8b5cf6', bg: 'rgba(139, 92, 246, 0.25)' }
};

const CATEGORIES: TokenCategory[] = ['input', 'output', 'cached', 'reasoning'];
const HOUR_MS = 60 * 60 * 1000;

type TokenBreakdownChartPeriod = 'hour' | 'day';

type TokenSeriesSource = NonNullable<UsageOverviewPayload['series']>;

export type TokenBreakdownChartSeries = {
  labels: string[];
  dataByCategory: Record<TokenCategory, number[]>;
};

export type BuildTokenBreakdownChartSeriesOptions = {
  usage: UsageOverviewPayload | null;
  period: TokenBreakdownChartPeriod;
  hourWindowHours?: number;
  endMs?: number;
  includeFinalHourBucket?: boolean;
};

const resolveHourBucketCount = (hourWindowHours?: number, includeFinalBucket = false): number => {
  if (!Number.isFinite(hourWindowHours) || !hourWindowHours || hourWindowHours <= 0) {
    return includeFinalBucket ? 25 : 24;
  }
  const resolvedHours = Math.min(Math.max(Math.floor(hourWindowHours), 1), 24);
  return includeFinalBucket ? resolvedHours + 1 : resolvedHours >= 24 ? 24 : resolvedHours + 1;
};

const formatHourBucketKey = (timestampMs: number): string => `${new Date(timestampMs).toISOString().slice(0, 13)}:00:00Z`;

const formatChartLabel = (label: string, period: TokenBreakdownChartPeriod, isFinalBucket = false) => {
  if (period !== 'hour') return label;
  if (isFinalBucket) return '24:00';
  const date = new Date(label);
  if (Number.isNaN(date.getTime())) return label;
  return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`;
};

const getTokenSource = (usage: UsageOverviewPayload | null, period: TokenBreakdownChartPeriod): TokenSeriesSource | undefined => (
  period === 'hour' ? (usage?.hourly_series ?? usage?.series) : (usage?.daily_series ?? usage?.series)
);

const buildHourlyLabels = (source: TokenSeriesSource | undefined, hourWindowHours?: number, endMs?: number, includeFinalBucket = false) => {
  const labels = Object.keys(source?.input_tokens ?? {}).sort((a, b) => a.localeCompare(b));
  if (labels.length === 0) return [];

  const bucketCount = resolveHourBucketCount(hourWindowHours, includeFinalBucket);
  const latestLabelMs = Date.parse(labels[labels.length - 1]);
  const requestedEndMs = Number.isFinite(endMs) && endMs && endMs > 0 ? endMs : latestLabelMs;
  const currentHour = new Date(requestedEndMs);
  currentHour.setUTCMinutes(0, 0, 0);
  const earliestTime = currentHour.getTime() - ((bucketCount - 1) * HOUR_MS);

  return Array.from({ length: bucketCount }, (_, index) => formatHourBucketKey(earliestTime + index * HOUR_MS));
};

export const buildTokenBreakdownChartSeries = ({
  usage,
  period,
  hourWindowHours,
  endMs,
  includeFinalHourBucket = false,
}: BuildTokenBreakdownChartSeriesOptions): TokenBreakdownChartSeries => {
  const source = getTokenSource(usage, period);
  const labels = period === 'hour'
    ? buildHourlyLabels(source, hourWindowHours, endMs, includeFinalHourBucket)
    : Object.keys(source?.input_tokens ?? {}).sort((a, b) => a.localeCompare(b));

  return {
    labels: labels.map((label, index) => formatChartLabel(label, period, includeFinalHourBucket && index === labels.length - 1)),
    dataByCategory: {
      input: labels.map((label) => Number(source?.input_tokens?.[label] ?? 0)),
      output: labels.map((label) => Number(source?.output_tokens?.[label] ?? 0)),
      cached: labels.map((label) => Number(source?.cached_tokens?.[label] ?? 0)),
      reasoning: labels.map((label) => Number(source?.reasoning_tokens?.[label] ?? 0)),
    },
  };
};

export interface TokenBreakdownChartProps {
  usage: UsageOverviewPayload | null;
  loading: boolean;
  isDark: boolean;
  isMobile: boolean;
  hourWindowHours?: number;
  endMs?: number;
  includeFinalHourBucket?: boolean;
  preferredPeriod?: TokenBreakdownChartPeriod;
}

export const buildTokenBreakdownChartOptions = ({
  period,
  labels,
  isDark,
  isMobile,
  stacked = false,
}: {
  period: TokenBreakdownChartPeriod;
  labels: string[];
  isDark: boolean;
  isMobile: boolean;
  stacked?: boolean;
}): ChartOptions<'line'> => {
  const baseOptions = buildChartOptions({ period, labels, isDark, isMobile });
  return {
    ...baseOptions,
    plugins: {
      ...baseOptions.plugins,
      tooltip: {
        ...baseOptions.plugins?.tooltip,
        callbacks: {
          label: (context) => {
            const label = context.dataset.label ? `${context.dataset.label}: ` : '';
            return `${label}${formatCompactTokenValue(Number(context.parsed.y ?? 0), true)}`;
          },
        },
      },
    },
    scales: {
      ...baseOptions.scales,
      y: {
        ...baseOptions.scales?.y,
        stacked,
        ticks: {
          ...baseOptions.scales?.y?.ticks,
          callback: (value) => formatCompactTokenValue(Number(value)),
        },
      },
      x: {
        ...baseOptions.scales?.x,
        stacked,
      },
    },
  };
};

export function TokenBreakdownChart({
  usage,
  loading,
  isDark,
  isMobile,
  hourWindowHours,
  endMs,
  includeFinalHourBucket = false,
  preferredPeriod = 'hour'
}: TokenBreakdownChartProps) {
  const { t } = useTranslation();
  const [period, setPeriod] = useState<TokenBreakdownChartPeriod>(preferredPeriod);

  useEffect(() => {
    setPeriod(preferredPeriod);
  }, [preferredPeriod]);

  const { chartData, chartOptions } = useMemo(() => {
    const series = buildTokenBreakdownChartSeries({ usage, period, hourWindowHours, endMs, includeFinalHourBucket });
    const categoryLabels: Record<TokenCategory, string> = {
      input: t('usage_stats.input_tokens'),
      output: t('usage_stats.output_tokens'),
      cached: t('usage_stats.cached_tokens'),
      reasoning: t('usage_stats.reasoning_tokens')
    };

    const data = {
      labels: series.labels,
      datasets: CATEGORIES.map((cat) => ({
        label: categoryLabels[cat],
        data: series.dataByCategory[cat],
        borderColor: TOKEN_COLORS[cat].border,
        backgroundColor: TOKEN_COLORS[cat].bg,
        pointBackgroundColor: TOKEN_COLORS[cat].border,
        pointBorderColor: TOKEN_COLORS[cat].border,
        fill: true,
        tension: 0.35
      }))
    };

    const options = buildTokenBreakdownChartOptions({
      period,
      labels: series.labels,
      isDark,
      isMobile,
      stacked: true,
    });

    return { chartData: data, chartOptions: options };
  }, [usage, period, isDark, isMobile, hourWindowHours, endMs, includeFinalHourBucket, t]);

  return (
    <Card
      title={t('usage_stats.token_breakdown_title')}
      extra={
        <div className={styles.periodButtons}>
          <Button
            variant={period === 'hour' ? 'primary' : 'secondary'}
            size="sm"
            onClick={() => setPeriod('hour')}
          >
            {t('usage_stats.by_hour')}
          </Button>
          <Button
            variant={period === 'day' ? 'primary' : 'secondary'}
            size="sm"
            onClick={() => setPeriod('day')}
          >
            {t('usage_stats.by_day')}
          </Button>
        </div>
      }
    >
      {loading ? (
        <div className={styles.hint}>{t('common.loading')}</div>
      ) : chartData.labels.length > 0 ? (
        <div className={styles.chartWrapper}>
          <div className={styles.chartLegend} aria-label="Chart legend">
            {chartData.datasets.map((dataset, index) => (
              <div
                key={`${dataset.label}-${index}`}
                className={styles.legendItem}
                title={dataset.label}
              >
                <span className={styles.legendDot} style={{ backgroundColor: dataset.borderColor }} />
                <span className={styles.legendLabel}>{dataset.label}</span>
              </div>
            ))}
          </div>
          <div className={styles.chartArea}>
            <div className={styles.chartScroller}>
              <div
                className={styles.chartCanvas}
                style={
                  period === 'hour'
                    ? { minWidth: getHourChartMinWidth(chartData.labels.length, isMobile) }
                    : undefined
                }
              >
                <Line data={chartData} options={chartOptions} />
              </div>
            </div>
          </div>
        </div>
      ) : (
        <div className={styles.hint}>{t('usage_stats.no_data')}</div>
      )}
    </Card>
  );
}
