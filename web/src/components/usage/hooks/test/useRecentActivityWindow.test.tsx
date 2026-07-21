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

    selectWindow('7d');
    expect(latest?.manualWindow).toBe('7d');
    expect(latest?.request).toEqual({ range: '7d' });
  });

  it('keeps a manual window while the top time identity is unchanged', () => {
    const query = buildUsageRangeQuery({ range: '8h' });
    renderQuery(query);
    selectWindow('30d');
    renderQuery({ ...query });

    expect(latest?.manualWindow).toBe('30d');
    expect(latest?.request).toEqual({ range: '30d' });
  });

  it('uses the Activity-specific query when the user selects one year', () => {
    renderQuery(buildUsageRangeQuery({ range: '30d' }));
    selectWindow('1y');

    expect(latest?.manualWindow).toBe('1y');
    expect(latest?.request).toEqual({ window: '1y' });
  });

  it('clears a manual window for every top time identity change including A to B to A', () => {
    const first = buildUsageRangeQuery({ range: '8h' });
    const second = buildUsageRangeQuery({ range: '24h' });
    renderQuery(first);
    selectWindow('7d');

    renderQuery(second);
    expect(latest?.manualWindow).toBeNull();
    expect(latest?.request).toEqual({ range: '24h' });

    renderQuery(first);
    expect(latest?.manualWindow).toBeNull();
    expect(latest?.request).toEqual({ range: '8h' });
  });
});
