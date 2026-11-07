import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError, fetchUsageEvents, isCostsBusy, openUsageEventsLive, type FetchUsageEventsOptions } from '@/lib/api';
import type { UsageEvent, UsageEventsResponse, UsageRangeRequest } from '@/lib/types';
import { readRequestEventsSSE, type RequestEventsSSEFrame } from './requestEventsSSE';

const MAX_PENDING = 1000;
const MAX_AUTO_ADDED = 2000;
const FLUSH_INTERVAL_MS = 250;
const INACTIVE_TIMEOUT_MS = 45_000;

export type RequestEventsConnectionStatus = 'connecting' | 'live' | 'reconnecting' | 'paused' | 'needs_sync' | 'busy';
type Bounds = { range_start: string; range_end: string; end_exclusive: boolean };
type View = {
  scope: string; events: UsageEvent[]; totalCount: number; historyCursor: string | null;
  loading: boolean; loadingMore: boolean; autoLoadMore: boolean; error: string;
  status: RequestEventsConnectionStatus; pendingCount: number;
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
  setFollowing: (following: boolean) => void;
  viewLatest: () => void;
};

const emptyView = (scope: string): View => ({
  scope, events: [], totalCount: 0, historyCursor: null, loading: true, loadingMore: false,
  autoLoadMore: true, error: '', status: 'connecting', pendingCount: 0,
});

// RFC3339 的纳秒尾数不能经 Date.parse 丢失；时区先换算到同一秒，再比较固定宽度小数。
function timestampParts(value: string): [number, string] {
  const match = /^(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(?:\.(\d{1,9}))?(Z|[+-]\d\d:\d\d)$/.exec(value);
  if (!match) return [Date.parse(value) || 0, '000000000'];
  return [Date.parse(match[1] + match[3]), (match[2] ?? '').padEnd(9, '0')];
}

export function compareRequestEventTimestamps(left: string, right: string): number {
  const [leftSecond, leftFraction] = timestampParts(left);
  const [rightSecond, rightFraction] = timestampParts(right);
  return leftSecond - rightSecond || (leftFraction < rightFraction ? -1 : leftFraction > rightFraction ? 1 : 0);
}

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
  return merged.sort((a, b) => compareRequestEventTimestamps(b.timestamp, a.timestamp) || compareIDs(b.id ?? '', a.id ?? ''));
}

function insideBounds(event: UsageEvent, bounds: Bounds): boolean {
  const end = compareRequestEventTimestamps(event.timestamp, bounds.range_end);
  return compareRequestEventTimestamps(event.timestamp, bounds.range_start) >= 0 && (bounds.end_exclusive ? end < 0 : end <= 0);
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
    if (!enabled) {
      controlsRef.current = null;
      return;
    }
    let active = true;
    const state = emptyView(scope);
    let cursor = '';
    let historyBoundary: UsageEvent | null = null;
    let bounds: Bounds | null = null;
    let following = true;
    let needsSync = false;
    let autoAdded = 0;
    let pending = new Map<string, UsageEvent>();
    let known = new Set<string>();
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
    const stopConnection = () => {
      generation++;
      controller?.abort();
      controller = null;
      clearRetry();
    };
    const stopHistory = () => {
      historyController?.abort();
      historyController = null;
      state.loadingMore = false;
    };
    const prune = () => {
      if (!bounds) return;
      state.events = state.events.filter((event) => insideBounds(event, bounds!));
      pending = new Map([...pending].filter(([, event]) => insideBounds(event, bounds!)));
      known = new Set([...state.events.map((event) => event.id!), ...pending.keys()]);
    };
    const flush = () => {
      flushTimer = undefined;
      if (!following || needsSync) return;
      prune();
      state.events = mergeRequestEvents(state.events, [...pending.values()]);
      pending.clear();
      state.pendingCount = 0;
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
        state.status = 'needs_sync';
        publish();
        return true;
      }
      if (error instanceof ApiError && [400, 404, 405, 409, 501].includes(error.status)) {
        state.status = 'needs_sync';
        state.error = error.message;
        publish();
        return true;
      }
      state.error = isCostsBusy(error) ? 'COSTS_BUSY' : '';
      state.status = isCostsBusy(error) ? 'busy' : 'reconnecting';
      publish();
      return false;
    };
    const connect = (fresh: boolean) => {
      if (!active || document.visibilityState === 'hidden' || needsSync) return;
      stopConnection();
      const currentGeneration = generation;
      const localController = new AbortController();
      controller = localController;
      const after = fresh ? '' : cursor;
      if (fresh) state.loading = true;
      state.status = after ? 'reconnecting' : 'connecting';
      publish();
      let timedOut = false;
      let watchdog: ReturnType<typeof setTimeout> | undefined;
      const touch = () => {
        if (watchdog !== undefined) clearTimeout(watchdog);
        watchdog = setTimeout(() => { timedOut = true; localController.abort(); }, INACTIVE_TIMEOUT_MS);
      };
      const isCurrent = () => active && generation === currentGeneration;
      const requestSnapshot = () => {
        stopConnection();
        stopHistory();
        needsSync = false;
        state.loading = true;
        state.status = 'connecting';
        publish();
        retryTimer = setTimeout(() => connect(true), 0);
      };
      const receive = (frame: RequestEventsSSEFrame) => {
        if (!isCurrent()) return;
        const payload = framePayload(frame);
        if (frame.event === 'reset') {
          if (following && after) requestSnapshot();
          else { needsSync = true; state.status = 'needs_sync'; state.loading = false; settleRefresh(); stopConnection(); publish(); }
          return;
        }
        if (frame.event === 'busy') { state.status = 'busy'; publish(); return; }
        if (frame.event === 'heartbeat') {
          bounds = readBounds(payload);
          state.status = payload.busy === true ? 'busy' : 'live';
          state.error = '';
          attempts = 0;
          if (following) prune();
          publish();
          return;
        }
        if (!['snapshot', 'append', 'checkpoint'].includes(frame.event)) return;
        if (!frame.id) throw new Error('Live event cursor is missing');
        const nextBounds = readBounds(payload);
        if (frame.event === 'snapshot') {
          if (after) throw new Error('Unexpected live snapshot on resume');
          const events = readEvents(payload);
          if (typeof payload.total_count !== 'number') throw new Error('Invalid live snapshot');
          stopHistory();
          bounds = nextBounds;
          state.events = mergeRequestEvents([], events);
          historyBoundary = events.at(-1) ?? null;
          state.totalCount = payload.total_count;
          state.historyCursor = payload.has_more === true && typeof payload.next_cursor === 'string' ? payload.next_cursor : null;
          pending.clear();
          known = new Set(state.events.map((event) => event.id!));
          autoAdded = 0;
          needsSync = false;
          state.pendingCount = 0;
          state.loading = false;
          state.autoLoadMore = true;
          cursor = frame.id;
          state.error = '';
          state.status = 'live';
          attempts = 0;
          settleRefresh();
          publish();
          return;
        }
        if (!cursor) throw new Error('Live event arrived before snapshot');
        if (frame.event === 'append') {
          // 非并发的界外迟到事件由后续历史分页覆盖；在途查询可能已取数，必须停止并请求重新同步。
          let historyConflict = false;
          const incoming = readEvents(payload).filter((event) => {
            if (known.has(event.id!)) return false;
            if (!state.historyCursor || !historyBoundary) return true;
            const timeOrder = compareRequestEventTimestamps(event.timestamp, historyBoundary.timestamp);
            const withinLoaded = timeOrder > 0 || (timeOrder === 0 && compareIDs(event.id!, historyBoundary.id!) > 0);
            if (!withinLoaded && historyController) historyConflict = true;
            return withinLoaded;
          });
          if (historyConflict) {
            needsSync = true;
            state.status = 'needs_sync';
            stopHistory();
            stopConnection();
            publish();
            return;
          }
          const unique = [...new Map(incoming.map((event) => [event.id!, event])).values()];
          if ((!following && pending.size + unique.length > MAX_PENDING) || (following && autoAdded + unique.length > MAX_AUTO_ADDED)) {
            if (following) requestSnapshot();
            else { needsSync = true; state.status = 'needs_sync'; stopConnection(); publish(); }
            return;
          }
          for (const event of unique) { pending.set(event.id!, event); known.add(event.id!); }
          autoAdded += unique.length;
          bounds = nextBounds;
          cursor = frame.id;
          state.status = 'live';
          state.error = '';
          attempts = 0;
          if (following) scheduleFlush();
          else { state.pendingCount = pending.size; publish(); }
          return;
        }
        bounds = nextBounds;
        cursor = frame.id;
        state.status = 'live';
        state.error = '';
        attempts = 0;
        if (following) { prune(); publish(); }
      };
      void (async () => {
        try {
          touch();
          const response = await openUsageEventsLive(request, options, after, localController.signal);
          if (!isCurrent()) return;
          await readRequestEventsSSE(response.body!, receive, touch);
          if (isCurrent()) throw new Error('Live stream disconnected');
        } catch (error) {
          if (!isCurrent() || (localController.signal.aborted && !timedOut)) return;
          if (fail(error)) return;
          const delay = error instanceof ApiError && error.status === 429 && error.retryAfterMs
            ? error.retryAfterMs
            : Math.min(30_000, 1000 * 2 ** Math.min(attempts++, 5)) * (0.8 + Math.random() * 0.4);
          retryTimer = setTimeout(() => connect(!cursor), delay);
        } finally {
          if (watchdog !== undefined) clearTimeout(watchdog);
          // 解析失败等非 AbortError 路径也必须取消 fetch，不能遗留实际 SSE 连接。
          localController.abort();
          if (isCurrent()) controller = null;
        }
      })();
    };
    const refresh = () => new Promise<void>((resolve) => {
      settleRefresh();
      refreshDone = resolve;
      stopHistory();
      needsSync = false;
      state.error = '';
      state.loading = true;
      if (document.visibilityState === 'hidden') {
        state.loading = false;
        state.status = 'paused';
        settleRefresh();
        publish();
        return;
      }
      connect(true);
    });
    const loadMore = async () => {
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
        state.events = mergeRequestEvents(state.events, response.events);
        historyBoundary = response.events.at(-1) ?? historyBoundary;
        for (const event of response.events) {
          if (event.id) { known.add(event.id); pending.delete(event.id); }
        }
        state.pendingCount = pending.size;
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
    const setFollowing = (value: boolean) => {
      if (following === value) return;
      following = value;
      if (following) {
        if (needsSync) { needsSync = false; void refresh(); }
        else flush();
      } else { state.pendingCount = pending.size; publish(); }
    };
    const viewLatest = () => {
      following = true;
      if (needsSync) { needsSync = false; void refresh(); }
      else flush();
    };
    const resume = () => {
      if (!active || needsSync || document.visibilityState === 'hidden' || controller) return;
      clearRetry();
      connect(!cursor);
    };
    const visibility = () => {
      if (document.visibilityState === 'hidden') {
        stopConnection();
        state.loading = false;
        state.status = 'paused';
        settleRefresh();
        publish();
      } else if (needsSync) {
        state.status = 'needs_sync';
        publish();
      } else resume();
    };
    controlsRef.current = { refresh, loadMore, setFollowing, viewLatest };
    publish();
    if (document.visibilityState === 'hidden') visibility();
    else connect(true);
    document.addEventListener('visibilitychange', visibility);
    window.addEventListener('online', resume);
    window.addEventListener('pageshow', resume);
    return () => {
      active = false;
      stopConnection();
      stopHistory();
      if (flushTimer !== undefined) clearTimeout(flushTimer);
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
  const setFollowing = useCallback((following: boolean) => controlsRef.current?.setFollowing(following), []);
  const viewLatest = useCallback(() => controlsRef.current?.viewLatest(), []);
  return { ...(view.scope === scope ? view : emptyView(scope)), refresh, loadMore, setFollowing, viewLatest };
}
