// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { triggerHeaderRefresh } from '@/hooks/useHeaderRefresh'
import i18n from '@/i18n'
import { ApiError } from '@/lib/api'

const api = vi.hoisted(() => ({
  fetchPricingModels: vi.fn(),
  fetchPricingModelOptions: vi.fn(),
  fetchCurrentPricingRecalculation: vi.fn(),
  fetchUsageEvents: vi.fn(),
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
}))

import { UsagePage } from '../UsagePage'

describe('UsagePage complete pricing integration', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    await i18n.changeLanguage('en')
    localStorage.clear()
    window.history.replaceState(null, '', '/settings')
    api.fetchPricingModels.mockReset().mockResolvedValue({ models: [{
      model: 'model-a', pricing_style: 'openai',
      base_prices: { input: 1, output: 2, cache_read: 0, cache_write: 0 },
      model_multiplier: 1, conditional_multipliers: [],
      branches: [{ id: 'night', name: 'Night', context: { type: 'all' },
        period: { type: 'window', start: '22:00', end: '06:00' },
        prices: { input: 0.5, output: 1, cache_read: 0, cache_write: 0 } }],
    }], config_revision: 3 })
    api.fetchPricingModelOptions.mockReset().mockResolvedValue({ models: ['model-a'] })
    api.fetchCurrentPricingRecalculation.mockReset().mockResolvedValue(null)
    api.fetchUsageEvents.mockReset().mockResolvedValue({ events: [{
      id: '401', timestamp: '2026-09-23T12:00:00Z', model: 'historic-fee-model',
      source: 'source-a', failed: false, latency_ms: 30, cost_usd: 3.14, cost_available: true,
      tokens: { input_tokens: 1, output_tokens: 1, reasoning_tokens: 0,
        cache_read_tokens: 0, cache_creation_tokens: 0, total_tokens: 2 },
    }], total_count: 1, page: 1, page_size: 50, total_pages: 1, has_more: false, next_cursor: null })
    api.fetchAnalysis.mockReset().mockResolvedValue({
      granularity: 'hourly', timezone: 'America/New_York', token_usage: [],
      api_key_composition: [], model_composition: [], auth_files_composition: [], ai_provider_composition: [],
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

  it('mounts complete pricing models and the deployment timezone on the real settings page', async () => {
    await act(async () => root.render(<UsagePage />))
    expect(api.fetchPricingModels).toHaveBeenCalledTimes(1)
    expect(api.fetchPricingModelOptions).toHaveBeenCalledTimes(1)
    const edit = [...container.querySelectorAll<HTMLButtonElement>('button')].find((button) => button.textContent === 'Edit')
    expect(edit).toBeDefined()
    await act(async () => edit!.click())
    expect(document.body.textContent).toContain('model-a')
    expect(document.body.textContent).toContain('America/New_York')
  })

  it('rereads complete model pricing through the existing page refresh action', async () => {
    await act(async () => root.render(<UsagePage />))
    expect(api.fetchPricingModels).toHaveBeenCalledTimes(1)
    expect(api.fetchCurrentPricingRecalculation).toHaveBeenCalledTimes(1)
    await act(async () => triggerHeaderRefresh())
    expect(api.fetchPricingModels).toHaveBeenCalledTimes(2)
    expect(api.fetchCurrentPricingRecalculation).toHaveBeenCalledTimes(2)
  })

  it('keeps the displayed request cost on same-scope costs_busy and refreshes through the real page action', async () => {
    window.history.replaceState(null, '', '/request-events')
    await act(async () => root.render(<UsagePage />))
    expect(container.textContent).toContain('historic-fee-model')
    api.fetchUsageEvents.mockRejectedValueOnce(new ApiError('busy', 503, 'costs_busy'))
    await act(async () => triggerHeaderRefresh())
    expect(api.fetchUsageEvents).toHaveBeenCalledTimes(2)
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
