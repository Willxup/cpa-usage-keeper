import { useState, useMemo, useCallback, useEffect, useRef, type KeyboardEvent, type SyntheticEvent } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  BarElement,
  BarController,
  Title,
  Tooltip,
  Legend,
  Filler
} from 'chart.js';
import { ApiError, fetchStatus, fetchUpdateCheck, fetchUsageAnalysis, fetchUsageEventModelFilterOptions, fetchUsageEventSourceFilterOptions, fetchUsageEvents } from '@/lib/api';
import type { StatusResponse, UsageAnalysisResponse, UsageEvent, UsageSourceFilterOption } from '@/lib/types';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { LanguageSwitcher } from '@/components/ui/LanguageSwitcher';
import { Select } from '@/components/ui/Select';
import { IconRefreshCw } from '@/components/ui/icons';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import { useThemeStore } from '@/stores';
import {
  StatCards,
  UsageChart,
  ChartLineSelector,
  ApiDetailsCard,
  ModelStatsCard,
  PriceSettingsCard,
  AuthFileCredentialsSection,
  AiProviderCredentialsSection,
  RequestEventsDetailsCard,
  TokenBreakdownChart,
  CostTrendChart,
  ServiceHealthCard,
  useUsageData,
  usePricingData,
  useSparklines,
  useChartData,
  useCredentialsTabData
} from '@/components/usage';
import {
  getModelNamesFromUsage,
  resolveUsageFilterWindow,
  sanitizeChartLines,
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
  BarElement,
  BarController,
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
  { value: 'today', labelKey: 'usage_stats.range_today' },
  { value: '7d', labelKey: 'usage_stats.range_7d' },
  { value: '30d', labelKey: 'usage_stats.range_30d' },
  { value: 'custom', labelKey: 'usage_stats.range_custom' },
];
const HOUR_WINDOW_BY_TIME_RANGE: Record<Extract<UsageTimeRange, '4h' | '8h' | '12h' | '24h' | '7d' | '30d'>, number> = {
  '4h': 4,
  '8h': 8,
  '12h': 12,
  '24h': 24,
  '7d': 7 * 24,
  '30d': 30 * 24
};
const THEME_OPTIONS: ReadonlyArray<{ value: Theme; labelKey: string }> = [
  { value: 'white', labelKey: 'usage_stats.theme_light' },
  { value: 'dark', labelKey: 'usage_stats.theme_dark' },
  { value: 'auto', labelKey: 'usage_stats.theme_auto' }
];
const USAGE_TAB_OPTIONS = ['overview', 'credentials', 'events', 'analysis', 'pricing'] as const;
type UsageTab = (typeof USAGE_TAB_OPTIONS)[number];
type Translate = (key: string) => string;
const USAGE_TAB_LABEL_KEYS: Record<UsageTab, string> = {
  overview: 'usage_stats.tab_overview',
  analysis: 'usage_stats.tab_analysis',
  events: 'usage_stats.tab_events',
  credentials: 'usage_stats.tab_credentials',
  pricing: 'usage_stats.tab_pricing',
};
const DEFAULT_USAGE_TAB: UsageTab = 'overview';
const USAGE_TAB_STORAGE_KEY = 'cli-proxy-usage-tab-v1';
const REQUEST_EVENTS_PAGE_SIZES = [20, 50, 100, 500, 1000] as const;
const REQUEST_EVENTS_DEFAULT_PAGE_SIZE = 100;
const ALL_REQUEST_EVENTS_FILTER = '__all__';
const OVERVIEW_AUTO_REFRESH_INTERVAL_MS = 10_000;

export const shouldShowRangeControls = (tab: UsageTab) => tab !== 'pricing' && tab !== 'credentials';

export const shouldShowUpdateCheckButton = (status: Pick<StatusResponse, 'updateCheckEnabled'> | null) => status?.updateCheckEnabled === true;

export const getUpdateCheckToastDuration = (kind: 'success' | 'info' | 'error') => (kind === 'error' ? 6_000 : 4_000);

export const shouldAutoRefreshUsageTab = ({
  activeTab,
  eventsPage,
  authFilePage,
  aiProviderPage,
}: {
  activeTab: UsageTab;
  eventsPage: number;
  authFilePage: number;
  aiProviderPage: number;
}) => {
  if (activeTab === 'overview') return true;
  if (activeTab === 'events') return eventsPage === 1;
  if (activeTab === 'credentials') return authFilePage === 1 && aiProviderPage === 1;
  return false;
};

type RequestEventFilterState = {
  model: string;
  source: string;
  result: string;
};

type RequestEventFilterOptionsState = {
  models: string[];
  sources: UsageSourceFilterOption[];
};

type RefreshPageDataOptions = {
  refreshActiveTab: () => Promise<void>;
  triggerBackendSync?: () => Promise<void>;
};

type OverviewAutoRefreshDocument = Pick<Document, 'visibilityState' | 'addEventListener' | 'removeEventListener'>;

type OverviewAutoRefreshOptions = {
  enabled: boolean;
  refreshOverview: () => void | Promise<void>;
  documentRef?: OverviewAutoRefreshDocument;
  intervalMs?: number;
};

type SyncCpaDataOptions = {
  triggerBackendSync: () => Promise<StatusResponse>;
  refreshActiveTab: () => Promise<void>;
  refreshStatus: () => Promise<StatusResponse>;
  onStatus: (status: StatusResponse) => void;
};

export const refreshPageData = async ({ refreshActiveTab }: RefreshPageDataOptions) => {
  await refreshActiveTab();
};

export const getOverviewDisplayLoading = ({ loading, hasUsage }: { loading: boolean; hasUsage: boolean }) => loading && !hasUsage;

export const scheduleOverviewAutoRefresh = ({
  enabled,
  refreshOverview,
  documentRef,
  intervalMs = OVERVIEW_AUTO_REFRESH_INTERVAL_MS,
}: OverviewAutoRefreshOptions) => {
  if (!enabled) {
    return () => undefined;
  }

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
  const refreshIfVisible = () => {
    if (targetDocument.visibilityState === 'hidden') {
      stopTimer();
      return;
    }
    void refreshOverview();
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
    void refreshOverview();
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

export const syncCpaData = async ({ triggerBackendSync, refreshActiveTab, refreshStatus, onStatus }: SyncCpaDataOptions) => {
  try {
    await triggerBackendSync();
    await refreshActiveTab();
    const nextStatus = await refreshStatus();
    onStatus(nextStatus);
  } catch (error) {
    if (!(error instanceof ApiError && error.status === 401)) {
      try {
        const nextStatus = await refreshStatus();
        onStatus(nextStatus);
      } catch {
        // 忽略状态刷新失败，继续抛出原始同步错误。
      }
    }
    throw error;
  }
};

export const sanitizeRequestEventFilters = (
  filters: RequestEventFilterState,
  options: RequestEventFilterOptionsState,
): RequestEventFilterState => {
  const model = filters.model === ALL_REQUEST_EVENTS_FILTER || options.models.includes(filters.model)
    ? filters.model
    : ALL_REQUEST_EVENTS_FILTER;
  const source = filters.source === ALL_REQUEST_EVENTS_FILTER || options.sources.some((option) => option.value === filters.source)
    ? filters.source
    : ALL_REQUEST_EVENTS_FILTER;
  const result = filters.result === 'success' || filters.result === 'failed'
    ? filters.result
    : ALL_REQUEST_EVENTS_FILTER;

  return { model, source, result };
};

const isUsageTimeRange = (value: unknown): value is UsageTimeRange =>
  value === '4h' || value === '8h' || value === '12h' || value === '24h' || value === 'today' || value === '7d' || value === '30d' || value === 'all' || value === 'custom';

const toDateInputValue = (timestamp: number): string => {
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return '';
  const pad = (value: number) => String(value).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
};

const toDateInputValueInTimezone = (timestamp: number, timezone?: string): string => {
  if (!timezone) return toDateInputValue(timestamp);
  try {
    const parts = new Intl.DateTimeFormat('en-US', {
      timeZone: timezone,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    }).formatToParts(new Date(timestamp));
    const year = parts.find((part) => part.type === 'year')?.value;
    const month = parts.find((part) => part.type === 'month')?.value;
    const day = parts.find((part) => part.type === 'day')?.value;
    if (!year || !month || !day) return toDateInputValue(timestamp);
    return `${year}-${month}-${day}`;
  } catch {
    return toDateInputValue(timestamp);
  }
};

const previousMonthStartDateInputValue = (value: string): string => {
  const match = /^(\d{4})-(\d{2})-\d{2}$/.exec(value);
  if (!match) return value;
  const [, year, month] = match;
  const date = new Date(Date.UTC(Number(year), Number(month) - 2, 1));
  const pad = (nextValue: number) => String(nextValue).padStart(2, '0');
  return `${date.getUTCFullYear()}-${pad(date.getUTCMonth() + 1)}-01`;
};

export const getCustomDateRangeBounds = (anchorMs = Date.now(), timezone?: string) => {
  const max = toDateInputValueInTimezone(anchorMs, timezone);
  return {
    min: previousMonthStartDateInputValue(max),
    max,
  };
};

export const isCustomDateWithinBounds = (value: string, bounds: { min: string; max: string }) => (
  value === '' || (value >= bounds.min && value <= bounds.max)
);

export const openDateInputPicker = (input: HTMLInputElement) => {
  try {
    input.showPicker?.();
  } catch {
    // 某些浏览器会拒绝非用户手势触发的 showPicker。
  }
};

const parseCustomDateBoundary = (value: string, endOfDay: boolean): number | undefined => {
  if (!value) return undefined;
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  if (!match) return undefined;
  const [, year, month, day] = match;
  const yearNumber = Number(year);
  const monthNumber = Number(month);
  const dayNumber = Number(day);
  const date = endOfDay
    ? new Date(yearNumber, monthNumber - 1, dayNumber, 23, 59, 59, 999)
    : new Date(yearNumber, monthNumber - 1, dayNumber, 0, 0, 0, 0);
  if (Number.isNaN(date.getTime())) return undefined;
  if (date.getFullYear() !== yearNumber || date.getMonth() !== monthNumber - 1 || date.getDate() !== dayNumber) return undefined;
  return date.getTime();
};

const parseCustomDateStart = (value: string): number | undefined => parseCustomDateBoundary(value, false);

const parseCustomDateEnd = (value: string): number | undefined => parseCustomDateBoundary(value, true);

export const buildCustomDateRangeQuery = (range: { start: string; end: string }) => {
  const startMs = parseCustomDateStart(range.start);
  const endMs = parseCustomDateEnd(range.end);
  if (!range.start || !range.end || startMs === undefined || endMs === undefined || startMs > endMs) {
    return { valid: false, start: undefined, end: undefined };
  }
  return { valid: true, start: range.start, end: range.end };
};

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

export const getUsageTabOptions = (translate: Translate): Array<{ value: UsageTab; label: string }> =>
  USAGE_TAB_OPTIONS.map((value) => ({
    value,
    label: translate(USAGE_TAB_LABEL_KEYS[value]),
  }));

export const getTimeRangeOptions = (translate: Translate) =>
  TIME_RANGE_OPTIONS.map((option) => ({
    value: option.value,
    label: translate(option.labelKey),
  }));

export const getOverviewHourWindowHours = ({ timeRange, filterWindow }: { timeRange: UsageTimeRange; filterWindow: UsageFilterWindow }) => {
  if (timeRange === 'all') return 24;
  if (timeRange === 'today') return 24;
  if (timeRange !== 'custom') return Math.min(HOUR_WINDOW_BY_TIME_RANGE[timeRange], 24);
  if (filterWindow.windowMinutes === undefined) return 24;
  return Math.min(Math.max(Math.ceil(filterWindow.windowMinutes / 60), 1), 24);
};

export const getPreferredOverviewChartPeriod = ({ windowMinutes }: { windowMinutes?: number }): 'hour' | 'day' => (
  windowMinutes !== undefined && windowMinutes > 24 * 60 ? 'day' : 'hour'
);

const toTimestampMs = (value: string | undefined): number | undefined => {
  if (!value) return undefined;
  const timestamp = Date.parse(value);
  return Number.isFinite(timestamp) ? timestamp : undefined;
};

export const getOverviewChartEndMs = ({ timeRange, filterWindow, fallbackEndMs, resolvedRangeEndMs }: { timeRange: UsageTimeRange; filterWindow: UsageFilterWindow; fallbackEndMs: number; resolvedRangeEndMs?: number }) => {
  if (timeRange === 'today' && filterWindow.startMs !== undefined) {
    return filterWindow.startMs + 24 * 60 * 60 * 1000;
  }
  if (resolvedRangeEndMs !== undefined) return resolvedRangeEndMs;
  return filterWindow.endMs ?? fallbackEndMs;
};

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
  const isMobile = useMediaQuery('(max-width: 768px)');
  const theme = useThemeStore((state) => state.theme);
  const resolvedTheme = useThemeStore((state) => state.resolvedTheme);
  const setTheme = useThemeStore((state) => state.setTheme);
  const isDark = resolvedTheme === 'dark';
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
    loadPricing,
    setModelPrices,
  } = usePricingData({
    onAuthRequired,
    enabled: activeTab === 'pricing',
  });
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [statusError, setStatusError] = useState('');
  const [updateCheckLoading, setUpdateCheckLoading] = useState(false);
  const [updateCheckNotice, setUpdateCheckNotice] = useState<{ kind: 'success' | 'info' | 'error'; message: string } | null>(null);
  const [hasNewVersion, setHasNewVersion] = useState(false);
  const updateCheckNoticeTimerRef = useRef<ReturnType<typeof window.setTimeout> | null>(null);
  const [customRangeError, setCustomRangeError] = useState('');
  const [customRangeHint, setCustomRangeHint] = useState('');
  const [eventsLoading, setEventsLoading] = useState(false);
  const [eventsError, setEventsError] = useState('');
  const [eventsData, setEventsData] = useState<UsageEvent[]>([]);
  const [eventsPage, setEventsPage] = useState(1);
  const [eventsPageSize, setEventsPageSize] = useState<number>(REQUEST_EVENTS_DEFAULT_PAGE_SIZE);
  const [eventsTotalCount, setEventsTotalCount] = useState(0);
  const [eventsTotalPages, setEventsTotalPages] = useState(0);
  const [eventsModelOptions, setEventsModelOptions] = useState<string[]>([]);
  const [eventsSourceOptions, setEventsSourceOptions] = useState<UsageSourceFilterOption[]>([]);
  const [eventsModelFilter, setEventsModelFilter] = useState(ALL_REQUEST_EVENTS_FILTER);
  const [eventsSourceFilter, setEventsSourceFilter] = useState(ALL_REQUEST_EVENTS_FILTER);
  const [eventsResultFilter, setEventsResultFilter] = useState(ALL_REQUEST_EVENTS_FILTER);
  const eventsRequestControllerRef = useRef<AbortController | null>(null);
  const eventsFilterOptionsRequestControllerRef = useRef<AbortController | null>(null);
  const [manualRefreshLoading, setManualRefreshLoading] = useState(false);
  const credentialsData = useCredentialsTabData({
    enabled: activeTab === 'credentials',
    onAuthRequired,
  });
  const refreshCredentials = credentialsData.refresh;
  const [analysisLoading, setAnalysisLoading] = useState(false);
  const [analysisError, setAnalysisError] = useState('');
  const [analysisData, setAnalysisData] = useState<UsageAnalysisResponse>({ apis: [], models: [] });
  const [, setAnalysisLastRefreshedAt] = useState<Date | null>(null);
  const analysisRequestControllerRef = useRef<AbortController | null>(null);

  const tabOptions = useMemo(() => getUsageTabOptions(t), [t]);
  const timeRangeOptions = useMemo(() => getTimeRangeOptions(t), [t]);
  const themeOptions = useMemo(
    () =>
      THEME_OPTIONS.map((option) => ({
        ...option,
        label: t(option.labelKey)
      })),
    [t]
  );
  const updateCheckToastClassName = updateCheckNotice ? (() => {
    if (updateCheckNotice.kind === 'error') return styles.updateCheckToastError;
    if (updateCheckNotice.kind === 'success') return styles.updateCheckToastSuccess;
    return styles.updateCheckToastInfo;
  })() : '';

  const resolvedRangeStartMs = toTimestampMs(usage?.range_start);
  const resolvedRangeEndMs = toTimestampMs(usage?.range_end);
  const filterWindow = useMemo<UsageFilterWindow>(() => {
    if (!usage) return {};
    return resolveUsageFilterWindow(usage.usage, timeRange, {
      nowMs: resolvedRangeEndMs ?? lastRefreshedAt?.getTime() ?? Date.now(),
      customStart:
        timeRange === 'custom' ? (resolvedRangeStartMs ?? parseCustomDateStart(customTimeRange.start)) : customTimeRange.start,
      customEnd:
        timeRange === 'custom' ? (resolvedRangeEndMs ?? parseCustomDateEnd(customTimeRange.end)) : customTimeRange.end
    });
  }, [customTimeRange.end, customTimeRange.start, lastRefreshedAt, resolvedRangeEndMs, resolvedRangeStartMs, timeRange, usage]);

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
      const queryWindow = timeRange === 'custom' ? buildCustomDateRangeQuery({ start: customTimeRange.start, end: customTimeRange.end }) : { start: undefined, end: undefined };
      const response = await fetchUsageAnalysis(timeRange, queryWindow.start, queryWindow.end, controller.signal);
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
  const includeFinalHourBucket = timeRange === 'today';
  const preferredOverviewChartPeriod = getPreferredOverviewChartPeriod({
    windowMinutes: filterWindow.windowMinutes,
  });
  const isCustomRange = timeRange === 'custom';
  const customDateRangeBounds = useMemo(() => getCustomDateRangeBounds(Date.now(), status?.timezone), [status?.timezone]);
  const handleCustomDateInputKeyDown = useCallback((event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Tab') return;
    event.preventDefault();
    openDateInputPicker(event.currentTarget);
  }, []);
  const handleCustomDateInputActivate = useCallback((event: SyntheticEvent<HTMLInputElement>) => {
    openDateInputPicker(event.currentTarget);
  }, []);

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
    setEventsPage(1);
  }, [customTimeRange.end, customTimeRange.start, timeRange]);

  useEffect(() => {
    if (timeRange !== 'custom') return;
    if (customTimeRange.start && customTimeRange.end) return;
    const anchorMs = lastRefreshedAt?.getTime() ?? Date.now();
    setCustomTimeRange(buildDefaultCustomRange(anchorMs));
  }, [customTimeRange.end, customTimeRange.start, lastRefreshedAt, timeRange]);

  useEffect(() => {
    let controller: AbortController | null = null;
    const loadStatus = async () => {
      controller?.abort();
      const requestController = new AbortController();
      controller = requestController;
      try {
        const status: StatusResponse = await fetchStatus(requestController.signal);
        setStatus(status);
        setStatusError(status.last_error || '');
      } catch (error) {
        if (requestController.signal.aborted) return;
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
      controller?.abort();
      window.clearInterval(timer);
    };
  }, [onAuthRequired]);

  useEffect(() => {
    if (!shouldShowUpdateCheckButton(status)) {
      setHasNewVersion(false);
    }
  }, [status]);

  useEffect(() => () => {
    if (updateCheckNoticeTimerRef.current !== null) {
      window.clearTimeout(updateCheckNoticeTimerRef.current);
      updateCheckNoticeTimerRef.current = null;
    }
  }, []);

  const getEventQueryWindow = useCallback(() => {
    if (timeRange !== 'custom') {
      return { valid: true, start: undefined, end: undefined };
    }
    if (!customTimeRange.start || !customTimeRange.end) {
      return { valid: false, start: undefined, end: undefined };
    }
    const startMs = parseCustomDateStart(customTimeRange.start);
    const endMs = parseCustomDateEnd(customTimeRange.end);
    if (startMs === undefined || endMs === undefined || startMs > endMs) {
      return { valid: false, start: undefined, end: undefined };
    }
    return buildCustomDateRangeQuery({ start: customTimeRange.start, end: customTimeRange.end });
  }, [customTimeRange.end, customTimeRange.start, timeRange]);

  const loadEventFilterOptions = useCallback(async () => {
    eventsFilterOptionsRequestControllerRef.current?.abort();
    const controller = new AbortController();
    eventsFilterOptionsRequestControllerRef.current = controller;

    try {
      const [modelResponse, sourceResponse] = await Promise.all([
        fetchUsageEventModelFilterOptions(controller.signal),
        fetchUsageEventSourceFilterOptions(controller.signal),
      ]);
      if (eventsFilterOptionsRequestControllerRef.current !== controller) {
        return;
      }
      setEventsModelOptions(modelResponse.models ?? []);
      setEventsSourceOptions(sourceResponse.sources ?? []);
    } catch (error) {
      if (controller.signal.aborted) {
        return;
      }
      if (eventsFilterOptionsRequestControllerRef.current === controller) {
        setEventsModelOptions([]);
        setEventsSourceOptions([]);
      }
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.();
      }
    } finally {
      if (eventsFilterOptionsRequestControllerRef.current === controller) {
        eventsFilterOptionsRequestControllerRef.current = null;
      }
    }
  }, [onAuthRequired]);

  const loadEvents = useCallback(async () => {
    const queryWindow = getEventQueryWindow();
    if (!queryWindow.valid) {
      eventsRequestControllerRef.current?.abort();
      eventsRequestControllerRef.current = null;
      setEventsData([]);
      setEventsTotalCount(0);
      setEventsTotalPages(0);
      setEventsError('');
      setEventsLoading(false);
      return;
    }

    eventsRequestControllerRef.current?.abort();
    const controller = new AbortController();
    eventsRequestControllerRef.current = controller;

    setEventsLoading(true);
    setEventsError('');
    try {
      const response = await fetchUsageEvents(timeRange, queryWindow.start, queryWindow.end, controller.signal, {
        page: eventsPage,
        pageSize: eventsPageSize,
        model: eventsModelFilter === ALL_REQUEST_EVENTS_FILTER ? undefined : eventsModelFilter,
        source: eventsSourceFilter === ALL_REQUEST_EVENTS_FILTER ? undefined : eventsSourceFilter,
        result: eventsResultFilter === ALL_REQUEST_EVENTS_FILTER ? undefined : eventsResultFilter,
      });
      if (eventsRequestControllerRef.current !== controller) {
        return;
      }
      if (response.total_pages > 0 && eventsPage > response.total_pages) {
        setEventsPage(response.total_pages);
        return;
      }
      setEventsData(response.events);
      setEventsTotalCount(response.total_count);
      setEventsTotalPages(response.total_pages);
    } catch (error) {
      if (controller.signal.aborted) {
        return;
      }
      if (eventsRequestControllerRef.current === controller) {
        setEventsData([]);
        setEventsTotalCount(0);
        setEventsTotalPages(0);
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
  }, [eventsModelFilter, eventsPage, eventsPageSize, eventsResultFilter, eventsSourceFilter, getEventQueryWindow, onAuthRequired, timeRange]);

  const resetEventsPage = useCallback(() => {
    setEventsPage(1);
  }, []);

  const handleEventsPageSizeChange = useCallback((pageSize: number) => {
    setEventsPageSize(pageSize);
    resetEventsPage();
  }, [resetEventsPage]);

  const handleEventsModelFilterChange = useCallback((model: string) => {
    setEventsModelFilter(model);
    resetEventsPage();
  }, [resetEventsPage]);

  const handleEventsSourceFilterChange = useCallback((source: string) => {
    setEventsSourceFilter(source);
    resetEventsPage();
  }, [resetEventsPage]);

  const handleEventsResultFilterChange = useCallback((result: string) => {
    setEventsResultFilter(result);
    resetEventsPage();
  }, [resetEventsPage]);

  const refreshActiveTab = useCallback(async () => {
    if (activeTab === 'events') {
      await Promise.all([loadEventFilterOptions(), loadEvents()]);
      return;
    }
    if (activeTab === 'credentials') {
      await refreshCredentials();
      return;
    }
    if (activeTab === 'analysis') {
      await loadAnalysis();
      return;
    }
    if (activeTab === 'pricing') {
      await loadPricing();
      return;
    }
    await loadUsage();
  }, [activeTab, loadAnalysis, loadEventFilterOptions, loadEvents, loadPricing, loadUsage, refreshCredentials]);

  const refreshAutoRefreshTab = useCallback(async () => {
    if (activeTab === 'events') {
      await loadEvents();
      return;
    }
    if (activeTab === 'credentials') {
      await refreshCredentials();
      return;
    }
    await loadUsage();
  }, [activeTab, loadEvents, loadUsage, refreshCredentials]);

  const autoRefreshEnabled = shouldAutoRefreshUsageTab({
    activeTab,
    eventsPage,
    authFilePage: credentialsData.authFilePage,
    aiProviderPage: credentialsData.aiProviderPage,
  });

  const handleManualRefresh = useCallback(async () => {
    setManualRefreshLoading(true);
    try {
      await refreshPageData({ refreshActiveTab });
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.();
        return;
      }
      setStatusError(error instanceof Error ? error.message : t('notification.refresh_failed'));
    } finally {
      setManualRefreshLoading(false);
    }
  }, [onAuthRequired, refreshActiveTab, t]);

  const showUpdateCheckNotice = useCallback((kind: 'success' | 'info' | 'error', message: string) => {
    if (updateCheckNoticeTimerRef.current !== null) {
      window.clearTimeout(updateCheckNoticeTimerRef.current);
    }
    setUpdateCheckNotice({ kind, message });
    updateCheckNoticeTimerRef.current = window.setTimeout(() => {
      setUpdateCheckNotice(null);
      updateCheckNoticeTimerRef.current = null;
    }, getUpdateCheckToastDuration(kind));
  }, []);

  const handleUpdateCheck = useCallback(async () => {
    setUpdateCheckLoading(true);
    try {
      const result = await fetchUpdateCheck();
      if (!result.canCompare) {
        setHasNewVersion(false);
        showUpdateCheckNotice('info', t('usage_stats.update_check_dev_build'));
        return;
      }
      if (result.updateAvailable) {
        setHasNewVersion(true);
        showUpdateCheckNotice('success', t('usage_stats.update_check_new_version', { version: result.latestVersion }));
        return;
      }
      setHasNewVersion(false);
      showUpdateCheckNotice('info', t('usage_stats.update_check_latest'));
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.();
        return;
      }
      setHasNewVersion(false);
      showUpdateCheckNotice('error', t('usage_stats.update_check_failed'));
    } finally {
      setUpdateCheckLoading(false);
    }
  }, [onAuthRequired, showUpdateCheckNotice, t]);

  useEffect(() => scheduleOverviewAutoRefresh({
    enabled: autoRefreshEnabled,
    refreshOverview: refreshAutoRefreshTab,
  }), [autoRefreshEnabled, refreshAutoRefreshTab]);

  useHeaderRefresh(refreshActiveTab);

  useEffect(() => {
    if (activeTab !== 'events') {
      eventsRequestControllerRef.current?.abort();
      eventsRequestControllerRef.current = null;
      eventsFilterOptionsRequestControllerRef.current?.abort();
      eventsFilterOptionsRequestControllerRef.current = null;
      setEventsLoading(false);
      return;
    }
    void loadEventFilterOptions();
    void loadEvents();
    return () => {
      eventsRequestControllerRef.current?.abort();
      eventsRequestControllerRef.current = null;
      eventsFilterOptionsRequestControllerRef.current?.abort();
      eventsFilterOptionsRequestControllerRef.current = null;
    };
  }, [activeTab, loadEventFilterOptions, loadEvents]);

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

  useEffect(() => {
    const next = sanitizeRequestEventFilters(
      {
        model: eventsModelFilter,
        source: eventsSourceFilter,
        result: eventsResultFilter,
      },
      {
        models: eventsModelOptions,
        sources: eventsSourceOptions,
      },
    );

    if (next.model !== eventsModelFilter) {
      setEventsModelFilter(next.model);
    }
    if (next.source !== eventsSourceFilter) {
      setEventsSourceFilter(next.source);
    }
    if (next.result !== eventsResultFilter) {
      setEventsResultFilter(next.result);
    }
    if (next.model !== eventsModelFilter || next.source !== eventsSourceFilter || next.result !== eventsResultFilter) {
      resetEventsPage();
    }
  }, [eventsModelFilter, eventsModelOptions, eventsResultFilter, eventsSourceFilter, eventsSourceOptions, resetEventsPage]);

  const lastSyncAt = useMemo(() => {
    if (!status?.last_run_at) return null;
    const parsed = new Date(status.last_run_at);
    return Number.isNaN(parsed.getTime()) ? null : parsed;
  }, [status?.last_run_at]);
  // 只有需要时间范围的 tab 才渲染 Range 控件，避免 Credentials/Pricing 产生空白占位。
  const showRangeControls = shouldShowRangeControls(activeTab);
  const {
    requestsSparkline,
    tokensSparkline,
    rpmSparkline,
    tpmSparkline,
    costSparkline
  } = useSparklines({ usage, loading });

  const {
    requestsPeriod,
    setRequestsPeriod,
    tokensPeriod,
    setTokensPeriod,
    requestsChartData,
    tokensChartData,
    requestsChartOptions,
    tokensChartOptions
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

  const overviewModelNames = useMemo(
    () => getModelNamesFromUsage(usage?.usage ?? null),
    [usage]
  );

  useEffect(() => {
    if (!isOverviewTab) return;
    setChartLines((current) => {
      const next = sanitizeChartLines(current, overviewModelNames);
      if (next.length === current.length && next.every((line, index) => line === current[index])) {
        return current;
      }
      return next;
    });
  }, [isOverviewTab, overviewModelNames]);
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
  const overviewDisplayLoading = getOverviewDisplayLoading({ loading, hasUsage: Boolean(usage) });

  return (
    <div className={styles.pageShell}>
      <div className={styles.pageFrame}>
        <header className={styles.topBar}>
          <div className={styles.brandBlock}>
            <span className={styles.eyebrow}>CPA Usage Keeper</span>
          </div>
          <div className={styles.topBarActions}>
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
            {shouldShowUpdateCheckButton(status) && (
              <div className={styles.updateCheckSwitcher} role="group" aria-label={t('usage_stats.check_updates')}>
                <button
                  type="button"
                  className={`${styles.updateCheckPill} ${styles.updateCheckPillActive} ${updateCheckLoading ? styles.updateCheckPillLoading : ''}`.trim()}
                  onClick={() => void handleUpdateCheck()}
                  disabled={updateCheckLoading}
                  aria-busy={updateCheckLoading}
                  aria-pressed={hasNewVersion}
                >
                  {updateCheckLoading ? (
                    <span className={styles.updateCheckPillInner}>
                      <LoadingSpinner size={12} className={styles.updateCheckSpinner} />
                      <span>{t('common.loading')}</span>
                    </span>
                  ) : (
                    <span className={styles.updateCheckPillInner}>
                      <span>{t('usage_stats.check_updates')}</span>
                      {hasNewVersion && <span className={styles.updateCheckDot} aria-hidden="true" />}
                    </span>
                  )}
                </button>
              </div>
            )}
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

            {lastSyncAt && (
              <div className={styles.toolbarMetaRow}>
                <span className={styles.lastRefreshed}>
                  {t('usage_stats.last_updated')}: {lastSyncAt.toLocaleTimeString()}
                </span>
              </div>
            )}

            {updateCheckNotice && (
              <div
                className={`${styles.updateCheckToast} ${updateCheckToastClassName}`.trim()}
                role="status"
                aria-live="polite"
              >
                <span className={styles.updateCheckToastMessage}>{updateCheckNotice.message}</span>
                <button
                  type="button"
                  className={styles.updateCheckToastClose}
                  onClick={() => {
                    if (updateCheckNoticeTimerRef.current !== null) {
                      window.clearTimeout(updateCheckNoticeTimerRef.current);
                      updateCheckNoticeTimerRef.current = null;
                    }
                    setUpdateCheckNotice(null);
                  }}
                >
                  {t('usage_stats.dismiss_notice')}
                </button>
              </div>
            )}

            <div className={styles.toolbarRow}>
              <div className={styles.tabBar} role="tablist" aria-label={t('usage_stats.tabs_aria_label')}>
                {tabOptions.map((option) => (
                  <button
                    key={option.value}
                    type="button"
                    role="tab"
                    aria-selected={activeTab === option.value}
                    className={`${styles.tabPill} ${activeTab === option.value ? styles.tabPillActive : ''}`.trim()}
                    onClick={() => setActiveTab(option.value)}
                  >
                    {option.label}
                  </button>
                ))}
              </div>

              <div className={styles.toolbarActionsRight}>
                {showRangeControls && (
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
                    {/* 自定义日期只在 Custom 模式挂载，非自定义范围保持工具栏内容宽度。 */}
                    {isCustomRange && (
                      <div className={`${styles.customRangeInline} ${styles.customRangeInlineOpen}`.trim()}>
                        <div className={styles.customRangeFields}>
                          <label className={styles.customRangeField}>
                            <span className={styles.customRangeFieldLabel}>{t('usage_stats.custom_start')}</span>
                            <input
                              type="date"
                              className={`input ${styles.customRangeInput}`}
                              value={customTimeRange.start}
                              min={customDateRangeBounds.min}
                              max={customDateRangeBounds.max}
                              onClick={handleCustomDateInputActivate}
                              onFocus={handleCustomDateInputActivate}
                              onKeyDown={handleCustomDateInputKeyDown}
                              onPaste={(event) => event.preventDefault()}
                              onChange={(event) => {
                                const nextValue = event.target.value;
                                if (!isCustomDateWithinBounds(nextValue, customDateRangeBounds)) return;
                                setCustomTimeRange((current) => ({
                                  ...current,
                                  start: nextValue
                                }));
                              }}
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
                              min={customDateRangeBounds.min}
                              max={customDateRangeBounds.max}
                              onClick={handleCustomDateInputActivate}
                              onFocus={handleCustomDateInputActivate}
                              onKeyDown={handleCustomDateInputKeyDown}
                              onPaste={(event) => event.preventDefault()}
                              onChange={(event) => {
                                const nextValue = event.target.value;
                                if (!isCustomDateWithinBounds(nextValue, customDateRangeBounds)) return;
                                setCustomTimeRange((current) => ({
                                  ...current,
                                  end: nextValue
                                }));
                              }}
                              aria-label={t('usage_stats.custom_end')}
                            />
                          </label>
                        </div>
                      </div>
                    )}
                  </div>
                )}
                {showRangeControls && isCustomRange && customRangeHint && (
                  <span className={styles.customRangeHint}>{customRangeHint}</span>
                )}
                {showRangeControls && isCustomRange && customRangeError && (
                  <span className={styles.customRangeError}>{customRangeError}</span>
                )}
                <div className={styles.refreshSwitcher} role="group" aria-label={t('usage_stats.refresh')}>
                  <button
                    type="button"
                    className={`${styles.refreshPill} ${styles.refreshPillActive} ${manualRefreshLoading ? styles.refreshPillLoading : ''}`.trim()}
                    onClick={() => void handleManualRefresh().catch(() => {})}
                    disabled={manualRefreshLoading}
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

            {activeTab === 'overview' && error && <div className={styles.errorBox}>{error === 'AUTH_REQUIRED' ? t('auth.session_expired') : error}</div>}
            {activeTab === 'pricing' && pricingError && <div className={styles.errorBox}>{pricingError === 'AUTH_REQUIRED' ? t('auth.session_expired') : pricingError}</div>}
            {!(activeTab === 'overview' ? error : activeTab === 'pricing' ? pricingError : '') && statusError && <div className={styles.errorBox}>{statusError}</div>}

            {activeTab === 'overview' && (
              <>
                <StatCards
                  usage={usage}
                  loading={overviewDisplayLoading}
                  sparklines={{
                    requests: requestsSparkline,
                    tokens: tokensSparkline,
                    rpm: rpmSparkline,
                    tpm: tpmSparkline,
                    cost: costSparkline
                  }}
                />

                <ServiceHealthCard usage={usage} loading={overviewDisplayLoading} />

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
                    loading={overviewDisplayLoading}
                    isMobile={isMobile}
                    emptyText={t('usage_stats.no_data')}
                  />
                  <UsageChart
                    title={t('usage_stats.tokens_trend')}
                    period={tokensPeriod}
                    onPeriodChange={setTokensPeriod}
                    chartData={tokensChartData}
                    chartOptions={tokensChartOptions}
                    loading={overviewDisplayLoading}
                    isMobile={isMobile}
                    emptyText={t('usage_stats.no_data')}
                  />
                </div>

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
                  page={eventsPage}
                  pageSize={eventsPageSize}
                  pageSizeOptions={REQUEST_EVENTS_PAGE_SIZES}
                  totalCount={eventsTotalCount}
                  totalPages={eventsTotalPages}
                  modelOptions={eventsModelOptions}
                  sourceOptions={eventsSourceOptions}
                  modelFilter={eventsModelFilter}
                  sourceFilter={eventsSourceFilter}
                  resultFilter={eventsResultFilter}
                  modelPrices={modelPrices}
                  onPageChange={setEventsPage}
                  onPageSizeChange={handleEventsPageSizeChange}
                  onModelFilterChange={handleEventsModelFilterChange}
                  onSourceFilterChange={handleEventsSourceFilterChange}
                  onResultFilterChange={handleEventsResultFilterChange}
                />
              </>
            )}

            {activeTab === 'credentials' && (
              <>
                {credentialsData.error && <div className={styles.errorBox}>{credentialsData.error}</div>}
                <div className={styles.credentialsSections}>
                  <AuthFileCredentialsSection
                    rows={credentialsData.authFileRows}
                    total={credentialsData.authFileTotal}
                    page={credentialsData.authFilePage}
                    totalPages={credentialsData.authFileTotalPages}
                    pageSize={credentialsData.authFilePageSize}
                    loading={credentialsData.loading}
                    quotaRefreshing={credentialsData.quotaRefreshing}
                    quotaRefreshError={credentialsData.quotaRefreshError}
                    onPageChange={credentialsData.setAuthFilePage}
                    onPageSizeChange={credentialsData.setAuthFilePageSize}
                    onRefreshQuota={credentialsData.refreshQuotaForCurrentAuthFilePage}
                    onRefreshQuotaForAuthIndex={credentialsData.refreshQuotaForAuthIndex}
                  />
                  <AiProviderCredentialsSection
                    rows={credentialsData.aiProviderRows}
                    total={credentialsData.aiProviderTotal}
                    page={credentialsData.aiProviderPage}
                    totalPages={credentialsData.aiProviderTotalPages}
                    pageSize={credentialsData.aiProviderPageSize}
                    loading={credentialsData.loading}
                    onPageChange={credentialsData.setAiProviderPage}
                    onPageSizeChange={credentialsData.setAiProviderPageSize}
                  />
                </div>
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
