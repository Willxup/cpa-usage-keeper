import { useEffect, useCallback } from 'react';
import { ApiError } from '@/lib/api';
import type { UsageOverviewResponse, UsageOverviewUsageSnapshot, UsageTimeRange } from '@/lib/types';
import { buildUsageStatsQueryKey, USAGE_STATS_STALE_TIME_MS, useUsageStatsStore } from '@/stores';
import { getCurrentOverviewUsage } from '@/utils/usage/overview';
import { buildUsageRangeQuery, normalizeUsageRange } from '@/utils/usage/rangeQuery';

export type UsagePayload = Partial<UsageOverviewUsageSnapshot>;

export type UsageOverviewPayload = Omit<UsageOverviewResponse, 'usage'> & {
  usage: UsagePayload;
};

export interface UseUsageDataReturn {
  usage: UsageOverviewPayload | null;
  currentUsage: UsageOverviewPayload | null;
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
  apiKeyId?: string;
}

export const normalizeUsageOverviewRange = normalizeUsageRange;

export function useUsageData(options: UseUsageDataOptions = {}): UseUsageDataReturn {
  const { onAuthRequired, range = '8h', customStart, customEnd, enabled = true, apiKeyId } = options;
  const usageSnapshot = useUsageStatsStore((state) => state.usage);
  const loading = useUsageStatsStore((state) => state.loading);
  const storeError = useUsageStatsStore((state) => state.error);
  const lastRefreshedAtTs = useUsageStatsStore((state) => state.lastRefreshedAt);
  const lastQueryKey = useUsageStatsStore((state) => state.lastQueryKey);
  const loadUsageStats = useUsageStatsStore((state) => state.loadUsageStats);

  const rangeQuery = buildUsageRangeQuery({ range, customStart, customEnd });
  const resolvedRange = rangeQuery.range;
  const requestStart = rangeQuery.start;
  const requestEnd = rangeQuery.end;
  const customRangeReady = rangeQuery.valid;

  const loadUsage = useCallback(async () => {
    if (!customRangeReady) return;
    try {
      await loadUsageStats({
        force: true,
        staleTimeMs: USAGE_STATS_STALE_TIME_MS,
        range: resolvedRange,
        start: requestStart,
        end: requestEnd,
        apiKeyId,
      });
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.();
      }
      throw error;
    }
  }, [apiKeyId, customRangeReady, loadUsageStats, onAuthRequired, requestEnd, requestStart, resolvedRange]);

  useEffect(() => {
    if (!enabled || !customRangeReady) {
      return;
    }
    void loadUsageStats({
      staleTimeMs: USAGE_STATS_STALE_TIME_MS,
      range: resolvedRange,
      start: requestStart,
      end: requestEnd,
      apiKeyId,
    }).catch((error) => {
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.();
      }
    });
  }, [apiKeyId, customRangeReady, enabled, loadUsageStats, onAuthRequired, requestEnd, requestStart, resolvedRange]);

  const currentQueryKey = customRangeReady ? buildUsageStatsQueryKey(resolvedRange, requestStart, requestEnd, apiKeyId) : null;
  const usage = usageSnapshot as UsageOverviewPayload | null;

  return {
    usage,
    currentUsage: getCurrentOverviewUsage(usage, currentQueryKey, lastQueryKey),
    loading,
    error: storeError || '',
    lastRefreshedAt: lastRefreshedAtTs ? new Date(lastRefreshedAtTs) : null,
    loadUsage,
  };
}
