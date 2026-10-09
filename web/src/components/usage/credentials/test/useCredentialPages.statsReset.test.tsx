// @vitest-environment happy-dom

import { act, useEffect } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { UsageIdentity } from '@/lib/types'
import { useCredentialPages } from '../useCredentialPages'

globalThis.IS_REACT_ACT_ENVIRONMENT = true
let latest: ReturnType<typeof useCredentialPages>
function Harness() {
  const state = useCredentialPages({ enabledAuthFiles: true, enabledAiProviders: true })
  useEffect(() => { latest = state }, [state])
  return null
}

const response = (body: unknown) => ({ ok: true, json: async () => body }) as Response
const page = (identity: UsageIdentity) => response({ identities: [identity], total_count: 1, total_pages: 1 })

describe('credential stats reset list synchronization', () => {
  let root: Root
  let container: HTMLDivElement
  beforeEach(() => {
    window.localStorage.clear()
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })
  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    vi.restoreAllMocks()
  })

  it.each([1, 2] as const)('invalidates stale list data, preserves health and refreshes ordering for type %s', async (authType) => {
    const identity = { id: String(authType), auth_type: authType, total_requests: 10, credential_health: { total_success: 8 } } as UsageIdentity
    const updated = { ...identity, credential_health: undefined, stats_reset_at: '2026-09-10T16:00:00+08:00', period_stats: { total_requests: 0, success_count: 0, failure_count: 0, total_tokens: 0, input_tokens: 0, cache_read_tokens: 0 } }
    const stale = Promise.withResolvers<Response>()
    const refresh = Promise.withResolvers<Response>()
    let listRequests = 0
    let oldSignal: AbortSignal | undefined
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = new URL(String(input), 'http://localhost')
      if (url.pathname.endsWith('/stats/reset')) {
        expect(init?.method).toBe('POST')
        return response(updated)
      }
      if (url.searchParams.get('auth_type') !== String(authType)) return response({ identities: [], total_count: 0, total_pages: 0 })
      listRequests++
      if (listRequests === 1) return page(identity)
      if (listRequests === 2) {
        oldSignal = init?.signal ?? undefined
        return stale.promise
      }
      return refresh.promise
    })
    await act(async () => root.render(<Harness />))
    let oldRefresh!: Promise<void>
    await act(async () => { oldRefresh = latest.refresh() })
    await act(async () => { await latest.resetStats(identity.id) })
    const rows = () => authType === 1 ? latest.authFileIdentities : latest.aiProviderIdentities
    expect(oldSignal?.aborted).toBe(true)
    expect(rows()[0].period_stats?.total_requests).toBe(0)
    expect(rows()[0].credential_health).toEqual(identity.credential_health)
    await act(async () => { stale.resolve(page(identity)); await oldRefresh })
    expect(rows()[0].period_stats?.total_requests).toBe(0)
    // 最新服务端分页可能因排序变化不再包含刚重置的凭证。
    await act(async () => { refresh.resolve(response({ identities: [], total_count: 1, total_pages: 1 })) })
    expect(rows()).toEqual([])
    expect(listRequests).toBe(3)
  })
})
