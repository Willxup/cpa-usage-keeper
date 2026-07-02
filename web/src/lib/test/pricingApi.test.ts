import { afterEach, describe, expect, it, vi } from 'vitest';
import { updatePricing } from '../api';

const headerValue = (init: RequestInit | undefined, name: string): string | null => new Headers(init?.headers).get(name);

describe('pricing API client', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('updates one model through the pricing endpoint without sending a pricing snapshot', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        model: 'openai/gpt-4.1',
        pricing_style: 'openai',
        prompt_price_per_1m: 3,
        completion_price_per_1m: 15,
        cache_price_per_1m: 0.3,
        cache_creation_price_per_1m: 0,
        price_multiplier: 1,
      }),
    } as Response);

    await updatePricing('openai/gpt-4.1', {
      pricing_style: 'openai',
      prompt_price_per_1m: 3,
      completion_price_per_1m: 15,
      cache_price_per_1m: 0.3,
      cache_creation_price_per_1m: 0,
      price_multiplier: 1,
    });

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');
    const body = JSON.parse(String(init?.body));

    expect(parsed.pathname).toBe('/api/v1/pricing');
    expect(init).toMatchObject({ credentials: 'include', method: 'PUT' });
    expect(headerValue(init, 'Content-Type')).toBe('application/json');
    expect(body).toEqual({
      model: 'openai/gpt-4.1',
      pricing_style: 'openai',
      prompt_price_per_1m: 3,
      completion_price_per_1m: 15,
      cache_price_per_1m: 0.3,
      cache_creation_price_per_1m: 0,
      price_multiplier: 1,
    });
    expect(body).not.toHaveProperty('pricing');
  });
});
