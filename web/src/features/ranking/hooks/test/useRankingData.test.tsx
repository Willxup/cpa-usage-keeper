// @vitest-environment happy-dom

import { act, useEffect } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { RankingApiError } from '../../api';
import { RANKING_PREFERENCES_STORAGE_KEY } from '../../preferences';
import { useRankingData, type RankingDataAPI } from '../useRankingData';
import type {
  RankingLeaderboardResponse,
  RankingMetadataResponse,
  RankingMetric,
  RankingPeriod,
  RankingStatusResponse,
} from '../../types';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const metadata: RankingMetadataResponse = {
  server_time: '2026-07-24T04:00:00Z',
  generated_at: '2026-07-24T04:00:00Z',
  stale: false,
  protocol_version: 1,
  metrics_version: 1,
  period_timezone: 'Asia/Shanghai',
  avatar_catalog_version: 1,
  avatar_count: 66,
  read_marker_version: 1,
  refresh_interval_seconds: 60,
  suggested_sync_interval_seconds: 1800,
  periods: [
    { period: 'today', period_key: '2026-07-24', online: true },
    { period: 'yesterday', period_key: '2026-07-23', online: true },
    { period: 'current_month', period_key: '2026-07', online: true },
    { period: 'previous_month', period_key: '2026-06', online: true },
  ],
  metrics: ['overall', 'total_tokens', 'request_count', 'cache_read_rate', 'ttft_average', 'latency_average', 'peak_tpm', 'peak_rpm'],
  overall_weights: {},
};

const board = (period: RankingPeriod, metric: RankingMetric, value = 9_325): RankingLeaderboardResponse => ({
  period,
  period_key: period.includes('month') ? '2026-07' : '2026-07-24',
  metric,
  generated_at: '2026-07-24T04:05:00Z',
  stale: false,
  entries: [{
    rank: 1,
    participant_id: 'p_hidden',
    display_name: 'Keeper_01',
    avatar_id: 7,
    value,
  }],
});

const createAPI = (overrides: Partial<RankingDataAPI> = {}): RankingDataAPI => ({
  status: async () => ({ status: 'disabled' }),
  metadata: async () => metadata,
  leaderboard: async (period, metric) => board(period, metric),
  join: async (profile) => ({ status: 'active', ...profile }),
  sync: async () => ({ status: 'active', display_name: 'Keeper_01', avatar_id: 7 }),
  pause: async () => ({ status: 'paused', display_name: 'Keeper_01', avatar_id: 7 }),
  resume: async () => ({ status: 'active', display_name: 'Keeper_01', avatar_id: 7 }),
  exit: async () => ({ status: 'deleted', display_name: 'Keeper_01', avatar_id: 7 }),
  ...overrides,
});

const deferred = <T,>() => {
  let resolve: (value: T) => void = () => undefined;
  let reject: (reason: unknown) => void = () => undefined;
  const promise = new Promise<T>((nextResolve, nextReject) => {
    resolve = nextResolve;
    reject = nextReject;
  });
  return { promise, resolve, reject };
};

let latest: ReturnType<typeof useRankingData> | null = null;

function Harness({ enabled, api, onAuthRequired, onBackgroundRefreshError }: {
  enabled: boolean;
  api: RankingDataAPI;
  onAuthRequired?: () => void;
  onBackgroundRefreshError?: (error: unknown) => void;
}) {
  const result = useRankingData({ enabled, api, onAuthRequired, onBackgroundRefreshError });
  useEffect(() => {
    latest = result;
  }, [result]);
  return null;
}

describe('useRankingData', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    // period/metric 现在会持久化，用例之间必须清掉，否则上一条的选择会泄漏成下一条的初始值。
    try {
      window.localStorage.removeItem(RANKING_PREFERENCES_STORAGE_KEY);
    } catch {
      // 环境没有可用的 localStorage 时 hook 也不会写入，无需清理。
    }
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    latest = null;
    vi.useRealTimers();
  });

  const renderHook = async (
    enabled: boolean,
    api: RankingDataAPI,
    onAuthRequired?: () => void,
    onBackgroundRefreshError?: (error: unknown) => void,
  ) => {
    await act(async () => {
      root.render(
        <Harness
          enabled={enabled}
          api={api}
          onAuthRequired={onAuthRequired}
          onBackgroundRefreshError={onBackgroundRefreshError}
        />,
      );
    });
  };

  it('loads status, metadata, and the current board only while Ranking is enabled', async () => {
    let calls = 0;
    const api = createAPI({
      status: async () => { calls += 1; return { status: 'disabled' }; },
      metadata: async () => { calls += 1; return metadata; },
      leaderboard: async (period, metric) => { calls += 1; return board(period, metric); },
    });

    await renderHook(false, api);
    expect(calls).toBe(0);

    await renderHook(true, api);
    expect(calls).toBe(3);
    expect(latest?.status?.status).toBe('disabled');
    expect(latest?.metadata?.avatar_count).toBe(66);
    expect(latest?.leaderboard?.period).toBe('today');
    expect(latest?.leaderboard?.metric).toBe('overall');
  });

  it('reloads only the leaderboard when its period or metric changes', async () => {
    const selections: string[] = [];
    const api = createAPI({
      leaderboard: async (period, metric) => {
        selections.push(`${period}:${metric}`);
        return board(period, metric);
      },
    });
    await renderHook(true, api);

    await act(async () => latest?.setPeriod('yesterday'));
    await act(async () => latest?.setMetric('total_tokens'));

    expect(selections).toEqual(['today:overall', 'yesterday:overall', 'yesterday:total_tokens']);
    expect(latest?.leaderboard).toMatchObject({ period: 'yesterday', metric: 'total_tokens' });
  });

  it('clears foreground loading when an immediate manual refresh takes over and succeeds', async () => {
    const firstBoard = deferred<RankingLeaderboardResponse>();
    let leaderboardCalls = 0;
    const api = createAPI({
      leaderboard: async (period, metric) => {
        leaderboardCalls += 1;
        if (leaderboardCalls === 1) return firstBoard.promise;
        return board(period, metric, 9_400);
      },
    });
    await renderHook(true, api);
    expect(latest?.leaderboardLoading).toBe(true);

    await act(async () => latest?.refreshRanking());

    expect(latest?.leaderboardLoading).toBe(false);
    expect(latest?.leaderboard?.entries[0]?.value).toBe(9_400);
    firstBoard.resolve(board('today', 'overall'));
  });

  it('ends foreground loading with an error when an immediate manual refresh fails', async () => {
    const firstBoard = deferred<RankingLeaderboardResponse>();
    let leaderboardCalls = 0;
    const api = createAPI({
      leaderboard: async () => {
        leaderboardCalls += 1;
        if (leaderboardCalls === 1) return firstBoard.promise;
        throw new RankingApiError('ranking_center_unavailable', 503);
      },
    });
    await renderHook(true, api);
    expect(latest?.leaderboardLoading).toBe(true);

    await act(async () => latest?.refreshRanking());

    expect(latest?.leaderboardLoading).toBe(false);
    expect(latest?.leaderboardError).toBeInstanceOf(RankingApiError);
    firstBoard.resolve(board('today', 'overall'));
  });

  it('clears selection loading when refresh immediately replaces a period request', async () => {
    const pendingYesterday = deferred<RankingLeaderboardResponse>();
    let yesterdayCalls = 0;
    const api = createAPI({
      leaderboard: async (period, metric) => {
        if (period !== 'yesterday') return board(period, metric);
        yesterdayCalls += 1;
        if (yesterdayCalls === 1) return pendingYesterday.promise;
        return board(period, metric, 8_800);
      },
    });
    await renderHook(true, api);

    await act(async () => latest?.setPeriod('yesterday'));
    expect(latest?.leaderboardLoading).toBe(true);
    expect(latest?.leaderboard).toMatchObject({ period: 'today', metric: 'overall' });
    await act(async () => latest?.refreshRanking());

    expect(latest?.leaderboardLoading).toBe(false);
    expect(latest?.leaderboard).toMatchObject({ period: 'yesterday', metric: 'overall' });
    pendingYesterday.resolve(board('yesterday', 'overall'));
  });

  it('follows the center leaderboard refresh interval', async () => {
    vi.useFakeTimers();
    let leaderboardCalls = 0;
    let metadataCalls = 0;
    const fasterMetadata = { ...metadata, refresh_interval_seconds: 45 };
    const api = createAPI({
      metadata: async () => {
        metadataCalls += 1;
        return fasterMetadata;
      },
      leaderboard: async (period, metric) => {
        leaderboardCalls += 1;
        return board(period, metric, 9_325 + leaderboardCalls);
      },
    });
    await renderHook(true, api);
    expect(leaderboardCalls).toBe(1);

    await act(async () => vi.advanceTimersByTimeAsync(44_999));
    expect(leaderboardCalls).toBe(1);
    await act(async () => vi.advanceTimersByTimeAsync(1));
    expect(leaderboardCalls).toBe(2);
    expect(metadataCalls).toBe(2);
  });

  it('refreshes the local participation state on the background poll', async () => {
    vi.useFakeTimers();
    let statusCalls = 0;
    const api = createAPI({
      status: async () => {
        statusCalls += 1;
        return statusCalls === 1
          ? { status: 'active', display_name: 'Keeper_01', avatar_id: 7 }
          : { status: 'deleted', display_name: 'Keeper_01', avatar_id: 7 };
      },
    });
    await renderHook(true, api);
    expect(latest?.status?.status).toBe('active');

    await act(async () => vi.advanceTimersByTimeAsync(60_000));

    expect(statusCalls).toBe(2);
    expect(latest?.status?.status).toBe('deleted');
  });

  it('continues polling metadata while the selected period is offline and reloads when it reopens', async () => {
    vi.useFakeTimers();
    let metadataCalls = 0;
    let leaderboardCalls = 0;
    const offlineMetadata: RankingMetadataResponse = {
      ...metadata,
      periods: metadata.periods.map((item) => item.period === 'today'
        ? { ...item, online: false }
        : item),
    };
    const api = createAPI({
      metadata: async () => {
        metadataCalls += 1;
        return metadataCalls === 1 ? offlineMetadata : metadata;
      },
      leaderboard: async (period, metric) => {
        leaderboardCalls += 1;
        return board(period, metric);
      },
    });
    await renderHook(true, api);
    expect(latest?.metadata?.periods.find((item) => item.period === 'today')?.online).toBe(false);
    const callsBeforeReopen = leaderboardCalls;

    await act(async () => vi.advanceTimersByTimeAsync(60_000));

    expect(metadataCalls).toBe(2);
    expect(latest?.metadata?.periods.find((item) => item.period === 'today')?.online).toBe(true);
    expect(leaderboardCalls).toBe(callsBeforeReopen + 1);
  });

  it('never polls the leaderboard faster than the thirty-second safety floor', async () => {
    vi.useFakeTimers();
    let leaderboardCalls = 0;
    const api = createAPI({
      metadata: async () => ({ ...metadata, refresh_interval_seconds: 5 }),
      leaderboard: async (period, metric) => {
        leaderboardCalls += 1;
        return board(period, metric);
      },
    });
    await renderHook(true, api);

    await act(async () => vi.advanceTimersByTimeAsync(29_999));
    expect(leaderboardCalls).toBe(1);
    await act(async () => vi.advanceTimersByTimeAsync(1));
    expect(leaderboardCalls).toBe(2);
  });

  it('refreshes registration state once after a failed manual join', async () => {
    let statusCalls = 0;
    const api = createAPI({
      status: async () => {
        statusCalls += 1;
        return statusCalls === 1
          ? { status: 'disabled' }
          : { status: 'joining', display_name: 'Keeper_02', avatar_id: 9 };
      },
      join: async () => {
        throw new RankingApiError('ranking_center_unavailable', 503);
      },
    });
    await renderHook(true, api);

    await act(async () => latest?.join({ display_name: 'Keeper_02', avatar_id: 9 }));

    expect(statusCalls).toBe(2);
    expect(latest?.status).toMatchObject({ status: 'joining', display_name: 'Keeper_02', avatar_id: 9 });
    expect(latest?.actionError).toBeInstanceOf(RankingApiError);
  });

  it('keeps the last successful board and reports one notice per background failure episode', async () => {
    vi.useFakeTimers();
    let shouldFail = false;
    const onBackgroundRefreshError = vi.fn();
    const api = createAPI({
      leaderboard: async (period, metric) => {
        if (shouldFail) throw new RankingApiError('ranking_center_unavailable', 503);
        return board(period, metric);
      },
    });
    await renderHook(true, api, undefined, onBackgroundRefreshError);
    shouldFail = true;

    await act(async () => vi.advanceTimersByTimeAsync(60_000));
    expect(latest?.leaderboard?.entries[0]?.display_name).toBe('Keeper_01');
    expect(latest?.leaderboard?.stale).toBe(true);
    expect(latest?.leaderboardError).toBeNull();
    expect(onBackgroundRefreshError).toHaveBeenCalledOnce();

    await act(async () => vi.advanceTimersByTimeAsync(60_000));
    expect(onBackgroundRefreshError).toHaveBeenCalledOnce();

    shouldFail = false;
    await act(async () => vi.advanceTimersByTimeAsync(60_000));
    expect(latest?.leaderboard?.stale).toBe(false);
    shouldFail = true;
    await act(async () => vi.advanceTimersByTimeAsync(60_000));
    expect(onBackgroundRefreshError).toHaveBeenCalledTimes(2);
  });

  it('marks a board stale when its period key no longer matches refreshed metadata', async () => {
    vi.useFakeTimers();
    let metadataCalls = 0;
    const nextDayMetadata: RankingMetadataResponse = {
      ...metadata,
      periods: metadata.periods.map((item) => item.period === 'today'
        ? { ...item, period_key: '2026-07-25' }
        : item),
    };
    const api = createAPI({
      metadata: async () => {
        metadataCalls += 1;
        return metadataCalls === 1 ? metadata : nextDayMetadata;
      },
      leaderboard: async (period, metric) => board(period, metric),
    });
    await renderHook(true, api);

    await act(async () => vi.advanceTimersByTimeAsync(60_000));

    expect(latest?.metadata?.periods.find((item) => item.period === 'today')?.period_key).toBe('2026-07-25');
    expect(latest?.leaderboard?.period_key).toBe('2026-07-24');
    expect(latest?.leaderboard?.stale).toBe(true);
  });

  it('rechecks an already loaded board when newer metadata arrives later', async () => {
    const pendingMetadata = deferred<RankingMetadataResponse>();
    const nextDayMetadata: RankingMetadataResponse = {
      ...metadata,
      periods: metadata.periods.map((item) => item.period === 'today'
        ? { ...item, period_key: '2026-07-25' }
        : item),
    };
    const api = createAPI({
      metadata: async () => pendingMetadata.promise,
      leaderboard: async (period, metric) => board(period, metric),
    });
    await renderHook(true, api);
    expect(latest?.leaderboard?.stale).toBe(false);

    pendingMetadata.resolve(nextDayMetadata);
    await act(async () => pendingMetadata.promise);

    expect(latest?.leaderboard?.period_key).toBe('2026-07-24');
    expect(latest?.leaderboard?.stale).toBe(true);
  });

  it('keeps the blocking error when no leaderboard has ever loaded', async () => {
    vi.useFakeTimers();
    const onBackgroundRefreshError = vi.fn();
    const api = createAPI({
      leaderboard: async () => {
        throw new RankingApiError('ranking_center_unavailable', 503);
      },
    });
    await renderHook(true, api, undefined, onBackgroundRefreshError);
    expect(latest?.leaderboard).toBeNull();
    expect(latest?.leaderboardError).toBeInstanceOf(RankingApiError);

    await act(async () => vi.advanceTimersByTimeAsync(60_000));

    expect(latest?.leaderboardError).toBeInstanceOf(RankingApiError);
    expect(onBackgroundRefreshError).not.toHaveBeenCalled();
  });

  it('refreshes metadata and the selected board together only on manual refresh', async () => {
    let statusCalls = 0;
    let metadataCalls = 0;
    let leaderboardCalls = 0;
    const api = createAPI({
      status: async () => {
        statusCalls += 1;
        return { status: 'disabled' };
      },
      metadata: async () => {
        metadataCalls += 1;
        return metadata;
      },
      leaderboard: async (period, metric) => {
        leaderboardCalls += 1;
        return board(period, metric);
      },
    });
    await renderHook(true, api);
    const refreshRanking = (latest as unknown as { refreshRanking?: () => Promise<unknown> } | null)?.refreshRanking;
    expect(refreshRanking).toBeTypeOf('function');

    await act(async () => refreshRanking?.());

    expect(statusCalls).toBe(2);
    expect(metadataCalls).toBe(2);
    expect(leaderboardCalls).toBe(2);
  });

  it('keeps the matching cached board when revisiting a selection that fails to refresh', async () => {
    const onBackgroundRefreshError = vi.fn();
    let failToday = false;
    const api = createAPI({
      leaderboard: async (period, metric) => {
        if (period === 'today' && failToday) throw new RankingApiError('ranking_center_unavailable', 503);
        return board(period, metric, period === 'today' ? 9_325 : 8_100);
      },
    });
    await renderHook(true, api, undefined, onBackgroundRefreshError);
    await act(async () => latest?.setPeriod('yesterday'));
    failToday = true;

    await act(async () => latest?.setPeriod('today'));

    expect(latest?.leaderboard).toMatchObject({ period: 'today', metric: 'overall' });
    expect(latest?.leaderboard?.entries[0]?.value).toBe(9_325);
    expect(latest?.leaderboardError).toBeNull();
    expect(onBackgroundRefreshError).toHaveBeenCalledOnce();
  });

  it('does not reuse another selection when the current board has never loaded', async () => {
    const api = createAPI({
      leaderboard: async (period, metric) => {
        if (period === 'yesterday') throw new RankingApiError('ranking_center_unavailable', 503);
        return board(period, metric);
      },
    });
    await renderHook(true, api);

    await act(async () => latest?.setPeriod('yesterday'));

    expect(latest?.leaderboard).toBeNull();
    expect(latest?.leaderboardError).toBeInstanceOf(RankingApiError);
  });

  it('reports a manual metadata failure while retaining the last metadata and successful board', async () => {
    const onBackgroundRefreshError = vi.fn();
    let metadataCalls = 0;
    const api = createAPI({
      metadata: async () => {
        metadataCalls += 1;
        if (metadataCalls > 1) throw new RankingApiError('ranking_center_unavailable', 503);
        return metadata;
      },
    });
    await renderHook(true, api, undefined, onBackgroundRefreshError);

    await act(async () => latest?.refreshRanking());
    await act(async () => latest?.refreshRanking());

    expect(latest?.metadata).toBe(metadata);
    expect(latest?.leaderboard?.entries[0]?.display_name).toBe('Keeper_01');
    expect(onBackgroundRefreshError).toHaveBeenCalledOnce();
  });

  it('reports a manual status failure while retaining the last participation state', async () => {
    const onBackgroundRefreshError = vi.fn();
    let statusCalls = 0;
    const api = createAPI({
      status: async () => {
        statusCalls += 1;
        if (statusCalls > 1) throw new RankingApiError('ranking_center_unavailable', 503);
        return { status: 'active', display_name: 'Keeper_01', avatar_id: 7 };
      },
    });
    await renderHook(true, api, undefined, onBackgroundRefreshError);

    await act(async () => latest?.refreshRanking());
    await act(async () => latest?.refreshRanking());

    expect(latest?.status).toMatchObject({ status: 'active', display_name: 'Keeper_01' });
    expect(onBackgroundRefreshError).toHaveBeenCalledOnce();
  });

  it('does not start a follow-up leaderboard request after Ranking is disabled', async () => {
    const pendingStatus = deferred<RankingStatusResponse>();
    const pendingMetadata = deferred<RankingMetadataResponse>();
    let statusCalls = 0;
    let metadataCalls = 0;
    let leaderboardCalls = 0;
    const api = createAPI({
      status: async () => {
        statusCalls += 1;
        return statusCalls === 1 ? { status: 'disabled' } : pendingStatus.promise;
      },
      metadata: async () => {
        metadataCalls += 1;
        return metadataCalls === 1 ? metadata : pendingMetadata.promise;
      },
      leaderboard: async (period, metric) => {
        leaderboardCalls += 1;
        return board(period, metric);
      },
    });
    await renderHook(true, api);

    let refreshPromise: Promise<unknown> = Promise.resolve();
    await act(async () => {
      refreshPromise = latest?.refreshRanking() ?? Promise.resolve();
      await Promise.resolve();
    });
    await renderHook(false, api);
    pendingStatus.resolve({ status: 'disabled' });
    pendingMetadata.resolve(metadata);
    await act(async () => refreshPromise);

    expect(leaderboardCalls).toBe(1);
  });

  it('does not refresh the leaderboard after an action finishes on another tab', async () => {
    const pendingSync = deferred<RankingStatusResponse>();
    let leaderboardCalls = 0;
    const api = createAPI({
      sync: async () => pendingSync.promise,
      leaderboard: async (period, metric) => {
        leaderboardCalls += 1;
        return board(period, metric);
      },
    });
    await renderHook(true, api);

    let syncPromise: Promise<unknown> = Promise.resolve();
    await act(async () => {
      syncPromise = latest?.sync() ?? Promise.resolve();
      await Promise.resolve();
    });
    await renderHook(false, api);
    pendingSync.resolve({ status: 'active', display_name: 'Keeper_01', avatar_id: 7 });
    await act(async () => syncPromise);

    expect(leaderboardCalls).toBe(1);
  });

  it('skips a manual leaderboard request when refreshed metadata marks the period offline', async () => {
    const onBackgroundRefreshError = vi.fn();
    let metadataCalls = 0;
    let leaderboardCalls = 0;
    const offlineMetadata: RankingMetadataResponse = {
      ...metadata,
      periods: metadata.periods.map((item) => item.period === 'today' ? { ...item, online: false } : item),
    };
    const api = createAPI({
      metadata: async () => {
        metadataCalls += 1;
        return metadataCalls === 1 ? metadata : offlineMetadata;
      },
      leaderboard: async (period, metric) => {
        leaderboardCalls += 1;
        if (leaderboardCalls > 1) throw new RankingApiError('ranking_center_unavailable', 503);
        return board(period, metric);
      },
    });
    await renderHook(true, api, undefined, onBackgroundRefreshError);

    await act(async () => latest?.refreshRanking());

    expect(leaderboardCalls).toBe(1);
    expect(latest?.metadata?.periods.find((item) => item.period === 'today')?.online).toBe(false);
    expect(onBackgroundRefreshError).not.toHaveBeenCalled();
  });

  it('keeps leaderboard reading available while reporting a participation error', async () => {
    const onAuthRequired = vi.fn();
    const api = createAPI({
      status: async () => { throw new RankingApiError('auth_required', 401); },
      leaderboard: async (period, metric) => board(period, metric),
    });

    await renderHook(true, api, onAuthRequired);

    expect(onAuthRequired).toHaveBeenCalledOnce();
    expect(latest?.status).toBeNull();
    expect(latest?.leaderboard?.entries[0]?.display_name).toBe('Keeper_01');
  });

  it('updates the local participation state after join, sync, pause, resume, and permanent exit', async () => {
    const statuses: RankingStatusResponse[] = [
      { status: 'active', display_name: 'Keeper_02', avatar_id: 9 },
      { status: 'active', display_name: 'Keeper_02', avatar_id: 9, last_successful_sync_at: '2026-07-24T05:00:00Z' },
      { status: 'paused', display_name: 'Keeper_02', avatar_id: 9, last_successful_sync_at: '2026-07-24T05:00:00Z' },
      { status: 'active', display_name: 'Keeper_02', avatar_id: 9, last_successful_sync_at: '2026-07-24T05:00:00Z' },
      { status: 'deleted', display_name: 'Keeper_02', avatar_id: 9 },
    ];
    const api = createAPI({
      join: async () => statuses[0],
      sync: async () => statuses[1],
      pause: async () => statuses[2],
      resume: async () => statuses[3],
      exit: async () => statuses[4],
    });
    await renderHook(true, api);

    await act(async () => latest?.join({ display_name: 'Keeper_02', avatar_id: 9 }));
    expect(latest?.status?.status).toBe('active');
    await act(async () => latest?.sync());
    expect(latest?.status?.last_successful_sync_at).toBe('2026-07-24T05:00:00Z');
    await act(async () => latest?.pause());
    expect(latest?.status?.status).toBe('paused');
    await act(async () => latest?.resume());
    expect(latest?.status?.status).toBe('active');
    await act(async () => latest?.exit());
    expect(latest?.status?.status).toBe('deleted');
  });

  it('does not let an older status request overwrite a completed participation action', async () => {
    const pendingStatus = deferred<RankingStatusResponse>();
    let statusCalls = 0;
    const api = createAPI({
      status: async () => {
        statusCalls += 1;
        return statusCalls === 1
          ? { status: 'active', display_name: 'Keeper_01', avatar_id: 7 }
          : pendingStatus.promise;
      },
      pause: async () => ({ status: 'paused', display_name: 'Keeper_01', avatar_id: 7 }),
    });
    await renderHook(true, api);

    let refreshPromise: Promise<unknown> = Promise.resolve();
    await act(async () => {
      refreshPromise = latest?.refreshStatus() ?? Promise.resolve();
      await Promise.resolve();
    });
    await act(async () => latest?.pause());
    expect(latest?.status?.status).toBe('paused');

    pendingStatus.resolve({ status: 'active', display_name: 'Keeper_01', avatar_id: 7 });
    await act(async () => refreshPromise);

    expect(latest?.status?.status).toBe('paused');
  });

  it('does not refresh the center leaderboard for local-only pause and resume actions', async () => {
    let leaderboardCalls = 0;
    const api = createAPI({
      leaderboard: async (period, metric) => {
        leaderboardCalls += 1;
        return board(period, metric);
      },
    });
    await renderHook(true, api);
    expect(leaderboardCalls).toBe(1);

    await act(async () => latest?.pause());
    await act(async () => latest?.resume());

    expect(leaderboardCalls).toBe(1);
  });

  it('reloads the local tombstone after any action learns that the center deleted the participant', async () => {
    let statusCalls = 0;
    const api = createAPI({
      status: async () => {
        statusCalls += 1;
        return statusCalls === 1
          ? { status: 'active', display_name: 'Keeper_01', avatar_id: 7 }
          : { status: 'deleted', display_name: 'Keeper_01', avatar_id: 7 };
      },
      sync: async () => {
        throw new RankingApiError('ranking_participant_deleted', 410);
      },
    });
    await renderHook(true, api);

    await act(async () => latest?.sync());

    expect(statusCalls).toBe(2);
    expect(latest?.status?.status).toBe('deleted');
    expect(latest?.actionError).toBeInstanceOf(RankingApiError);
  });

  it('clears the retained action error when the profile modal dismisses its feedback', async () => {
    const api = createAPI({
      sync: async () => {
        throw new RankingApiError('ranking_center_unavailable', 503);
      },
    });
    await renderHook(true, api);
    await act(async () => latest?.sync());
    expect(latest?.actionError).toBeInstanceOf(RankingApiError);

    const clearActionError = (latest as unknown as { clearActionError?: () => void } | null)?.clearActionError;
    expect(clearActionError).toBeTypeOf('function');
    if (!clearActionError) return;

    await act(async () => clearActionError());
    expect(latest?.actionError).toBeNull();
  });
});
