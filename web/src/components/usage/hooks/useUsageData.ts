import { useEffect, useState, useCallback, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { ApiError, fetchPricing, fetchUsedModels, updatePricing } from '@/lib/api';
import type { PricingEntry, UsageTimeRange } from '@/lib/types';
import { USAGE_STATS_STALE_TIME_MS, useNotificationStore, useUsageStatsStore } from '@/stores';
import { downloadBlob } from '@/utils/download';
import { loadModelPrices, saveModelPrices, type ModelPrice } from '@/utils/usage';

export interface UsagePayload {
  total_requests?: number;
  success_count?: number;
  failure_count?: number;
  total_tokens?: number;
  apis?: Record<string, unknown>;
  requests_by_day?: Record<string, number>;
  requests_by_hour?: Record<string, number>;
  tokens_by_day?: Record<string, number>;
  tokens_by_hour?: Record<string, number>;
  [key: string]: unknown;
}

export interface UseUsageDataReturn {
  usage: UsagePayload | null;
  loading: boolean;
  error: string;
  lastRefreshedAt: Date | null;
  modelPrices: Record<string, ModelPrice>;
  setModelPrices: (prices: Record<string, ModelPrice>) => void;
  loadUsage: () => Promise<void>;
  handleExport: () => Promise<void>;
  handleImport: () => void;
  handleImportChange: (event: React.ChangeEvent<HTMLInputElement>) => Promise<void>;
  importInputRef: React.RefObject<HTMLInputElement | null>;
  exporting: boolean;
  importing: boolean;
}

const pricingToModelPrice = (entry: PricingEntry): ModelPrice => ({
  prompt: entry.prompt_price_per_1m,
  completion: entry.completion_price_per_1m,
  cache: entry.cache_price_per_1m
});

export interface UseUsageDataOptions {
  onAuthRequired?: () => void;
  range?: UsageTimeRange;
  customStart?: string;
  customEnd?: string;
  enabled?: boolean;
}

const toRangeQuery = (value: string): UsageTimeRange => (
  value === '4h' || value === '8h' || value === '12h' || value === '24h' || value === '7d' || value === 'all' || value === 'custom'
    ? value
    : 'all'
);

const toCustomBoundary = (value: string | undefined, endOfDay: boolean): string | undefined => {
  if (!value) return undefined;
  const suffix = endOfDay ? 'T23:59:59.999Z' : 'T00:00:00.000Z';
  const date = new Date(`${value}${suffix}`);
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
}

export function useUsageData(options: UseUsageDataOptions = {}): UseUsageDataReturn {
  const { onAuthRequired, range = 'all', customStart, customEnd, enabled = true } = options;
  const { t } = useTranslation();
  const { showNotification } = useNotificationStore();
  const usageSnapshot = useUsageStatsStore((state) => state.usage);
  const loading = useUsageStatsStore((state) => state.loading);
  const storeError = useUsageStatsStore((state) => state.error);
  const lastRefreshedAtTs = useUsageStatsStore((state) => state.lastRefreshedAt);
  const loadUsageStats = useUsageStatsStore((state) => state.loadUsageStats);

  const [modelPrices, setModelPricesState] = useState<Record<string, ModelPrice>>({});
  const [exporting, setExporting] = useState(false);
  const [importing, setImporting] = useState(false);
  const importInputRef = useRef<HTMLInputElement | null>(null);

  const resolvedRange = toRangeQuery(range);
  const requestStart = resolvedRange === 'custom' ? toCustomBoundary(customStart, false) : undefined;
  const requestEnd = resolvedRange === 'custom' ? toCustomBoundary(customEnd, true) : undefined;

  const loadUsage = useCallback(async () => {
    try {
      await loadUsageStats({
        force: true,
        staleTimeMs: USAGE_STATS_STALE_TIME_MS,
        range: resolvedRange,
        start: requestStart,
        end: requestEnd,
      });
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.();
      }
      throw error;
    }
  }, [loadUsageStats, onAuthRequired, requestEnd, requestStart, resolvedRange]);

  useEffect(() => {
    if (enabled) {
      void loadUsageStats({
        staleTimeMs: USAGE_STATS_STALE_TIME_MS,
        range: resolvedRange,
        start: requestStart,
        end: requestEnd,
      }).catch((error) => {
        if (error instanceof ApiError && error.status === 401) {
          onAuthRequired?.();
        }
      });
    }
    let cancelled = false;
    void Promise.allSettled([fetchPricing(), fetchUsedModels()]).then(([pricingResult]) => {
      if (cancelled) return;
      if (pricingResult.status === 'fulfilled') {
        const prices = Object.fromEntries(
          pricingResult.value.pricing.map((entry) => [entry.model, pricingToModelPrice(entry)])
        );
        setModelPricesState(prices);
        saveModelPrices(prices);
        return;
      }
      if (pricingResult.reason instanceof ApiError && pricingResult.reason.status === 401) {
        onAuthRequired?.();
        return;
      }
      setModelPricesState(loadModelPrices());
    });
    return () => {
      cancelled = true;
    };
  }, [customEnd, customStart, enabled, loadUsageStats, onAuthRequired, requestEnd, requestStart, resolvedRange]);

  const handleExport = async () => {
    setExporting(true);
    try {
      const payload = {
        exported_at: new Date().toISOString(),
        usage: usageSnapshot,
        pricing: Object.entries(modelPrices).map(([model, pricing]) => ({
          model,
          prompt_price_per_1m: pricing.prompt,
          completion_price_per_1m: pricing.completion,
          cache_price_per_1m: pricing.cache
        }))
      };
      const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
      downloadBlob({
        filename: `usage-export-${timestamp}.json`,
        blob: new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' })
      });
      showNotification(t('usage_stats.export_success'), 'success');
    } catch (error) {
      const message = error instanceof Error ? error.message : '';
      showNotification(
        `${t('notification.download_failed')}${message ? `: ${message}` : ''}`,
        'error'
      );
    } finally {
      setExporting(false);
    }
  };

  const handleImport = () => {
    importInputRef.current?.click();
  };

  const handleImportChange = async (event: React.ChangeEvent<HTMLInputElement>) => {
    event.target.value = '';
    showNotification(t('usage_stats.import_invalid'), 'error');
  };

  const setModelPrices = useCallback(
    async (prices: Record<string, ModelPrice>) => {
      const previousPrices = modelPrices;
      setModelPricesState(prices);
      saveModelPrices(prices);

      try {
        await Promise.all(
          Object.entries(prices).map(([model, pricing]) =>
            updatePricing(model, {
              prompt_price_per_1m: pricing.prompt,
              completion_price_per_1m: pricing.completion,
              cache_price_per_1m: pricing.cache
            })
          )
        );
      } catch (error) {
        setModelPricesState(previousPrices);
        saveModelPrices(previousPrices);
        if (error instanceof ApiError && error.status === 401) {
          onAuthRequired?.();
          return;
        }
        const message = error instanceof Error ? error.message : '';
        showNotification(
          `${t('notification.upload_failed')}${message ? `: ${message}` : ''}`,
          'error'
        );
      }
    },
    [modelPrices, onAuthRequired, showNotification, t]
  );

  return {
    usage: usageSnapshot as UsagePayload | null,
    loading,
    error: storeError || '',
    lastRefreshedAt: lastRefreshedAtTs ? new Date(lastRefreshedAtTs) : null,
    modelPrices,
    setModelPrices,
    loadUsage,
    handleExport,
    handleImport,
    handleImportChange,
    importInputRef,
    exporting,
    importing
  };
}
