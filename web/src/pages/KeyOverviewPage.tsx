import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ApiError, fetchKeyOverview, fetchKeyOverviewRealtime, logout } from '@/lib/api';
import type { AuthSessionAPIKeySummary, KeyOverviewTimeRange, OverviewRealtimeBlock, OverviewRealtimeWindow, UsageOverviewResponse } from '@/lib/types';
import { LanguageSwitcher } from '@/components/ui/LanguageSwitcher';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { Select } from '@/components/ui/Select';
import { IconRefreshCw } from '@/components/ui/icons';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import { useThemeStore } from '@/stores';
import {
  DailyAveragePanel,
  OverviewRealtimePanel,
  ServiceHealthCard,
  StatCards,
  useSparklines,
} from '@/components/usage';
import type { UsageOverviewPayload } from '@/components/usage/hooks/useUsageData';
import { BrandLink } from '@/components/BrandLink';
import { getCurrentOverviewUsage, getDailyAveragePanelUsage, getOverviewDisplayLoading, isDailyAverageRange } from '@/utils/usage/overview';
import type { Theme } from '@/types';
import styles from './KeyOverviewPage.module.scss';

const KEY_OVERVIEW_RANGE_STORAGE_KEY = 'cli-proxy-key-overview-range-v1';
const OVERVIEW_REALTIME_WINDOW_STORAGE_KEY = 'cli-proxy-usage-overview-realtime-window-v1';
const DEFAULT_TIME_RANGE: KeyOverviewTimeRange = '8h';
const DEFAULT_REALTIME_WINDOW: OverviewRealtimeWindow = '15m';
const KEY_OVERVIEW_REALTIME_VISIBLE_DIMENSIONS = ['models'] as const;
const REFRESH_THROTTLE_MS = 1_000;
const KEY_OVERVIEW_AUTO_REFRESH_INTERVAL_MS = 10_000;

const TIME_RANGE_OPTIONS: ReadonlyArray<{ value: KeyOverviewTimeRange; labelKey: string }> = [
  { value: '4h', labelKey: 'usage_stats.range_4h' },
  { value: '8h', labelKey: 'usage_stats.range_8h' },
  { value: '12h', labelKey: 'usage_stats.range_12h' },
  { value: '24h', labelKey: 'usage_stats.range_24h' },
  { value: 'today', labelKey: 'usage_stats.range_today' },
  { value: 'yesterday', labelKey: 'usage_stats.range_yesterday' },
  { value: '7d', labelKey: 'usage_stats.range_7d' },
  { value: '30d', labelKey: 'usage_stats.range_30d' },
];

const THEME_OPTIONS: ReadonlyArray<{ value: Theme; labelKey: string }> = [
  { value: 'white', labelKey: 'usage_stats.theme_light' },
  { value: 'dark', labelKey: 'usage_stats.theme_dark' },
  { value: 'auto', labelKey: 'usage_stats.theme_auto' },
];

const isKeyOverviewTimeRange = (value: unknown): value is KeyOverviewTimeRange => (
  value === '4h' || value === '8h' || value === '12h' || value === '24h' || value === 'today' || value === 'yesterday' || value === '7d' || value === '30d'
);

const loadTimeRange = (): KeyOverviewTimeRange => {
  try {
    if (typeof localStorage === 'undefined') return DEFAULT_TIME_RANGE;
    const raw = localStorage.getItem(KEY_OVERVIEW_RANGE_STORAGE_KEY);
    return isKeyOverviewTimeRange(raw) ? raw : DEFAULT_TIME_RANGE;
  } catch {
    return DEFAULT_TIME_RANGE;
  }
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
  onAuthRequired?: () => void;
}

export function KeyOverviewPage({ apiKey, onAuthRequired }: KeyOverviewPageProps) {
  const { t } = useTranslation();
  const isMobile = useMediaQuery('(max-width: 768px)');
  const theme = useThemeStore((state) => state.theme);
  const resolvedTheme = useThemeStore((state) => state.resolvedTheme);
  const isDark = resolvedTheme === 'dark';
  const setTheme = useThemeStore((state) => state.setTheme);
  const [timeRange, setTimeRange] = useState<KeyOverviewTimeRange>(loadTimeRange);
  const [realtimeWindow, setRealtimeWindow] = useState<OverviewRealtimeWindow>(loadRealtimeWindow);
  const [usage, setUsage] = useState<UsageOverviewPayload | null>(null);
  const [loadedUsageRange, setLoadedUsageRange] = useState<KeyOverviewTimeRange | null>(null);
  const [realtime, setRealtime] = useState<OverviewRealtimeBlock | null>(null);
  const [loading, setLoading] = useState(false);
  const [realtimeLoading, setRealtimeLoading] = useState(false);
  const [error, setError] = useState('');
  const [realtimeError, setRealtimeError] = useState('');
  const [lastRefreshedAt, setLastRefreshedAt] = useState<Date | null>(null);
  const [manualRefreshLoading, setManualRefreshLoading] = useState(false);
  const [refreshThrottled, setRefreshThrottled] = useState(false);
  const [loggingOut, setLoggingOut] = useState(false);
  const overviewRequestControllerRef = useRef<AbortController | null>(null);
  const realtimeRequestControllerRef = useRef<AbortController | null>(null);
  const refreshThrottleTimerRef = useRef<ReturnType<typeof window.setTimeout> | null>(null);

  const rangeOptions = useMemo(() => TIME_RANGE_OPTIONS.map((option) => ({
    value: option.value,
    label: t(option.labelKey),
  })), [t]);

  const themeOptions = useMemo(
    () => THEME_OPTIONS.map((option) => ({ ...option, label: t(option.labelKey) })),
    [t]
  );

  const loadOverview = useCallback(async (options: KeyOverviewLoadOptions = {}) => {
    const { controller, skipped } = startKeyOverviewRequest({
      currentController: overviewRequestControllerRef.current,
      skipIfInFlight: options.skipIfInFlight,
    });
    if (skipped || !controller) return;
    overviewRequestControllerRef.current = controller;
    const requestRange = timeRange;
    setLoading(true);
    setError('');
    try {
      const overview = await fetchKeyOverview(requestRange, controller.signal);
      if (overviewRequestControllerRef.current !== controller) return;
      setUsage(overview as UsageOverviewResponse as UsageOverviewPayload);
      setLoadedUsageRange(requestRange);
      setLastRefreshedAt(new Date());
    } catch (nextError) {
      if (controller.signal.aborted) return;
      if (nextError instanceof ApiError && nextError.status === 401) {
        onAuthRequired?.();
        return;
      }
      if (nextError instanceof ApiError && nextError.status === 429) {
        setError('KEY_OVERVIEW_RATE_LIMITED');
        return;
      }
      setError(nextError instanceof Error ? nextError.message : 'KEY_OVERVIEW_LOAD_FAILED');
    } finally {
      if (overviewRequestControllerRef.current === controller) {
        setLoading(false);
        overviewRequestControllerRef.current = null;
      }
    }
  }, [onAuthRequired, timeRange]);

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
      if (nextError instanceof ApiError && nextError.status === 429) {
        setRealtimeError('KEY_OVERVIEW_RATE_LIMITED');
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

  useEffect(() => () => {
    if (refreshThrottleTimerRef.current !== null) {
      window.clearTimeout(refreshThrottleTimerRef.current);
      refreshThrottleTimerRef.current = null;
    }
  }, []);

  const refreshKeyOverview = useCallback(async (options: KeyOverviewLoadOptions = {}) => {
    await Promise.all([loadOverview(options), loadRealtime(options)]);
  }, [loadOverview, loadRealtime]);

  const handleAutoRefreshError = useCallback((nextError: unknown) => {
    if (nextError instanceof ApiError && nextError.status === 401) {
      onAuthRequired?.();
      return;
    }
    if (nextError instanceof ApiError && nextError.status === 429) {
      setError('KEY_OVERVIEW_RATE_LIMITED');
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
    try {
      localStorage.setItem(KEY_OVERVIEW_RANGE_STORAGE_KEY, timeRange);
    } catch {
      // ignore storage failures
    }
  }, [timeRange]);

  useEffect(() => {
    try {
      localStorage.setItem(OVERVIEW_REALTIME_WINDOW_STORAGE_KEY, realtimeWindow);
    } catch {
      // ignore storage failures
    }
  }, [realtimeWindow]);

  const overviewDisplayLoading = getOverviewDisplayLoading({ loading, hasUsage: Boolean(usage) });
  const currentOverviewUsage = getCurrentOverviewUsage(usage, timeRange, loadedUsageRange);
  const reserveDailyAveragePanel = isDailyAverageRange({ range: timeRange });
  const dailyAveragePanelUsage = getDailyAveragePanelUsage(currentOverviewUsage, usage, reserveDailyAveragePanel, loading);
  const {
    requestsSparkline,
    tokensSparkline,
    rpmSparkline,
    tpmSparkline,
    cachedRateSparkline,
    costSparkline,
  } = useSparklines({ usage, loading });

  const refreshDisabled = manualRefreshLoading || refreshThrottled;
  const handleManualRefresh = useCallback(async () => {
    if (refreshDisabled) return;
    setManualRefreshLoading(true);
    try {
      await refreshKeyOverview();
      setRefreshThrottled(true);
      if (refreshThrottleTimerRef.current !== null) {
        window.clearTimeout(refreshThrottleTimerRef.current);
      }
      refreshThrottleTimerRef.current = window.setTimeout(() => {
        refreshThrottleTimerRef.current = null;
        setRefreshThrottled(false);
      }, REFRESH_THROTTLE_MS);
    } finally {
      setManualRefreshLoading(false);
    }
  }, [refreshDisabled, refreshKeyOverview]);

  const handleLogout = useCallback(async () => {
    setLoggingOut(true);
    try {
      await logout();
    } finally {
      onAuthRequired?.();
      setLoggingOut(false);
    }
  }, [onAuthRequired]);

  const identityLabel = apiKey?.display_key || t('key_overview.identity_unknown');
  const displayError = error === 'KEY_OVERVIEW_RATE_LIMITED'
    ? t('key_overview.rate_limited')
    : error === 'KEY_OVERVIEW_LOAD_FAILED'
      ? t('key_overview.load_failed')
      : error;
  const displayRealtimeError = realtimeError
    ? realtimeError === 'KEY_OVERVIEW_RATE_LIMITED'
      ? t('key_overview.rate_limited')
      : t('usage_stats.overview_realtime_load_failed')
    : '';

  return (
    <div className={styles.pageShell}>
      <div className={styles.pageFrame}>
        <header className={styles.topBar}>
          <div className={styles.brandBlock}>
            <BrandLink className={styles.eyebrow} />
          </div>
          <div className={styles.topBarActions}>
            <span className={styles.identityChip} title={identityLabel}>
              <span className={styles.identityDot} aria-hidden="true" />
              <span className={styles.identityText}>{identityLabel}</span>
            </span>
            <LanguageSwitcher />
            <div className={styles.themeSwitcher} role="tablist" aria-label={t('usage_stats.theme_switch')}>
              {themeOptions.map((option) => {
                const active = theme === option.value;
                return (
                  <button
                    key={option.value}
                    type="button"
                    role="tab"
                    aria-selected={active}
                    className={`${styles.themePill} ${active ? styles.themePillActive : ''}`.trim()}
                    onClick={() => setTheme(option.value)}
                  >
                    {option.label}
                  </button>
                );
              })}
            </div>
            <div className={styles.logoutSwitcher} role="group" aria-label={t('common.logout')}>
              <button
                type="button"
                className={`${styles.logoutPill} ${styles.logoutPillActive}`.trim()}
                onClick={() => void handleLogout()}
                disabled={loggingOut}
              >
                <span className={styles.logoutPillInner}>{loggingOut ? t('common.loading') : t('common.logout')}</span>
              </button>
            </div>
          </div>
        </header>

        <main className={styles.contentColumn}>
          <div className={styles.container}>
            {loading && !usage && (
              <div className={styles.loadingOverlay} aria-busy="true">
                <div className={styles.loadingOverlayContent}>
                  <LoadingSpinner size={28} className={styles.loadingOverlaySpinner} />
                  <span className={styles.loadingOverlayText}>{t('common.loading')}</span>
                </div>
              </div>
            )}

            {lastRefreshedAt && (
              <div className={styles.toolbarMetaRow}>
                <span className={styles.lastRefreshed}>
                  {t('usage_stats.last_updated')}: {lastRefreshedAt.toLocaleTimeString()}
                </span>
              </div>
            )}

            <div className={styles.toolbarRow}>
              <div className={styles.tabBar} role="tablist" aria-label={t('key_overview.tabs_aria_label')}>
                <button type="button" role="tab" aria-selected="true" className={`${styles.tabPill} ${styles.tabPillActive}`.trim()}>
                  {t('usage_stats.tab_overview')}
                </button>
              </div>

              <div className={styles.toolbarActionsRight}>
                <div className={styles.usageFilterBar}>
                  <div className={styles.timeRangeGroup}>
                    <label className={`${styles.usageFilterField} ${styles.rangeFilterField}`.trim()}>
                      <span className={styles.usageFilterLabel}>{t('usage_stats.range_filter')}</span>
                      <Select
                        value={timeRange}
                        options={rangeOptions}
                        onChange={(value) => setTimeRange(value as KeyOverviewTimeRange)}
                        className={styles.rangeSelectControl}
                        ariaLabel={t('usage_stats.range_filter')}
                        fullWidth
                      />
                    </label>
                  </div>
                </div>
                <div className={styles.usageRefreshSlot}>
                  <div className={styles.usageFilterActions}>
                    <div className={styles.refreshSwitcher} role="group" aria-label={t('usage_stats.refresh')}>
                      <button
                        type="button"
                        className={`${styles.refreshPill} ${styles.refreshPillActive} ${manualRefreshLoading ? styles.refreshPillLoading : ''}`.trim()}
                        onClick={() => void handleManualRefresh()}
                        disabled={refreshDisabled}
                        aria-busy={manualRefreshLoading}
                      >
                        {manualRefreshLoading ? (
                          <span className={styles.refreshPillInner}>
                            <LoadingSpinner size={12} className={styles.refreshSpinner} />
                            <span>{t('common.loading')}</span>
                          </span>
                        ) : (
                          <span className={styles.refreshPillInner}>
                            <IconRefreshCw size={14} />
                            <span>{t('usage_stats.refresh')}</span>
                          </span>
                        )}
                      </button>
                    </div>
                  </div>
                </div>
              </div>
            </div>

            {displayError && <div className={styles.errorBox}>{displayError}</div>}

            <DailyAveragePanel usage={dailyAveragePanelUsage} loading={overviewDisplayLoading} reserveVisible={reserveDailyAveragePanel} />

            <StatCards
              usage={usage}
              loading={overviewDisplayLoading}
              sparklines={{
                requests: requestsSparkline,
                tokens: tokensSparkline,
                rpm: rpmSparkline,
                tpm: tpmSparkline,
                cachedRate: cachedRateSparkline,
                cost: costSparkline,
              }}
            />

            <ServiceHealthCard usage={usage} loading={overviewDisplayLoading} />

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
          </div>
        </main>
      </div>
    </div>
  );
}
