// @vitest-environment happy-dom

import { act, useEffect, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, fetchUsageQuotaRefreshTask, refreshUsageQuotas } from '@/lib/api'
import type { UsageQuotaCheckResponse, UsageQuotaRefreshResponse, UsageQuotaRefreshTaskResponse } from '@/lib/types'
import { useQuotaRefreshTasks } from '../useQuotaRefreshTasks'

vi.mock('@/lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/lib/api')>(),
  fetchUsageQuotaRefreshTask: vi.fn(),
  refreshUsageQuotas: vi.fn(),
}))

let latest: ReturnType<typeof useQuotaRefreshTasks> | null = null
let quotaCache: Record<string, UsageQuotaCheckResponse> = {}
function Harness({ enabled = true, onAuthRequired }: { enabled?: boolean; onAuthRequired?: () => void }) {
  const [cache, setCache] = useState<Record<string, UsageQuotaCheckResponse>>({})
  const result = useQuotaRefreshTasks({ enabled, currentAuthIndexes: ['auth-1'], setQuotaResponseByAuthIndex: setCache, onAuthRequired })
  useEffect(() => { latest = result; quotaCache = cache }, [result, cache])
  return null
}

const submission: UsageQuotaRefreshResponse = { tasks: [{ authIndex: 'auth-1' }], rejected: [], accepted: 1, skipped: 0, limit: 1 }

describe('quota refresh task ownership', () => {
  let root: Root
  let container: HTMLDivElement

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    vi.useFakeTimers()
    vi.mocked(refreshUsageQuotas).mockReset()
    vi.mocked(fetchUsageQuotaRefreshTask).mockReset()
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(() => {
    act(() => root.unmount())
    container.remove()
    latest = null
    quotaCache = {}
    vi.useRealTimers()
  })

  it('ignores an accepted POST returned after reset and lets a new manual refresh work', async () => {
    let resolveOld!: (value: UsageQuotaRefreshResponse) => void
    vi.mocked(refreshUsageQuotas)
      .mockImplementationOnce(() => new Promise((resolve) => { resolveOld = resolve }))
      .mockResolvedValueOnce(submission)
    vi.mocked(fetchUsageQuotaRefreshTask).mockResolvedValue({ authIndex: 'auth-1', status: 'completed', quota: { id: 'new', quota: [] } })
    await act(async () => root.render(<Harness />))
    let oldSubmit!: Promise<void>
    await act(async () => { oldSubmit = latest!.refreshQuotaForCurrentAuthFilePage() })
    expect(latest!.quotaRefreshing).toBe(true)
    await act(async () => latest!.resetQuotaRefreshTasks())
    expect(latest!.quotaRefreshing).toBe(false)
    await act(async () => { resolveOld(submission); await oldSubmit })
    expect(latest!.quotaStateByAuthIndex).toEqual({})
    expect(fetchUsageQuotaRefreshTask).not.toHaveBeenCalled()

    await act(async () => latest!.refreshQuotaForAuthIndex('auth-1'))
    expect(refreshUsageQuotas).toHaveBeenCalledTimes(2)
    expect(latest!.quotaStateByAuthIndex['auth-1']?.refreshStatus).toBe('completed')
    expect(quotaCache['auth-1']?.id).toBe('new')
  })

  it('settles a disappeared 404 task without leaving queued status or reporting completion', async () => {
    vi.mocked(refreshUsageQuotas).mockResolvedValue(submission)
    vi.mocked(fetchUsageQuotaRefreshTask)
      .mockResolvedValueOnce({ authIndex: 'auth-1', status: 'completed', quota: { id: 'old', quota: [] } })
      .mockRejectedValueOnce(new ApiError('missing', 404))
    await act(async () => root.render(<Harness />))
    await act(async () => latest!.refreshQuotaForAuthIndex('auth-1'))
    expect(quotaCache['auth-1']?.id).toBe('old')
    await act(async () => latest!.refreshQuotaForAuthIndex('auth-1'))
    expect(latest!.quotaStateByAuthIndex['auth-1']).toBeUndefined()
    expect(quotaCache).toEqual({})
    await act(async () => vi.advanceTimersByTimeAsync(5_000))
    expect(fetchUsageQuotaRefreshTask).toHaveBeenCalledTimes(2)
  })

  it('discards a late polling GET after reset', async () => {
    let resolvePoll!: (value: UsageQuotaRefreshTaskResponse) => void
    vi.mocked(refreshUsageQuotas).mockResolvedValue(submission)
    vi.mocked(fetchUsageQuotaRefreshTask).mockImplementationOnce(() => new Promise((resolve) => { resolvePoll = resolve }))
    await act(async () => root.render(<Harness />))
    await act(async () => latest!.refreshQuotaForAuthIndex('auth-1'))
    const signal = vi.mocked(fetchUsageQuotaRefreshTask).mock.calls[0][1]!
    await act(async () => latest!.resetQuotaRefreshTasks())
    expect(signal.aborted).toBe(true)
    await act(async () => resolvePoll({ authIndex: 'auth-1', status: 'completed', quota: { id: 'old', quota: [] } }))
    expect(latest!.quotaStateByAuthIndex).toEqual({})
    expect(quotaCache).toEqual({})
  })

  it('does not publish a late quota POST error after the component unmounts', async () => {
    let rejectOld!: (error: Error) => void
    const onAuthRequired = vi.fn()
    vi.mocked(refreshUsageQuotas).mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectOld = reject }))
    await act(async () => root.render(<Harness onAuthRequired={onAuthRequired} />))
    let submit!: Promise<void>
    await act(async () => { submit = latest!.refreshQuotaForAuthIndex('auth-1') })
    await act(async () => root.unmount())
    await act(async () => { rejectOld(new ApiError('unauthorized', 401)); await submit })
    expect(onAuthRequired).not.toHaveBeenCalled()
    expect(fetchUsageQuotaRefreshTask).not.toHaveBeenCalled()
  })
})
