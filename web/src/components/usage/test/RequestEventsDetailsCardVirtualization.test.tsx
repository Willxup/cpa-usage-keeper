// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { RequestEventsTestCard } from './requestEventsFixtures';
import { TestResizeObserver } from './virtualizationFixtures';
import type { UsageEvent } from '@/lib/types';
import type { RequestEventsDetailsCard } from '../RequestEventsDetailsCard';

const buildEvent = (index: number): UsageEvent => ({
  id: String(index + 1),
  timestamp: '2026-07-11T10:00:00.000Z',
  api_key: 'Production Key',
  model: `model-${index}`,
  endpoint: 'POST /v1/messages',
  source: 'Provider A',
  source_raw: 'source-a',
  source_type: 'openai',
  auth_index: '1',
  request_id: `request-${index}`,
  failed: false,
  latency_ms: 120,
  ttft_ms: 45,
  speed_tps: 30,
  tokens: {
    input_tokens: 100,
    output_tokens: 60,
    reasoning_tokens: 20,
    cache_read_tokens: 20,
    cache_creation_tokens: 0,
    total_tokens: 200,
  },
  cost_usd: 0.1234,
  cost_available: true,
  pricing_style: 'claude',
});

describe('RequestEventsDetailsCard event table virtualization', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    vi.useFakeTimers();
    vi.stubGlobal('ResizeObserver', TestResizeObserver);
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
      callback(performance.now());
      return 0;
    });
    vi.stubGlobal('cancelAnimationFrame', () => undefined);
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function getBoundingClientRect() {
      if (this instanceof HTMLTableRowElement) {
        const spacerHeight = Number.parseFloat(this.style.height);
        return new DOMRect(0, 0, 1200, Number.isFinite(spacerHeight) ? spacerHeight : 44);
      }
      return new DOMRect(0, 0, 1200, 600);
    });
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    // 完成虚拟列表的滚动结束更新，再销毁 DOM。
    await act(async () => vi.advanceTimersByTimeAsync(200));
    await act(async () => root.unmount());
    container.remove();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  const renderEvents = async (
    events: UsageEvent[],
    props: Partial<React.ComponentProps<typeof RequestEventsDetailsCard>> = {},
  ) => {
    await act(async () => {
      root.render(
        <RequestEventsTestCard
          events={events}
          initialVisibleColumnIds={['timestamp', 'model', 'total_tokens']}
          {...props}
        />,
      );
      await Promise.resolve();
    });
    return document.querySelector<HTMLElement>('[class*="requestEventsTableWrapper"]')!;
  };

  const scrollTo = async (scroller: HTMLElement, scrollTop: number) => {
    scroller.scrollTop = scrollTop;
    await act(async () => {
      scroller.dispatchEvent(new Event('scroll'));
      await vi.advanceTimersByTimeAsync(0);
    });
  };

  const scrollNearBottom = async (scroller: HTMLElement) => {
    Object.defineProperties(scroller, {
      clientHeight: { configurable: true, value: 600 },
      scrollHeight: { configurable: true, value: 4400 },
    });
    await scrollTo(scroller, 3500);
  };

  it('keeps a 1000-event page bounded in the DOM and advances the window on scroll', async () => {
    const events = Array.from({ length: 1000 }, (_, index) => buildEvent(index));
    const scroller = await renderEvents(events);
    const table = scroller.querySelector('table');
    expect(scroller.dataset.virtualized).toBe('true');
    expect(table?.getAttribute('aria-rowcount')).toBe('1001');

    const initialRows = Array.from(scroller.querySelectorAll<HTMLTableRowElement>('tbody tr[data-index]'));
    const initialIndexes = initialRows.map((row) => Number(row.dataset.index));
    expect(initialRows.length).toBeGreaterThan(0);
    expect(initialRows.length).toBeLessThan(100);

    await scrollTo(scroller, 35_000);

    const scrolledRows = Array.from(scroller.querySelectorAll<HTMLTableRowElement>('tbody tr[data-index]'));
    const scrolledIndexes = scrolledRows.map((row) => Number(row.dataset.index));
    expect(scrolledRows.length).toBeGreaterThan(0);
    expect(scrolledRows.length).toBeLessThan(100);
    expect(Math.min(...scrolledIndexes)).toBeGreaterThan(Math.min(...initialIndexes));

    await scrollTo(scroller, 69_500);

    const finalRows = Array.from(scroller.querySelectorAll<HTMLTableRowElement>('tbody tr[data-index]'));
    const finalIndexes = finalRows.map((row) => Number(row.dataset.index));
    expect(finalRows.length).toBeLessThan(100);
    expect(Math.max(...finalIndexes)).toBe(999);
  });

  it('keeps small pages fully rendered without virtual spacer rows', async () => {
    const events = Array.from({ length: 3 }, (_, index) => buildEvent(index));
    const scroller = await renderEvents(events);
    const rows = scroller.querySelectorAll('tbody tr');
    expect(scroller.dataset.virtualized).toBe('false');
    expect(rows).toHaveLength(3);
    expect(scroller.querySelector('[class*="requestEventsVirtualSpacerRow"]')).toBeNull();
    expect(scroller.textContent).toContain('model-2');
  });

  it('requests the next cursor batch when infinite scrolling nears the bottom', async () => {
    const events = Array.from({ length: 100 }, (_, index) => buildEvent(index));
    const onLoadMore = vi.fn();
    const scroller = await renderEvents(events, {
      totalCount: 500,
      hasMore: true,
      onLoadMore,
    });
    await scrollNearBottom(scroller);

    expect(onLoadMore).toHaveBeenCalledTimes(1);
  });

  it('does not auto-load while the first page is refreshing', async () => {
    const events = Array.from({ length: 100 }, (_, index) => buildEvent(index));
    const onLoadMore = vi.fn();
    const scroller = await renderEvents(events, {
      loading: true,
      totalCount: 500,
      hasMore: true,
      onLoadMore,
    });
    await scrollNearBottom(scroller);

    expect(onLoadMore).not.toHaveBeenCalled();
  });

  it('pauses automatic loading after an error while keeping manual retry available', async () => {
    const events = Array.from({ length: 100 }, (_, index) => buildEvent(index));
    const onLoadMore = vi.fn();
    const scroller = await renderEvents(events, {
      totalCount: 500,
      hasMore: true,
      autoLoadMore: false,
      onLoadMore,
    });
    await scrollNearBottom(scroller);
    expect(onLoadMore).not.toHaveBeenCalled();

    const loadMoreButton = container.querySelector<HTMLButtonElement>(
      '[class*="requestEventsPaginationFooter"] button',
    )!;
    const loadedSummary = container.querySelector<HTMLElement>(
      '[class*="requestEventsPaginationPage"]',
    );
    expect(loadedSummary?.getAttribute('role')).toBe('status');
    expect(loadedSummary?.getAttribute('aria-label')).toBe('Loaded 100 / 500');
    await act(async () => {
      loadMoreButton.click();
    });
    expect(onLoadMore).toHaveBeenCalledOnce();
  });
});
