// @vitest-environment happy-dom

import { act, useEffect } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchUsageQuotaCache } from '@/lib/api'
import { useQuotaCache } from '../useQuotaCache'

vi.mock('@/lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/lib/api')>(),
  fetchUsageQuotaCache: vi.fn(),
}))

let latest: ReturnType<typeof useQuotaCache> | null = null
function Harness({ enabled, authIndexes = ['auth-1'] }: { enabled: boolean; authIndexes?: string[] }) {
  const result = useQuotaCache({ enabled, authIndexes })
  useEffect(() => { latest = result }, [result])
  return null
}

describe('quota cache polling', () => {
  let root: Root
  let container: HTMLDivElement

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    vi.useFakeTimers()
    vi.mocked(fetchUsageQuotaCache).mockReset().mockResolvedValue({ items: [] })
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(() => {
    act(() => root.unmount())
    container.remove()
    latest = null
    vi.useRealTimers()
  })

  it('polls every minute only while enabled and ignores fresh references to equal auth indexes', async () => {
    await act(async () => root.render(<Harness enabled={false} />))
    expect(vi.getTimerCount()).toBe(0)
    expect(fetchUsageQuotaCache).not.toHaveBeenCalled()

    await act(async () => root.render(<Harness enabled />))
    await act(async () => root.render(<Harness enabled />))
    expect(fetchUsageQuotaCache).toHaveBeenCalledOnce()
    expect(fetchUsageQuotaCache).toHaveBeenCalledWith(['auth-1'], expect.any(AbortSignal))
    await act(async () => vi.advanceTimersByTimeAsync(59_999))
    expect(fetchUsageQuotaCache).toHaveBeenCalledOnce()
    await act(async () => vi.advanceTimersByTimeAsync(1))
    expect(fetchUsageQuotaCache).toHaveBeenCalledTimes(2)

    await act(async () => root.render(<Harness enabled={false} />))
    expect(vi.getTimerCount()).toBe(0)
    await act(async () => vi.advanceTimersByTimeAsync(60_000))
    expect(fetchUsageQuotaCache).toHaveBeenCalledTimes(2)
  })

  it('drops old GET results after reset and replaces completed quota with a failed or empty cache result', async () => {
    const oldQuota = { id: 'old', quota: [] }
    let resolveOld!: (value: { items: { auth_index: string; status: 'completed'; quota: typeof oldQuota }[] }) => void
    vi.mocked(fetchUsageQuotaCache)
      .mockImplementationOnce(() => new Promise((resolve) => { resolveOld = resolve }))
      .mockResolvedValueOnce({ items: [{ auth_index: 'auth-1', status: 'failed', error: 'expired' }] })
      .mockResolvedValueOnce({ items: [] })
    await act(async () => root.render(<Harness enabled />))
    const oldSignal = vi.mocked(fetchUsageQuotaCache).mock.calls[0][1]!
    await act(async () => {
      latest!.resetQuotaCache()
      await latest!.refreshQuotaCache()
    })
    expect(oldSignal.aborted).toBe(true)
    expect(latest!.quotaResponseByAuthIndex).toEqual({})
    expect(latest!.cachedQuotaStateByAuthIndex['auth-1']?.refreshStatus).toBe('failed')
    await act(async () => resolveOld({ items: [{ auth_index: 'auth-1', status: 'completed', quota: oldQuota }] }))
    expect(latest!.quotaResponseByAuthIndex).toEqual({})

    await act(async () => latest!.refreshQuotaCache())
    expect(latest!.cachedQuotaStateByAuthIndex).toEqual({})
    await act(async () => root.render(<Harness enabled authIndexes={[]} />))
    expect(latest!.quotaResponseByAuthIndex).toEqual({})
  })

  it('removes a previously completed quota when the same auth index fails or leaves the current page', async () => {
    const oldQuota = { id: 'old', quota: [] }
    vi.mocked(fetchUsageQuotaCache)
      .mockResolvedValueOnce({ items: [{ auth_index: 'auth-1', status: 'completed', quota: oldQuota }] })
      .mockResolvedValueOnce({ items: [{ auth_index: 'auth-1', status: 'failed', error: 'expired' }] })
    await act(async () => root.render(<Harness enabled />))
    expect(latest!.quotaResponseByAuthIndex['auth-1']?.id).toBe('old')
    await act(async () => latest!.refreshQuotaCache())
    expect(latest!.quotaResponseByAuthIndex).toEqual({})
    expect(latest!.cachedQuotaStateByAuthIndex['auth-1']?.refreshStatus).toBe('failed')
    await act(async () => root.render(<Harness enabled authIndexes={[]} />))
    expect(latest!.cachedQuotaStateByAuthIndex).toEqual({})
  })
})
