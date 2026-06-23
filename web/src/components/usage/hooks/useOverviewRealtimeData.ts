import { useCallback, useEffect } from 'react';
import { ApiError } from '@/lib/api';
import type { OverviewRealtimeBlock, OverviewRealtimeWindow } from '@/lib/types';
import { USAGE_STATS_STALE_TIME_MS, useUsageStatsStore } from '@/stores';

export interface UseOverviewRealtimeDataReturn {
  realtime: OverviewRealtimeBlock | null;
  loading: boolean;
  error: string;
  lastRefreshedAt: Date | null;
  loadRealtime: () => Promise<void>;
}

export interface UseOverviewRealtimeDataOptions {
  onAuthRequired?: () => void;
  enabled?: boolean;
  apiKeyId?: string;
  realtimeWindow?: OverviewRealtimeWindow;
}

interface ResolveDisplayRealtimeOptions {
  realtime: OverviewRealtimeBlock | null;
  lastRealtimeQueryKey: string | null;
  lastRealtimeErrorQueryKey?: string | null;
  realtimeQueryKey: string;
}

const realtimeQueryScope = (queryKey: string | null): string | null => {
  if (queryKey === null) return null;
  const separatorIndex = queryKey.lastIndexOf(':');
  return separatorIndex === -1 ? queryKey : queryKey.slice(0, separatorIndex);
};

export function resolveDisplayRealtime({
  realtime,
  lastRealtimeQueryKey,
  lastRealtimeErrorQueryKey,
  realtimeQueryKey,
}: ResolveDisplayRealtimeOptions): OverviewRealtimeBlock | null {
  if (lastRealtimeQueryKey === realtimeQueryKey) return realtime;
  if (lastRealtimeErrorQueryKey === realtimeQueryKey) return null;
  if (realtime && realtimeQueryScope(lastRealtimeQueryKey) === realtimeQueryScope(realtimeQueryKey)) {
    return realtime;
  }
  return null;
}

export function useOverviewRealtimeData(options: UseOverviewRealtimeDataOptions = {}): UseOverviewRealtimeDataReturn {
  const { onAuthRequired, enabled = true, apiKeyId, realtimeWindow } = options;
  const realtime = useUsageStatsStore((state) => state.realtime);
  const loading = useUsageStatsStore((state) => state.realtimeLoading);
  const storeError = useUsageStatsStore((state) => state.realtimeError);
  const lastRealtimeQueryKey = useUsageStatsStore((state) => state.lastRealtimeQueryKey);
  const lastRealtimeErrorQueryKey = useUsageStatsStore((state) => state.lastRealtimeErrorQueryKey);
  const lastRefreshedAtTs = useUsageStatsStore((state) => state.lastRealtimeRefreshedAt);
  const loadUsageStatsRealtime = useUsageStatsStore((state) => state.loadUsageStatsRealtime);
  const realtimeQueryKey = `${apiKeyId ?? ''}:${realtimeWindow ?? ''}`;
  const currentRealtime = resolveDisplayRealtime({
    realtime,
    lastRealtimeQueryKey,
    lastRealtimeErrorQueryKey,
    realtimeQueryKey,
  });

  const loadRealtime = useCallback(async () => {
    try {
      await loadUsageStatsRealtime({
        force: true,
        staleTimeMs: USAGE_STATS_STALE_TIME_MS,
        apiKeyId,
        realtimeWindow,
      });
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.();
      }
      throw error;
    }
  }, [apiKeyId, loadUsageStatsRealtime, onAuthRequired, realtimeWindow]);

  useEffect(() => {
    if (!enabled) {
      return;
    }
    void loadUsageStatsRealtime({
      staleTimeMs: USAGE_STATS_STALE_TIME_MS,
      apiKeyId,
      realtimeWindow,
    }).catch((error) => {
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.();
      }
    });
  }, [apiKeyId, enabled, loadUsageStatsRealtime, onAuthRequired, realtimeWindow]);

  return {
    realtime: currentRealtime,
    loading,
    error: lastRealtimeErrorQueryKey === realtimeQueryKey ? storeError || '' : '',
    lastRefreshedAt: lastRefreshedAtTs ? new Date(lastRefreshedAtTs) : null,
    loadRealtime,
  };
}
