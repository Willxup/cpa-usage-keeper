import { useState, useMemo, useCallback, useEffect, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import i18n, { persistLanguage } from '@/i18n';
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler
} from 'chart.js';
import { ApiError, fetchStatus, fetchUsageAnalysis, fetchUsageCredentials, fetchUsageEvents } from '@/lib/api';
import type { StatusResponse, UsageAnalysisResponse, UsageCredential, UsageEvent } from '@/lib/types';
import { Button } from '@/components/ui/Button';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { Select } from '@/components/ui/Select';
import { IconRefreshCw } from '@/components/ui/icons';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import { useThemeStore, useConfigStore } from '@/stores';
import {
  StatCards,
  UsageChart,
  ChartLineSelector,
  ApiDetailsCard,
  ModelStatsCard,
  PriceSettingsCard,
  CredentialStatsCard,
  RequestEventsDetailsCard,
  TokenBreakdownChart,
  CostTrendChart,
  ServiceHealthCard,
  useUsageData,
  usePricingData,
  useSparklines,
  useChartData
} from '@/components/usage';
import {
  getModelNamesFromUsage,
  getApiStats,
  getModelStats,
  resolveUsageFilterWindow,
  type UsageFilterWindow,
  type UsageTimeRange
} from '@/utils/usage';
import type { Theme } from '@/types';
import styles from './UsagePage.module.scss';

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler
);

const CHART_LINES_STORAGE_KEY = 'cli-proxy-usage-chart-lines-v1';
const TIME_RANGE_STORAGE_KEY = 'cli-proxy-usage-time-range-v1';
const CUSTOM_TIME_RANGE_STORAGE_KEY = 'cli-proxy-usage-custom-range-v1';
const DEFAULT_CHART_LINES = ['all'];
const DEFAULT_TIME_RANGE: UsageTimeRange = '8h';
const DEFAULT_CUSTOM_WINDOW_HOURS = 8;
const MAX_CHART_LINES = 9;
const TIME_RANGE_OPTIONS: ReadonlyArray<{ value: Exclude<UsageTimeRange, 'all'>; labelKey: string }> = [
  { value: '4h', labelKey: 'usage_stats.range_4h' },
  { value: '8h', labelKey: 'usage_stats.range_8h' },
  { value: '12h', labelKey: 'usage_stats.range_12h' },
  { value: '24h', labelKey: 'usage_stats.range_24h' },
  { value: '7d', labelKey: 'usage_stats.range_7d' },
  { value: 'custom', labelKey: 'usage_stats.range_custom' },
];
const HOUR_WINDOW_BY_TIME_RANGE: Record<Exclude<UsageTimeRange, 'all' | 'custom'>, number> = {
  '4h': 4,
  '8h': 8,
  '12h': 12,
  '24h': 24,
  '7d': 7 * 24
};
const THEME_OPTIONS: ReadonlyArray<{ value: Theme; labelKey: string }> = [
  { value: 'white', labelKey: 'usage_stats.theme_light' },
  { value: 'dark', labelKey: 'usage_stats.theme_dark' },
  { value: 'auto', labelKey: 'usage_stats.theme_auto' }
];
const USAGE_TAB_OPTIONS = ['overview', 'analysis', 'events', 'credentials', 'pricing'] as const;
type UsageTab = (typeof USAGE_TAB_OPTIONS)[number];
const DEFAULT_USAGE_TAB: UsageTab = 'overview';
const USAGE_TAB_STORAGE_KEY = 'cli-proxy-usage-tab-v1';

const isUsageTimeRange = (value: unknown): value is UsageTimeRange =>
  value === '4h' || value === '8h' || value === '12h' || value === '24h' || value === '7d' || value === 'all' || value === 'custom';

const toDateInputValue = (timestamp: number): string => {
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return '';
  const pad = (value: number) => String(value).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
};

const parseCustomDateBoundary = (value: string, endOfDay: boolean): number | undefined => {
  if (!value) return undefined;
  const suffix = endOfDay ? 'T23:59:59.999Z' : 'T00:00:00.000Z';
  const timestamp = Date.parse(`${value}${suffix}`);
  return Number.isFinite(timestamp) ? timestamp : undefined;
};

const parseCustomDateStart = (value: string): number | undefined => parseCustomDateBoundary(value, false);

const parseCustomDateEnd = (value: string): number | undefined => parseCustomDateBoundary(value, true);

const buildDefaultCustomRange = (anchorMs: number) => ({
  start: toDateInputValue(anchorMs - DEFAULT_CUSTOM_WINDOW_HOURS * 60 * 60 * 1000),
  end: toDateInputValue(anchorMs)
});

const loadCustomTimeRange = () => {
  try {
    if (typeof localStorage === 'undefined') {
      return buildDefaultCustomRange(Date.now());
    }
    const raw = localStorage.getItem(CUSTOM_TIME_RANGE_STORAGE_KEY);
    if (!raw) {
      return buildDefaultCustomRange(Date.now());
    }
    const parsed = JSON.parse(raw) as { start?: string; end?: string };
    const start = typeof parsed?.start === 'string' ? parsed.start : '';
    const end = typeof parsed?.end === 'string' ? parsed.end : '';
    if (!start || !end) {
      return { start, end };
    }
    const startMs = parseCustomDateStart(start);
    const endMs = parseCustomDateEnd(end);
    if (startMs === undefined || endMs === undefined || startMs > endMs) {
      return buildDefaultCustomRange(Date.now());
    }
    return { start, end };
  } catch {
    return buildDefaultCustomRange(Date.now());
  }
};

const normalizeChartLines = (value: unknown, maxLines = MAX_CHART_LINES): string[] => {
  if (!Array.isArray(value)) {
    return DEFAULT_CHART_LINES;
  }

  const filtered = value
    .filter((item): item is string => typeof item === 'string')
    .map((item) => item.trim())
    .filter(Boolean)
    .slice(0, maxLines);

  return filtered.length ? filtered : DEFAULT_CHART_LINES;
};

const loadChartLines = (): string[] => {
  try {
    if (typeof localStorage === 'undefined') {
      return DEFAULT_CHART_LINES;
    }
    const raw = localStorage.getItem(CHART_LINES_STORAGE_KEY);
    if (!raw) {
      return DEFAULT_CHART_LINES;
    }
    return normalizeChartLines(JSON.parse(raw));
  } catch {
    return DEFAULT_CHART_LINES;
  }
};

const loadTimeRange = (): UsageTimeRange => {
  try {
    if (typeof localStorage === 'undefined') {
      return DEFAULT_TIME_RANGE;
    }
    const raw = localStorage.getItem(TIME_RANGE_STORAGE_KEY);
    if (!isUsageTimeRange(raw) || raw === 'all') {
      return DEFAULT_TIME_RANGE;
    }
    return raw;
  } catch {
    return DEFAULT_TIME_RANGE;
  }
};

const isUsageTab = (value: unknown): value is UsageTab =>
  typeof value === 'string' && USAGE_TAB_OPTIONS.includes(value as UsageTab);

const loadUsageTab = (): UsageTab => {
  try {
    if (typeof localStorage === 'undefined') {
      return DEFAULT_USAGE_TAB;
    }
    const raw = localStorage.getItem(USAGE_TAB_STORAGE_KEY);
    return isUsageTab(raw) ? raw : DEFAULT_USAGE_TAB;
  } catch {
    return DEFAULT_USAGE_TAB;
  }
};

export function UsagePage({ onAuthRequired }: { onAuthRequired?: () => void }) {
  const { t } = useTranslation();
  const currentLanguage = i18n.language === 'zh' ? 'zh' : 'en';
  const isMobile = useMediaQuery('(max-width: 768px)');
  const theme = useThemeStore((state) => state.theme);
  const resolvedTheme = useThemeStore((state) => state.resolvedTheme);
  const setTheme = useThemeStore((state) => state.setTheme);
  const isDark = resolvedTheme === 'dark';
  const config = useConfigStore((state) => state.config);

  const [activeTab, setActiveTab] = useState<UsageTab>(loadUsageTab);
  const [chartLines, setChartLines] = useState<string[]>(loadChartLines);
  const [timeRange, setTimeRange] = useState<UsageTimeRange>(loadTimeRange);
  const [customTimeRange, setCustomTimeRange] = useState<{ start: string; end: string }>(loadCustomTimeRange);
  const isOverviewTab = activeTab === 'overview';

  const {
    usage,
    loading,
    error,
    lastRefreshedAt,
    loadUsage
  } = useUsageData({
    onAuthRequired,
    range: timeRange,
    customStart: customTimeRange.start,
    customEnd: customTimeRange.end,
    enabled: activeTab === 'overview',
  });
  const {
    modelNames,
    modelPrices,
    loading: pricingLoading,
    error: pricingError,
    lastRefreshedAt: pricingLastRefreshedAt,
    loadPricing,
    setModelPrices,
  } = usePricingData({
    onAuthRequired,
    enabled: activeTab === 'pricing',
  });
  const [statusError, setStatusError] = useState('');
  const [customRangeError, setCustomRangeError] = useState('');
  const [customRangeHint, setCustomRangeHint] = useState('');
  const [eventsLoading, setEventsLoading] = useState(false);
  const [eventsError, setEventsError] = useState('');
  const [eventsData, setEventsData] = useState<UsageEvent[]>([]);
  const eventsRequestControllerRef = useRef<AbortController | null>(null);
  const [credentialsLoading, setCredentialsLoading] = useState(false);
  const [credentialsError, setCredentialsError] = useState('');
  const [credentialsData, setCredentialsData] = useState<UsageCredential[]>([]);
  const credentialsRequestControllerRef = useRef<AbortController | null>(null);
  const [analysisLoading, setAnalysisLoading] = useState(false);
  const [analysisError, setAnalysisError] = useState('');
  const [analysisData, setAnalysisData] = useState<UsageAnalysisResponse>({ apis: [], models: [] });
  const [analysisLastRefreshedAt, setAnalysisLastRefreshedAt] = useState<Date | null>(null);
  const analysisRequestControllerRef = useRef<AbortController | null>(null);

  const timeRangeOptions = useMemo(
    () =>
      TIME_RANGE_OPTIONS.map((opt) => ({
        value: opt.value,
        label: t(opt.labelKey)
      })),
    [t]
  );
  const themeOptions = useMemo(
    () =>
      THEME_OPTIONS.map((option) => ({
        ...option,
        label: t(option.labelKey)
      })),
    [t]
  );

  const filterWindow = useMemo<UsageFilterWindow>(() => {
    if (!usage) return {};
    return resolveUsageFilterWindow(usage, timeRange, {
      nowMs: lastRefreshedAt?.getTime() ?? Date.now(),
      customStart:
        timeRange === 'custom' ? parseCustomDateStart(customTimeRange.start) : customTimeRange.start,
      customEnd:
        timeRange === 'custom' ? parseCustomDateEnd(customTimeRange.end) : customTimeRange.end
    });
  }, [customTimeRange.end, customTimeRange.start, lastRefreshedAt, timeRange, usage]);

  useEffect(() => {
    if (timeRange !== 'custom') {
      setCustomRangeError('');
      setCustomRangeHint('');
      return;
    }
    if (!customTimeRange.start || !customTimeRange.end) {
      setCustomRangeError('');
      setCustomRangeHint(t('usage_stats.custom_incomplete'));
      return;
    }
    const startMs = parseCustomDateStart(customTimeRange.start);
    const endMs = parseCustomDateEnd(customTimeRange.end);
    if (startMs === undefined || endMs === undefined || startMs > endMs) {
      setCustomRangeHint('');
      setCustomRangeError(t('usage_stats.custom_invalid'));
      return;
    }
    setCustomRangeError('');
    setCustomRangeHint('');
  }, [customTimeRange.end, customTimeRange.start, t, timeRange]);

  const filteredUsage = usage;

  const loadAnalysis = useCallback(async () => {
    if (timeRange === 'custom') {
      if (!customTimeRange.start || !customTimeRange.end) {
        analysisRequestControllerRef.current?.abort();
        analysisRequestControllerRef.current = null;
        setAnalysisData({ apis: [], models: [] });
        setAnalysisError('');
        setAnalysisLoading(false);
        return;
      }
      const startMs = parseCustomDateStart(customTimeRange.start);
      const endMs = parseCustomDateEnd(customTimeRange.end);
      if (startMs === undefined || endMs === undefined || startMs > endMs) {
        analysisRequestControllerRef.current?.abort();
        analysisRequestControllerRef.current = null;
        setAnalysisData({ apis: [], models: [] });
        setAnalysisError('');
        setAnalysisLoading(false);
        return;
      }
    }

    analysisRequestControllerRef.current?.abort();
    const controller = new AbortController();
    analysisRequestControllerRef.current = controller;

    setAnalysisLoading(true);
    setAnalysisError('');
    setAnalysisData({ apis: [], models: [] });
    try {
      const start = timeRange === 'custom' ? new Date(`${customTimeRange.start}T00:00:00.000Z`).toISOString() : undefined;
      const end = timeRange === 'custom' ? new Date(`${customTimeRange.end}T23:59:59.999Z`).toISOString() : undefined;
      const response = await fetchUsageAnalysis(timeRange, start, end, controller.signal);
      if (analysisRequestControllerRef.current !== controller) {
        return;
      }
      setAnalysisData(response);
      setAnalysisLastRefreshedAt(new Date());
    } catch (error) {
      if (controller.signal.aborted) {
        return;
      }
      if (analysisRequestControllerRef.current === controller) {
        setAnalysisData({ apis: [], models: [] });
      }
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.();
        return;
      }
      setAnalysisError(error instanceof Error ? error.message : 'Failed to load usage analysis');
    } finally {
      if (analysisRequestControllerRef.current === controller) {
        setAnalysisLoading(false);
        analysisRequestControllerRef.current = null;
      }
    }
  }, [customTimeRange.end, customTimeRange.start, onAuthRequired, timeRange]);
  const hourWindowHours = useMemo(() => {
    if (timeRange === 'all') return undefined;
    if (timeRange !== 'custom') return HOUR_WINDOW_BY_TIME_RANGE[timeRange];
    if (filterWindow.windowMinutes === undefined) return undefined;
    return Math.max(Math.ceil(filterWindow.windowMinutes / 60), 1);
  }, [filterWindow.windowMinutes, timeRange]);
  const filterWindowEndMs = filterWindow.endMs ?? lastRefreshedAt?.getTime() ?? Date.now();

  const handleChartLinesChange = useCallback((lines: string[]) => {
    setChartLines(normalizeChartLines(lines));
  }, []);

  useEffect(() => {
    try {
      if (typeof localStorage === 'undefined') {
        return;
      }
      localStorage.setItem(CHART_LINES_STORAGE_KEY, JSON.stringify(chartLines));
    } catch {
      // Ignore storage errors.
    }
  }, [chartLines]);

  useEffect(() => {
    try {
      if (typeof localStorage === 'undefined') {
        return;
      }
      localStorage.setItem(TIME_RANGE_STORAGE_KEY, timeRange);
    } catch {
      // Ignore storage errors.
    }
  }, [timeRange]);

  useEffect(() => {
    try {
      if (typeof localStorage === 'undefined') {
        return;
      }
      localStorage.setItem(CUSTOM_TIME_RANGE_STORAGE_KEY, JSON.stringify(customTimeRange));
    } catch {
      // Ignore storage errors.
    }
  }, [customTimeRange]);

  useEffect(() => {
    try {
      if (typeof localStorage === 'undefined') {
        return;
      }
      localStorage.setItem(USAGE_TAB_STORAGE_KEY, activeTab);
    } catch {
      // Ignore storage errors.
    }
  }, [activeTab]);

  useEffect(() => {
    if (timeRange !== 'custom') return;
    if (customTimeRange.start && customTimeRange.end) return;
    const anchorMs = lastRefreshedAt?.getTime() ?? Date.now();
    setCustomTimeRange(buildDefaultCustomRange(anchorMs));
  }, [customTimeRange.end, customTimeRange.start, lastRefreshedAt, timeRange]);

  useEffect(() => {
    let cancelled = false;
    const loadStatus = async () => {
      try {
        const status: StatusResponse = await fetchStatus();
        if (cancelled) return;
        setStatusError(status.last_error || '');
      } catch (error) {
        if (cancelled) return;
        if (error instanceof ApiError && error.status === 401) {
          onAuthRequired?.();
          return;
        }
      }
    };
    void loadStatus();
    const timer = window.setInterval(() => {
      void loadStatus();
    }, 30_000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [onAuthRequired]);

  const loadEvents = useCallback(async () => {
    if (timeRange === 'custom') {
      if (!customTimeRange.start || !customTimeRange.end) {
        eventsRequestControllerRef.current?.abort();
        eventsRequestControllerRef.current = null;
        setEventsData([]);
        setEventsError('');
        setEventsLoading(false);
        return;
      }
      const startMs = parseCustomDateStart(customTimeRange.start);
      const endMs = parseCustomDateEnd(customTimeRange.end);
      if (startMs === undefined || endMs === undefined || startMs > endMs) {
        eventsRequestControllerRef.current?.abort();
        eventsRequestControllerRef.current = null;
        setEventsData([]);
        setEventsError('');
        setEventsLoading(false);
        return;
      }
    }

    eventsRequestControllerRef.current?.abort();
    const controller = new AbortController();
    eventsRequestControllerRef.current = controller;

    setEventsLoading(true);
    setEventsError('');
    setEventsData([]);
    try {
      const start = timeRange === 'custom' ? new Date(`${customTimeRange.start}T00:00:00.000Z`).toISOString() : undefined;
      const end = timeRange === 'custom' ? new Date(`${customTimeRange.end}T23:59:59.999Z`).toISOString() : undefined;
      const response = await fetchUsageEvents(timeRange, start, end, controller.signal);
      if (eventsRequestControllerRef.current !== controller) {
        return;
      }
      setEventsData(response.events);
    } catch (error) {
      if (controller.signal.aborted) {
        return;
      }
      if (eventsRequestControllerRef.current === controller) {
        setEventsData([]);
      }
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.();
        return;
      }
      setEventsError(error instanceof Error ? error.message : 'Failed to load usage events');
    } finally {
      if (eventsRequestControllerRef.current === controller) {
        setEventsLoading(false);
        eventsRequestControllerRef.current = null;
      }
    }
  }, [customTimeRange.end, customTimeRange.start, onAuthRequired, timeRange]);

  const loadCredentials = useCallback(async () => {
    if (timeRange === 'custom') {
      if (!customTimeRange.start || !customTimeRange.end) {
        credentialsRequestControllerRef.current?.abort();
        credentialsRequestControllerRef.current = null;
        setCredentialsData([]);
        setCredentialsError('');
        setCredentialsLoading(false);
        return;
      }
      const startMs = parseCustomDateStart(customTimeRange.start);
      const endMs = parseCustomDateEnd(customTimeRange.end);
      if (startMs === undefined || endMs === undefined || startMs > endMs) {
        credentialsRequestControllerRef.current?.abort();
        credentialsRequestControllerRef.current = null;
        setCredentialsData([]);
        setCredentialsError('');
        setCredentialsLoading(false);
        return;
      }
    }

    credentialsRequestControllerRef.current?.abort();
    const controller = new AbortController();
    credentialsRequestControllerRef.current = controller;

    setCredentialsLoading(true);
    setCredentialsError('');
    setCredentialsData([]);
    try {
      const start = timeRange === 'custom' ? new Date(`${customTimeRange.start}T00:00:00.000Z`).toISOString() : undefined;
      const end = timeRange === 'custom' ? new Date(`${customTimeRange.end}T23:59:59.999Z`).toISOString() : undefined;
      const response = await fetchUsageCredentials(timeRange, start, end, controller.signal);
      if (credentialsRequestControllerRef.current !== controller) {
        return;
      }
      setCredentialsData(response.credentials);
    } catch (error) {
      if (controller.signal.aborted) {
        return;
      }
      if (credentialsRequestControllerRef.current === controller) {
        setCredentialsData([]);
      }
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.();
        return;
      }
      setCredentialsError(error instanceof Error ? error.message : 'Failed to load usage credentials');
    } finally {
      if (credentialsRequestControllerRef.current === controller) {
        setCredentialsLoading(false);
        credentialsRequestControllerRef.current = null;
      }
    }
  }, [customTimeRange.end, customTimeRange.start, onAuthRequired, timeRange]);

  useHeaderRefresh(
    activeTab === 'events'
      ? loadEvents
      : activeTab === 'credentials'
        ? loadCredentials
        : activeTab === 'analysis'
          ? loadAnalysis
          : activeTab === 'pricing'
            ? loadPricing
            : loadUsage
  );

  useEffect(() => {
    if (activeTab !== 'events') {
      eventsRequestControllerRef.current?.abort();
      eventsRequestControllerRef.current = null;
      setEventsLoading(false);
      return;
    }
    void loadEvents();
    return () => {
      eventsRequestControllerRef.current?.abort();
      eventsRequestControllerRef.current = null;
    };
  }, [activeTab, loadEvents]);

  useEffect(() => {
    if (activeTab !== 'credentials') {
      credentialsRequestControllerRef.current?.abort();
      credentialsRequestControllerRef.current = null;
      setCredentialsLoading(false);
      return;
    }
    void loadCredentials();
    return () => {
      credentialsRequestControllerRef.current?.abort();
      credentialsRequestControllerRef.current = null;
    };
  }, [activeTab, loadCredentials]);

  useEffect(() => {
    if (activeTab !== 'analysis') {
      analysisRequestControllerRef.current?.abort();
      analysisRequestControllerRef.current = null;
      setAnalysisLoading(false);
      return;
    }
    void loadAnalysis();
    return () => {
      analysisRequestControllerRef.current?.abort();
      analysisRequestControllerRef.current = null;
    };
  }, [activeTab, loadAnalysis]);

  const handleLanguageChange = useCallback(async (language: 'en' | 'zh') => {
    if (currentLanguage === language) return;
    await i18n.changeLanguage(language);
    persistLanguage(language);
  }, [currentLanguage]);

  const activeLastRefreshedAt = activeTab === 'analysis'
    ? analysisLastRefreshedAt
    : activeTab === 'pricing'
      ? pricingLastRefreshedAt
      : lastRefreshedAt;
  const nowMs = filterWindowEndMs;

  const {
    requestsSparkline,
    tokensSparkline,
    rpmSparkline,
    tpmSparkline,
    costSparkline
  } = useSparklines({ usage: filteredUsage, loading, nowMs, timeRange, hourWindowHours, modelPrices });

  const {
    requestsPeriod,
    setRequestsPeriod,
    tokensPeriod,
    setTokensPeriod,
    requestsChartData,
    tokensChartData,
    requestsChartOptions,
    tokensChartOptions
  } = useChartData({ usage: filteredUsage, chartLines, isDark, isMobile, hourWindowHours, endMs: filterWindowEndMs });

  const overviewModelNames = useMemo(() => getModelNamesFromUsage(usage), [usage]);
  const apiStats = useMemo(
    () => analysisData.apis.map((api) => ({
      endpoint: api.api_key,
      displayName: api.display_name || api.api_key,
      totalRequests: api.total_requests,
      successCount: api.success_count,
      failureCount: api.failure_count,
      totalTokens: api.total_tokens,
      totalCost: api.models.reduce((sum, model) => {
        const pricing = modelPrices[model.model];
        if (!pricing) return sum;
        const cachedTokens = Math.max(Number(model.cached_tokens) || 0, 0);
        const inputTokens = Math.max(Number(model.input_tokens) || 0, 0);
        const outputTokens = Math.max(Number(model.output_tokens) || 0, 0);
        const promptTokens = Math.max(inputTokens - cachedTokens, 0);
        return sum + ((promptTokens / 1_000_000) * pricing.prompt) + ((outputTokens / 1_000_000) * pricing.completion) + ((cachedTokens / 1_000_000) * pricing.cache);
      }, 0),
      models: Object.fromEntries(api.models.map((model) => [model.model, {
        requests: model.total_requests,
        successCount: model.success_count,
        failureCount: model.failure_count,
        tokens: model.total_tokens,
      }]))
    })),
    [analysisData.apis, modelPrices]
  );
  const modelStats = useMemo(
    () => analysisData.models.map((model) => {
      const pricing = modelPrices[model.model];
      const cachedTokens = Math.max(Number(model.cached_tokens) || 0, 0);
      const inputTokens = Math.max(Number(model.input_tokens) || 0, 0);
      const outputTokens = Math.max(Number(model.output_tokens) || 0, 0);
      const promptTokens = Math.max(inputTokens - cachedTokens, 0);
      const cost = pricing
        ? ((promptTokens / 1_000_000) * pricing.prompt) + ((outputTokens / 1_000_000) * pricing.completion) + ((cachedTokens / 1_000_000) * pricing.cache)
        : 0;
      return {
        model: model.model,
        requests: model.total_requests,
        successCount: model.success_count,
        failureCount: model.failure_count,
        tokens: model.total_tokens,
        averageLatencyMs: model.latency_sample_count > 0 ? model.total_latency_ms / model.latency_sample_count : null,
        totalLatencyMs: model.latency_sample_count > 0 ? model.total_latency_ms : null,
        latencySampleCount: model.latency_sample_count,
        cost,
      };
    }),
    [analysisData.models, modelPrices]
  );
  const hasPrices = Object.keys(modelPrices).length > 0;

  return (
    <div className={styles.pageShell}>
      <div className={styles.pageFrame}>
        <header className={styles.topBar}>
          <div className={styles.brandBlock}>
            <span className={styles.eyebrow}>CPA Usage Keeper</span>
          </div>
          <div className={styles.topBarActions}>
            <div className={styles.languageSwitcher} role="group" aria-label={t('usage_stats.language_switch')}>
              <button
                type="button"
                className={`${styles.languagePill} ${currentLanguage === 'en' ? styles.languagePillActive : ''}`.trim()}
                onClick={() => void handleLanguageChange('en')}
                aria-pressed={currentLanguage === 'en'}
                title={t('usage_stats.language_switch')}
              >
                EN
              </button>
              <button
                type="button"
                className={`${styles.languagePill} ${currentLanguage === 'zh' ? styles.languagePillActive : ''}`.trim()}
                onClick={() => void handleLanguageChange('zh')}
                aria-pressed={currentLanguage === 'zh'}
                title={t('usage_stats.language_switch')}
              >
                中
              </button>
            </div>
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
          </div>
        </header>

        <main className={styles.contentColumn}>
          <div className={styles.container}>
            {loading && !usage && activeTab === 'overview' && (
              <div className={styles.loadingOverlay} aria-busy="true">
                <div className={styles.loadingOverlayContent}>
                  <LoadingSpinner size={28} className={styles.loadingOverlaySpinner} />
                  <span className={styles.loadingOverlayText}>{t('common.loading')}</span>
                </div>
              </div>
            )}

            <div className={styles.toolbarRow}>
              <div className={styles.toolbarActionsRight}>
                <div className={styles.timeRangeGroup}>
                  <span className={styles.timeRangeLabel}>{t('usage_stats.range_filter')}</span>
                  <Select
                    value={timeRange}
                    options={timeRangeOptions}
                    onChange={(value) => setTimeRange(value as UsageTimeRange)}
                    className={styles.timeRangeSelectControl}
                    ariaLabel={t('usage_stats.range_filter')}
                    fullWidth={false}
                  />
                  {timeRange === 'custom' && (
                    <div className={styles.customRangeInline}>
                      <div className={styles.customRangeFields}>
                        <label className={styles.customRangeField}>
                          <span className={styles.customRangeFieldLabel}>{t('usage_stats.custom_start')}</span>
                          <input
                            type="date"
                            className={`input ${styles.customRangeInput}`}
                            value={customTimeRange.start}
                            onChange={(event) =>
                              setCustomTimeRange((current) => ({
                                ...current,
                                start: event.target.value
                              }))
                            }
                            aria-label={t('usage_stats.custom_start')}
                          />
                        </label>
                        <span className={styles.customRangeSeparator} aria-hidden="true">—</span>
                        <label className={styles.customRangeField}>
                          <span className={styles.customRangeFieldLabel}>{t('usage_stats.custom_end')}</span>
                          <input
                            type="date"
                            className={`input ${styles.customRangeInput}`}
                            value={customTimeRange.end}
                            onChange={(event) =>
                              setCustomTimeRange((current) => ({
                                ...current,
                                end: event.target.value
                              }))
                            }
                            aria-label={t('usage_stats.custom_end')}
                          />
                        </label>
                      </div>
                    </div>
                  )}
                </div>
                {timeRange === 'custom' && customRangeHint && (
                  <span className={styles.customRangeHint}>{customRangeHint}</span>
                )}
                {timeRange === 'custom' && customRangeError && (
                  <span className={styles.customRangeError}>{customRangeError}</span>
                )}
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => void (activeTab === 'events' ? loadEvents() : activeTab === 'credentials' ? loadCredentials() : activeTab === 'analysis' ? loadAnalysis() : activeTab === 'pricing' ? loadPricing() : loadUsage()).catch(() => {})}
                  disabled={activeTab === 'events' ? eventsLoading : activeTab === 'credentials' ? credentialsLoading : activeTab === 'analysis' ? analysisLoading : activeTab === 'pricing' ? pricingLoading : loading}
                  className={styles.refreshButton}
                >
                  <IconRefreshCw size={14} />
                  <span>{(activeTab === 'events' ? eventsLoading : activeTab === 'credentials' ? credentialsLoading : activeTab === 'analysis' ? analysisLoading : activeTab === 'pricing' ? pricingLoading : loading) ? t('common.loading') : t('usage_stats.refresh')}</span>
                </Button>
              </div>
              {activeLastRefreshedAt && (
                <span className={styles.lastRefreshed}>
                  {t('usage_stats.last_updated')}: {activeLastRefreshedAt.toLocaleTimeString()}
                </span>
              )}
            </div>

            {activeTab === 'overview' && error && <div className={styles.errorBox}>{error === 'AUTH_REQUIRED' ? t('auth.session_expired') : error}</div>}
            {activeTab === 'pricing' && pricingError && <div className={styles.errorBox}>{pricingError === 'AUTH_REQUIRED' ? t('auth.session_expired') : pricingError}</div>}
            {!(activeTab === 'overview' ? error : activeTab === 'pricing' ? pricingError : '') && statusError && <div className={styles.errorBox}>{statusError}</div>}

            <div className={styles.tabBar} role="tablist" aria-label="Usage sections">
              <button
                type="button"
                role="tab"
                aria-selected={activeTab === 'overview'}
                className={`${styles.tabPill} ${activeTab === 'overview' ? styles.tabPillActive : ''}`.trim()}
                onClick={() => setActiveTab('overview')}
              >
                Overview
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={activeTab === 'analysis'}
                className={`${styles.tabPill} ${activeTab === 'analysis' ? styles.tabPillActive : ''}`.trim()}
                onClick={() => setActiveTab('analysis')}
              >
                API & Models
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={activeTab === 'events'}
                className={`${styles.tabPill} ${activeTab === 'events' ? styles.tabPillActive : ''}`.trim()}
                onClick={() => setActiveTab('events')}
              >
                Request Events
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={activeTab === 'credentials'}
                className={`${styles.tabPill} ${activeTab === 'credentials' ? styles.tabPillActive : ''}`.trim()}
                onClick={() => setActiveTab('credentials')}
              >
                Credentials
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={activeTab === 'pricing'}
                className={`${styles.tabPill} ${activeTab === 'pricing' ? styles.tabPillActive : ''}`.trim()}
                onClick={() => setActiveTab('pricing')}
              >
                Pricing
              </button>
            </div>

            {activeTab === 'overview' && (
              <>
                <StatCards
                  usage={filteredUsage}
                  loading={loading}
                  modelPrices={modelPrices}
                  nowMs={nowMs}
                  filterWindow={filterWindow}
                  sparklines={{
                    requests: requestsSparkline,
                    tokens: tokensSparkline,
                    rpm: rpmSparkline,
                    tpm: tpmSparkline,
                    cost: costSparkline
                  }}
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
                    onPeriodChange={setRequestsPeriod}
                    chartData={requestsChartData}
                    chartOptions={requestsChartOptions}
                    loading={loading}
                    isMobile={isMobile}
                    emptyText={t('usage_stats.no_data')}
                  />
                  <UsageChart
                    title={t('usage_stats.tokens_trend')}
                    period={tokensPeriod}
                    onPeriodChange={setTokensPeriod}
                    chartData={tokensChartData}
                    chartOptions={tokensChartOptions}
                    loading={loading}
                    isMobile={isMobile}
                    emptyText={t('usage_stats.no_data')}
                  />
                </div>
              </>
            )}

            {activeTab === 'analysis' && (
              <>
                {analysisError && <div className={styles.errorBox}>{analysisError}</div>}
                <div className={styles.detailsGrid}>
                  <ApiDetailsCard apiStats={apiStats} loading={analysisLoading} hasPrices={hasPrices} />
                  <ModelStatsCard modelStats={modelStats} loading={analysisLoading} hasPrices={hasPrices} />
                </div>
              </>
            )}

            {activeTab === 'events' && (
              <>
                {eventsError && <div className={styles.errorBox}>{eventsError}</div>}
                <RequestEventsDetailsCard
                  events={eventsData}
                  loading={eventsLoading}
                />
              </>
            )}

            {activeTab === 'credentials' && (
              <>
                {credentialsError && <div className={styles.errorBox}>{credentialsError}</div>}
                <CredentialStatsCard
                  credentials={credentialsData}
                  loading={credentialsLoading}
                />
              </>
            )}

            {activeTab === 'pricing' && (
              <PriceSettingsCard
                modelNames={modelNames}
                modelPrices={modelPrices}
                onPricesChange={setModelPrices}
                loading={pricingLoading}
              />
            )}
          </div>
        </main>
      </div>
    </div>
  );
}
