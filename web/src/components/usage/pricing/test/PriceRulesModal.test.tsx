// @vitest-environment happy-dom

import { readFileSync } from 'node:fs'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PricingRule, ReplacePricingRuleInput } from '@/lib/types'
import { PriceRulesModal } from '../PriceRulesModal'

const modalStylesSource = readFileSync('src/components/usage/pricing/PriceRulesModal.module.scss', 'utf8')
const questionMarkHelpStylesSource = readFileSync('src/components/ui/QuestionMarkHelpButton.module.scss', 'utf8')

globalThis.IS_REACT_ACT_ENVIRONMENT = true

const translations: Record<string, string> = {
  'common.cancel': 'Cancel',
  'common.save': 'Save',
  'usage_stats.model_price_rules_title': 'Rules · {{model}}',
  'usage_stats.model_price_rules_key': 'Key',
  'usage_stats.model_price_rules_value': 'Value',
  'usage_stats.model_price_rules_multiplier': 'Multiplier',
  'usage_stats.model_price_rules_add': 'Add Rule',
  'usage_stats.model_price_rules_remove': 'Remove',
  'usage_stats.model_price_rules_help_label': 'How pricing rules work',
  'usage_stats.model_price_rules_help': 'Matching rules multiply together.',
  'usage_stats.model_price_rules_help_examples': 'Examples:',
  'usage_stats.model_price_rules_help_service_tier': 'service_tier = priority, then set a multiplier.',
  'usage_stats.model_price_rules_help_reasoning_effort': 'reasoning_effort = xhigh, then set a multiplier.',
  'usage_stats.model_price_rules_loading': 'Loading rules...',
  'usage_stats.model_price_rules_load_failed': 'Unable to load rules.',
  'usage_stats.model_price_rules_save_success': 'Rules saved.',
  'usage_stats.model_price_rules_save_failed': 'Unable to save rules.',
  'usage_stats.model_price_rules_key_required': 'Enter a key.',
  'usage_stats.model_price_rules_value_required': 'Enter a value.',
  'usage_stats.model_price_rules_multiplier_invalid': 'Enter a finite non-negative multiplier.',
  'usage_stats.model_price_rules_duplicate': 'This rule is duplicated.',
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, params?: Record<string, string>) => (
      (translations[key] ?? key).replace('{{model}}', params?.model ?? '')
    ),
  }),
}))

const deferred = <T,>() => {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

const buttonByText = (text: string): HTMLButtonElement => {
  const button = Array.from(document.body.querySelectorAll<HTMLButtonElement>('button'))
    .find((candidate) => candidate.textContent?.trim() === text)
  expect(button, `button ${text}`).toBeDefined()
  return button as HTMLButtonElement
}

describe('PriceRulesModal', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(() => {
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    document.body.innerHTML = ''
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  const renderModal = async (props: {
    model: string
    loadRules: (model: string) => Promise<PricingRule[]>
    saveRules?: (model: string, rules: ReplacePricingRuleInput[]) => Promise<PricingRule[]>
    onClose?: () => void
    onNotice?: (kind: 'success' | 'info' | 'error', message: string) => void
  }) => {
    await act(async () => {
      root.render(
        <PriceRulesModal
          open
          model={props.model}
          onClose={props.onClose ?? (() => undefined)}
          loadRules={props.loadRules}
          saveRules={props.saveRules ?? (async () => [])}
          onNotice={props.onNotice}
        />,
      )
      await Promise.resolve()
    })
  }

  const changeInput = async (selector: string, value: string) => {
    const input = document.body.querySelector<HTMLInputElement>(selector)
    expect(input, selector).not.toBeNull()
	await changeInputElement(input!, value)
  }

  const changeInputElement = async (input: HTMLInputElement, value: string) => {
    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
      setter?.call(input, value)
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
  }

  const dispatchPointer = async (target: Element, type: string, pointerType: string) => {
    await act(async () => {
      const event = new Event(type, { bubbles: true })
      Object.defineProperty(event, 'pointerType', { value: pointerType })
      target.dispatchEvent(event)
    })
  }

  it('treats the empty row as a placeholder and saves an explicit empty set', async () => {
    const saveRules = vi.fn(async (_model: string, _rules: ReplacePricingRuleInput[]) => [])
    const onClose = vi.fn()
    await renderModal({ model: 'model-a', loadRules: async () => [], saveRules, onClose })

    expect(document.body.querySelectorAll('[data-rule-field="key"]')).toHaveLength(1)
    await act(async () => buttonByText('Save').click())

    expect(saveRules).toHaveBeenCalledWith('model-a', [])
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('removes the final persisted rule and submits rules: [] only when saved', async () => {
    const saveRules = vi.fn(async (_model: string, _rules: ReplacePricingRuleInput[]) => [])
    await renderModal({
      model: 'model-a',
      loadRules: async () => [{ key: 'service_tier', value: 'priority', multiplier: 2 }],
      saveRules,
    })

    await act(async () => buttonByText('Remove').click())
    expect(saveRules).not.toHaveBeenCalled()
    expect(document.body.querySelectorAll('[data-rule-field="key"]')).toHaveLength(1)

    await act(async () => buttonByText('Save').click())
    expect(saveRules).toHaveBeenCalledWith('model-a', [])
  })

  it('uses the same danger treatment as Delete without a leading icon', async () => {
    await renderModal({ model: 'model-a', loadRules: async () => [] })

    const removeButton = buttonByText('Remove')
    expect(removeButton.classList.contains('btn-danger')).toBe(true)
    expect(removeButton.querySelector('svg')).toBeNull()
  })

  it('keeps all three rule fields on the shared pill input contract', async () => {
    await renderModal({ model: 'model-a', loadRules: async () => [] })

    expect(document.body.querySelectorAll('.input[data-rule-field]')).toHaveLength(3)
    const inputRule = modalStylesSource.match(/\.ruleInput:global\(\.input\)\s*\{([\s\S]*?)\n\}/)?.[1] ?? ''
    const removeButtonRule = modalStylesSource.match(/\.removeButton\s*\{([\s\S]*?)\n\}/)?.[1] ?? ''

    expect(inputRule).toContain('height: 32px;')
    expect(inputRule).toContain('min-height: 32px;')
    expect(inputRule).toContain('padding: 6px 12px;')
    expect(inputRule).toContain('line-height: 18px;')
    expect(inputRule).toContain('border-radius: 999px;')
    expect(inputRule).not.toContain('height: 40px;')
    expect(removeButtonRule).toContain('margin-top: 16px;')
  })

  it('shows row validation and does not call the backend for a partial rule', async () => {
    const saveRules = vi.fn(async (_model: string, _rules: ReplacePricingRuleInput[]) => [])
    await renderModal({ model: 'model-a', loadRules: async () => [], saveRules })

    await changeInput('[data-rule-field="key"]', 'service_tier')
    await act(async () => buttonByText('Save').click())

    expect(saveRules).not.toHaveBeenCalled()
    expect(document.body.textContent).toContain('Enter a value.')
	const invalidValue = document.body.querySelector<HTMLInputElement>('[data-rule-field="value"]')
	expect(invalidValue?.getAttribute('aria-invalid')).toBe('true')
	expect(document.activeElement).toBe(invalidValue)
  })

  it('clears a stale duplicate error as soon as editing another row resolves the conflict', async () => {
    const saveRules = vi.fn(async (_model: string, rules: ReplacePricingRuleInput[]) => rules.map((rule) => ({
      ...rule,
      multiplier: rule.multiplier ?? 1,
    })))
    await renderModal({
      model: 'model-a',
      loadRules: async () => [{ key: 'service_tier', value: 'priority', multiplier: 2 }],
      saveRules,
    })
    await act(async () => buttonByText('Add Rule').click())
    const keys = document.body.querySelectorAll<HTMLInputElement>('[data-rule-field="key"]')
    const values = document.body.querySelectorAll<HTMLInputElement>('[data-rule-field="value"]')
    await changeInputElement(keys[1], 'service_tier')
    await changeInputElement(values[1], 'priority')
    await act(async () => buttonByText('Save').click())
    expect(document.body.textContent).toContain('This rule is duplicated.')

    await changeInputElement(values[0], 'default')
    expect(document.body.textContent).not.toContain('This rule is duplicated.')
  })

  it('clears a stale duplicate error when either conflicting row is removed', async () => {
    await renderModal({
      model: 'model-a',
      loadRules: async () => [{ key: 'service_tier', value: 'priority', multiplier: 2 }],
    })
    await act(async () => buttonByText('Add Rule').click())
    const keys = document.body.querySelectorAll<HTMLInputElement>('[data-rule-field="key"]')
    const values = document.body.querySelectorAll<HTMLInputElement>('[data-rule-field="value"]')
    await changeInputElement(keys[1], 'service_tier')
    await changeInputElement(values[1], 'priority')
    await act(async () => buttonByText('Save').click())
    expect(document.body.textContent).toContain('This rule is duplicated.')

    const removeButtons = Array.from(document.body.querySelectorAll<HTMLButtonElement>('button'))
      .filter((button) => button.textContent?.trim() === 'Remove')
    await act(async () => removeButtons[0].click())
    expect(document.body.textContent).not.toContain('This rule is duplicated.')
  })

  it('shows the backend rule validation detail in an assertive alert without clearing the draft', async () => {
	const saveRules = vi.fn(async () => {
	  throw new Error('invalid pricing rule: unsupported key "provider"')
	})
	await renderModal({ model: 'model-a', loadRules: async () => [], saveRules })
	await changeInput('[data-rule-field="key"]', 'provider')
	await changeInput('[data-rule-field="value"]', 'openai')

	await act(async () => {
	  buttonByText('Save').click()
	  await Promise.resolve()
	})

	const alert = document.body.querySelector<HTMLElement>('[role="alert"]')
	expect(alert?.getAttribute('aria-live')).toBe('assertive')
	expect(alert?.textContent).toContain('unsupported key "provider"')
	expect(document.activeElement).toBe(alert)
	expect(document.body.querySelector<HTMLInputElement>('[data-rule-field="key"]')?.value).toBe('provider')
	expect(document.body.querySelector<HTMLInputElement>('[data-rule-field="value"]')?.value).toBe('openai')
  })

  it('does not allow an empty placeholder to overwrite rules after loading fails', async () => {
    const saveRules = vi.fn(async (_model: string, _rules: ReplacePricingRuleInput[]) => [])
    await renderModal({
      model: 'model-a',
      loadRules: async () => {
        throw new Error('network unavailable')
      },
      saveRules,
    })

    expect(document.body.textContent).toContain('Unable to load rules.')
    expect(buttonByText('Save').disabled).toBe(true)
    await act(async () => buttonByText('Save').click())
    expect(saveRules).not.toHaveBeenCalled()
  })

  it('locks closing, inputs and row actions while the complete set is saving', async () => {
    const saveRequest = deferred<PricingRule[]>()
    await renderModal({
      model: 'model-a',
      loadRules: async () => [{ key: 'reasoning_effort', value: 'xhigh', multiplier: 1.5 }],
      saveRules: async () => saveRequest.promise,
    })

    await act(async () => buttonByText('Save').click())

    const dialog = document.body.querySelector<HTMLElement>('[role="dialog"]')
    expect(dialog?.querySelector<HTMLButtonElement>('.modal-close-floating')?.disabled).toBe(true)
    expect(Array.from(dialog?.querySelectorAll<HTMLInputElement>('input') ?? []).every((input) => input.disabled)).toBe(true)
    expect(buttonByText('Add Rule').disabled).toBe(true)
    expect(buttonByText('Remove').disabled).toBe(true)
    expect(buttonByText('Cancel').disabled).toBe(true)

    await act(async () => {
      saveRequest.resolve([{ key: 'reasoning_effort', value: 'xhigh', multiplier: 1.5 }])
      await saveRequest.promise
    })
  })

  it('keeps late model A reads and saves from overwriting model B', async () => {
    const loadA = deferred<PricingRule[]>()
    const saveA = deferred<PricingRule[]>()
    const loadRules = vi.fn((model: string) => (
      model === 'model-a'
        ? loadA.promise
        : Promise.resolve([{ key: 'reasoning_effort', value: 'xhigh', multiplier: 3 }])
    ))
    const saveRules = vi.fn((model: string) => (
      model === 'model-a' ? saveA.promise : Promise.resolve([])
    ))
    const onClose = vi.fn()

    await renderModal({ model: 'model-a', loadRules, saveRules, onClose })
    await renderModal({ model: 'model-b', loadRules, saveRules, onClose })
    expect(document.body.querySelector<HTMLInputElement>('[data-rule-field="value"]')?.value).toBe('xhigh')

    await act(async () => {
      loadA.resolve([{ key: 'service_tier', value: 'priority', multiplier: 2 }])
      await loadA.promise
    })
    expect(document.body.querySelector<HTMLInputElement>('[data-rule-field="value"]')?.value).toBe('xhigh')

    await renderModal({
      model: 'model-a',
      loadRules: async () => [{ key: 'service_tier', value: 'priority', multiplier: 2 }],
      saveRules,
      onClose,
    })
    await act(async () => buttonByText('Save').click())
    await renderModal({ model: 'model-b', loadRules, saveRules, onClose })
    await act(async () => {
      saveA.resolve([{ key: 'service_tier', value: 'priority', multiplier: 2 }])
      await saveA.promise
    })

    expect(document.body.querySelector<HTMLInputElement>('[data-rule-field="value"]')?.value).toBe('xhigh')
    expect(onClose).not.toHaveBeenCalled()
  })

  it('exposes a keyboard-focusable help tooltip with only the approved examples', async () => {
	vi.stubGlobal('innerWidth', 360)
	vi.stubGlobal('innerHeight', 220)
	await renderModal({ model: 'model-a', loadRules: async () => [] })

	const helpButton = document.body.querySelector<HTMLButtonElement>('[aria-label="How pricing rules work"]')
	expect(helpButton).not.toBeNull()
	expect(helpButton?.textContent).toBe('?')
	expect(questionMarkHelpStylesSource).toMatch(/\.button\s*\{[\s\S]*display:\s*inline-grid;/)
	expect(questionMarkHelpStylesSource).toMatch(/\.button\s*\{[\s\S]*width:\s*18px;/)
	expect(questionMarkHelpStylesSource).toMatch(/\.button\s*\{[\s\S]*height:\s*18px;/)
	expect(questionMarkHelpStylesSource).toMatch(/\.button\s*\{[\s\S]*background:\s*var\(--bg-secondary\);/)
	expect(questionMarkHelpStylesSource).toMatch(/\.button\s*\{[\s\S]*font-size:\s*11px;/)
	expect(questionMarkHelpStylesSource).toMatch(/\.button\s*\{[\s\S]*font-weight:\s*750;/)
	expect(questionMarkHelpStylesSource).toMatch(/\.button\s*\{[\s\S]*cursor:\s*default;/)
	helpButton!.getBoundingClientRect = () => ({
	  x: 280, y: 160, left: 280, top: 160, right: 298, bottom: 178,
	  width: 18, height: 18, toJSON: () => ({}),
	})
	await act(async () => helpButton!.focus())
	const describedBy = helpButton?.getAttribute('aria-describedby')
	const description = describedBy ? document.getElementById(describedBy) : null
	const tooltipId = helpButton?.getAttribute('aria-controls')
	const tooltip = tooltipId ? document.body.querySelector<HTMLElement>(`#${tooltipId}[role="tooltip"]`) : null
	expect(helpButton).not.toBeNull()
	expect(description?.textContent).toContain('Matching rules multiply together.')
	expect(tooltip?.style.position).toBe('fixed')
	expect(tooltip?.style.transform).toBe('translateY(-100%)')
	expect(tooltip?.style.maxHeight).toBe('136px')
	expect(tooltip?.textContent).toContain('Examples:')
    expect(tooltip?.textContent).toContain('service_tier = priority')
    expect(tooltip?.textContent).toContain('reasoning_effort = xhigh')
    expect(tooltip?.textContent?.indexOf('Examples:')).toBeLessThan(
      tooltip?.textContent?.indexOf('service_tier = priority') ?? -1,
    )
    expect(tooltip?.textContent).not.toContain('api_group_key')
    expect(tooltip?.textContent).not.toContain('response_service_tier')

  })

  it('keeps the tooltip open while the pointer crosses from the help button into the portal', async () => {
    vi.useFakeTimers()
    await renderModal({ model: 'model-a', loadRules: async () => [] })

    const helpButton = document.body.querySelector<HTMLButtonElement>('[aria-label="How pricing rules work"]')
    const help = helpButton?.parentElement
    const tooltipId = helpButton?.getAttribute('aria-controls')
    const tooltip = tooltipId ? document.body.querySelector<HTMLElement>(`#${tooltipId}`) : null
    expect(help).not.toBeNull()
    expect(tooltip).not.toBeNull()

    await dispatchPointer(help!, 'pointerover', 'mouse')
    expect(tooltip?.getAttribute('aria-hidden')).toBe('false')
    await dispatchPointer(help!, 'pointerout', 'mouse')
    await act(async () => vi.advanceTimersByTime(50))
    await dispatchPointer(tooltip!, 'pointerover', 'mouse')
    await act(async () => vi.advanceTimersByTime(200))

    expect(tooltip?.getAttribute('aria-hidden')).toBe('false')
  })

  it('opens the scrollable tooltip from a touch pointer on a short viewport', async () => {
    vi.stubGlobal('innerWidth', 360)
    vi.stubGlobal('innerHeight', 120)
    await renderModal({ model: 'model-a', loadRules: async () => [] })

    const helpButton = document.body.querySelector<HTMLButtonElement>('[aria-label="How pricing rules work"]')
    expect(helpButton).not.toBeNull()
    helpButton!.getBoundingClientRect = () => ({
      x: 280, y: 70, left: 280, top: 70, right: 306, bottom: 96,
      width: 26, height: 26, toJSON: () => ({}),
    })
    await dispatchPointer(helpButton!, 'pointerdown', 'touch')

    const tooltipId = helpButton?.getAttribute('aria-controls')
    const tooltip = tooltipId ? document.body.querySelector<HTMLElement>(`#${tooltipId}`) : null
    expect(tooltip?.getAttribute('aria-hidden')).toBe('false')
    expect(Number.parseFloat(tooltip?.style.maxHeight ?? '')).toBeLessThan(144)
    expect(modalStylesSource).toMatch(/\.helpTooltip\s*\{[\s\S]*overflow-y:\s*auto;/)
    expect(modalStylesSource).toMatch(/\.helpTooltipVisible\s*\{[\s\S]*pointer-events:\s*auto;/)
  })

  it('closes a touch-opened tooltip on the second tap even when the button keeps focus', async () => {
    await renderModal({ model: 'model-a', loadRules: async () => [] })

    const helpButton = document.body.querySelector<HTMLButtonElement>('[aria-label="How pricing rules work"]')
    expect(helpButton).not.toBeNull()
    const tooltipId = helpButton?.getAttribute('aria-controls')
    const tooltip = tooltipId ? document.body.querySelector<HTMLElement>(`#${tooltipId}`) : null

    await dispatchPointer(helpButton!, 'pointerdown', 'touch')
    await act(async () => helpButton!.focus())
    expect(tooltip?.getAttribute('aria-hidden')).toBe('false')

    await dispatchPointer(helpButton!, 'pointerdown', 'touch')

    expect(tooltip?.getAttribute('aria-hidden')).toBe('true')
    expect(document.activeElement).not.toBe(helpButton)
  })
})
