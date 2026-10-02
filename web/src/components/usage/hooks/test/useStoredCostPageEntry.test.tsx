// @vitest-environment happy-dom

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useUsageStatsStore } from '@/stores';
import { useUsageData } from '../useUsageData';
import { useOverviewRealtimeData } from '../useOverviewRealtimeData';

const api = vi.hoisted(() => ({
  fetchUsageOverview: vi.fn(),
  fetchUsageOverviewRealtime: vi.fn(),
}));

vi.mock('@/lib/api', async (original) => ({
  ...await original<typeof import('@/lib/api')>(),
  ...api,
}));

function Page({ enabled }: { enabled: boolean }) {
  const overview = useUsageData({ enabled, range: '24h' });
  const realtime = useOverviewRealtimeData({ enabled, realtimeWindow: '15m' });
  return <div>{overview.currentUsage?.usage.total_cost}:{realtime.realtime?.bucket_seconds}</div>;
}

describe('stored-cost page entry', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    vi.resetAllMocks();
    useUsageStatsStore.getState().clearUsageStats();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    useUsageStatsStore.getState().clearUsageStats();
  });

  it('fetches on entry and re-entry even while previous local data is still fresh', async () => {
    api.fetchUsageOverview.mockResolvedValue({ usage: { total_cost: 1 } });
    api.fetchUsageOverviewRealtime.mockResolvedValue({ window: '15m', bucket_seconds: 30 });
    await useUsageStatsStore.getState().loadUsageStats({ range: '24h' });
    await useUsageStatsStore.getState().loadUsageStatsRealtime({ realtimeWindow: '15m' });

    api.fetchUsageOverview.mockResolvedValue({ usage: { total_cost: 2 } });
    api.fetchUsageOverviewRealtime.mockResolvedValue({ window: '15m', bucket_seconds: 60 });
    await act(async () => root.render(<Page enabled />));
    expect(container.textContent).toBe('2:60');
    expect(api.fetchUsageOverview).toHaveBeenCalledTimes(2);
    expect(api.fetchUsageOverviewRealtime).toHaveBeenCalledTimes(2);

    await act(async () => root.render(<Page enabled={false} />));
    expect(api.fetchUsageOverview).toHaveBeenCalledTimes(2);
    expect(api.fetchUsageOverviewRealtime).toHaveBeenCalledTimes(2);
    api.fetchUsageOverview.mockResolvedValue({ usage: { total_cost: 3 } });
    api.fetchUsageOverviewRealtime.mockResolvedValue({ window: '15m', bucket_seconds: 120 });
    await act(async () => root.render(<Page enabled />));
    expect(container.textContent).toBe('3:120');
    expect(api.fetchUsageOverview).toHaveBeenCalledTimes(3);
    expect(api.fetchUsageOverviewRealtime).toHaveBeenCalledTimes(3);
  });
});
