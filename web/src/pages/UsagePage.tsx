import { useState, useMemo, useCallback, useEffect } from 'react';
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
import { ApiError, fetchStatus } from '@/lib/api';
import type { StatusResponse } from '@/lib/types';
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
  useSparklines,
  useChartData
} from '@/components/usage';
import {
  getModelNamesFromUsage,
  getApiStats,
  getModelStats,
  filterUsageByTimeRange,
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
const DEFAULT_CHART_LINES = ['all'];
const DEFAULT_TIME_RANGE: UsageTimeRange = '24h';
const MAX_CHART_LINES = 9;
const TIME_RANGE_OPTIONS: ReadonlyArray<{ value: UsageTimeRange; labelKey: string }> = [
  { value: 'all', labelKey: 'usage_stats.range_all' },
  { value: '4h', labelKey: 'usage_stats.range_4h' },
  { value: '8h', labelKey: 'usage_stats.range_8h' },
  { value: '12h', labelKey: 'usage_stats.range_12h' },
  { value: '24h', labelKey: 'usage_stats.range_24h' },
  { value: '7d', labelKey: 'usage_stats.range_7d' },
];
const HOUR_WINDOW_BY_TIME_RANGE: Record<Exclude<UsageTimeRange, 'all'>, number> = {
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

const isUsageTimeRange = (value: unknown): value is UsageTimeRange =>
  value === '4h' || value === '8h' || value === '12h' || value === '24h' || value === '7d' || value === 'all';

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
    return isUsageTimeRange(raw) ? raw : DEFAULT_TIME_RANGE;
  } catch {
    return DEFAULT_TIME_RANGE;
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

  const {
    usage,
    loading,
    error,
    lastRefreshedAt,
    modelPrices,
    setModelPrices,
    loadUsage
  } = useUsageData({ onAuthRequired });

  useHeaderRefresh(loadUsage);

  const [chartLines, setChartLines] = useState<string[]>(loadChartLines);
  const [timeRange, setTimeRange] = useState<UsageTimeRange>(loadTimeRange);
  const [statusError, setStatusError] = useState('');

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

  const filteredUsage = useMemo(
    () => (usage ? filterUsageByTimeRange(usage, timeRange) : null),
    [usage, timeRange]
  );
  const hourWindowHours =
    timeRange === 'all' ? undefined : HOUR_WINDOW_BY_TIME_RANGE[timeRange];

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

  const handleLanguageChange = useCallback(async (language: 'en' | 'zh') => {
    if (currentLanguage === language) return;
    await i18n.changeLanguage(language);
    persistLanguage(language);
  }, [currentLanguage]);

  const nowMs = lastRefreshedAt?.getTime() ?? 0;

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
  } = useChartData({ usage: filteredUsage, chartLines, isDark, isMobile, hourWindowHours });

  const modelNames = useMemo(() => getModelNamesFromUsage(usage), [usage]);
  const apiStats = useMemo(
    () => getApiStats(filteredUsage, modelPrices),
    [filteredUsage, modelPrices]
  );
  const modelStats = useMemo(
    () => getModelStats(filteredUsage, modelPrices),
    [filteredUsage, modelPrices]
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
            {loading && !usage && (
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
                </div>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => void loadUsage().catch(() => {})}
                  disabled={loading}
                  className={styles.refreshButton}
                >
                  <IconRefreshCw size={14} />
                  <span>{loading ? t('common.loading') : t('usage_stats.refresh')}</span>
                </Button>
              </div>
              {lastRefreshedAt && (
                <span className={styles.lastRefreshed}>
                  {t('usage_stats.last_updated')}: {lastRefreshedAt.toLocaleTimeString()}
                </span>
              )}
            </div>

            {error && <div className={styles.errorBox}>{error === 'AUTH_REQUIRED' ? t('auth.session_expired') : error}</div>}
            {!error && statusError && <div className={styles.errorBox}>{statusError}</div>}

            <StatCards
              usage={filteredUsage}
              loading={loading}
              modelPrices={modelPrices}
              nowMs={nowMs}
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
              modelNames={modelNames}
              maxLines={MAX_CHART_LINES}
              onChange={handleChartLinesChange}
            />

            <ServiceHealthCard usage={usage} loading={loading} />

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

            <TokenBreakdownChart
              usage={filteredUsage}
              loading={loading}
              isDark={isDark}
              isMobile={isMobile}
              hourWindowHours={hourWindowHours}
            />

            <CostTrendChart
              usage={filteredUsage}
              loading={loading}
              isDark={isDark}
              isMobile={isMobile}
              modelPrices={modelPrices}
              hourWindowHours={hourWindowHours}
            />

            <div className={styles.detailsGrid}>
              <ApiDetailsCard apiStats={apiStats} loading={loading} hasPrices={hasPrices} />
              <ModelStatsCard modelStats={modelStats} loading={loading} hasPrices={hasPrices} />
            </div>

            <RequestEventsDetailsCard
              usage={filteredUsage}
              loading={loading}
              geminiKeys={config?.geminiApiKeys || []}
              claudeConfigs={config?.claudeApiKeys || []}
              codexConfigs={config?.codexApiKeys || []}
              vertexConfigs={config?.vertexApiKeys || []}
              openaiProviders={config?.openaiCompatibility || []}
            />

            <CredentialStatsCard
              usage={filteredUsage}
              loading={loading}
              geminiKeys={config?.geminiApiKeys || []}
              claudeConfigs={config?.claudeApiKeys || []}
              codexConfigs={config?.codexApiKeys || []}
              vertexConfigs={config?.vertexApiKeys || []}
              openaiProviders={config?.openaiCompatibility || []}
            />

            <PriceSettingsCard
              modelNames={modelNames}
              modelPrices={modelPrices}
              onPricesChange={setModelPrices}
            />
          </div>
        </main>
      </div>
    </div>
  );
}
