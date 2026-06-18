import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ApiError, deletePricing, fetchPricing, fetchPricingSyncPreview, fetchUsedModels, updatePricing } from '@/lib/api';
import { buildPricingEntryKey, normalizePricingServiceTier, type PricingEntry, type PricingSaveResult, type PricingServiceTier, type PricingStyle, type PricingSyncPreviewResponse, type PricingSyncSource } from '@/lib/types';
import { useNotificationStore } from '@/stores';

export interface UsePricingDataOptions {
  onAuthRequired?: () => void;
  enabled?: boolean;
}

export interface UsePricingDataReturn {
  modelNames: string[];
  pricingEntries: PricingEntry[];
  loading: boolean;
  error: string;
  lastRefreshedAt: Date | null;
  loadPricing: () => Promise<void>;
  setPricingEntries: (entries: PricingEntry[]) => Promise<void>;
  syncPricingEntries: (entries: PricingEntry[]) => Promise<PricingSaveResult>;
  previewPricingSync: (source: PricingSyncSource) => Promise<PricingSyncPreviewResponse>;
}

const pricingServiceTierOrder: Record<PricingServiceTier, number> = {
  '': 0,
  default: 1,
  priority: 2,
};

const normalizePricingStyle = (style: PricingStyle | string | undefined): PricingStyle =>
  style === 'claude' ? 'claude' : 'openai';

const normalizePricingEntry = (entry: PricingEntry): PricingEntry => ({
  model: entry.model.trim(),
  service_tier: normalizePricingServiceTier(entry.service_tier),
  pricing_style: normalizePricingStyle(entry.pricing_style),
  prompt_price_per_1m: entry.prompt_price_per_1m,
  completion_price_per_1m: entry.completion_price_per_1m,
  cache_price_per_1m: entry.cache_price_per_1m,
  cache_creation_price_per_1m: entry.cache_creation_price_per_1m ?? 0,
});

const sortPricingEntries = (entries: PricingEntry[]): PricingEntry[] => (
  [...entries].sort((left, right) => {
    const modelOrder = left.model.localeCompare(right.model);
    if (modelOrder !== 0) return modelOrder;
    return pricingServiceTierOrder[left.service_tier] - pricingServiceTierOrder[right.service_tier];
  })
);

const normalizePricingEntries = (entries: PricingEntry[]): PricingEntry[] => {
  const deduped = new Map<string, PricingEntry>();
  for (const entry of entries) {
    const normalized = normalizePricingEntry(entry);
    if (!normalized.model) continue;
    deduped.set(buildPricingEntryKey(normalized), normalized);
  }
  return sortPricingEntries(Array.from(deduped.values()));
};

const pricingEntryPayload = (entry: PricingEntry): Omit<PricingEntry, 'model'> => ({
  service_tier: entry.service_tier,
  pricing_style: entry.pricing_style,
  prompt_price_per_1m: entry.prompt_price_per_1m,
  completion_price_per_1m: entry.completion_price_per_1m,
  cache_price_per_1m: entry.cache_price_per_1m,
  cache_creation_price_per_1m: entry.cache_creation_price_per_1m,
});

interface PricingPersistence {
  updatePricingEntry: typeof updatePricing;
}

const defaultPricingPersistence: PricingPersistence = {
  updatePricingEntry: updatePricing,
};

export async function persistPricingEntries(
  entries: PricingEntry[],
  persistence: PricingPersistence = defaultPricingPersistence,
): Promise<PricingSaveResult> {
  const settled = await Promise.all(normalizePricingEntries(entries).map(async (entry) => {
    const pricingKey = buildPricingEntryKey(entry);
    try {
      await persistence.updatePricingEntry(entry.model, pricingEntryPayload(entry));
      return {
        pricingKey,
        model: entry.model,
        serviceTier: entry.service_tier,
        ok: true as const,
      };
    } catch (error) {
      return {
        pricingKey,
        model: entry.model,
        serviceTier: entry.service_tier,
        ok: false as const,
        message: error instanceof Error ? error.message : String(error),
        error,
      };
    }
  }));

  return settled.reduce<PricingSaveResult>((result, item) => {
    if (item.ok) {
      result.success_keys.push(item.pricingKey);
    } else {
      result.failures.push({
        model: item.model,
        service_tier: item.serviceTier,
        pricing_key: item.pricingKey,
        message: item.message,
        error: item.error,
      });
    }
    return result;
  }, { success_keys: [], failures: [] });
}

export function usePricingData(options: UsePricingDataOptions = {}): UsePricingDataReturn {
  const { onAuthRequired, enabled = true } = options;
  const { t } = useTranslation();
  const { showNotification } = useNotificationStore();
  const [modelNames, setModelNames] = useState<string[]>([]);
  const [pricingEntries, setPricingEntriesState] = useState<PricingEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [lastRefreshedAt, setLastRefreshedAt] = useState<Date | null>(null);
  const requestControllerRef = useRef<AbortController | null>(null);
  const onAuthRequiredRef = useRef(onAuthRequired);

  useEffect(() => {
    onAuthRequiredRef.current = onAuthRequired;
  }, [onAuthRequired]);

  const applyPricingResponse = useCallback((pricingResponse: Awaited<ReturnType<typeof fetchPricing>>) => {
    setPricingEntriesState(normalizePricingEntries(pricingResponse.pricing ?? []));
    setLastRefreshedAt(new Date());
  }, []);

  const loadPricing = useCallback(async () => {
    requestControllerRef.current?.abort();
    const controller = new AbortController();
    requestControllerRef.current = controller;

    setLoading(true);
    setError('');

    try {
      const [pricingResponse, usedModelsResponse] = await Promise.all([
        fetchPricing(controller.signal),
        fetchUsedModels(controller.signal),
      ]);
      if (requestControllerRef.current !== controller) {
        return;
      }
      applyPricingResponse(pricingResponse);
      setModelNames(usedModelsResponse.models);
    } catch (error) {
      if (controller.signal.aborted) {
        return;
      }
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequiredRef.current?.();
        return;
      }
      setError(error instanceof Error ? error.message : 'Failed to load pricing');
    } finally {
      if (requestControllerRef.current === controller) {
        setLoading(false);
        requestControllerRef.current = null;
      }
    }
  }, [applyPricingResponse]);

  useEffect(() => {
    if (!enabled) {
      requestControllerRef.current?.abort();
      requestControllerRef.current = null;
      setLoading(false);
      return;
    }
    void loadPricing();
    return () => {
      requestControllerRef.current?.abort();
      requestControllerRef.current = null;
    };
  }, [enabled, loadPricing]);

  const setPricingEntries = useCallback(async (entries: PricingEntry[]) => {
    const previousEntries = pricingEntries;
    const nextEntries = normalizePricingEntries(entries);
    setPricingEntriesState(nextEntries);

    try {
      const nextKeys = new Set(nextEntries.map((entry) => buildPricingEntryKey(entry)));
      await Promise.all([
        ...nextEntries.map((entry) => updatePricing(entry.model, pricingEntryPayload(entry))),
        ...previousEntries
          .filter((entry) => !nextKeys.has(buildPricingEntryKey(entry)))
          .map((entry) => deletePricing(entry.model, entry.service_tier)),
      ]);
      setLastRefreshedAt(new Date());
    } catch (error) {
      setPricingEntriesState(previousEntries);
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequiredRef.current?.();
        return;
      }
      const message = error instanceof Error ? error.message : '';
      showNotification(
        `${t('notification.upload_failed')}${message ? `: ${message}` : ''}`,
        'error'
      );
    }
  }, [pricingEntries, showNotification, t]);

  const syncPricingEntries = useCallback(async (entries: PricingEntry[]) => {
    const normalizedEntries = normalizePricingEntries(entries);
    const result = await persistPricingEntries(normalizedEntries);
    if (result.success_keys.length > 0) {
      setPricingEntriesState((current) => {
        const nextEntries = new Map(current.map((entry) => [buildPricingEntryKey(entry), entry]));
        const successKeys = new Set(result.success_keys);
        for (const entry of normalizedEntries) {
          const entryKey = buildPricingEntryKey(entry);
          if (successKeys.has(entryKey)) {
            nextEntries.set(entryKey, entry);
          }
        }
        return sortPricingEntries(Array.from(nextEntries.values()));
      });
      setLastRefreshedAt(new Date());
    }
    if (result.failures.some((failure) => failure.error instanceof ApiError && failure.error.status === 401)) {
      onAuthRequiredRef.current?.();
    }
    return result;
  }, []);

  const previewPricingSync = useCallback(async (source: PricingSyncSource) => {
    try {
      return await fetchPricingSyncPreview(source);
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequiredRef.current?.();
      }
      throw error;
    }
  }, []);

  return {
    modelNames,
    pricingEntries,
    loading,
    error,
    lastRefreshedAt,
    loadPricing,
    setPricingEntries,
    syncPricingEntries,
    previewPricingSync,
  };
}
