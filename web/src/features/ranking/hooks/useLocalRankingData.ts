import { useCallback, useEffect, useRef, useState } from 'react';
import { fetchLocalRankingLeaderboard, RankingApiError, updateLocalRankingProfile } from '../api';
import type {
  LocalRankingProfileRequest,
  LocalRankingProfileResponse,
  RankingLeaderboardResponse,
  RankingMetric,
  RankingPeriod,
} from '../types';

export const LOCAL_RANKING_POLL_INTERVAL_MS = 60_000;

export interface LocalRankingDataAPI {
  leaderboard: (
    period: RankingPeriod,
    metric: RankingMetric,
    signal?: AbortSignal,
  ) => Promise<RankingLeaderboardResponse>;
  updateProfile?: (
    participantID: string,
    profile: LocalRankingProfileRequest,
    signal?: AbortSignal,
  ) => Promise<LocalRankingProfileResponse>;
}

const defaultAPI: LocalRankingDataAPI = {
  leaderboard: fetchLocalRankingLeaderboard,
  updateProfile: updateLocalRankingProfile,
};
const isAbortError = (error: unknown) => error instanceof DOMException && error.name === 'AbortError';
const leaderboardKey = (period: RankingPeriod, metric: RankingMetric) => `${period}:${metric}`;
type LocalRankingProfilePatch = Partial<Pick<LocalRankingProfileResponse, 'key_alias' | 'display_name' | 'avatar_id'>>;
const applyLocalProfile = (
  board: RankingLeaderboardResponse,
  participantID: string,
  profile: LocalRankingProfilePatch,
): RankingLeaderboardResponse => ({
  ...board,
  entries: board.entries.map((entry) => entry.participant_id === participantID
    ? {
      ...entry,
      ...profile,
    }
    : entry),
});

export interface UseLocalRankingDataOptions {
  enabled: boolean;
  period: RankingPeriod;
  metric: RankingMetric;
  onAuthRequired?: () => void;
  onBackgroundRefreshError?: (error: unknown) => void;
  api?: LocalRankingDataAPI;
}

// useLocalRankingData 只维护本地只读榜单，不加载 Community metadata、状态或参与动作。
export function useLocalRankingData({
  enabled,
  period,
  metric,
  onAuthRequired,
  onBackgroundRefreshError,
  api = defaultAPI,
}: UseLocalRankingDataOptions) {
  const [leaderboard, setLeaderboard] = useState<RankingLeaderboardResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const enabledRef = useRef(enabled);
  const controllerRef = useRef<AbortController | null>(null);
  const generationRef = useRef(0);
  const cacheRef = useRef(new Map<string, RankingLeaderboardResponse>());
  enabledRef.current = enabled;

  const loadLeaderboard = useCallback(async (
    nextPeriod: RankingPeriod,
    nextMetric: RankingMetric,
    { silent = false }: { silent?: boolean } = {},
  ) => {
    controllerRef.current?.abort();
    const controller = new AbortController();
    controllerRef.current = controller;
    const generation = ++generationRef.current;
    const key = leaderboardKey(nextPeriod, nextMetric);
    const cachedBoard = cacheRef.current.get(key) ?? null;
    if (!silent) {
      setLoading(true);
      setError(null);
      // 目标缓存缺失时保留上一份响应；页面会按 period/metric 过滤内容，但标题栏无需闪动。
      setLeaderboard((current) => cachedBoard ?? current);
    }
    try {
      const board = await api.leaderboard(nextPeriod, nextMetric, controller.signal);
      if (generationRef.current === generation) {
        cacheRef.current.set(key, board);
        setLeaderboard(board);
        setError(null);
      }
      return board;
    } catch (loadError) {
      if (!isAbortError(loadError) && generationRef.current === generation) {
        if (loadError instanceof RankingApiError && loadError.status === 401) onAuthRequired?.();
        if (cachedBoard) {
          setLeaderboard({ ...cachedBoard, stale: true });
          setError(null);
          onBackgroundRefreshError?.(loadError);
        } else {
          setLeaderboard(null);
          setError(loadError);
        }
      }
      return null;
    } finally {
      if (controllerRef.current === controller) controllerRef.current = null;
      if (generationRef.current === generation) setLoading(false);
    }
  }, [api, onAuthRequired, onBackgroundRefreshError]);

  const refreshLeaderboard = useCallback(
    () => loadLeaderboard(period, metric),
    [loadLeaderboard, metric, period],
  );

  const patchProfileCache = useCallback((participantID: string, profile: LocalRankingProfilePatch) => {
    // 设置页和排行弹窗共用 Key 资料；同步废弃旧读取并更新所有已加载榜单投影。
    controllerRef.current?.abort();
    controllerRef.current = null;
    generationRef.current += 1;
    setLoading(false);
    for (const [key, cachedBoard] of cacheRef.current) {
      cacheRef.current.set(key, applyLocalProfile(cachedBoard, participantID, profile));
    }
    setLeaderboard((current) => current ? applyLocalProfile(current, participantID, profile) : current);
  }, []);

  const updateProfile = useCallback(async (
    participantID: string,
    profile: LocalRankingProfileRequest,
    signal?: AbortSignal,
  ) => {
    const update = api.updateProfile ?? defaultAPI.updateProfile;
    if (!update) throw new Error('local ranking profile API is not configured');
    try {
      const updated = await update(participantID, profile, signal);
      patchProfileCache(updated.participant_id, updated);
      return updated;
    } catch (updateError) {
      if (updateError instanceof RankingApiError && updateError.status === 401) onAuthRequired?.();
      throw updateError;
    }
  }, [api, onAuthRequired, patchProfileCache]);

  useEffect(() => {
    if (!enabled) {
      controllerRef.current?.abort();
      controllerRef.current = null;
      setLoading(false);
      return;
    }
    void loadLeaderboard(period, metric);
    return () => controllerRef.current?.abort();
  }, [enabled, loadLeaderboard, metric, period]);

  useEffect(() => {
    if (!enabled) return;
    const interval = window.setInterval(() => {
      if (document.visibilityState === 'hidden') return;
      void loadLeaderboard(period, metric, { silent: true });
    }, LOCAL_RANKING_POLL_INTERVAL_MS);
    return () => window.clearInterval(interval);
  }, [enabled, loadLeaderboard, metric, period]);

  useEffect(() => () => controllerRef.current?.abort(), []);

  return {
    leaderboard,
    leaderboardLoading: loading,
    leaderboardError: error,
    refreshLeaderboard,
    updateProfile,
    patchProfileCache,
  };
}
