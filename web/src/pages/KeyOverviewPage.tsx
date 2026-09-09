import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ApiError, fetchKeyOverview, fetchKeyOverviewRealtime, isUsageRangeBoundsConflict } from '@/lib/api';
import type { AuthSessionAPIKeySummary, OverviewRealtimeBlock, OverviewRealtimeWindow, UsageCustomRange, UsageOverviewResponse, UsageTimeRange } from '@/lib/types';
import { MainActionButton } from '@/components/ui/MainActionButton';
import { IconRefreshCw } from '@/components/ui/icons';
import { KeyViewerShell } from '@/features/key-viewer/KeyViewerShell';
import type { KeyViewerPath } from '@/features/key-viewer/navigation';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import { buildUsageStatsQueryKey, useThemeStore } from '@/stores';
import {
  OverviewRealtimePanel,
  RecentActivityPanel,
  StatCards,
  TimeRangeControl,
  useRecentActivityWindow,
  useSparklines,
  useUsageActivityData,
} from '@/components/usage';
import type { UsageOverviewPayload } from '@/components/usage/hooks/useUsageData';
import { getCurrentOverviewUsage, getDailyAverageCardUsage, getOverviewDisplayLoading, isDailyAverageRange } from '@/utils/usage/overview';
import { clampStoredUsageRangeStateToCurrentBounds, resolveUsageRangeRecoveryTimeZone, type StoredUsageRangeState } from '@/utils/usage/customRange';
import { buildUsageRangeQuery } from '@/utils/usage/rangeQuery';
import { loadKeyViewerTimeRange, persistKeyViewerTimeRange } from '@/features/key-viewer/timeRange';
import styles from '@/features/key-viewer/KeyViewerShell.module.scss';

const OVERVIEW_REALTIME_WINDOW_STORAGE_KEY = 'cli-proxy-usage-overview-realtime-window-v1';
const DEFAULT_REALTIME_WINDOW: OverviewRealtimeWindow = '15m';
const KEY_OVERVIEW_REALTIME_VISIBLE_DIMENSIONS = ['models'] as const;
const KEY_OVERVIEW_AUTO_REFRESH_INTERVAL_MS = 10_000;

const loadTimeRange = (): StoredUsageRangeState => {
  return loadKeyViewerTimeRange();
};

const isOverviewRealtimeWindow = (value: unknown): value is OverviewRealtimeWindow => (
  value === '15m' || value === '30m' || value === '60m'
);

const loadRealtimeWindow = (): OverviewRealtimeWindow => {
  try {
    if (typeof localStorage === 'undefined') return DEFAULT_REALTIME_WINDOW;
    const raw = localStorage.getItem(OVERVIEW_REALTIME_WINDOW_STORAGE_KEY);
    return isOverviewRealtimeWindow(raw) ? raw : DEFAULT_REALTIME_WINDOW;
  } catch {
    return DEFAULT_REALTIME_WINDOW;
  }
};

type KeyOverviewAutoRefreshDocument = Pick<Document, 'visibilityState' | 'addEventListener' | 'removeEventListener'>;

type KeyOverviewAutoRefreshOptions = {
  refreshOverview: () => void | Promise<void>;
  onRefreshError?: (error: unknown) => void;
  documentRef?: KeyOverviewAutoRefreshDocument;
  intervalMs?: number;
};

type KeyOverviewLoadOptions = {
  skipIfInFlight?: boolean;
};

type KeyOverviewRequestStartOptions = {
  currentController: AbortController | null;
  skipIfInFlight?: boolean;
};

export const startKeyOverviewRequest = ({
  currentController,
  skipIfInFlight,
}: KeyOverviewRequestStartOptions): { controller: AbortController | null; skipped: boolean } => {
  if (currentController && skipIfInFlight) {
    return { controller: null, skipped: true };
  }
  currentController?.abort();
  return { controller: new AbortController(), skipped: false };
};

export const scheduleKeyOverviewAutoRefresh = ({
  refreshOverview,
  onRefreshError,
  documentRef,
  intervalMs = KEY_OVERVIEW_AUTO_REFRESH_INTERVAL_MS,
}: KeyOverviewAutoRefreshOptions) => {
  const targetDocument = documentRef ?? (typeof document === 'undefined' ? undefined : document);
  if (!targetDocument) {
    return () => undefined;
  }

  let timer: ReturnType<typeof setInterval> | undefined;
  const stopTimer = () => {
    if (timer === undefined) return;
    clearInterval(timer);
    timer = undefined;
  };
  const runRefresh = () => {
    Promise.resolve(refreshOverview()).catch((nextError: unknown) => {
      onRefreshError?.(nextError);
    });
  };
  const refreshIfVisible = () => {
    if (targetDocument.visibilityState === 'hidden') {
      stopTimer();
      return;
    }
    runRefresh();
  };
  const startTimer = () => {
    if (timer !== undefined) return;
    timer = setInterval(refreshIfVisible, intervalMs);
  };
  const handleVisibilityChange = () => {
    if (targetDocument.visibilityState === 'hidden') {
      stopTimer();
      return;
    }
    runRefresh();
    stopTimer();
    startTimer();
  };

  if (targetDocument.visibilityState !== 'hidden') {
    startTimer();
  }
  targetDocument.addEventListener('visibilitychange', handleVisibilityChange);

  return () => {
    stopTimer();
    targetDocument.removeEventListener('visibilitychange', handleVisibilityChange);
  };
};

export interface KeyOverviewPageProps {
  apiKey?: AuthSessionAPIKeySummary;
  onNavigate: (path: KeyViewerPath) => void;
  onAuthRequired?: () => void;
}

export function KeyOverviewPage({ apiKey, onNavigate, onAuthRequired }: KeyOverviewPageProps) {
  const { t } = useTranslation();
  const isMobile = useMediaQuery('(max-width: 768px)');
  const resolvedTheme = useThemeStore((state) => state.resolvedTheme);
  const isDark = resolvedTheme === 'dark';
  const [timeRangeState, setTimeRangeState] = useState<StoredUsageRangeState>(loadTimeRange);
  const { range: timeRange, customRange } = timeRangeState;
  const [realtimeWindow, setRealtimeWindow] = useState<OverviewRealtimeWindow>(loadRealtimeWindow);
  const [usage, setUsage] = useState<UsageOverviewPayload | null>(null);
  const [loadedUsageRange, setLoadedUsageRange] = useState<string | null>(null);
  const [realtime, setRealtime] = useState<OverviewRealtimeBlock | null>(null);
  const [loading, setLoading] = useState(false);
  const [realtimeLoading, setRealtimeLoading] = useState(false);
  const [error, setError] = useState('');
  const [realtimeError, setRealtimeError] = useState('');
  const [manualRefreshLoading, setManualRefreshLoading] = useState(false);
  const overviewRequestControllerRef = useRef<AbortController | null>(null);
  const realtimeRequestControllerRef = useRef<AbortController | null>(null);
  const usageRangeQuery = useMemo(() => buildUsageRangeQuery({
    range: timeRange,
    customUnit: customRange?.unit,
    customStart: customRange?.start,
    customEnd: customRange?.end,
  }), [customRange?.end, customRange?.start, customRange?.unit, timeRange]);
  const usageRangeQueryKey = usageRangeQuery.valid ? buildUsageStatsQueryKey(usageRangeQuery) : null;
  const {
    request: activityRangeRequest,
    manualWindow: manualActivityWindow,
    setWindow: setActivityWindow,
  } = useRecentActivityWindow(usageRangeQuery);
  const {
    activity,
    activityMatchesRequest,
    loading: activityLoading,
    error: activityError,
    requestIdentity: activityRequestIdentity,
    loadActivity,
  } = useUsageActivityData({
    viewer: 'key',
    request: activityRangeRequest,
    enabled: usageRangeQuery.valid,
    onAuthRequired,
  });
  const activityWindow = manualActivityWindow ?? activity?.window ?? null;
  const activityWindowIsCurrent = manualActivityWindow !== null || activityMatchesRequest;
  const rangeTimeZone = usage?.timezone ?? timeRangeState.timeZone;
  const rangeRecoveryTimeZone = resolveUsageRangeRecoveryTimeZone(timeRangeState, usage?.timezone);
  const recoverRangeBoundsConflict = useCallback((error: unknown) => {
    if (!isUsageRangeBoundsConflict(error)) return false;
    const timeZone = rangeRecoveryTimeZone?.trim();
    if (!timeZone) return false;
    const nextState = clampStoredUsageRangeStateToCurrentBounds(timeRangeState, {
      nowMs: Date.now(),
      timeZone,
    });
    if (nextState === timeRangeState) return false;
    setTimeRangeState(nextState);
    return true;
  }, [rangeRecoveryTimeZone, timeRangeState]);
  const handleTimeRangeChange = useCallback((range: UsageTimeRange, nextCustomRange?: UsageCustomRange) => {
    let nextState: StoredUsageRangeState;
    if (range === 'custom' && nextCustomRange) {
      nextState = { range, customRange: nextCustomRange, timeZone: rangeTimeZone };
    } else {
      nextState = { ...timeRangeState, range };
    }
    setTimeRangeState(nextState);
    // 切换页面可能紧接着发生，先同步写入共享缓存，再等待状态 effect。
    persistKeyViewerTimeRange(nextState);
  }, [rangeTimeZone, timeRangeState]);

  const loadOverview = useCallback(async (options: KeyOverviewLoadOptions = {}) => {
    if (!usageRangeQuery.valid) return;
    const { controller, skipped } = startKeyOverviewRequest({
      currentController: overviewRequestControllerRef.current,
      skipIfInFlight: options.skipIfInFlight,
    });
    if (skipped || !controller) return;
    overviewRequestControllerRef.current = controller;
    setLoading(true);
    setError('');
    try {
      const overview = await fetchKeyOverview(usageRangeQuery, controller.signal);
      if (overviewRequestControllerRef.current !== controller) return;
      setUsage(overview as UsageOverviewResponse as UsageOverviewPayload);
      setLoadedUsageRange(usageRangeQueryKey);
    } catch (nextError) {
      if (controller.signal.aborted) return;
      if (recoverRangeBoundsConflict(nextError)) return;
      if (nextError instanceof ApiError && nextError.status === 401) {
        onAuthRequired?.();
        return;
      }
      setError(nextError instanceof Error ? nextError.message : 'KEY_OVERVIEW_LOAD_FAILED');
    } finally {
      if (overviewRequestControllerRef.current === controller) {
        setLoading(false);
        overviewRequestControllerRef.current = null;
      }
    }
  }, [onAuthRequired, recoverRangeBoundsConflict, usageRangeQuery, usageRangeQueryKey]);

  const loadRealtime = useCallback(async (options: KeyOverviewLoadOptions = {}) => {
    const { controller, skipped } = startKeyOverviewRequest({
      currentController: realtimeRequestControllerRef.current,
      skipIfInFlight: options.skipIfInFlight,
    });
    if (skipped || !controller) return;
    realtimeRequestControllerRef.current = controller;
    setRealtimeLoading(true);
    setRealtimeError('');
    try {
      const nextRealtime = await fetchKeyOverviewRealtime({
        window: realtimeWindow,
        signal: controller.signal,
      });
      if (realtimeRequestControllerRef.current !== controller) return;
      setRealtime(nextRealtime);
    } catch (nextError) {
      if (controller.signal.aborted) return;
      if (nextError instanceof ApiError && nextError.status === 401) {
        onAuthRequired?.();
        return;
      }
      setRealtimeError('KEY_OVERVIEW_REALTIME_LOAD_FAILED');
    } finally {
      if (realtimeRequestControllerRef.current === controller) {
        setRealtimeLoading(false);
        realtimeRequestControllerRef.current = null;
      }
    }
  }, [onAuthRequired, realtimeWindow]);

  useEffect(() => {
    void loadOverview();
    return () => {
      overviewRequestControllerRef.current?.abort();
      overviewRequestControllerRef.current = null;
    };
  }, [loadOverview]);

  useEffect(() => {
    void loadRealtime();
    return () => {
      realtimeRequestControllerRef.current?.abort();
      realtimeRequestControllerRef.current = null;
    };
  }, [loadRealtime]);

  const refreshKeyOverview = useCallback(async (options: KeyOverviewLoadOptions = {}) => {
    await Promise.all([loadOverview(options), loadActivity(options), loadRealtime(options)]);
  }, [loadActivity, loadOverview, loadRealtime]);

  const handleAutoRefreshError = useCallback((nextError: unknown) => {
    if (nextError instanceof ApiError && nextError.status === 401) {
      onAuthRequired?.();
      return;
    }
    setError('KEY_OVERVIEW_LOAD_FAILED');
  }, [onAuthRequired]);

  useEffect(() => scheduleKeyOverviewAutoRefresh({
    refreshOverview: () => refreshKeyOverview({ skipIfInFlight: true }),
    onRefreshError: handleAutoRefreshError,
    intervalMs: KEY_OVERVIEW_AUTO_REFRESH_INTERVAL_MS,
  }), [handleAutoRefreshError, refreshKeyOverview]);

  useEffect(() => {
    persistKeyViewerTimeRange(timeRangeState);
  }, [timeRangeState]);

  useEffect(() => {
    try {
      localStorage.setItem(OVERVIEW_REALTIME_WINDOW_STORAGE_KEY, realtimeWindow);
    } catch {
      // ignore storage failures
    }
  }, [realtimeWindow]);

  const overviewDisplayLoading = getOverviewDisplayLoading({ loading, hasUsage: Boolean(usage) });
  const currentOverviewUsage = getCurrentOverviewUsage(usage, usageRangeQueryKey, loadedUsageRange);
  const reserveDailyAverageCard = isDailyAverageRange({
    range: timeRange,
    customUnit: customRange?.unit,
    customStart: customRange?.start,
    customEnd: customRange?.end,
  });
  const dailyAverageCardUsage = getDailyAverageCardUsage(currentOverviewUsage, usage, reserveDailyAverageCard, loading);
  const {
    requestsSparkline,
    tokensSparkline,
    rpmSparkline,
    tpmSparkline,
    cacheReadRateSparkline,
    costSparkline,
  } = useSparklines({ usage, loading });

  const refreshDisabled = manualRefreshLoading;
  const handleManualRefresh = useCallback(async () => {
    if (refreshDisabled) return;
    setManualRefreshLoading(true);
    try {
      await refreshKeyOverview();
    } finally {
      setManualRefreshLoading(false);
    }
  }, [refreshDisabled, refreshKeyOverview]);

  const displayError = error === 'KEY_OVERVIEW_LOAD_FAILED'
    ? t('key_overview.load_failed')
    : error;
  const displayRealtimeError = realtimeError
    ? t('usage_stats.overview_realtime_load_failed')
    : '';

  const toolbar = (
    <>
      <div className={styles.usageFilterBar}>
        <TimeRangeControl
          value={timeRange}
          customRange={customRange}
          timeZone={rangeTimeZone}
          onChange={handleTimeRangeChange}
          ariaLabel={t('usage_stats.range_filter')}
        />
      </div>
      <div className={styles.usageRefreshSlot}>
        <div className={styles.usageFilterActions}>
          <MainActionButton
            type="button"
            shellClassName={styles.refreshMainActionShell}
            className={styles.refreshMainActionButton}
            onClick={() => void handleManualRefresh()}
            disabled={refreshDisabled}
            loading={manualRefreshLoading}
          >
            {manualRefreshLoading ? t('common.loading') : (
              <>
                <IconRefreshCw size={14} />
                <span>{t('usage_stats.refresh')}</span>
              </>
            )}
          </MainActionButton>
        </div>
      </div>
    </>
  );

  return (
    <KeyViewerShell
      activePage="overview"
      apiKey={apiKey}
      loading={loading && !usage}
      toolbar={toolbar}
      onNavigate={onNavigate}
      onAuthRequired={onAuthRequired}
    >
      {displayError && <div className={styles.errorBox}>{displayError}</div>}

      <StatCards
        usage={usage}
        loading={overviewDisplayLoading}
        dailyAverageUsage={dailyAverageCardUsage}
        reserveDailyAverage={reserveDailyAverageCard}
        sparklines={{
          requests: requestsSparkline,
          tokens: tokensSparkline,
          rpm: rpmSparkline,
          tpm: tpmSparkline,
          cacheReadRate: cacheReadRateSparkline,
          cost: costSparkline,
        }}
      />

      <RecentActivityPanel
        activity={activity}
        loading={activityLoading}
        error={activityError}
        window={activityWindow}
        windowIsCurrent={activityWindowIsCurrent}
        requestIdentity={activityRequestIdentity}
        onWindowChange={setActivityWindow}
      />

      <OverviewRealtimePanel
        realtime={realtime?.window === realtimeWindow ? realtime : undefined}
        loading={realtimeLoading}
        error={displayRealtimeError}
        window={realtimeWindow}
        onWindowChange={setRealtimeWindow}
        isDark={isDark}
        isMobile={isMobile}
        timezone={realtime?.timezone ?? usage?.timezone}
        visibleDimensions={KEY_OVERVIEW_REALTIME_VISIBLE_DIMENSIONS}
      />
    </KeyViewerShell>
  );
}
