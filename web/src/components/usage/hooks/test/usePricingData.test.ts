import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { persistModelPriceEntries, pricingToModelPrice } from '../usePricingData';

const source = readFileSync(new URL('../usePricingData.ts', import.meta.url), 'utf8');

const openAIPrice = {
  style: 'openai' as const,
  prompt: 2.5,
  completion: 10,
  cache: 1.25,
  cacheCreation: 0,
  multiplier: 1,
};

describe('usePricingData auth callback stability', () => {
  it('keeps pricing loaders stable when the auth callback reference changes', () => {
    expect(source).toContain('const onAuthRequiredRef = useRef(onAuthRequired);');
    expect(source).toContain('onAuthRequiredRef.current?.();');
    expect(source).not.toContain('}, [applyPricingResponse, onAuthRequired]);');
  });
});

describe('persistModelPriceEntries', () => {
  it('reports partial failures without blocking other pricing updates', async () => {
    const calls: string[] = [];

    const result = await persistModelPriceEntries({
      'gpt-4o': openAIPrice,
      'gpt-4o-mini': openAIPrice,
      'claude-sonnet': openAIPrice,
    }, {
      updatePricingEntry: async (model, pricing) => {
        calls.push(model);
        expect(pricing.price_multiplier).toBe(1);
        if (model === 'gpt-4o-mini') {
          throw new Error('network unavailable');
        }
        return { model, ...pricing };
      },
    });

    expect(calls).toEqual(['gpt-4o', 'gpt-4o-mini', 'claude-sonnet']);
    expect(result.successModels).toEqual(['gpt-4o', 'claude-sonnet']);
    expect(result.failures).toEqual([
      { model: 'gpt-4o-mini', message: 'network unavailable', error: expect.any(Error) },
    ]);
  });

  it('preserves an explicit zero price multiplier in update payloads', async () => {
    const payloads: number[] = [];

    const result = await persistModelPriceEntries({
      'free-model': {
        ...openAIPrice,
        multiplier: 0,
      },
    }, {
      updatePricingEntry: async (model, pricing) => {
        payloads.push(pricing.price_multiplier);
        return { model, ...pricing };
      },
    });

    expect(payloads).toEqual([0]);
    expect(result).toEqual({ successModels: ['free-model'], failures: [] });
  });
});

describe('pricingToModelPrice', () => {
  it('defaults invalid multipliers to 1 while preserving explicit zero', () => {
    const entry = {
      model: 'free-model',
      pricing_style: 'openai' as const,
      prompt_price_per_1m: 2.5,
      completion_price_per_1m: 10,
      cache_price_per_1m: 1.25,
      cache_creation_price_per_1m: 0,
      price_multiplier: 0,
    };

    expect(pricingToModelPrice(entry).multiplier).toBe(0);
    expect(pricingToModelPrice({ ...entry, price_multiplier: -1 }).multiplier).toBe(1);
    expect(pricingToModelPrice({ ...entry, price_multiplier: Number.NaN }).multiplier).toBe(1);
  });
});
