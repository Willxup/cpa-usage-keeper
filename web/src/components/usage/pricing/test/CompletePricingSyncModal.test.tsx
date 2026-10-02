import { selectOption } from './selectOption'
// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ModelPricingConfig, PricingSyncFetchResponse } from '@/lib/types'
import { CompletePricingSyncModal } from '../CompletePricingSyncModal'

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({ t: (key: string, params?: Record<string, unknown>) => (
    params ? `${key}:${JSON.stringify(params)}` : key
  ) }),
}))

const saved: ModelPricingConfig = {
  model: 'provider/gpt', pricing_style: 'openai',
  base_prices: { input: 2, output: 8, cache_read: 0.2, cache_write: 1 },
  model_multiplier: 1.7, conditional_multipliers: [{ key: 'endpoint', value: '/v1/responses', multiplier: 1.2 }],
  branches: [{ id: 'long', name: 'Long', context: { type: 'gt', threshold: 200_000 }, period: { type: 'all' },
    prices: { input: 1, output: 7, cache_read: 0.1, cache_write: 0.8 } }],
}

const preview = (source: 'models-dev' | 'litellm'): PricingSyncFetchResponse => ({
  source,
  matches: [
    { model: 'provider/gpt', matched_model: 'gpt', provider: 'OpenAI', pricing_style: 'claude',
      base_prices: { input: 2.5, output: 9, cache_read: 0, cache_write: 1.5 } },
    { model: 'new/model', matched_model: 'new/model', provider: 'Example', pricing_style: 'claude',
      base_prices: { input: 0, output: 3, cache_read: 0, cache_write: 0 } },
  ],
  unmatched_models: ['local-only'],
})

const deferred = <T,>() => {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

const button = (label: string) => {
  const found = [...document.querySelectorAll<HTMLButtonElement>('button')]
    .find((candidate) => candidate.textContent?.trim().startsWith(label))
  expect(found, label).toBeDefined()
  return found!
}

const setInput = async (input: HTMLInputElement, value: string) => {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}

describe('CompletePricingSyncModal', () => {
  let root: Root
  let container: HTMLDivElement
  let onClose: ReturnType<typeof vi.fn>
  let onRefreshPricing: ReturnType<typeof vi.fn>
  let onNotice: ReturnType<typeof vi.fn>

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    localStorage.clear()
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
    onClose = vi.fn()
    onRefreshPricing = vi.fn(async () => true)
    onNotice = vi.fn()
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    vi.restoreAllMocks()
  })

  const renderModal = async (open = true) => {
    await act(async () => root.render(<CompletePricingSyncModal open={open} models={[saved]}
      onClose={onClose} onRefreshPricing={onRefreshPricing} onNotice={onNotice} />))
  }

  it('fetches only on demand and applies only selected base prices with the explicit source', async () => {
    const requests: Array<{ path: string; method: string; body?: unknown }> = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = new URL(String(input), 'http://localhost')
      requests.push({ path: `${url.pathname}${url.search}`, method: init?.method ?? 'GET',
        body: init?.body ? JSON.parse(String(init.body)) : undefined })
      if (url.pathname.endsWith('/sync/fetch')) return Response.json(preview(url.searchParams.get('source') as 'models-dev' | 'litellm'))
      if (url.pathname.endsWith('/sync/apply')) return Response.json({ models: [saved], config_revision: 8 })
      throw new Error(`unexpected ${url}`)
    })
    await renderModal()
    expect(requests).toHaveLength(0)
    const source = document.querySelector<HTMLButtonElement>('[data-sync-source]')!
    await selectOption(source, 'LiteLLM')
    expect(requests).toHaveLength(0)
    expect(localStorage.getItem('cpa-pricing-sync-source-v1')).toBe('litellm')
    await act(async () => button('usage_stats.pricing_settings_sync_fetch').click())
    expect(requests).toHaveLength(1)
    expect(requests[0].path).toContain('source=litellm')
    expect(document.querySelector('[data-sync-model="provider/gpt"]')?.textContent).toContain('OpenAI')
    expect(document.querySelector('[data-sync-model="provider/gpt"]')?.textContent).toContain('2')
    expect(document.querySelector<HTMLInputElement>('[data-sync-model="provider/gpt"] [data-sync-price="input"]')?.value).toBe('2.5')
    expect(document.body.textContent).toContain('local-only')
    await act(async () => button('usage_stats.pricing_settings_sync_clear_selection').click())
    expect(button('usage_stats.pricing_settings_sync_apply').disabled).toBe(true)
    await act(async () => button('usage_stats.pricing_settings_sync_select_all').click())
    await act(async () => document.querySelector<HTMLInputElement>('[data-sync-model="new/model"] input[type="checkbox"]')!.click())
    await act(async () => button('usage_stats.pricing_settings_sync_apply').click())
    expect(requests).toHaveLength(2)
    expect(requests[1]).toEqual({ path: '/api/v1/pricing/sync/apply', method: 'POST', body: {
      source: 'litellm', items: [{ model: 'provider/gpt', pricing_style: 'claude',
        base_prices: { input: 2.5, output: 9, cache_read: 0, cache_write: 1.5 } }],
    } })
    expect(onRefreshPricing).toHaveBeenCalledTimes(1)
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('aborts source and close requests, ignoring responses that arrive late anyway', async () => {
    const first = deferred<Response>()
    const second = deferred<Response>()
    const signals: AbortSignal[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation((input, init) => {
      const source = new URL(String(input), 'http://localhost').searchParams.get('source')
      signals.push(init!.signal!)
      return source === 'models-dev' ? first.promise : second.promise
    })
    await renderModal()
    await act(async () => button('usage_stats.pricing_settings_sync_fetch').click())
    const source = document.querySelector<HTMLButtonElement>('[data-sync-source]')!
    await selectOption(source, 'LiteLLM')
    expect(signals[0].aborted).toBe(true)
    await act(async () => first.resolve(Response.json(preview('models-dev'))))
    expect(document.querySelector('[data-sync-model="provider/gpt"]')).toBeNull()
    await act(async () => button('usage_stats.pricing_settings_sync_fetch').click())
    await renderModal(false)
    expect(signals[1].aborted).toBe(true)
    await act(async () => second.resolve(Response.json(preview('litellm'))))
    expect(document.querySelector('[data-sync-model="provider/gpt"]')).toBeNull()
  })

  it('keeps the chosen source after a fetch timeout and allows an explicit retry', async () => {
    let attempts = 0
    vi.spyOn(globalThis, 'fetch').mockImplementation(async () => {
      attempts += 1
      return attempts === 1
        ? Response.json({ code: 'price_source_timeout', message: 'Source timed out' }, { status: 504 })
        : Response.json(preview('models-dev'))
    })
    await renderModal()
    await act(async () => button('usage_stats.pricing_settings_sync_fetch').click())
    expect(document.body.textContent).toContain('Source timed out')
    expect(document.querySelector<HTMLButtonElement>('[data-sync-source]')?.textContent).toBe('Models.dev')
    expect(document.querySelector('[data-sync-model]')).toBeNull()
    await act(async () => button('usage_stats.pricing_settings_sync_fetch').click())
    expect(attempts).toBe(2)
    expect(document.querySelectorAll('[data-sync-model]')).toHaveLength(2)
  })

  it('keeps the whole selected draft after a field validation failure', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const path = new URL(String(input), 'http://localhost').pathname
      if (path.endsWith('/sync/fetch')) return Response.json(preview('models-dev'))
      return Response.json({ code: 'invalid_pricing', message: 'Invalid price',
        fields: [{ path: 'items[0].base_prices.input', code: 'invalid' }] }, { status: 400 })
    })
    await renderModal()
    await act(async () => button('usage_stats.pricing_settings_sync_fetch').click())
    await act(async () => button('usage_stats.pricing_settings_sync_apply').click())
    const input = document.querySelector<HTMLInputElement>('[data-sync-model="provider/gpt"] [data-sync-price="input"]')!
    expect(input.getAttribute('aria-invalid')).toBe('true')
    expect(document.activeElement).toBe(input)
    expect(document.querySelectorAll('[data-sync-model]')).toHaveLength(2)
    expect(onClose).not.toHaveBeenCalled()
    expect(onRefreshPricing).not.toHaveBeenCalled()
  })

  it('reports a concurrent pricing_busy response so the list can follow the running task', async () => {
    const onPricingBusy = vi.fn()
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const path = new URL(String(input), 'http://localhost').pathname
      if (path.endsWith('/sync/fetch')) return Response.json(preview('models-dev'))
      return Response.json({ code: 'pricing_busy', message: 'Recalculation running' }, { status: 409 })
    })
    await act(async () => root.render(<CompletePricingSyncModal open models={[saved]} onClose={onClose}
      onRefreshPricing={onRefreshPricing} onNotice={onNotice} onPricingBusy={onPricingBusy} />))
    await act(async () => button('usage_stats.pricing_settings_sync_fetch').click())
    await act(async () => button('usage_stats.pricing_settings_sync_apply').click())
    expect(onPricingBusy).toHaveBeenCalledTimes(1)
    expect(onClose).not.toHaveBeenCalled()
    expect(document.querySelectorAll('[data-sync-model]')).toHaveLength(2)
  })

  it('maps selected-item errors to the right row and accepts a corrected explicit zero', async () => {
    const posts: unknown[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const path = new URL(String(input), 'http://localhost').pathname
      if (path.endsWith('/sync/fetch')) return Response.json(preview('models-dev'))
      posts.push(JSON.parse(String(init?.body)))
      if (posts.length === 1) return Response.json({ code: 'invalid_pricing', message: 'Invalid price',
        fields: [{ path: 'items[0].base_prices.input', code: 'invalid' }] }, { status: 400 })
      return Response.json({ models: [], config_revision: 8 })
    })
    await renderModal()
    await act(async () => button('usage_stats.pricing_settings_sync_fetch').click())
    await act(async () => document.querySelector<HTMLInputElement>('[data-sync-model="provider/gpt"] input[type="checkbox"]')!.click())
    const input = document.querySelector<HTMLInputElement>('[data-sync-model="new/model"] [data-sync-price="input"]')!
    await setInput(input, '')
    await act(async () => button('usage_stats.pricing_settings_sync_apply').click())
    expect(posts).toHaveLength(0)
    expect(input.getAttribute('aria-invalid')).toBe('true')
    await setInput(input, '0')
    await act(async () => button('usage_stats.pricing_settings_sync_apply').click())
    expect(posts).toHaveLength(1)
    expect(posts[0]).toMatchObject({ items: [{ model: 'new/model', base_prices: { input: 0 } }] })
    expect(input.getAttribute('aria-invalid')).toBe('true')
    expect(document.activeElement).toBe(input)
    expect(document.querySelector<HTMLInputElement>('[data-sync-model="provider/gpt"] [data-sync-price="input"]')?.getAttribute('aria-invalid')).toBe('false')
    await setInput(input, '0')
    await act(async () => button('usage_stats.pricing_settings_sync_apply').click())
    expect(posts).toHaveLength(2)
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('does not close or submit twice while the whole batch is pending', async () => {
    const pending = deferred<Response>()
    let posts = 0
    vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
      const path = new URL(String(input), 'http://localhost').pathname
      if (path.endsWith('/sync/fetch')) return Promise.resolve(Response.json(preview('models-dev')))
      posts += 1
      return pending.promise
    })
    await renderModal()
    await act(async () => button('usage_stats.pricing_settings_sync_fetch').click())
    await act(async () => button('usage_stats.pricing_settings_sync_apply').click())
    expect(button('common.cancel').disabled).toBe(true)
    expect(document.querySelector<HTMLButtonElement>('[aria-label="common.close"]')?.disabled).toBe(true)
    await act(async () => button('usage_stats.pricing_settings_sync_apply').click())
    expect(posts).toBe(1)
    expect(onClose).not.toHaveBeenCalled()
    await act(async () => pending.resolve(Response.json({ models: [], config_revision: 8 })))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('reconciles a lost apply response with GET without retrying POST or claiming a commit receipt', async () => {
    const methods: string[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const path = new URL(String(input), 'http://localhost').pathname
      methods.push(`${init?.method ?? 'GET'} ${path}`)
      if (path.endsWith('/sync/fetch')) return Response.json(preview('models-dev'))
      if (path.endsWith('/sync/apply')) throw new TypeError('connection lost')
      if (path.endsWith('/pricing/models')) return Response.json({ models: [{ ...saved,
        base_prices: { input: 2.5, output: 9, cache_read: 0, cache_write: 1.5 } },
        { model: 'new/model', pricing_style: 'claude', base_prices: { input: 0, output: 3, cache_read: 0, cache_write: 0 },
          model_multiplier: 1, conditional_multipliers: [], branches: [] }], config_revision: 9 })
      throw new Error(`unexpected ${path}`)
    })
    await renderModal()
    await act(async () => button('usage_stats.pricing_settings_sync_fetch').click())
    await act(async () => button('usage_stats.pricing_settings_sync_apply').click())
    expect(methods.filter((method) => method === 'POST /api/v1/pricing/sync/apply')).toHaveLength(1)
    expect(methods).toContain('GET /api/v1/pricing/models')
    expect(document.body.textContent).toContain('usage_stats.pricing_settings_sync_current_matches')
    expect(onClose).not.toHaveBeenCalled()
  })

  it.each(['different', 'failed'])('keeps the draft when readback is %s after an unknown result', async (readback) => {
    const methods: string[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const path = new URL(String(input), 'http://localhost').pathname
      methods.push(`${init?.method ?? 'GET'} ${path}`)
      if (path.endsWith('/sync/fetch')) return Response.json(preview('models-dev'))
      if (path.endsWith('/sync/apply')) throw new TypeError('connection lost')
      if (readback === 'failed') return Response.json({ code: 'internal_error', message: 'read failed' }, { status: 500 })
      return Response.json({ models: [saved], config_revision: 7 })
    })
    await renderModal()
    await act(async () => button('usage_stats.pricing_settings_sync_fetch').click())
    await act(async () => button('usage_stats.pricing_settings_sync_apply').click())
    expect(methods.filter((method) => method === 'POST /api/v1/pricing/sync/apply')).toHaveLength(1)
    expect(document.body.textContent).toContain('usage_stats.pricing_settings_sync_result_unknown')
    expect(document.querySelectorAll('[data-sync-model]')).toHaveLength(2)
    expect(onClose).not.toHaveBeenCalled()
  })
})
