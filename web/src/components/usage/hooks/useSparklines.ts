import { useCallback, useMemo } from 'react';
import {
  buildDailyCostSeries,
  buildHourlyCostSeries,
  calculateCost,
  collectUsageDetails,
  extractTotalTokens,
  type ModelPrice,
  type UsageTimeRange
} from '@/utils/usage';
import type { UsagePayload } from './useUsageData';

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
  usage: UsagePayload | null;
  loading: boolean;
  nowMs: number;
  timeRange: UsageTimeRange;
  hourWindowHours?: number;
  modelPrices: Record<string, ModelPrice>;
}

export interface UseSparklinesReturn {
  requestsSparkline: SparklineBundle | null;
  tokensSparkline: SparklineBundle | null;
  rpmSparkline: SparklineBundle | null;
  tpmSparkline: SparklineBundle | null;
  costSparkline: SparklineBundle | null;
}

export function useSparklines({ usage, loading, nowMs, timeRange, hourWindowHours, modelPrices }: UseSparklinesOptions): UseSparklinesReturn {
  const series = useMemo(() => {
    if (!usage) {
      return { labels: [], requests: [], tokens: [], cost: [], rpm: [], tpm: [] };
    }

    const details = collectUsageDetails(usage);
    if (!details.length) {
      const isDaily = timeRange === '7d' || timeRange === 'all';
      const requestMap = isDaily ? usage.requests_by_day ?? {} : usage.requests_by_hour ?? {};
      const tokenMap = isDaily ? usage.tokens_by_day ?? {} : usage.tokens_by_hour ?? {};
      const keys = Object.keys(requestMap).sort((a, b) => a.localeCompare(b));
      if (!keys.length) {
        return { labels: [], requests: [], tokens: [], cost: [], rpm: [], tpm: [] };
      }
      const requests = keys.map((key) => Number(requestMap[key] ?? 0));
      const tokens = keys.map((key) => Number(tokenMap[key] ?? 0));
      const divisor = isDaily ? 24 * 60 : 60;
      return {
        labels: keys,
        requests,
        tokens,
        cost: new Array(keys.length).fill(0),
        rpm: requests.map((value) => value / divisor),
        tpm: tokens.map((value) => value / divisor),
      };
    }

    if (timeRange === '7d' || timeRange === 'all') {
      const grouped = new Map<string, { requests: number; tokens: number; cost: number }>();
      details.forEach((detail) => {
        const key = detail.timestamp.slice(0, 10);
        const current = grouped.get(key) ?? { requests: 0, tokens: 0, cost: 0 };
        current.requests += 1;
        current.tokens += extractTotalTokens(detail);
        current.cost += calculateCost(detail, modelPrices);
        grouped.set(key, current);
      });
      const labels = Array.from(grouped.keys()).sort((a, b) => a.localeCompare(b));
      const requests = labels.map((label) => grouped.get(label)?.requests ?? 0);
      const tokens = labels.map((label) => grouped.get(label)?.tokens ?? 0);
      const cost = labels.map((label) => grouped.get(label)?.cost ?? 0);
      const rpm = requests.map((value) => value / (24 * 60));
      const tpm = tokens.map((value) => value / (24 * 60));
      return { labels, requests, tokens, cost, rpm, tpm };
    }

    const windowHours = hourWindowHours ?? 24;
    const currentHour = new Date(Number.isFinite(nowMs) && nowMs > 0 ? nowMs : Date.now());
    currentHour.setMinutes(0, 0, 0);
    const earliestHour = new Date(currentHour);
    earliestHour.setHours(earliestHour.getHours() - (windowHours - 1));
    const earliestMs = earliestHour.getTime();
    const labels = Array.from({ length: windowHours }, (_, index) => {
      const bucketDate = new Date(earliestMs + index * 60 * 60 * 1000);
      const h = bucketDate.getHours().toString().padStart(2, '0');
      return `${h}:00`;
    });
    const requests = new Array(windowHours).fill(0);
    const tokens = new Array(windowHours).fill(0);
    const cost = new Array(windowHours).fill(0);

    details.forEach((detail) => {
      const timestamp = detail.__timestampMs ?? 0;
      if (!Number.isFinite(timestamp) || timestamp <= 0) return;
      const normalized = new Date(timestamp);
      normalized.setMinutes(0, 0, 0);
      const bucketStart = normalized.getTime();
      const bucketIndex = Math.floor((bucketStart - earliestMs) / (60 * 60 * 1000));
      if (bucketIndex < 0 || bucketIndex >= windowHours) return;
      requests[bucketIndex] += 1;
      tokens[bucketIndex] += extractTotalTokens(detail);
      cost[bucketIndex] += calculateCost(detail, modelPrices);
    });

    const rpm = requests.map((value) => value / 60);
    const tpm = tokens.map((value) => value / 60);
    return { labels, requests, tokens, cost, rpm, tpm };
  }, [hourWindowHours, modelPrices, nowMs, timeRange, usage]);

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
