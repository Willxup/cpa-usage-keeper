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
