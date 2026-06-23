import { create } from 'zustand';
import { ApiError, fetchUsageOverview, fetchUsageOverviewRealtime } from '@/lib/api';
import type { OverviewRealtimeBlock, OverviewRealtimeWindow, UsageOverviewResponse, UsageTimeRange } from '@/lib/types';

export const USAGE_STATS_STALE_TIME_MS = 60_000;

interface LoadUsageStatsOptions {
  force?: boolean;
  staleTimeMs?: number;
  range?: UsageTimeRange;
  start?: string;
  end?: string;
  apiKeyId?: string;
}

interface LoadUsageStatsRealtimeOptions {
  force?: boolean;
  staleTimeMs?: number;
  apiKeyId?: string;
  realtimeWindow?: OverviewRealtimeWindow;
}

interface UsageStatsState {
  usage: UsageOverviewResponse | null;
  realtime: OverviewRealtimeBlock | null;
  loading: boolean;
  realtimeLoading: boolean;
  error: string;
  realtimeError: string;
  lastRefreshedAt: number | null;
  lastRealtimeRefreshedAt: number | null;
  lastQueryKey: string | null;
  lastRealtimeQueryKey: string | null;
  lastRealtimeErrorQueryKey: string | null;
  loadUsageStats: (options?: LoadUsageStatsOptions) => Promise<void>;
  loadUsageStatsRealtime: (options?: LoadUsageStatsRealtimeOptions) => Promise<void>;
  clearUsageStats: () => void;
}

let activeOverviewRequest: Promise<void> | null = null;
let activeOverviewRequestKey: string | null = null;
let activeOverviewRequestController: AbortController | null = null;
let activeRealtimeRequest: Promise<void> | null = null;
let activeRealtimeRequestKey: string | null = null;
let activeRealtimeRequestController: AbortController | null = null;

export const buildUsageStatsQueryKey = (range: UsageTimeRange, start?: string, end?: string, apiKeyId?: string): string =>
  `${range}:${start ?? ''}:${end ?? ''}:${apiKeyId ?? ''}`;

const buildRealtimeQueryKey = (apiKeyId?: string, realtimeWindow?: OverviewRealtimeWindow): string =>
  `${apiKeyId ?? ''}:${realtimeWindow ?? ''}`;

export const useUsageStatsStore = create<UsageStatsState>((set, get) => ({
  usage: null,
  realtime: null,
  loading: false,
  realtimeLoading: false,
  error: '',
  realtimeError: '',
  lastRefreshedAt: null,
  lastRealtimeRefreshedAt: null,
  lastQueryKey: null,
  lastRealtimeQueryKey: null,
  lastRealtimeErrorQueryKey: null,
  loadUsageStats: async (options = {}) => {
    const {
      force = false,
      staleTimeMs = USAGE_STATS_STALE_TIME_MS,
      range = '8h',
      start,
      end,
      apiKeyId,
    } = options;
    const { lastRefreshedAt, loading, usage, lastQueryKey } = get();
    const now = Date.now();
    const queryKey = buildUsageStatsQueryKey(range, start, end, apiKeyId);
    const overviewFresh = Boolean(!force && usage && lastRefreshedAt && lastQueryKey === queryKey && now - lastRefreshedAt < staleTimeMs);

    if (overviewFresh) {
      return;
    }

    if (loading && activeOverviewRequest) {
      if (activeOverviewRequestKey === queryKey) {
        return activeOverviewRequest;
      }
      activeOverviewRequestController?.abort();
    }

    const controller = new AbortController();
    activeOverviewRequestController = controller;
    activeOverviewRequestKey = queryKey;
    set({
      loading: true,
      error: '',
    });

    activeOverviewRequest = (async () => {
      try {
        const overview = await fetchUsageOverview(range, start, end, controller.signal, apiKeyId);
        if (activeOverviewRequestController !== controller) return;
        set({
          usage: overview,
          loading: false,
          error: '',
          lastRefreshedAt: Date.now(),
          lastQueryKey: queryKey,
        });
      } catch (error) {
        if (controller.signal.aborted) {
          return;
        }
        const message = error instanceof ApiError && error.status === 401
          ? 'AUTH_REQUIRED'
          : error instanceof Error
            ? error.message
            : 'Failed to load usage overview';
        if (activeOverviewRequestController === controller) {
          set({
            loading: false,
            error: message,
          });
        }
        throw error;
      } finally {
        if (activeOverviewRequestController === controller) {
          activeOverviewRequest = null;
          activeOverviewRequestKey = null;
          activeOverviewRequestController = null;
        }
      }
    })();

    return activeOverviewRequest;
  },
  loadUsageStatsRealtime: async (options = {}) => {
    const {
      force = false,
      staleTimeMs = USAGE_STATS_STALE_TIME_MS,
      apiKeyId,
      realtimeWindow,
    } = options;
    const { lastRealtimeRefreshedAt, realtimeLoading, realtime, lastRealtimeQueryKey, realtimeError, lastRealtimeErrorQueryKey } = get();
    const now = Date.now();
    const realtimeQueryKey = buildRealtimeQueryKey(apiKeyId, realtimeWindow);
    const realtimeFresh = Boolean(!force && realtime && lastRealtimeRefreshedAt && lastRealtimeQueryKey === realtimeQueryKey && now - lastRealtimeRefreshedAt < staleTimeMs);

    if (realtimeFresh) {
      if (realtimeError && lastRealtimeErrorQueryKey !== realtimeQueryKey) {
        set({ realtimeError: '', lastRealtimeErrorQueryKey: null });
      }
      return;
    }

    if (realtimeLoading && activeRealtimeRequest) {
      if (activeRealtimeRequestKey === realtimeQueryKey) {
        return activeRealtimeRequest;
      }
      activeRealtimeRequestController?.abort();
    }

    const controller = new AbortController();
    activeRealtimeRequestController = controller;
    activeRealtimeRequestKey = realtimeQueryKey;
    set({
      realtimeLoading: true,
      realtimeError: '',
      lastRealtimeErrorQueryKey: null,
    });

    activeRealtimeRequest = (async () => {
      try {
        const nextRealtime = await fetchUsageOverviewRealtime({
          signal: controller.signal,
          apiKeyId,
          window: realtimeWindow,
        });
        if (activeRealtimeRequestController !== controller) return;
        set({
          realtime: nextRealtime,
          realtimeLoading: false,
          realtimeError: '',
          lastRealtimeErrorQueryKey: null,
          lastRealtimeRefreshedAt: Date.now(),
          lastRealtimeQueryKey: realtimeQueryKey,
        });
      } catch (error) {
        if (controller.signal.aborted) {
          return;
        }
        const message = error instanceof ApiError && error.status === 401
          ? 'AUTH_REQUIRED'
          : error instanceof Error
            ? error.message
            : 'Failed to load usage overview realtime';
        if (activeRealtimeRequestController === controller) {
          set({ realtimeLoading: false, realtimeError: message, lastRealtimeErrorQueryKey: realtimeQueryKey });
        }
        if (error instanceof ApiError && error.status === 401) {
          throw error;
        }
      } finally {
        if (activeRealtimeRequestController === controller) {
          activeRealtimeRequest = null;
          activeRealtimeRequestKey = null;
          activeRealtimeRequestController = null;
        }
      }
    })();

    return activeRealtimeRequest;
  },
  clearUsageStats: () => {
    activeOverviewRequestController?.abort();
    activeRealtimeRequestController?.abort();
    activeOverviewRequest = null;
    activeOverviewRequestKey = null;
    activeOverviewRequestController = null;
    activeRealtimeRequest = null;
    activeRealtimeRequestKey = null;
    activeRealtimeRequestController = null;
    set({ usage: null, realtime: null, error: '', realtimeError: '', loading: false, realtimeLoading: false, lastRefreshedAt: null, lastRealtimeRefreshedAt: null, lastQueryKey: null, lastRealtimeQueryKey: null, lastRealtimeErrorQueryKey: null });
  }
}));
