import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  ApiError,
  deletePricingModel,
  fetchPricingModelOptions,
  fetchPricingModels,
  savePricingModel,
} from '../api'
import type { ModelPricingConfig } from '../types'

const model = 'provider/claude sonnet?preview'
const savedConfig: ModelPricingConfig = {
  model,
  pricing_style: 'claude',
  base_prices: { input: 3, output: 15, cache_read: 0.3, cache_write: 3.75 },
  model_multiplier: 1.2,
  conditional_multipliers: [
    { key: 'response_service_tier', value: 'Priority', multiplier: 1.4 },
    { key: 'reasoning_effort', value: 'xhigh', multiplier: 1.1 },
  ],
  branches: [{
    id: 'branch-1', name: 'Large night request',
    context: { type: 'range', min: 200_001, max: 300_000 },
    period: { type: 'window', start: '22:00', end: '06:00' },
    prices: { input: 2.5, output: 12, cache_read: 0.25, cache_write: 3 },
  }],
}

describe('complete pricing model API', () => {
  beforeEach(() => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper/' })
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('round-trips a complete config and encodes a slash-containing model only in the delete query', async () => {
    let stored: ModelPricingConfig[] = [structuredClone(savedConfig)]
    const updatedConfig = structuredClone(savedConfig)
    updatedConfig.base_prices.input = 4.25
    updatedConfig.branches[0].prices.input = 1.75
    let revision = 7
    const requests: Array<{ url: URL; init?: RequestInit }> = []
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = new URL(String(input), 'http://localhost')
      requests.push({ url, init })
      if (url.pathname === '/keeper/api/v1/pricing/model-options') {
        return Response.json({ models: [model, 'other-model'] })
      }
      expect(url.pathname).toBe('/keeper/api/v1/pricing/models')
      switch (init?.method ?? 'GET') {
        case 'PUT':
          stored = [JSON.parse(String(init?.body)) as ModelPricingConfig]
          revision = 8
          return Response.json({ model, config_revision: revision })
        case 'DELETE':
          expect(url.searchParams.get('model')).toBe(model)
          stored = []
          revision = 9
          return Response.json({ config_revision: revision })
        default:
          return Response.json({ models: stored, config_revision: revision })
      }
    })
    const signal = new AbortController().signal

    expect(await fetchPricingModels(signal)).toEqual({ models: [savedConfig], config_revision: 7 })
    expect(await fetchPricingModelOptions(signal)).toEqual({ models: [model, 'other-model'] })
    expect(await savePricingModel(updatedConfig, signal)).toEqual({ model, config_revision: 8 })
    expect(await fetchPricingModels(signal)).toEqual({ models: [updatedConfig], config_revision: 8 })
    expect(await deletePricingModel(model, signal)).toEqual({ config_revision: 9 })
    expect(await fetchPricingModels(signal)).toEqual({ models: [], config_revision: 9 })

    expect(fetchMock).toHaveBeenCalledTimes(6)
    expect(requests.map(({ url }) => url.pathname)).toEqual([
      '/keeper/api/v1/pricing/models', '/keeper/api/v1/pricing/model-options',
      '/keeper/api/v1/pricing/models', '/keeper/api/v1/pricing/models',
      '/keeper/api/v1/pricing/models', '/keeper/api/v1/pricing/models',
    ])
    expect(requests[2].init).toMatchObject({ method: 'PUT', credentials: 'include', signal })
    expect(new Headers(requests[2].init?.headers).get('Content-Type')).toBe('application/json')
    expect(new Headers(requests[2].init?.headers).get('X-CPA-Usage-Keeper-Request')).toBe('fetch')
    expect(requests[2].init?.body).toBe(JSON.stringify(updatedConfig))
    expect(requests[4].init).toMatchObject({ method: 'DELETE', credentials: 'include', signal })
    expect(requests[4].url.searchParams.get('model')).toBe(model)
    expect(requests[4].url.search).toContain('%2F')
    for (const index of [0, 1, 3, 5]) {
      expect(requests[index].init).toMatchObject({ credentials: 'include', signal, cache: 'no-store' })
    }
  })

  it('preserves structured field errors and conflict branch IDs in ApiError', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(Response.json({
      code: 'branch_conflict',
      message: 'Branches overlap',
      fields: [{ path: 'branches[1].context', code: 'conflict', branch_ids: ['branch-1', 'branch-2'] }],
    }, { status: 400 }))

    await expect(savePricingModel(savedConfig)).rejects.toMatchObject({
      name: 'ApiError', status: 400, code: 'branch_conflict', message: 'Branches overlap',
      fields: [{ path: 'branches[1].context', code: 'conflict', branch_ids: ['branch-1', 'branch-2'] }],
    } satisfies Partial<ApiError>)
  })

  it('keeps authorization errors distinguishable from pricing validation errors', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(Response.json({ code: 'unauthorized', message: 'Sign in' }, { status: 401 }))

    await expect(fetchPricingModels()).rejects.toMatchObject({ status: 401, code: 'unauthorized', message: 'Sign in' })
  })

  it('passes the caller abort signal through every pricing model request', async () => {
    const controller = new AbortController()
    controller.abort()
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (_input, init) => {
      expect(init?.signal).toBe(controller.signal)
      throw new DOMException('aborted', 'AbortError')
    })

    for (const request of [
      () => fetchPricingModels(controller.signal),
      () => fetchPricingModelOptions(controller.signal),
      () => savePricingModel(savedConfig, controller.signal),
      () => deletePricingModel(model, controller.signal),
    ]) {
      await expect(request()).rejects.toMatchObject({ name: 'AbortError' })
    }
    expect(fetchMock).toHaveBeenCalledTimes(4)
  })
})
