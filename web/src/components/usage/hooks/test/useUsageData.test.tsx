// @vitest-environment happy-dom

import { act, useEffect } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/lib/api';
import { useUsageStatsStore } from '@/stores';
import { useUsageData, type UseUsageDataOptions } from '../useUsageData';

const apiMocks = vi.hoisted(() => ({
  fetchUsageOverview: vi.fn(),
}));

vi.mock('@/lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/lib/api')>(),
  fetchUsageOverview: apiMocks.fetchUsageOverview,
}));

let latest: ReturnType<typeof useUsageData> | null = null;

function Harness({ options }: { options: UseUsageDataOptions }) {
  const result = useUsageData(options);
  useEffect(() => {
    latest = result;
  }, [result]);
  return null;
}

describe('useUsageData', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    apiMocks.fetchUsageOverview.mockReset();
    useUsageStatsStore.getState().clearUsageStats();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    latest = null;
  });

  it('reports a bounds conflict from the initial automatic request', async () => {
    const onRangeBoundsConflict = vi.fn(() => true);
    apiMocks.fetchUsageOverview.mockRejectedValueOnce(new ApiError('expired range', 409));

    await act(async () => {
      root.render(<Harness options={{
        range: 'custom',
        customUnit: 'day',
        customStart: '2025-07-22',
        customEnd: '2026-07-21',
        onRangeBoundsConflict,
      }} />);
      await Promise.resolve();
    });

    expect(onRangeBoundsConflict).toHaveBeenCalledTimes(1);
    expect(onRangeBoundsConflict).toHaveBeenCalledWith(expect.objectContaining({ status: 409 }));
    expect(latest?.error).toBe('expired range');
  });
});
