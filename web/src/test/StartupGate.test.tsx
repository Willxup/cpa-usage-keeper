// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { StartupGate } from '../StartupGate'
import i18n from '../i18n'
import { apiFetch } from '../lib/api'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

const startup = vi.hoisted(() => ({ getStartupStatus: vi.fn() }))
vi.mock('../lib/startup', async (importOriginal) => ({
  ...await importOriginal<typeof import('../lib/startup')>(),
  getStartupStatus: startup.getStartupStatus,
}))
vi.mock('../App', () => ({ default: () => <div data-business>Business page</div> }))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

const state = (phase: 'opening' | 'migrating' | 'failed' | 'ready', progress: { processed_count: number; total_count: number | null } | null = null) => ({
  phase, message: 'Public status', progress,
})

describe('StartupGate', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(async () => {
    vi.useFakeTimers()
    startup.getStartupStatus.mockReset()
    window.__APP_BASE_PATH__ = '/cpa'
    window.history.replaceState(null, '', '/cpa/analysis?embed=cpamc')
    await i18n.changeLanguage('en')
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    vi.useRealTimers()
    vi.unstubAllGlobals()
    delete window.__APP_BASE_PATH__
  })

  const render = async () => {
    await act(async () => root.render(<I18nextProvider i18n={i18n}><StartupGate /></I18nextProvider>))
  }

  const nextPoll = async () => {
    await act(async () => { await vi.advanceTimersByTimeAsync(2500) })
  }

  it('keeps business unmounted through opening, migration and failure, then enters ready', async () => {
    startup.getStartupStatus
      .mockResolvedValueOnce(state('opening'))
      .mockResolvedValueOnce(state('migrating', { processed_count: 7, total_count: null }))
      .mockResolvedValueOnce(state('failed'))
      .mockResolvedValueOnce(state('ready'))
    await render()
    expect(container.querySelector('[data-business]')).toBeNull()
    expect(container.textContent).toContain('Preparing Keeper')
    expect(container.querySelector('.app-frame')?.getAttribute('data-embed')).toBe('cpamc')
    expect(window.location.pathname + window.location.search).toBe('/cpa/analysis?embed=cpamc')

    await nextPoll()
    expect(container.textContent).toContain('7 records processed')
    expect(container.querySelector('[role="progressbar"]')).toBeNull()
    await nextPoll()
    expect(container.textContent).toContain('Check the service logs')
    expect(container.querySelector('[data-business]')).toBeNull()
    await nextPoll()
    expect(container.querySelector('[data-business]')).not.toBeNull()
    expect(startup.getStartupStatus).toHaveBeenCalledTimes(4)
    await nextPoll()
    expect(startup.getStartupStatus).toHaveBeenCalledTimes(4)
  })

  it('shows only a real ratio and translates the page', async () => {
    startup.getStartupStatus.mockResolvedValueOnce(state('migrating', { processed_count: 1, total_count: 4 }))
    await render()
    expect(container.textContent).toContain('1 of 4 records processed')
    expect(container.querySelector('[role="progressbar"]')?.getAttribute('aria-valuenow')).toBe('25')
    await act(async () => { await i18n.changeLanguage('zh') })
    expect(container.textContent).toContain('已处理 1 / 4 条记录')
  })

  it.each(['migrating', 'failed'] as const)('notifies the CPAMC parent while still %s', async (phase) => {
    const postMessage = vi.fn()
    const ownParent = Object.getOwnPropertyDescriptor(window, 'parent')
    Object.defineProperty(window, 'parent', { configurable: true, value: { postMessage } })
    try {
      startup.getStartupStatus.mockResolvedValueOnce(state(phase))
      await render()
      expect(container.querySelector('[data-business]')).toBeNull()
      expect(postMessage).toHaveBeenCalledExactlyOnceWith({ type: 'cpa-usage-keeper:ready' }, '*')
    } finally {
      if (ownParent) Object.defineProperty(window, 'parent', ownParent)
      else Reflect.deleteProperty(window, 'parent')
    }
  })

  it('keeps the last state during a network error and ignores older retry responses', async () => {
    const old = deferred<ReturnType<typeof state>>()
    const latest = deferred<ReturnType<typeof state>>()
    startup.getStartupStatus.mockRejectedValueOnce(new Error('offline'))
      .mockImplementationOnce(() => old.promise)
      .mockImplementationOnce(() => latest.promise)
    await render()
    expect(container.textContent).toContain('Connection interrupted')
    expect(container.querySelector('[data-business]')).toBeNull()

    await act(async () => container.querySelector<HTMLButtonElement>('[data-keeper-page="startup"] button.btn')!.click())
    await act(async () => container.querySelector<HTMLButtonElement>('[data-keeper-page="startup"] button.btn')!.click())
    await act(async () => latest.resolve(state('ready')))
    expect(container.querySelector('[data-business]')).not.toBeNull()
    await act(async () => old.resolve(state('migrating')))
    expect(container.querySelector('[data-business]')).not.toBeNull()
  })

  it('ignores a pending status response after unmount', async () => {
    const pending = deferred<ReturnType<typeof state>>()
    startup.getStartupStatus.mockImplementationOnce(() => pending.promise)
    await render()
    await act(async () => root.unmount())
    await act(async () => pending.resolve(state('ready')))
    expect(container.childElementCount).toBe(0)
    root = createRoot(container)
  })

  it('returns an existing business page to startup only for a current migration response', async () => {
    const old = deferred<Response>()
    const resumed = deferred<ReturnType<typeof state>>()
    startup.getStartupStatus.mockResolvedValueOnce(state('ready')).mockImplementationOnce(() => resumed.promise)
    await render()
    expect(container.querySelector('[data-business]')).not.toBeNull()

    const fetchMock = vi.fn((input: RequestInfo | URL) => String(input).endsWith('/old')
      ? old.promise
      : Promise.resolve(Response.json({ code: String(input).endsWith('/busy') ? 'costs_busy' : 'migration_in_progress' }, { status: 503 })))
    vi.stubGlobal('fetch', fetchMock)
    const oldRequest = apiFetch('/cpa/api/v1/old')
    await act(async () => { await apiFetch('/cpa/api/v1/busy') })
    expect(container.querySelector('[data-business]')).not.toBeNull()
    await act(async () => { await apiFetch('/cpa/api/v1/new') })
    expect(container.querySelector('[data-business]')).toBeNull()
    await act(async () => resumed.resolve(state('ready')))
    expect(container.querySelector('[data-business]')).not.toBeNull()
    await act(async () => old.resolve(Response.json({ code: 'migration_in_progress' }, { status: 503 })))
    await oldRequest
    expect(container.querySelector('[data-business]')).not.toBeNull()
  })
})
