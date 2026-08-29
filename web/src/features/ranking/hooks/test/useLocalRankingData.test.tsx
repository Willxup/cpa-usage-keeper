// @vitest-environment happy-dom

import { act, useEffect } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { RankingLeaderboardResponse, RankingMetric, RankingPeriod } from '../../types';
import {
  LOCAL_RANKING_POLL_INTERVAL_MS,
  useLocalRankingData,
  type LocalRankingDataAPI,
} from '../useLocalRankingData';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const board = (period: RankingPeriod, metric: RankingMetric, value = 93): RankingLeaderboardResponse => ({
  period,
  period_key: period.includes('month') ? '2026-07' : '2026-07-31',
  metric,
  generated_at: '2026-07-31T04:00:00Z',
  stale: false,
  entries: [{ rank: 1, participant_id: '1', display_name: 'Primary', avatar_id: 1, value }],
});

const deferred = <T,>() => {
  let resolve: (value: T) => void = () => undefined;
  const promise = new Promise<T>((nextResolve) => {
    resolve = nextResolve;
  });
  return { promise, resolve };
};

let latest: ReturnType<typeof useLocalRankingData> | null = null;

function Harness({ enabled, period, metric, api }: {
  enabled: boolean;
  period: RankingPeriod;
  metric: RankingMetric;
  api: LocalRankingDataAPI;
}) {
  const result = useLocalRankingData({ enabled, period, metric, api });
  useEffect(() => { latest = result; }, [result]);
  return null;
}

describe('useLocalRankingData', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
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

  const renderHook = async (enabled: boolean, period: RankingPeriod, metric: RankingMetric, api: LocalRankingDataAPI) => {
    await act(async () => {
      root.render(<Harness enabled={enabled} period={period} metric={metric} api={api} />);
    });
  };

  it('loads only while local scope is enabled and follows selection changes', async () => {
    const calls: string[] = [];
    const api: LocalRankingDataAPI = {
      leaderboard: async (period, metric) => {
        calls.push(`${period}:${metric}`);
        return board(period, metric);
      },
    };

    await renderHook(false, 'today', 'overall', api);
    expect(calls).toEqual([]);
    await renderHook(true, 'today', 'overall', api);
    await renderHook(true, 'yesterday', 'total_tokens', api);

    expect(calls).toEqual(['today:overall', 'yesterday:total_tokens']);
    expect(latest?.leaderboard).toMatchObject({ period: 'yesterday', metric: 'total_tokens' });
  });

  it('polls the selected local board every minute', async () => {
    vi.useFakeTimers();
    let calls = 0;
    const api: LocalRankingDataAPI = {
      leaderboard: async (period, metric) => {
        calls += 1;
        return board(period, metric, 90 + calls);
      },
    };

    await renderHook(true, 'today', 'overall', api);
    expect(calls).toBe(1);
    await act(async () => vi.advanceTimersByTimeAsync(LOCAL_RANKING_POLL_INTERVAL_MS));
    expect(calls).toBe(2);
    expect(latest?.leaderboard?.entries[0]?.value).toBe(92);
  });

  it('retains the previous response while another selection is loading', async () => {
    const pendingYesterday = deferred<RankingLeaderboardResponse>();
    const api: LocalRankingDataAPI = {
      leaderboard: async (period, metric) => (
        period === 'yesterday' ? pendingYesterday.promise : board(period, metric)
      ),
    };

    await renderHook(true, 'today', 'overall', api);
    await renderHook(true, 'yesterday', 'overall', api);

    expect(latest?.leaderboardLoading).toBe(true);
    expect(latest?.leaderboard).toMatchObject({ period: 'today', metric: 'overall' });

    await act(async () => pendingYesterday.resolve(board('yesterday', 'overall', 88)));
    expect(latest?.leaderboardLoading).toBe(false);
    expect(latest?.leaderboard).toMatchObject({ period: 'yesterday', metric: 'overall' });
  });

  it('refreshes only the local leaderboard', async () => {
    let calls = 0;
    const api: LocalRankingDataAPI = {
      leaderboard: async (period, metric) => {
        calls += 1;
        return board(period, metric, 90 + calls);
      },
    };

    await renderHook(true, 'today', 'overall', api);
    await act(async () => latest?.refreshLeaderboard());
    expect(calls).toBe(2);
    expect(latest?.leaderboard?.entries[0]?.value).toBe(92);
  });

  it('updates every cached occurrence of a saved Key profile immediately', async () => {
	const updateProfile = vi.fn(async () => ({
		participant_id: '1', key_alias: 'Renamed', display_name: 'Renamed', avatar_id: 42,
	}));
	const api: LocalRankingDataAPI = {
		leaderboard: async (period, metric) => board(period, metric),
		updateProfile,
	};

	await renderHook(true, 'today', 'overall', api);
	await act(async () => latest?.updateProfile('1', { key_alias: 'Renamed', avatar_id: 42 }));

	expect(updateProfile).toHaveBeenCalledWith('1', { key_alias: 'Renamed', avatar_id: 42 }, undefined);
    expect(latest?.leaderboard?.entries[0]).toMatchObject({
      participant_id: '1', key_alias: 'Renamed', display_name: 'Renamed', avatar_id: 42,
    });
  });

  it('keeps a settings alias patch when the local board reload fails', async () => {
    let shouldFail = false;
    const api: LocalRankingDataAPI = {
      leaderboard: async (period, metric) => {
        if (shouldFail) throw new Error('local board unavailable');
        return board(period, metric);
      },
    };

    await renderHook(true, 'today', 'overall', api);
    await renderHook(false, 'today', 'overall', api);
    await act(async () => latest?.patchProfileCache('1', {
      key_alias: 'Settings Alias',
      display_name: 'Settings Alias',
    }));
    shouldFail = true;
    await renderHook(true, 'today', 'overall', api);

    expect(latest?.leaderboard).toMatchObject({ stale: true });
    expect(latest?.leaderboard?.entries[0]).toMatchObject({
      participant_id: '1',
      key_alias: 'Settings Alias',
      display_name: 'Settings Alias',
      avatar_id: 1,
    });
  });

  it('does not let an older in-flight board overwrite a saved Key profile', async () => {
    const pendingRefresh = deferred<RankingLeaderboardResponse>();
    let leaderboardCalls = 0;
    const api: LocalRankingDataAPI = {
      leaderboard: async (period, metric) => {
        leaderboardCalls += 1;
        return leaderboardCalls === 1 ? board(period, metric) : pendingRefresh.promise;
      },
      updateProfile: async () => ({
        participant_id: '1', key_alias: 'Renamed', display_name: 'Renamed', avatar_id: 42,
      }),
    };

    await renderHook(true, 'today', 'overall', api);
    let pendingLoad: Promise<RankingLeaderboardResponse | null> | undefined;
    await act(async () => {
      pendingLoad = latest?.refreshLeaderboard();
    });
    await act(async () => latest?.updateProfile('1', { key_alias: 'Renamed', avatar_id: 42 }));
    await act(async () => pendingRefresh.resolve(board('today', 'overall')));
    await pendingLoad;

    expect(latest?.leaderboard?.entries[0]).toMatchObject({
      key_alias: 'Renamed', display_name: 'Renamed', avatar_id: 42,
    });
    expect(latest?.leaderboardLoading).toBe(false);
  });
});
