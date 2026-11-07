// @vitest-environment happy-dom
import { act, useEffect } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, fetchUsageEvents, openUsageEventsLive } from '@/lib/api';
import type { UsageEvent, UsageEventsResponse } from '@/lib/types';
import { useRequestEventsFeed } from '../useRequestEventsFeed';

vi.mock('@/lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/lib/api')>(),
  openUsageEventsLive: vi.fn(),
  fetchUsageEvents: vi.fn(),
}));

const encoder = new TextEncoder();
const bounds = { range_start: '2026-01-01T00:00:00Z', range_end: '2026-12-31T00:00:00Z', end_exclusive: false };
const event = (id: string, second: number): UsageEvent => ({ id, timestamp: `2026-06-01T00:00:${String(second).padStart(2, '0')}Z` }) as UsageEvent;
const snapshot = (events: UsageEvent[]) => ({ ...bounds, events, total_count: 20, has_more: true, next_cursor: 'history-1' });
let streams: ReadableStreamDefaultController<Uint8Array>[];
let latest: ReturnType<typeof useRequestEventsFeed>;
const onAuthRequired = vi.fn();
const onRangeBoundsConflict = vi.fn((_error: unknown) => false);

function Harness() {
  const feed = useRequestEventsFeed({ enabled: true, request: { range: 'today' }, options: { cursorMode: true, pageSize: 50 }, onAuthRequired, onRangeBoundsConflict });
  useEffect(() => { latest = feed; });
  return <div>{feed.events.map((item) => item.id).join(',')}</div>;
}

async function emit(index: number, kind: string, id: string, data: unknown) {
  await act(async () => {
    streams[index].enqueue(encoder.encode(`id: ${id}\nevent: ${kind}\ndata: ${JSON.stringify(data)}\n\n`));
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

describe('useRequestEventsFeed', () => {
  let root: Root;
  let container: HTMLDivElement;
  beforeEach(async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    streams = [];
    onAuthRequired.mockReset();
    onRangeBoundsConflict.mockReset().mockReturnValue(false);
    vi.mocked(openUsageEventsLive).mockReset().mockImplementation(async (_request, _options, _after, signal) => {
      const body = new ReadableStream<Uint8Array>({ start(controller) {
        streams.push(controller);
        signal.addEventListener('abort', () => controller.error(new DOMException('Aborted', 'AbortError')), { once: true });
      } });
      return new Response(body, { headers: { 'Content-Type': 'text/event-stream' } });
    });
    vi.mocked(fetchUsageEvents).mockReset();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    await act(async () => { root.render(<Harness />); await new Promise((resolve) => setTimeout(resolve, 0)); });
  });
  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
    vi.restoreAllMocks();
  });

  it('keeps history visible while buffering live events, deduplicates overlap and resumes from the accepted cursor', async () => {
    await emit(0, 'snapshot', 'live-1', snapshot([event('5', 5)]));
    await act(async () => latest.setFollowing(false));
    vi.mocked(fetchUsageEvents).mockResolvedValue({ events: [event('4', 4)], total_count: -1, page: 1, page_size: 50, total_pages: 0, has_more: false });
    await emit(0, 'append', 'live-2', { ...bounds, events: [event('6', 6), event('4', 4)] });
    await act(async () => { await latest.loadMore(); });
    expect(latest.events.map((item) => item.id)).toEqual(['5', '4']);
    expect(latest.pendingCount).toBe(1);
    await act(async () => latest.viewLatest());
    expect(latest.events.map((item) => item.id)).toEqual(['6', '5', '4']);

    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
    await act(async () => document.dispatchEvent(new Event('visibilitychange')));
    expect(latest.status).toBe('paused');
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'));
      window.dispatchEvent(new Event('online'));
      window.dispatchEvent(new Event('pageshow'));
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    expect(openUsageEventsLive).toHaveBeenCalledTimes(2);
    expect(vi.mocked(openUsageEventsLive).mock.calls[1][2]).toBe('live-2');
    await emit(1, 'append', 'live-3', { ...bounds, events: [event('7', 7), event('6', 6)] });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 300)); });
    expect(latest.events.map((item) => item.id)).toEqual(['7', '6', '5', '4']);
  });

  it('leaves late arrivals below the loaded history boundary to the cursor without opening a gap', async () => {
    await emit(0, 'snapshot', 'live-1', snapshot([event('5', 5), event('4', 4)]));
    await emit(0, 'append', 'live-2', { ...bounds, events: [event('1', 1), event('6', 6)] });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 300)); });
    expect(latest.events.map((item) => item.id)).toEqual(['6', '5', '4']);
    vi.mocked(fetchUsageEvents).mockResolvedValue({ events: [event('3', 3), event('2', 2), event('1', 1)], total_count: -1, page: 1, page_size: 50, total_pages: 0, has_more: false });
    await act(async () => { await latest.loadMore(); });
    expect(latest.events.map((item) => item.id)).toEqual(['6', '5', '4', '3', '2', '1']);
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
    await act(async () => document.dispatchEvent(new Event('visibilitychange')));
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
    await act(async () => { document.dispatchEvent(new Event('visibilitychange')); await new Promise((resolve) => setTimeout(resolve, 0)); });
    expect(vi.mocked(openUsageEventsLive).mock.calls[1][2]).toBe('live-2');
  });

  it('requires a fresh snapshot when a late append races an in-flight history page', async () => {
    await emit(0, 'snapshot', 'live-1', snapshot([event('5', 50), event('4', 40)]));
    await act(async () => latest.setFollowing(false));
    let resolveHistory!: (value: UsageEventsResponse) => void;
    let history!: Promise<void>;
    vi.mocked(fetchUsageEvents).mockImplementationOnce(() => new Promise((resolve) => { resolveHistory = resolve; }));
    await act(async () => { history = latest.loadMore(); });

    // 已加载记录与历史请求重叠时仍是正常去重，不应触发同步。
    await emit(0, 'append', 'live-2', { ...bounds, events: [event('4', 40)] });
    expect(latest.status).toBe('live');
    expect(latest.loadingMore).toBe(true);
    await emit(0, 'append', 'conflicting-batch', { ...bounds, events: [event('7', 55), event('6', 25)] });
    expect(latest.status).toBe('needs_sync');
    expect(latest.events.map((item) => item.id)).toEqual(['5', '4']);
    expect(latest.pendingCount).toBe(0);
    expect(latest.historyCursor).toBe('history-1');
    expect(latest.loadingMore).toBe(false);
    expect(vi.mocked(openUsageEventsLive).mock.calls[0][3].aborted).toBe(true);
    expect(vi.mocked(fetchUsageEvents).mock.calls[0][1]?.aborted).toBe(true);

    await act(async () => {
      resolveHistory({ events: [event('3', 30), event('2', 20)], total_count: -1, page: 1, page_size: 50, total_pages: 0, has_more: true, next_cursor: 'history-2' });
      await history;
    });
    expect(latest.events.map((item) => item.id)).toEqual(['5', '4']);
    expect(latest.historyCursor).toBe('history-1');
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
    await act(async () => document.dispatchEvent(new Event('visibilitychange')));
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
    await act(async () => document.dispatchEvent(new Event('visibilitychange')));
    expect(latest.status).toBe('needs_sync');
    await act(async () => { latest.viewLatest(); await new Promise((resolve) => setTimeout(resolve, 0)); });
    expect(vi.mocked(openUsageEventsLive).mock.calls[1][2]).toBe('');
    await emit(1, 'snapshot', 'fresh', snapshot([event('7', 55), event('5', 50), event('4', 40), event('6', 25)]));
    expect(latest.events.map((item) => item.id)).toEqual(['7', '5', '4', '6']);
    expect(latest.status).toBe('live');
  });

  it('pauses old pagination during refresh and ignores its late response after the fresh snapshot', async () => {
    await emit(0, 'snapshot', 'live-1', snapshot([event('5', 50), event('4', 40)]));
    vi.mocked(fetchUsageEvents).mockResolvedValueOnce({ events: [event('3', 30), event('2', 20)], total_count: -1, page: 1, page_size: 50, total_pages: 0, has_more: true, next_cursor: 'history-2' });
    await act(async () => { await latest.loadMore(); });
    let resolveHistory!: (value: UsageEventsResponse) => void;
    let history!: Promise<void>;
    vi.mocked(fetchUsageEvents).mockImplementationOnce(() => new Promise((resolve) => { resolveHistory = resolve; }));
    vi.mocked(fetchUsageEvents).mockResolvedValue({ events: [event('1', 10)], total_count: -1, page: 1, page_size: 50, total_pages: 0, has_more: false });
    await act(async () => { history = latest.loadMore(); });
    expect(vi.mocked(fetchUsageEvents).mock.calls[1][2]?.cursor).toBe('history-2');
    let done = false;
    await act(async () => { void latest.refresh().then(() => { done = true; }); await new Promise((resolve) => setTimeout(resolve, 0)); });
    expect(latest.events.map((item) => item.id)).toEqual(['5', '4', '3', '2']);
    expect(done).toBe(false);
    await act(async () => { await latest.loadMore(); });
    expect(fetchUsageEvents).toHaveBeenCalledTimes(2);
    expect(latest.loading).toBe(true);
    expect(vi.mocked(fetchUsageEvents).mock.calls[1][1]?.aborted).toBe(true);

    await emit(1, 'snapshot', 'fresh', { ...snapshot([event('5', 50), event('4', 40)]), next_cursor: 'history-4' });
    expect(done).toBe(true);
    expect(latest.loading).toBe(false);
    await act(async () => {
      resolveHistory({ events: [event('1', 10)], total_count: -1, page: 1, page_size: 50, total_pages: 0, has_more: false });
      await history;
    });
    expect(latest.events.map((item) => item.id)).toEqual(['5', '4']);
    expect(latest.historyCursor).toBe('history-4');
    expect(latest.loadingMore).toBe(false);
    await act(async () => { await latest.loadMore(); });
    expect(vi.mocked(fetchUsageEvents).mock.calls[2][2]?.cursor).toBe('history-4');
    expect(latest.events.map((item) => item.id)).toEqual(['5', '4', '1']);
  });

  it('pauses history while an automatic resync waits for its replacement snapshot', async () => {
    await emit(0, 'snapshot', 'live-1', snapshot([event('5', 5)]));
    await emit(0, 'append', 'overflow', { ...bounds, events: Array.from({ length: 2001 }, (_, index) => event(String(100 + index), 6)) });
    expect(latest.events.map((item) => item.id)).toEqual(['5']);
    expect(latest.loading).toBe(true);
    await act(async () => { await latest.loadMore(); });
    expect(fetchUsageEvents).not.toHaveBeenCalled();
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
    await emit(1, 'snapshot', 'fresh', snapshot([event('8', 8)]));
    expect(latest.loading).toBe(false);
    vi.mocked(fetchUsageEvents).mockResolvedValueOnce({ events: [event('7', 7)], total_count: -1, page: 1, page_size: 50, total_pages: 0, has_more: false });
    await act(async () => { await latest.loadMore(); });
    expect(latest.events.map((item) => item.id)).toEqual(['8', '7']);
  });

  it.each(['failure', 'range conflict', 'background pause', 'replacement'] as const)('releases snapshot loading after %s', async (outcome) => {
    await emit(0, 'snapshot', 'live-1', snapshot([event('5', 5)]));
    if (outcome === 'failure') vi.mocked(openUsageEventsLive).mockRejectedValueOnce(new Error('connection failed'));
    if (outcome === 'range conflict') {
      onRangeBoundsConflict.mockReturnValue(true);
      vi.mocked(openUsageEventsLive).mockRejectedValueOnce(new ApiError('range changed', 409));
    }
    let firstDone = false;
    await act(async () => { void latest.refresh().then(() => { firstDone = true; }); await new Promise((resolve) => setTimeout(resolve, 0)); });
    if (outcome === 'background pause') {
      Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
      await act(async () => document.dispatchEvent(new Event('visibilitychange')));
    } else if (outcome === 'replacement') {
      let secondDone = false;
      await act(async () => { void latest.refresh().then(() => { secondDone = true; }); await new Promise((resolve) => setTimeout(resolve, 0)); });
      expect(firstDone).toBe(true);
      expect(secondDone).toBe(false);
      expect(latest.loading).toBe(true);
      expect(vi.mocked(openUsageEventsLive).mock.calls[1][3].aborted).toBe(true);
      await emit(2, 'snapshot', 'fresh', snapshot([event('5', 5)]));
      expect(secondDone).toBe(true);
    }
    expect(firstDone).toBe(true);
    expect(latest.loading).toBe(false);
    expect(latest.events.map((item) => item.id)).toEqual(['5']);
    vi.mocked(fetchUsageEvents).mockResolvedValueOnce({ events: [event('4', 4)], total_count: -1, page: 1, page_size: 50, total_pages: 0, has_more: false });
    await act(async () => { await latest.loadMore(); });
    expect(latest.events.map((item) => item.id)).toEqual(['5', '4']);
    expect(latest.loadingMore).toBe(false);
  });

  it('aborts the actual connection when an invalid frame terminates the reader', async () => {
    await emit(0, 'snapshot', 'live-1', snapshot([event('5', 5)]));
    const signal = vi.mocked(openUsageEventsLive).mock.calls[0][3];
    await emit(0, 'append', 'bad-cursor', 'not an object');
    expect(signal.aborted).toBe(true);
    expect(latest.status).toBe('reconnecting');
    expect(latest.events.map((item) => item.id)).toEqual(['5']);
  });

  it.each(['ordinary', 'range conflict', 'unauthorized'] as const)('keeps history and pauses auto-load after a %s error', async (kind) => {
    await emit(0, 'snapshot', 'live-1', snapshot([event('5', 5)]));
    const error = kind === 'range conflict' ? new ApiError('range changed', 409)
      : kind === 'unauthorized' ? new ApiError('unauthorized', 401) : new Error('cursor failed');
    onRangeBoundsConflict.mockReturnValue(kind === 'range conflict');
    vi.mocked(fetchUsageEvents).mockRejectedValueOnce(error);
    await act(async () => { await latest.loadMore(); });
    expect(latest.autoLoadMore).toBe(false);
    expect(latest.events.map((item) => item.id)).toEqual(['5']);
    expect(latest.historyCursor).toBe('history-1');
    expect(onRangeBoundsConflict).toHaveBeenCalledWith(error);
    expect(onAuthRequired).toHaveBeenCalledTimes(kind === 'unauthorized' ? 1 : 0);
    expect(latest.error).toBe(kind === 'ordinary' ? 'cursor failed' : '');
  });

  it('halts on deep-reading overflow and a manual refresh resolves on snapshot without closing the stream', async () => {
    await emit(0, 'snapshot', 'live-1', snapshot([event('5', 5)]));
    await act(async () => latest.setFollowing(false));
    await emit(0, 'append', 'unaccepted', { ...bounds, events: Array.from({ length: 1001 }, (_, index) => event(String(100 + index), 6)) });
    expect(latest.status).toBe('needs_sync');
    expect(latest.events.map((item) => item.id)).toEqual(['5']);
    let done = false;
    await act(async () => { void latest.refresh().then(() => { done = true; }); await new Promise((resolve) => setTimeout(resolve, 0)); });
    expect(done).toBe(false);
    expect(latest.events.map((item) => item.id)).toEqual(['5']);
    await emit(1, 'snapshot', 'fresh', snapshot([event('8', 8)]));
    expect(done).toBe(true);
    expect(latest.events.map((item) => item.id)).toEqual(['8']);
    expect(latest.status).toBe('live');
  });
});
