// @vitest-environment happy-dom

import { act, useEffect } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, resetUsageQuota } from '@/lib/api'
import { quotaResetDisplayError, useCredentialsTabData } from '../useCredentialsTabData'

const mocks = vi.hoisted(() => ({
  refreshPages: vi.fn(),
  refreshCache: vi.fn(),
  refreshQuota: vi.fn(),
  inspection: vi.fn(),
  resetCache: vi.fn(),
  resetTasks: vi.fn(),
  resetInspection: vi.fn(),
  refreshInspection: vi.fn(),
}))

vi.mock('../useCredentialPages', () => ({
  useCredentialPages: () => ({ authFileIdentities: [], aiProviderIdentities: [], refresh: mocks.refreshPages }),
}))
vi.mock('../useQuotaCache', () => ({
  useQuotaCache: () => ({ quotaResponseByAuthIndex: {}, cachedQuotaStateByAuthIndex: {}, refreshQuotaCache: mocks.refreshCache, resetQuotaCache: mocks.resetCache }),
}))
vi.mock('../useQuotaRefreshTasks', async (importOriginal) => ({
  ...await importOriginal<typeof import('../useQuotaRefreshTasks')>(),
  useQuotaRefreshTasks: () => ({ quotaStateByAuthIndex: {}, refreshQuotaForAuthIndex: mocks.refreshQuota, resetQuotaRefreshTasks: mocks.resetTasks }),
}))
vi.mock('../useQuotaInspection', () => ({
  useQuotaInspection: (options: unknown) => { mocks.inspection(options); return { resetQuotaInspection: mocks.resetInspection, refreshQuotaInspectionStatus: mocks.refreshInspection } },
}))
vi.mock('@/lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/lib/api')>(),
  resetUsageQuota: vi.fn(),
}))

let latest: ReturnType<typeof useCredentialsTabData> | null = null
function Harness(props: Parameters<typeof useCredentialsTabData>[0]) {
  const result = useCredentialsTabData(props)
  useEffect(() => { latest = result }, [result])
  return null
}

describe('credential hook coordination', () => {
  let root: Root
  let container: HTMLDivElement

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    vi.resetAllMocks()
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(() => {
    act(() => root.unmount())
    container.remove()
    latest = null
  })

  it('refreshes identities and cached quota together, and refreshes quota after inspection', async () => {
    await act(async () => root.render(<Harness enabledAuthFiles enabledAiProviders={false} />))
    await act(async () => latest!.refresh())
    expect(mocks.refreshPages).toHaveBeenCalledOnce()
    expect(mocks.refreshCache).toHaveBeenCalledOnce()

    await act(async () => mocks.inspection.mock.calls[0][0].onInspectionCompleted())
    expect(mocks.refreshCache).toHaveBeenCalledTimes(2)
  })

  it('clears quota hooks and rereads cache/status only while auth files are visible', async () => {
    await act(async () => root.render(<Harness enabledAuthFiles enabledAiProviders={false} />))
    await act(async () => latest!.refreshAfterRecalculation())
    expect(mocks.resetCache).toHaveBeenCalledOnce()
    expect(mocks.resetTasks).toHaveBeenCalledOnce()
    expect(mocks.resetInspection).toHaveBeenCalledOnce()
    expect(mocks.refreshCache).toHaveBeenCalledOnce()
    expect(mocks.refreshInspection).toHaveBeenCalledOnce()
    expect(mocks.refreshQuota).not.toHaveBeenCalled()

    await act(async () => root.render(<Harness enabledAuthFiles={false} enabledAiProviders={false} />))
    await act(async () => latest!.refreshAfterRecalculation())
    expect(mocks.resetCache).toHaveBeenCalledTimes(2)
    expect(mocks.resetTasks).toHaveBeenCalledTimes(2)
    expect(mocks.resetInspection).toHaveBeenCalledTimes(2)
    expect(mocks.refreshCache).toHaveBeenCalledOnce()
    expect(mocks.refreshInspection).toHaveBeenCalledOnce()
  })

  it.each([401, 502])('reports reset HTTP %i as a notice without logging out or refreshing quota', async (status) => {
    const onNotice = vi.fn()
    const onAuthRequired = vi.fn()
    await act(async () => root.render(<Harness enabledAuthFiles enabledAiProviders={false} onNotice={onNotice} />))
    const reset = latest!.resetQuotaForAuthIndex
    await act(async () => root.render(<Harness enabledAuthFiles enabledAiProviders={false} onNotice={onNotice} onAuthRequired={onAuthRequired} />))
    expect(latest!.resetQuotaForAuthIndex).toBe(reset)

    vi.mocked(resetUsageQuota).mockRejectedValueOnce(new ApiError('quota_reset_failed', status))
    await act(async () => latest!.resetQuotaForAuthIndex('auth-1'))
    expect(resetUsageQuota).toHaveBeenCalledWith('auth-1')
    expect(onNotice).toHaveBeenCalledWith('error', quotaResetDisplayError())
    expect(onAuthRequired).not.toHaveBeenCalled()
    expect(mocks.refreshQuota).not.toHaveBeenCalled()
  })
})
