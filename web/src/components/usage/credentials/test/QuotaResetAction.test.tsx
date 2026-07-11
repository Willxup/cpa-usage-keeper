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
    await act(async () => trigger?.click())
  }

  it('opens immediately, loads details, and gates confirmation until the request succeeds', async () => {
    const request = deferred<UsageQuotaResetCreditsResponse>()
    await renderAction(() => request.promise)

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

  it('keeps confirmation disabled when the live response reports no credits', async () => {
    await renderAction(async () => ({ authIndex: 'codex-auth', availableCount: 0, credits: [] }))

    expect(container.textContent).toContain('usage_stats.credentials_quota_reset_expiry_empty')
    expect(Array.from(container.querySelectorAll<HTMLButtonElement>('button')).at(-1)?.disabled).toBe(true)
  })

  it('allows confirmation with a warning when the count is positive but no expiry rows are returned', async () => {
    await renderAction(async () => ({ authIndex: 'codex-auth', availableCount: 2, credits: [] }))

    expect(container.textContent).toContain('usage_stats.credentials_quota_reset_expiry_failed')
    expect(container.textContent).not.toContain('usage_stats.credentials_quota_reset_expiry_empty')
    expect(Array.from(container.querySelectorAll<HTMLButtonElement>('button')).at(-1)?.disabled).toBe(false)
  })

  it('shows available expiry rows with a warning when the response omits some details', async () => {
    await renderAction(async () => ({
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
    await renderAction(() => request.promise)

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
      await renderAction((_authIndex, signal) => {
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
})
