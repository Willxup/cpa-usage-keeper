import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Select, Space, Tag, Typography } from 'antd';
import { InfoCircleOutlined, KeyOutlined, LogoutOutlined, ReloadOutlined } from '@ant-design/icons';
import { ApiError, fetchKeyOverview, fetchKeyOverviewRealtime, logout } from '@/lib/api';
import type { AuthSessionAPIKeySummary, KeyOverviewTimeRange, OverviewRealtimeBlock, OverviewRealtimeWindow, UsageOverviewResponse } from '@/lib/types';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { PreferencesDropdown } from '@/components/ui/PreferencesDropdown';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import { useThemeStore } from '@/stores';
import {
  OverviewRealtimePanel,
  PricingCoverageNotice,
  ServiceHealthCard,
  StatCards,
} from '@/components/usage';
import type { UsageOverviewPayload } from '@/components/usage/hooks/useUsageData';
import { BrandLink } from '@/components/BrandLink';
import { AppShell, PageToolbar, ProductAbout } from '@/components/layout';
import { useEmbed } from '@/embed/EmbedContext';
import { getCurrentOverviewUsage, getDailyAverageUsage, getOverviewDisplayLoading, isDailyAverageRange } from '@/utils/usage/overview';
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
  const embedded = useEmbed();
  const isMobile = useMediaQuery('(max-width: 768px)');
  const resolvedTheme = useThemeStore((state) => state.resolvedTheme);
  const isDark = resolvedTheme === 'dark';
  const [timeRange, setTimeRange] = useState<KeyOverviewTimeRange>(loadTimeRange);
  const [realtimeWindow, setRealtimeWindow] = useState<OverviewRealtimeWindow>(loadRealtimeWindow);
  const [usage, setUsage] = useState<UsageOverviewPayload | null>(null);
  const [loadedUsageRange, setLoadedUsageRange] = useState<KeyOverviewTimeRange | null>(null);
  const [realtime, setRealtime] = useState<OverviewRealtimeBlock | null>(null);
  const [loading, setLoading] = useState(false);
  const [realtimeLoading, setRealtimeLoading] = useState(false);
  const [error, setError] = useState('');
  const [realtimeError, setRealtimeError] = useState('');
  const [manualRefreshLoading, setManualRefreshLoading] = useState(false);
  const [refreshThrottled, setRefreshThrottled] = useState(false);
  const [loggingOut, setLoggingOut] = useState(false);
  const [aboutOpen, setAboutOpen] = useState(false);
  const overviewRequestControllerRef = useRef<AbortController | null>(null);
  const realtimeRequestControllerRef = useRef<AbortController | null>(null);
  const refreshThrottleTimerRef = useRef<ReturnType<typeof window.setTimeout> | null>(null);

  const rangeOptions = useMemo(() => TIME_RANGE_OPTIONS.map((option) => ({
    value: option.value,
    label: t(option.labelKey),
  })), [t]);

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
      const overview = await fetchKeyOverview({ range: requestRange }, controller.signal);
      if (overviewRequestControllerRef.current !== controller) return;
      setUsage(overview as UsageOverviewResponse as UsageOverviewPayload);
      setLoadedUsageRange(requestRange);
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
  const showDailyAverages = isDailyAverageRange({ range: timeRange });
  const dailyAverageUsage = getDailyAverageUsage(currentOverviewUsage, usage, showDailyAverages, loading);
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
    <>
      <AppShell
      className={styles.viewerShell}
      variant={embedded ? 'embed' : 'viewer'}
      sticky="desktop"
      nav={{ mode: 'none' }}
      slots={{
        brand: <BrandLink className={styles.brandLink} />,
        headerTitle: t('usage_stats.tab_overview'),
        headerSubtitle: 'API Key Viewer',
        headerUtility: (
          <Space size={8} wrap>
            <Tag className={styles.identityTag} icon={<KeyOutlined />} color="success" title={identityLabel}>
              {identityLabel}
            </Tag>
            <PreferencesDropdown />
            <Button
              type="text"
              icon={<InfoCircleOutlined />}
              onClick={() => setAboutOpen(true)}
              aria-label={t('common.about_open')}
            >
              {t('common.about_title')}
            </Button>
            <Button
              type="text"
              danger
              icon={<LogoutOutlined />}
              onClick={() => void handleLogout()}
              loading={loggingOut}
            >
              {t('common.logout')}
            </Button>
          </Space>
        ),
        toolbar: (
          <PageToolbar
            leading={(
              <div className={styles.keyOverviewLeading}>
                <Typography.Text type="secondary">{identityLabel}</Typography.Text>
                <label className={styles.rangeField}>
                  <span className={styles.rangeLabel}>{t('usage_stats.range_filter')}</span>
                  <Select
                    value={timeRange}
                    options={rangeOptions}
                    onChange={(value) => setTimeRange(value as KeyOverviewTimeRange)}
                    className={styles.rangeSelectControl}
                    aria-label={t('usage_stats.range_filter')}
                  />
                </label>
              </div>
            )}
            actions={(
              <Button
                type="primary"
                icon={<ReloadOutlined />}
                onClick={() => void handleManualRefresh()}
                disabled={refreshThrottled}
                loading={manualRefreshLoading}
              >
                {t('usage_stats.refresh')}
              </Button>
            )}
          />
        ),
        content: (
          <>
            {loading && !usage && (
              <div className={styles.loadingOverlay} aria-busy="true">
                <div className={styles.loadingOverlayContent}>
                  <LoadingSpinner size={28} className={styles.loadingOverlaySpinner} />
                  <span className={styles.loadingOverlayText}>{t('common.loading')}</span>
                </div>
              </div>
            )}

            {displayError && <Alert type="error" message={displayError} showIcon />}

            <StatCards
              usage={usage}
              loading={overviewDisplayLoading}
              isDark={isDark}
              dailyAverageUsage={dailyAverageUsage}
              showDailyAverages={showDailyAverages}
            />

            <PricingCoverageNotice models={usage?.summary?.unpriced_models ?? []} />

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
          </>
        ),
      }}
      />
      <ProductAbout open={aboutOpen} onClose={() => setAboutOpen(false)} />
    </>
  );
}
