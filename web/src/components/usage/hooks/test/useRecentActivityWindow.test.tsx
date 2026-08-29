// @vitest-environment happy-dom

import { act, useEffect } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import type { UsageRangeQuery } from '@/utils/usage/rangeQuery';
import { buildUsageRangeQuery } from '@/utils/usage/rangeQuery';
import type { UsageActivityWindow } from '@/lib/types';
import { useRecentActivityWindow } from '../useRecentActivityWindow';

let latest: ReturnType<typeof useRecentActivityWindow> | null = null;

function Harness({ query }: { query: UsageRangeQuery }) {
  const result = useRecentActivityWindow(query);
  useEffect(() => {
    latest = result;
  }, [result]);
  return null;
}

describe('useRecentActivityWindow', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    latest = null;
  });

  const renderQuery = (query: UsageRangeQuery) => {
    act(() => root.render(<Harness query={query} />));
  };

  const selectWindow = (window: UsageActivityWindow) => {
    act(() => latest?.setWindow(window));
  };

  it('sends the top Overview query until the user selects a window', () => {
    const query = buildUsageRangeQuery({
      range: 'custom',
      customUnit: 'day',
      customStart: '2026-07-18',
      customEnd: '2026-07-21',
    });
    renderQuery(query);

    expect(latest?.manualWindow).toBeNull();
    expect(latest?.request).toEqual({
      range: 'custom',
      unit: 'day',
      start: '2026-07-18',
      end: '2026-07-21',
    });

    selectWindow('week');
    expect(latest?.manualWindow).toBe('week');
    expect(latest?.request).toEqual({ window: 'week' });
  });

  it('keeps a manual window while the top time identity is unchanged', () => {
    const query = buildUsageRangeQuery({ range: '8h' });
    renderQuery(query);
    selectWindow('month');
    renderQuery({ ...query });

    expect(latest?.manualWindow).toBe('month');
    expect(latest?.request).toEqual({ window: 'month' });
  });

  it('uses the Activity-specific query when the user selects one year', () => {
    renderQuery(buildUsageRangeQuery({ range: '30d' }));
    selectWindow('year');

    expect(latest?.manualWindow).toBe('year');
    expect(latest?.request).toEqual({ window: 'year' });
  });

  it('uses dedicated calendar Activity windows for Today and Yesterday', () => {
    renderQuery(buildUsageRangeQuery({ range: 'today' }));
    expect(latest?.manualWindow).toBeNull();
    expect(latest?.request).toEqual({ window: 'today' });

    renderQuery(buildUsageRangeQuery({ range: 'yesterday' }));
    expect(latest?.manualWindow).toBeNull();
    expect(latest?.request).toEqual({ window: 'yesterday' });
  });

  it('clears a manual window for every top time identity change including A to B to A', () => {
    const first = buildUsageRangeQuery({ range: '8h' });
    const second = buildUsageRangeQuery({ range: '24h' });
    renderQuery(first);
    selectWindow('week');

    renderQuery(second);
    expect(latest?.manualWindow).toBeNull();
    expect(latest?.request).toEqual({ range: '24h' });

    renderQuery(first);
    expect(latest?.manualWindow).toBeNull();
    expect(latest?.request).toEqual({ range: '8h' });
  });
});
