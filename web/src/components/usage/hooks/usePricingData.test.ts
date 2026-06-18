import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { buildPricingEntryKey } from '@/lib/types';
import { persistPricingEntries } from './usePricingData';

const source = readFileSync(new URL('./usePricingData.ts', import.meta.url), 'utf8');

const openAIPrice = {
  style: 'openai' as const,
  prompt: 2.5,
  completion: 10,
  cache: 1.25,
  cacheCreation: 0,
};

describe('usePricingData auth callback stability', () => {
  it('keeps pricing loaders stable when the auth callback reference changes', () => {
    expect(source).toContain('const onAuthRequiredRef = useRef(onAuthRequired);');
    expect(source).toContain('onAuthRequiredRef.current?.();');
    expect(source).not.toContain('}, [applyPricingResponse, onAuthRequired]);');
  });
});

describe('persistPricingEntries', () => {
  it('reports partial failures without blocking other pricing updates for different service tiers', async () => {
    const calls: string[] = [];

    const result = await persistPricingEntries([
      {
        model: 'gpt-4o',
        service_tier: 'default',
        pricing_style: openAIPrice.style,
        prompt_price_per_1m: openAIPrice.prompt,
        completion_price_per_1m: openAIPrice.completion,
        cache_price_per_1m: openAIPrice.cache,
        cache_creation_price_per_1m: openAIPrice.cacheCreation,
      },
      {
        model: 'gpt-4o',
        service_tier: 'priority',
        pricing_style: openAIPrice.style,
        prompt_price_per_1m: 4.25,
        completion_price_per_1m: 17,
        cache_price_per_1m: 2.125,
        cache_creation_price_per_1m: 0,
      },
      {
        model: 'claude-sonnet',
        service_tier: '',
        pricing_style: openAIPrice.style,
        prompt_price_per_1m: openAIPrice.prompt,
        completion_price_per_1m: openAIPrice.completion,
        cache_price_per_1m: openAIPrice.cache,
        cache_creation_price_per_1m: openAIPrice.cacheCreation,
      },
    ], {
      updatePricingEntry: async (model, pricing) => {
        calls.push(buildPricingEntryKey({ model, service_tier: pricing.service_tier }));
        if (model === 'gpt-4o' && pricing.service_tier === 'priority') {
          throw new Error('network unavailable');
        }
        return { model, ...pricing };
      },
    });

    expect(calls).toEqual([
      'claude-sonnet::',
      'gpt-4o::default',
      'gpt-4o::priority',
    ]);
    expect(result.success_keys).toEqual([
      'claude-sonnet::',
      'gpt-4o::default',
    ]);
    expect(result.failures).toEqual([
      {
        model: 'gpt-4o',
        service_tier: 'priority',
        pricing_key: 'gpt-4o::priority',
        message: 'network unavailable',
        error: expect.any(Error),
      },
    ]);
  });
});
