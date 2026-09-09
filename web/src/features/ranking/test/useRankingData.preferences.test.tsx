// @vitest-environment happy-dom

import { act, useEffect } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useRankingData, type RankingDataAPI } from '../hooks/useRankingData';
import { RANKING_PREFERENCES_STORAGE_KEY } from '../preferences';
import type { RankingLeaderboardResponse } from '../types';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let latest: ReturnType<typeof useRankingData> | null = null;

const leaderboard = vi.fn(async (period, metric): Promise<RankingLeaderboardResponse> => ({
  period,
  period_key: `${period}-key`,
  metric,
  generated_at: '2026-09-02T00:00:00Z',
  stale: false,
  entries: [],
}));

const api: RankingDataAPI = {
  status: async () => ({ status: 'active' }),
  metadata: async () => ({
    server_time: '2026-09-02T00:00:00Z',
    generated_at: '2026-09-02T00:00:00Z',
    stale: false,
    protocol_version: 1,
    metrics_version: 1,
    period_timezone: 'UTC',
    avatar_catalog_version: 1,
    avatar_count: 1,
    read_marker_version: 1,
    refresh_interval_seconds: 60,
    suggested_sync_interval_seconds: 60,
    periods: [],
    metrics: [],
    overall_weights: {},
  }),
  leaderboard,
  join: async () => ({ status: 'active' }),
  sync: async () => ({ status: 'active' }),
  pause: async () => ({ status: 'paused' }),
  resume: async () => ({ status: 'active' }),
  exit: async () => ({ status: 'disabled' }),
};

function Harness() {
  const result = useRankingData({ enabled: true, api });
  useEffect(() => { latest = result; }, [result]);
  return null;
}

const storedPreferences = () => JSON.parse(String(window.localStorage.getItem(RANKING_PREFERENCES_STORAGE_KEY)));

describe('ranking period and metric wiring', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    window.localStorage.clear();
    leaderboard.mockClear();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    latest = null;
  });

  it('defaults to today and overall without a stored selection', async () => {
    await act(async () => root.render(<Harness />));

    expect(latest?.period).toBe('today');
    expect(latest?.metric).toBe('overall');
    expect(leaderboard).toHaveBeenCalledWith('today', 'overall', expect.anything());
  });

  it('restores the stored selection into the first leaderboard request', async () => {
    window.localStorage.setItem(RANKING_PREFERENCES_STORAGE_KEY, JSON.stringify({
      version: 1,
      period: 'previous_month',
      metric: 'peak_tpm',
    }));
    await act(async () => root.render(<Harness />));

    expect(latest?.period).toBe('previous_month');
    expect(latest?.metric).toBe('peak_tpm');
    expect(leaderboard).toHaveBeenCalledWith('previous_month', 'peak_tpm', expect.anything());
  });

  it('persists each selector without dropping the other', async () => {
    await act(async () => root.render(<Harness />));

    await act(async () => latest?.setMetric('cache_read_rate'));
    expect(storedPreferences()).toEqual({ version: 1, period: 'today', metric: 'cache_read_rate' });

    await act(async () => latest?.setPeriod('yesterday'));
    expect(latest?.period).toBe('yesterday');
    expect(latest?.metric).toBe('cache_read_rate');
    expect(storedPreferences()).toEqual({ version: 1, period: 'yesterday', metric: 'cache_read_rate' });
  });

  it('ignores a damaged record instead of requesting it', async () => {
    window.localStorage.setItem(RANKING_PREFERENCES_STORAGE_KEY, JSON.stringify({
      version: 1,
      period: 'last_week',
      metric: 'total_cost',
    }));
    await act(async () => root.render(<Harness />));

    expect(leaderboard).toHaveBeenCalledWith('today', 'overall', expect.anything());
  });
});
