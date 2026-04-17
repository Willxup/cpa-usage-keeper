import { create } from 'zustand';
import { ApiError, fetchUsage } from '@/lib/api';
import type { UsageSnapshot } from '@/lib/types';

export const USAGE_STATS_STALE_TIME_MS = 60_000;

interface LoadUsageStatsOptions {
  force?: boolean;
  staleTimeMs?: number;
}

interface UsageStatsState {
  usage: UsageSnapshot | null;
  loading: boolean;
  error: string;
  lastRefreshedAt: number | null;
  loadUsageStats: (options?: LoadUsageStatsOptions) => Promise<void>;
  clearUsageStats: () => void;
}

let activeRequest: Promise<void> | null = null;

export const useUsageStatsStore = create<UsageStatsState>((set, get) => ({
  usage: null,
  loading: false,
  error: '',
  lastRefreshedAt: null,
  loadUsageStats: async (options = {}) => {
    const { force = false, staleTimeMs = USAGE_STATS_STALE_TIME_MS } = options;
    const { lastRefreshedAt, loading, usage } = get();
    const now = Date.now();

    if (!force && usage && lastRefreshedAt && now - lastRefreshedAt < staleTimeMs) {
      return;
    }

    if (loading && activeRequest) {
      return activeRequest;
    }

    set({ loading: true, error: '' });

    activeRequest = (async () => {
      try {
        const response = await fetchUsage();
        set({
          usage: response.usage,
          loading: false,
          error: '',
          lastRefreshedAt: Date.now()
        });
      } catch (error) {
        const message = error instanceof ApiError && error.status === 401
          ? 'AUTH_REQUIRED'
          : error instanceof Error
            ? error.message
            : 'Failed to load usage'
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
  clearUsageStats: () => set({ usage: null, error: '', loading: false, lastRefreshedAt: null })
}));
