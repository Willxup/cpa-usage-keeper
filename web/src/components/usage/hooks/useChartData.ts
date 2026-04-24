import { useState, useMemo } from 'react';
import type { ChartOptions } from 'chart.js';
import { buildChartData, type ChartData } from '@/utils/usage';
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
  endMs
}: UseChartDataOptions): UseChartDataReturn {
  const [requestsPeriod, setRequestsPeriod] = useState<'hour' | 'day'>('hour');
  const [tokensPeriod, setTokensPeriod] = useState<'hour' | 'day'>('hour');

  const requestsChartData = useMemo(() => {
    if (!usage) return { labels: [], datasets: [] };
    const source = requestsPeriod === 'hour' ? (usage.hourly_series ?? usage.series) : (usage.daily_series ?? usage.series);
    return buildChartData(buildChartUsageFromOverview(usage, source), requestsPeriod, 'requests', chartLines, { hourWindowHours, endMs });
  }, [usage, requestsPeriod, chartLines, hourWindowHours, endMs]);

  const tokensChartData = useMemo(() => {
    if (!usage) return { labels: [], datasets: [] };
    const source = tokensPeriod === 'hour' ? (usage.hourly_series ?? usage.series) : (usage.daily_series ?? usage.series);
    return buildChartData(buildChartUsageFromOverview(usage, source), tokensPeriod, 'tokens', chartLines, { hourWindowHours, endMs });
  }, [usage, tokensPeriod, chartLines, hourWindowHours, endMs]);

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
        isMobile
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
