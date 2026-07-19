import { useEffect, useCallback, useMemo } from 'react';
import { ApiError } from '@/lib/api';
import type { UsageCustomRangeUnit, UsageOverviewResponse, UsageOverviewUsageSnapshot, UsageTimeRange } from '@/lib/types';
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
  customUnit?: UsageCustomRangeUnit;
  customStart?: string;
  customEnd?: string;
  enabled?: boolean;
  apiKeyId?: string;
}

export const normalizeUsageOverviewRange = normalizeUsageRange;

export function useUsageData(options: UseUsageDataOptions = {}): UseUsageDataReturn {
  const { onAuthRequired, range = '8h', customUnit, customStart, customEnd, enabled = true, apiKeyId } = options;
  const usageSnapshot = useUsageStatsStore((state) => state.usage);
  const loading = useUsageStatsStore((state) => state.loading);
  const storeError = useUsageStatsStore((state) => state.error);
  const lastRefreshedAtTs = useUsageStatsStore((state) => state.lastRefreshedAt);
  const lastQueryKey = useUsageStatsStore((state) => state.lastQueryKey);
  const loadUsageStats = useUsageStatsStore((state) => state.loadUsageStats);

  const rangeQuery = useMemo(
    () => buildUsageRangeQuery({ range, customUnit, customStart, customEnd }),
    [customEnd, customStart, customUnit, range],
  );

  const loadUsage = useCallback(async () => {
    if (!rangeQuery.valid) return;
    try {
      await loadUsageStats({
        force: true,
        staleTimeMs: USAGE_STATS_STALE_TIME_MS,
        range: rangeQuery.range,
        unit: rangeQuery.unit,
        start: rangeQuery.start,
        end: rangeQuery.end,
        apiKeyId,
      });
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.();
      }
      throw error;
    }
  }, [apiKeyId, loadUsageStats, onAuthRequired, rangeQuery]);

  useEffect(() => {
    if (!enabled || !rangeQuery.valid) {
      return;
    }
    void loadUsageStats({
      staleTimeMs: USAGE_STATS_STALE_TIME_MS,
      range: rangeQuery.range,
      unit: rangeQuery.unit,
      start: rangeQuery.start,
      end: rangeQuery.end,
      apiKeyId,
    }).catch((error) => {
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.();
      }
    });
  }, [apiKeyId, enabled, loadUsageStats, onAuthRequired, rangeQuery]);

  const currentQueryKey = rangeQuery.valid ? buildUsageStatsQueryKey(rangeQuery, apiKeyId) : null;
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
