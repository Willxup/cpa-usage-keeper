// @vitest-environment happy-dom

import { act, useEffect } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import * as api from '@/lib/api'
import type { PricingRecalculationTask } from '@/lib/types'
import { usePricingRecalculation } from '../usePricingRecalculation'

vi.mock('@/lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/lib/api')>(),
  fetchCurrentPricingRecalculation: vi.fn(),
  startPricingRecalculation: vi.fn(),
}))

const running: PricingRecalculationTask = {
  task_id: 'one', status: 'running', stage: 'events',
  start_at: '2026-09-22T10:00:00+08:00', end_at: '2026-09-23T10:15:00+08:00',
  config_revision: 7, processed_count: 1, total_count: 10,
  updated_at: '2026-09-23T10:15:00+08:00', error: null,
}
const completed: PricingRecalculationTask = { ...running, status: 'completed', stage: 'finalizing', processed_count: 10 }

let latest: ReturnType<typeof usePricingRecalculation> | null = null
function Harness({ enabled = true, onAuthRequired, onSettled }: {
  enabled?: boolean
  onAuthRequired?: () => void
  onSettled?: (task: PricingRecalculationTask) => void
}) {
  const result = usePricingRecalculation({ enabled, onAuthRequired, onSettled })
  useEffect(() => { latest = result }, [result])
  return null
}

describe('usePricingRecalculation', () => {
  let root: Root
  let container: HTMLDivElement

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    vi.useFakeTimers()
    vi.resetAllMocks()
    vi.mocked(api.fetchCurrentPricingRecalculation).mockResolvedValue(null)
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

  it('loads current on entry and polls one request at a time while running', async () => {
    const onSettled = vi.fn()
    vi.mocked(api.fetchCurrentPricingRecalculation).mockResolvedValueOnce(running)
    await act(async () => root.render(<Harness onSettled={onSettled} />))
    expect(latest!.task).toEqual(running)
    expect(api.fetchCurrentPricingRecalculation).toHaveBeenCalledTimes(1)

    const pending = Promise.withResolvers<PricingRecalculationTask | null>()
    vi.mocked(api.fetchCurrentPricingRecalculation).mockReturnValueOnce(pending.promise)
    await act(async () => { vi.advanceTimersByTime(2000) })
    expect(api.fetchCurrentPricingRecalculation).toHaveBeenCalledTimes(2)
    await act(async () => { vi.advanceTimersByTime(10_000) })
    expect(api.fetchCurrentPricingRecalculation).toHaveBeenCalledTimes(2)
    await act(async () => { pending.resolve(completed) })
    expect(latest!.task).toEqual(completed)
    expect(onSettled).toHaveBeenCalledOnce()
    await act(async () => { vi.advanceTimersByTime(10_000) })
    expect(api.fetchCurrentPricingRecalculation).toHaveBeenCalledTimes(2)
  })

  it('synchronizes double-clicks and adopts current state after an unknown POST result without retrying POST', async () => {
    await act(async () => root.render(<Harness />))
    const pending = Promise.withResolvers<Awaited<ReturnType<typeof api.startPricingRecalculation>>>()
    vi.mocked(api.startPricingRecalculation).mockReturnValueOnce(pending.promise)
    let first!: Promise<Awaited<ReturnType<NonNullable<typeof latest>['start']>>>
    let second!: Promise<Awaited<ReturnType<NonNullable<typeof latest>['start']>>>
    act(() => {
      first = latest!.start(running.start_at, 7)
      second = latest!.start(running.start_at, 7)
    })
    expect(api.startPricingRecalculation).toHaveBeenCalledOnce()
    await act(async () => { pending.resolve({ started: true, task: running }) })
    await expect(first).resolves.toEqual({ started: true, task: running })
    await expect(second).resolves.toBeNull()
    expect(latest!.task).toEqual(running)

    vi.mocked(api.startPricingRecalculation).mockRejectedValueOnce(new TypeError('connection dropped'))
    const newCompleted = { ...completed, task_id: 'new' }
    vi.mocked(api.fetchCurrentPricingRecalculation).mockResolvedValueOnce(newCompleted)
    await act(async () => expect(latest!.start(running.start_at, 7)).resolves.toEqual({ started: false, task: newCompleted }))
    expect(api.startPricingRecalculation).toHaveBeenCalledTimes(2)
    expect(latest!.task).toEqual(newCompleted)
    expect(latest!.error).toBe('')
  })

  it('does not infer a successful POST from an old completed task or a failed current read', async () => {
    vi.mocked(api.fetchCurrentPricingRecalculation).mockResolvedValueOnce(completed)
    await act(async () => root.render(<Harness />))
    vi.mocked(api.startPricingRecalculation).mockRejectedValueOnce(new TypeError('post connection dropped'))
    vi.mocked(api.fetchCurrentPricingRecalculation).mockResolvedValueOnce(completed)
    await act(async () => expect(latest!.start(running.start_at, 7)).resolves.toBeNull())
    expect(latest!.task).toEqual(completed)
    expect(latest!.error).toBe('post connection dropped')

    vi.mocked(api.startPricingRecalculation).mockRejectedValueOnce(new TypeError('post offline'))
    vi.mocked(api.fetchCurrentPricingRecalculation).mockRejectedValueOnce(new TypeError('get offline'))
    await act(async () => expect(latest!.start(running.start_at, 7)).resolves.toBeNull())
    expect(latest!.task).toEqual(completed)
    expect(latest!.error).toBe('post offline')
    expect(latest!.connectionError).toBe('get offline')
    expect(api.startPricingRecalculation).toHaveBeenCalledTimes(2)
  })

  it('does not treat an old terminal task as a new POST result before the first current read completes', async () => {
    const pending = Promise.withResolvers<PricingRecalculationTask | null>()
    vi.mocked(api.fetchCurrentPricingRecalculation).mockReturnValueOnce(pending.promise)
    act(() => root.render(<Harness />))
    vi.mocked(api.startPricingRecalculation).mockRejectedValueOnce(new TypeError('post connection dropped'))
    vi.mocked(api.fetchCurrentPricingRecalculation).mockResolvedValueOnce(completed)
    await act(async () => expect(latest!.start(running.start_at, 7)).resolves.toBeNull())
    expect(latest!.task).toEqual(completed)
    expect(latest!.error).toBe('post connection dropped')
    await act(async () => { pending.resolve(running) })
    expect(latest!.task).toEqual(completed)
  })

  it('recognizes a newly completed slot after a confirmed empty current read', async () => {
    const onSettled = vi.fn()
    await act(async () => root.render(<Harness onSettled={onSettled} />))
    const newCompleted = { ...completed, task_id: 'new' }
    vi.mocked(api.startPricingRecalculation).mockRejectedValueOnce(new TypeError('post connection dropped'))
    vi.mocked(api.fetchCurrentPricingRecalculation).mockResolvedValueOnce(newCompleted)
    await act(async () => expect(latest!.start(running.start_at, 7)).resolves.toEqual({ started: false, task: newCompleted }))
    expect(latest!.task).toEqual(newCompleted)
    expect(latest!.error).toBe('')
    expect(onSettled).toHaveBeenCalledExactlyOnceWith(newCompleted)
    vi.mocked(api.fetchCurrentPricingRecalculation).mockResolvedValueOnce(newCompleted)
    await act(async () => { await latest!.refreshCurrent() })
    expect(onSettled).toHaveBeenCalledTimes(1)
  })

  it('retries a failed current read even when no task is known, then resumes normal polling', async () => {
    vi.mocked(api.fetchCurrentPricingRecalculation).mockRejectedValueOnce(new TypeError('offline'))
    vi.mocked(api.fetchCurrentPricingRecalculation).mockResolvedValueOnce(running)
    await act(async () => root.render(<Harness />))
    expect(latest!.task).toBeNull()
    expect(latest!.connectionError).toBe('offline')
    await act(async () => { vi.advanceTimersByTime(2000) })
    expect(api.fetchCurrentPricingRecalculation).toHaveBeenCalledTimes(2)
    expect(latest!.task).toEqual(running)
    expect(latest!.connectionError).toBe('')
  })

  it('keeps the last task during connection errors, rejects late reads, and silently clears after a missing current task', async () => {
    vi.mocked(api.fetchCurrentPricingRecalculation).mockResolvedValueOnce(running)
    const onSettled = vi.fn()
    await act(async () => root.render(<Harness onSettled={onSettled} />))
    vi.mocked(api.fetchCurrentPricingRecalculation).mockRejectedValueOnce(new Error('offline'))
    await act(async () => { await latest!.refreshCurrent() })
    expect(latest!.task).toEqual(running)
    expect(latest!.connectionError).toBe('offline')

    const stale = Promise.withResolvers<PricingRecalculationTask | null>()
    vi.mocked(api.fetchCurrentPricingRecalculation).mockReturnValueOnce(stale.promise)
    let old!: Promise<PricingRecalculationTask | null>
    act(() => { old = latest!.refreshCurrent() })
    await act(async () => root.render(<Harness enabled={false} onSettled={onSettled} />))
    expect(vi.mocked(api.fetchCurrentPricingRecalculation).mock.lastCall?.[0]?.aborted).toBe(true)
    await act(async () => { stale.resolve(completed); await old })
    expect(latest!.task).toEqual(running)
    expect(onSettled).not.toHaveBeenCalled()

    vi.mocked(api.fetchCurrentPricingRecalculation).mockResolvedValueOnce(null)
    await act(async () => root.render(<Harness enabled onSettled={onSettled} />))
    expect(latest!.task).toBeNull()
    expect(latest!.connectionError).toBe('')
    expect(onSettled).not.toHaveBeenCalled()
  })

  it('notifies once for a watched running task and for a task completed in the POST response, not old completed state', async () => {
    const onSettled = vi.fn()
    vi.mocked(api.fetchCurrentPricingRecalculation).mockResolvedValueOnce(completed)
    await act(async () => root.render(<Harness onSettled={onSettled} />))
    expect(onSettled).not.toHaveBeenCalled()
    vi.mocked(api.startPricingRecalculation).mockResolvedValueOnce({ started: true, task: { ...completed, task_id: 'fast' } })
    await act(async () => expect(latest!.start(running.start_at, 7)).resolves.toMatchObject({ started: true }))
    expect(onSettled).toHaveBeenCalledTimes(1)
    vi.mocked(api.fetchCurrentPricingRecalculation).mockResolvedValueOnce({ ...completed, task_id: 'fast' })
    await act(async () => { await latest!.refreshCurrent() })
    expect(onSettled).toHaveBeenCalledTimes(1)
  })

  it('preserves pricing_changed and auth errors for the modal without repeating POST', async () => {
    const onAuthRequired = vi.fn()
    await act(async () => root.render(<Harness onAuthRequired={onAuthRequired} />))
    vi.mocked(api.startPricingRecalculation).mockRejectedValueOnce(new api.ApiError('price changed', 409, 'pricing_changed'))
    await act(async () => expect(latest!.start(running.start_at, 7)).resolves.toBeNull())
    expect(latest!.errorCode).toBe('pricing_changed')
    expect(latest!.error).toBe('price changed')
    expect(api.fetchCurrentPricingRecalculation).toHaveBeenCalledOnce()

    vi.mocked(api.startPricingRecalculation).mockRejectedValueOnce(new api.ApiError('sign in', 401, 'unauthorized'))
    await act(async () => expect(latest!.start(running.start_at, 7)).resolves.toBeNull())
    expect(onAuthRequired).toHaveBeenCalledOnce()
    expect(api.startPricingRecalculation).toHaveBeenCalledTimes(2)
  })
})
