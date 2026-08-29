import { afterEach, describe, expect, it, vi } from 'vitest'
import { deletePricing, fetchPricingRules, replacePricingRules, updatePricing, updatePricingBatch } from '../api'

const headerValue = (init: RequestInit | undefined, name: string): string | null => (
  new Headers(init?.headers).get(name)
)

describe('pricing API client', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('updates one model through the pricing endpoint without sending a pricing snapshot', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined })
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        model: 'openai/gpt-4.1',
        pricing_style: 'openai',
        prompt_price_per_1m: 3,
        completion_price_per_1m: 15,
        cache_read_price_per_1m: 0.3,
        cache_write_price_per_1m: 0,
        price_multiplier: 1,
      }),
    } as Response)

    await updatePricing('openai/gpt-4.1', {
      pricing_style: 'openai',
      prompt_price_per_1m: 3,
      completion_price_per_1m: 15,
      cache_read_price_per_1m: 0.3,
      cache_write_price_per_1m: 0,
      price_multiplier: 1,
    })

    const [url, init] = fetchMock.mock.calls[0]
    const parsed = new URL(String(url), 'http://localhost')
    const body = JSON.parse(String(init?.body))

    expect(parsed.pathname).toBe('/api/v1/pricing')
    expect(init).toMatchObject({ credentials: 'include', method: 'PUT' })
    expect(headerValue(init, 'Content-Type')).toBe('application/json')
    expect(body).toEqual({
      model: 'openai/gpt-4.1',
      pricing_style: 'openai',
      prompt_price_per_1m: 3,
      completion_price_per_1m: 15,
      cache_read_price_per_1m: 0.3,
      cache_write_price_per_1m: 0,
      price_multiplier: 1,
    })
    expect(body).not.toHaveProperty('pricing')
  })

  it('deletes one model through the pricing endpoint without sending a request body', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined })
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
    } as Response)

    await deletePricing('openai/gpt-4.1')

    const [url, init] = fetchMock.mock.calls[0]
    const parsed = new URL(String(url), 'http://localhost')

    expect(parsed.pathname).toBe('/api/v1/pricing')
    expect(parsed.searchParams.get('model')).toBe('openai/gpt-4.1')
    expect(init).toMatchObject({ credentials: 'include', method: 'DELETE' })
    expect(init?.body).toBeUndefined()
  })

  it('updates multiple model prices in one batch request', async () => {
	vi.stubGlobal('window', { __APP_BASE_PATH__: undefined })
	const pricing = [
	  { model: 'model-a', pricing_style: 'openai' as const, prompt_price_per_1m: 2, completion_price_per_1m: 0, cache_read_price_per_1m: 0, cache_write_price_per_1m: 0, price_multiplier: 1 },
	  { model: 'model-b', pricing_style: 'openai' as const, prompt_price_per_1m: 3, completion_price_per_1m: 0, cache_read_price_per_1m: 0, cache_write_price_per_1m: 0, price_multiplier: 1 },
	]
	const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
	  ok: true,
	  json: async () => ({ pricing }),
	} as Response)

	await updatePricingBatch(pricing)

	expect(fetchMock).toHaveBeenCalledTimes(1)
	const [rawURL, init] = fetchMock.mock.calls[0]
	expect(new URL(String(rawURL), 'http://localhost').pathname).toBe('/api/v1/pricing/batch')
	expect(init).toMatchObject({ credentials: 'include', method: 'PUT' })
	expect(JSON.parse(String(init?.body))).toEqual({ pricing })
  })
})

describe('pricing rules API', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('loads rules through a query parameter so model names may contain slashes', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined })
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ model: 'openai/gpt-5.6', rules: [] }),
    } as Response)
    const signal = new AbortController().signal

    await fetchPricingRules('openai/gpt-5.6', signal)

    const [rawURL, init] = fetchMock.mock.calls[0]
    const url = new URL(String(rawURL), 'http://localhost')
    expect(url.pathname).toBe('/api/v1/pricing/rules')
    expect(url.searchParams.get('model')).toBe('openai/gpt-5.6')
    expect(init).toMatchObject({ credentials: 'include', signal, cache: 'no-store' })
  })

  it('replaces the complete rule set and preserves an explicit empty array', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined })
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ model: 'model-a', rules: [] }),
    } as Response)

    await replacePricingRules({ model: 'model-a', rules: [] })

    const [rawURL, init] = fetchMock.mock.calls[0]
    expect(new URL(String(rawURL), 'http://localhost').pathname).toBe('/api/v1/pricing/rules')
    expect(init).toMatchObject({ credentials: 'include', method: 'PUT' })
    expect(headerValue(init, 'Content-Type')).toBe('application/json')
    expect(headerValue(init, 'X-CPA-Usage-Keeper-Request')).toBe('fetch')
    expect(init?.body).toBe(JSON.stringify({ model: 'model-a', rules: [] }))
  })
})
