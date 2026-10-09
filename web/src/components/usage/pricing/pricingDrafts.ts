import { ApiError } from '@/lib/api';
import type { SelectOption } from '@/components/ui/Select';
import type { ModelPrice, PricingSaveResult, PricingStyle, PricingSyncMatch, PricingSyncSource } from '@/lib/types';

export type PricingNotice = ((kind: 'success' | 'info' | 'error', message: string) => void) | undefined;
export const PRICING_SYNC_SOURCES = [
  { value: 'models-dev', label: 'Models.dev' },
  { value: 'litellm', label: 'LiteLLM' },
] as const;
const sourceStorageKey = 'cpa-pricing-sync-source-v1';

export function readPricingSyncSource(): PricingSyncSource {
  try {
    return window.localStorage.getItem(sourceStorageKey) === 'litellm' ? 'litellm' : 'models-dev';
  } catch {
    return 'models-dev';
  }
}

export function storePricingSyncSource(source: PricingSyncSource): void {
  try { window.localStorage.setItem(sourceStorageKey, source); } catch { /* 浏览器存储不可用时仅保留当前选择。 */ }
}

export const formatDisplayName = (value: string): string => {
  const normalized = value.trim();
  if (!normalized) return '-';
  return normalized;
};

export const pricingStyleOptions = (t: (key: string) => string): SelectOption[] => [
  { value: 'openai', label: t('usage_stats.model_price_style_openai') },
  { value: 'claude', label: t('usage_stats.model_price_style_claude') },
];

export interface PricingSyncDraft {
  model: string;
  matchedModel: string;
  matchType: string;
  sourceProviderId: string;
  sourceProviderName: string;
  selected: boolean;
  style: PricingStyle;
  prompt: string;
  completion: string;
  cacheRead: string;
  cacheWrite: string;
  multiplier: string;
  saveStatus?: 'failed';
  saveError?: string;
}

export interface PricingDraftInput {
  style: PricingStyle;
  prompt: string;
  completion: string;
  cacheRead: string;
  cacheWrite: string;
  multiplier: string;
}

const parsePriceValue = (value: string): number | null => {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null;
};

const parseMultiplierValue = (value: string): number | null => {
  if (value.trim() === '') return 1;
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null;
};

const parseOptionalCachePriceValue = (value: string): number | null => (
  value.trim() === '' ? 0 : parsePriceValue(value)
);

export const priceToInputValue = (value: number | undefined): string => (
  typeof value === 'number' && Number.isFinite(value) ? value.toString() : ''
);

const normalizePricingStyle = (style: PricingStyle | string | undefined): PricingStyle => (
  style === 'claude' ? 'claude' : 'openai'
);

export const syncMatchToDraft = (match: PricingSyncMatch, existingPrice?: ModelPrice): PricingSyncDraft => ({
  model: match.model,
  matchedModel: match.matched_model,
  matchType: match.match_type,
  sourceProviderId: match.source_provider_id,
  sourceProviderName: match.source_provider_name,
  selected: true,
  style: normalizePricingStyle(match.pricing_style),
  prompt: priceToInputValue(match.prompt_price_per_1m),
  completion: priceToInputValue(match.completion_price_per_1m),
  cacheRead: priceToInputValue(match.cache_read_price_per_1m),
  cacheWrite: priceToInputValue(match.cache_write_price_per_1m),
  multiplier: priceToInputValue(existingPrice?.multiplier ?? 1),
});

export const pricingDraftToModelPrice = (draft: PricingDraftInput): ModelPrice | null => {
  const prompt = parsePriceValue(draft.prompt);
  const completion = parsePriceValue(draft.completion);
  if (prompt === null || completion === null) return null;
  const cacheRead = parseOptionalCachePriceValue(draft.cacheRead);
  const cacheWrite = parseOptionalCachePriceValue(draft.cacheWrite);
  const multiplier = parseMultiplierValue(draft.multiplier);
  if (cacheRead === null || cacheWrite === null || multiplier === null) return null;
  return {
    style: draft.style,
    prompt,
    completion,
    cacheRead,
    cacheWrite,
    multiplier,
  };
};

export const syncDraftToModelPrice = (draft: PricingSyncDraft): ModelPrice | null => (
  pricingDraftToModelPrice(draft)
);

export const markPricingSyncFailures = (
  drafts: PricingSyncDraft[],
  result: PricingSaveResult,
): PricingSyncDraft[] => {
  const failedByModel = new Map(result.failures.map((failure) => [failure.model, failure.message]));
  const successModels = new Set(result.successModels);
  return drafts.map((draft) => {
    const failureMessage = failedByModel.get(draft.model);
    if (failureMessage !== undefined) {
      return {
        ...draft,
        selected: true,
        saveStatus: 'failed',
        saveError: failureMessage,
      };
    }
    if (successModels.has(draft.model)) {
      return {
        ...draft,
        selected: false,
        saveStatus: undefined,
        saveError: undefined,
      };
    }
    return {
      ...draft,
      saveStatus: undefined,
      saveError: undefined,
    };
  });
};

export const notifyPricingSyncUnexpectedError = (
  error: unknown,
  t: (key: string, options?: { source: string }) => string,
  onNotice: PricingNotice,
  sourceName = 'Models.dev',
) => {
  if (error instanceof ApiError && error.status === 504) {
    onNotice?.('error', t('usage_stats.model_price_sync_timeout', { source: sourceName }));
    return;
  }

  const message = error instanceof Error ? error.message : '';
  onNotice?.(
    'error',
    `${t('usage_stats.model_price_sync_failed')}${message ? `: ${message}` : ''}`,
  );
};

export const notifyPricingSyncFailures = (
  result: PricingSaveResult,
  t: (key: string, options?: { success: number; failed: number }) => string,
  onNotice: PricingNotice,
) => {
  if (result.failures.length === 0) return;
  const summary = t('usage_stats.model_price_sync_apply_partial', {
    success: result.successModels.length,
    failed: result.failures.length,
  });
  const detail = result.failures.find((failure) => failure.message.trim())?.message.trim() ?? '';
  onNotice?.(
    result.successModels.length > 0 ? 'info' : 'error',
    `${summary}${detail ? `: ${detail}` : ''}`,
  );
};

export interface SelectedSyncPrices {
  selectedDrafts: PricingSyncDraft[];
  prices: Record<string, ModelPrice>;
  invalidModel: string | null;
}

export const buildSelectedSyncPrices = (drafts: PricingSyncDraft[]): SelectedSyncPrices => {
  const selectedDrafts = drafts.filter((draft) => draft.selected);
  const prices: Record<string, ModelPrice> = {};
  for (const draft of selectedDrafts) {
    const price = syncDraftToModelPrice(draft);
    if (!price) {
      return { selectedDrafts, prices: {}, invalidModel: draft.model };
    }
    prices[draft.model] = price;
  }
  return { selectedDrafts, prices, invalidModel: null };
};
