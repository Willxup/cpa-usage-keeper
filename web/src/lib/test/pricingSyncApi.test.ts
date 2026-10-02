import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { applyPricingSync, fetchPricingSync } from '../api'
import type { ModelPricingConfig, PricingSyncApplyRequest, PricingSyncFetchResponse } from '../types'

const fetched: PricingSyncFetchResponse = {
  source: 'litellm',
  matches: [{
    model: 'openai/gpt-5',
    matched_model: 'gpt-5',
    provider: 'OpenAI',
    pricing_style: 'openai',
    base_prices: { input: 1.25, output: 10, cache_read: 0.125, cache_write: 0 },
  }],
  unmatched_models: ['custom/local'],
}

const existing: ModelPricingConfig = {
  model: 'openai/gpt-5', pricing_style: 'claude',
  base_prices: { input: 1.5, output: 11, cache_read: 0.15, cache_write: 0.25 },
  model_multiplier: 1.2,
  conditional_multipliers: [{ key: 'reasoning_effort', value: 'xhigh', multiplier: 1.5 }],
  branches: [{
    id: 'night', name: 'Night', context: { type: 'gt', threshold: 200_000 },
    period: { type: 'window', start: '22:00', end: '06:00' },
    prices: { input: 2, output: 12, cache_read: 0.2, cache_write: 0.3 },
  }],
}

describe('pricing sync API', () => {
  beforeEach(() => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper/' })
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('fetches only the explicitly selected source and forwards cancellation', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(Response.json(fetched))
    const signal = new AbortController().signal

    expect(await fetchPricingSync('litellm', signal)).toEqual(fetched)

    const [rawURL, init] = fetchMock.mock.calls[0]
    const url = new URL(String(rawURL), 'http://localhost')
    expect(url.pathname).toBe('/keeper/api/v1/pricing/sync/fetch')
    expect(url.searchParams.get('source')).toBe('litellm')
    expect(init).toMatchObject({ credentials: 'include', cache: 'no-store', signal })
  })

  it('applies selected base prices in one request and returns complete updated configs', async () => {
    const request: PricingSyncApplyRequest = {
      source: 'models-dev',
      items: [
        { model: 'openai/gpt-5', pricing_style: 'openai', base_prices: fetched.matches[0].base_prices },
        { model: 'custom/new', pricing_style: 'claude', base_prices: { input: 0, output: 3, cache_read: 0, cache_write: 0 } },
      ],
    }
    const updated: ModelPricingConfig = { ...existing, base_prices: request.items[0].base_prices }
    const created: ModelPricingConfig = {
      model: 'custom/new', pricing_style: 'claude', base_prices: request.items[1].base_prices,
      model_multiplier: 1, conditional_multipliers: [], branches: [],
    }
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(Response.json({
      models: [updated, created], config_revision: 12,
    }))
    const signal = new AbortController().signal

    expect(await applyPricingSync(request, signal)).toEqual({ models: [updated, created], config_revision: 12 })

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [rawURL, init] = fetchMock.mock.calls[0]
    expect(new URL(String(rawURL), 'http://localhost').pathname).toBe('/keeper/api/v1/pricing/sync/apply')
    expect(init).toMatchObject({ method: 'POST', credentials: 'include', signal })
    expect(new Headers(init?.headers).get('Content-Type')).toBe('application/json')
    expect(new Headers(init?.headers).get('X-CPA-Usage-Keeper-Request')).toBe('fetch')
    // 同步请求不得携带可能覆盖已保存倍率、分支和条件的完整配置。
    expect(JSON.parse(String(init?.body))).toEqual(request)
    expect(Object.keys(request.items[0]).sort()).toEqual(['base_prices', 'model', 'pricing_style'])
  })

  it('retains the selected source timeout code for later UI handling', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(Response.json({
      code: 'price_source_timeout', message: 'LiteLLM request timed out',
    }, { status: 504 }))

    await expect(fetchPricingSync('litellm')).rejects.toMatchObject({
      name: 'ApiError', status: 504, code: 'price_source_timeout', message: 'LiteLLM request timed out',
    })
  })

  it('keeps an item field path from a rejected atomic apply', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(Response.json({
      code: 'invalid_pricing', message: 'Invalid input price',
      fields: [{ path: 'items[1].base_prices.input', code: 'invalid' }],
    }, { status: 400 }))

    await expect(applyPricingSync({
      source: 'models-dev', items: [
        { model: 'good', pricing_style: 'openai', base_prices: { input: 1, output: 1, cache_read: 0, cache_write: 0 } },
        { model: 'bad', pricing_style: 'openai', base_prices: { input: -1, output: 1, cache_read: 0, cache_write: 0 } },
      ],
    })).rejects.toMatchObject({
      name: 'ApiError', status: 400, code: 'invalid_pricing',
      fields: [{ path: 'items[1].base_prices.input', code: 'invalid' }],
    })
  })

  it('propagates AbortError from both fetch and apply without treating it as a server response', async () => {
    const controller = new AbortController()
    controller.abort()
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (_url, init) => {
      expect(init?.signal).toBe(controller.signal)
      throw new DOMException('aborted', 'AbortError')
    })

    await expect(fetchPricingSync('models-dev', controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
    await expect(applyPricingSync({ source: 'litellm', items: [] }, controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})
