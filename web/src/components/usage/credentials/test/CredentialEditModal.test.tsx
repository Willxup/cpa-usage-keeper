// @vitest-environment happy-dom
import { act, useRef, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { CredentialEditModal } from '../CredentialEditModal'
import { ApiError } from '@/lib/api'
import type { CredentialDetailSelection, CredentialEditChange } from '../credentialViewModels'

globalThis.IS_REACT_ACT_ENVIRONMENT = true
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }))
const selection = (type = 'codex'): CredentialDetailSelection => ({
  kind: type === 'codex' ? 'auth-file' : 'ai-provider',
  row: { displayName: 'Office', identity: { id: '7', identity: 'idx/one', type, alias: 'Office', priority: 5, disabled: false } },
} as CredentialDetailSelection)
const field = (key: string) => {
  const label = Array.from(document.querySelectorAll('label')).find((el) => el.textContent === `usage_stats.credentials_edit_${key}`)!
  return document.getElementById(label.htmlFor) as HTMLInputElement
}
async function change(el: HTMLInputElement, value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(el, value)
    el.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
const button = (key: string) => Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find((el) => el.textContent === `common.${key}`)!

describe('credential unified editor', () => {
  let container: HTMLDivElement
  let root: Root
  const onClose = vi.fn()
  const onSaved = vi.fn()
  beforeEach(() => {
    container = document.createElement('div'); document.body.appendChild(container); root = createRoot(container)
    onClose.mockClear(); onSaved.mockClear()
  })
  afterEach(async () => { await act(async () => root.unmount()); container.remove() })
  const render = async (onSaveField: (change: CredentialEditChange) => Promise<void>, type = 'codex') => {
    await act(async () => root.render(<CredentialEditModal selection={selection(type)} onClose={onClose} onSaved={onSaved} onSaveField={onSaveField} />))
  }

  it('opens all fields without sending requests and cancels an unsaved draft', async () => {
    const save = vi.fn(async () => undefined)
    await render(save)
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    expect(field('alias').value).toBe('Office')
    expect(field('priority').value).toBe('5')
    expect(button('save').disabled).toBe(true)
    await change(field('alias'), 'Draft')
    await act(async () => button('cancel').click())
    expect(onClose).toHaveBeenCalledTimes(1)
    expect(save).not.toHaveBeenCalled()
  })

  it('preserves the draft and retries only unsaved fields after partial success', async () => {
    let fail = true
    const save = vi.fn(async (change: CredentialEditChange) => {
      if (change.field === 'priority' && fail) { fail = false; throw new Error('upstream failed') }
    })
    await render(save)
    await change(field('alias'), 'Home')
    await change(field('priority'), '-2')
    await act(async () => document.querySelector<HTMLInputElement>('input[type="checkbox"]')!.click())
    await act(async () => button('save').click())
    expect(save.mock.calls.map(([change]) => change.field)).toEqual(['alias', 'priority'])
    expect(onSaved).not.toHaveBeenCalled()
    expect(field('alias').value).toBe('Home')
    expect(document.querySelector('[role="status"]')?.textContent).toContain('credentials_edit_partial')
    await act(async () => button('save').click())
    expect(save.mock.calls.map(([change]) => change)).toEqual([
      { field: 'alias', value: 'Home' }, { field: 'priority', value: -2 },
      { field: 'priority', value: -2 }, { field: 'disabled', value: true },
    ])
    expect(onSaved).toHaveBeenCalledTimes(1)
  })

  it('shows the general edit error when priority saving returns HTTP 502', async () => {
    const save = vi.fn(async () => { throw new ApiError('upstream failed', 502) })
    await render(save)
    await change(field('priority'), '6')
    await act(async () => button('save').click())
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('usage_stats.credentials_edit_failed')
  })

  it('rejects invalid priority before saving any field and permits an explicit zero', async () => {
    const save = vi.fn(async () => undefined)
    await render(save)
    await change(field('alias'), '')
    await change(field('priority'), '1.5')
    await act(async () => button('save').click())
    expect(save).not.toHaveBeenCalled()
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('credentials_priority_invalid')
    await change(field('priority'), '0')
    await act(async () => button('save').click())
    expect(save.mock.calls).toEqual([[{ field: 'alias', value: '' }], [{ field: 'priority', value: 0 }]])
  })

  it('keeps unsupported provider status read-only and explains OpenAI priority scope', async () => {
    const save = vi.fn(async () => undefined)
    await render(save, 'openai')
    expect(document.querySelector<HTMLInputElement>('input[type="checkbox"]')!.disabled).toBe(true)
    expect(document.body.textContent).toContain('credentials_edit_unsupported')
    expect(document.body.textContent).toContain('credentials_priority_openai_scope')
    await change(field('priority'), '7')
    await act(async () => button('save').click())
    expect(save.mock.calls).toEqual([[{ field: 'priority', value: 7 }]])
  })

  it('locks fields and closure during an outstanding save', async () => {
    let finish!: () => void
    const save = vi.fn(() => new Promise<void>((resolve) => { finish = resolve }))
    await render(save)
    await change(field('alias'), 'New')
    await act(async () => button('save').click())
    expect(field('alias').disabled).toBe(true)
    expect(button('cancel').disabled).toBe(true)
    await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })))
    expect(onClose).not.toHaveBeenCalled()
    await act(async () => finish())
    expect(onSaved).toHaveBeenCalledTimes(1)
  })
  it.each(['cancel', 'escape', 'save'])('restores focus to the opener after %s closes the modal', async (action) => {
    function Harness() {
      const [open, setOpen] = useState(false)
      return <><button onClick={() => setOpen(true)}>Edit</button>{open && <CredentialEditModal selection={selection()} onClose={() => setOpen(false)} onSaved={() => setOpen(false)} onSaveField={async () => undefined} />}</>
    }
    await act(async () => root.render(<Harness />))
    const trigger = container.querySelector<HTMLButtonElement>('button')!
    await act(async () => { trigger.focus(); trigger.click() })
    if (action === 'save') await change(field('alias'), 'Updated')
    await act(async () => {
      field('alias').focus()
      if (action === 'escape') document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      else button(action).click()
    })
    expect(document.activeElement).toBe(trigger)
  })

  it('focuses the page fallback when saving removes the original row', async () => {
    function Harness() {
      const fallbackRef = useRef<HTMLElement | null>(null)
      const [open, setOpen] = useState(false)
      const [visible, setVisible] = useState(true)
      return <main ref={fallbackRef} tabIndex={-1}>
        {visible && <button onClick={() => setOpen(true)}>Edit</button>}
        {open && <CredentialEditModal selection={selection()} fallbackFocusRef={fallbackRef} onClose={() => setOpen(false)} onSaved={() => setOpen(false)} onSaveField={async () => { setVisible(false) }} />}
      </main>
    }
    await act(async () => root.render(<Harness />))
    const trigger = container.querySelector<HTMLButtonElement>('button')!
    await act(async () => { trigger.focus(); trigger.click() })
    await change(field('alias'), 'Updated')
    await act(async () => button('save').click())
    expect(trigger.isConnected).toBe(false)
    expect(document.activeElement).toBe(container.querySelector('main'))
  })

  it('ignores IME confirmation and saves once on the following ordinary Enter', async () => {
    const save = vi.fn(async () => undefined)
    await render(save)
    const input = field('alias')
    await change(input, 'New alias')
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
    expect(save).toHaveBeenCalledWith({ field: 'alias', value: 'New alias' })
  })

  it('keeps the draft open when Escape cancels IME composition', async () => {
    const save = vi.fn(async () => undefined)
    await render(save)
    const input = field('alias')
    await act(async () => input.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true })))
    const escape = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    await act(async () => input.dispatchEvent(escape))
    expect(escape.defaultPrevented).toBe(false)
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    expect(onClose).not.toHaveBeenCalled()
    expect(save).not.toHaveBeenCalled()
    await act(async () => input.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true })))
    for (const options of [{ isComposing: true }, { keyCode: 229 }]) {
      await act(async () => input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true, ...options })))
      expect(document.querySelector('[role="dialog"]')).not.toBeNull()
      expect(onClose).not.toHaveBeenCalled()
    }
    await act(async () => input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

})
