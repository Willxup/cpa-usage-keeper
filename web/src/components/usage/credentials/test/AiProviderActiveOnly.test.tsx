// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AiProviderCredentialsSection } from '../AiProviderCredentialsSection'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({ t: (key: string) => key }),
}))

const dispatchPointer = async (target: Element, type: string, pointerType: string) => {
  await act(async () => {
    const event = new Event(type, { bubbles: true, cancelable: true })
    Object.defineProperty(event, 'pointerType', { value: pointerType })
    target.dispatchEvent(event)
  })
}

describe('AI Provider active-only controls', () => {
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
  })

  const renderSection = async () => {
    await act(async () => {
      root.render(
        <AiProviderCredentialsSection
          rows={[]}
          total={0}
          page={1}
          totalPages={0}
          pageSize={10}
          activeOnly={true}
          sort="priority"
          loading={false}
          onPageChange={() => undefined}
          onPageSizeChange={() => undefined}
          onActiveOnlyChange={() => undefined}
          onSortChange={() => undefined}
        />,
      )
    })
  }

  it('renders the shared switch and a permanently described compatibility help trigger', async () => {
    await renderSection()

    const checkbox = container.querySelector<HTMLInputElement>('input[type="checkbox"]')
    const button = container.querySelector<HTMLButtonElement>('[data-question-mark-help]')
    const descriptionId = button?.getAttribute('aria-describedby')
    const tooltipId = button?.getAttribute('aria-controls')

    expect(checkbox?.checked).toBe(true)
    expect(container.textContent).toContain('usage_stats.credentials_ai_providers_active_only')
    expect(button?.textContent).toBe('?')
    expect(button?.getAttribute('aria-label')).toBe('usage_stats.credentials_ai_providers_active_only_help_label')
    expect(descriptionId ? document.getElementById(descriptionId)?.textContent : null)
      .toBe('usage_stats.credentials_ai_providers_active_only_help')
    expect(tooltipId ? document.getElementById(tooltipId)?.getAttribute('role') : null).toBe('tooltip')
  })

  it('opens on focus and touch, then closes on a second touch or outside pointer', async () => {
    await renderSection()

    const button = container.querySelector<HTMLButtonElement>('[data-question-mark-help]')
    const tooltipId = button?.getAttribute('aria-controls')
    const tooltip = tooltipId ? document.getElementById(tooltipId) : null

    expect(tooltip?.getAttribute('aria-hidden')).toBe('true')
    await act(async () => button?.focus())
    expect(tooltip?.getAttribute('aria-hidden')).toBe('false')
    await act(async () => button?.blur())
    expect(tooltip?.getAttribute('aria-hidden')).toBe('true')

    await dispatchPointer(button!, 'pointerdown', 'touch')
    expect(tooltip?.getAttribute('aria-hidden')).toBe('false')
    await dispatchPointer(button!, 'pointerdown', 'touch')
    expect(tooltip?.getAttribute('aria-hidden')).toBe('true')

    await dispatchPointer(button!, 'pointerdown', 'touch')
    await dispatchPointer(document.body, 'pointerdown', 'mouse')
    expect(tooltip?.getAttribute('aria-hidden')).toBe('true')
  })
})
