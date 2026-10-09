// @vitest-environment happy-dom

import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { act, type ReactNode } from 'react'
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

const componentsStyles = readFileSync(resolve(process.cwd(), 'src/styles/components.scss'), 'utf8')

describe('Modal', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot>

  beforeEach(() => {
    vi.useFakeTimers()
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    document.body.style.cssText = ''
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  async function render(children: ReactNode) {
    await act(async () => root.render(children))
  }

  it('renders a right-aligned full-height drawer while preserving dialog semantics', async () => {
    await render(
      <Modal open title="Credential" variant="drawer" width={840} onClose={() => undefined}>
        Detail
      </Modal>,
    )

    const dialog = document.body.querySelector<HTMLElement>('[role="dialog"]')!
    expect(dialog.classList.contains('modal-drawer')).toBe(true)
    expect(dialog.parentElement!.classList.contains('modal-overlay-drawer')).toBe(true)
    expect(dialog.getAttribute('aria-modal')).toBe('true')
  })

  it('keeps the drawer flush with the viewport and scrolls only its body', () => {
    expect(componentsStyles).toMatch(/\.modal-overlay-drawer\s*\{[\s\S]*?align-items:\s*stretch;[\s\S]*?justify-content:\s*flex-end;[\s\S]*?padding:\s*0;/)
    expect(componentsStyles).toMatch(/\.modal-drawer\s*\{[\s\S]*?height:\s*100vh;[\s\S]*?max-height:\s*100vh;/)
    expect(componentsStyles).toMatch(/\.modal-drawer\s*\{[\s\S]*?\.modal-body\s*\{[\s\S]*?flex:\s*1 1 auto;[\s\S]*?max-height:\s*none;/)
  })

  it('lets only the topmost nested modal handle Escape', async () => {
    const closeDrawer = vi.fn()
    const closeNested = vi.fn()
    await render(
      <Modal open title="Credential" variant="drawer" onClose={closeDrawer}>
        <Modal open title="Request log" onClose={closeNested}>Log</Modal>
      </Modal>,
    )

    const nestedDialog = Array.from(document.body.querySelectorAll<HTMLElement>('[role="dialog"]'))
      .find((dialog) => dialog.querySelector('.modal-title')!.textContent === 'Request log')!
    nestedDialog.querySelector<HTMLButtonElement>('.modal-close-floating')!.focus()

    await act(async () => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    })

    expect(closeNested).toHaveBeenCalledTimes(1)
    expect(closeDrawer).not.toHaveBeenCalled()
  })

  it('blocks overlay scrolling without changing body layout and restores the content scroll', async () => {
    container.className = 'content'
    container.scrollTop = 125
    const restoreScroll = vi.spyOn(container, 'scrollTo')
    const restoreWindowScroll = vi.spyOn(window, 'scrollTo')
    const scrollY = window.scrollY
    document.body.style.cssText = 'position: relative; width: 900px; overflow: auto;'
    const bodyStyle = document.body.style.cssText
    await render(<Modal open onClose={vi.fn()}>Scrollable content</Modal>)
    expect(document.body.style.cssText).toBe(bodyStyle)

    for (const type of ['wheel', 'touchmove']) {
      const overlayEvent = new Event(type, { bubbles: true, cancelable: true })
      document.body.querySelector('.modal-overlay')!.dispatchEvent(overlayEvent)
      expect(overlayEvent.defaultPrevented).toBe(true)
      const contentEvent = new Event(type, { bubbles: true, cancelable: true })
      document.body.querySelector('.modal-body')!.dispatchEvent(contentEvent)
      expect(contentEvent.defaultPrevented).toBe(false)
    }

    container.scrollTop = 0
    await render(null)
    expect(restoreScroll).toHaveBeenCalledWith({ top: 125, left: 0, behavior: 'auto' })
    expect(restoreWindowScroll).toHaveBeenCalledWith({ top: scrollY, left: 0, behavior: 'auto' })
    expect(document.body.style.cssText).toBe(bodyStyle)
  })

  it('notifies on overlay click immediately and keeps the last content until closing finishes', async () => {
    const onClose = vi.fn()
    await render(<Modal open title="Original" width={640} onClose={onClose}>Original body</Modal>)
    const dialog = document.body.querySelector<HTMLElement>('[role="dialog"]')!
    await act(async () => { dialog.dispatchEvent(new MouseEvent('mousedown', { bubbles: true })) })
    expect(onClose).not.toHaveBeenCalled()
    await act(async () => { dialog.parentElement!.dispatchEvent(new MouseEvent('mousedown', { bubbles: true })) })
    expect(onClose).toHaveBeenCalledTimes(1)

    await render(<Modal open={false} title="Changed" width={320} onClose={onClose}>Changed body</Modal>)
    expect(dialog.textContent).toContain('Original body')
    expect(dialog.querySelector('.modal-title')!.textContent).toBe('Original')
    expect(dialog.style.width).toBe('640px')
    await act(async () => { vi.runAllTimers() })
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('disables interaction during closing and respects reduced motion', () => {
    expect(componentsStyles).toMatch(/\.modal-overlay-closing[\s\S]*?\.modal[\s\S]*?pointer-events:\s*none;/)
    expect(componentsStyles).toMatch(/@media \(prefers-reduced-motion: reduce\)[\s\S]*?\.modal-overlay-entering,[\s\S]*?\.modal-overlay-closing,[\s\S]*?\.modal-entering,[\s\S]*?\.modal-closing[\s\S]*?animation:\s*none;/)
    expect(componentsStyles).toMatch(/@media \(prefers-reduced-motion: reduce\)[\s\S]*?\.modal-overlay-closing,[\s\S]*?\.modal-closing[\s\S]*?opacity:\s*0;/)
  })
})
