import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { persistModelPriceEntries, pricingToModelPrice } from '../usePricingData';

const source = readFileSync(new URL('../usePricingData.ts', import.meta.url), 'utf8');

const openAIPrice = {
  style: 'openai' as const,
  prompt: 2.5,
  completion: 10,
  cacheRead: 1.25,
  cacheWrite: 0,
  multiplier: 1,
};

describe('usePricingData auth callback stability', () => {
  it('keeps pricing loaders stable when the auth callback reference changes', () => {
    expect(source).toContain('const onAuthRequiredRef = useRef(onAuthRequired);');
    expect(source).toContain('onAuthRequiredRef.current?.();');
    expect(source).not.toContain('}, [applyPricingResponse, onAuthRequired]);');
  });

  it('persists single model price changes without submitting a full pricing snapshot', () => {
    const saveModelPriceStart = source.indexOf('const saveModelPrice = useCallback(async (model: string, price: ModelPrice) => {');
    const saveModelPriceEnd = source.indexOf('\n  const deleteModelPrice = useCallback', saveModelPriceStart);
    const saveModelPriceBlock = source.slice(saveModelPriceStart, saveModelPriceEnd);

    expect(source).toContain('saveModelPrice: (model: string, price: ModelPrice) => Promise<void>;');
    expect(source).not.toContain('setModelPrices: (prices: Record<string, ModelPrice>) => Promise<void>;');
    expect(saveModelPriceStart).toBeGreaterThanOrEqual(0);
    expect(saveModelPriceBlock).toContain('await updatePricing(model, modelPriceToPricingEntry(price));');
    expect(saveModelPriceBlock).toContain('setModelPricesState((current) => ({');
    expect(saveModelPriceBlock).toContain('[model]: price,');
    expect(saveModelPriceBlock).toContain('throw error;');
  });

  it('persists single model deletions without rebuilding the full pricing snapshot', () => {
    const deleteModelPriceStart = source.indexOf('const deleteModelPrice = useCallback(async (model: string) => {');
    const deleteModelPriceEnd = source.indexOf('\n  const syncModelPrices = useCallback', deleteModelPriceStart);
    const deleteModelPriceBlock = source.slice(deleteModelPriceStart, deleteModelPriceEnd);

    expect(source).toContain('deleteModelPrice: (model: string) => Promise<void>;');
    expect(deleteModelPriceStart).toBeGreaterThanOrEqual(0);
    expect(deleteModelPriceBlock).toContain('await deletePricing(model);');
    expect(deleteModelPriceBlock).toContain('setModelPricesState((current) => {');
    expect(deleteModelPriceBlock).toContain('delete nextPrices[model];');
    expect(deleteModelPriceBlock).toContain('throw error;');
  });

  it('binds pricing rule reads and writes to a model-specific latest request', () => {
    expect(source).toContain('const pricingRulesReadRequestRef = useRef')
    expect(source).toContain('const pricingRulesWriteRequestRef = useRef')
    expect(source).toContain('pricingRulesReadRequestRef.current?.controller.abort()')
    expect(source).toContain('pricingRulesWriteRequestRef.current?.controller.abort()')
    expect(source).toContain('await fetchPricingRules(model, controller.signal)')
    expect(source).toContain('await replacePricingRules({ model, rules }, controller.signal)')
    expect(source).toContain('response.model !== model')
    expect(source).toContain('return null')
  })
});

describe('persistModelPriceEntries', () => {
	it('persists every model through one atomic batch update', async () => {
	let calls = 0;

	const result = await persistModelPriceEntries({
	  'gpt-4o': openAIPrice,
	  'gpt-4o-mini': openAIPrice,
	  'claude-sonnet': openAIPrice,
	}, {
	  updatePricingEntries: async (pricing) => {
		calls += 1;
		expect(pricing.map((entry) => entry.model)).toEqual(['gpt-4o', 'gpt-4o-mini', 'claude-sonnet']);
		expect(pricing.every((entry) => entry.price_multiplier === 1)).toBe(true);
		return { pricing };
	  },
	});

	expect(calls).toBe(1);
	expect(result.successModels).toEqual(['gpt-4o', 'gpt-4o-mini', 'claude-sonnet']);
	expect(result.failures).toEqual([]);
	});

	it('reports one atomic batch failure against every submitted model', async () => {
	const error = new Error('network unavailable');
	const result = await persistModelPriceEntries({
	  'gpt-4o': openAIPrice,
	  'gpt-4o-mini': openAIPrice,
	}, {
	  updatePricingEntries: async () => { throw error; },
	});

	expect(result).toEqual({
	  successModels: [],
	  failures: [
		{ model: 'gpt-4o', message: 'network unavailable', error },
		{ model: 'gpt-4o-mini', message: 'network unavailable', error },
	  ],
	});
  });

  it('preserves an explicit zero price multiplier in update payloads', async () => {
    const payloads: number[] = [];

    const result = await persistModelPriceEntries({
      'free-model': {
        ...openAIPrice,
        multiplier: 0,
      },
    }, {
	  updatePricingEntries: async (pricing) => {
		payloads.push(pricing[0].price_multiplier);
		return { pricing };
      },
    });

    expect(payloads).toEqual([0]);
    expect(result).toEqual({ successModels: ['free-model'], failures: [] });
  });

	it('preserves Models.dev OpenAI cache read and write prices in update payloads', async () => {
		const payloads: Array<{ cacheRead: number; cacheWrite: number }> = [];

		const result = await persistModelPriceEntries({
			'gpt-5.6-terra': {
				...openAIPrice,
				cacheRead: 0.25,
				cacheWrite: 3.125,
			},
		}, {
		  updatePricingEntries: async (pricing) => {
			payloads.push({
			  cacheRead: pricing[0].cache_read_price_per_1m,
			  cacheWrite: pricing[0].cache_write_price_per_1m,
			});
			return { pricing };
		  },
		});

		expect(payloads).toEqual([{ cacheRead: 0.25, cacheWrite: 3.125 }]);
		expect(result).toEqual({ successModels: ['gpt-5.6-terra'], failures: [] });
	});
});

describe('pricingToModelPrice', () => {
  it('defaults invalid multipliers to 1 while preserving explicit zero', () => {
    const entry = {
      model: 'free-model',
      pricing_style: 'openai' as const,
      prompt_price_per_1m: 2.5,
      completion_price_per_1m: 10,
      cache_read_price_per_1m: 1.25,
      cache_write_price_per_1m: 0,
      price_multiplier: 0,
    };

    expect(pricingToModelPrice(entry).multiplier).toBe(0);
    expect(pricingToModelPrice({ ...entry, price_multiplier: -1 }).multiplier).toBe(1);
    expect(pricingToModelPrice({ ...entry, price_multiplier: Number.NaN }).multiplier).toBe(1);
  });
});
