import { create } from 'zustand';
import { ApiError, fetchUsageOverview } from '@/lib/api';
import type { UsageOverviewResponse, UsageTimeRange } from '@/lib/types';

export const USAGE_STATS_STALE_TIME_MS = 60_000;

interface LoadUsageStatsOptions {
  force?: boolean;
  staleTimeMs?: number;
  range?: UsageTimeRange;
  start?: string;
  end?: string;
}

interface UsageStatsState {
  usage: UsageOverviewResponse | null;
  loading: boolean;
  error: string;
  lastRefreshedAt: number | null;
  lastQueryKey: string | null;
  loadUsageStats: (options?: LoadUsageStatsOptions) => Promise<void>;
  clearUsageStats: () => void;
}

let activeRequest: Promise<void> | null = null;
let activeRequestKey: string | null = null;
let activeRequestController: AbortController | null = null;

const buildQueryKey = (range: UsageTimeRange, start?: string, end?: string): string =>
  `${range}:${start ?? ''}:${end ?? ''}`;

export const useUsageStatsStore = create<UsageStatsState>((set, get) => ({
  usage: null,
  loading: false,
  error: '',
  lastRefreshedAt: null,
  lastQueryKey: null,
  loadUsageStats: async (options = {}) => {
    const {
      force = false,
      staleTimeMs = USAGE_STATS_STALE_TIME_MS,
      range = 'all',
      start,
      end,
    } = options;
    const { lastRefreshedAt, loading, usage, lastQueryKey } = get();
    const now = Date.now();
    const queryKey = buildQueryKey(range, start, end);

    if (!force && usage && lastRefreshedAt && lastQueryKey === queryKey && now - lastRefreshedAt < staleTimeMs) {
      return;
    }

    if (loading && activeRequest) {
      if (activeRequestKey === queryKey) {
        return activeRequest;
      }
      activeRequestController?.abort();
    }

    const controller = new AbortController();
    activeRequestController = controller;
    activeRequestKey = queryKey;
    set({ loading: true, error: '' });

    activeRequest = (async () => {
      try {
        const response = await fetchUsageOverview(range, start, end, controller.signal);
        if (activeRequestController !== controller) {
          return;
        }
        set({
          usage: response,
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
            : 'Failed to load usage overview'
        if (activeRequestController === controller) {
          set({
            loading: false,
            error: message
          });
        }
        throw error;
      } finally {
        if (activeRequestController === controller) {
          activeRequest = null;
          activeRequestKey = null;
          activeRequestController = null;
        }
      }
    })();

    return activeRequest;
  },
  clearUsageStats: () => set({ usage: null, error: '', loading: false, lastRefreshedAt: null, lastMode: null, lastQueryKey: null })
}));
