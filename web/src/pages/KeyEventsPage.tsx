import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  ApiError,
  exportKeyEvents,
  fetchKeyEventModelFilterOptions,
  fetchKeyEventSourceFilterOptions,
  fetchKeyStatus,
  isUsageRangeBoundsConflict,
  type UsageEventsExportFormat,
} from '@/lib/api';
import type { AuthSessionAPIKeySummary, UsageCustomRange, UsageSourceFilterOption, UsageTimeRange } from '@/lib/types';
import { TimeRangeControl } from '@/components/usage';
import {
  RequestEventsDetailsCard,
  type RequestEventColumnId,
} from '@/components/usage/RequestEventsDetailsCard';
import { useRequestEventsFeed } from '@/components/usage/useRequestEventsFeed';
import { KeyViewerShell } from '@/features/key-viewer/KeyViewerShell';
import type { KeyViewerPath } from '@/features/key-viewer/navigation';
import { loadKeyViewerTimeRange, persistKeyViewerTimeRange } from '@/features/key-viewer/timeRange';
import {
  clampCustomRangeToCurrentBounds,
  clampStoredUsageRangeStateToCurrentBounds,
  resolveUsageRangeRecoveryTimeZone,
  type StoredUsageRangeState,
} from '@/utils/usage/customRange';
import { buildUsageRangeQuery } from '@/utils/usage/rangeQuery';
import {
  loadRequestEventsPreferences,
  normalizeRequestEventsPreferences,
  sanitizeRequestEventFilters,
  triggerBrowserFileDownload,
} from '@/pages/UsagePage';
import shellStyles from '@/features/key-viewer/KeyViewerShell.module.scss';
import usageStyles from '@/pages/UsagePage.module.scss';

const REQUEST_EVENTS_DEFAULT_PAGE_SIZE = 50;
const REQUEST_EVENTS_CUSTOM_DAY_RANGE_MAX_DAYS = 90;
const ALL_REQUEST_EVENTS_FILTER = '__all__';
const KEY_VIEWER_EVENTS_PREFERENCES_STORAGE_KEY = 'cli-proxy-key-viewer-request-events-preferences-v1';

const loadKeyEventsPreferences = () => {
  try {
    if (typeof localStorage === 'undefined') {
      return loadRequestEventsPreferences(null);
    }
    const raw = localStorage.getItem(KEY_VIEWER_EVENTS_PREFERENCES_STORAGE_KEY);
    if (!raw) return loadRequestEventsPreferences(null);
    return normalizeRequestEventsPreferences(JSON.parse(raw));
  } catch {
    return loadRequestEventsPreferences(null);
  }
};

export interface KeyEventsPageProps {
  apiKey?: AuthSessionAPIKeySummary;
  onNavigate: (path: KeyViewerPath) => void;
  onAuthRequired?: () => void;
}

export function KeyEventsPage({ apiKey, onNavigate, onAuthRequired }: KeyEventsPageProps) {
  const { t } = useTranslation();
  const [timeRangeState, setTimeRangeState] = useState<StoredUsageRangeState>(loadKeyViewerTimeRange);
  const { range: timeRange, customRange } = timeRangeState;
  const [statusTimeZone, setStatusTimeZone] = useState<string | undefined>();
  const [manualRefreshLoading, setManualRefreshLoading] = useState(false);
  const [initialPreferences] = useState(loadKeyEventsPreferences);
  const [modelOptions, setModelOptions] = useState<string[]>([]);
  const [sourceOptions, setSourceOptions] = useState<UsageSourceFilterOption[]>([]);
  const [filterOptionsLoaded, setFilterOptionsLoaded] = useState(false);
  const [modelFilter, setModelFilter] = useState(initialPreferences.filters.model);
  const [sourceFilter, setSourceFilter] = useState(initialPreferences.filters.source);
  const [resultFilter, setResultFilter] = useState(initialPreferences.filters.result);
  const [visibleColumnIds, setVisibleColumnIds] = useState<RequestEventColumnId[]>(initialPreferences.visibleColumnIds);
  const [columnOrder, setColumnOrder] = useState<RequestEventColumnId[]>(initialPreferences.columnOrder);
  const [exportingFormat, setExportingFormat] = useState<UsageEventsExportFormat | null>(null);
  const [topNotice, setTopNotice] = useState<{ kind: 'success' | 'error'; message: string } | null>(null);
  const topNoticeTimerRef = useRef<ReturnType<typeof window.setTimeout> | null>(null);
  const filterOptionsControllerRef = useRef<AbortController | null>(null);

  const rangeTimeZone = statusTimeZone ?? timeRangeState.timeZone;
  const activeCustomRange = useMemo(() => {
    const normalizedTimeZone = rangeTimeZone?.trim();
    if (!customRange || !normalizedTimeZone) return customRange;
    return clampCustomRangeToCurrentBounds(customRange, {
      nowMs: Date.now(),
      timeZone: normalizedTimeZone,
      maxDayRangeDays: REQUEST_EVENTS_CUSTOM_DAY_RANGE_MAX_DAYS,
    });
  }, [customRange, rangeTimeZone]);

  const usageRangeQuery = useMemo(() => buildUsageRangeQuery({
    range: timeRange,
    customUnit: activeCustomRange?.unit,
    customStart: activeCustomRange?.start,
    customEnd: activeCustomRange?.end,
  }), [activeCustomRange?.end, activeCustomRange?.start, activeCustomRange?.unit, timeRange]);

  const showTopNotice = useCallback((kind: 'success' | 'error', message: string) => {
    if (topNoticeTimerRef.current !== null) {
      window.clearTimeout(topNoticeTimerRef.current);
    }
    setTopNotice({ kind, message });
    topNoticeTimerRef.current = window.setTimeout(() => {
      setTopNotice(null);
      topNoticeTimerRef.current = null;
    }, kind === 'error' ? 6_000 : 4_000);
  }, []);

  useEffect(() => () => {
    if (topNoticeTimerRef.current !== null) {
      window.clearTimeout(topNoticeTimerRef.current);
      topNoticeTimerRef.current = null;
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void fetchKeyStatus(controller.signal)
      .then((res) => {
        if (!controller.signal.aborted && res.timezone) {
          setStatusTimeZone(res.timezone);
        }
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        if (error instanceof ApiError && error.status === 401) {
          onAuthRequired?.();
        }
      });
    return () => controller.abort();
  }, [onAuthRequired]);

  const recoverRangeBoundsConflict = useCallback((error: unknown) => {
    if (!isUsageRangeBoundsConflict(error)) return false;
    const timeZone = resolveUsageRangeRecoveryTimeZone(timeRangeState, statusTimeZone)?.trim();
    if (!timeZone) return false;
    const nextState = clampStoredUsageRangeStateToCurrentBounds(timeRangeState, {
      nowMs: Date.now(),
      timeZone,
    });
    if (nextState === timeRangeState) return false;
    setTimeRangeState(nextState);
    return true;
  }, [statusTimeZone, timeRangeState]);

  const handleTimeRangeChange = useCallback((range: UsageTimeRange, nextCustomRange?: UsageCustomRange) => {
    let nextState: StoredUsageRangeState;
    if (range === 'custom' && nextCustomRange) {
      nextState = { range, customRange: nextCustomRange, timeZone: rangeTimeZone };
    } else {
      nextState = { ...timeRangeState, range };
    }
    setTimeRangeState(nextState);
    persistKeyViewerTimeRange(nextState);
  }, [rangeTimeZone, timeRangeState]);

  useEffect(() => {
    persistKeyViewerTimeRange(timeRangeState);
  }, [timeRangeState]);

  useEffect(() => {
    try {
      if (typeof localStorage === 'undefined') return;
      localStorage.setItem(KEY_VIEWER_EVENTS_PREFERENCES_STORAGE_KEY, JSON.stringify(normalizeRequestEventsPreferences({
        version: initialPreferences.version,
        filters: { model: modelFilter, source: sourceFilter, result: resultFilter },
        visibleColumnIds,
        columnOrder,
      })));
    } catch {
      // Ignore storage errors.
    }
  }, [columnOrder, initialPreferences.version, modelFilter, resultFilter, sourceFilter, visibleColumnIds]);

  const loadFilterOptions = useCallback(async () => {
    filterOptionsControllerRef.current?.abort();
    const controller = new AbortController();
    filterOptionsControllerRef.current = controller;
    setFilterOptionsLoaded(false);
    try {
      const [modelsResponse, sourcesResponse] = await Promise.all([
        fetchKeyEventModelFilterOptions(controller.signal),
        fetchKeyEventSourceFilterOptions(controller.signal),
      ]);
      if (filterOptionsControllerRef.current !== controller) return;
      setModelOptions(modelsResponse.models ?? []);
      setSourceOptions(sourcesResponse.sources ?? []);
      setFilterOptionsLoaded(true);
    } catch (error) {
      if (controller.signal.aborted) return;
      if (filterOptionsControllerRef.current === controller) {
        setModelOptions([]);
        setSourceOptions([]);
        setFilterOptionsLoaded(false);
      }
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.();
      }
    } finally {
      if (filterOptionsControllerRef.current === controller) {
        filterOptionsControllerRef.current = null;
      }
    }
  }, [onAuthRequired]);

  useEffect(() => {
    void loadFilterOptions();
    return () => {
      filterOptionsControllerRef.current?.abort();
      filterOptionsControllerRef.current = null;
    };
  }, [loadFilterOptions]);

  useEffect(() => {
    const next = sanitizeRequestEventFilters(
      { model: modelFilter, source: sourceFilter, result: resultFilter },
      { models: modelOptions, sources: sourceOptions },
      filterOptionsLoaded,
    );
    if (next.model !== modelFilter) setModelFilter(next.model);
    if (next.source !== sourceFilter) setSourceFilter(next.source);
    if (next.result !== resultFilter) setResultFilter(next.result);
  }, [filterOptionsLoaded, modelFilter, modelOptions, resultFilter, sourceFilter, sourceOptions]);

  const eventsFeed = useRequestEventsFeed({
    viewer: 'key',
    enabled: usageRangeQuery.valid,
    request: usageRangeQuery,
    options: {
      pageSize: REQUEST_EVENTS_DEFAULT_PAGE_SIZE,
      cursorMode: true,
      model: modelFilter === ALL_REQUEST_EVENTS_FILTER ? undefined : modelFilter,
      source: sourceFilter === ALL_REQUEST_EVENTS_FILTER ? undefined : sourceFilter,
      result: resultFilter === ALL_REQUEST_EVENTS_FILTER ? undefined : resultFilter,
    },
    onAuthRequired,
    onRangeBoundsConflict: recoverRangeBoundsConflict,
  });

  const handleManualRefresh = useCallback(async () => {
    if (manualRefreshLoading) return;
    setManualRefreshLoading(true);
    try {
      await Promise.all([loadFilterOptions(), eventsFeed.refresh()]);
    } finally {
      setManualRefreshLoading(false);
    }
  }, [eventsFeed, loadFilterOptions, manualRefreshLoading]);

  const handleExport = useCallback(async (format: UsageEventsExportFormat) => {
    if (!usageRangeQuery.valid) return;
    setExportingFormat(format);
    try {
      const file = await exportKeyEvents(usageRangeQuery, format, {
        model: modelFilter === ALL_REQUEST_EVENTS_FILTER ? undefined : modelFilter,
        source: sourceFilter === ALL_REQUEST_EVENTS_FILTER ? undefined : sourceFilter,
        result: resultFilter === ALL_REQUEST_EVENTS_FILTER ? undefined : resultFilter,
      });
      triggerBrowserFileDownload(file.blob, file.filename);
      showTopNotice('success', t('usage_stats.export_success'));
    } catch (error) {
      if (recoverRangeBoundsConflict(error)) return;
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.();
        return;
      }
      if (error instanceof ApiError && error.status === 429) {
        showTopNotice('error', t('usage_stats.export_busy'));
        return;
      }
      showTopNotice('error', t('notification.download_failed'));
    } finally {
      setExportingFormat(null);
    }
  }, [modelFilter, onAuthRequired, recoverRangeBoundsConflict, resultFilter, showTopNotice, sourceFilter, t, usageRangeQuery]);

  return (
    <KeyViewerShell
      activePage="events"
      apiKey={apiKey}
      filters={[
        <TimeRangeControl
          key="range"
          value={timeRange}
          customRange={activeCustomRange}
          timeZone={rangeTimeZone}
          maxCustomDayRangeDays={REQUEST_EVENTS_CUSTOM_DAY_RANGE_MAX_DAYS}
          onChange={handleTimeRangeChange}
          ariaLabel={t('usage_stats.range_filter')}
          labelInsideTrigger
        />,
      ]}
      onRefresh={() => void handleManualRefresh()}
      refreshing={manualRefreshLoading}
      onNavigate={onNavigate}
      onAuthRequired={onAuthRequired}
    >
      {topNotice && (
        <div
          className={`${usageStyles.updateCheckToast} ${topNotice.kind === 'error' ? usageStyles.updateCheckToastError : usageStyles.updateCheckToastSuccess}`.trim()}
          role="status"
          aria-live="polite"
        >
          <span className={usageStyles.updateCheckToastMessage}>{topNotice.message}</span>
          <button
            type="button"
            className={usageStyles.updateCheckToastClose}
            onClick={() => {
              if (topNoticeTimerRef.current !== null) {
                window.clearTimeout(topNoticeTimerRef.current);
                topNoticeTimerRef.current = null;
              }
              setTopNotice(null);
            }}
          >
            {t('usage_stats.dismiss_notice')}
          </button>
        </div>
      )}

      {eventsFeed.error && (
        <div className={shellStyles.errorBox} role="alert">
          {eventsFeed.error === 'COSTS_BUSY' ? t('usage_stats.costs_busy') : eventsFeed.error}
        </div>
      )}

      <RequestEventsDetailsCard
        events={eventsFeed.events}
        loading={eventsFeed.loading}
        totalCount={eventsFeed.totalCount}
        reading={eventsFeed.reading}
        restoring={eventsFeed.restoring}
        onTablePositionChange={eventsFeed.setAtTop}
        onStartReading={eventsFeed.startReading}
        onReturnToTop={eventsFeed.returnToTop}
        modelOptions={modelOptions}
        sourceOptions={sourceOptions}
        modelFilter={modelFilter}
        sourceFilter={sourceFilter}
        resultFilter={resultFilter}
        exportingFormat={exportingFormat}
        hasMore={Boolean(eventsFeed.historyCursor)}
        loadingMore={eventsFeed.loadingMore}
        autoLoadMore={eventsFeed.autoLoadMore}
        visibleColumnIds={visibleColumnIds}
        columnOrder={columnOrder}
        onModelFilterChange={setModelFilter}
        onSourceFilterChange={setSourceFilter}
        onResultFilterChange={setResultFilter}
        onExport={handleExport}
        onLoadMore={eventsFeed.loadMore}
        onVisibleColumnIdsChange={setVisibleColumnIds}
        onColumnOrderChange={setColumnOrder}
        requestLogAccessEnabled={false}
      />
    </KeyViewerShell>
  );
}
