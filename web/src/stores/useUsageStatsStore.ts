import { create } from 'zustand';
import { ApiError, fetchUsage, fetchUsageOverview } from '@/lib/api';
import type { UsageSnapshot, UsageTimeRange } from '@/lib/types';

export const USAGE_STATS_STALE_TIME_MS = 60_000;

interface LoadUsageStatsOptions {
  force?: boolean;
  staleTimeMs?: number;
  mode?: 'full' | 'overview';
  range?: UsageTimeRange;
  start?: string;
  end?: string;
}

interface UsageStatsState {
  usage: UsageSnapshot | null;
  loading: boolean;
  error: string;
  lastRefreshedAt: number | null;
  lastMode: 'full' | 'overview' | null;
  lastQueryKey: string | null;
  loadUsageStats: (options?: LoadUsageStatsOptions) => Promise<void>;
  clearUsageStats: () => void;
}

let activeRequest: Promise<void> | null = null;

const buildQueryKey = (mode: 'full' | 'overview', range: UsageTimeRange, start?: string, end?: string): string =>
  `${mode}:${range}:${start ?? ''}:${end ?? ''}`;

export const useUsageStatsStore = create<UsageStatsState>((set, get) => ({
  usage: null,
  loading: false,
  error: '',
  lastRefreshedAt: null,
  lastMode: null,
  lastQueryKey: null,
  loadUsageStats: async (options = {}) => {
    const {
      force = false,
      staleTimeMs = USAGE_STATS_STALE_TIME_MS,
      mode = 'full',
      range = 'all',
      start,
      end,
    } = options;
    const { lastRefreshedAt, loading, usage, lastQueryKey } = get();
    const now = Date.now();
    const queryKey = buildQueryKey(mode, range, start, end);

    if (!force && usage && lastRefreshedAt && lastQueryKey === queryKey && now - lastRefreshedAt < staleTimeMs) {
      return;
    }

    if (loading && activeRequest) {
      return activeRequest;
    }

    set({ loading: true, error: '' });

    activeRequest = (async () => {
      try {
        const response = mode === 'overview'
          ? await fetchUsageOverview(range, start, end)
          : await fetchUsage();
        set({
          usage: response.usage,
          loading: false,
          error: '',
          lastRefreshedAt: Date.now(),
          lastMode: mode,
          lastQueryKey: queryKey,
        });
      } catch (error) {
        const message = error instanceof ApiError && error.status === 401
          ? 'AUTH_REQUIRED'
          : error instanceof Error
            ? error.message
            : `Failed to load usage ${mode}`
        set({
          loading: false,
          error: message
        });
        throw error;
      } finally {
        activeRequest = null;
      }
    })();

    return activeRequest;
  },
  clearUsageStats: () => set({ usage: null, error: '', loading: false, lastRefreshedAt: null, lastMode: null, lastQueryKey: null })
}));
