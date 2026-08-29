import { useCallback, useEffect, useRef, useState } from 'react';
import {
  exitRanking,
  fetchRankingLeaderboard,
  fetchRankingMetadata,
  fetchRankingStatus,
  joinRanking,
  pauseRanking,
  RankingApiError,
  resumeRanking,
  syncRanking,
} from '../api';
import type {
  RankingLeaderboardResponse,
  RankingMetadataResponse,
  RankingMetric,
  RankingPeriod,
  RankingProfileRequest,
  RankingStatusResponse,
} from '../types';

export const RANKING_POLL_INTERVAL_MS = 60_000;
export const RANKING_MIN_POLL_INTERVAL_MS = 30_000;

export const resolveRankingPollIntervalMS = (seconds?: number): number => {
  if (!Number.isFinite(seconds) || seconds === undefined) return RANKING_POLL_INTERVAL_MS;
  return Math.max(RANKING_MIN_POLL_INTERVAL_MS, Math.trunc(seconds * 1000));
};

export interface RankingDataAPI {
  status: (signal?: AbortSignal) => Promise<RankingStatusResponse>;
  metadata: (signal?: AbortSignal) => Promise<RankingMetadataResponse>;
  leaderboard: (
    period: RankingPeriod,
    metric: RankingMetric,
    signal?: AbortSignal,
  ) => Promise<RankingLeaderboardResponse>;
  join: (profile: RankingProfileRequest, signal?: AbortSignal) => Promise<RankingStatusResponse>;
  sync: (signal?: AbortSignal) => Promise<RankingStatusResponse>;
  pause: (signal?: AbortSignal) => Promise<RankingStatusResponse>;
  resume: (signal?: AbortSignal) => Promise<RankingStatusResponse>;
  exit: (signal?: AbortSignal) => Promise<RankingStatusResponse>;
}

const defaultAPI: RankingDataAPI = {
  status: fetchRankingStatus,
  metadata: fetchRankingMetadata,
  leaderboard: fetchRankingLeaderboard,
  join: joinRanking,
  sync: syncRanking,
  pause: pauseRanking,
  resume: resumeRanking,
  exit: exitRanking,
};

type RankingAction = 'join' | 'sync' | 'pause' | 'resume' | 'exit';

const isAbortError = (error: unknown) => error instanceof DOMException && error.name === 'AbortError';
const leaderboardKey = (period: RankingPeriod, metric: RankingMetric) => `${period}:${metric}`;
const isDeletedParticipantError = (error: unknown) => error instanceof RankingApiError
  && (error.status === 410 || error.code === 'ranking_participant_deleted');
const applyMetadataFreshness = (
  board: RankingLeaderboardResponse,
  metadata: RankingMetadataResponse | null,
): RankingLeaderboardResponse => {
  const expectedPeriodKey = metadata?.periods.find((item) => item.period === board.period)?.period_key;
  if (!expectedPeriodKey || board.period_key === expectedPeriodKey) return board;
  return { ...board, stale: true };
};

export interface UseRankingDataOptions {
  enabled: boolean;
  onAuthRequired?: () => void;
  onBackgroundRefreshError?: (error: unknown) => void;
  api?: RankingDataAPI;
}

export function useRankingData({
  enabled,
  onAuthRequired,
  onBackgroundRefreshError,
  api = defaultAPI,
}: UseRankingDataOptions) {
  const [period, setPeriod] = useState<RankingPeriod>('today');
  const [metric, setMetric] = useState<RankingMetric>('overall');
  const [status, setStatus] = useState<RankingStatusResponse | null>(null);
  const [metadata, setMetadata] = useState<RankingMetadataResponse | null>(null);
  const [leaderboard, setLeaderboard] = useState<RankingLeaderboardResponse | null>(null);
  const [statusLoading, setStatusLoading] = useState(false);
  const [metadataLoading, setMetadataLoading] = useState(false);
  const [leaderboardLoading, setLeaderboardLoading] = useState(false);
  const [statusError, setStatusError] = useState<unknown>(null);
  const [metadataError, setMetadataError] = useState<unknown>(null);
  const [leaderboardError, setLeaderboardError] = useState<unknown>(null);
  const [action, setAction] = useState<RankingAction | null>(null);
  const [actionError, setActionError] = useState<unknown>(null);
  const enabledRef = useRef(enabled);
  const statusControllerRef = useRef<AbortController | null>(null);
  const metadataControllerRef = useRef<AbortController | null>(null);
  const leaderboardControllerRef = useRef<AbortController | null>(null);
  const actionControllerRef = useRef<AbortController | null>(null);
  const leaderboardGenerationRef = useRef(0);
  const leaderboardCacheRef = useRef(new Map<string, RankingLeaderboardResponse>());
  const metadataRef = useRef<RankingMetadataResponse | null>(null);
  const lastStatusErrorRef = useRef<unknown>(null);
  const lastMetadataErrorRef = useRef<unknown>(null);
  const lastLeaderboardErrorRef = useRef<unknown>(null);
  const refreshFailureKeyRef = useRef<string | null>(null);
  enabledRef.current = enabled;

  const notifyAuthentication = useCallback((error: unknown) => {
    if (error instanceof RankingApiError && error.status === 401) {
      onAuthRequired?.();
    }
  }, [onAuthRequired]);

  const invalidateStatusLoad = useCallback(() => {
    const controller = statusControllerRef.current;
    statusControllerRef.current = null;
    controller?.abort();
    setStatusLoading(false);
  }, []);

  const loadStatus = useCallback(async () => {
    statusControllerRef.current?.abort();
    const controller = new AbortController();
    statusControllerRef.current = controller;
    setStatusLoading(true);
    setStatusError(null);
    lastStatusErrorRef.current = null;
    try {
      const nextStatus = await api.status(controller.signal);
      if (statusControllerRef.current === controller) setStatus(nextStatus);
      return nextStatus;
    } catch (error) {
      if (!isAbortError(error) && statusControllerRef.current === controller) {
        lastStatusErrorRef.current = error;
        notifyAuthentication(error);
        setStatusError(error);
      }
      return null;
    } finally {
      if (statusControllerRef.current === controller) {
        statusControllerRef.current = null;
        setStatusLoading(false);
      }
    }
  }, [api, notifyAuthentication]);

  const loadMetadata = useCallback(async () => {
    metadataControllerRef.current?.abort();
    const controller = new AbortController();
    metadataControllerRef.current = controller;
    setMetadataLoading(true);
    setMetadataError(null);
    lastMetadataErrorRef.current = null;
    try {
      const nextMetadata = await api.metadata(controller.signal);
      if (metadataControllerRef.current === controller) {
        metadataRef.current = nextMetadata;
        setMetadata(nextMetadata);
      }
      return nextMetadata;
    } catch (error) {
      if (!isAbortError(error) && metadataControllerRef.current === controller) {
        lastMetadataErrorRef.current = error;
        notifyAuthentication(error);
        setMetadataError(error);
      }
      return null;
    } finally {
      if (metadataControllerRef.current === controller) {
        metadataControllerRef.current = null;
        setMetadataLoading(false);
      }
    }
  }, [api, notifyAuthentication]);

  const loadLeaderboard = useCallback(async (
    nextPeriod: RankingPeriod,
    nextMetric: RankingMetric,
    {
      silent = false,
      manageRefreshEpisode = true,
    }: { silent?: boolean; manageRefreshEpisode?: boolean } = {},
  ) => {
    leaderboardControllerRef.current?.abort();
    const controller = new AbortController();
    leaderboardControllerRef.current = controller;
    const generation = ++leaderboardGenerationRef.current;
    const key = leaderboardKey(nextPeriod, nextMetric);
    const cachedBoard = leaderboardCacheRef.current.get(key) ?? null;
    lastLeaderboardErrorRef.current = null;
    if (!silent) {
      setLeaderboardLoading(true);
      setLeaderboardError(null);
      // 目标缓存缺失时保留上一份响应；页面会按 period/metric 过滤内容，但标题栏无需闪动。
      setLeaderboard((current) => cachedBoard ?? current);
    }
    try {
      const nextLeaderboard = await api.leaderboard(nextPeriod, nextMetric, controller.signal);
      if (leaderboardGenerationRef.current === generation) {
        const normalizedLeaderboard = applyMetadataFreshness(nextLeaderboard, metadataRef.current);
        leaderboardCacheRef.current.set(key, normalizedLeaderboard);
        if (manageRefreshEpisode && refreshFailureKeyRef.current === key) {
          refreshFailureKeyRef.current = null;
        }
        setLeaderboardError(null);
        setLeaderboard(normalizedLeaderboard);
        return normalizedLeaderboard;
      }
      return nextLeaderboard;
    } catch (error) {
      if (!isAbortError(error) && leaderboardGenerationRef.current === generation) {
        lastLeaderboardErrorRef.current = error;
        notifyAuthentication(error);
        const lastSuccessfulBoard = leaderboardCacheRef.current.get(key) ?? null;
        if (lastSuccessfulBoard) {
          setLeaderboard({ ...lastSuccessfulBoard, stale: true });
          setLeaderboardError(null);
          if (manageRefreshEpisode && refreshFailureKeyRef.current !== key) {
            refreshFailureKeyRef.current = key;
            onBackgroundRefreshError?.(error);
          }
        } else {
          setLeaderboard(null);
          setLeaderboardError(error);
        }
      }
      return null;
    } finally {
      if (leaderboardControllerRef.current === controller) leaderboardControllerRef.current = null;
      // silent 请求也可能接管一个尚未完成的前台请求，当前代完成时必须统一结束 Loading。
      if (leaderboardGenerationRef.current === generation) setLeaderboardLoading(false);
    }
  }, [api, notifyAuthentication, onBackgroundRefreshError]);

  const refreshLeaderboard = useCallback(
    () => loadLeaderboard(period, metric),
    [loadLeaderboard, metric, period],
  );

  const refreshRanking = useCallback(
    async () => {
      const key = leaderboardKey(period, metric);
      const [, nextMetadata] = await Promise.all([loadStatus(), loadMetadata()]);
      if (!enabledRef.current) return [nextMetadata, null] as const;
      const effectiveMetadata = nextMetadata ?? metadataRef.current;
      const periodOffline = effectiveMetadata?.periods
        .find((item) => item.period === period)?.online === false;
      const nextLeaderboard = periodOffline
        ? null
        : await loadLeaderboard(period, metric, { silent: true, manageRefreshEpisode: false });
      const refreshError = lastStatusErrorRef.current
        ?? lastMetadataErrorRef.current
        ?? (periodOffline ? null : (nextLeaderboard ? null : lastLeaderboardErrorRef.current));
      if (refreshError) {
        if (refreshFailureKeyRef.current !== key) onBackgroundRefreshError?.(refreshError);
        refreshFailureKeyRef.current = key;
      } else if (refreshFailureKeyRef.current === key) {
        refreshFailureKeyRef.current = null;
      }
      return [nextMetadata, nextLeaderboard] as const;
    },
    [loadLeaderboard, loadMetadata, loadStatus, metric, onBackgroundRefreshError, period],
  );
  const selectedPeriodOffline = metadata?.periods
    .find((item) => item.period === period)?.online === false;
  const pollIntervalMS = resolveRankingPollIntervalMS(metadata?.refresh_interval_seconds);

  useEffect(() => {
    if (!enabled) return;
    void loadStatus();
    void loadMetadata();
    return () => {
      statusControllerRef.current?.abort();
      metadataControllerRef.current?.abort();
      statusControllerRef.current = null;
      metadataControllerRef.current = null;
    };
  }, [enabled, loadMetadata, loadStatus]);

  useEffect(() => {
    if (!metadata) return;
    setLeaderboard((current) => {
      if (!current) return current;
      const normalized = applyMetadataFreshness(current, metadata);
      if (normalized === current) return current;
      leaderboardCacheRef.current.set(leaderboardKey(current.period, current.metric), normalized);
      return normalized;
    });
  }, [metadata]);

  useEffect(() => {
    if (!enabled) return;
    if (selectedPeriodOffline) {
      leaderboardControllerRef.current?.abort();
      setLeaderboard(leaderboardCacheRef.current.get(leaderboardKey(period, metric)) ?? null);
      setLeaderboardError(null);
      setLeaderboardLoading(false);
      return;
    }
    void loadLeaderboard(period, metric);
    return () => leaderboardControllerRef.current?.abort();
  }, [enabled, loadLeaderboard, metric, period, selectedPeriodOffline]);

  useEffect(() => {
    if (!enabled) return;
    // 中心统一下发读取频率；周期关闭时仍轮询 metadata，以便管理员重新开启后页面自动恢复。
    const interval = window.setInterval(() => {
      if (document.visibilityState === 'hidden') return;
      void (async () => {
        const [, nextMetadata] = await Promise.all([loadStatus(), loadMetadata()]);
        if (!enabledRef.current) return;
        const effectiveMetadata = nextMetadata ?? metadataRef.current;
        const periodOffline = effectiveMetadata?.periods
          .find((item) => item.period === period)?.online === false;
        // offline -> online 交给 selection effect 首次加载，避免同一轮重复请求；稳定在线时继续静默刷新。
        if (!periodOffline && !selectedPeriodOffline) {
          await loadLeaderboard(period, metric, { silent: true });
        }
      })();
    }, pollIntervalMS);
    return () => window.clearInterval(interval);
  }, [enabled, loadLeaderboard, loadMetadata, loadStatus, metric, period, pollIntervalMS, selectedPeriodOffline]);

  useEffect(() => () => {
    statusControllerRef.current?.abort();
    metadataControllerRef.current?.abort();
    leaderboardControllerRef.current?.abort();
    actionControllerRef.current?.abort();
  }, []);

  const runAction = useCallback(async (
    nextAction: RankingAction,
    operation: (signal: AbortSignal) => Promise<RankingStatusResponse>,
  ) => {
    if (action) return null;
    // 操作结果必须成为最新状态；先废弃旧查询，并在成功落盘前再次废弃操作期间启动的查询。
    invalidateStatusLoad();
    const controller = new AbortController();
    actionControllerRef.current = controller;
    setAction(nextAction);
    setActionError(null);
    try {
      const nextStatus = await operation(controller.signal);
      if (actionControllerRef.current !== controller) return null;
      invalidateStatusLoad();
      setStatus(nextStatus);
      if (enabledRef.current && nextAction !== 'pause' && nextAction !== 'resume') {
        await loadLeaderboard(period, metric, { silent: true });
      }
      return nextStatus;
    } catch (error) {
      if (!isAbortError(error) && actionControllerRef.current === controller) {
        notifyAuthentication(error);
        setActionError(error);
        // Join 失败可能已经固化本地身份；任意操作收到 410 时也要立即展示本地墓碑。
        if (nextAction === 'join' || isDeletedParticipantError(error)) await loadStatus();
      }
      return null;
    } finally {
      if (actionControllerRef.current === controller) {
        actionControllerRef.current = null;
        setAction(null);
      }
    }
  }, [action, invalidateStatusLoad, loadLeaderboard, loadStatus, metric, notifyAuthentication, period]);

  const join = useCallback(
    (profile: RankingProfileRequest) => runAction('join', (signal) => api.join(profile, signal)),
    [api, runAction],
  );
  const sync = useCallback(
    () => runAction('sync', (signal) => api.sync(signal)),
    [api, runAction],
  );
  const pause = useCallback(
    () => runAction('pause', (signal) => api.pause(signal)),
    [api, runAction],
  );
  const resume = useCallback(
    () => runAction('resume', (signal) => api.resume(signal)),
    [api, runAction],
  );
  const exit = useCallback(
    () => runAction('exit', (signal) => api.exit(signal)),
    [api, runAction],
  );
  const clearActionError = useCallback(() => setActionError(null), []);

  return {
    period,
    setPeriod,
    metric,
    setMetric,
    status,
    metadata,
    leaderboard,
    statusLoading,
    metadataLoading,
    leaderboardLoading,
    statusError,
    metadataError,
    leaderboardError,
    action,
    actionError,
    clearActionError,
    refreshStatus: loadStatus,
    refreshMetadata: loadMetadata,
    refreshLeaderboard,
    refreshRanking,
    join,
    sync,
    pause,
    resume,
    exit,
  };
}
