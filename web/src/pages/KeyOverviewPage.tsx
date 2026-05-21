import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ApiError, fetchKeyOverview, logout } from '@/lib/api';
import type { AuthSessionAPIKeySummary, KeyOverviewTimeRange, UsageOverviewResponse } from '@/lib/types';
import { LanguageSwitcher } from '@/components/ui/LanguageSwitcher';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { Select } from '@/components/ui/Select';
import { IconRefreshCw } from '@/components/ui/icons';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import { useThemeStore } from '@/stores';
import {
  ChartLineSelector,
  CostTrendChart,
  ServiceHealthCard,
  StatCards,
  TokenBreakdownChart,
  UsageChart,
  useChartData,
  useSparklines,
} from '@/components/usage';
import type { UsageOverviewPayload } from '@/components/usage/hooks/useUsageData';
import { BrandLink } from '@/components/BrandLink';
import {
  getModelNamesFromUsage,
  resolveUsageFilterWindow,
  sanitizeChartLines,
  type UsageFilterWindow,
} from '@/utils/usage';
import {
  getOverviewChartEndMs,
  getOverviewDisplayLoading,
  getOverviewHourWindowHours,
  getPreferredOverviewChartPeriod,
} from '@/utils/usage/overview';
import type { Theme } from '@/types';
import styles from './KeyOverviewPage.module.scss';

const KEY_OVERVIEW_RANGE_STORAGE_KEY = 'cli-proxy-key-overview-range-v1';
const KEY_OVERVIEW_CHART_LINES_STORAGE_KEY = 'cli-proxy-key-overview-chart-lines-v1';
const DEFAULT_TIME_RANGE: KeyOverviewTimeRange = '8h';
const DEFAULT_CHART_LINES = ['all'];
const MAX_CHART_LINES = 9;
const REFRESH_THROTTLE_MS = 1_000;

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

const toTimestampMs = (value: string | undefined): number | undefined => {
  if (!value) return undefined;
  const timestamp = Date.parse(value);
  return Number.isFinite(timestamp) ? timestamp : undefined;
};

const normalizeChartLines = (value: unknown, maxLines = MAX_CHART_LINES): string[] => {
  if (!Array.isArray(value)) return DEFAULT_CHART_LINES;
  const filtered = value
    .filter((item): item is string => typeof item === 'string')
    .map((item) => item.trim())
    .filter(Boolean)
    .slice(0, maxLines);
  return filtered.length ? filtered : DEFAULT_CHART_LINES;
};

const loadTimeRange = (): KeyOverviewTimeRange => {
  try {
    if (typeof localStorage === 'undefined') return DEFAULT_TIME_RANGE;
    const raw = localStorage.getItem(KEY_OVERVIEW_RANGE_STORAGE_KEY);
    return isKeyOverviewTimeRange(raw) ? raw : DEFAULT_TIME_RANGE;
  } catch {
    return DEFAULT_TIME_RANGE;
  }
};

const loadChartLines = (): string[] => {
  try {
    if (typeof localStorage === 'undefined') return DEFAULT_CHART_LINES;
    const raw = localStorage.getItem(KEY_OVERVIEW_CHART_LINES_STORAGE_KEY);
    return raw ? normalizeChartLines(JSON.parse(raw)) : DEFAULT_CHART_LINES;
  } catch {
    return DEFAULT_CHART_LINES;
  }
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
  const setTheme = useThemeStore((state) => state.setTheme);
  const isDark = resolvedTheme === 'dark';
  const [timeRange, setTimeRange] = useState<KeyOverviewTimeRange>(loadTimeRange);
  const [chartLines, setChartLines] = useState<string[]>(loadChartLines);
  const [usage, setUsage] = useState<UsageOverviewPayload | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [lastRefreshedAt, setLastRefreshedAt] = useState<Date | null>(null);
  const [manualRefreshLoading, setManualRefreshLoading] = useState(false);
  const [refreshThrottled, setRefreshThrottled] = useState(false);
  const [loggingOut, setLoggingOut] = useState(false);
  const requestControllerRef = useRef<AbortController | null>(null);
  const refreshLockedUntilRef = useRef(0);
  const refreshThrottleTimerRef = useRef<ReturnType<typeof window.setTimeout> | null>(null);

  const rangeOptions = useMemo(() => TIME_RANGE_OPTIONS.map((option) => ({
    value: option.value,
    label: t(option.labelKey),
  })), [t]);

  const themeOptions = useMemo(
    () => THEME_OPTIONS.map((option) => ({ ...option, label: t(option.labelKey) })),
    [t]
  );

  const loadOverview = useCallback(async ({ force = false }: { force?: boolean } = {}) => {
    if (force && Date.now() < refreshLockedUntilRef.current) return;
    requestControllerRef.current?.abort();
    const controller = new AbortController();
    requestControllerRef.current = controller;
    setLoading(true);
    setError('');
    try {
      const response = await fetchKeyOverview(timeRange, controller.signal);
      if (requestControllerRef.current !== controller) return;
      setUsage(response as UsageOverviewResponse as UsageOverviewPayload);
      setLastRefreshedAt(new Date());
      if (force) {
        refreshLockedUntilRef.current = Date.now() + REFRESH_THROTTLE_MS;
        setRefreshThrottled(true);
        if (refreshThrottleTimerRef.current !== null) {
          window.clearTimeout(refreshThrottleTimerRef.current);
        }
        refreshThrottleTimerRef.current = window.setTimeout(() => {
          refreshThrottleTimerRef.current = null;
          setRefreshThrottled(false);
        }, REFRESH_THROTTLE_MS);
      }
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
      if (requestControllerRef.current === controller) {
        setLoading(false);
        requestControllerRef.current = null;
      }
    }
  }, [onAuthRequired, timeRange]);

  useEffect(() => {
    void loadOverview();
    return () => {
      requestControllerRef.current?.abort();
      requestControllerRef.current = null;
    };
  }, [loadOverview]);

  useEffect(() => () => {
    if (refreshThrottleTimerRef.current !== null) {
      window.clearTimeout(refreshThrottleTimerRef.current);
      refreshThrottleTimerRef.current = null;
    }
  }, []);

  useEffect(() => {
    try {
      localStorage.setItem(KEY_OVERVIEW_RANGE_STORAGE_KEY, timeRange);
    } catch {
      // ignore storage failures
    }
  }, [timeRange]);

  useEffect(() => {
    try {
      localStorage.setItem(KEY_OVERVIEW_CHART_LINES_STORAGE_KEY, JSON.stringify(chartLines));
    } catch {
      // ignore storage failures
    }
  }, [chartLines]);

  const resolvedRangeEndMs = toTimestampMs(usage?.range_end);
  const filterWindow = useMemo<UsageFilterWindow>(() => {
    if (!usage) return {};
    return resolveUsageFilterWindow(usage.usage, timeRange, {
      nowMs: resolvedRangeEndMs ?? lastRefreshedAt?.getTime() ?? Date.now(),
    });
  }, [lastRefreshedAt, resolvedRangeEndMs, timeRange, usage]);

  const hourWindowHours = useMemo(
    () => getOverviewHourWindowHours({ timeRange, filterWindow }),
    [filterWindow, timeRange]
  );
  const filterWindowEndMs = getOverviewChartEndMs({
    timeRange,
    filterWindow,
    fallbackEndMs: lastRefreshedAt?.getTime() ?? Date.now(),
    resolvedRangeEndMs,
  });
  const includeFinalHourBucket = timeRange === 'today' || timeRange === 'yesterday';
  const preferredOverviewChartPeriod = getPreferredOverviewChartPeriod({
    windowMinutes: filterWindow.windowMinutes,
  });

  const overviewModelNames = useMemo(
    () => getModelNamesFromUsage(usage?.usage ?? null),
    [usage]
  );

  useEffect(() => {
    setChartLines((current) => {
      const next = sanitizeChartLines(current, overviewModelNames);
      if (next.length === current.length && next.every((line, index) => line === current[index])) {
        return current;
      }
      return next;
    });
  }, [overviewModelNames]);

  const overviewDisplayLoading = getOverviewDisplayLoading({ loading, hasUsage: Boolean(usage) });
  const {
    requestsSparkline,
    tokensSparkline,
    rpmSparkline,
    tpmSparkline,
    costSparkline,
  } = useSparklines({ usage, loading });
  const {
    requestsPeriod,
    tokensPeriod,
    requestsChartData,
    tokensChartData,
    requestsChartOptions,
    tokensChartOptions,
  } = useChartData({
    usage,
    chartLines,
    isDark,
    isMobile,
    hourWindowHours,
    endMs: filterWindowEndMs,
    includeFinalHourBucket,
    preferredPeriod: preferredOverviewChartPeriod,
  });

  const handleChartLinesChange = useCallback((lines: string[]) => {
    setChartLines(normalizeChartLines(lines));
  }, []);

  const refreshDisabled = manualRefreshLoading || loading || refreshThrottled;
  const handleManualRefresh = useCallback(async () => {
    if (refreshDisabled) return;
    setManualRefreshLoading(true);
    try {
      await loadOverview({ force: true });
    } finally {
      setManualRefreshLoading(false);
    }
  }, [loadOverview, refreshDisabled]);

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

            {displayError && <div className={styles.errorBox}>{displayError === 'AUTH_REQUIRED' ? t('auth.session_expired') : displayError}</div>}

            <StatCards
              usage={usage}
              loading={overviewDisplayLoading}
              sparklines={{
                requests: requestsSparkline,
                tokens: tokensSparkline,
                rpm: rpmSparkline,
                tpm: tpmSparkline,
                cost: costSparkline,
              }}
            />

            <ServiceHealthCard usage={usage} loading={overviewDisplayLoading} />

            <TokenBreakdownChart
              usage={usage}
              loading={overviewDisplayLoading}
              isDark={isDark}
              isMobile={isMobile}
              hourWindowHours={hourWindowHours}
              endMs={filterWindowEndMs}
              includeFinalHourBucket={includeFinalHourBucket}
              preferredPeriod={preferredOverviewChartPeriod}
            />

            <CostTrendChart
              usage={usage}
              loading={overviewDisplayLoading}
              isDark={isDark}
              isMobile={isMobile}
              hourWindowHours={hourWindowHours}
              endMs={filterWindowEndMs}
              includeFinalHourBucket={includeFinalHourBucket}
              preferredPeriod={preferredOverviewChartPeriod}
            />

            <ChartLineSelector
              chartLines={chartLines}
              modelNames={overviewModelNames}
              maxLines={MAX_CHART_LINES}
              onChange={handleChartLinesChange}
            />

            <div className={styles.chartsGrid}>
              <UsageChart
                title={t('usage_stats.requests_trend')}
                period={requestsPeriod}
                chartData={requestsChartData}
                chartOptions={requestsChartOptions}
                loading={overviewDisplayLoading}
                isMobile={isMobile}
                emptyText={t('usage_stats.no_data')}
              />
              <UsageChart
                title={t('usage_stats.tokens_trend')}
                period={tokensPeriod}
                chartData={tokensChartData}
                chartOptions={tokensChartOptions}
                loading={overviewDisplayLoading}
                isMobile={isMobile}
                emptyText={t('usage_stats.no_data')}
              />
            </div>
          </div>
        </main>
      </div>
    </div>
  );
}
