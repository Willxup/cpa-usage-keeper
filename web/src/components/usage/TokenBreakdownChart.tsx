import { useState, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Line } from 'react-chartjs-2';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import {
  buildHourlyTokenBreakdown,
  buildDailyTokenBreakdown,
  type TokenCategory,
  type UsageDetailRecord,
} from '@/utils/usage';
import { buildChartOptions, getHourChartMinWidth } from '@/utils/usage/chartConfig';
import type { UsageEvent } from '@/lib/types';
import type { UsagePayload } from './hooks/useUsageData';
import styles from '@/pages/UsagePage.module.scss';

const TOKEN_COLORS: Record<TokenCategory, { border: string; bg: string }> = {
  input: { border: '#8b8680', bg: 'rgba(139, 134, 128, 0.25)' },
  output: { border: '#22c55e', bg: 'rgba(34, 197, 94, 0.25)' },
  cached: { border: '#f59e0b', bg: 'rgba(245, 158, 11, 0.25)' },
  reasoning: { border: '#8b5cf6', bg: 'rgba(139, 92, 246, 0.25)' }
};

const CATEGORIES: TokenCategory[] = ['input', 'output', 'cached', 'reasoning'];

export interface TokenBreakdownChartProps {
  usage: UsagePayload | null;
  events?: UsageEvent[];
  loading: boolean;
  isDark: boolean;
  isMobile: boolean;
  hourWindowHours?: number;
  endMs?: number;
}

const toUsageDetailRecord = (event: UsageEvent): UsageDetailRecord => ({
  timestamp: event.timestamp,
  source: String(event.source ?? ''),
  source_raw: String(event.source_raw ?? ''),
  source_type: String(event.source_type ?? ''),
  source_key: String(event.source_key ?? ''),
  auth_index: String(event.auth_index ?? ''),
  failed: event.failed === true,
  latency_ms: Number.isFinite(event.latency_ms) ? event.latency_ms : 0,
  tokens: {
    input_tokens: Number(event.tokens?.input_tokens ?? 0),
    output_tokens: Number(event.tokens?.output_tokens ?? 0),
    reasoning_tokens: Number(event.tokens?.reasoning_tokens ?? 0),
    cached_tokens: Number(event.tokens?.cached_tokens ?? 0),
    total_tokens: Number(event.tokens?.total_tokens ?? 0),
  },
  __timestampMs: Number.isFinite(Date.parse(event.timestamp)) ? Date.parse(event.timestamp) : 0,
});

export function TokenBreakdownChart({
  usage,
  events = [],
  loading,
  isDark,
  isMobile,
  hourWindowHours,
  endMs
}: TokenBreakdownChartProps) {
  const { t } = useTranslation();
  const [period, setPeriod] = useState<'hour' | 'day'>('hour');

  const { chartData, chartOptions } = useMemo(() => {
    const detailRecords = events.map(toUsageDetailRecord);
    const usageWithDetails = detailRecords.length > 0
      ? {
          ...(usage ?? {}),
          apis: {
            __overview__: {
              total_requests: detailRecords.length,
              success_count: detailRecords.filter((detail) => !detail.failed).length,
              failure_count: detailRecords.filter((detail) => detail.failed).length,
              total_tokens: detailRecords.reduce((sum, detail) => sum + Number(detail.tokens.total_tokens ?? 0), 0),
              models: {
                __overview__: {
                  total_requests: detailRecords.length,
                  success_count: detailRecords.filter((detail) => !detail.failed).length,
                  failure_count: detailRecords.filter((detail) => detail.failed).length,
                  total_tokens: detailRecords.reduce((sum, detail) => sum + Number(detail.tokens.total_tokens ?? 0), 0),
                  details: detailRecords,
                }
              }
            }
          }
        }
      : usage;
    const series =
      period === 'hour'
        ? buildHourlyTokenBreakdown(usageWithDetails, hourWindowHours, endMs)
        : buildDailyTokenBreakdown(usageWithDetails);
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

    const baseOptions = buildChartOptions({ period, labels: series.labels, isDark, isMobile });
    const options = {
      ...baseOptions,
      scales: {
        ...baseOptions.scales,
        y: {
          ...baseOptions.scales?.y,
          stacked: true
        },
        x: {
          ...baseOptions.scales?.x,
          stacked: true
        }
      }
    };

    return { chartData: data, chartOptions: options };
  }, [usage, period, isDark, isMobile, hourWindowHours, endMs, t]);

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
