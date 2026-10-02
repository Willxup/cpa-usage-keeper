import { selectOption } from './selectOption'
// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ModelPricingConfig, PricingRecalculationTask } from '@/lib/types'
import { PricingSettings } from '../PricingSettings'

vi.mock('react-i18next', () => ({
  Trans: ({ i18nKey, values }: { i18nKey: string; values: Record<string, number> }) => `${i18nKey}:${JSON.stringify(values)}`,
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({ t: (key: string, params?: Record<string, unknown>) => (
    params ? `${key}:${JSON.stringify(params)}` : key
  ) }),
}))

const existing: ModelPricingConfig = {
  model: 'provider/claude-sonnet', pricing_style: 'claude',
  base_prices: { input: 3, output: 15, cache_read: 0.3, cache_write: 3.75 },
  model_multiplier: 1.4,
  conditional_multipliers: [
    { key: 'reasoning_effort', value: 'xhigh', multiplier: 1.2 },
    { key: 'endpoint', value: '/v1/responses', multiplier: 1.1 },
  ],
  branches: [{
    id: 'saved-branch', name: 'Long context', context: { type: 'gt', threshold: 200_000 },
    period: { type: 'all' },
    prices: { input: 2.5, output: 12, cache_read: 0.25, cache_write: 3 },
  }],
}

const runningTask: PricingRecalculationTask = {
  task_id: 'running-1', status: 'running', stage: 'events', start_at: '2026-09-22T09:30:00+05:30',
  end_at: '2026-09-23T09:30:00+05:30', config_revision: 7, processed_count: 4,
  total_count: 8, updated_at: '2026-09-23T09:31:00+05:30', error: null,
}

const setInput = async (input: HTMLInputElement, value: string) => {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}

const button = (label: string) => {
  const found = [...document.body.querySelectorAll<HTMLButtonElement>('button')]
    .find((candidate) => candidate.textContent?.trim() === label)
  expect(found, label).toBeDefined()
  return found!
}

describe('PricingSettings', () => {
  let container: HTMLDivElement
  let root: Root
  let configs: ModelPricingConfig[]
  let revision: number
  let currentTask: PricingRecalculationTask | null
  let writes: Array<{ method: string; url: URL; body?: ModelPricingConfig }>

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    configs = [structuredClone(existing)]
    revision = 7
    currentTask = null
    writes = []
    vi.stubGlobal('window', window)
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = new URL(String(input), 'http://localhost')
      if (url.pathname.endsWith('/pricing/model-options')) {
        return Response.json({ models: ['provider/claude-sonnet', 'openai/gpt-5', 'gemini/3'] })
      }
      if (url.pathname.endsWith('/pricing/recalculations/current')) return Response.json(currentTask)
      if (!url.pathname.endsWith('/pricing/models')) throw new Error(`unexpected pricing request ${url}`)
      if (init?.method === 'PUT') {
        const config = JSON.parse(String(init.body)) as ModelPricingConfig
        writes.push({ method: 'PUT', url, body: config })
        configs = [...configs.filter((item) => item.model !== config.model), config]
        revision++
        return Response.json({ model: config.model, config_revision: revision })
      }
      if (init?.method === 'DELETE') {
        writes.push({ method: 'DELETE', url })
        configs = configs.filter((item) => item.model !== url.searchParams.get('model'))
        revision++
        return Response.json({ config_revision: revision })
      }
      return Response.json({ models: configs, config_revision: revision })
    })
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    vi.useRealTimers()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  const renderSettings = async (props: Partial<Parameters<typeof PricingSettings>[0]> = {}) => {
    await act(async () => root.render(<PricingSettings {...props} />))
  }

  it('loads complete saved prices and adds a model with free-text conditions in one save', async () => {
    await renderSettings()
    expect(document.body.textContent).toContain('provider/claude-sonnet')
    await act(async () => button('usage_stats.pricing_settings_add').click())
    const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!
    const modelSelect = dialog.querySelector<HTMLButtonElement>('[data-pricing-field="model"]')!
    const inputPrice = dialog.querySelector<HTMLInputElement>('[data-pricing-field="base_prices.input"]')!
    expect(modelSelect.compareDocumentPosition(inputPrice) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(modelSelect.textContent).toBe('openai/gpt-5')
    expect(inputPrice.getAttribute('aria-invalid')).not.toBe('true')
    for (const [field, value] of Object.entries({ input: '1.25', output: '10', cache_read: '0.125', cache_write: '0' })) {
      await setInput(dialog.querySelector<HTMLInputElement>(`[data-pricing-field="base_prices.${field}"]`)!, value)
    }
    await act(async () => button('usage_stats.pricing_settings_add_condition').click())
    await setInput(dialog.querySelector<HTMLInputElement>('[data-pricing-field="conditional_multipliers[0].key"]')!, 'reasoning_effort')
    await setInput(dialog.querySelector<HTMLInputElement>('[data-pricing-field="conditional_multipliers[0].value"]')!, 'xhigh')
    await setInput(dialog.querySelector<HTMLInputElement>('[data-pricing-field="conditional_multipliers[0].multiplier"]')!, '1.5')
    await act(async () => button('common.save').click())

    expect(writes).toHaveLength(1)
    expect(writes[0].body).toEqual({
      model: 'openai/gpt-5', pricing_style: 'openai',
      base_prices: { input: 1.25, output: 10, cache_read: 0.125, cache_write: 0 },
      model_multiplier: 1,
      conditional_multipliers: [{ key: 'reasoning_effort', value: 'xhigh', multiplier: 1.5 }],
      branches: [],
    })
    expect(document.body.textContent).toContain('openai/gpt-5')
  })

  it('discards an edit draft and later saves the original branches and legal condition values', async () => {
    await renderSettings()
    await act(async () => button('common.edit').click())
    let dialog = document.querySelector<HTMLElement>('[role="dialog"]')!
    expect(dialog.querySelector<HTMLButtonElement>('[data-pricing-field="model"]')).toBeNull()
    expect(dialog.querySelector<HTMLInputElement>('[data-pricing-field="conditional_multipliers[0].value"]')?.value).toBe('xhigh')
    expect(dialog.querySelector<HTMLInputElement>('[data-pricing-field="conditional_multipliers[1].key"]')?.value).toBe('endpoint')
    expect(dialog.querySelector<HTMLInputElement>('[data-pricing-field="conditional_multipliers[1].value"]')?.value).toBe('/v1/responses')
    await setInput(dialog.querySelector<HTMLInputElement>('[data-pricing-field="base_prices.input"]')!, '99')
    await act(async () => button('common.cancel').click())
    expect(writes).toHaveLength(0)

    await act(async () => button('common.edit').click())
    dialog = document.querySelector<HTMLElement>('[role="dialog"]')!
    expect(dialog.querySelector<HTMLInputElement>('[data-pricing-field="base_prices.input"]')?.value).toBe('3')
    await setInput(dialog.querySelector<HTMLInputElement>('[data-pricing-field="base_prices.input"]')!, '4.5')
    await act(async () => button('common.save').click())
    expect(writes).toHaveLength(1)
    expect(writes[0].body?.base_prices.input).toBe(4.5)
    expect(writes[0].body?.conditional_multipliers).toEqual(existing.conditional_multipliers)
    expect(writes[0].body?.branches).toEqual([{ ...existing.branches[0], days: 'all' }])
  })

  it('keeps an unsaved price while switching new-model candidates and discards it on cancel', async () => {
    await renderSettings()
    await act(async () => button('usage_stats.pricing_settings_add').click())
    let dialog = document.querySelector<HTMLElement>('[role="dialog"]')!
    await setInput(dialog.querySelector<HTMLInputElement>('[data-pricing-field="base_prices.input"]')!, '1.75')
    await act(async () => button('common.save').click())
    expect(dialog.querySelector<HTMLInputElement>('[data-pricing-field="base_prices.output"]')?.getAttribute('aria-invalid')).toBe('true')
    const select = dialog.querySelector<HTMLButtonElement>('[data-pricing-field="model"]')!
    await selectOption(select, 'gemini/3')
    expect(select.textContent).toBe('gemini/3')
    expect(dialog.querySelector<HTMLInputElement>('[data-pricing-field="base_prices.input"]')?.value).toBe('1.75')
    expect(dialog.querySelector('[aria-invalid="true"]')).toBeNull()
    expect(dialog.querySelector('[data-shake]')).toBeNull()
    await act(async () => button('common.cancel').click())
    expect(writes).toHaveLength(0)

    await act(async () => button('usage_stats.pricing_settings_add').click())
    dialog = document.querySelector<HTMLElement>('[role="dialog"]')!
    expect(dialog.querySelector<HTMLInputElement>('[data-pricing-field="base_prices.input"]')?.value).toBe('')
    expect(dialog.querySelector<HTMLButtonElement>('[data-pricing-field="model"]')?.textContent).toBe('openai/gpt-5')
  })

  it('shows submitted validation only after save, focuses the field, and accepts explicit zero', async () => {
    await renderSettings()
    await act(async () => button('usage_stats.pricing_settings_add').click())
    const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!
    const input = dialog.querySelector<HTMLInputElement>('[data-pricing-field="base_prices.input"]')!
    const form = dialog.querySelector('form')!
    expect(form.noValidate).toBe(true)
    expect(input.getAttribute('aria-invalid')).toBe('false')

    await act(async () => button('common.save').click())
    expect(writes).toHaveLength(0)
    expect(input.getAttribute('aria-invalid')).toBe('true')
    expect(document.activeElement).toBe(input)
    expect(input.closest('[data-shake]')?.getAttribute('data-shake')).toBe('odd')

    for (const key of ['input', 'output', 'cache_read', 'cache_write']) {
      await setInput(dialog.querySelector<HTMLInputElement>(`[data-pricing-field="base_prices.${key}"]`)!, '0')
    }
    expect(input.getAttribute('aria-invalid')).toBe('false')
    await act(async () => button('common.save').click())
    expect(writes).toHaveLength(1)
    expect(writes[0].body?.base_prices).toEqual({ input: 0, output: 0, cache_read: 0, cache_write: 0 })
  })

  it.each([
    ['conditional_multipliers', 'conditional_multipliers[0].multiplier', 'conditional_multipliers[1].multiplier'],
    ['base_prices', 'base_prices.input', 'base_prices.output'],
  ])('maps a backend %s group error to relevant numeric fields instead of only a banner', async (groupPath, expectedField, anotherField) => {
    await renderSettings()
    await act(async () => button('common.edit').click())
    const details = document.querySelector<HTMLDetailsElement>('[data-pricing-conditions]')!
    await act(async () => details.querySelector('summary')!.click())
    expect(details.open).toBe(false)
    vi.mocked(globalThis.fetch).mockImplementationOnce(async () => Response.json({
      code: 'invalid_pricing', message: 'Invalid field group',
      fields: [{ path: groupPath, code: 'invalid' }],
    }, { status: 400 }))
    await act(async () => button('common.save').click())
    const field = [...document.querySelectorAll<HTMLInputElement>('[data-pricing-field]')]
      .find((input) => input.dataset.pricingField === expectedField)!
    expect(field.getAttribute('aria-invalid')).toBe('true')
    expect(document.querySelector<HTMLInputElement>(`[data-pricing-field="${anotherField}"]`)?.getAttribute('aria-invalid')).toBe('true')
    if (groupPath === 'conditional_multipliers') expect(details.open).toBe(true)
    expect(document.activeElement).toBe(field)
    expect(document.querySelector('[role="alert"]')).toBeNull()
  })

  it('collapses conditions without discarding their draft values', async () => {
    await renderSettings()
    await act(async () => button('common.edit').click())
    let details = document.querySelector<HTMLDetailsElement>('[data-pricing-conditions]')!
    const value = details.querySelector<HTMLInputElement>('[data-pricing-field="conditional_multipliers[0].value"]')!
    await setInput(value, 'new-free-text')
    await act(async () => details.querySelector('summary')!.click())
    expect(details.open).toBe(false)
    await act(async () => details.querySelector('summary')!.click())
    expect(details.open).toBe(true)
    expect(value.value).toBe('new-free-text')
    await act(async () => details.querySelector('summary')!.click())
    await act(async () => document.querySelector<HTMLButtonElement>('[data-branch-action="edit"]')!.click())
    await act(async () => button('common.cancel').click())
    details = document.querySelector<HTMLDetailsElement>('[data-pricing-conditions]')!
    expect(details.open).toBe(false)
    expect(details.querySelector<HTMLInputElement>('[data-pricing-field="conditional_multipliers[0].value"]')?.value).toBe('new-free-text')
    expect(writes).toHaveLength(0)
  })

  it('previews only draft prices times the model multiplier and lists conditions separately', async () => {
    await renderSettings()
    await act(async () => button('common.edit').click())
    const preview = document.querySelector<HTMLElement>('[data-pricing-preview]')!
    expect(preview).not.toBeNull()
    expect(preview.querySelector('[data-preview-plan="default"] [data-preview-price="input"]')?.textContent).toContain('$4.2')
    expect(preview.querySelector('[data-preview-plan="saved-branch"] [data-preview-price="input"]')?.textContent).toContain('$3.5')
    expect(preview.textContent).toContain('reasoning_effort = xhigh')
    expect(preview.textContent).toContain('×1.2')
    await setInput(document.querySelector<HTMLInputElement>('[data-pricing-field="base_prices.input"]')!, '4')
    expect(preview.querySelector('[data-preview-plan="default"] [data-preview-price="input"]')?.textContent).toContain('$5.6')
    expect(preview.querySelector('[data-preview-plan="saved-branch"] [data-preview-price="input"]')?.textContent).toContain('$3.5')
    expect(writes).toHaveLength(0)
  })

  it('shows an explicit zero multiplier as free and leaves incomplete prices unavailable in preview', async () => {
    await renderSettings()
    await act(async () => button('common.edit').click())
    const multiplier = document.querySelector<HTMLInputElement>('[data-pricing-field="model_multiplier"]')!
    const defaultInput = document.querySelector<HTMLOutputElement>('[data-preview-plan="default"] [data-preview-price="input"]')!
    await setInput(multiplier, '0')
    expect(defaultInput.textContent).toBe('$0')
    await setInput(multiplier, '')
    expect(defaultInput.textContent).toBe('—')
    await setInput(multiplier, '1')
    await setInput(document.querySelector<HTMLInputElement>('[data-pricing-field="base_prices.input"]')!, '')
    expect(defaultInput.textContent).toBe('—')
  })

  it('keeps a branch subdraft isolated until saved, and rejects a copied overlapping branch', async () => {
    await renderSettings()
    await act(async () => button('common.edit').click())
    await act(async () => document.querySelector<HTMLButtonElement>('[data-branch-action="edit"]')!.click())
    await setInput(document.querySelector<HTMLInputElement>('[data-pricing-field="name"]')!, 'Unsaved name')
    await act(async () => button('common.cancel').click())
    expect(document.body.textContent).toContain('Long context')
    expect(document.body.textContent).not.toContain('Unsaved name')

    await act(async () => document.querySelector<HTMLButtonElement>('[data-branch-action="copy"]')!.click())
    expect(document.querySelector<HTMLInputElement>('[data-pricing-field="prices.input"]')?.value).toBe('2.5')
    await act(async () => button('usage_stats.pricing_settings_save_branch').click())
    expect(document.querySelector<HTMLInputElement>('[data-pricing-field="context.threshold"]')?.getAttribute('aria-invalid')).toBe('true')
    expect(writes).toHaveLength(0)
    const contextSelect = document.querySelector<HTMLButtonElement>('[data-pricing-field="context.type"]')!
    await selectOption(contextSelect, 'usage_stats.pricing_settings_context_lte')
    await act(async () => button('usage_stats.pricing_settings_save_branch').click())
    expect(document.querySelector('[data-pricing-branch-editor]')).toBeNull()
    expect(writes).toHaveLength(0)
    await act(async () => button('common.save').click())
    expect(writes).toHaveLength(1)
    expect(writes[0].body?.branches).toHaveLength(2)
    expect(writes[0].body?.branches[0]).toEqual({ ...existing.branches[0], days: 'all' })
    expect(writes[0].body?.branches[1].id).not.toBe(existing.branches[0].id)
    expect(writes[0].body?.branches[1].context).toEqual({ type: 'lte', threshold: 200_000 })
  })

  it('edits and copies applicable days, rejects a cross-midnight weekday window, and saves both branches', async () => {
    await renderSettings()
    await act(async () => button('common.edit').click())
    await act(async () => document.querySelector<HTMLButtonElement>('[data-branch-action="edit"]')!.click())
    await act(async () => button('usage_stats.pricing_settings_days_weekday').click())
    expect(button('usage_stats.pricing_settings_days_weekday').getAttribute('aria-pressed')).toBe('true')
    await selectOption(document.querySelector<HTMLButtonElement>('[data-pricing-field="period.type"]')!, 'usage_stats.pricing_settings_period_window')
    await selectOption(document.querySelector<HTMLButtonElement>('[data-pricing-field="period.start"]')!, '20')
    await selectOption(document.querySelector<HTMLButtonElement>('[data-pricing-field="period.end"]')!, '08')
    await act(async () => button('usage_stats.pricing_settings_save_branch').click())
    expect(document.body.textContent).toContain('usage_stats.pricing_settings_error_cross_day')
    expect(document.querySelector('[data-pricing-branch-editor]')).not.toBeNull()
    await selectOption(document.querySelector<HTMLButtonElement>('[data-pricing-field="period.end"]')!, '22')
    await act(async () => button('usage_stats.pricing_settings_save_branch').click())
    expect(document.querySelector('[data-pricing-branch-editor]')).toBeNull()
    await act(async () => document.querySelector<HTMLButtonElement>('[data-branch-action="copy"]')!.click())
    expect(button('usage_stats.pricing_settings_days_weekday').getAttribute('aria-pressed')).toBe('true')
    await act(async () => button('usage_stats.pricing_settings_days_weekend').click())
    await act(async () => button('usage_stats.pricing_settings_save_branch').click())
    await act(async () => button('common.save').click())
    expect(writes).toHaveLength(1)
    expect(writes[0].body?.branches.map((branch) => branch.days)).toEqual(['weekday', 'weekend'])
  })

  it('adds a range and overnight branch with four independent prices in the same model save', async () => {
    await renderSettings()
    await act(async () => button('common.edit').click())
    await act(async () => button('usage_stats.pricing_settings_add_branch').click())
    await setInput(document.querySelector<HTMLInputElement>('[data-pricing-field="name"]')!, 'Short night')
    for (const [path, value] of [['context.type', 'range'], ['period.type', 'window']]) {
      const select = document.querySelector<HTMLButtonElement>(`[data-pricing-field="${path}"]`)!
    await selectOption(select, `usage_stats.pricing_settings_${path.startsWith('context') ? 'context' : 'period'}_${value}`)
    }
    await setInput(document.querySelector<HTMLInputElement>('[data-pricing-field="context.min"]')!, '0')
    await setInput(document.querySelector<HTMLInputElement>('[data-pricing-field="context.max"]')!, '200000')
    await selectOption(document.querySelector<HTMLButtonElement>('[data-pricing-field="period.start"]')!, '20')
    await selectOption(document.querySelector<HTMLButtonElement>('[data-pricing-field="period.end"]')!, '08')
    await setInput(document.querySelector<HTMLInputElement>('[data-pricing-field="prices.input"]')!, '0')
    await act(async () => button('usage_stats.pricing_settings_save_branch').click())
    expect(document.querySelector('[data-pricing-branch-editor]')).toBeNull()
    await act(async () => button('common.save').click())
    expect(writes).toHaveLength(1)
    expect(writes[0].body?.branches[1]).toMatchObject({
      name: 'Short night', context: { type: 'range', min: 0, max: 200_000 },
      period: { type: 'window', start: '20:00', end: '08:00' },
      prices: { input: 0, output: 15, cache_read: 0.3, cache_write: 3.75 },
    })
  })

  it.each([
    ['branches[0].prices.input', false],
    ['branches[0].prices', true],
  ])('opens the affected branch when the complete save receives %s', async (path, group) => {
    await renderSettings()
    await act(async () => button('common.edit').click())
    vi.mocked(globalThis.fetch).mockImplementationOnce(async () => Response.json({
      code: 'invalid_pricing', message: 'Invalid branch price',
      fields: [{ path, code: 'invalid' }],
    }, { status: 400 }))
    await act(async () => button('common.save').click())
    const field = document.querySelector<HTMLInputElement>('[data-pricing-field="prices.input"]')!
    expect(field).not.toBeNull()
    expect(field.getAttribute('aria-invalid')).toBe('true')
    expect(document.activeElement).toBe(field)
    expect(document.querySelector<HTMLInputElement>('[data-pricing-field="prices.output"]')?.getAttribute('aria-invalid')).toBe(group ? 'true' : 'false')
    expect(writes).toHaveLength(0)
  })

  it('keeps a saved branch when model edits are cancelled, and deletes it only with the model save', async () => {
    await renderSettings()
    await act(async () => button('common.edit').click())
    await act(async () => document.querySelector<HTMLButtonElement>('[data-branch-action="delete"]')!.click())
    expect(document.querySelector('[data-pricing-branch-id="saved-branch"]')).toBeNull()
    expect(writes).toHaveLength(0)
    await act(async () => button('common.cancel').click())

    await act(async () => button('common.edit').click())
    expect(document.querySelector('[data-pricing-branch-id="saved-branch"]')).not.toBeNull()
    await act(async () => document.querySelector<HTMLButtonElement>('[data-branch-action="delete"]')!.click())
    await act(async () => button('common.save').click())
    expect(writes).toHaveLength(1)
    expect(writes[0].body?.branches).toEqual([])
  })

  it('initializes desktop preview from mounted form width and preserves manual collapse through branch editing', async () => {
    vi.spyOn(HTMLFormElement.prototype, 'clientWidth', 'get').mockReturnValue(1100)
    await renderSettings()
    await act(async () => button('common.edit').click())
    const preview = document.querySelector<HTMLDetailsElement>('[data-pricing-preview]')!
    expect(preview.open).toBe(true)
    expect(document.querySelector<HTMLDetailsElement>('[data-pricing-branches]')?.open).toBe(true)
    await act(async () => preview.querySelector('summary')!.click())
    expect(preview.open).toBe(false)
    await act(async () => document.querySelector<HTMLButtonElement>('[data-branch-action="edit"]')!.click())
    await act(async () => button('common.cancel').click())
    expect(document.querySelector<HTMLDetailsElement>('[data-pricing-preview]')?.open).toBe(false)
  })

  it('marks changed model drafts as unsaved and clears the badge when reverted', async () => {
    await renderSettings()
    await act(async () => button('common.edit').click())
    const title = () => document.querySelector('[role="dialog"]')!.textContent
    expect(title()).not.toContain('usage_stats.pricing_settings_unsaved')
    const price = document.querySelector<HTMLInputElement>('[data-pricing-field="base_prices.input"]')!
    const original = price.value
    await setInput(price, '99')
    expect(title()).toContain('usage_stats.pricing_settings_unsaved')
    await setInput(price, original)
    expect(title()).not.toContain('usage_stats.pricing_settings_unsaved')
  })

  it('does not append a completed recalculation notice to the model list', async () => {
    currentTask = { ...runningTask, status: 'completed', stage: 'finalizing', processed_count: 8 }
    await renderSettings()
    expect(document.querySelector('section[aria-label="usage_stats.model_price_settings_title"] > p[role="status"]')).toBeNull()
    expect(document.body.textContent).not.toContain('usage_stats.pricing_recalculation_view_progress')
  })

  it('requires confirmation before deleting configuration and keeps the model query intact', async () => {
    await renderSettings()
    await act(async () => document.querySelector<HTMLButtonElement>('[aria-label="common.delete provider/claude-sonnet"]')!.click())
    expect(writes).toHaveLength(0)
    const confirmation = document.querySelector<HTMLElement>('[role="dialog"]')!
    expect(confirmation.textContent).toContain('provider/claude-sonnet')
    await act(async () => confirmation.querySelector<HTMLButtonElement>('button')!.click())
    expect(writes).toHaveLength(0)
    await act(async () => document.querySelector<HTMLButtonElement>('[aria-label="common.delete provider/claude-sonnet"]')!.click())
    const confirm = [...document.querySelectorAll<HTMLElement>('[role="dialog"] button')]
      .find((candidate) => candidate.textContent?.trim() === 'common.delete') as HTMLButtonElement
    await act(async () => confirm.click())
    expect(writes).toHaveLength(1)
    expect(writes[0].method).toBe('DELETE')
    expect(writes[0].url.pathname).toBe('/api/v1/pricing/models')
    expect(writes[0].url.searchParams.get('model')).toBe('provider/claude-sonnet')
    expect(document.body.textContent).toContain('usage_stats.model_price_empty')
  })

  it('keeps the all-model recalculation entry available after every price configuration is deleted', async () => {
    configs = []
    await renderSettings()
    const recalculate = button('usage_stats.pricing_recalculation_title')
    expect(recalculate.disabled).toBe(false)
  })

  it('locks every configuration write while the server reports a running recalculation', async () => {
    currentTask = runningTask
    await renderSettings()
    expect(button('usage_stats.pricing_settings_add').disabled).toBe(true)
    expect(button('usage_stats.pricing_settings_sync_title').disabled).toBe(true)
    expect(button('usage_stats.pricing_recalculation_title').disabled).toBe(true)
    expect(button('common.edit').disabled).toBe(true)
    expect(document.querySelector<HTMLButtonElement>('[aria-label="common.delete provider/claude-sonnet"]')!.disabled).toBe(true)
    expect(document.body.textContent).toContain('50%')
    expect(button('usage_stats.pricing_recalculation_view_progress').disabled).toBe(false)
    expect(writes).toHaveLength(0)
  })

  it('refreshes the current task when a concurrent editor save receives pricing_busy', async () => {
    await renderSettings()
    await act(async () => button('common.edit').click())
    currentTask = runningTask
    vi.mocked(globalThis.fetch).mockImplementationOnce(async () => Response.json({ code: 'pricing_busy', message: 'Recalculation running' }, { status: 409 }))
    await act(async () => button('common.save').click())
    expect(writes).toHaveLength(0)
    expect(button('usage_stats.pricing_settings_add').disabled).toBe(true)
    expect(document.body.textContent).toContain('usage_stats.pricing_recalculation_view_progress')
  })

  it('rereads the visible price list and retires outer quota responses only after the observed task settles', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    currentTask = runningTask
    const onRecalculationSettled = vi.fn()
    await renderSettings({ onRecalculationSettled })
    const modelReads = () => vi.mocked(globalThis.fetch).mock.calls
      .filter(([input]) => new URL(String(input), 'http://localhost').pathname.endsWith('/pricing/models')).length
    expect(modelReads()).toBe(1)
    currentTask = { ...runningTask, status: 'completed', stage: 'finalizing', processed_count: 8 }
    await act(async () => vi.advanceTimersByTimeAsync(2000))
    expect(modelReads()).toBe(2)
    expect(onRecalculationSettled).toHaveBeenCalledTimes(1)
    expect(onRecalculationSettled).toHaveBeenCalledWith(currentTask)
  })
})
