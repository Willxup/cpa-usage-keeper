// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchUsageQuotaCache } from '@/lib/api'
import { useQuotaCache } from '../useQuotaCache'

vi.mock('@/lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/lib/api')>(),
  fetchUsageQuotaCache: vi.fn(),
}))

function Harness({ enabled }: { enabled: boolean }) {
  useQuotaCache({ enabled, authIndexes: ['auth-1'] })
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
})
