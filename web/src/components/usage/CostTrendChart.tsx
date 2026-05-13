import { useState, useMemo, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import type { ScriptableContext } from 'chart.js';
import { Line } from 'react-chartjs-2';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { formatUsd } from '@/utils/usage';
import { buildChartOptions, getHourChartMinWidth } from '@/utils/usage/chartConfig';
import type { UsageOverviewPayload } from './hooks/useUsageData';
import styles from '@/pages/UsagePage.module.scss';

export interface CostTrendChartProps {
  usage: UsageOverviewPayload | null;
  loading: boolean;
  isDark: boolean;
  isMobile: boolean;
  hourWindowHours?: number;
  endMs?: number;
  includeFinalHourBucket?: boolean;
  preferredPeriod?: 'hour' | 'day';
}

interface OverviewCostTrendSeries {
  labels: string[];
  data: number[];
  hasData: boolean;
  costAvailable: boolean;
}

const HOUR_MS = 60 * 60 * 1000;
const HOUR_BUCKET_PATTERN = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(Z|[+-]\d{2}:\d{2})$/;

const parseHourBucketOffsetMinutes = (key?: string): number => {
  const match = key?.match(HOUR_BUCKET_PATTERN);
  const offset = match?.[7];
  if (!offset || offset === 'Z') return 0;
  const sign = offset[0] === '-' ? -1 : 1;
  const hours = Number(offset.slice(1, 3));
  const minutes = Number(offset.slice(4, 6));
  return sign * ((hours * 60) + minutes);
};

const startOfOffsetHourMs = (timestampMs: number, offsetMinutes: number): number => {
  const shiftedMs = timestampMs + offsetMinutes * 60 * 1000;
  return Math.floor(shiftedMs / HOUR_MS) * HOUR_MS - offsetMinutes * 60 * 1000;
};

const formatHourBucketKey = (timestampMs: number, referenceKey?: string): string => {
  const offsetMinutes = parseHourBucketOffsetMinutes(referenceKey);
  const shifted = new Date(timestampMs + offsetMinutes * 60 * 1000);
  const pad = (value: number) => String(value).padStart(2, '0');
  const offset = offsetMinutes === 0
    ? 'Z'
    : `${offsetMinutes < 0 ? '-' : '+'}${pad(Math.floor(Math.abs(offsetMinutes) / 60))}:${pad(Math.abs(offsetMinutes) % 60)}`;
  return `${shifted.getUTCFullYear()}-${pad(shifted.getUTCMonth() + 1)}-${pad(shifted.getUTCDate())}T${pad(shifted.getUTCHours())}:00:00${offset}`;
};

const formatHourLabel = (key: string, isFinalBucket = false): string => {
  if (isFinalBucket) return '24:00';
  const match = key.match(HOUR_BUCKET_PATTERN);
  if (match) return `${match[4]}:${match[5]}`;
  const date = new Date(key);
  if (Number.isNaN(date.getTime())) return key;
  return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`;
};

const startOfDayKey = (key: string): string => {
  const date = new Date(key);
  if (Number.isNaN(date.getTime())) return key;
  const pad = (value: number) => String(value).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
};

const resolveHourBucketCount = (hourWindowHours?: number, includeFinalBucket = false): number => {
  if (!Number.isFinite(hourWindowHours) || !hourWindowHours || hourWindowHours <= 0) {
    return includeFinalBucket ? 25 : 24;
  }
  const resolvedHours = Math.min(Math.max(Math.floor(hourWindowHours), 1), 24);
  return includeFinalBucket ? resolvedHours + 1 : resolvedHours >= 24 ? 24 : resolvedHours + 1;
};

const toOffsetHourMs = (value: string | number): number => {
  const timestampMs = typeof value === 'number' ? value : Date.parse(value);
  return Number.isNaN(timestampMs) ? NaN : startOfOffsetHourMs(timestampMs, parseHourBucketOffsetMinutes(String(value)));
};

export function buildOverviewCostTrendSeries({
  usage,
  period,
  hourWindowHours,
  endMs,
  includeFinalHourBucket = false,
}: {
  usage: UsageOverviewPayload | null;
  period: 'hour' | 'day';
  hourWindowHours?: number;
  endMs?: number;
  includeFinalHourBucket?: boolean;
}): OverviewCostTrendSeries {
  if (!usage) {
    return { labels: [], data: [], hasData: false, costAvailable: false };
  }

  const selectedSeries = period === 'hour'
    ? (usage.hourly_series ?? usage.series)
    : (usage.daily_series ?? usage.series);
  const costSeries = selectedSeries?.cost ?? {};
  const costAvailable = usage.summary?.cost_available === true;
  const hourlyEntries = Object.entries(costSeries)
    .filter(([label]) => label.includes('T'))
    .sort(([left], [right]) => left.localeCompare(right));
  const dailyEntries = Object.entries(costSeries)
    .filter(([label]) => !label.includes('T'))
    .sort(([left], [right]) => left.localeCompare(right));

  if (period === 'hour') {
    const bucketCount = resolveHourBucketCount(hourWindowHours, includeFinalHourBucket);
    const referenceKey = hourlyEntries[hourlyEntries.length - 1]?.[0];
    const offsetMinutes = parseHourBucketOffsetMinutes(referenceKey);
    const anchorMs = Number.isFinite(endMs) && endMs ? endMs : (referenceKey ? Date.parse(referenceKey) : Date.now());
    const hourMs = 60 * 60 * 1000;
    const earliestMs = startOfOffsetHourMs(anchorMs, offsetMinutes) - ((bucketCount - 1) * hourMs);
    const labels = Array.from({ length: bucketCount }, (_, index) => {
      const bucketMs = earliestMs + (index * hourMs);
      return formatHourLabel(formatHourBucketKey(bucketMs, referenceKey), includeFinalHourBucket && index === bucketCount - 1);
    });
    const valueByHour = new Map(hourlyEntries.map(([label, value]) => [toOffsetHourMs(label), Number(value ?? 0)]));
    const data = Array.from({ length: bucketCount }, (_, index) => {
      const bucketMs = earliestMs + (index * hourMs);
      return valueByHour.get(bucketMs) ?? 0;
    });

    return {
      labels,
      data,
      hasData: data.some((value) => value > 0),
      costAvailable,
    };
  }

  const grouped = new Map<string, number>();
  if (dailyEntries.length > 0) {
    dailyEntries.forEach(([label, value]) => {
      grouped.set(label, Number(value ?? 0));
    });
  } else {
    hourlyEntries.forEach(([label, value]) => {
      const dayKey = startOfDayKey(label);
      grouped.set(dayKey, (grouped.get(dayKey) ?? 0) + Number(value ?? 0));
    });
  }
  const labels = Array.from(grouped.keys()).sort((a, b) => a.localeCompare(b));
  const data = labels.map((label) => grouped.get(label) ?? 0);

  return {
    labels,
    data,
    hasData: data.some((value) => value > 0),
    costAvailable,
  };
}

const COST_COLOR = '#f59e0b';
const COST_BG = 'rgba(245, 158, 11, 0.15)';

function buildGradient(ctx: ScriptableContext<'line'>) {
  const chart = ctx.chart;
  const area = chart.chartArea;
  if (!area) return COST_BG;
  const gradient = chart.ctx.createLinearGradient(0, area.top, 0, area.bottom);
  gradient.addColorStop(0, 'rgba(245, 158, 11, 0.28)');
  gradient.addColorStop(0.6, 'rgba(245, 158, 11, 0.12)');
  gradient.addColorStop(1, 'rgba(245, 158, 11, 0.02)');
  return gradient;
}

export function shouldShowCostPricingHint({ costAvailable, hasData }: { costAvailable: boolean; hasData: boolean }): boolean {
  return !costAvailable && !hasData;
}

export function CostTrendChart({
  usage,
  loading,
  isDark,
  isMobile,
  hourWindowHours,
  endMs,
  includeFinalHourBucket = false,
  preferredPeriod = 'hour'
}: CostTrendChartProps) {
  const { t } = useTranslation();
  const [period, setPeriod] = useState<'hour' | 'day'>(preferredPeriod);

  useEffect(() => {
    setPeriod(preferredPeriod);
  }, [preferredPeriod]);

  const { chartData, chartOptions, hasData, costAvailable } = useMemo(() => {
    const series = buildOverviewCostTrendSeries({ usage, period, hourWindowHours, endMs, includeFinalHourBucket });

    const data = {
      labels: series.labels,
      datasets: [
        {
          label: t('usage_stats.total_cost'),
          data: series.data,
          borderColor: COST_COLOR,
          backgroundColor: buildGradient,
          pointBackgroundColor: COST_COLOR,
          pointBorderColor: COST_COLOR,
          fill: true,
          tension: 0.35
        }
      ]
    };

    const baseOptions = buildChartOptions({ period, labels: series.labels, isDark, isMobile });
    const options = {
      ...baseOptions,
      scales: {
        ...baseOptions.scales,
        y: {
          ...baseOptions.scales?.y,
          ticks: {
            ...(baseOptions.scales?.y && 'ticks' in baseOptions.scales.y ? baseOptions.scales.y.ticks : {}),
            callback: (value: string | number) => formatUsd(Number(value))
          }
        }
      }
    };

    return { chartData: data, chartOptions: options, hasData: series.hasData, costAvailable: series.costAvailable };
  }, [usage, period, isDark, isMobile, hourWindowHours, endMs, includeFinalHourBucket, t]);

  const shouldRenderChart = chartData.labels.length > 0 && hasData;
  const showPricingHint = shouldShowCostPricingHint({ costAvailable, hasData });

  return (
    <Card
      title={t('usage_stats.cost_trend_title')}
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
      ) : !shouldRenderChart ? (
        <div className={styles.hint}>{showPricingHint ? t('usage_stats.cost_need_price') : t('usage_stats.cost_no_data')}</div>
      ) : (
        <div className={styles.chartWrapper}>
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
      )}
    </Card>
  );
}
