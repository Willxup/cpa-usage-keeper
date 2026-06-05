import { useCallback, useMemo } from 'react';
import { calculateCacheRate } from '@/utils/usage';
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
  cachedRateSparkline: SparklineBundle | null;
  costSparkline: SparklineBundle | null;
}

export interface UsageSparklineSeries {
  labels: string[];
  requests: number[];
  tokens: number[];
  rpm: number[];
  tpm: number[];
  cachedRate: number[];
  cost: number[];
}

export const SPARKLINE_COLORS = {
  requests: { border: '#3b82f6', background: 'rgba(59, 130, 246, 0.18)' },
  tokens: { border: '#8b5cf6', background: 'rgba(139, 92, 246, 0.18)' },
  rpm: { border: '#22c55e', background: 'rgba(34, 197, 94, 0.18)' },
  tpm: { border: '#f97316', background: 'rgba(249, 115, 22, 0.18)' },
  cachedRate: { border: '#14b8a6', background: 'rgba(20, 184, 166, 0.18)' },
  cost: { border: '#f59e0b', background: 'rgba(245, 158, 11, 0.18)' },
} as const;

const normalizeSparklineNumber = (value: unknown): number => {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? Math.max(parsed, 0) : 0;
};

export function buildUsageSparklineSeries({ usage }: Omit<UseSparklinesOptions, 'loading'>): UsageSparklineSeries {
  if (!usage?.series) {
    return { labels: [], requests: [], tokens: [], rpm: [], tpm: [], cachedRate: [], cost: [] };
  }

  const labels = Object.keys(usage.series.requests ?? {}).sort((a, b) => a.localeCompare(b));
  if (!labels.length) {
    return { labels: [], requests: [], tokens: [], rpm: [], tpm: [], cachedRate: [], cost: [] };
  }

  return {
    labels,
    requests: labels.map((label) => normalizeSparklineNumber(usage.series?.requests?.[label])),
    tokens: labels.map((label) => normalizeSparklineNumber(usage.series?.tokens?.[label])),
    rpm: labels.map((label) => normalizeSparklineNumber(usage.series?.rpm?.[label])),
    tpm: labels.map((label) => normalizeSparklineNumber(usage.series?.tpm?.[label])),
    cachedRate: labels.map((label) => {
      const inputTokens = normalizeSparklineNumber(usage.series?.input_tokens?.[label]);
      const cachedTokens = normalizeSparklineNumber(usage.series?.cached_tokens?.[label]);
      return calculateCacheRate({ inputTokens, cachedTokens }) ?? 0;
    }),
    cost: labels.map((label) => normalizeSparklineNumber(usage.series?.cost?.[label])),
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
    () => buildSparkline({ labels: series.labels, data: series.requests }, SPARKLINE_COLORS.requests.border, SPARKLINE_COLORS.requests.background),
    [buildSparkline, series.labels, series.requests]
  );

  const tokensSparkline = useMemo(
    () => buildSparkline({ labels: series.labels, data: series.tokens }, SPARKLINE_COLORS.tokens.border, SPARKLINE_COLORS.tokens.background),
    [buildSparkline, series.labels, series.tokens]
  );

  const rpmSparkline = useMemo(
    () => buildSparkline({ labels: series.labels, data: series.rpm }, SPARKLINE_COLORS.rpm.border, SPARKLINE_COLORS.rpm.background),
    [buildSparkline, series.labels, series.rpm]
  );

  const tpmSparkline = useMemo(
    () => buildSparkline({ labels: series.labels, data: series.tpm }, SPARKLINE_COLORS.tpm.border, SPARKLINE_COLORS.tpm.background),
    [buildSparkline, series.labels, series.tpm]
  );

  const cachedRateSparkline = useMemo(
    () => buildSparkline({ labels: series.labels, data: series.cachedRate }, SPARKLINE_COLORS.cachedRate.border, SPARKLINE_COLORS.cachedRate.background),
    [buildSparkline, series.cachedRate, series.labels]
  );

  const costSparkline = useMemo(
    () => buildSparkline({ labels: series.labels, data: series.cost }, SPARKLINE_COLORS.cost.border, SPARKLINE_COLORS.cost.background),
    [buildSparkline, series.labels, series.cost]
  );

  return {
    requestsSparkline,
    tokensSparkline,
    rpmSparkline,
    tpmSparkline,
    cachedRateSparkline,
    costSparkline
  };
}
