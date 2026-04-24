import { useState, useMemo } from 'react';
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
}

interface OverviewCostTrendSeries {
  labels: string[];
  data: number[];
  hasData: boolean;
  costAvailable: boolean;
}

const formatHourLabel = (key: string): string => {
  const date = new Date(key);
  if (Number.isNaN(date.getTime())) return key;
  return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`;
};

const startOfDayKey = (key: string): string => {
  const date = new Date(key);
  if (Number.isNaN(date.getTime())) return key;
  return date.toISOString().slice(0, 10);
};

const resolveHourBucketCount = (hourWindowHours?: number): number => {
  if (!Number.isFinite(hourWindowHours) || !hourWindowHours || hourWindowHours <= 0) {
    return 24;
  }
  const resolvedHours = Math.min(Math.max(Math.floor(hourWindowHours), 1), 24);
  return resolvedHours >= 24 ? 24 : resolvedHours + 1;
};

const toUtcHourMs = (value: string | number): number => {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return NaN;
  date.setUTCMinutes(0, 0, 0);
  return date.getTime();
};

export function buildOverviewCostTrendSeries({
  usage,
  period,
  hourWindowHours,
  endMs,
}: {
  usage: UsageOverviewPayload | null;
  period: 'hour' | 'day';
  hourWindowHours?: number;
  endMs?: number;
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
    const bucketCount = resolveHourBucketCount(hourWindowHours);
    const anchorMs = Number.isFinite(endMs) && endMs ? endMs : (hourlyEntries.length ? Date.parse(hourlyEntries[hourlyEntries.length - 1][0]) : Date.now());
    const currentHour = new Date(anchorMs);
    currentHour.setUTCMinutes(0, 0, 0);
    const hourMs = 60 * 60 * 1000;
    const earliestMs = currentHour.getTime() - ((bucketCount - 1) * hourMs);
    const labels = Array.from({ length: bucketCount }, (_, index) => {
      const bucketMs = earliestMs + (index * hourMs);
      return formatHourLabel(new Date(bucketMs).toISOString());
    });
    const valueByHour = new Map(hourlyEntries.map(([label, value]) => [toUtcHourMs(label), Number(value ?? 0)]));
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
  endMs
}: CostTrendChartProps) {
  const { t } = useTranslation();
  const [period, setPeriod] = useState<'hour' | 'day'>('hour');

  const { chartData, chartOptions, hasData, costAvailable } = useMemo(() => {
    const series = buildOverviewCostTrendSeries({ usage, period, hourWindowHours, endMs });

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
  }, [usage, period, isDark, isMobile, hourWindowHours, endMs, t]);

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
