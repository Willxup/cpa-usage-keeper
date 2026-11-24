// @vitest-environment happy-dom
import { act, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import type { CredentialListContentMode } from '../credentialListPreferences'
import { CredentialListContentModeSwitch } from '../list/CredentialListContentModeSwitch'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

describe('credential list content mode control', () => {
  let container: HTMLDivElement
  let root: Root
  beforeEach(async () => {
    await i18n.changeLanguage('en')
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })
  afterEach(async () => { await act(async () => root.unmount()); container.remove() })

  it('offers accessible icon-only choices and switches the selected content', async () => {
    const changed = vi.fn()
    function Harness() {
      const [mode, setMode] = useState<CredentialListContentMode>('quota')
      return <CredentialListContentModeSwitch mode={mode} onChange={(next) => { changed(next); setMode(next) }} />
    }
    await act(async () => root.render(<Harness />))
    const group = container.querySelector<HTMLElement>('[role="group"][aria-label="List side panel"]')!
    const quota = group.querySelector<HTMLButtonElement>('button[aria-label="Quota"]')!
    const health = group.querySelector<HTMLButtonElement>('button[aria-label="Health"]')!
    expect(group.textContent).toBe('')
    expect(group.querySelectorAll('button')).toHaveLength(2)
    expect(group.querySelectorAll('button > svg')).toHaveLength(2)
    expect(quota.getAttribute('aria-pressed')).toBe('true')
    expect(health.getAttribute('aria-pressed')).toBe('false')
    await act(async () => health.click())
    expect(changed).toHaveBeenCalledOnce()
    expect(changed).toHaveBeenCalledWith('health')
    expect(health.getAttribute('aria-pressed')).toBe('true')
    expect(quota.getAttribute('aria-pressed')).toBe('false')
  })

  it('supports focus and hover tooltips, Escape, and first-tap selection', async () => {
    const changed = vi.fn()
    await act(async () => root.render(<CredentialListContentModeSwitch mode="quota" onChange={changed} />))
    const health = container.querySelector<HTMLButtonElement>('button[aria-label="Health"]')!
    await act(async () => health.focus())
    expect(document.querySelector('[role="tooltip"]')?.textContent).toBe('Health')
    await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })))
    expect(document.querySelector('[role="tooltip"]')).toBeNull()
    await act(async () => health.blur())
    await act(async () => health.dispatchEvent(new PointerEvent('pointerover', { bubbles: true, pointerType: 'mouse' })))
    expect(document.querySelector('[role="tooltip"]')?.textContent).toBe('Health')
    await act(async () => health.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, pointerType: 'touch' })))
    await act(async () => health.focus())
    await act(async () => health.click())
    expect(changed).toHaveBeenCalledOnce()
    expect(changed).toHaveBeenCalledWith('health')
    expect(document.querySelector('[role="tooltip"]')).toBeNull()
  })
})
