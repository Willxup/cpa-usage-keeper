// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ModelPricingConfig, PricingRecalculationTask, StartPricingRecalculationResponse } from '@/lib/types'
import { PricingRecalculationModal } from '../PricingRecalculationModal'

vi.mock('react-i18next', () => ({
  Trans: ({ i18nKey, values }: { i18nKey: string; values: Record<string, number> }) => `${i18nKey}:${JSON.stringify(values)}`,
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({ t: (key: string, params?: Record<string, unknown>) => (
    params ? `${key}:${JSON.stringify(params)}` : key
  ) }),
}))

const models: ModelPricingConfig[] = [
  { model: 'model-a', pricing_style: 'openai', base_prices: { input: 1, output: 4, cache_read: 0, cache_write: 0 }, model_multiplier: 1, conditional_multipliers: [], branches: [] },
  { model: 'model-b', pricing_style: 'claude', base_prices: { input: 3, output: 15, cache_read: 0.3, cache_write: 3.75 }, model_multiplier: 1.2, conditional_multipliers: [{ key: 'endpoint', value: '/v1/messages', multiplier: 2 }], branches: [{
    id: 'night', name: 'Night', context: { type: 'gt', threshold: 200_000 },
    period: { type: 'window', start: '22:00', end: '06:00' },
    prices: { input: 2, output: 10, cache_read: 0.2, cache_write: 2 },
  }] },
]

const runningTask: PricingRecalculationTask = {
  task_id: 'task-1', status: 'running', stage: 'events', start_at: '2026-09-22T09:30:00+05:30',
  end_at: '2026-09-23T10:30:00+05:30', config_revision: 7, processed_count: 20,
  total_count: null, updated_at: '2026-09-23T10:31:00+05:30', error: null,
}

describe('PricingRecalculationModal', () => {
  let container: HTMLDivElement
  let root: Root
  let revision: number
  let emptyHours: boolean
  let optionsCalls: number
  let onStart: ReturnType<typeof vi.fn<(startAt: string, configRevision: number) => Promise<StartPricingRecalculationResponse | null>>>
  let onReloadPricing: ReturnType<typeof vi.fn<() => Promise<boolean>>>
  let onClose: ReturnType<typeof vi.fn<() => void>>

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    revision = 7
    emptyHours = false
    optionsCalls = 0
    onStart = vi.fn(async () => ({ started: true, task: runningTask }))
    onReloadPricing = vi.fn(async () => true)
    onClose = vi.fn()
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = new URL(String(input), 'http://localhost')
      if (!url.pathname.endsWith('/pricing/recalculations/options')) throw new Error(`unexpected request ${url}`)
      optionsCalls++
      return Response.json({
        timezone: 'Asia/Kolkata', earliest_start: emptyHours ? null : '2026-09-22T09:30:00+05:30',
        latest_start: emptyHours ? null : '2026-09-22T11:30:00+05:30', step_seconds: 3600,
        max_days: 30, config_revision: revision,
      })
    })
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    vi.restoreAllMocks()
  })

  const renderModal = async (overrides: Partial<Parameters<typeof PricingRecalculationModal>[0]> = {}) => {
    await act(async () => root.render(<PricingRecalculationModal open models={models} configRevision={7}
      task={null} starting={false} error="" errorCode="" connectionError=""
      onStart={onStart} onReloadPricing={onReloadPricing} onRefreshCurrent={async () => null} onClose={onClose} {...overrides} />))
  }

  it('shows loading inside disabled time controls until options arrive', async () => {
    let finish!: (response: Response) => void
    vi.mocked(fetch).mockImplementationOnce(() => new Promise<Response>((resolve) => { finish = resolve }))
    await renderModal()
    const date = document.querySelector<HTMLButtonElement>('[data-recalculation-date]')!
    const hour = document.querySelector<HTMLButtonElement>('[data-recalculation-hour]')!
    expect(date.disabled).toBe(true)
    expect(hour.disabled).toBe(true)
    expect(date.textContent).toBe('common.loading')
    expect(hour.textContent).toBe('common.loading')
    expect(document.querySelector('[role="dialog"] p[role="status"]')).toBeNull()
    await act(async () => finish(Response.json({ timezone: 'Asia/Kolkata',
      earliest_start: '2026-09-22T09:30:00+05:30', latest_start: '2026-09-22T11:30:00+05:30',
      step_seconds: 3600, max_days: 30, config_revision: 7 })))
    expect(date.disabled).toBe(false)
    expect(hour.disabled).toBe(false)
    expect(date.textContent).toBe('2026-09-22')
    expect(hour.textContent).toBe('11:30')
  })

  it('uses one saved model snapshot and server hours, preserving the half-hour offset in the start request', async () => {
    await renderModal()
    expect(optionsCalls).toBe(1)
    const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!
    expect(dialog.textContent).toContain('model-a')
    expect(dialog.textContent).toContain('model-b')
    expect(dialog.textContent).toContain('$15')
    expect(dialog.textContent).not.toContain('$18')
    expect(dialog.textContent).not.toContain('Night')
    expect(dialog.textContent).not.toContain('endpoint = /v1/messages')
    expect(dialog.textContent).toContain('×1.2')
    expect(dialog.textContent).toContain('usage_stats.pricing_settings_rule_counts:{"branches":1,"conditions":1}')
    expect(dialog.textContent).toContain('usage_stats.pricing_recalculation_recent_days')
    const date = dialog.querySelector<HTMLButtonElement>('[data-recalculation-date]')!
    const hour = dialog.querySelector<HTMLButtonElement>('[data-recalculation-hour]')!
    expect(date.textContent).toBe('2026-09-22')
    expect(hour.textContent).toBe('11:30')
    await act(async () => dialog.querySelector<HTMLButtonElement>('[data-recalculation-start]')!.click())
    expect(onStart).toHaveBeenCalledTimes(1)
    expect(onStart).toHaveBeenCalledWith('2026-09-22T11:30:00+05:30', 7)
  })

  it('requires an explicit reread when options revision differs from the displayed models', async () => {
    revision = 8
    await renderModal()
    const start = document.querySelector<HTMLButtonElement>('[data-recalculation-start]')!
    expect(start.disabled).toBe(true)
    expect(document.body.textContent).toContain('usage_stats.pricing_recalculation_config_changed')
    await act(async () => start.click())
    expect(onStart).not.toHaveBeenCalled()
    const reload = document.querySelector<HTMLButtonElement>('[data-recalculation-reload]')!
    await act(async () => reload.click())
    expect(onReloadPricing).toHaveBeenCalledTimes(1)
    expect(optionsCalls).toBe(2)
    await renderModal({ configRevision: 8 })
    expect(document.querySelector<HTMLButtonElement>('[data-recalculation-start]')?.disabled).toBe(false)
  })

  it('shows only committed progress and does not invent a percentage for unknown or zero totals', async () => {
    await renderModal({ task: runningTask })
    expect(document.body.textContent).toContain('20')
    expect(document.querySelector('[role="progressbar"]')).toBeNull()
    await renderModal({ task: { ...runningTask, total_count: 0 } })
    expect(document.querySelector('[role="progressbar"]')).toBeNull()
    await renderModal({ task: { ...runningTask, total_count: 80 } })
    const progress = document.querySelector<HTMLElement>('[role="progressbar"]')!
    expect(progress.getAttribute('aria-valuenow')).toBe('25')
    expect(document.body.textContent).toContain('20')
  })

  it('blocks all closing paths while running and restores return after completion', async () => {
    await renderModal({ task: runningTask })
    const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!
    expect(dialog.textContent).toContain('2026-09-22 09:30')
    expect(dialog.textContent).not.toContain('UTC+05:30')
    expect(dialog.textContent).not.toContain('2026-09-22T09:30:00+05:30')
    expect(dialog.querySelector('[data-recalculation-close]')).toBeNull()
    expect(dialog.querySelector<HTMLButtonElement>('.modal-close-floating')?.disabled).toBe(true)
    await act(async () => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      document.querySelector('.modal-overlay')!.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    })
    expect(onClose).not.toHaveBeenCalled()
    expect(dialog.querySelectorAll('[data-state="done"]')).toHaveLength(1)
    expect(dialog.querySelector('[data-state="active"]')?.textContent).toContain('events')
    await renderModal({ task: { ...runningTask, status: 'completed', stage: 'finalizing' } })
    expect(dialog.querySelectorAll('[data-state="done"]')).toHaveLength(4)
    await act(async () => dialog.querySelector<HTMLButtonElement>('[data-recalculation-close]')!.click())
    expect(onClose).toHaveBeenCalledTimes(1)
    expect(onStart).not.toHaveBeenCalled()
  })

  it('allows an empty saved-price snapshot to recalculate historical events as unavailable costs', async () => {
    await renderModal({ models: [] })
    expect(document.querySelector<HTMLButtonElement>('[data-recalculation-start]')?.disabled).toBe(false)
    await act(async () => document.querySelector<HTMLButtonElement>('[data-recalculation-start]')!.click())
    expect(onStart).toHaveBeenCalledWith('2026-09-22T11:30:00+05:30', 7)
  })

  it('reloads legal hours after a running task disappears on server restart', async () => {
    await renderModal({ task: runningTask })
    expect(optionsCalls).toBe(0)
    await renderModal({ task: null })
    expect(optionsCalls).toBe(1)
    expect(document.querySelector<HTMLButtonElement>('[data-recalculation-start]')?.disabled).toBe(false)
  })

  it('disables start and explains when the server has no legal recalculation hour', async () => {
    emptyHours = true
    await renderModal()
    expect(document.body.textContent).toContain('usage_stats.pricing_recalculation_no_data')
    expect(document.querySelector<HTMLButtonElement>('[data-recalculation-start]')?.disabled).toBe(true)
    expect(onStart).not.toHaveBeenCalled()
  })

  it('distinguishes different UTC offsets when the task range crosses a DST change', async () => {
    await renderModal({ task: { ...runningTask, start_at: '2026-11-01T01:00:00-04:00', end_at: '2026-11-01T01:00:00-05:00' } })
    expect(document.body.textContent).toContain('2026-11-01 01:00 UTC-04:00')
    expect(document.body.textContent).toContain('2026-11-01 01:00 UTC-05:00')
  })
})
