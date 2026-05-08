import { useState, useMemo, useEffect } from 'react';
import type { ChartOptions } from 'chart.js';
import { buildChartData, formatCompactTokenValue, type ChartData } from '@/utils/usage';
import { buildChartOptions } from '@/utils/usage/chartConfig';
import type { UsageOverviewPayload } from './useUsageData';
import type { UsageOverviewSeries } from '@/lib/types';

const buildChartUsageFromOverview = (usage: UsageOverviewPayload, source?: UsageOverviewSeries) => {
  if (!source) return usage.usage;
  return {
    ...usage.usage,
    requests_by_hour: source.requests,
    tokens_by_hour: source.tokens,
    requests_by_day: source.requests,
    tokens_by_day: source.tokens,
    model_series: Object.fromEntries(Object.entries(source.models ?? {}).map(([model, series]) => [model, {
      requests_by_hour: series.requests,
      tokens_by_hour: series.tokens,
      requests_by_day: series.requests,
      tokens_by_day: series.tokens,
    }])),
  };
};

export interface UseChartDataOptions {
  usage: UsageOverviewPayload | null;
  chartLines: string[];
  isDark: boolean;
  isMobile: boolean;
  hourWindowHours?: number;
  endMs?: number;
  includeFinalHourBucket?: boolean;
  preferredPeriod?: 'hour' | 'day';
}

export interface UseChartDataReturn {
  requestsPeriod: 'hour' | 'day';
  setRequestsPeriod: (period: 'hour' | 'day') => void;
  tokensPeriod: 'hour' | 'day';
  setTokensPeriod: (period: 'hour' | 'day') => void;
  requestsChartData: ChartData;
  tokensChartData: ChartData;
  requestsChartOptions: ChartOptions<'line'>;
  tokensChartOptions: ChartOptions<'line'>;
}

export function useChartData({
  usage,
  chartLines,
  isDark,
  isMobile,
  hourWindowHours,
  endMs,
  includeFinalHourBucket = false,
  preferredPeriod = 'hour'
}: UseChartDataOptions): UseChartDataReturn {
  const [requestsPeriod, setRequestsPeriod] = useState<'hour' | 'day'>(preferredPeriod);
  const [tokensPeriod, setTokensPeriod] = useState<'hour' | 'day'>(preferredPeriod);

  useEffect(() => {
    setRequestsPeriod(preferredPeriod);
    setTokensPeriod(preferredPeriod);
  }, [preferredPeriod]);

  const requestsChartData = useMemo(() => {
    if (!usage) return { labels: [], datasets: [] };
    const source = requestsPeriod === 'hour' ? (usage.hourly_series ?? usage.series) : (usage.daily_series ?? usage.series);
    return buildChartData(buildChartUsageFromOverview(usage, source), requestsPeriod, 'requests', chartLines, { hourWindowHours, endMs, includeFinalHourBucket });
  }, [usage, requestsPeriod, chartLines, hourWindowHours, endMs, includeFinalHourBucket]);

  const tokensChartData = useMemo(() => {
    if (!usage) return { labels: [], datasets: [] };
    const source = tokensPeriod === 'hour' ? (usage.hourly_series ?? usage.series) : (usage.daily_series ?? usage.series);
    return buildChartData(buildChartUsageFromOverview(usage, source), tokensPeriod, 'tokens', chartLines, { hourWindowHours, endMs, includeFinalHourBucket });
  }, [usage, tokensPeriod, chartLines, hourWindowHours, endMs, includeFinalHourBucket]);

  const requestsChartOptions = useMemo(
    () =>
      buildChartOptions({
        period: requestsPeriod,
        labels: requestsChartData.labels,
        isDark,
        isMobile
      }),
    [requestsPeriod, requestsChartData.labels, isDark, isMobile]
  );

  const tokensChartOptions = useMemo(
    () =>
      buildChartOptions({
        period: tokensPeriod,
        labels: tokensChartData.labels,
        isDark,
        isMobile,
        valueFormatter: (value) => formatCompactTokenValue(value),
        tooltipValueFormatter: (value) => formatCompactTokenValue(value, true)
      }),
    [tokensPeriod, tokensChartData.labels, isDark, isMobile]
  );

  return {
    requestsPeriod,
    setRequestsPeriod,
    tokensPeriod,
    setTokensPeriod,
    requestsChartData,
    tokensChartData,
    requestsChartOptions,
    tokensChartOptions
  };
}
