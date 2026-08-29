// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { QuotaResetAction } from '../AuthFileCredentialsSection'
import type { UsageQuotaResetCreditsResponse } from '@/lib/types'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({
    t: (key: string, params?: Record<string, string | number>) => `${key}:${params?.index ?? ''}`,
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

describe('QuotaResetAction reset credit details', () => {
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

  const renderAction = async (
    fetchResetCredits: (authIndex: string, signal?: AbortSignal) => Promise<UsageQuotaResetCreditsResponse>,
  ) => {
    await act(async () => {
      root.render(
        <QuotaResetAction
          authIndex="codex-auth"
          resetCredits={2}
          disabled={false}
          loading={false}
          fetchResetCredits={fetchResetCredits}
          onConfirm={async () => undefined}
        />,
      )
    })
    const trigger = container.querySelector<HTMLButtonElement>('button[aria-haspopup="dialog"]')
    expect(trigger).not.toBeNull()
    return trigger as HTMLButtonElement
  }

  const openAction = async (trigger: HTMLButtonElement) => {
    await act(async () => trigger.click())
  }

  const renderOpenAction = async (
    fetchResetCredits: (authIndex: string, signal?: AbortSignal) => Promise<UsageQuotaResetCreditsResponse>,
  ) => {
    const trigger = await renderAction(fetchResetCredits)
    await openAction(trigger)
    return trigger
  }

  it('opens immediately, loads details, and gates confirmation until the request succeeds', async () => {
    const request = deferred<UsageQuotaResetCreditsResponse>()
    await renderOpenAction(() => request.promise)

    expect(container.textContent).toContain('usage_stats.credentials_quota_reset_expiry_loading')
    expect(container.querySelector<HTMLButtonElement>('button[aria-busy="false"][disabled]')?.disabled).toBe(true)

    await act(async () => {
      request.resolve({
        authIndex: 'codex-auth',
        availableCount: 2,
        credits: [
          { id: 'credit-1', status: 'available', expiresAt: '2026-07-20T00:00:00Z' },
          { id: 'credit-2', status: 'available', expiresAt: '2026-07-21T00:00:00Z' },
        ],
      })
      await request.promise
    })

    expect(container.textContent).toContain('usage_stats.credentials_quota_reset_expiry_title')
    expect(container.textContent).toContain('usage_stats.credentials_quota_reset_expiry_item:1')
    expect(container.textContent).toContain('2026-07-20 08:00:00')
    expect(Array.from(container.querySelectorAll<HTMLButtonElement>('button')).at(-1)?.disabled).toBe(false)
  })

  it('loads reset credit details only after the popover opens', async () => {
    const fetchResetCredits = vi.fn(async (_authIndex: string, _signal?: AbortSignal) => ({
      authIndex: 'codex-auth',
      availableCount: 2,
      credits: [],
    }))
    const trigger = await renderAction(fetchResetCredits)

    expect(fetchResetCredits).not.toHaveBeenCalled()

    await openAction(trigger)

    expect(fetchResetCredits).toHaveBeenCalledTimes(1)
    expect(fetchResetCredits).toHaveBeenCalledWith('codex-auth', expect.any(AbortSignal))
  })

  it('keeps confirmation disabled when the live response reports no credits', async () => {
    await renderOpenAction(async () => ({ authIndex: 'codex-auth', availableCount: 0, credits: [] }))

    expect(container.textContent).toContain('usage_stats.credentials_quota_reset_expiry_empty')
    expect(Array.from(container.querySelectorAll<HTMLButtonElement>('button')).at(-1)?.disabled).toBe(true)
  })

  it('falls back to available expiry rows when the live count is missing', async () => {
    await renderOpenAction(async () => ({
      authIndex: 'codex-auth',
      availableCount: null,
      credits: [{ id: 'credit-1', status: 'available', expiresAt: '2026-07-20T00:00:00Z' }],
    }))

    const dialog = container.querySelector<HTMLElement>('[role="dialog"]')
    expect(dialog?.textContent).toContain('1usage_stats.credentials_quota_reset_message_suffix')
    expect(dialog?.textContent).not.toContain('usage_stats.credentials_quota_reset_expiry_empty')
    expect(Array.from(container.querySelectorAll<HTMLButtonElement>('button')).at(-1)?.disabled).toBe(false)
  })

  it('falls back to the cached count when both live count and expiry rows are missing', async () => {
    await renderOpenAction(async () => ({ authIndex: 'codex-auth', availableCount: null, credits: [] }))

    const dialog = container.querySelector<HTMLElement>('[role="dialog"]')
    expect(dialog?.textContent).toContain('2usage_stats.credentials_quota_reset_message_suffix')
    expect(dialog?.textContent).toContain('usage_stats.credentials_quota_reset_expiry_failed')
    expect(dialog?.textContent).not.toContain('usage_stats.credentials_quota_reset_expiry_empty')
    expect(Array.from(container.querySelectorAll<HTMLButtonElement>('button')).at(-1)?.disabled).toBe(false)
  })

  it('allows confirmation with a warning when the count is positive but no expiry rows are returned', async () => {
    await renderOpenAction(async () => ({ authIndex: 'codex-auth', availableCount: 2, credits: [] }))

    expect(container.textContent).toContain('usage_stats.credentials_quota_reset_expiry_failed')
    expect(container.textContent).not.toContain('usage_stats.credentials_quota_reset_expiry_empty')
    expect(Array.from(container.querySelectorAll<HTMLButtonElement>('button')).at(-1)?.disabled).toBe(false)
  })

  it('shows available expiry rows with a warning when the response omits some details', async () => {
    await renderOpenAction(async () => ({
      authIndex: 'codex-auth',
      availableCount: 3,
      credits: [{ id: 'credit-1', status: 'available', expiresAt: '2026-07-20T00:00:00Z' }],
    }))

    expect(container.textContent).toContain('2026-07-20 08:00:00')
    expect(container.textContent).toContain('usage_stats.credentials_quota_reset_expiry_failed')
    expect(Array.from(container.querySelectorAll<HTMLButtonElement>('button')).at(-1)?.disabled).toBe(false)
  })

  it('shows a non-blocking warning and falls back to the cached count after lookup failure', async () => {
    const request = deferred<UsageQuotaResetCreditsResponse>()
    await renderOpenAction(() => request.promise)

    await act(async () => {
      request.reject(new Error('lookup failed'))
      try {
        await request.promise
      } catch {
        // 请求失败是本用例的预期路径。
      }
    })

    expect(container.textContent).toContain('usage_stats.credentials_quota_reset_expiry_failed')
    expect(Array.from(container.querySelectorAll<HTMLButtonElement>('button')).at(-1)?.disabled).toBe(false)
  })

  it('times out after five seconds, aborts the lookup, and allows fallback confirmation', async () => {
    vi.useFakeTimers()
    let lookupSignal: AbortSignal | undefined
    try {
      await renderOpenAction((_authIndex, signal) => {
        lookupSignal = signal
        return new Promise<UsageQuotaResetCreditsResponse>(() => undefined)
      })

      expect(lookupSignal?.aborted).toBe(false)
      expect(Array.from(container.querySelectorAll<HTMLButtonElement>('button')).at(-1)?.disabled).toBe(true)

      await act(async () => {
        await vi.advanceTimersByTimeAsync(5_000)
      })

      expect(lookupSignal?.aborted).toBe(true)
      expect(container.textContent).toContain('usage_stats.credentials_quota_reset_expiry_failed')
      expect(Array.from(container.querySelectorAll<HTMLButtonElement>('button')).at(-1)?.disabled).toBe(false)
    } finally {
      vi.useRealTimers()
    }
  })

  it('opens above the trigger and limits its height near the viewport bottom', async () => {
    vi.spyOn(window, 'innerWidth', 'get').mockReturnValue(1_000)
    vi.spyOn(window, 'innerHeight', 'get').mockReturnValue(600)
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
      x: 950,
      y: 540,
      top: 540,
      right: 980,
      bottom: 570,
      left: 950,
      width: 30,
      height: 30,
      toJSON: () => ({}),
    })

    await renderOpenAction(async () => ({ authIndex: 'codex-auth', availableCount: 2, credits: [] }))

    const dialog = container.querySelector<HTMLElement>('[role="dialog"]')
    expect(dialog?.style.top).toBe('')
    expect(dialog?.style.bottom).toBe('68px')
    expect(dialog?.style.maxHeight).toBe('360px')
  })
})
