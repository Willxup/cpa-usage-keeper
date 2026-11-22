// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import { AiProviderCredentialsSection } from '../AiProviderCredentialsSection'
import { createAiProviderSectionProps } from './credentialSectionFixtures'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

describe('AI provider title help', () => {
  let container: HTMLDivElement
  let root: Root
  beforeEach(async () => {
    await i18n.changeLanguage('en')
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })
  afterEach(async () => { await act(async () => root.unmount()); container.remove() })

  it('places the help beside the title before the enabled filter and view controls', async () => {
    await act(async () => root.render(<AiProviderCredentialsSection {...createAiProviderSectionProps()} />))
    const title = container.querySelector('h3')!
    const help = container.querySelector<HTMLButtonElement>('button[aria-label="About AI provider enabled-only filtering"]')!
    const filter = container.querySelector<HTMLInputElement>('input[type="checkbox"]')!
    const view = container.querySelector('[role="group"][aria-label="View layout"]')!
    expect(title.parentElement).toBe(help.parentElement?.parentElement)
    expect(title.compareDocumentPosition(help) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(help.compareDocumentPosition(filter) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(filter.compareDocumentPosition(view) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(help.closest('label')).toBeNull()
    expect(help.getAttribute('aria-describedby')).toBeTruthy()
    const description = document.getElementById(help.getAttribute('aria-describedby')!)
    expect(description?.textContent).toContain('Enabled only hides credentials marked as disabled')
    await act(async () => help.focus())
    expect(help.getAttribute('aria-expanded')).toBe('true')
    const tooltip = document.getElementById(help.getAttribute('aria-controls')!)
    expect(tooltip?.getAttribute('aria-hidden')).toBe('false')
    expect(tooltip?.textContent).toContain('OpenAI-compatible credentials')
    await act(async () => help.blur())
    expect(help.getAttribute('aria-expanded')).toBe('false')
  })
})
