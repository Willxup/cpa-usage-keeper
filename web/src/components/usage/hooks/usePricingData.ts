import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ApiError, deletePricing, fetchPricing, fetchPricingRules, fetchPricingSyncPreview, fetchUsedModels, replacePricingRules, updatePricing, updatePricingBatch } from '@/lib/api';
import type { ModelPrice, PricingEntry, PricingRule, PricingSaveResult, PricingStyle, PricingSyncPreviewResponse, ReplacePricingRuleInput } from '@/lib/types';
import { useNotificationStore } from '@/stores';

export interface UsePricingDataOptions {
  onAuthRequired?: () => void;
  enabled?: boolean;
}

export interface UsePricingDataReturn {
  modelNames: string[];
  modelPrices: Record<string, ModelPrice>;
  loading: boolean;
  error: string;
  loadPricing: () => Promise<void>;
  saveModelPrice: (model: string, price: ModelPrice) => Promise<void>;
  deleteModelPrice: (model: string) => Promise<void>;
  loadPricingRules: (model: string) => Promise<PricingRule[] | null>;
  savePricingRules: (model: string, rules: ReplacePricingRuleInput[]) => Promise<PricingRule[] | null>;
  syncModelPrices: (prices: Record<string, ModelPrice>) => Promise<PricingSaveResult>;
  previewPricingSync: () => Promise<PricingSyncPreviewResponse>;
}

const normalizePricingStyle = (style: PricingStyle | string | undefined): PricingStyle =>
  style === 'claude' ? 'claude' : 'openai';

export const pricingToModelPrice = (entry: PricingEntry): ModelPrice => ({
  style: normalizePricingStyle(entry.pricing_style),
  prompt: entry.prompt_price_per_1m,
  completion: entry.completion_price_per_1m,
  cacheRead: entry.cache_read_price_per_1m,
  cacheWrite: entry.cache_write_price_per_1m,
  multiplier: Number.isFinite(entry.price_multiplier) && entry.price_multiplier >= 0 ? entry.price_multiplier : 1,
});

const modelPriceToPricingEntry = (pricing: ModelPrice): Omit<PricingEntry, 'model'> => ({
  prompt_price_per_1m: pricing.prompt,
  completion_price_per_1m: pricing.completion,
  cache_read_price_per_1m: pricing.cacheRead,
  cache_write_price_per_1m: pricing.cacheWrite,
  price_multiplier: pricing.multiplier,
  pricing_style: pricing.style,
});

interface PricingPersistence {
  updatePricingEntries: typeof updatePricingBatch;
}

const defaultPricingPersistence: PricingPersistence = {
  updatePricingEntries: updatePricingBatch,
};

export async function persistModelPriceEntries(
  prices: Record<string, ModelPrice>,
  persistence: PricingPersistence = defaultPricingPersistence,
): Promise<PricingSaveResult> {
  const entries = Object.entries(prices).map(([model, pricing]) => ({
    model,
    ...modelPriceToPricingEntry(pricing),
  }));
  if (entries.length === 0) {
    return { successModels: [], failures: [] };
  }

  try {
    await persistence.updatePricingEntries(entries);
    return { successModels: entries.map((entry) => entry.model), failures: [] };
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return {
      successModels: [],
      failures: entries.map((entry) => ({ model: entry.model, message, error })),
    };
  }
}

export function usePricingData(options: UsePricingDataOptions = {}): UsePricingDataReturn {
  const { onAuthRequired, enabled = true } = options;
  const { t } = useTranslation();
  const { showNotification } = useNotificationStore();
  const [modelNames, setModelNames] = useState<string[]>([]);
  const [modelPrices, setModelPricesState] = useState<Record<string, ModelPrice>>({});
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const requestControllerRef = useRef<AbortController | null>(null);
  const pricingRulesReadRequestRef = useRef<{ id: number; controller: AbortController } | null>(null);
  const pricingRulesWriteRequestRef = useRef<{ id: number; controller: AbortController } | null>(null);
  const pricingRulesRequestIDRef = useRef(0);
  const onAuthRequiredRef = useRef(onAuthRequired);

  useEffect(() => {
    onAuthRequiredRef.current = onAuthRequired;
  }, [onAuthRequired]);

  const applyPricingResponse = useCallback((pricingResponse: Awaited<ReturnType<typeof fetchPricing>>) => {
    const prices = Object.fromEntries(
      pricingResponse.pricing.map((entry) => [entry.model, pricingToModelPrice(entry)])
    );
    setModelPricesState(prices);
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
      pricingRulesReadRequestRef.current?.controller.abort();
      pricingRulesWriteRequestRef.current?.controller.abort();
      requestControllerRef.current = null;
      pricingRulesReadRequestRef.current = null;
      pricingRulesWriteRequestRef.current = null;
      setLoading(false);
      return;
    }
    void loadPricing();
    return () => {
      requestControllerRef.current?.abort();
      pricingRulesReadRequestRef.current?.controller.abort();
      pricingRulesWriteRequestRef.current?.controller.abort();
      requestControllerRef.current = null;
      pricingRulesReadRequestRef.current = null;
      pricingRulesWriteRequestRef.current = null;
    };
  }, [enabled, loadPricing]);

  const saveModelPrice = useCallback(async (model: string, price: ModelPrice) => {
    try {
      await updatePricing(model, modelPriceToPricingEntry(price));
      setModelPricesState((current) => ({
        ...current,
        [model]: price,
      }));
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequiredRef.current?.();
        throw error;
      }
      const message = error instanceof Error ? error.message : '';
      showNotification(
        `${t('notification.upload_failed')}${message ? `: ${message}` : ''}`,
        'error'
      );
      throw error;
    }
  }, [showNotification, t]);

  const deleteModelPrice = useCallback(async (model: string) => {
    try {
      await deletePricing(model);
      setModelPricesState((current) => {
        const nextPrices = { ...current };
        delete nextPrices[model];
        return nextPrices;
      });
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequiredRef.current?.();
        throw error;
      }
      const message = error instanceof Error ? error.message : '';
      showNotification(
        `${t('notification.upload_failed')}${message ? `: ${message}` : ''}`,
        'error'
      );
      throw error;
    }
  }, [showNotification, t]);

  const loadPricingRules = useCallback(async (model: string): Promise<PricingRule[] | null> => {
    pricingRulesReadRequestRef.current?.controller.abort();
    const controller = new AbortController();
    const request = { id: ++pricingRulesRequestIDRef.current, controller };
    pricingRulesReadRequestRef.current = request;
    try {
      const response = await fetchPricingRules(model, controller.signal);
      if (pricingRulesReadRequestRef.current?.id !== request.id || response.model !== model) {
        return null;
      }
      return response.rules ?? [];
    } catch (error) {
      if (controller.signal.aborted || pricingRulesReadRequestRef.current?.id !== request.id) {
        return null;
      }
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequiredRef.current?.();
      }
      throw error;
    } finally {
      if (pricingRulesReadRequestRef.current?.id === request.id) {
        pricingRulesReadRequestRef.current = null;
      }
    }
  }, []);

  const savePricingRules = useCallback(async (
    model: string,
    rules: ReplacePricingRuleInput[],
  ): Promise<PricingRule[] | null> => {
    pricingRulesWriteRequestRef.current?.controller.abort();
    const controller = new AbortController();
    const request = { id: ++pricingRulesRequestIDRef.current, controller };
    pricingRulesWriteRequestRef.current = request;
    try {
      const response = await replacePricingRules({ model, rules }, controller.signal);
      if (pricingRulesWriteRequestRef.current?.id !== request.id || response.model !== model) {
        return null;
      }
      return response.rules ?? [];
    } catch (error) {
      if (controller.signal.aborted || pricingRulesWriteRequestRef.current?.id !== request.id) {
        return null;
      }
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequiredRef.current?.();
      }
      throw error;
    } finally {
      if (pricingRulesWriteRequestRef.current?.id === request.id) {
        pricingRulesWriteRequestRef.current = null;
      }
    }
  }, []);

  const syncModelPrices = useCallback(async (prices: Record<string, ModelPrice>) => {
    const result = await persistModelPriceEntries(prices);
    if (result.successModels.length > 0) {
      setModelPricesState((current) => {
        const nextPrices = { ...current };
        for (const model of result.successModels) {
          nextPrices[model] = prices[model];
        }
        return nextPrices;
      });
    }
    if (result.failures.some((failure) => failure.error instanceof ApiError && failure.error.status === 401)) {
      onAuthRequiredRef.current?.();
    }
    return result;
  }, []);

  const previewPricingSync = useCallback(async () => {
    try {
      return await fetchPricingSyncPreview();
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequiredRef.current?.();
      }
      throw error;
    }
  }, []);

  return {
    modelNames,
    modelPrices,
    loading,
    error,
    loadPricing,
    saveModelPrice,
    deleteModelPrice,
    loadPricingRules,
    savePricingRules,
    syncModelPrices,
    previewPricingSync,
  };
}
