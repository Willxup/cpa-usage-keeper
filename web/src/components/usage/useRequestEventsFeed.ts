import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError, fetchUsageEvents, isCostsBusy, openUsageEventsLive, type FetchUsageEventsOptions } from '@/lib/api';
import type { UsageEvent, UsageEventsResponse, UsageRangeRequest } from '@/lib/types';
import { readRequestEventsSSE, type RequestEventsSSEFrame } from './requestEventsSSE';

const FLUSH_INTERVAL_MS = 250;
const INACTIVE_TIMEOUT_MS = 45_000;

type Bounds = { range_start: string; range_end: string; end_exclusive: boolean };
type View = {
  scope: string; events: UsageEvent[]; totalCount: number; historyCursor: string | null;
  loading: boolean; loadingMore: boolean; autoLoadMore: boolean; reading: boolean; restoring: boolean; error: string;
};
type FeedOptions = {
  enabled: boolean;
  request: UsageRangeRequest;
  options: FetchUsageEventsOptions;
  onAuthRequired?: () => void;
  onRangeBoundsConflict?: (error: unknown) => boolean;
};
type FeedControls = {
  refresh: () => Promise<void>;
  loadMore: () => Promise<void>;
  setAtTop: (atTop: boolean) => void;
  startReading: () => void;
  returnToTop: () => void;
};

const emptyView = (scope: string): View => ({
  scope, events: [], totalCount: 0, historyCursor: null, loading: true, loadingMore: false,
  autoLoadMore: true, reading: false, restoring: false, error: '',
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

export function useRequestEventsFeed({ enabled, request, options, onAuthRequired, onRangeBoundsConflict }: FeedOptions) {
  const scope = JSON.stringify([request, options]);
  const [view, setView] = useState<View>(() => emptyView(scope));
  const callbacksRef = useRef({ onAuthRequired, onRangeBoundsConflict });
  callbacksRef.current = { onAuthRequired, onRangeBoundsConflict };
  const controlsRef = useRef<FeedControls | null>(null);

  useEffect(() => {
    if (!enabled) { controlsRef.current = null; return; }
    let active = true;
    const state = emptyView(scope);
    const pageSize = options.pageSize ?? 50;
    let atTop = true;
    let following = true;
    let restoring = false;
    let mayHaveMore = false;
    let bounds: Bounds | null = null;
    let pending: UsageEvent[] = [];
    let controller: AbortController | null = null;
    let historyController: AbortController | null = null;
    let generation = 0;
    let attempts = 0;
    let retryTimer: ReturnType<typeof setTimeout> | undefined;
    let flushTimer: ReturnType<typeof setTimeout> | undefined;
    let refreshDone: (() => void) | undefined;
    const publish = () => { if (active) setView({ ...state }); };
    const settleRefresh = () => { refreshDone?.(); refreshDone = undefined; };
    const clearRetry = () => { if (retryTimer !== undefined) clearTimeout(retryTimer); retryTimer = undefined; };
    const clearPending = () => {
      if (flushTimer !== undefined) clearTimeout(flushTimer);
      flushTimer = undefined;
      pending = [];
    };
    const stopConnection = () => {
      generation++;
      controller?.abort();
      controller = null;
      clearRetry();
      clearPending();
    };
    const stopHistory = () => {
      historyController?.abort();
      historyController = null;
      state.loadingMore = false;
    };
    const updateBoundary = () => {
      const last = state.events.at(-1);
      state.historyCursor = mayHaveMore && last ? eventCursor(last) : null;
    };
    const prune = () => {
      if (!bounds) return;
      const last = state.events.at(-1);
      state.events = state.events.filter((event) => insideBounds(event, bounds!));
      if (last && !state.events.includes(last)) mayHaveMore = false;
    };
    const flush = () => {
      flushTimer = undefined;
      if (!following || !atTop) { pending = []; return; }
      prune();
      const merged = mergeRequestEvents(state.events, pending.filter((event) => !bounds || insideBounds(event, bounds)));
      if (merged.length > pageSize) mayHaveMore = true;
      state.events = merged.slice(0, pageSize);
      pending = [];
      updateBoundary();
      publish();
    };
    const scheduleFlush = () => {
      if (flushTimer === undefined) flushTimer = setTimeout(flush, FLUSH_INTERVAL_MS);
    };
    const fail = (error: unknown) => {
      state.loading = false;
      settleRefresh();
      if (callbacksRef.current.onRangeBoundsConflict?.(error)) { publish(); return true; }
      if (error instanceof ApiError && (error.status === 401 || error.status === 403)) {
        callbacksRef.current.onAuthRequired?.();
        publish();
        return true;
      }
      if (error instanceof ApiError && [400, 404, 405, 409, 501].includes(error.status)) {
        state.error = error.message;
        publish();
        return true;
      }
      state.error = isCostsBusy(error) ? 'COSTS_BUSY' : '';
      publish();
      return false;
    };
    const connect = () => {
      if (!active || document.visibilityState === 'hidden' || (!following && !restoring)) return;
      stopConnection();
      const currentGeneration = generation;
      const localController = new AbortController();
      controller = localController;
      let receivedSnapshot = false;
      state.loading = true;
      publish();
      let timedOut = false;
      let watchdog: ReturnType<typeof setTimeout> | undefined;
      const touch = () => {
        if (watchdog !== undefined) clearTimeout(watchdog);
        watchdog = setTimeout(() => { timedOut = true; localController.abort(); }, INACTIVE_TIMEOUT_MS);
      };
      const isCurrent = () => active && generation === currentGeneration;
      const receive = (frame: RequestEventsSSEFrame) => {
        if (!isCurrent()) return;
        const payload = framePayload(frame);
        if (frame.event === 'reset') {
          if (following && atTop && receivedSnapshot) {
            stopConnection();
            retryTimer = setTimeout(connect, 0);
          } else {
            restoring = false;
            state.restoring = false;
            state.loading = false;
            settleRefresh();
            stopConnection();
            publish();
          }
          return;
        }
        if (frame.event === 'busy') return;
        if (frame.event === 'heartbeat') {
          if (!following || !atTop) return;
          bounds = readBounds(payload);
          if (typeof payload.total_count === 'number') state.totalCount = payload.total_count;
          state.error = '';
          attempts = 0;
          prune();
          updateBoundary();
          publish();
          return;
        }
        if (!['snapshot', 'append', 'checkpoint'].includes(frame.event)) return;
        if (!frame.id) throw new Error('Live event cursor is missing');
        const nextBounds = readBounds(payload);
        if (frame.event === 'snapshot') {
          const events = readEvents(payload);
          if (typeof payload.total_count !== 'number') throw new Error('Invalid live snapshot');
          if (restoring && !atTop) return;
          stopHistory();
          bounds = nextBounds;
          state.events = mergeRequestEvents([], events).slice(0, pageSize);
          // 空结果不再有滚动容器，旧阅读位置不能阻止最新流继续接收。
          if (state.events.length === 0) atTop = true;
          receivedSnapshot = true;
          state.totalCount = payload.total_count;
          mayHaveMore = payload.has_more === true;
          state.historyCursor = mayHaveMore && typeof payload.next_cursor === 'string' ? payload.next_cursor : null;
          following = true;
          restoring = false;
          state.restoring = false;
          state.reading = false;
          state.loading = false;
          state.autoLoadMore = true;
          state.error = '';
          attempts = 0;
          settleRefresh();
          publish();
          if (!atTop) stopConnection();
          return;
        }
        if (!receivedSnapshot) throw new Error('Live event arrived before snapshot');
        if (!following || !atTop) return;
        bounds = nextBounds;
        state.error = '';
        attempts = 0;
        if (frame.event === 'append') {
          // 最新页只保留首屏候选，历史模式不接收也不缓存流。
          pending = mergeRequestEvents(pending, readEvents(payload)).slice(0, pageSize + 1);
          scheduleFlush();
        } else {
          prune();
          updateBoundary();
          publish();
        }
      };
      void (async () => {
        try {
          touch();
          const response = await openUsageEventsLive(request, options, '', localController.signal);
          if (!isCurrent()) return;
          await readRequestEventsSSE(response.body!, receive, touch);
          if (isCurrent()) throw new Error('Live stream disconnected');
        } catch (error) {
          if (!isCurrent() || (localController.signal.aborted && !timedOut)) return;
          const terminal = fail(error);
          if (terminal) {
            restoring = false;
            state.restoring = false;
            publish();
            return;
          }
          if (restoring) { state.loading = true; publish(); }
          const delay = error instanceof ApiError && error.status === 429 && error.retryAfterMs
            ? error.retryAfterMs
            : Math.min(30_000, 1000 * 2 ** Math.min(attempts++, 5)) * (0.8 + Math.random() * 0.4);
          // 断线时尚未刷新的小批次不推进阅读边界；重连首屏避免丢失这些记录。
          retryTimer = setTimeout(connect, delay);
        } finally {
          if (watchdog !== undefined) clearTimeout(watchdog);
          localController.abort();
          if (isCurrent()) controller = null;
        }
      })();
    };
    const startReading = () => {
      if (!following && !restoring) return;
      following = false;
      restoring = false;
      state.restoring = false;
      state.reading = true;
      stopConnection();
      state.loading = false;
      settleRefresh();
      publish();
    };
    const refresh = () => new Promise<void>((resolve) => {
      settleRefresh();
      refreshDone = resolve;
      stopHistory();
      following = true;
      restoring = false;
      state.restoring = false;
      state.error = '';
      state.loading = true;
      if (document.visibilityState === 'hidden') {
        stopConnection();
        state.loading = false;
        settleRefresh();
        publish();
      } else connect();
    });
    const loadMore = async () => {
      startReading();
      const historyCursor = state.historyCursor;
      if (!historyCursor || historyController || state.loading) return;
      const localController = new AbortController();
      historyController = localController;
      state.loadingMore = true;
      state.error = '';
      publish();
      try {
        const response: UsageEventsResponse = await fetchUsageEvents(request, localController.signal, { ...options, cursor: historyCursor });
        if (!active || historyController !== localController) return;
        state.events = appendHistory(state.events, response.events);
        state.historyCursor = response.has_more === true ? response.next_cursor?.trim() || null : null;
        state.autoLoadMore = true;
      } catch (error) {
        if (!active || localController.signal.aborted) return;
        state.autoLoadMore = false;
        if (!callbacksRef.current.onRangeBoundsConflict?.(error)) {
          if (error instanceof ApiError && (error.status === 401 || error.status === 403)) callbacksRef.current.onAuthRequired?.();
          else state.error = isCostsBusy(error) ? 'COSTS_BUSY' : error instanceof Error ? error.message : 'Failed to load more usage events';
        }
      } finally {
        if (historyController === localController) { historyController = null; state.loadingMore = false; publish(); }
      }
    };
    const returnToTop = () => {
      if (following || restoring || !atTop || document.visibilityState === 'hidden') return;
      stopHistory();
      restoring = true;
      state.restoring = true;
      connect();
    };
    const setAtTop = (value: boolean) => {
      if (atTop === value) return;
      atTop = value;
      if (!atTop) {
        if (controller || retryTimer !== undefined) stopConnection();
        restoring = false;
        state.restoring = false;
        if (state.loading) { state.loading = false; settleRefresh(); publish(); }
      } else if (following && !controller && document.visibilityState !== 'hidden') connect();
    };
    const resume = () => {
      if (!active || !following || !atTop || document.visibilityState === 'hidden' || controller) return;
      clearRetry();
      connect();
    };
    const visibility = () => {
      if (document.visibilityState === 'hidden') {
        stopConnection();
        restoring = false;
        state.restoring = false;
        state.loading = false;
        settleRefresh();
        publish();
      } else resume();
    };
    controlsRef.current = { refresh, loadMore, setAtTop, startReading, returnToTop };
    publish();
    if (document.visibilityState === 'hidden') visibility();
    else connect();
    document.addEventListener('visibilitychange', visibility);
    window.addEventListener('online', resume);
    window.addEventListener('pageshow', resume);
    return () => {
      active = false;
      stopConnection();
      stopHistory();
      settleRefresh();
      document.removeEventListener('visibilitychange', visibility);
      window.removeEventListener('online', resume);
      window.removeEventListener('pageshow', resume);
      controlsRef.current = null;
    };
    // scope 是所有筛选的稳定值；回调始终从 ref 读取。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, scope]);

  const refresh = useCallback(() => controlsRef.current?.refresh() ?? Promise.resolve(), []);
  const loadMore = useCallback(() => controlsRef.current?.loadMore() ?? Promise.resolve(), []);
  const setAtTop = useCallback((atTop: boolean) => controlsRef.current?.setAtTop(atTop), []);
  const startReading = useCallback(() => controlsRef.current?.startReading(), []);
  const returnToTop = useCallback(() => controlsRef.current?.returnToTop(), []);
  return { ...(view.scope === scope ? view : emptyView(scope)), refresh, loadMore, setAtTop, startReading, returnToTop };
}
