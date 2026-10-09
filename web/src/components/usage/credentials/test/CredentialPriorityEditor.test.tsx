// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { CredentialPriorityEditor, parseCredentialPriority } from '../CredentialPriorityEditor'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }))

function changeInput(input: HTMLInputElement, value: string) {
  const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!
  setValue.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}

const popover = () => document.querySelector<HTMLDivElement>('[role="dialog"]')

describe('CredentialPriorityEditor', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(() => {
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    vi.restoreAllMocks()
  })

  it('accepts signed and zero safe integers and rejects fractions and unsafe values', () => {
    expect(parseCredentialPriority('0')).toBe(0)
    expect(parseCredentialPriority('-0')).toBe(0)
    expect(parseCredentialPriority('+10')).toBe(10)
    expect(parseCredentialPriority('0010')).toBe(10)
    expect(parseCredentialPriority('-12')).toBe(-12)
    expect(parseCredentialPriority('9007199254740991')).toBe(Number.MAX_SAFE_INTEGER)
    for (const input of ['', '1.2', '1e3', '9007199254740992', '--2']) expect(parseCredentialPriority(input)).toBeNull()
  })

  it('offers P0 when priority is absent, rejects invalid input, then saves a negative value with loading feedback', async () => {
    let resolveSave!: () => void
    const onSave = vi.fn(() => new Promise<void>((resolve) => { resolveSave = resolve }))
    await act(async () => root.render(<CredentialPriorityEditor displayName="Auth" onSave={onSave} />))
    const trigger = container.querySelector<HTMLButtonElement>('button[aria-label="usage_stats.credentials_priority_edit"]')!
    expect(trigger.textContent).toBe('P0')
    await act(async () => trigger.click())
    expect(container.querySelector('[role="dialog"]')).toBeNull()
    const input = popover()!.querySelector<HTMLInputElement>('input')!
    expect(document.activeElement).toBe(input)
    expect(input.getAttribute('aria-describedby')).toContain(popover()!.querySelector('span')!.id)
    await act(async () => changeInput(input, '1.5'))
    await act(async () => input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })))
    expect(onSave).not.toHaveBeenCalled()
    expect(popover()!.querySelector('[role="alert"]')?.textContent).toBe('usage_stats.credentials_priority_invalid')

    await act(async () => changeInput(input, '-12'))
    await act(async () => input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })))
    expect(onSave).toHaveBeenCalledWith(-12)
    expect(popover()!.querySelector<HTMLInputElement>('input')?.disabled).toBe(true)
    expect(popover()?.getAttribute('aria-busy')).toBe('true')
    await act(async () => { resolveSave(); await Promise.resolve() })
    expect(popover()).toBeNull()
    expect(document.activeElement).toBe(container.querySelector('button[aria-label="usage_stats.credentials_priority_edit"]'))
  })

  it('keeps the draft and error after a failed save, and Escape restores badge focus', async () => {
    const onSave = vi.fn(async () => { throw new Error('failed') })
    await act(async () => root.render(<CredentialPriorityEditor priority={3} displayName="Provider" onSave={onSave} openAIShared />))
    await act(async () => container.querySelector<HTMLButtonElement>('button')!.click())
    const input = popover()!.querySelector<HTMLInputElement>('input')!
    expect(popover()!.textContent).toContain('usage_stats.credentials_priority_openai_scope')
    expect(popover()!.textContent).toContain('usage_stats.credentials_priority_order_hint')
    await act(async () => changeInput(input, '4'))
    await act(async () => { input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })); await Promise.resolve() })
    expect(input.value).toBe('4')
    expect(popover()!.querySelector('[role="alert"]')?.textContent).toBe('usage_stats.credentials_priority_save_failed')
    await act(async () => input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })))
    expect(popover()).toBeNull()
    expect(document.activeElement).toBe(container.querySelector('button'))
  })

  it('sends an explicit zero even when the badge shows the effective default P0', async () => {
    const onSave = vi.fn(async () => undefined)
    await act(async () => root.render(<CredentialPriorityEditor displayName="Auth" onSave={onSave} />))
    await act(async () => container.querySelector<HTMLButtonElement>('button')!.click())
    await act(async () => { popover()!.querySelector<HTMLButtonElement>('button[aria-label="usage_stats.credentials_priority_save"]')!.click(); await Promise.resolve() })
    expect(onSave).toHaveBeenCalledWith(0)
  })

  it('closes on an outside pointer action but stays open while saving', async () => {
    let resolveSave!: () => void
    const onSave = vi.fn(() => new Promise<void>((resolve) => { resolveSave = resolve }))
    const outside = document.createElement('button')
    document.body.appendChild(outside)
    try {
      await act(async () => root.render(<CredentialPriorityEditor priority={1} displayName="Auth" onSave={onSave} />))
      const trigger = container.querySelector<HTMLButtonElement>('button')!
      await act(async () => trigger.click())
      await act(async () => outside.dispatchEvent(new Event('pointerdown', { bubbles: true })))
      expect(popover()).toBeNull()
      await act(async () => trigger.click())
      await act(async () => changeInput(popover()!.querySelector('input')!, '2'))
      await act(async () => popover()!.querySelector<HTMLButtonElement>('button[aria-label="usage_stats.credentials_priority_save"]')!.click())
      expect(popover()?.getAttribute('aria-busy')).toBe('true')
      await act(async () => {
        outside.dispatchEvent(new Event('pointerdown', { bubbles: true }))
        document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      })
      expect(popover()).not.toBeNull()
      await act(async () => { resolveSave(); await Promise.resolve() })
      expect(popover()).toBeNull()
      expect(document.activeElement).toBe(trigger)
    } finally {
      outside.remove()
    }
  })

  it('clamps the floating editor inside a narrow viewport and flips above the badge', async () => {
    await act(async () => root.render(<CredentialPriorityEditor priority={1} displayName="Auth" onSave={async () => undefined} />))
    const trigger = container.querySelector<HTMLButtonElement>('button')!
    await act(async () => trigger.click())
    vi.spyOn(window, 'innerWidth', 'get').mockReturnValue(240)
    vi.spyOn(window, 'innerHeight', 'get').mockReturnValue(200)
    vi.spyOn(trigger, 'getBoundingClientRect').mockReturnValue({ left: 220, right: 245, top: 180, bottom: 190, width: 25, height: 10, x: 220, y: 180, toJSON: () => undefined })
    vi.spyOn(popover()!, 'getBoundingClientRect').mockReturnValue({ left: 0, right: 224, top: 0, bottom: 110, width: 224, height: 110, x: 0, y: 0, toJSON: () => undefined })
    await act(async () => window.dispatchEvent(new Event('resize')))
    expect(popover()!.style.left).toBe('8px')
    expect(popover()!.style.top).toBe('64px')
  })
  it('ignores IME confirmation and saves once on the following ordinary Enter', async () => {
    const save = vi.fn(async () => undefined)
    await act(async () => root.render(<CredentialPriorityEditor priority={1} displayName="Auth" onSave={save} />))
    await act(async () => container.querySelector<HTMLButtonElement>('button')!.click())
    const input = popover()!.querySelector<HTMLInputElement>('input')!
    await act(async () => changeInput(input, '8'))
    const key = async (options: KeyboardEventInit = {}) => {
      await act(async () => input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true, ...options })))
    }
    await key({ isComposing: true })
    expect(save).not.toHaveBeenCalled()
    await act(async () => input.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true })))
    await key()
    expect(save).not.toHaveBeenCalled()
    await act(async () => input.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true })))
    await key({ keyCode: 229 })
    expect(save).not.toHaveBeenCalled()
    await key()
    expect(save).toHaveBeenCalledTimes(1)
    expect(save).toHaveBeenCalledWith(8)
  })

  it('keeps the draft open when Escape cancels IME composition', async () => {
    const save = vi.fn(async () => undefined)
    await act(async () => root.render(<CredentialPriorityEditor priority={1} displayName="Auth" onSave={save} />))
    await act(async () => container.querySelector<HTMLButtonElement>('button')!.click())
    const input = popover()!.querySelector<HTMLInputElement>('input')!
    await act(async () => input.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true })))
    const escape = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    await act(async () => input.dispatchEvent(escape))
    expect(escape.defaultPrevented).toBe(false)
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()

    expect(save).not.toHaveBeenCalled()
    await act(async () => input.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true })))
    for (const options of [{ isComposing: true }, { keyCode: 229 }]) {
      await act(async () => input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true, ...options })))
      expect(document.querySelector('[role="dialog"]')).not.toBeNull()

    }
    await act(async () => input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })))
    expect(popover()).toBeNull()
  })

})
