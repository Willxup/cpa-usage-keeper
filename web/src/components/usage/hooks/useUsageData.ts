import { useEffect, useCallback } from 'react';
import { ApiError } from '@/lib/api';
import type { UsageOverviewResponse, UsageSnapshot, UsageTimeRange } from '@/lib/types';
import { USAGE_STATS_STALE_TIME_MS, useUsageStatsStore } from '@/stores';

export type UsagePayload = Partial<UsageSnapshot>;

export type UsageOverviewPayload = Omit<UsageOverviewResponse, 'usage'> & {
  usage: UsagePayload;
};

export interface UseUsageDataReturn {
  usage: UsageOverviewPayload | null;
  loading: boolean;
  error: string;
  lastRefreshedAt: Date | null;
  loadUsage: () => Promise<void>;
}

export interface UseUsageDataOptions {
  onAuthRequired?: () => void;
  range?: UsageTimeRange;
  customStart?: string;
  customEnd?: string;
  enabled?: boolean;
}

const toRangeQuery = (value: string): UsageTimeRange => (
  value === '4h' || value === '8h' || value === '12h' || value === '24h' || value === 'today' || value === '7d' || value === 'all' || value === 'custom'
    ? value
    : 'all'
);

const toCustomBoundary = (value: string | undefined, endOfDay: boolean): string | undefined => {
  if (!value) return undefined;
  const suffix = endOfDay ? 'T23:59:59.999Z' : 'T00:00:00.000Z';
  const date = new Date(`${value}${suffix}`);
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
}

export function useUsageData(options: UseUsageDataOptions = {}): UseUsageDataReturn {
  const { onAuthRequired, range = 'all', customStart, customEnd, enabled = true } = options;
  const usageSnapshot = useUsageStatsStore((state) => state.usage);
  const loading = useUsageStatsStore((state) => state.loading);
  const storeError = useUsageStatsStore((state) => state.error);
  const lastRefreshedAtTs = useUsageStatsStore((state) => state.lastRefreshedAt);
  const loadUsageStats = useUsageStatsStore((state) => state.loadUsageStats);

  const resolvedRange = toRangeQuery(range);
  const requestStart = resolvedRange === 'custom' ? toCustomBoundary(customStart, false) : undefined;
  const requestEnd = resolvedRange === 'custom' ? toCustomBoundary(customEnd, true) : undefined;

  const loadUsage = useCallback(async () => {
    try {
      await loadUsageStats({
        force: true,
        staleTimeMs: USAGE_STATS_STALE_TIME_MS,
        range: resolvedRange,
        start: requestStart,
        end: requestEnd,
      });
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.();
      }
      throw error;
    }
  }, [loadUsageStats, onAuthRequired, requestEnd, requestStart, resolvedRange]);

  useEffect(() => {
    if (!enabled) {
      return;
    }
    void loadUsageStats({
      staleTimeMs: USAGE_STATS_STALE_TIME_MS,
      range: resolvedRange,
      start: requestStart,
      end: requestEnd,
    }).catch((error) => {
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.();
      }
    });
  }, [enabled, loadUsageStats, onAuthRequired, requestEnd, requestStart, resolvedRange]);

  return {
    usage: usageSnapshot as UsageOverviewPayload | null,
    loading,
    error: storeError || '',
    lastRefreshedAt: lastRefreshedAtTs ? new Date(lastRefreshedAtTs) : null,
    loadUsage,
  };
}
