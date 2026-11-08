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
const event = (id: string, timestamp = '2026-06-01T00:00:00.900000001Z'): UsageEvent => ({ id, timestamp }) as UsageEvent;
const snapshot = (events: UsageEvent[], hasMore = true, total = 150) => ({ ...bounds, events, total_count: total, has_more: hasMore, next_cursor: hasMore ? 'snapshot-cursor' : '' });
const page = (events: UsageEvent[], next?: string): UsageEventsResponse => ({ events, total_count: -1, page: 1, page_size: 50, total_pages: 0, has_more: Boolean(next), next_cursor: next });
const wait = () => new Promise((resolve) => setTimeout(resolve, 0));
let streams: ReadableStreamDefaultController<Uint8Array>[];
let latest: ReturnType<typeof useRequestEventsFeed>;
const onAuthRequired = vi.fn();
const onRangeBoundsConflict = vi.fn((_error: unknown) => false);

function Harness({ model = '' }: { model?: string }) {
  const feed = useRequestEventsFeed({ enabled: true, request: { range: 'today' }, options: { cursorMode: true, pageSize: 50, model }, onAuthRequired, onRangeBoundsConflict });
  useEffect(() => { latest = feed; });
  return <div>{feed.events.map((item) => item.id).join(',')}</div>;
}
async function emit(index: number, kind: string, id: string, data: unknown) {
  await act(async () => {
    streams[index].enqueue(encoder.encode(`id: ${id}\nevent: ${kind}\ndata: ${JSON.stringify(data)}\n\n`));
    await wait();
  });
}
function decodeCursor(cursor: string | null) {
  return atob((cursor ?? '').replace(/-/g, '+').replace(/_/g, '/'));
}

describe('useRequestEventsFeed', () => {
  let root: Root;
  let container: HTMLDivElement;
  beforeEach(async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    streams = [];
    onAuthRequired.mockReset();
    onRangeBoundsConflict.mockReset().mockReturnValue(false);
    // 故意不在 mock abort 时关闭流，确保迟到帧也无法通过请求身份检查。
    vi.mocked(openUsageEventsLive).mockReset().mockImplementation(async () => {
      const body = new ReadableStream<Uint8Array>({ start(controller) { streams.push(controller); } });
      return new Response(body, { headers: { 'Content-Type': 'text/event-stream' } });
    });
    vi.mocked(fetchUsageEvents).mockReset();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    await act(async () => { root.render(<Harness />); await wait(); });
  });
  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
    vi.restoreAllMocks();
  });

  it('keeps 50 unique latest rows in SQL text order and pages from the actual boundary (UTC and int64)', async () => {
    const rows = Array.from({ length: 50 }, (_, i) => event(String(9007199254741041n - BigInt(i))));
    await emit(0, 'snapshot', 's1', snapshot(rows));
    // RFC3339Nano 的 Z 在文本排序中大于小数点，即使实际瞬间更早。
    const utcLate = event('9007199254741052', '2026-06-01T00:00:00Z');
    await emit(0, 'append', 's2', { ...bounds, events: [utcLate, rows[0], utcLate] });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 300)); });
    expect(latest.events).toHaveLength(50);
    expect(latest.events[0].id).toBe(utcLate.id);
    expect(latest.events.filter((row) => row.id === utcLate.id)).toHaveLength(1);
    const next = event('9007199254741053');
    await emit(0, 'append', 's3', { ...bounds, events: [next, utcLate] });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 300)); });
    expect(latest.events).toHaveLength(50);
    expect(latest.events.slice(0, 2).map((row) => row.id)).toEqual([utcLate.id, next.id]);
    const last = latest.events[49];
    expect(decodeCursor(latest.historyCursor)).toBe(`${last.timestamp}|${last.id}`);
    vi.mocked(fetchUsageEvents).mockResolvedValueOnce(page([rows[48], rows[49]]));
    await act(async () => { await latest.loadMore(); });
    expect(decodeCursor(vi.mocked(fetchUsageEvents).mock.calls[0][2]?.cursor ?? null)).toBe(`${last.timestamp}|${last.id}`);
    expect(latest.events.slice(-2).map((row) => row.id)).toEqual([rows[48].id, rows[49].id]);
  });

  it('keeps pagination available when a late event falls below an initially complete first page', async () => {
    const rows = Array.from({ length: 50 }, (_, i) => event(String(100 - i)));
    await emit(0, 'snapshot', 's1', snapshot(rows, false, 50));
    await emit(0, 'append', 's2', { ...bounds, events: [event('1')] });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 300)); });
    expect(latest.events).toHaveLength(50);
    expect(decodeCursor(latest.historyCursor)).toBe(`${rows[49].timestamp}|${rows[49].id}`);
    vi.mocked(fetchUsageEvents).mockResolvedValueOnce(page([event('1')]));
    await act(async () => { await latest.loadMore(); });
    expect(latest.events.at(-1)?.id).toBe('1');
  });

  it('closes the stream as soon as reading starts, discards queued data and appends history without rearranging', async () => {
    await emit(0, 'snapshot', 's1', snapshot([event('5'), event('4')]));
    await emit(0, 'append', 's2', { ...bounds, events: [event('6')] });
    await act(async () => { latest.setAtTop(false); latest.startReading(); });
    expect(vi.mocked(openUsageEventsLive).mock.calls[0][3].aborted).toBe(true);
    vi.mocked(fetchUsageEvents).mockResolvedValueOnce(page([event('3')]));
    await emit(0, 'heartbeat', '', { ...bounds, total_count: 0 });
    await act(async () => { await latest.loadMore(); await new Promise((resolve) => setTimeout(resolve, 300)); });
    expect(latest.events.map((row) => row.id)).toEqual(['5', '4', '3']);
    expect(latest.totalCount).toBe(150);
  });

  it('cancels restoration on renewed reading, keeps the old cursor and ignores a late snapshot', async () => {
    await emit(0, 'snapshot', 's1', snapshot([event('5'), event('4')]));
    vi.mocked(fetchUsageEvents).mockResolvedValueOnce(page([event('3')], 'history-2'));
    await act(async () => { await latest.loadMore(); });
    let resolveOldHistory!: (value: UsageEventsResponse) => void;
    let oldHistory!: Promise<void>;
    vi.mocked(fetchUsageEvents).mockImplementationOnce(() => new Promise((resolve) => { resolveOldHistory = resolve; }));
    await act(async () => { oldHistory = latest.loadMore(); });
    await act(async () => { latest.setAtTop(true); latest.returnToTop(); await wait(); });
    expect(vi.mocked(fetchUsageEvents).mock.calls[1][1]?.aborted).toBe(true);
    expect(latest.events.map((row) => row.id)).toEqual(['5', '4', '3']);
    expect(vi.mocked(openUsageEventsLive).mock.calls[1][2]).toBe('');
    await act(async () => { latest.setAtTop(false); latest.startReading(); });
    await emit(1, 'snapshot', 'too-late', snapshot([event('9')]));
    await act(async () => { resolveOldHistory(page([event('1')])); await oldHistory; });
    expect(latest.events.map((row) => row.id)).toEqual(['5', '4', '3']);
    expect(latest.historyCursor).toBe('history-2');
    vi.mocked(fetchUsageEvents).mockResolvedValueOnce(page([event('2')]));
    await act(async () => { await latest.loadMore(); });
    expect(latest.events.map((row) => row.id)).toEqual(['5', '4', '3', '2']);
    expect(vi.mocked(fetchUsageEvents).mock.calls[2][2]?.cursor).toBe('history-2');
    await act(async () => { latest.setAtTop(true); latest.returnToTop(); await wait(); });
    await emit(2, 'snapshot', 'fresh', snapshot([event('10'), event('9')]));
    expect(latest.events.map((row) => row.id)).toEqual(['10', '9']);
    expect(latest.historyCursor).toBe('snapshot-cursor');
  });

  it('keeps live updates after a non-top reading view is manually refreshed to an empty snapshot', async () => {
    await emit(0, 'snapshot', 's1', snapshot([event('5')]));
    await act(async () => { latest.setAtTop(false); latest.startReading(); });
    let done = false;
    await act(async () => { void latest.refresh().then(() => { done = true; }); await wait(); });
    await emit(1, 'snapshot', 'empty', snapshot([], false, 0));
    expect(done).toBe(true);
    expect(latest.events).toEqual([]);
    expect(vi.mocked(openUsageEventsLive).mock.calls[1][3].aborted).toBe(false);
    await emit(1, 'append', 'new', { ...bounds, events: [event('6')] });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 300)); });
    expect(latest.events.map((row) => row.id)).toEqual(['6']);
  });

  it('manual refresh aborts in-flight history and ignores the old response', async () => {
    await emit(0, 'snapshot', 's1', snapshot([event('5')]));
    let resolveHistory!: (value: UsageEventsResponse) => void;
    vi.mocked(fetchUsageEvents).mockImplementationOnce(() => new Promise((resolve) => { resolveHistory = resolve; }));
    let history!: Promise<void>;
    await act(async () => { history = latest.loadMore(); });
    let done = false;
    await act(async () => { void latest.refresh().then(() => { done = true; }); await wait(); });
    expect(vi.mocked(fetchUsageEvents).mock.calls[0][1]?.aborted).toBe(true);
    expect(latest.events.map((row) => row.id)).toEqual(['5']);
    await emit(1, 'snapshot', 'fresh', snapshot([event('7')]));
    await act(async () => { resolveHistory(page([event('4')])); await history; });
    expect(done).toBe(true);
    expect(latest.events.map((row) => row.id)).toEqual(['7']);
  });

  it('reconnects in latest mode, but never on visibility or network recovery while reading', async () => {
    await emit(0, 'snapshot', 's1', snapshot([event('5')]));
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
    await act(async () => document.dispatchEvent(new Event('visibilitychange')));
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
    await act(async () => { document.dispatchEvent(new Event('visibilitychange')); window.dispatchEvent(new Event('online')); await wait(); });
    expect(vi.mocked(openUsageEventsLive).mock.calls[1][2]).toBe('');
    await emit(1, 'snapshot', 's2', snapshot([event('6')]));
    await act(async () => { latest.startReading(); });
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
    await act(async () => document.dispatchEvent(new Event('visibilitychange')));
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
    await act(async () => { document.dispatchEvent(new Event('visibilitychange')); window.dispatchEvent(new Event('online')); window.dispatchEvent(new Event('pageshow')); await wait(); });
    expect(openUsageEventsLive).toHaveBeenCalledTimes(2);
    expect(latest.events.map((row) => row.id)).toEqual(['6']);
  });

  it('keeps the history cursor when load more cancels an in-flight return snapshot', async () => {
    await emit(0, 'snapshot', 's1', snapshot([event('5')]));
    vi.mocked(fetchUsageEvents).mockResolvedValueOnce(page([event('4')], 'history-2'));
    await act(async () => { await latest.loadMore(); });
    await act(async () => { latest.returnToTop(); await wait(); });
    expect(latest.restoring).toBe(true);
    vi.mocked(fetchUsageEvents).mockResolvedValueOnce(page([event('3')]));
    await act(async () => { await latest.loadMore(); });
    expect(vi.mocked(openUsageEventsLive).mock.calls[1][3].aborted).toBe(true);
    expect(latest.restoring).toBe(false);
    await emit(1, 'snapshot', 'too-late', snapshot([event('9')]));
    expect(latest.events.map((row) => row.id)).toEqual(['5', '4', '3']);
    expect(vi.mocked(fetchUsageEvents).mock.calls[1][2]?.cursor).toBe('history-2');
  });

  it('retries a failed return snapshot while preserving history and the option to cancel', async () => {
    await emit(0, 'snapshot', 's1', snapshot([event('5')]));
    vi.mocked(fetchUsageEvents).mockResolvedValueOnce(page([event('4')], 'history-2'));
    await act(async () => { await latest.loadMore(); });
    vi.spyOn(Math, 'random').mockReturnValue(0.5);
    vi.mocked(openUsageEventsLive).mockRejectedValueOnce(new Error('temporary failure'));
    await act(async () => { latest.returnToTop(); await wait(); });
    expect(latest.events.map((row) => row.id)).toEqual(['5', '4']);
    expect(latest.restoring).toBe(true);
    expect(latest.loading).toBe(true);
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 1050)); });
    expect(openUsageEventsLive).toHaveBeenCalledTimes(3);
    await emit(1, 'snapshot', 'fresh', snapshot([event('9')]));
    expect(latest.restoring).toBe(false);
    expect(latest.events.map((row) => row.id)).toEqual(['9']);
  });

  it('retires the old stream and page on a filter change', async () => {
    await emit(0, 'snapshot', 's1', snapshot([event('5')]));
    await act(async () => { root.render(<Harness model="other" />); await wait(); });
    expect(vi.mocked(openUsageEventsLive).mock.calls[0][3].aborted).toBe(true);
    await emit(0, 'append', 'old', { ...bounds, events: [event('9')] });
    await emit(1, 'snapshot', 'new', snapshot([event('2')]));
    expect(latest.events.map((row) => row.id)).toEqual(['2']);
    expect(vi.mocked(openUsageEventsLive).mock.calls[1][1].model).toBe('other');
  });

  it('reports an initial 500 until the existing retry receives a snapshot', async () => {
    vi.spyOn(Math, 'random').mockReturnValue(0.5);
    const error = new ApiError('live database failure', 500);
    await act(async () => { streams[0].error(error); await wait(); });
    expect(latest.loading).toBe(false);
    expect(latest.error).toBe('live database failure');
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 1050)); });
    expect(openUsageEventsLive).toHaveBeenCalledTimes(2);
    await emit(1, 'snapshot', 'fresh', snapshot([event('5')]));
    expect(latest.events.map((row) => row.id)).toEqual(['5']);
    expect(latest.error).toBe('');
  });

  it('aborts the connection on malformed frames without losing the visible page', async () => {
    await emit(0, 'snapshot', 's1', snapshot([event('5')]));
    await emit(0, 'append', 'bad', 'not an object');
    expect(vi.mocked(openUsageEventsLive).mock.calls[0][3].aborted).toBe(true);
    expect(latest.events.map((row) => row.id)).toEqual(['5']);
  });

  it.each(['range conflict', 'unauthorized'] as const)('keeps history on a %s error', async (kind) => {
    await emit(0, 'snapshot', 's1', snapshot([event('5')]));
    const error = kind === 'range conflict' ? new ApiError('range changed', 409) : new ApiError('unauthorized', 401);
    onRangeBoundsConflict.mockReturnValue(kind === 'range conflict');
    vi.mocked(fetchUsageEvents).mockRejectedValueOnce(error);
    await act(async () => { await latest.loadMore(); });
    expect(latest.events.map((row) => row.id)).toEqual(['5']);
    expect(latest.historyCursor).toBe('snapshot-cursor');
    expect(latest.autoLoadMore).toBe(false);
    expect(onAuthRequired).toHaveBeenCalledTimes(kind === 'unauthorized' ? 1 : 0);
  });

  it('updates the last authoritative count from heartbeats and keeps history errors visible', async () => {
    await emit(0, 'snapshot', 's1', snapshot([event('5')], true, 1));
    await emit(0, 'append', 's2', { ...bounds, events: [event('6')] });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 300)); });
    expect(latest.events.map((row) => row.id)).toEqual(['6', '5']);
    expect(latest.totalCount).toBe(1);
    await emit(0, 'heartbeat', '', { ...bounds, total_count: 2 });
    expect(latest.totalCount).toBe(2);
    // 保留清理后的最新权威值不伪造，旧行也不因此被强制清空。
    await emit(0, 'heartbeat', '', { ...bounds, total_count: 1 });
    expect(latest.totalCount).toBe(1);
    expect(latest.events.map((row) => row.id)).toEqual(['6', '5']);
    vi.mocked(fetchUsageEvents).mockRejectedValueOnce(new ApiError('cursor failed', 500));
    await act(async () => { await latest.loadMore(); });
    expect(latest.autoLoadMore).toBe(false);
    expect(latest.error).toBe('cursor failed');
  });
});
