import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError, fetchUsageEvents, isCostsBusy, openUsageEventsLive, type FetchUsageEventsOptions } from '@/lib/api';
import type { UsageEvent, UsageRangeRequest } from '@/lib/types';
import { readRequestEventsSSE, type RequestEventsSSEFrame } from './requestEventsSSE';

const FLUSH_INTERVAL_MS = 250;
const INACTIVE_TIMEOUT_MS = 45_000;

type Bounds = { range_start: string; range_end: string; end_exclusive: boolean };
type FeedState = {
  scope: string; events: UsageEvent[]; totalCount: number; historyCursor: string | null;
  loading: boolean; loadingMore: boolean; autoLoadMore: boolean; error: string;
  mode: 'latest' | 'reading' | 'restoring' | 'refreshing';
  atTop: boolean; bounds: Bounds | null; mayHaveMore: boolean; token: object;
};
type FeedOptions = {
  enabled: boolean;
  request: UsageRangeRequest;
  options: FetchUsageEventsOptions;
  onAuthRequired?: () => void;
  onRangeBoundsConflict?: (error: unknown) => boolean;
};
const emptyState = (scope: string): FeedState => ({
  scope, events: [], totalCount: 0, historyCursor: null, loading: true, loadingMore: false,
  autoLoadMore: true, error: '', mode: 'refreshing', atTop: true, bounds: null, mayHaveMore: false, token: {},
});

// 时间戳按 SQLite 的存储文本比较，ID 始终以十进制字符串比较，不能经过 Date 或 Number。
function compareIDs(left: string, right: string): number {
  const a = left.replace(/^0+/, '') || '0';
  const b = right.replace(/^0+/, '') || '0';
  return a.length - b.length || (a < b ? -1 : a > b ? 1 : 0);
}

export function mergeRequestEvents(current: readonly UsageEvent[], incoming: readonly UsageEvent[]): UsageEvent[] {
  const seen = new Set(current.map((event) => event.id));
  const merged = [...current];
  for (const event of incoming) {
    if (seen.has(event.id)) continue;
    seen.add(event.id);
    merged.push(event);
  }
  return merged.sort((a, b) => (
    a.timestamp === b.timestamp ? compareIDs(b.id!, a.id!) : a.timestamp < b.timestamp ? 1 : -1
  ));
}

function appendHistory(current: readonly UsageEvent[], incoming: readonly UsageEvent[]): UsageEvent[] {
  const seen = new Set(current.map((event) => event.id));
  const result = [...current];
  for (const event of incoming) {
    if (!seen.has(event.id)) { seen.add(event.id); result.push(event); }
  }
  return result;
}

function eventCursor(event: UsageEvent): string {
  return btoa(`${event.timestamp}|${event.id}`).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

function insideBounds(event: UsageEvent, bounds: Bounds): boolean {
  return event.timestamp >= bounds.range_start && (bounds.end_exclusive ? event.timestamp < bounds.range_end : event.timestamp <= bounds.range_end);
}

function framePayload(frame: RequestEventsSSEFrame): Record<string, unknown> {
  const value: unknown = JSON.parse(frame.data);
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Invalid live event payload');
  return value as Record<string, unknown>;
}

function readBounds(payload: Record<string, unknown>): Bounds {
  if (typeof payload.range_start !== 'string' || typeof payload.range_end !== 'string' || typeof payload.end_exclusive !== 'boolean') {
    throw new Error('Invalid live event range');
  }
  return { range_start: payload.range_start, range_end: payload.range_end, end_exclusive: payload.end_exclusive };
}

function readEvents(payload: Record<string, unknown>): UsageEvent[] {
  if (!Array.isArray(payload.events) || payload.events.some((event: unknown) => (
    !event || typeof event !== 'object' || typeof (event as UsageEvent).id !== 'string' || !(event as UsageEvent).id || typeof (event as UsageEvent).timestamp !== 'string'
  ))) throw new Error('Invalid live event records');
  return payload.events as UsageEvent[];
}

function withBoundary(state: FeedState): FeedState {
  const last = state.events.at(-1);
  return { ...state, historyCursor: state.mayHaveMore && last ? eventCursor(last) : null };
}

function prune(state: FeedState): FeedState {
  const { bounds, events } = state;
  if (!bounds) return state;
  const kept = events.filter((event) => insideBounds(event, bounds));
  return withBoundary({ ...state, events: kept, mayHaveMore: state.mayHaveMore && (!events.length || kept.includes(events.at(-1)!)) });
}

function wantsStream(state: FeedState): boolean {
  return state.mode === 'refreshing' || (state.mode !== 'reading' && state.atTop);
}

export function useRequestEventsFeed({ enabled, request, options, onAuthRequired, onRangeBoundsConflict }: FeedOptions) {
  const scope = JSON.stringify([request, options]);
  const [state, setState] = useState(() => emptyState(scope));
  const [visible, setVisible] = useState(() => document.visibilityState !== 'hidden');
  const callbacksRef = useRef({ onAuthRequired, onRangeBoundsConflict });
  callbacksRef.current = { onAuthRequired, onRangeBoundsConflict };
  const liveRef = useRef<AbortController | null>(null);
  const historyRef = useRef<AbortController | null>(null);
  const attemptsRef = useRef(0);
  const refreshRef = useRef<{ token: object; resolve: () => void } | null>(null);
  const settleRefresh = useCallback((token?: object) => {
    if (refreshRef.current && (!token || refreshRef.current.token === token)) {
      refreshRef.current.resolve();
      refreshRef.current = null;
    }
  }, []);
  const abortHistory = useCallback(() => { historyRef.current?.abort(); historyRef.current = null; }, []);

  useEffect(() => {
    attemptsRef.current = 0;
    setState((current) => enabled && current.scope === scope ? current : emptyState(scope));
    return () => { abortHistory(); settleRefresh(); };
  }, [enabled, scope, abortHistory, settleRefresh]);

  useEffect(() => {
    const resume = () => {
      if (document.visibilityState === 'hidden' || liveRef.current) return;
      setState((current) => (current.mode === 'latest' || current.mode === 'refreshing') && current.atTop
        ? { ...current, token: {} } : current);
    };
    const visibility = () => {
      const shown = document.visibilityState !== 'hidden';
      setVisible(shown);
      if (!shown) setState((current) => ({
        ...current, token: {}, mode: current.mode === 'restoring' ? 'reading' : current.mode === 'refreshing' ? 'latest' : current.mode, loading: false,
      }));
    };
    visibility();
    document.addEventListener('visibilitychange', visibility);
    window.addEventListener('online', resume);
    window.addEventListener('pageshow', resume);
    return () => {
      document.removeEventListener('visibilitychange', visibility);
      window.removeEventListener('online', resume);
      window.removeEventListener('pageshow', resume);
    };
  }, []);

  const { token } = state;
  const connect = enabled && state.scope === scope && visible && wantsStream(state);
  useEffect(() => {
    if (!connect) { settleRefresh(token); return; }
    abortHistory();
    const controller = new AbortController();
    liveRef.current = controller;
    const pageSize = options.pageSize ?? 50;
    let disposed = false;
    let receivedSnapshot = false;
    let timedOut = false;
    let pending: UsageEvent[] = [];
    let retryTimer: ReturnType<typeof setTimeout> | undefined;
    let flushTimer: ReturnType<typeof setTimeout> | undefined;
    let watchdog: ReturnType<typeof setTimeout> | undefined;
    const update = (change: (current: FeedState) => FeedState) => setState((current) => (
      !disposed && current.scope === scope && current.token === token && wantsStream(current) ? change(current) : current
    ));
    const touch = () => {
      if (disposed) return;
      clearTimeout(watchdog);
      watchdog = setTimeout(() => { timedOut = true; controller.abort(); }, INACTIVE_TIMEOUT_MS);
    };
    const receive = (frame: RequestEventsSSEFrame) => {
      if (disposed || controller.signal.aborted) return;
      const payload = framePayload(frame);
      if (frame.event === 'reset') {
        update((current) => receivedSnapshot && current.mode === 'latest' && current.atTop
          ? { ...current, token: {} }
          : { ...current, mode: current.mode === 'restoring' ? 'reading' : 'latest', loading: false });
        settleRefresh(token);
        controller.abort();
        return;
      }
      if (frame.event === 'busy') return;
      if (!['snapshot', 'append', 'checkpoint', 'heartbeat'].includes(frame.event)) return;
      const bounds = readBounds(payload);
      if (frame.event === 'snapshot') {
        const events = mergeRequestEvents([], readEvents(payload)).slice(0, pageSize);
        if (typeof payload.total_count !== 'number') throw new Error('Invalid live snapshot');
        const totalCount = payload.total_count;
        receivedSnapshot = true;
        attemptsRef.current = 0;
        update((current) => ({
          ...current, events, bounds, totalCount, atTop: !events.length || current.atTop, mode: 'latest',
          mayHaveMore: payload.has_more === true,
          historyCursor: payload.has_more === true && typeof payload.next_cursor === 'string' ? payload.next_cursor : null,
          loading: false, loadingMore: false, autoLoadMore: true, error: '',
        }));
        settleRefresh(token);
        return;
      }
      if (frame.event !== 'heartbeat' && !receivedSnapshot) throw new Error('Live event arrived before snapshot');
      const events = frame.event === 'append' ? readEvents(payload) : null;
      attemptsRef.current = 0;
      update((current) => {
        if (current.mode !== 'latest' || !current.atTop) return current;
        const next = {
          ...current, bounds, error: '',
          totalCount: frame.event === 'heartbeat' && typeof payload.total_count === 'number' ? payload.total_count : current.totalCount,
        };
        return events ? next : prune(next);
      });
      if (events) {
        // 只缓存本连接的首屏候选；离开最新模式时由effect cleanup丢弃。
        pending = mergeRequestEvents(pending, events).slice(0, pageSize + 1);
        if (flushTimer === undefined) flushTimer = setTimeout(() => {
          flushTimer = undefined;
          const batch = pending;
          pending = [];
          update((current) => {
            if (current.mode !== 'latest' || !current.atTop) return current;
            const pruned = prune(current);
            const merged = mergeRequestEvents(pruned.events, batch.filter((event) => !pruned.bounds || insideBounds(event, pruned.bounds)));
            return withBoundary({ ...pruned, events: merged.slice(0, pageSize), mayHaveMore: pruned.mayHaveMore || merged.length > pageSize });
          });
        }, FLUSH_INTERVAL_MS);
      }
    };
    update((current) => ({ ...current, loading: true }));
    void (async () => {
      try {
        touch();
        const response = await openUsageEventsLive(request, options, controller.signal);
        if (disposed) return;
        await readRequestEventsSSE(response.body!, receive, touch);
        if (!disposed && !controller.signal.aborted) throw new Error('Live stream disconnected');
      } catch (error) {
        if (disposed || (controller.signal.aborted && !timedOut)) return;
        const conflict = callbacksRef.current.onRangeBoundsConflict?.(error);
        const unauthorized = error instanceof ApiError && [401, 403].includes(error.status);
        if (!conflict && unauthorized) callbacksRef.current.onAuthRequired?.();
        const handled = conflict || unauthorized;
        const terminal = handled || (error instanceof ApiError && [400, 404, 405, 409, 501].includes(error.status));
        update((current) => ({
          ...current, loading: !terminal && current.mode === 'restoring',
          mode: terminal && current.mode === 'restoring' ? 'reading' : current.mode,
          error: handled ? current.error : isCostsBusy(error) ? 'COSTS_BUSY'
            : terminal || !receivedSnapshot ? error instanceof Error ? error.message : 'Failed to load usage events' : '',
        }));
        settleRefresh(token);
        if (!terminal) {
          const delay = error instanceof ApiError && error.status === 429 && error.retryAfterMs
            ? error.retryAfterMs
            : Math.min(30_000, 1000 * 2 ** Math.min(attemptsRef.current++, 5)) * (0.8 + Math.random() * 0.4);
          retryTimer = setTimeout(() => update((current) => ({ ...current, token: {} })), delay);
        }
      } finally {
        clearTimeout(watchdog);
        controller.abort();
        if (liveRef.current === controller) liveRef.current = null;
      }
    })();
    return () => {
      disposed = true;
      controller.abort();
      if (liveRef.current === controller) liveRef.current = null;
      clearTimeout(watchdog);
      clearTimeout(retryTimer);
      clearTimeout(flushTimer);
      settleRefresh(token);
    };
    // scope代表全部请求参数；数据更新不重建连接，只有请求身份/连接条件变化才替换effect。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connect, scope, token, abortHistory, settleRefresh]);

  const startReading = useCallback(() => {
    settleRefresh();
    setState((current) => ({ ...current, mode: 'reading', loading: false }));
  }, [settleRefresh]);
  const setAtTop = useCallback((atTop: boolean) => setState((current) => current.atTop === atTop ? current : ({
    ...current, atTop, token: {}, loading: atTop && current.loading,
    mode: !atTop && current.mode === 'restoring' ? 'reading' : !atTop && current.mode === 'refreshing' ? 'latest' : current.mode,
  })), []);
  const returnToTop = useCallback(() => setState((current) => (
    current.mode === 'reading' && current.atTop && document.visibilityState !== 'hidden'
      ? { ...current, mode: 'restoring', token: {}, loading: true, loadingMore: false } : current
  )), []);
  const refresh = useCallback(() => new Promise<void>((resolve) => {
    if (!enabled) { resolve(); return; }
    settleRefresh();
    abortHistory();
    const token = {};
    refreshRef.current = { token, resolve };
    setState((current) => ({
      ...(current.scope === scope ? current : emptyState(scope)), token,
      mode: document.visibilityState === 'hidden' ? 'latest' : 'refreshing', loading: document.visibilityState !== 'hidden', loadingMore: false, error: '',
    }));
  }), [enabled, scope, abortHistory, settleRefresh]);
  const loadMore = useCallback(async () => {
    if (!enabled || state.scope !== scope) return;
    startReading();
    if (!state.historyCursor || historyRef.current) return;
    const controller = new AbortController();
    historyRef.current = controller;
    setState((current) => ({ ...current, loadingMore: true, error: '' }));
    try {
      const response = await fetchUsageEvents(request, controller.signal, { ...options, cursor: state.historyCursor });
      if (historyRef.current !== controller) return;
      setState((current) => current.scope === scope && current.mode === 'reading' ? {
        ...current, events: appendHistory(current.events, response.events), autoLoadMore: true,
        historyCursor: response.has_more === true ? response.next_cursor?.trim() || null : null,
      } : current);
    } catch (error) {
      if (controller.signal.aborted) return;
      let message = '';
      if (!callbacksRef.current.onRangeBoundsConflict?.(error)) {
        if (error instanceof ApiError && [401, 403].includes(error.status)) callbacksRef.current.onAuthRequired?.();
        else message = isCostsBusy(error) ? 'COSTS_BUSY' : error instanceof Error ? error.message : 'Failed to load more usage events';
      }
      setState((current) => current.scope === scope ? { ...current, autoLoadMore: false, error: message } : current);
    } finally {
      if (historyRef.current === controller) {
        historyRef.current = null;
        setState((current) => current.scope === scope ? { ...current, loadingMore: false } : current);
      }
    }
    // scope代表request/options，游标来自当前画面；分页不管理SSE连接。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, scope, state.scope, state.historyCursor, startReading]);

  const current = state.scope === scope ? state : emptyState(scope);
  return { ...current, reading: current.mode === 'reading' || current.mode === 'restoring', restoring: current.mode === 'restoring', refresh, loadMore, setAtTop, startReading, returnToTop };
}
