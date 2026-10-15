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
}))

vi.mock('../useCredentialPages', () => ({
  useCredentialPages: () => ({ authFileIdentities: [], aiProviderIdentities: [], refresh: mocks.refreshPages }),
}))
vi.mock('../useQuotaCache', () => ({
  useQuotaCache: () => ({ quotaResponseByAuthIndex: {}, quotaStateByAuthIndex: {}, applyRefreshUpdates: vi.fn(), refreshQuotaCache: mocks.refreshCache }),
}))
vi.mock('../useQuotaRefreshTasks', async (importOriginal) => ({
  ...await importOriginal<typeof import('../useQuotaRefreshTasks')>(),
  useQuotaRefreshTasks: () => ({ quotaStateByAuthIndex: {}, refreshQuotaForAuthIndex: mocks.refreshQuota }),
}))
vi.mock('../useQuotaInspection', () => ({
  useQuotaInspection: (options: unknown) => { mocks.inspection(options); return {} },
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
