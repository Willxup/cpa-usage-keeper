import { useCallback, useMemo } from 'react';
import type { UsageOverviewPayload } from './useUsageData';

export interface SparklineData {
  labels: string[];
  datasets: [
    {
      data: number[];
      borderColor: string;
      backgroundColor: string;
      fill: boolean;
      tension: number;
      pointRadius: number;
      borderWidth: number;
    }
  ];
}

export interface SparklineBundle {
  data: SparklineData;
}

export interface UseSparklinesOptions {
  usage: UsageOverviewPayload | null;
  loading: boolean;
}

export interface UseSparklinesReturn {
  requestsSparkline: SparklineBundle | null;
  tokensSparkline: SparklineBundle | null;
  rpmSparkline: SparklineBundle | null;
  tpmSparkline: SparklineBundle | null;
  costSparkline: SparklineBundle | null;
}

export interface UsageSparklineSeries {
  labels: string[];
  requests: number[];
  tokens: number[];
  rpm: number[];
  tpm: number[];
  cost: number[];
}

export function buildUsageSparklineSeries({ usage }: Omit<UseSparklinesOptions, 'loading'>): UsageSparklineSeries {
  if (!usage?.series) {
    return { labels: [], requests: [], tokens: [], rpm: [], tpm: [], cost: [] };
  }

  const labels = Object.keys(usage.series.requests ?? {}).sort((a, b) => a.localeCompare(b));
  if (!labels.length) {
    return { labels: [], requests: [], tokens: [], rpm: [], tpm: [], cost: [] };
  }

  return {
    labels,
    requests: labels.map((label) => Number(usage.series?.requests?.[label] ?? 0)),
    tokens: labels.map((label) => Number(usage.series?.tokens?.[label] ?? 0)),
    rpm: labels.map((label) => Number(usage.series?.rpm?.[label] ?? 0)),
    tpm: labels.map((label) => Number(usage.series?.tpm?.[label] ?? 0)),
    cost: labels.map((label) => Number(usage.series?.cost?.[label] ?? 0)),
  };
}

export function useSparklines({ usage, loading }: UseSparklinesOptions): UseSparklinesReturn {
  const series = useMemo(
    () => buildUsageSparklineSeries({ usage }),
    [usage]
  );

  const buildSparkline = useCallback(
    (
      input: { labels: string[]; data: number[] },
      color: string,
      backgroundColor: string
    ): SparklineBundle | null => {
      if (loading || !input?.data?.length) {
        return null;
      }
      return {
        data: {
          labels: input.labels,
          datasets: [
            {
              data: input.data,
              borderColor: color,
              backgroundColor,
              fill: true,
              tension: 0.45,
              pointRadius: 0,
              borderWidth: 2
            }
          ]
        }
      };
    },
    [loading]
  );

  const requestsSparkline = useMemo(
    () => buildSparkline({ labels: series.labels, data: series.requests }, '#8b8680', 'rgba(139, 134, 128, 0.18)'),
    [buildSparkline, series.labels, series.requests]
  );

  const tokensSparkline = useMemo(
    () => buildSparkline({ labels: series.labels, data: series.tokens }, '#8b5cf6', 'rgba(139, 92, 246, 0.18)'),
    [buildSparkline, series.labels, series.tokens]
  );

  const rpmSparkline = useMemo(
    () => buildSparkline({ labels: series.labels, data: series.rpm }, '#22c55e', 'rgba(34, 197, 94, 0.18)'),
    [buildSparkline, series.labels, series.rpm]
  );

  const tpmSparkline = useMemo(
    () => buildSparkline({ labels: series.labels, data: series.tpm }, '#f97316', 'rgba(249, 115, 22, 0.18)'),
    [buildSparkline, series.labels, series.tpm]
  );

  const costSparkline = useMemo(
    () => buildSparkline({ labels: series.labels, data: series.cost }, '#f59e0b', 'rgba(245, 158, 11, 0.18)'),
    [buildSparkline, series.labels, series.cost]
  );

  return {
    requestsSparkline,
    tokensSparkline,
    rpmSparkline,
    tpmSparkline,
    costSparkline
  };
}
