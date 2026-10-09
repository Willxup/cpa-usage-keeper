// @vitest-environment happy-dom

import { act, useEffect } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import * as api from '@/lib/api'
import type { UsageQuotaInspectionStatusResponse } from '@/lib/types'
import { shouldContinueQuotaInspectionPolling, shouldNotifyQuotaInspectionCompleted, useQuotaInspection } from '../useQuotaInspection'

vi.mock('@/lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/lib/api')>(),
  fetchUsageQuotaInspectionStatus: vi.fn(),
}))

const status = (running: boolean, completed: boolean): UsageQuotaInspectionStatusResponse => ({
  running, completed, total: 0, cached: 0, normal: 0, limit_reached: 0,
  unauthorized_401: 0, payment_required_402: 0, unauthorized_401_402: 0,
  other_failed: 0, unknown: 0, results: [],
})

let latest: ReturnType<typeof useQuotaInspection> | null = null
function Harness(props: Parameters<typeof useQuotaInspection>[0]) {
  const result = useQuotaInspection(props)
  useEffect(() => { latest = result }, [result])
  return null
}

describe('useQuotaInspection', () => {
  let root: Root
  let container: HTMLDivElement

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    vi.useFakeTimers()
    vi.mocked(api.fetchUsageQuotaInspectionStatus).mockReset()
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

  it.each([
    [null, false, false],
    [status(true, false), true, false],
    [status(true, true), false, false],
    [status(false, false), false, false],
    [status(false, true), false, true],
  ] as const)('derives polling and completion from %j', (response, poll, notify) => {
    expect(shouldContinueQuotaInspectionPolling(response)).toBe(poll)
    expect(shouldNotifyQuotaInspectionCompleted(response)).toBe(notify)
  })

  it('resumes a running round once and notifies the latest callback when polling completes', async () => {
    const original = vi.fn()
    const replacement = vi.fn()
    vi.mocked(api.fetchUsageQuotaInspectionStatus)
      .mockResolvedValueOnce(status(true, false))
      .mockResolvedValueOnce(status(false, true))
    await act(async () => root.render(<Harness enabled onInspectionCompleted={original} />))
    await act(async () => root.render(<Harness enabled onInspectionCompleted={replacement} />))
    expect(api.fetchUsageQuotaInspectionStatus).toHaveBeenCalledOnce()

    await act(async () => vi.advanceTimersByTimeAsync(3_000))
    expect(api.fetchUsageQuotaInspectionStatus).toHaveBeenCalledTimes(2)
    expect(original).not.toHaveBeenCalled()
    expect(replacement).toHaveBeenCalledOnce()
    await act(async () => vi.advanceTimersByTimeAsync(3_000))
    expect(api.fetchUsageQuotaInspectionStatus).toHaveBeenCalledTimes(2)
  })

  it('uses the latest auth callback without reloading on parent rerenders', async () => {
    const original = vi.fn()
    const replacement = vi.fn()
    vi.mocked(api.fetchUsageQuotaInspectionStatus).mockResolvedValueOnce(status(false, false))
    await act(async () => root.render(<Harness enabled onAuthRequired={original} />))
    await act(async () => root.render(<Harness enabled onAuthRequired={replacement} />))
    expect(api.fetchUsageQuotaInspectionStatus).toHaveBeenCalledOnce()

    vi.mocked(api.fetchUsageQuotaInspectionStatus).mockRejectedValueOnce(new api.ApiError('unauthorized', 401))
    await act(async () => latest!.refreshQuotaInspectionStatus())
    expect(original).not.toHaveBeenCalled()
    expect(replacement).toHaveBeenCalledOnce()
  })
})
