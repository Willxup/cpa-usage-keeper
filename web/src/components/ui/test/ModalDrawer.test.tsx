// @vitest-environment happy-dom

import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Modal } from '../Modal'

vi.mock('react-i18next', () => {
  const t = (key: string) => key
  return {
    initReactI18next: { type: '3rdParty', init: () => undefined },
    useTranslation: () => ({ t }),
  }
})

const componentsStyles = readFileSync(resolve(process.cwd(), 'src/styles/components.scss'), 'utf8').replace(/\r\n/g, '\n')

describe('Modal drawer variant', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot>

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    document.body.innerHTML = ''
  })

  it('renders a right-aligned full-height drawer while preserving dialog semantics', async () => {
    await act(async () => {
      root.render(
        <Modal open title="Credential" variant="drawer" width={840} onClose={() => undefined}>
          Detail
        </Modal>,
      )
      await Promise.resolve()
    })

    const dialog = document.body.querySelector<HTMLElement>('[role="dialog"]')
    expect(dialog).not.toBeNull()
    expect(dialog?.classList.contains('modal-drawer')).toBe(true)
    expect(dialog?.parentElement?.classList.contains('modal-overlay-drawer')).toBe(true)
    expect(dialog?.getAttribute('aria-modal')).toBe('true')
  })

  it('keeps the drawer flush with the viewport and scrolls only its body', () => {
    expect(componentsStyles).toMatch(/\.modal-overlay-drawer\s*\{[\s\S]*?align-items:\s*stretch;[\s\S]*?justify-content:\s*flex-end;[\s\S]*?padding:\s*0;/)
    expect(componentsStyles).toMatch(/\.modal-drawer\s*\{[\s\S]*?height:\s*100vh;[\s\S]*?max-height:\s*100vh;/)
    expect(componentsStyles).toMatch(/\.modal-drawer\s*\{[\s\S]*?\.modal-body\s*\{[\s\S]*?flex:\s*1 1 auto;[\s\S]*?max-height:\s*none;/)
  })

  it('lets only the topmost nested modal handle Escape', async () => {
    const closeDrawer = vi.fn()
    const closeNested = vi.fn()
    await act(async () => {
      root.render(
        <Modal open title="Credential" variant="drawer" onClose={closeDrawer}>
          <Modal open title="Request log" onClose={closeNested}>Log</Modal>
        </Modal>,
      )
      await Promise.resolve()
    })

    const nestedDialog = Array.from(document.body.querySelectorAll<HTMLElement>('[role="dialog"]'))
      .find((dialog) => dialog.querySelector('.modal-title')?.textContent === 'Request log')
    nestedDialog?.querySelector<HTMLButtonElement>('.modal-close-floating')?.focus()

    await act(async () => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    })

    expect(closeNested).toHaveBeenCalledTimes(1)
    expect(closeDrawer).not.toHaveBeenCalled()
  })
})
