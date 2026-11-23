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
  startUsageQuotaInspection: vi.fn(),
}))

const status = (running: boolean, completed: boolean): UsageQuotaInspectionStatusResponse => ({
  running, completed, total: 0, cached: 0, normal: 0, limit_reached: 0,
  unauthorized_401: 0, payment_required_402: 0, unauthorized_401_402: 0,
  other_failed: 0, unknown: 0, results: [],
})

const deferredStatus = () => {
  let resolve!: (value: UsageQuotaInspectionStatusResponse) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<UsageQuotaInspectionStatusResponse>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}

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
    vi.mocked(api.startUsageQuotaInspection).mockReset()
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

  it('resets a pending GET and POST without publishing old completion, then reads the current status', async () => {
    let resolveOldGet!: (value: UsageQuotaInspectionStatusResponse) => void
    let resolveOldPost!: (value: UsageQuotaInspectionStatusResponse) => void
    const onCompleted = vi.fn()
    vi.mocked(api.fetchUsageQuotaInspectionStatus)
      .mockImplementationOnce(() => new Promise((resolve) => { resolveOldGet = resolve }))
      .mockResolvedValueOnce(status(false, false))
    vi.mocked(api.startUsageQuotaInspection).mockImplementationOnce(() => new Promise((resolve) => { resolveOldPost = resolve }))
    await act(async () => root.render(<Harness enabled onInspectionCompleted={onCompleted} />))
    let start!: Promise<void>
    await act(async () => { start = latest!.startQuotaInspection() })
    const signal = vi.mocked(api.fetchUsageQuotaInspectionStatus).mock.calls[0][0]!
    await act(async () => {
      latest!.resetQuotaInspection()
      await latest!.refreshQuotaInspectionStatus()
    })
    expect(signal.aborted).toBe(true)
    expect(latest!.quotaInspectionStatus).toEqual(status(false, false))
    expect(latest!.quotaInspectionStarting).toBe(false)
    await act(async () => { resolveOldGet(status(false, true)); resolveOldPost(status(false, true)); await start })
    expect(latest!.quotaInspectionStatus).toEqual(status(false, false))
    expect(onCompleted).not.toHaveBeenCalled()
  })

  it('keeps polling after a manual status read replaces an in-flight poll', async () => {
    let resolveOldPoll!: (value: UsageQuotaInspectionStatusResponse) => void
    const onCompleted = vi.fn()
    vi.mocked(api.fetchUsageQuotaInspectionStatus)
      .mockResolvedValueOnce(status(true, false))
      .mockImplementationOnce(() => new Promise((resolve) => { resolveOldPoll = resolve }))
      .mockResolvedValueOnce(status(true, false))
      .mockResolvedValueOnce(status(false, true))
    await act(async () => root.render(<Harness enabled onInspectionCompleted={onCompleted} />))
    await act(async () => vi.advanceTimersByTimeAsync(3_000))
    const oldSignal = vi.mocked(api.fetchUsageQuotaInspectionStatus).mock.calls[1][0]!
    await act(async () => latest!.refreshQuotaInspectionStatus())
    expect(oldSignal.aborted).toBe(true)
    await act(async () => resolveOldPoll(status(false, true)))
    expect(onCompleted).not.toHaveBeenCalled()
    await act(async () => vi.advanceTimersByTimeAsync(3_000))
    expect(api.fetchUsageQuotaInspectionStatus).toHaveBeenCalledTimes(4)
    expect(onCompleted).toHaveBeenCalledOnce()
  })

  it('continues polling after a manual status read fails during a running round', async () => {
    let resolveOldPoll!: (value: UsageQuotaInspectionStatusResponse) => void
    const onCompleted = vi.fn()
    vi.mocked(api.fetchUsageQuotaInspectionStatus)
      .mockResolvedValueOnce(status(true, false))
      .mockImplementationOnce(() => new Promise((resolve) => { resolveOldPoll = resolve }))
      .mockRejectedValueOnce(new Error('temporary outage'))
      .mockResolvedValueOnce(status(false, true))
    await act(async () => root.render(<Harness enabled onInspectionCompleted={onCompleted} />))
    await act(async () => vi.advanceTimersByTimeAsync(3_000))
    await act(async () => latest!.refreshQuotaInspectionStatus())
    expect(latest!.quotaInspectionError).toBe('temporary outage')
    await act(async () => resolveOldPoll(status(false, true)))
    expect(onCompleted).not.toHaveBeenCalled()
    await act(async () => vi.advanceTimersByTimeAsync(3_000))
    expect(api.fetchUsageQuotaInspectionStatus).toHaveBeenCalledTimes(4)
    expect(onCompleted).toHaveBeenCalledOnce()
  })

  it('serializes overlapping manual refreshes without letting a poll replace either snapshot', async () => {
    const first = deferredStatus(), second = deferredStatus()
    const firstResult = { ...status(true, false), cached: 1 }
    const secondResult = { ...status(true, false), cached: 2 }
    const onCompleted = vi.fn()
    vi.mocked(api.fetchUsageQuotaInspectionStatus)
      .mockResolvedValueOnce(status(true, false))
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise)
      .mockResolvedValueOnce(status(false, true))
    await act(async () => root.render(<Harness enabled onInspectionCompleted={onCompleted} />))
    let firstRefresh!: Promise<void>, secondRefresh!: Promise<void>
    await act(async () => {
      firstRefresh = latest!.refreshQuotaInspectionStatus()
      secondRefresh = latest!.refreshQuotaInspectionStatus()
    })
    const firstSignal = vi.mocked(api.fetchUsageQuotaInspectionStatus).mock.calls[1][0]!
    await act(async () => vi.advanceTimersByTimeAsync(6_000))
    expect(api.fetchUsageQuotaInspectionStatus).toHaveBeenCalledTimes(2)
    expect(firstSignal.aborted).toBe(false)
    await act(async () => { first.resolve(firstResult); await firstRefresh })
    expect(latest!.quotaInspectionStatus).toEqual(firstResult)
    expect(api.fetchUsageQuotaInspectionStatus).toHaveBeenCalledTimes(3)
    const secondSignal = vi.mocked(api.fetchUsageQuotaInspectionStatus).mock.calls[2][0]!
    await act(async () => vi.advanceTimersByTimeAsync(3_000))
    expect(api.fetchUsageQuotaInspectionStatus).toHaveBeenCalledTimes(3)
    expect(secondSignal.aborted).toBe(false)
    await act(async () => { second.resolve(secondResult); await secondRefresh })
    expect(latest!.quotaInspectionStatus).toEqual(secondResult)
    await act(async () => vi.advanceTimersByTimeAsync(3_000))
    expect(api.fetchUsageQuotaInspectionStatus).toHaveBeenCalledTimes(4)
    expect(onCompleted).toHaveBeenCalledOnce()
  })

  it.each(['error', 'unauthorized'] as const)('recovers polling after a slow manual %s', async (kind) => {
    const slow = deferredStatus()
    const onAuthRequired = vi.fn(), onCompleted = vi.fn()
    vi.mocked(api.fetchUsageQuotaInspectionStatus)
      .mockResolvedValueOnce(status(true, false))
      .mockReturnValueOnce(slow.promise)
      .mockResolvedValueOnce(status(false, true))
    await act(async () => root.render(<Harness enabled onAuthRequired={onAuthRequired} onInspectionCompleted={onCompleted} />))
    let refresh!: Promise<void>
    await act(async () => { refresh = latest!.refreshQuotaInspectionStatus() })
    await act(async () => vi.advanceTimersByTimeAsync(3_000))
    expect(vi.mocked(api.fetchUsageQuotaInspectionStatus).mock.calls[1][0]!.aborted).toBe(false)
    expect(api.fetchUsageQuotaInspectionStatus).toHaveBeenCalledTimes(2)
    await act(async () => {
      slow.reject(kind === 'error' ? new Error('temporary outage') : new api.ApiError('unauthorized', 401))
      await refresh
    })
    expect(latest!.quotaInspectionError).toBe(kind === 'error' ? 'temporary outage' : '')
    expect(onAuthRequired).toHaveBeenCalledTimes(kind === 'unauthorized' ? 1 : 0)
    await act(async () => vi.advanceTimersByTimeAsync(3_000))
    expect(api.fetchUsageQuotaInspectionStatus).toHaveBeenCalledTimes(3)
    expect(onCompleted).toHaveBeenCalledOnce()
    expect(latest!.quotaInspectionError).toBe('')
  })

  it.each(['reset', 'disable', 'unmount'] as const)('cancels manual and queued refreshes on %s without publishing old results', async (action) => {
    const onAuthRequired = vi.fn(), onCompleted = vi.fn()
    vi.mocked(api.fetchUsageQuotaInspectionStatus)
      .mockResolvedValueOnce(status(true, false))
      .mockImplementationOnce((signal) => new Promise((_, reject) => {
        signal?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
      }))
      .mockResolvedValueOnce(status(false, false))
    await act(async () => root.render(<Harness enabled onAuthRequired={onAuthRequired} onInspectionCompleted={onCompleted} />))
    let first!: Promise<void>, queued!: Promise<void>
    await act(async () => { first = latest!.refreshQuotaInspectionStatus(); queued = latest!.refreshQuotaInspectionStatus() })
    const signal = vi.mocked(api.fetchUsageQuotaInspectionStatus).mock.calls[1][0]!
    await act(async () => {
      if (action === 'reset') latest!.resetQuotaInspection()
      else if (action === 'disable') root.render(<Harness enabled={false} onAuthRequired={onAuthRequired} onInspectionCompleted={onCompleted} />)
      else root.unmount()
    })
    await act(async () => { await Promise.all([first, queued]) })
    expect(signal.aborted).toBe(true)
    expect(api.fetchUsageQuotaInspectionStatus).toHaveBeenCalledTimes(2)
    expect(onAuthRequired).not.toHaveBeenCalled()
    expect(onCompleted).not.toHaveBeenCalled()
    if (action === 'reset') {
      await act(async () => latest!.refreshQuotaInspectionStatus())
      expect(latest!.quotaInspectionStatus).toEqual(status(false, false))
    }
    await act(async () => vi.advanceTimersByTimeAsync(6_000))
    expect(api.fetchUsageQuotaInspectionStatus).toHaveBeenCalledTimes(action === 'reset' ? 3 : 2)
  })

  it('does not retire manual reads on either side of a new inspection POST', async () => {
    const before = deferredStatus(), during = deferredStatus(), post = deferredStatus()
    const onCompleted = vi.fn()
    vi.mocked(api.fetchUsageQuotaInspectionStatus)
      .mockResolvedValueOnce(status(true, false))
      .mockReturnValueOnce(before.promise)
      .mockReturnValueOnce(during.promise)
    vi.mocked(api.startUsageQuotaInspection).mockReturnValueOnce(post.promise)
    await act(async () => root.render(<Harness enabled onInspectionCompleted={onCompleted} />))
    let first!: Promise<void>, start!: Promise<void>, second!: Promise<void>
    await act(async () => { first = latest!.refreshQuotaInspectionStatus(); start = latest!.startQuotaInspection() })
    const firstSignal = vi.mocked(api.fetchUsageQuotaInspectionStatus).mock.calls[1][0]!
    expect(latest!.quotaInspectionStarting).toBe(true)
    expect(api.startUsageQuotaInspection).not.toHaveBeenCalled()
    await act(async () => { before.resolve(status(true, false)); await first })
    expect(firstSignal.aborted).toBe(false)
    expect(api.startUsageQuotaInspection).toHaveBeenCalledOnce()
    await act(async () => { second = latest!.refreshQuotaInspectionStatus() })
    const secondSignal = vi.mocked(api.fetchUsageQuotaInspectionStatus).mock.calls[2][0]!
    await act(async () => post.resolve(status(false, true)))
    expect(secondSignal.aborted).toBe(false)
    expect(onCompleted).not.toHaveBeenCalled()
    await act(async () => { during.resolve(status(true, false)); await second; await start })
    expect(latest!.quotaInspectionStatus).toEqual(status(false, true))
    expect(latest!.quotaInspectionStarting).toBe(false)
    expect(onCompleted).toHaveBeenCalledOnce()
  })

  it('retires the prior status GET before a new inspection POST publishes its round', async () => {
    let resolveOldGet!: (value: UsageQuotaInspectionStatusResponse) => void
    const onCompleted = vi.fn()
    vi.mocked(api.fetchUsageQuotaInspectionStatus)
      .mockImplementationOnce(() => new Promise((resolve) => { resolveOldGet = resolve }))
      .mockResolvedValueOnce(status(false, true))
    vi.mocked(api.startUsageQuotaInspection).mockResolvedValueOnce(status(true, false))
    await act(async () => root.render(<Harness enabled onInspectionCompleted={onCompleted} />))
    const oldSignal = vi.mocked(api.fetchUsageQuotaInspectionStatus).mock.calls[0][0]!
    await act(async () => latest!.startQuotaInspection())
    expect(oldSignal.aborted).toBe(true)
    await act(async () => resolveOldGet(status(false, true)))
    expect(latest!.quotaInspectionStatus).toEqual(status(true, false))
    expect(onCompleted).not.toHaveBeenCalled()
    await act(async () => vi.advanceTimersByTimeAsync(3_000))
    expect(api.fetchUsageQuotaInspectionStatus).toHaveBeenCalledTimes(2)
    expect(onCompleted).toHaveBeenCalledOnce()
  })

  it('does not poll the old round while a replacement inspection POST is pending', async () => {
    let resolveOldPoll!: (value: UsageQuotaInspectionStatusResponse) => void
    let resolveStart!: (value: UsageQuotaInspectionStatusResponse) => void
    const onCompleted = vi.fn()
    vi.mocked(api.fetchUsageQuotaInspectionStatus)
      .mockResolvedValueOnce(status(true, false))
      .mockImplementationOnce(() => new Promise((resolve) => { resolveOldPoll = resolve }))
      .mockResolvedValueOnce(status(false, true))
    vi.mocked(api.startUsageQuotaInspection).mockImplementationOnce(() => new Promise((resolve) => { resolveStart = resolve }))
    await act(async () => root.render(<Harness enabled onInspectionCompleted={onCompleted} />))
    await act(async () => vi.advanceTimersByTimeAsync(3_000))
    let start!: Promise<void>
    await act(async () => { start = latest!.startQuotaInspection() })
    await act(async () => vi.advanceTimersByTimeAsync(3_000))
    expect(api.fetchUsageQuotaInspectionStatus).toHaveBeenCalledTimes(2)
    await act(async () => resolveOldPoll(status(false, true)))
    expect(onCompleted).not.toHaveBeenCalled()
    await act(async () => { resolveStart(status(true, false)); await start })
    await act(async () => vi.advanceTimersByTimeAsync(3_000))
    expect(api.fetchUsageQuotaInspectionStatus).toHaveBeenCalledTimes(3)
    expect(onCompleted).toHaveBeenCalledOnce()
  })

  it('notifies completion once and stops polling when a manual refresh observes completion during a running round', async () => {
    const manual = deferredStatus()
    const onCompleted = vi.fn()
    vi.mocked(api.fetchUsageQuotaInspectionStatus)
      .mockResolvedValueOnce(status(true, false))
      .mockReturnValueOnce(manual.promise)
    await act(async () => root.render(<Harness enabled onInspectionCompleted={onCompleted} />))
    let refresh!: Promise<void>
    await act(async () => { refresh = latest!.refreshQuotaInspectionStatus() })
    await act(async () => vi.advanceTimersByTimeAsync(3_000))
    expect(api.fetchUsageQuotaInspectionStatus).toHaveBeenCalledTimes(2)
    await act(async () => { manual.resolve(status(false, true)); await refresh })
    expect(latest!.quotaInspectionStatus).toEqual(status(false, true))
    expect(onCompleted).toHaveBeenCalledOnce()
    await act(async () => vi.advanceTimersByTimeAsync(6_000))
    expect(api.fetchUsageQuotaInspectionStatus).toHaveBeenCalledTimes(2)
  })

  it('does not notify completion repeatedly for static completed refreshes or overlapping manual refreshes', async () => {
    const first = deferredStatus(), second = deferredStatus()
    const onCompleted = vi.fn()
    vi.mocked(api.fetchUsageQuotaInspectionStatus)
      .mockResolvedValueOnce(status(true, false))
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise)
      .mockResolvedValueOnce(status(false, true))
    await act(async () => root.render(<Harness enabled onInspectionCompleted={onCompleted} />))
    let firstRefresh!: Promise<void>, secondRefresh!: Promise<void>
    await act(async () => {
      firstRefresh = latest!.refreshQuotaInspectionStatus()
      secondRefresh = latest!.refreshQuotaInspectionStatus()
    })
    await act(async () => { first.resolve(status(false, true)); await firstRefresh })
    expect(onCompleted).toHaveBeenCalledOnce()
    await act(async () => { second.resolve(status(false, true)); await secondRefresh })
    expect(onCompleted).toHaveBeenCalledOnce()
    await act(async () => latest!.refreshQuotaInspectionStatus())
    expect(onCompleted).toHaveBeenCalledOnce()
    await act(async () => vi.advanceTimersByTimeAsync(6_000))
    expect(api.fetchUsageQuotaInspectionStatus).toHaveBeenCalledTimes(4)
  })

  it('does not notify completion when refreshing an already-completed inspection snapshot', async () => {
    const onCompleted = vi.fn()
    vi.mocked(api.fetchUsageQuotaInspectionStatus)
      .mockResolvedValueOnce(status(false, true))
      .mockResolvedValueOnce(status(false, true))
    await act(async () => root.render(<Harness enabled onInspectionCompleted={onCompleted} />))
    await act(async () => latest!.refreshQuotaInspectionStatus())
    expect(onCompleted).not.toHaveBeenCalled()
  })

  it('does not publish a late inspection POST after the component unmounts', async () => {
    let resolveOldPost!: (value: UsageQuotaInspectionStatusResponse) => void
    const onCompleted = vi.fn()
    vi.mocked(api.fetchUsageQuotaInspectionStatus).mockResolvedValueOnce(status(false, false))
    vi.mocked(api.startUsageQuotaInspection).mockImplementationOnce(() => new Promise((resolve) => { resolveOldPost = resolve }))
    await act(async () => root.render(<Harness enabled onInspectionCompleted={onCompleted} />))
    let start!: Promise<void>
    await act(async () => { start = latest!.startQuotaInspection() })
    await act(async () => root.unmount())
    await act(async () => { resolveOldPost(status(false, true)); await start })
    expect(onCompleted).not.toHaveBeenCalled()
  })
})
