import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Select } from '@/components/ui/Select';
import type { RankingMetric, RankingPeriod } from '../types';
import styles from '../RankingPage.module.scss';

const PERIODS: ReadonlyArray<{ value: RankingPeriod; labelKey: string; triggerLabelKey: string }> = [
  { value: 'today', labelKey: 'ranking.period_today', triggerLabelKey: 'ranking.period_trigger_today' },
  { value: 'yesterday', labelKey: 'ranking.period_yesterday', triggerLabelKey: 'ranking.period_trigger_yesterday' },
  { value: 'current_month', labelKey: 'ranking.period_current_month', triggerLabelKey: 'ranking.period_trigger_current_month' },
  { value: 'previous_month', labelKey: 'ranking.period_previous_month', triggerLabelKey: 'ranking.period_trigger_previous_month' },
];

// 触发器保持短名称，展开菜单保留完整指标语义。
const METRICS: ReadonlyArray<{ value: RankingMetric; labelKey: string; triggerLabelKey: string }> = [
  { value: 'overall', labelKey: 'ranking.metric_overall', triggerLabelKey: 'ranking.metric_short_overall' },
  { value: 'total_tokens', labelKey: 'ranking.metric_total_tokens', triggerLabelKey: 'ranking.metric_short_total_tokens' },
  { value: 'request_count', labelKey: 'ranking.metric_request_count', triggerLabelKey: 'ranking.metric_short_request_count' },
  { value: 'cache_read_rate', labelKey: 'ranking.metric_cache_read_rate', triggerLabelKey: 'ranking.metric_short_cache_read_rate' },
  { value: 'ttft_average', labelKey: 'ranking.metric_ttft_average', triggerLabelKey: 'ranking.metric_short_ttft_average' },
  { value: 'latency_average', labelKey: 'ranking.metric_latency_average', triggerLabelKey: 'ranking.metric_short_latency_average' },
  { value: 'peak_tpm', labelKey: 'ranking.metric_peak_tpm', triggerLabelKey: 'ranking.metric_short_peak_tpm' },
  { value: 'peak_rpm', labelKey: 'ranking.metric_peak_rpm', triggerLabelKey: 'ranking.metric_short_peak_rpm' },
];

export interface RankingToolbarProps {
  period: RankingPeriod;
  onPeriodChange: (period: RankingPeriod) => void;
}

export function RankingToolbar({
  period,
  onPeriodChange,
}: RankingToolbarProps) {
  const { t } = useTranslation();
  const periodOptions = useMemo(
    () => PERIODS.map((option) => ({
      value: option.value,
      label: t(option.labelKey),
      triggerLabel: t(option.triggerLabelKey),
    })),
    [t],
  );
  const currentPeriodLabel = periodOptions.find((option) => option.value === period)?.triggerLabel ?? period;

  return (
    <div className={styles.toolbar} data-ranking-toolbar>
      <div className={styles.periodControl} data-ranking-period>
        <Select
          value={period}
          options={periodOptions}
          onChange={(value) => onPeriodChange(value as RankingPeriod)}
          className={styles.periodSelect}
          ariaLabel={currentPeriodLabel}
          dropdownMinWidth={180}
          fullWidth={false}
        />
      </div>
    </div>
  );
}

export interface RankingMetricSelectProps {
  metric: RankingMetric;
  onMetricChange: (metric: RankingMetric) => void;
}

export function RankingMetricSelect({ metric, onMetricChange }: RankingMetricSelectProps) {
  const { t } = useTranslation();
  const metricOptions = useMemo(
    () => METRICS.map((option) => ({
      value: option.value,
      label: t(option.labelKey),
      triggerLabel: t(option.triggerLabelKey),
    })),
    [t],
  );
  const currentMetricLabel = metricOptions.find((option) => option.value === metric)?.triggerLabel ?? metric;

  return (
    <Select
      value={metric}
      options={metricOptions}
      onChange={(value) => onMetricChange(value as RankingMetric)}
      className={styles.titleMetricSelect}
      ariaLabel={`${t('ranking.metric_label')}: ${currentMetricLabel}`}
      dropdownMinWidth={260}
      fullWidth={false}
      id="ranking-metric-title"
    />
  );
}
