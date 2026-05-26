import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from 'react';
import { useTranslation } from 'react-i18next';
import { ApiError, fetchCycleCostBreakdown, fetchCycleCostCurrent, fetchCycleCostHistory, fetchCycleCostProviders, refreshUsageQuotas, fetchUsageQuotaRefreshTask } from '@/lib/api';
import type { CycleCostBreakdown, CycleCostSummary, CycleProviderSummary } from '@/lib/types';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { IconRefreshCw, IconSearch, IconX, IconFilterAll } from '@/components/ui/icons';
import { formatCompactNumber } from '@/utils/usage';
import antigravityIcon from '@/assets/icons/antigravity.svg';
import claudeIcon from '@/assets/icons/claude.svg';
import codexIcon from '@/assets/icons/codex.svg';
import geminiIcon from '@/assets/icons/gemini.svg';
import iflowIcon from '@/assets/icons/iflow.svg';
import styles from './CycleCostSection.module.scss';

interface CycleCostSectionProps {
  onAuthRequired?: () => void;
}

const PROVIDER_ALL = '__all__';
const PAGE_SIZE_OPTIONS = [5, 10, 20, 50, 200] as const;
const DEFAULT_PAGE_SIZE = 10;
const HISTORY_LIMIT = 12;
const STORAGE_KEY_PAGE_SIZE = 'cpa-keeper-cycle-cost-page-size-v1';
const STORAGE_KEY_SORT = 'cpa-keeper-cycle-cost-sort-v1';
const STORAGE_KEY_ACTIVE_ONLY = 'cpa-keeper-cycle-cost-active-only-v1';
const STORAGE_KEY_PROVIDER = 'cpa-keeper-cycle-cost-provider-v1';
const REFRESH_POLL_INTERVAL_MS = 1500;
const REFRESH_POLL_MAX_ATTEMPTS = 14;
const BULK_REFRESH_CONCURRENCY = 4;

type SortKey = 'usd_desc' | 'used_pct_desc' | 'time_left_asc' | 'identity_asc' | 'last_captured_desc' | 'missing_first';

const SORT_OPTIONS: ReadonlyArray<{ value: SortKey; labelKey: string }> = [
  { value: 'usd_desc', labelKey: 'usage_stats.cycle_cost_sort_usd_desc' },
  { value: 'used_pct_desc', labelKey: 'usage_stats.cycle_cost_sort_used_pct_desc' },
  { value: 'time_left_asc', labelKey: 'usage_stats.cycle_cost_sort_time_left_asc' },
  { value: 'identity_asc', labelKey: 'usage_stats.cycle_cost_sort_identity_asc' },
  { value: 'last_captured_desc', labelKey: 'usage_stats.cycle_cost_sort_last_captured_desc' },
  { value: 'missing_first', labelKey: 'usage_stats.cycle_cost_sort_missing_first' },
];

interface ProviderFilterOption {
  key: string;
  labelKey: string;
  icon?: string;
  defaultLabel?: string;
}

const PROVIDER_FILTER_OPTIONS: ProviderFilterOption[] = [
  { key: 'antigravity', labelKey: 'usage_stats.credentials_filter_antigravity', icon: antigravityIcon, defaultLabel: 'Antigravity' },
  { key: 'claude', labelKey: 'usage_stats.credentials_filter_claude', icon: claudeIcon, defaultLabel: 'Claude' },
  { key: 'codex', labelKey: 'usage_stats.credentials_filter_codex', icon: codexIcon, defaultLabel: 'Codex' },
  { key: 'gemini-cli', labelKey: 'usage_stats.credentials_filter_gemini_cli', icon: geminiIcon, defaultLabel: 'GeminiCLI' },
  { key: 'iflow', labelKey: 'usage_stats.credentials_filter_iflow', icon: iflowIcon, defaultLabel: 'iFlow' },
];

const PROVIDER_KEY_ALIASES: Record<string, string[]> = {
  antigravity: ['antigravity'],
  claude: ['claude', 'anthropic'],
  codex: ['codex'],
  'gemini-cli': ['gemini', 'gemini-cli', 'geminicli'],
  iflow: ['iflow', 'flowith'],
};

function normalizeProvider(p: string): string {
  return p.toLowerCase().replace(/[^a-z0-9-]/g, '');
}

function resolveProviderKey(raw: string): string {
  const normalized = normalizeProvider(raw);
  for (const [filterKey, aliases] of Object.entries(PROVIDER_KEY_ALIASES)) {
    if (aliases.includes(normalized)) return filterKey;
  }
  return normalized || raw;
}

const INT_FORMATTER = new Intl.NumberFormat();
const PCT_FORMATTER = new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 });

type UsedPercentTone = 'low' | 'medium' | 'high';

function usedPercentTone(p: number): UsedPercentTone {
  if (p >= 90) return 'high';
  if (p >= 60) return 'medium';
  return 'low';
}

function summaryKey(item: CycleCostSummary): string {
  return `${item.provider}::${item.authIndex}::${item.cycleEnd || 'no-snapshot'}`;
}

function readStored<T extends string>(key: string, allowed: ReadonlyArray<T>, fallback: T): T {
  if (typeof window === 'undefined') return fallback;
  try {
    const raw = window.localStorage.getItem(key);
    if (raw && (allowed as ReadonlyArray<string>).includes(raw)) return raw as T;
  } catch {
    // ignore
  }
  return fallback;
}

function readStoredNumber(key: string, allowed: ReadonlyArray<number>, fallback: number): number {
  if (typeof window === 'undefined') return fallback;
  try {
    const raw = window.localStorage.getItem(key);
    if (raw) {
      const parsed = Number(raw);
      if (Number.isFinite(parsed) && allowed.includes(parsed)) return parsed;
    }
  } catch {
    // ignore
  }
  return fallback;
}

function readStoredBool(key: string, fallback: boolean): boolean {
  if (typeof window === 'undefined') return fallback;
  try {
    const raw = window.localStorage.getItem(key);
    if (raw === '1') return true;
    if (raw === '0') return false;
  } catch {
    // ignore
  }
  return fallback;
}

function readStoredString(key: string, fallback: string): string {
  if (typeof window === 'undefined') return fallback;
  try {
    const raw = window.localStorage.getItem(key);
    if (typeof raw === 'string') return raw;
  } catch {
    // ignore
  }
  return fallback;
}

function writeStored(key: string, value: string): void {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // ignore
  }
}

function formatUsd(value: number, locale?: string): string {
  if (!Number.isFinite(value)) return '—';
  if (value !== 0 && Math.abs(value) < 0.01) {
    return new Intl.NumberFormat(locale, { style: 'currency', currency: 'USD', minimumFractionDigits: 4, maximumFractionDigits: 4 }).format(value);
  }
  return new Intl.NumberFormat(locale, { style: 'currency', currency: 'USD', minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(value);
}

function safeTimestamp(value?: string): number {
  if (!value) return 0;
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

function formatRelativeRemainingMinutes(ms: number, t: (key: string, opts?: Record<string, unknown>) => string): string {
  if (ms <= 0) return t('usage_stats.cycle_cost_window_ended');
  const totalMinutes = Math.floor(ms / 60_000);
  const days = Math.floor(totalMinutes / 1440);
  const hours = Math.floor((totalMinutes % 1440) / 60);
  const minutes = totalMinutes % 60;
  if (days > 0) return t('usage_stats.cycle_cost_window_remaining_dh', { days, hours });
  if (hours > 0) return t('usage_stats.cycle_cost_window_remaining_hm', { hours, minutes });
  return t('usage_stats.cycle_cost_window_remaining_m', { minutes });
}

function formatElapsedSinceMs(ms: number, t: (key: string, opts?: Record<string, unknown>) => string): string {
  if (ms <= 0) return t('usage_stats.cycle_cost_just_now');
  const totalMinutes = Math.floor(ms / 60_000);
  if (totalMinutes < 1) return t('usage_stats.cycle_cost_just_now');
  if (totalMinutes < 60) return t('usage_stats.cycle_cost_minutes_ago', { count: totalMinutes });
  const hours = Math.floor(totalMinutes / 60);
  if (hours < 24) return t('usage_stats.cycle_cost_hours_ago', { count: hours });
  const days = Math.floor(hours / 24);
  return t('usage_stats.cycle_cost_days_ago', { count: days });
}

function formatDateRange(start: string, end: string, locale?: string): string {
  const startMs = Date.parse(start);
  const endMs = Date.parse(end);
  if (!Number.isFinite(startMs) || !Number.isFinite(endMs)) return `${start || '—'} → ${end || '—'}`;
  const dateFmt = new Intl.DateTimeFormat(locale, { month: 'short', day: '2-digit', hour: '2-digit', minute: '2-digit' });
  return `${dateFmt.format(new Date(startMs))} → ${dateFmt.format(new Date(endMs))}`;
}

function cyclePredictedUsd(currentUsd: number, usedPercent: number): number | null {
  if (!Number.isFinite(usedPercent) || usedPercent <= 1) return null;
  if (!Number.isFinite(currentUsd) || currentUsd <= 0) return null;
  if (usedPercent >= 100) return currentUsd;
  return currentUsd * (100 / usedPercent);
}

function cycleTimeProjectionUsd(currentUsd: number, cycleStartMs: number, cycleEndMs: number, nowMs: number): number | null {
  if (!Number.isFinite(currentUsd) || currentUsd <= 0) return null;
  if (!Number.isFinite(cycleStartMs) || !Number.isFinite(cycleEndMs)) return null;
  if (cycleStartMs <= 0 || cycleEndMs <= cycleStartMs) return null;
  if (nowMs <= cycleStartMs) return null;
  if (nowMs >= cycleEndMs) return currentUsd;
  const elapsedFrac = (nowMs - cycleStartMs) / (cycleEndMs - cycleStartMs);
  if (!Number.isFinite(elapsedFrac) || elapsedFrac <= 0) return null;
  return currentUsd / elapsedFrac;
}

interface RefreshTaskState {
  taskId: string;
  authIndex: string;
  attempts: number;
}

const SEARCH_INPUT_INLINE_STYLE: CSSProperties = { display: 'inline-flex', alignItems: 'center', gap: 8, padding: '6px 12px' };
const SEARCH_INPUT_FIELD_STYLE: CSSProperties = { flex: 1, border: 'none', outline: 'none', background: 'transparent', color: 'inherit', font: 'inherit', minWidth: 0 };
const SEARCH_INPUT_CLEAR_STYLE: CSSProperties = { background: 'transparent', border: 'none', cursor: 'pointer', color: 'inherit', display: 'inline-flex', padding: 0 };

export function CycleCostSection({ onAuthRequired }: CycleCostSectionProps) {
  const { t, i18n } = useTranslation();

  const [providerSummaries, setProviderSummaries] = useState<CycleProviderSummary[]>([]);
  const [providerFilter, setProviderFilter] = useState<string>(() => readStoredString(STORAGE_KEY_PROVIDER, PROVIDER_ALL));
  const [currentItems, setCurrentItems] = useState<CycleCostSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [search, setSearch] = useState('');
  const [sort, setSort] = useState<SortKey>(() => readStored<SortKey>(STORAGE_KEY_SORT, SORT_OPTIONS.map((o) => o.value), 'usd_desc'));
  const [pageSize, setPageSize] = useState<number>(() => readStoredNumber(STORAGE_KEY_PAGE_SIZE, PAGE_SIZE_OPTIONS as unknown as number[], DEFAULT_PAGE_SIZE));
  const [activeOnly, setActiveOnly] = useState<boolean>(() => readStoredBool(STORAGE_KEY_ACTIVE_ONLY, false));
  const [page, setPage] = useState(1);

  const [selectedAuthIndex, setSelectedAuthIndex] = useState<string | null>(null);
  const [breakdown, setBreakdown] = useState<CycleCostBreakdown | null>(null);
  const [breakdownLoading, setBreakdownLoading] = useState(false);
  const [breakdownError, setBreakdownError] = useState<string | null>(null);
  const [history, setHistory] = useState<CycleCostSummary[]>([]);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [historyError, setHistoryError] = useState<string | null>(null);

  const [refreshTasks, setRefreshTasks] = useState<Record<string, RefreshTaskState>>({});
  const [refreshErrors, setRefreshErrors] = useState<Record<string, string>>({});
  const [bulkRefreshing, setBulkRefreshing] = useState(false);
  const [bulkProgress, setBulkProgress] = useState<{ done: number; total: number }>({ done: 0, total: 0 });
  const pollingRef = useRef<Record<string, number>>({});

  useEffect(() => () => {
    Object.values(pollingRef.current).forEach((id) => window.clearTimeout(id));
    pollingRef.current = {};
  }, []);

  useEffect(() => writeStored(STORAGE_KEY_SORT, sort), [sort]);
  useEffect(() => writeStored(STORAGE_KEY_PAGE_SIZE, String(pageSize)), [pageSize]);
  useEffect(() => writeStored(STORAGE_KEY_ACTIVE_ONLY, activeOnly ? '1' : '0'), [activeOnly]);
  useEffect(() => writeStored(STORAGE_KEY_PROVIDER, providerFilter), [providerFilter]);

  const handleApiError = useCallback((err: unknown, fallback: string, setter: (value: string | null) => void) => {
    if (err instanceof ApiError && err.status === 401) {
      onAuthRequired?.();
      return;
    }
    setter(err instanceof Error ? err.message : fallback);
  }, [onAuthRequired]);

  const loadProviders = useCallback(async () => {
    try {
      const response = await fetchCycleCostProviders();
      setProviderSummaries(response.items);
    } catch (err) {
      // 不阻塞主流程
      if (err instanceof ApiError && err.status === 401) onAuthRequired?.();
    }
  }, [onAuthRequired]);

  const loadCurrent = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const providerParam = providerFilter === PROVIDER_ALL ? '' : providerFilter;
      const response = await fetchCycleCostCurrent(providerParam);
      setCurrentItems(response.items);
    } catch (err) {
      handleApiError(err, t('usage_stats.cycle_cost_load_current_failed'), setError);
    } finally {
      setLoading(false);
    }
  }, [handleApiError, providerFilter, t]);

  const loadHistory = useCallback(async (authIndex: string) => {
    setHistoryLoading(true);
    setHistoryError(null);
    try {
      const itemForProvider = currentItems.find((item) => item.authIndex === authIndex);
      const providerParam = itemForProvider?.provider || (providerFilter === PROVIDER_ALL ? '' : providerFilter);
      const response = await fetchCycleCostHistory(authIndex, providerParam, HISTORY_LIMIT);
      setHistory(response.items);
    } catch (err) {
      handleApiError(err, t('usage_stats.cycle_cost_load_history_failed'), setHistoryError);
    } finally {
      setHistoryLoading(false);
    }
  }, [currentItems, handleApiError, providerFilter, t]);

  const loadBreakdown = useCallback(async (authIndex: string, cycleEnd: string) => {
    if (!cycleEnd) {
      setBreakdown(null);
      return;
    }
    setBreakdownLoading(true);
    setBreakdownError(null);
    try {
      const itemForProvider = currentItems.find((item) => item.authIndex === authIndex);
      const providerParam = itemForProvider?.provider || (providerFilter === PROVIDER_ALL ? '' : providerFilter);
      const response = await fetchCycleCostBreakdown(authIndex, cycleEnd, providerParam);
      setBreakdown(response);
    } catch (err) {
      handleApiError(err, t('usage_stats.cycle_cost_load_breakdown_failed'), setBreakdownError);
    } finally {
      setBreakdownLoading(false);
    }
  }, [currentItems, handleApiError, providerFilter, t]);

  useEffect(() => {
    void loadProviders();
  }, [loadProviders]);

  useEffect(() => {
    void loadCurrent();
  }, [loadCurrent]);

  useEffect(() => {
    if (!selectedAuthIndex) {
      setBreakdown(null);
      setHistory([]);
      return;
    }
    const current = currentItems.find((item) => item.authIndex === selectedAuthIndex);
    if (current && current.hasSnapshot) {
      void loadBreakdown(current.authIndex, current.cycleEnd);
    } else {
      setBreakdown(null);
    }
    void loadHistory(selectedAuthIndex);
  }, [currentItems, loadBreakdown, loadHistory, selectedAuthIndex]);

  const clearRefreshTask = useCallback((authIndex: string) => {
    setRefreshTasks((prev) => {
      const next = { ...prev };
      delete next[authIndex];
      return next;
    });
    const handle = pollingRef.current[authIndex];
    if (handle) {
      window.clearTimeout(handle);
      delete pollingRef.current[authIndex];
    }
  }, []);

  const setRefreshError = useCallback((authIndex: string, message: string | null) => {
    setRefreshErrors((prev) => {
      const next = { ...prev };
      if (message === null) {
        delete next[authIndex];
      } else {
        next[authIndex] = message;
      }
      return next;
    });
  }, []);

  const waitForRefreshTask = useCallback(async (authIndex: string, taskId: string): Promise<boolean> => {
    for (let attempt = 0; attempt < REFRESH_POLL_MAX_ATTEMPTS; attempt += 1) {
      await new Promise<void>((resolve) => {
        const handle = window.setTimeout(() => resolve(), REFRESH_POLL_INTERVAL_MS);
        pollingRef.current[authIndex] = handle;
      });
      try {
        const task = await fetchUsageQuotaRefreshTask(taskId);
        if (task.status === 'completed') return true;
        if (task.status === 'failed') {
          setRefreshError(authIndex, task.error || t('usage_stats.cycle_cost_refresh_failed'));
          return false;
        }
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) {
          onAuthRequired?.();
        }
        setRefreshError(authIndex, err instanceof Error ? err.message : t('usage_stats.cycle_cost_refresh_failed'));
        return false;
      }
    }
    setRefreshError(authIndex, t('usage_stats.cycle_cost_refresh_timeout'));
    return false;
  }, [onAuthRequired, setRefreshError, t]);

  const handleRefreshSingle = useCallback(async (authIndex: string) => {
    if (refreshTasks[authIndex]) return false;
    setRefreshError(authIndex, null);
    setRefreshTasks((prev) => ({ ...prev, [authIndex]: { taskId: '', authIndex, attempts: 0 } }));
    try {
      const response = await refreshUsageQuotas([authIndex]);
      const taskId = response.tasks?.[0]?.taskId;
      const rejected = response.rejected?.[0];
      if (!taskId) {
        clearRefreshTask(authIndex);
        setRefreshError(authIndex, rejected ? t(`usage_stats.cycle_cost_refresh_rejected_${rejected.error}`, { defaultValue: rejected.error }) : t('usage_stats.cycle_cost_refresh_failed'));
        return false;
      }
      setRefreshTasks((prev) => ({ ...prev, [authIndex]: { taskId, authIndex, attempts: 0 } }));
      const ok = await waitForRefreshTask(authIndex, taskId);
      clearRefreshTask(authIndex);
      return ok;
    } catch (err) {
      clearRefreshTask(authIndex);
      if (err instanceof ApiError && err.status === 401) {
        onAuthRequired?.();
        return false;
      }
      setRefreshError(authIndex, err instanceof Error ? err.message : t('usage_stats.cycle_cost_refresh_failed'));
      return false;
    }
  }, [clearRefreshTask, onAuthRequired, refreshTasks, setRefreshError, t, waitForRefreshTask]);

  const handleRefreshSingleAndReload = useCallback(async (authIndex: string) => {
    const ok = await handleRefreshSingle(authIndex);
    if (ok) {
      await loadCurrent();
      if (selectedAuthIndex === authIndex) {
        const current = currentItems.find((item) => item.authIndex === authIndex);
        if (current) {
          await loadBreakdown(current.authIndex, current.cycleEnd);
          await loadHistory(authIndex);
        }
      }
    }
  }, [currentItems, handleRefreshSingle, loadBreakdown, loadCurrent, loadHistory, selectedAuthIndex]);

  const handleBulkRefreshMissing = useCallback(async (rows: CycleCostSummary[]) => {
    const targets = rows.filter((row) => !row.hasSnapshot).map((row) => row.authIndex);
    if (targets.length === 0 || bulkRefreshing) return;
    setBulkRefreshing(true);
    setBulkProgress({ done: 0, total: targets.length });
    let cursor = 0;
    let done = 0;
    const next = async () => {
      while (cursor < targets.length) {
        const authIndex = targets[cursor];
        cursor += 1;
        await handleRefreshSingle(authIndex);
        done += 1;
        setBulkProgress({ done, total: targets.length });
      }
    };
    await Promise.all(Array.from({ length: Math.min(BULK_REFRESH_CONCURRENCY, targets.length) }, () => next()));
    setBulkRefreshing(false);
    await loadCurrent();
  }, [bulkRefreshing, handleRefreshSingle, loadCurrent]);

  const filteredItems = useMemo(() => {
    const searchTrimmed = search.trim().toLowerCase();
    const now = Date.now();
    return currentItems.filter((item) => {
      if (activeOnly) {
        const captured = safeTimestamp(item.lastCapturedAt);
        if (captured === 0) return false;
        const ageHours = (now - captured) / 3_600_000;
        if (ageHours > 30 * 24) return false;
      }
      if (!searchTrimmed) return true;
      return (
        (item.identityName || '').toLowerCase().includes(searchTrimmed) ||
        item.authIndex.toLowerCase().includes(searchTrimmed)
      );
    });
  }, [activeOnly, currentItems, search]);

  const sortedItems = useMemo(() => {
    const copy = [...filteredItems];
    const numCompare = (a: number, b: number, desc = true) => (desc ? b - a : a - b);
    const tsCompare = (a?: string, b?: string, desc = true) => numCompare(safeTimestamp(a), safeTimestamp(b), desc);
    switch (sort) {
      case 'used_pct_desc':
        copy.sort((a, b) => numCompare(a.usedPercent, b.usedPercent));
        break;
      case 'time_left_asc':
        copy.sort((a, b) => safeTimestamp(a.cycleEnd) - safeTimestamp(b.cycleEnd));
        break;
      case 'identity_asc':
        copy.sort((a, b) => (a.identityName || a.authIndex).localeCompare(b.identityName || b.authIndex));
        break;
      case 'last_captured_desc':
        copy.sort((a, b) => tsCompare(a.lastCapturedAt, b.lastCapturedAt));
        break;
      case 'missing_first':
        copy.sort((a, b) => Number(a.hasSnapshot) - Number(b.hasSnapshot));
        break;
      case 'usd_desc':
      default:
        copy.sort((a, b) => {
          if (a.hasSnapshot !== b.hasSnapshot) return Number(b.hasSnapshot) - Number(a.hasSnapshot);
          return numCompare(a.totalUsd, b.totalUsd);
        });
        break;
    }
    return copy;
  }, [filteredItems, sort]);

  const totalPages = Math.max(1, Math.ceil(sortedItems.length / pageSize));
  const currentPage = Math.min(page, totalPages);
  const paginatedItems = useMemo(() => {
    const start = (currentPage - 1) * pageSize;
    return sortedItems.slice(start, start + pageSize);
  }, [currentPage, pageSize, sortedItems]);

  useEffect(() => {
    if (page > totalPages) setPage(totalPages);
  }, [page, totalPages]);

  const aggregates = useMemo(() => {
    let totalUsd = 0;
    let totalTokens = 0;
    let totalRequests = 0;
    let highWaterCount = 0;
    let missingCount = 0;
    let pricingMissing = false;
    for (const item of sortedItems) {
      totalUsd += item.totalUsd;
      totalTokens += item.totalTokens;
      totalRequests += item.requestCount;
      if (item.usedPercent >= 90) highWaterCount += 1;
      if (!item.hasSnapshot) missingCount += 1;
      if (item.pricingMissing) pricingMissing = true;
    }
    return {
      totalUsd,
      totalTokens,
      totalRequests,
      highWaterCount,
      missingCount,
      pricingMissing,
      tracked: sortedItems.length,
    };
  }, [sortedItems]);

  // 后端 ListProviders 给的是按 keeper usage_identities 算的全量 count;
  // 加上 PROVIDER_ALL 项. 没出现过的 provider 不显示.
  const providerOptions = useMemo(() => {
    const totalKnown = providerSummaries.reduce((acc, item) => acc + item.count, 0);
    const allOption = { key: PROVIDER_ALL, labelKey: 'usage_stats.credentials_filter_all', icon: undefined, defaultLabel: 'All', count: totalKnown };
    const knownOptions = PROVIDER_FILTER_OPTIONS.map((option) => {
      const match = providerSummaries.find((summary) => resolveProviderKey(summary.provider) === option.key);
      const count = match ? Number(match.count) : 0;
      return { ...option, count };
    }).filter((option) => option.count > 0);
    return [allOption, ...knownOptions];
  }, [providerSummaries]);

  const missingInVisible = useMemo(() => sortedItems.filter((row) => !row.hasSnapshot).length, [sortedItems]);
  const refreshingAll = loading;
  const locale = i18n.language;

  return (
    <section className={styles.sectionCard}>
      <header className={styles.sectionHeader}>
        <div className={styles.titleBlock}>
          <span className={styles.eyebrow}>{t('usage_stats.cycle_cost_eyebrow')}</span>
          <div className={styles.titleRow}>
            <h3 className={styles.title}>{t('usage_stats.cycle_cost_title')}</h3>
            <span className={styles.countBadge}>{t('usage_stats.cycle_cost_count', { count: aggregates.tracked })}</span>
          </div>
          <p className={styles.subtitle}>{t('usage_stats.cycle_cost_subtitle')}</p>
        </div>
        <div className={styles.sectionActions}>
          {missingInVisible > 0 && (
            <div className={styles.refreshSwitcher}>
              <button
                type="button"
                className={`${styles.refreshButton} ${bulkRefreshing ? styles.refreshButtonLoading : ''}`.trim()}
                onClick={() => { void handleBulkRefreshMissing(sortedItems); }}
                disabled={bulkRefreshing}
                aria-busy={bulkRefreshing}
              >
                <span className={styles.refreshButtonInner}>
                  {bulkRefreshing ? <LoadingSpinner size={12} /> : <IconRefreshCw size={12} />}
                  <span>
                    {bulkRefreshing
                      ? t('usage_stats.cycle_cost_bulk_refresh_progress', { done: bulkProgress.done, total: bulkProgress.total })
                      : t('usage_stats.cycle_cost_bulk_refresh', { count: missingInVisible })}
                  </span>
                </span>
              </button>
            </div>
          )}
          <div className={styles.refreshSwitcher}>
            <button
              type="button"
              className={`${styles.refreshButton} ${styles.refreshButtonActive} ${refreshingAll ? styles.refreshButtonLoading : ''}`.trim()}
              onClick={() => { void loadCurrent(); }}
              disabled={refreshingAll}
              aria-busy={refreshingAll}
            >
              <span className={styles.refreshButtonInner}>
                {refreshingAll ? <LoadingSpinner size={12} /> : <IconRefreshCw size={12} />}
                <span>{refreshingAll ? t('usage_stats.cycle_cost_reloading') : t('usage_stats.cycle_cost_reload')}</span>
              </span>
            </button>
          </div>
        </div>
      </header>

      {providerOptions.length > 1 && (
        <div className={styles.providerFilterBar} role="toolbar" aria-label={t('usage_stats.credentials_filter_aria_label')}>
          {providerOptions.map((option) => {
            const selected = providerFilter === option.key;
            return (
              <button
                key={option.key}
                type="button"
                className={`${styles.providerFilterButton} ${selected ? styles.providerFilterButtonActive : ''}`.trim()}
                aria-pressed={selected}
                onClick={() => { setProviderFilter(option.key); setPage(1); }}
              >
                <span className={styles.providerFilterIconFrame}>
                  {option.key === PROVIDER_ALL
                    ? <IconFilterAll size={21} />
                    : option.icon
                      ? <img src={option.icon} alt="" aria-hidden="true" />
                      : null}
                </span>
                <span className={styles.providerFilterLabel}>
                  {t(option.labelKey, { defaultValue: option.defaultLabel || option.key })}
                </span>
                <span className={styles.providerFilterCount}>{option.count}</span>
              </button>
            );
          })}
        </div>
      )}

      <div className={styles.summaryBand}>
        <div className={styles.summaryTile}>
          <span className={styles.summaryTileLabel}>{t('usage_stats.cycle_cost_summary_total_usd')}</span>
          <span className={styles.summaryTileValue}>{formatUsd(aggregates.totalUsd, locale)}</span>
          <span className={styles.summaryTileHint}>
            {t('usage_stats.cycle_cost_summary_total_usd_hint', { count: aggregates.tracked })}
          </span>
        </div>
        <div className={styles.summaryTile}>
          <span className={styles.summaryTileLabel}>{t('usage_stats.cycle_cost_summary_total_tokens')}</span>
          <span className={styles.summaryTileValue}>{formatCompactNumber(aggregates.totalTokens)}</span>
          <span className={styles.summaryTileHint}>{t('usage_stats.cycle_cost_summary_total_requests', { count: aggregates.totalRequests })}</span>
        </div>
        <div className={styles.summaryTile}>
          <span className={styles.summaryTileLabel}>{t('usage_stats.cycle_cost_summary_high_water')}</span>
          <span className={styles.summaryTileValue}>{INT_FORMATTER.format(aggregates.highWaterCount)}</span>
          <span className={styles.summaryTileHint}>{t('usage_stats.cycle_cost_summary_high_water_hint')}</span>
        </div>
        <div className={styles.summaryTile}>
          <span className={styles.summaryTileLabel}>{t('usage_stats.cycle_cost_summary_missing')}</span>
          <span className={styles.summaryTileValue}>{INT_FORMATTER.format(aggregates.missingCount)}</span>
          <span className={styles.summaryTileHint}>
            {aggregates.missingCount > 0
              ? t('usage_stats.cycle_cost_summary_missing_hint')
              : aggregates.pricingMissing
                ? t('usage_stats.cycle_cost_summary_pricing_missing')
                : t('usage_stats.cycle_cost_summary_pricing_complete')}
          </span>
        </div>
      </div>

      <div className={styles.toolbar}>
        <label className={styles.searchInput} style={SEARCH_INPUT_INLINE_STYLE}>
          <IconSearch size={14} />
          <input
            type="search"
            value={search}
            onChange={(event) => { setSearch(event.target.value); setPage(1); }}
            placeholder={t('usage_stats.cycle_cost_search_placeholder')}
            style={SEARCH_INPUT_FIELD_STYLE}
          />
          {search && (
            <button
              type="button"
              onClick={() => { setSearch(''); setPage(1); }}
              aria-label={t('usage_stats.cycle_cost_search_clear')}
              style={SEARCH_INPUT_CLEAR_STYLE}
            >
              <IconX size={14} />
            </button>
          )}
        </label>
        <label className={styles.toolbarControl}>
          <span>{t('usage_stats.cycle_cost_sort_label')}</span>
          <select value={sort} onChange={(event) => setSort(event.target.value as SortKey)}>
            {SORT_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>{t(option.labelKey)}</option>
            ))}
          </select>
        </label>
        <label className={styles.toolbarControl}>
          <span>{t('usage_stats.cycle_cost_page_size_label')}</span>
          <select value={pageSize} onChange={(event) => { setPageSize(Number(event.target.value)); setPage(1); }}>
            {PAGE_SIZE_OPTIONS.map((option) => <option key={option} value={option}>{option}</option>)}
          </select>
        </label>
        <label className={styles.activeOnlySwitch}>
          <input type="checkbox" checked={activeOnly} onChange={(event) => { setActiveOnly(event.target.checked); setPage(1); }} />
          <span>{t('usage_stats.cycle_cost_active_only')}</span>
        </label>
      </div>

      {error && <div className={styles.inlineError}>{error}</div>}

      <p className={styles.helperText}>{t('usage_stats.cycle_cost_helper')}</p>

      <div className={styles.rows}>
        {!loading && paginatedItems.length === 0 && (
          <div className={styles.emptyState}>
            {sortedItems.length === 0 && search ? t('usage_stats.cycle_cost_empty_filtered') : t('usage_stats.cycle_cost_empty')}
          </div>
        )}
        {loading && paginatedItems.length === 0 && (
          <div className={styles.emptyState}>{t('common.loading')}</div>
        )}
        {paginatedItems.map((item) => {
          const isSelected = selectedAuthIndex === item.authIndex;
          const cycleEndMs = safeTimestamp(item.cycleEnd);
          const cycleStartMs = safeTimestamp(item.cycleStart);
          const lastCapturedMs = safeTimestamp(item.lastCapturedAt);
          const nowMs = Date.now();
          const usedTone = usedPercentTone(item.usedPercent);
          const predictedFromQuota = cyclePredictedUsd(item.totalUsd, item.usedPercent);
          const projectedFromTime = cycleTimeProjectionUsd(item.totalUsd, cycleStartMs, cycleEndMs, nowMs);
          const refreshing = Boolean(refreshTasks[item.authIndex]);
          const rowError = refreshErrors[item.authIndex];
          const rowClassName = `${styles.row} ${isSelected ? styles.rowSelected : ''} ${!item.hasSnapshot ? styles.rowMissing : ''}`.trim();
          return (
            <article key={summaryKey(item)} className={rowClassName}>
              <div className={styles.identityBlock}>
                <div className={styles.identityNameRow}>
                  <span className={styles.identityName}>{item.identityName || item.authIndex}</span>
                  {item.identityType && <span className={`${styles.badge} ${styles.badgePrimary}`}>{item.identityType}</span>}
                  {item.planType && <span className={`${styles.badge} ${styles.badgeNeutral}`}>{item.planType}</span>}
                  {item.disabled && <span className={`${styles.badge} ${styles.badgeDanger}`}>{t('usage_stats.cycle_cost_badge_disabled')}</span>}
                  {item.pricingMissing && (
                    <span className={`${styles.badge} ${styles.badgeWarning}`} title={t('usage_stats.cycle_cost_pricing_missing_badge_title')}>
                      {t('usage_stats.cycle_cost_pricing_missing_badge')}
                    </span>
                  )}
                </div>
                <span className={styles.identityIndex}>{item.authIndex}</span>
                <div className={styles.identitySubtext}>
                  {item.hasSnapshot ? (
                    <>
                      <span>{formatDateRange(item.cycleStart, item.cycleEnd, locale)}</span>
                      <span>{item.sealed ? t('usage_stats.cycle_cost_state_sealed') : formatRelativeRemainingMinutes(cycleEndMs - nowMs, t)}</span>
                    </>
                  ) : (
                    <span className={styles.rowMissingHint}>{t('usage_stats.cycle_cost_no_snapshot_hint')}</span>
                  )}
                </div>
              </div>

              <div className={styles.metricGroup}>
                {item.hasSnapshot ? (
                  <>
                    <div className={styles.metricRow}>
                      <span className={styles.metricPill}>
                        <span className={styles.metricPillLabel}>{t('usage_stats.cycle_cost_metric_used')}</span>
                        <span className={styles.metricPillValue}>{PCT_FORMATTER.format(item.usedPercent)}%</span>
                      </span>
                      <span className={styles.metricPill}>
                        <span className={styles.metricPillLabel}>{t('usage_stats.cycle_cost_metric_tokens')}</span>
                        <span className={styles.metricPillValue}>{formatCompactNumber(item.totalTokens)}</span>
                      </span>
                      <span className={styles.metricPill}>
                        <span className={styles.metricPillLabel}>{t('usage_stats.cycle_cost_metric_requests')}</span>
                        <span className={styles.metricPillValue}>{INT_FORMATTER.format(item.requestCount)}</span>
                      </span>
                    </div>
                    <div className={styles.usedBarOuter}>
                      <div
                        className={`${styles.usedBarFill} ${styles[`usedBarFill${usedTone[0].toUpperCase()}${usedTone.slice(1)}`]}`}
                        style={{ width: `${Math.min(100, Math.max(0, item.usedPercent))}%` }}
                      />
                    </div>
                    <div className={styles.windowText}>
                      {lastCapturedMs > 0
                        ? t('usage_stats.cycle_cost_last_captured', { ago: formatElapsedSinceMs(nowMs - lastCapturedMs, t) })
                        : t('usage_stats.cycle_cost_never_captured')}
                    </div>
                  </>
                ) : (
                  <div className={styles.rowMissingHint}>
                    {t('usage_stats.cycle_cost_no_snapshot_metric_hint')}
                  </div>
                )}
                {rowError && <div className={styles.windowText} style={{ color: 'var(--warning-text)' }}>{rowError}</div>}
              </div>

              <div className={styles.sidePanel}>
                <div className={styles.costBlock}>
                  {item.hasSnapshot ? (
                    <>
                      <div className={styles.costPrimary}>{formatUsd(item.totalUsd, locale)}</div>
                      <div className={styles.costSecondary}>
                        {predictedFromQuota !== null && predictedFromQuota > item.totalUsd
                          ? t('usage_stats.cycle_cost_projection_quota', { value: formatUsd(predictedFromQuota, locale) })
                          : projectedFromTime !== null && projectedFromTime > item.totalUsd
                            ? t('usage_stats.cycle_cost_projection_time', { value: formatUsd(projectedFromTime, locale) })
                            : t('usage_stats.cycle_cost_projection_none')}
                      </div>
                    </>
                  ) : (
                    <div className={styles.costPlaceholder}>{t('usage_stats.cycle_cost_no_data_placeholder')}</div>
                  )}
                </div>
                <div className={styles.actionRow}>
                  <button
                    type="button"
                    className={`${styles.rowActionButton} ${styles.rowActionButtonPrimary}`}
                    onClick={() => { void handleRefreshSingleAndReload(item.authIndex); }}
                    disabled={refreshing || bulkRefreshing}
                    aria-busy={refreshing}
                  >
                    {refreshing
                      ? <><LoadingSpinner size={10} /> <span>{t('usage_stats.cycle_cost_refresh_running')}</span></>
                      : <><IconRefreshCw size={10} /> <span>{t('usage_stats.cycle_cost_refresh_now')}</span></>}
                  </button>
                  {item.hasSnapshot && (
                    <button
                      type="button"
                      className={styles.rowActionButton}
                      onClick={() => setSelectedAuthIndex(isSelected ? null : item.authIndex)}
                      aria-expanded={isSelected}
                    >
                      {isSelected ? t('usage_stats.cycle_cost_hide_detail') : t('usage_stats.cycle_cost_show_detail')}
                    </button>
                  )}
                </div>
              </div>
            </article>
          );
        })}

        {selectedAuthIndex && paginatedItems.some((row) => row.authIndex === selectedAuthIndex && row.hasSnapshot) && (
          <section className={styles.detailPanel}>
            <header className={styles.detailHeader}>
              <h4 className={styles.detailHeaderTitle}>
                {t('usage_stats.cycle_cost_detail_title', { name: paginatedItems.find((r) => r.authIndex === selectedAuthIndex)?.identityName || selectedAuthIndex })}
              </h4>
              <button type="button" className={styles.detailHeaderClose} onClick={() => setSelectedAuthIndex(null)}>
                {t('usage_stats.cycle_cost_detail_close')}
              </button>
            </header>

            <div className={styles.detailSubsection}>
              <h5 className={styles.detailSubsectionTitle}>{t('usage_stats.cycle_cost_detail_breakdown_title')}</h5>
              {breakdownError && <div className={styles.inlineError} style={{ margin: 0 }}>{breakdownError}</div>}
              {breakdownLoading && <div className={styles.detailEmpty}>{t('common.loading')}</div>}
              {!breakdownLoading && !breakdownError && breakdown && (
                breakdown.models.length === 0
                  ? <div className={styles.detailEmpty}>{t('usage_stats.cycle_cost_detail_breakdown_empty')}</div>
                  : (
                    <table className={styles.detailTable}>
                      <thead>
                        <tr>
                          <th>{t('usage_stats.cycle_cost_detail_col_model')}</th>
                          <th>{t('usage_stats.cycle_cost_detail_col_input')}</th>
                          <th>{t('usage_stats.cycle_cost_detail_col_output')}</th>
                          <th>{t('usage_stats.cycle_cost_detail_col_cached')}</th>
                          <th>{t('usage_stats.cycle_cost_detail_col_reasoning')}</th>
                          <th>{t('usage_stats.cycle_cost_detail_col_requests')}</th>
                          <th>{t('usage_stats.cycle_cost_detail_col_usd')}</th>
                        </tr>
                      </thead>
                      <tbody>
                        {breakdown.models.map((entry) => (
                          <tr key={entry.model}>
                            <td className={styles.detailCellStrong}>
                              {entry.model}
                              {entry.modelAlias && entry.modelAlias !== entry.model && <span style={{ color: 'var(--text-tertiary)', fontWeight: 400 }}> ({entry.modelAlias})</span>}
                            </td>
                            <td>{formatCompactNumber(entry.inputTokens)}</td>
                            <td>{formatCompactNumber(entry.outputTokens)}</td>
                            <td>{formatCompactNumber(entry.cachedTokens + entry.cacheReadTokens)}</td>
                            <td>{formatCompactNumber(entry.reasoningTokens)}</td>
                            <td>{INT_FORMATTER.format(entry.requestCount)}</td>
                            <td className={`${styles.detailCellStrong} ${styles.detailCellRight}`}>
                              {formatUsd(entry.usdCost, locale)}
                              {entry.pricingMissing && (
                                <span title={t('usage_stats.cycle_cost_pricing_missing_badge_title')} style={{ color: 'var(--warning-text)', marginLeft: 4 }}>⚠</span>
                              )}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  )
              )}
            </div>

            <div className={styles.detailSubsection}>
              <h5 className={styles.detailSubsectionTitle}>{t('usage_stats.cycle_cost_detail_history_title')}</h5>
              {historyError && <div className={styles.inlineError} style={{ margin: 0 }}>{historyError}</div>}
              {historyLoading && <div className={styles.detailEmpty}>{t('common.loading')}</div>}
              {!historyLoading && !historyError && (
                history.length === 0
                  ? <div className={styles.detailEmpty}>{t('usage_stats.cycle_cost_detail_history_empty', { days: 7 })}</div>
                  : (
                    <>
                      <div className={styles.miniBars} aria-hidden>
                        {(() => {
                          const maxVal = Math.max(...history.map((h) => h.totalUsd), 0.0001);
                          return [...history].reverse().map((row) => (
                            <div
                              key={summaryKey(row)}
                              className={styles.miniBar}
                              style={{ height: `${Math.max(2, Math.round((row.totalUsd / maxVal) * 100))}%` }}
                              title={`${formatDateRange(row.cycleStart, row.cycleEnd, locale)} · ${formatUsd(row.totalUsd, locale)}`}
                            />
                          ));
                        })()}
                      </div>
                      <table className={styles.detailTable}>
                        <thead>
                          <tr>
                            <th>{t('usage_stats.cycle_cost_detail_col_window')}</th>
                            <th>{t('usage_stats.cycle_cost_detail_col_state')}</th>
                            <th>{t('usage_stats.cycle_cost_detail_col_usd')}</th>
                            <th>{t('usage_stats.cycle_cost_detail_col_tokens')}</th>
                            <th>{t('usage_stats.cycle_cost_detail_col_requests')}</th>
                          </tr>
                        </thead>
                        <tbody>
                          {history.map((row) => (
                            <tr key={summaryKey(row)}>
                              <td>{formatDateRange(row.cycleStart, row.cycleEnd, locale)}</td>
                              <td>
                                <span className={`${styles.badge} ${row.sealed ? styles.badgeNeutral : styles.badgePrimary}`}>
                                  {row.sealed ? t('usage_stats.cycle_cost_state_sealed') : t('usage_stats.cycle_cost_state_current')}
                                </span>
                              </td>
                              <td className={`${styles.detailCellStrong} ${styles.detailCellRight}`}>{formatUsd(row.totalUsd, locale)}</td>
                              <td>{formatCompactNumber(row.totalTokens)}</td>
                              <td>{INT_FORMATTER.format(row.requestCount)}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </>
                  )
              )}
            </div>
          </section>
        )}
      </div>

      {sortedItems.length > 0 && (
        <footer className={styles.pagination}>
          <span className={styles.paginationInfo}>
            {t('usage_stats.cycle_cost_pagination_info', {
              from: (currentPage - 1) * pageSize + 1,
              to: Math.min(currentPage * pageSize, sortedItems.length),
              total: sortedItems.length,
            })}
          </span>
          <div className={styles.paginationControls}>
            <button type="button" className={styles.paginationBtn} onClick={() => setPage((p) => Math.max(1, p - 1))} disabled={currentPage <= 1}>
              {t('usage_stats.cycle_cost_prev')}
            </button>
            <span className={styles.paginationPage}>{currentPage} / {totalPages}</span>
            <button type="button" className={styles.paginationBtn} onClick={() => setPage((p) => Math.min(totalPages, p + 1))} disabled={currentPage >= totalPages}>
              {t('usage_stats.cycle_cost_next')}
            </button>
          </div>
        </footer>
      )}
    </section>
  );
}
