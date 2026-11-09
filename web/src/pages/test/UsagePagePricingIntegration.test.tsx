// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { triggerHeaderRefresh } from '@/hooks/useHeaderRefresh'
import i18n from '@/i18n'
import { ApiError } from '@/lib/api'
import { requestEventsLiveSnapshot } from './requestEventsLiveFixture'
import type { ModelPricingConfig } from '@/lib/types'

const api = vi.hoisted(() => ({
  fetchAvailableModels: vi.fn(),
  fetchPricingModels: vi.fn(),
  fetchPricingModelOptions: vi.fn(),
  savePricingModel: vi.fn(),
  fetchPricingSync: vi.fn(),
  applyPricingSync: vi.fn(),
  fetchCurrentPricingRecalculation: vi.fn(),
  openUsageEventsLive: vi.fn(),
  fetchAnalysis: vi.fn(),
  fetchAnalysisLatency: vi.fn(),
}))

vi.mock('react-chartjs-2', () => ({ Bar: () => null, Chart: () => null, Doughnut: () => null, Line: () => null, Scatter: () => null }))
vi.mock('@/components/usage/analysis', async (original) => ({
  ...await original<typeof import('@/components/usage/analysis')>(),
  AnalysisPanel: ({ analysis, latencyDiagnostics }: { analysis: { cost_summary?: { total_cost_usd?: number } } | null; latencyDiagnostics: { total_points?: number } | null }) =>
    <div data-analysis-cost>{analysis?.cost_summary?.total_cost_usd ?? 'missing'}:{latencyDiagnostics?.total_points ?? 'missing'}</div>,
}))
vi.mock('@/lib/api', async (original) => ({
  ...await original<typeof import('@/lib/api')>(),
  ...api,
  fetchStatus: async () => ({ timezone: 'America/New_York' }),
  fetchVersion: async () => ({ version: 'test' }),
  fetchCpaApiKeyOptions: async () => ({ options: [] }),
  fetchUsageEventModelFilterOptions: async () => ({ models: [] }),
  fetchUsageEventSourceFilterOptions: async () => ({ sources: [] }),
  fetchCpaApiKeySettings: async () => ({ items: [] }),
  fetchAuthSessions: async () => ({ sessions: [] }),
  fetchUsageIdentitiesPage: async () => ({ identities: [], total_count: 0, total_pages: 0, type_counts: [] }),
  fetchUsageQuotaCache: async () => ({ results: [] }),
  fetchUsageQuotaInspectionStatus: async () => ({}),
  fetchQuotaAutoRefreshSettings: async () => ({}),
  fetchUsageQuotaResetCredits: async () => ({}),
}))

import { UsagePage } from '../UsagePage'

const pricingConfig: ModelPricingConfig = {
  model: 'model-a', pricing_style: 'openai',
  base_prices: { input: 1, output: 2, cache_read: 0, cache_write: 0 },
  model_multiplier: 1, conditional_multipliers: [],
  branches: [{ id: 'night', name: 'Night', context: { type: 'all' },
    period: { type: 'window', start: '22:00', end: '06:00' },
    prices: { input: 0.5, output: 1, cache_read: 0, cache_write: 0 } }],
}

describe('UsagePage complete pricing integration', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    await i18n.changeLanguage('en')
    localStorage.clear()
    window.history.replaceState(null, '', '/models')
    api.fetchAvailableModels.mockReset().mockResolvedValue({ object: 'list', data: [{ id: 'model-a', owned_by: 'openai' }] })
    api.fetchPricingModels.mockReset().mockResolvedValue({ models: [pricingConfig], config_revision: 3 })
    api.fetchPricingModelOptions.mockReset().mockResolvedValue({ models: ['model-a'] })
    api.savePricingModel.mockReset().mockResolvedValue({ model: 'model-a', config_revision: 4 })
    api.fetchPricingSync.mockReset()
    api.applyPricingSync.mockReset()
    api.fetchCurrentPricingRecalculation.mockReset().mockResolvedValue(null)
    const page = { events: [{
      id: '401', timestamp: '2026-09-23T12:00:00Z', model: 'historic-fee-model',
      source: 'source-a', failed: false, latency_ms: 30, cost_usd: 3.14, cost_available: true,
      tokens: { input_tokens: 1, output_tokens: 1, reasoning_tokens: 0,
        cache_read_tokens: 0, cache_creation_tokens: 0, total_tokens: 2 },
    }], total_count: 1, page: 1, page_size: 50, total_pages: 1, has_more: false, next_cursor: undefined }
    api.openUsageEventsLive.mockReset().mockImplementation(async (_request, _options, signal) => requestEventsLiveSnapshot(page, signal))
    api.fetchAnalysis.mockReset().mockResolvedValue({
      granularity: 'hourly', timezone: 'America/New_York', token_usage: [],
      api_key_composition: [], model_composition: [], credentials_composition: [], ai_provider_composition: [],
      cost_summary: { total_cost_usd: 4.25, cost_available: true }, model_efficiency: [],
      heatmap: { api_keys: [], api_key_labels: {}, models: [], cells: [] },
    })
    api.fetchAnalysisLatency.mockReset().mockResolvedValue({ total_points: 3 })
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    vi.restoreAllMocks()
  })

  it('mounts the catalog, complete pricing models and deployment timezone on the models page', async () => {
    await act(async () => root.render(<UsagePage />))
    expect(api.fetchPricingModels).toHaveBeenCalledTimes(1)
    expect(api.fetchPricingModelOptions).toHaveBeenCalledTimes(1)
    const edit = [...container.querySelectorAll<HTMLButtonElement>('button')].find((button) => button.textContent === 'Edit')
    expect(edit).toBeDefined()
    await act(async () => edit!.click())
    expect(document.body.textContent).toContain('model-a')
    expect(document.body.textContent).toContain('America/New_York')
  })

  it.each(['credentials', 'ai-provider'])('retains the loaded catalog and prices after visiting %s', async (tab) => {
    await act(async () => root.render(<UsagePage />))
    const catalog = () => container.querySelector<HTMLElement>('section[aria-label="Models"]')
    const pricing = () => container.querySelector<HTMLElement>('section[aria-label="Model Pricing Settings"]')
    expect(catalog()?.textContent).toContain('model-a')
    expect(pricing()?.textContent).toContain('$1')

    await act(async () => container.querySelector<HTMLAnchorElement>(`a[href^="/${tab}"]`)!.click())
    expect(catalog()).toBeNull()
    expect(pricing()).toBeNull()
    // 若切回时重读，保持请求进行中，让首次加载闪烁成为可观察的回归。
    api.fetchAvailableModels.mockReturnValueOnce(new Promise(() => {}))
    api.fetchPricingModels.mockReturnValueOnce(new Promise(() => {}))
    await act(async () => container.querySelector<HTMLAnchorElement>('a[href^="/models"]')!.click())

    expect.soft(catalog()?.textContent).toContain('model-a')
    expect.soft(pricing()?.textContent).toContain('model-a')
    expect.soft(pricing()?.textContent).toContain('$1')
    expect(catalog()?.querySelector('[aria-busy="false"]')).not.toBeNull()
    expect(pricing()?.textContent).not.toContain(i18n.t('common.loading'))
    expect(api.fetchAvailableModels).toHaveBeenCalledTimes(1)
    expect(api.fetchPricingModels).toHaveBeenCalledTimes(1)
    expect(api.fetchPricingModelOptions).toHaveBeenCalledTimes(1)
  })

  it('retains price edits made after returning to Models', async () => {
    await act(async () => root.render(<UsagePage />))
    await act(async () => container.querySelector<HTMLAnchorElement>('a[href^="/credentials"]')!.click())
    await act(async () => container.querySelector<HTMLAnchorElement>('a[href^="/models"]')!.click())
    const edit = [...container.querySelectorAll<HTMLButtonElement>('button')].find((button) => button.textContent === 'Edit')!
    await act(async () => edit.click())
    const input = document.querySelector<HTMLInputElement>('[data-pricing-field="base_prices.input"]')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, '4.5')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    api.fetchPricingModels.mockResolvedValueOnce({
      models: [{ ...pricingConfig, base_prices: { ...pricingConfig.base_prices, input: 4.5 } }], config_revision: 4,
    })
    const save = [...document.body.querySelectorAll<HTMLButtonElement>('button')].find((button) => button.textContent === 'Save')!
    await act(async () => save.click())
    expect(api.savePricingModel).toHaveBeenCalledOnce()
    expect(container.querySelector('section[aria-label="Model Pricing Settings"]')?.textContent).toContain('$4.5')
    await act(async () => container.querySelector<HTMLAnchorElement>('a[href^="/ai-provider"]')!.click())
    await act(async () => container.querySelector<HTMLAnchorElement>('a[href^="/models"]')!.click())
    expect(container.querySelector('section[aria-label="Model Pricing Settings"]')?.textContent).toContain('$4.5')
    expect(api.fetchPricingModels).toHaveBeenCalledTimes(2)
  })

  it('recovers synced prices on returning to Models when the post-sync read failed', async () => {
    const updated = { ...pricingConfig, base_prices: { ...pricingConfig.base_prices, input: 4.5 } }
    api.fetchPricingSync.mockResolvedValue({
      source: 'models-dev', unmatched_models: [],
      matches: [{ model: updated.model, matched_model: updated.model, provider: 'OpenAI',
        pricing_style: updated.pricing_style, base_prices: updated.base_prices }],
    })
    api.applyPricingSync.mockResolvedValue({ models: [updated], config_revision: 4 })
    await act(async () => root.render(<UsagePage />))
    const clickButton = async (key: string) => {
      const button = [...document.body.querySelectorAll<HTMLButtonElement>('button')]
        .find((candidate) => candidate.textContent?.trim().startsWith(i18n.t(key)))!
      await act(async () => button.click())
    }
    await clickButton('usage_stats.pricing_settings_sync_title')
    await clickButton('usage_stats.pricing_settings_sync_fetch')
    api.fetchPricingModels.mockRejectedValueOnce(new Error('list unavailable'))
    await clickButton('usage_stats.pricing_settings_sync_apply')
    expect(api.applyPricingSync).toHaveBeenCalledOnce()
    const pricing = () => container.querySelector('section[aria-label="Model Pricing Settings"]')
    expect(pricing()?.textContent).toContain('$1')
    expect(pricing()?.textContent).toContain('list unavailable')

    api.fetchPricingModels.mockResolvedValueOnce({ models: [updated], config_revision: 4 })
    await act(async () => container.querySelector<HTMLAnchorElement>('a[href^="/credentials"]')!.click())
    await act(async () => container.querySelector<HTMLAnchorElement>('a[href^="/models"]')!.click())
    expect(pricing()?.textContent).toContain('$4.5')
    expect(pricing()?.textContent).not.toContain('list unavailable')
    expect(api.fetchPricingModels).toHaveBeenCalledTimes(3)
    expect(api.fetchAvailableModels).toHaveBeenCalledTimes(1)
  })

  it('rereads complete model pricing through the existing page refresh action after returning to Models', async () => {
    await act(async () => root.render(<UsagePage />))
    await act(async () => container.querySelector<HTMLAnchorElement>('a[href^="/credentials"]')!.click())
    await act(async () => container.querySelector<HTMLAnchorElement>('a[href^="/models"]')!.click())
    expect(api.fetchPricingModels).toHaveBeenCalledTimes(1)
    expect(api.fetchCurrentPricingRecalculation).toHaveBeenCalledTimes(2)
    api.fetchAvailableModels.mockResolvedValueOnce({ object: 'list', data: [{ id: 'model-b', owned_by: 'openai' }] })
    api.fetchPricingModels.mockResolvedValueOnce({
      models: [{ ...pricingConfig, base_prices: { ...pricingConfig.base_prices, input: 4.5 } }], config_revision: 4,
    })
    await act(async () => triggerHeaderRefresh())
    expect(api.fetchPricingModels).toHaveBeenCalledTimes(2)
    expect(api.fetchCurrentPricingRecalculation).toHaveBeenCalledTimes(3)
    expect(api.fetchAvailableModels).toHaveBeenCalledTimes(2)
    expect(container.querySelector('section[aria-label="Models"]')?.textContent).toContain('model-b')
    expect(container.querySelector('section[aria-label="Model Pricing Settings"]')?.textContent).toContain('$4.5')
  })

  it('keeps pricing out of Settings and loads Models on the first visit', async () => {
    const catalogRequest = Promise.withResolvers<{ data: { id: string; owned_by: string }[] }>()
    const pricingRequest = Promise.withResolvers<{ models: typeof pricingConfig[]; config_revision: number }>()
    api.fetchAvailableModels.mockReturnValueOnce(catalogRequest.promise)
    api.fetchPricingModels.mockReturnValueOnce(pricingRequest.promise)
    window.history.replaceState(null, '', '/settings')
    await act(async () => root.render(<UsagePage />))
    expect(api.fetchPricingModels).not.toHaveBeenCalled()
    expect(api.fetchAvailableModels).not.toHaveBeenCalled()
    const links = [...container.querySelectorAll('a')]
    expect(links.findIndex((link) => link.textContent === 'Models')).toBe(links.findIndex((link) => link.textContent === 'Settings') - 1)
    await act(async () => container.querySelector<HTMLAnchorElement>('a[href^="/models"]')!.click())
    expect(container.querySelector('section[aria-label="Models"] [aria-busy="true"]')).not.toBeNull()
    expect(container.querySelector('section[aria-label="Model Pricing Settings"]')?.textContent).toContain(i18n.t('common.loading'))
    await act(async () => {
      catalogRequest.resolve({ data: [{ id: 'model-a', owned_by: 'openai' }] })
      pricingRequest.resolve({ models: [pricingConfig], config_revision: 3 })
    })
    expect(container.querySelector('section[aria-label="Models"]')?.textContent).toContain('model-a')
    expect(container.querySelector('section[aria-label="Model Pricing Settings"]')?.textContent).toContain('$1')
  })

  it('keeps the displayed request cost on same-scope costs_busy and refreshes through the real page action', async () => {
    window.history.replaceState(null, '', '/request-events')
    await act(async () => root.render(<UsagePage />))
    expect(container.textContent).toContain('historic-fee-model')
    api.openUsageEventsLive.mockRejectedValueOnce(new ApiError('busy', 503, 'costs_busy'))
    let refresh!: Promise<void>
    // 先提交刷新状态，让连接effect启动；再等待原刷新Promise完成。
    await act(async () => { refresh = triggerHeaderRefresh() })
    await act(async () => { await refresh })
    expect(api.openUsageEventsLive).toHaveBeenCalledTimes(2)
    expect(container.textContent).toContain('historic-fee-model')
    expect(container.textContent).toContain('Cost statistics are being updated')
  })

  it('keeps same-range analysis cost through costs_busy while latency refreshes independently', async () => {
    window.history.replaceState(null, '', '/analysis')
    await act(async () => root.render(<UsagePage />))
    expect(container.querySelector('[data-analysis-cost]')?.textContent).toBe('4.25:3')
    api.fetchAnalysis.mockRejectedValueOnce(new ApiError('busy', 503, 'costs_busy'))
    api.fetchAnalysisLatency.mockResolvedValueOnce({ total_points: 4 })
    await act(async () => triggerHeaderRefresh())
    expect(container.querySelector('[data-analysis-cost]')?.textContent).toBe('4.25:4')
    expect(container.textContent).toContain('Cost statistics are being updated')
  })
})
