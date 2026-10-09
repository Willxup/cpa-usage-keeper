// @vitest-environment happy-dom

import { act, useEffect } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import * as api from '@/lib/api';
import type { PricingEntry, PricingRulesResponse } from '@/lib/types';
import { persistModelPriceEntries, pricingToModelPrice, usePricingData } from '../usePricingData';

vi.mock('@/lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/lib/api')>(),
  fetchPricing: vi.fn(),
  fetchUsedModels: vi.fn(),
  updatePricing: vi.fn(),
  deletePricing: vi.fn(),
  fetchPricingRules: vi.fn(),
  replacePricingRules: vi.fn(),
}));
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock('@/stores', () => ({ useNotificationStore: () => ({ showNotification: vi.fn() }) }));

const openAIPrice = {
  style: 'openai' as const,
  prompt: 2.5,
  completion: 10,
  cacheRead: 1.25,
  cacheWrite: 0,
  multiplier: 1,
};

const pricingEntry: PricingEntry = {
  model: 'existing',
  pricing_style: 'openai',
  prompt_price_per_1m: 2.5,
  completion_price_per_1m: 10,
  cache_read_price_per_1m: 1.25,
  cache_write_price_per_1m: 0,
  price_multiplier: 1,
};

let latest: ReturnType<typeof usePricingData> | null = null;
function Harness({ onAuthRequired }: { onAuthRequired?: () => void }) {
  const result = usePricingData({ onAuthRequired });
  useEffect(() => { latest = result; }, [result]);
  return null;
}

describe('usePricingData', () => {
  let root: Root;
  let container: HTMLDivElement;

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    vi.resetAllMocks();
    vi.mocked(api.fetchPricing).mockResolvedValue({ pricing: [pricingEntry] });
    vi.mocked(api.fetchUsedModels).mockResolvedValue({ models: ['existing'] });
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    latest = null;
  });

  it('keeps the loader stable while using the latest auth callback', async () => {
    const original = vi.fn();
    const replacement = vi.fn();
    await act(async () => root.render(<Harness onAuthRequired={original} />));
    const loadPricing = latest!.loadPricing;
    await act(async () => root.render(<Harness onAuthRequired={replacement} />));

    expect(latest!.loadPricing).toBe(loadPricing);
    expect(api.fetchPricing).toHaveBeenCalledOnce();
    vi.mocked(api.fetchPricing).mockRejectedValueOnce(new api.ApiError('auth_required', 401));
    await act(async () => latest!.loadPricing());
    expect(original).not.toHaveBeenCalled();
    expect(replacement).toHaveBeenCalledOnce();
  });

  it('updates only the saved or deleted model and preserves state on failed mutations', async () => {
    await act(async () => root.render(<Harness />));
    await act(async () => latest!.saveModelPrice('new', openAIPrice));
    const { model: _model, ...pricePayload } = pricingEntry;
    expect(api.updatePricing).toHaveBeenCalledWith('new', pricePayload);
    expect(latest!.modelPrices).toEqual({ existing: openAIPrice, new: openAIPrice });

    await act(async () => latest!.deleteModelPrice('existing'));
    expect(api.deletePricing).toHaveBeenCalledWith('existing');
    expect(latest!.modelPrices).toEqual({ new: openAIPrice });

    const error = new Error('mutation failed');
    vi.mocked(api.updatePricing).mockRejectedValueOnce(error);
    vi.mocked(api.deletePricing).mockRejectedValueOnce(error);
    await act(async () => {
      await expect(latest!.saveModelPrice('new', { ...openAIPrice, prompt: 9 })).rejects.toBe(error);
      await expect(latest!.deleteModelPrice('new')).rejects.toBe(error);
    });
    expect(latest!.modelPrices).toEqual({ new: openAIPrice });
  });

  it.each(['read', 'write'] as const)('keeps only the latest model-specific rule %s response', async (mode) => {
    await act(async () => root.render(<Harness />));
    const pending = Promise.withResolvers<PricingRulesResponse>();
    const request = vi.mocked(mode === 'read' ? api.fetchPricingRules : api.replacePricingRules);
    const start = (model: string) => mode === 'read'
      ? latest!.loadPricingRules(model)
      : latest!.savePricingRules(model, []);
    const rules = [{ key: 'service_tier', value: 'priority', multiplier: 2 }];
    request.mockReturnValueOnce(pending.promise).mockResolvedValueOnce({ model: 'b', rules });

    const first = start('a');
    await expect(start('b')).resolves.toEqual(rules);
    expect(request.mock.calls[0][1]?.aborted).toBe(true);
    expect(request.mock.calls[0][0]).toEqual(mode === 'read' ? 'a' : { model: 'a', rules: [] });
    pending.resolve({ model: 'a', rules: [] });
    await expect(first).resolves.toBeNull();

    request.mockResolvedValueOnce({ model: 'wrong-model', rules });
    await expect(start('b')).resolves.toBeNull();
  });
});

describe('persistModelPriceEntries', () => {
  it('persists every model through one atomic batch update', async () => {
    const updatePricingEntries = vi.fn(async (pricing: PricingEntry[]) => ({ pricing }));
    const result = await persistModelPriceEntries({
      'gpt-4o': openAIPrice,
      'gpt-4o-mini': openAIPrice,
      'claude-sonnet': openAIPrice,
    }, { updatePricingEntries });

    expect(updatePricingEntries).toHaveBeenCalledOnce();
    expect(updatePricingEntries.mock.calls[0][0].map((entry) => [entry.model, entry.price_multiplier]))
      .toEqual([['gpt-4o', 1], ['gpt-4o-mini', 1], ['claude-sonnet', 1]]);
    expect(result).toEqual({ successModels: ['gpt-4o', 'gpt-4o-mini', 'claude-sonnet'], failures: [] });
  });

  it('reports one atomic batch failure against every submitted model', async () => {
    const error = new Error('network unavailable');
    const result = await persistModelPriceEntries({
      'gpt-4o': openAIPrice,
      'gpt-4o-mini': openAIPrice,
    }, { updatePricingEntries: async () => { throw error; } });

    expect(result).toEqual({
      successModels: [],
      failures: [
        { model: 'gpt-4o', message: 'network unavailable', error },
        { model: 'gpt-4o-mini', message: 'network unavailable', error },
      ],
    });
  });

  it.each([
    ['free-model', { multiplier: 0 }, { price_multiplier: 0 }],
    ['gpt-5.6-terra', { cacheRead: 0.25, cacheWrite: 3.125 }, { cache_read_price_per_1m: 0.25, cache_write_price_per_1m: 3.125 }],
  ] as const)('preserves special pricing values for %s', async (model, overrides, expected) => {
    const updatePricingEntries = vi.fn(async (pricing: PricingEntry[]) => ({ pricing }));
    const result = await persistModelPriceEntries({
      [model]: { ...openAIPrice, ...overrides },
    }, { updatePricingEntries });

    expect(updatePricingEntries).toHaveBeenCalledWith([expect.objectContaining({ model, ...expected })]);
    expect(result).toEqual({ successModels: [model], failures: [] });
  });
});

describe('pricingToModelPrice', () => {
  it('defaults invalid multipliers to 1 while preserving explicit zero', () => {
    expect(pricingToModelPrice({ ...pricingEntry, price_multiplier: 0 }).multiplier).toBe(0);
    expect(pricingToModelPrice({ ...pricingEntry, price_multiplier: -1 }).multiplier).toBe(1);
    expect(pricingToModelPrice({ ...pricingEntry, price_multiplier: Number.NaN }).multiplier).toBe(1);
  });
});
