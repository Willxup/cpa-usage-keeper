// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { RequestEventsTestCard } from './requestEventsFixtures';
import { TestResizeObserver } from './virtualizationFixtures';
import type { UsageEvent } from '@/lib/types';
import type { RequestEventsDetailsCard } from '../RequestEventsDetailsCard';

const measurementCache = vi.hoisted(() => ({ current: null as Map<string | number | bigint, number> | null }));
vi.mock('@tanstack/react-virtual', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-virtual')>();
  return {
    ...actual,
    useVirtualizer: (...args: Parameters<typeof actual.useVirtualizer>) => {
      const instance = actual.useVirtualizer(...args);
      measurementCache.current = instance.itemSizeCache;
      return instance;
    },
  };
});

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

  const scrollTo = async (scroller: HTMLElement, scrollTop: number, user = true) => {
    const deltaY = scrollTop - scroller.scrollTop;
    await act(async () => {
      if (user) scroller.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaY }));
      scroller.scrollTop = scrollTop;
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

  it('retains the same table and keyed rows across latest-page increments', async () => {
    const events = Array.from({ length: 50 }, (_, index) => buildEvent(index));
    const scroller = await renderEvents(events);
    const table = scroller.querySelector('table');
    const row = Array.from(scroller.querySelectorAll('tbody tr')).find((item) => item.textContent?.includes('model-1'));
    const nextScroller = await renderEvents([buildEvent(50), ...events.slice(0, 49)]);
    expect(nextScroller).toBe(scroller);
    expect(nextScroller.querySelector('table')).toBe(table);
    expect(Array.from(nextScroller.querySelectorAll('tbody tr')).find((item) => item.textContent?.includes('model-1'))).toBe(row);
    expect(nextScroller.querySelectorAll('tbody tr')).toHaveLength(50);
  });

  it('removes sizes for rows that leave the latest page while retaining current measurements', async () => {
    let events = Array.from({ length: 50 }, (_, index) => buildEvent(index));
    await renderEvents(events);
    const retainedSize = measurementCache.current!.get('1');
    expect(retainedSize).toBeDefined();
    for (let index = 50; index < 55; index++) {
      events = [buildEvent(index), ...events.slice(0, 49)];
      await renderEvents(events);
    }
    expect(measurementCache.current!.size).toBeLessThanOrEqual(events.length);
    expect(measurementCache.current!.has('50')).toBe(false);
    expect(measurementCache.current!.get('1')).toBe(retainedSize);
  });

  it('keeps the reading position when the first historical page enables virtualization', async () => {
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function getBoundingClientRect() {
      return new DOMRect(0, 0, 1200, this instanceof HTMLTableRowElement ? 79 : 600);
    });
    const events = Array.from({ length: 50 }, (_, index) => buildEvent(index));
    const scroller = await renderEvents(events);
    Object.defineProperty(scroller, 'scrollTo', { configurable: true, value: ({ top }: ScrollToOptions) => { scroller.scrollTop = top ?? 0; } });
    await scrollTo(scroller, 180);
    await renderEvents([...events, ...Array.from({ length: 50 }, (_, index) => buildEvent(index + 50))], { reading: true });
    await act(async () => vi.advanceTimersByTimeAsync(250));
    expect(scroller.dataset.virtualized).toBe('true');
    expect(scroller.scrollTop).toBe(180);
  });

  it('does not restore the removed container position when an empty page grows again', async () => {
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function getBoundingClientRect() {
      return new DOMRect(0, 0, 1200, this instanceof HTMLTableRowElement ? 79 : 600);
    });
    vi.spyOn(HTMLElement.prototype, 'scrollTo').mockImplementation(function (...args) {
      const options = args[0] as ScrollToOptions;
      const rowCount = Number(this.dataset.loadedRowCount ?? 0);
      this.scrollTop = Math.min(options.top ?? 0, Math.max(0, rowCount * 79 - 600));
    });
    const onTablePositionChange = vi.fn();
    const onStartReading = vi.fn();
    const props = { onTablePositionChange, onStartReading };
    const events = Array.from({ length: 50 }, (_, index) => buildEvent(index));
    const original = await renderEvents(events, props);
    await scrollTo(original, 220);
    expect(onTablePositionChange).toHaveBeenLastCalledWith(false);
    await renderEvents([], props);
    expect(container.querySelector('[class*="requestEventsTableWrapper"]')).toBeNull();
    await act(async () => vi.advanceTimersByTimeAsync(1100));
    const next = await renderEvents([buildEvent(50)], props);
    expect(next).not.toBe(original);
    expect(next.scrollTop).toBe(0);
    onTablePositionChange.mockClear();
    await renderEvents(Array.from({ length: 50 }, (_, index) => buildEvent(index + 50)), props);
    await act(async () => vi.advanceTimersByTimeAsync(500));
    expect(next.scrollTop).toBe(0);
    expect(onTablePositionChange).not.toHaveBeenCalledWith(false);
    onStartReading.mockClear();
    await scrollTo(next, 220);
    expect(onTablePositionChange).toHaveBeenLastCalledWith(false);
    expect(onStartReading).toHaveBeenCalledOnce();
  });

  it('does not carry the removed container input intent into a programmatic scroll', async () => {
    const onTablePositionChange = vi.fn();
    const onStartReading = vi.fn();
    const props = { onTablePositionChange, onStartReading };
    const events = Array.from({ length: 50 }, (_, index) => buildEvent(index));
    const original = await renderEvents(events, props);
    await scrollTo(original, 220);
    expect(onStartReading).toHaveBeenCalledOnce();
    await renderEvents([], props);
    const next = await renderEvents(events, props);
    onTablePositionChange.mockClear();
    onStartReading.mockClear();
    await scrollTo(next, 300, false);
    expect(onTablePositionChange).toHaveBeenLastCalledWith(false);
    expect(onStartReading).not.toHaveBeenCalled();
  });

  it('cancels a pending return when the scroll container becomes empty', async () => {
    const onReturnToTop = vi.fn();
    const props = { onReturnToTop };
    const events = Array.from({ length: 50 }, (_, index) => buildEvent(index));
    const original = await renderEvents(events, props);
    await scrollTo(original, 220);
    await scrollTo(original, 0);
    await renderEvents([], props);
    await act(async () => vi.advanceTimersByTimeAsync(400));
    expect(onReturnToTop).not.toHaveBeenCalled();
  });

  it('does not automatically load history when the latest page fits without scrolling', async () => {
    vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(600);
    vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockReturnValue(400);
    const onLoadMore = vi.fn();
    await renderEvents([buildEvent(1)], { hasMore: true, totalCount: 2, onLoadMore });
    await act(async () => vi.advanceTimersByTimeAsync(500));
    expect(onLoadMore).not.toHaveBeenCalled();
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

  it('allows load more to cancel an automatic return while still blocking it for manual refresh', async () => {
    const onStartReading = vi.fn();
    const onLoadMore = vi.fn();
    const events = Array.from({ length: 50 }, (_, index) => buildEvent(index));
    await renderEvents(events, { totalCount: 100, hasMore: true, loading: true, reading: true, restoring: true, onStartReading, onLoadMore });
    const button = container.querySelector<HTMLButtonElement>('[class*="requestEventsPaginationFooter"] button')!;
    expect(button.disabled).toBe(false);
    await act(async () => button.click());
    expect(onStartReading).toHaveBeenCalledOnce();
    expect(onLoadMore).toHaveBeenCalledOnce();
    await renderEvents(events, { totalCount: 100, hasMore: true, loading: true, onLoadMore });
    expect(button.disabled).toBe(true);
  });

  it('requires a real vertical return and 300ms stillness to resume after loading at the top', async () => {
    const onStartReading = vi.fn();
    const onReturnToTop = vi.fn();
    const onLoadMore = vi.fn();
    const onTablePositionChange = vi.fn();
    const scroller = await renderEvents(Array.from({ length: 50 }, (_, index) => buildEvent(index)), {
      totalCount: 100, hasMore: true, onLoadMore, onStartReading, onReturnToTop, onTablePositionChange,
    });
    const button = container.querySelector<HTMLButtonElement>('[class*="requestEventsPaginationFooter"] button')!;
    await act(async () => { button.click(); await vi.advanceTimersByTimeAsync(500); });
    expect(onStartReading).toHaveBeenCalledTimes(1);
    expect(onReturnToTop).not.toHaveBeenCalled();
    // 横向滚动和非用户触发的纵向滚动都不是回顶意图。
    await act(async () => { scroller.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaX: 20 })); scroller.dispatchEvent(new Event('scroll')); });
    await scrollTo(scroller, 100, false);
    await scrollTo(scroller, 0, false);
    await act(async () => vi.advanceTimersByTimeAsync(400));
    expect(onReturnToTop).not.toHaveBeenCalled();
    // 一次用户下滑不能把紧随其后的程序回顶误认为用户决定。
    await scrollTo(scroller, 100);
    await scrollTo(scroller, 0, false);
    await act(async () => vi.advanceTimersByTimeAsync(400));
    expect(onReturnToTop).not.toHaveBeenCalled();
    await scrollTo(scroller, 100);
    await scrollTo(scroller, 7);
    await act(async () => vi.advanceTimersByTimeAsync(200));
    await scrollTo(scroller, 0);
    await act(async () => vi.advanceTimersByTimeAsync(299));
    expect(onReturnToTop).not.toHaveBeenCalled();
    await act(async () => vi.advanceTimersByTimeAsync(1));
    expect(onReturnToTop).toHaveBeenCalledOnce();
    expect(onTablePositionChange).toHaveBeenCalledWith(false);
    expect(onTablePositionChange).toHaveBeenLastCalledWith(true);
  });

  it('treats every frame of one Home key scroll as a single return intent', async () => {
    const onReturnToTop = vi.fn();
    const scroller = await renderEvents(Array.from({ length: 50 }, (_, index) => buildEvent(index)), { onReturnToTop });
    await scrollTo(scroller, 420);
    await act(async () => { scroller.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'Home' })); });
    for (const position of [402, 363, 301, 225, 146, 78, 30, 5, 0]) {
      await scrollTo(scroller, position, false);
      await act(async () => vi.advanceTimersByTimeAsync(16));
    }
    await act(async () => vi.advanceTimersByTimeAsync(283));
    expect(onReturnToTop).not.toHaveBeenCalled();
    await act(async () => vi.advanceTimersByTimeAsync(1));
    expect(onReturnToTop).toHaveBeenCalledOnce();
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
