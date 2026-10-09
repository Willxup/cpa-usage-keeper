// @vitest-environment happy-dom

import { act, useEffect } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { beforeEach, afterEach, expect, it, vi } from 'vitest'
import { fetchUsageQuotaCache, fetchUsageQuotaRefreshTask, refreshUsageQuotas } from '@/lib/api'
import type { UsageIdentity, UsageQuotaCacheResponse, UsageQuotaCheckResponse, UsageQuotaRefreshTaskResponse } from '@/lib/types'
import { useQuotaCache } from '../useQuotaCache'
import { useQuotaRefreshTasks } from '../useQuotaRefreshTasks'
import { buildCredentialQuotaStateMap } from '../useCredentialsTabData'
import { AuthFileQuotaPanel } from '../AuthFileCredentialsSection'
import { buildAuthFileCredentialRows } from '../credentialViewModels'

vi.mock('@/lib/api', async (original) => ({
  ...await original<typeof import('@/lib/api')>(),
  fetchUsageQuotaCache: vi.fn(), fetchUsageQuotaRefreshTask: vi.fn(), refreshUsageQuotas: vi.fn(),
}))
vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({ t: (s: string) => s }),
}))

const id = 'diagnostic-auth'
const message = 'HTTP 401: Your session has ended. Please log in again.'
const quota: UsageQuotaCheckResponse = { id, quota: [{ key: 'primary', label: '5h', used: 25, limit: 100, remaining: 75 }] }
const newQuota: UsageQuotaCheckResponse = { id, quota: [{ key: 'primary', label: '5h', used: 80, limit: 100, remaining: 20 }] }
const failure: UsageQuotaRefreshTaskResponse = { authIndex: id, status: 'failed', error: message, http_status_code: 401 }
const successCache: UsageQuotaCacheResponse = { items: [{ auth_index: id, status: 'completed', quota }] }
const failedCache: UsageQuotaCacheResponse = { items: [{ auth_index: id, status: 'failed', error: message, http_status_code: 401 }] }
const recoveredCache: UsageQuotaCacheResponse = { items: [{ auth_index: id, status: 'completed', quota: newQuota }] }

let latest: { cache: ReturnType<typeof useQuotaCache>; tasks: ReturnType<typeof useQuotaRefreshTasks> }
function Harness({ authIndexes = [id] }: { authIndexes?: string[] }) {
  const cache = useQuotaCache({ enabled: true, authIndexes })
  const tasks = useQuotaRefreshTasks({ enabled: true, currentAuthIndexes: authIndexes, quotaStateByAuthIndex: cache.quotaStateByAuthIndex, applyRefreshUpdates: cache.applyRefreshUpdates })
  const states = buildCredentialQuotaStateMap(cache.quotaStateByAuthIndex)
  const identities: UsageIdentity[] = authIndexes.map((authIndex) => ({
    id: authIndex, identity: authIndex, name: 'Codex', type: 'codex', provider: 'codex', auth_type: 1,
    auth_type_name: 'oauth', prefix: '', disabled: false, is_deleted: false,
    total_requests: 0, success_count: 0, failure_count: 0, input_tokens: 0,
    output_tokens: 0, reasoning_tokens: 0, cache_read_tokens: 0, total_tokens: 0,
    last_aggregated_usage_event_id: '', created_at: '', updated_at: '',
  }))
  const rows = buildAuthFileCredentialRows(identities, new Map(Object.entries(cache.quotaResponseByAuthIndex)), states)
  useEffect(() => { latest = { cache, tasks } }, [cache, tasks])
  return <>{rows.map((row) => <AuthFileQuotaPanel key={row.identity.id} row={row} quotaUsageMode="remaining" />)}</>
}

let root: Root
let container: HTMLDivElement
beforeEach(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true
  vi.useFakeTimers()
  vi.mocked(fetchUsageQuotaCache).mockReset().mockResolvedValue(successCache)
  vi.mocked(refreshUsageQuotas).mockReset().mockResolvedValue({ tasks: [{ authIndex: id }], rejected: [], accepted: 1, skipped: 0, limit: 1 })
  vi.mocked(fetchUsageQuotaRefreshTask).mockReset().mockResolvedValue(failure)
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
})
afterEach(() => { act(() => root.unmount()); container.remove(); vi.useRealTimers() })

function expectFailure() {
  expect(container.textContent).toContain('Your session has ended')
  expect(container.textContent).not.toContain('5h')
  expect(latest.cache.quotaResponseByAuthIndex[id]).toBeUndefined()
}

it('shows manual failure over old quota, including after cache reread and remount', async () => {
  await act(async () => root.render(<Harness />))
  expect(container.textContent).toContain('75%')
  await act(async () => latest.tasks.refreshQuotaForAuthIndex(id))
  expectFailure()
  vi.mocked(fetchUsageQuotaCache).mockResolvedValue(failedCache)
  await act(async () => latest.cache.refreshQuotaCache())
  expectFailure()
  await act(async () => root.unmount())
  root = createRoot(container)
  await act(async () => root.render(<Harness />))
  expectFailure()
})

it('shows the same manual failure with no old quota', async () => {
  vi.mocked(fetchUsageQuotaCache).mockResolvedValue({ items: [] })
  await act(async () => root.render(<Harness />))
  await act(async () => latest.tasks.refreshQuotaForAuthIndex(id))
  expectFailure()
})

it('shows a cached failure after a successful manual refresh', async () => {
  vi.mocked(fetchUsageQuotaRefreshTask).mockResolvedValue({ authIndex: id, status: 'completed', quota })
  await act(async () => root.render(<Harness />))
  await act(async () => latest.tasks.refreshQuotaForAuthIndex(id))
  vi.mocked(fetchUsageQuotaCache).mockResolvedValue(failedCache)
  await act(async () => vi.advanceTimersByTimeAsync(60_000))
  expectFailure()
})

it.each(['manual', 'inspection'] as const)('clears a manual failure after %s succeeds', async (source) => {
  await act(async () => root.render(<Harness />))
  await act(async () => latest.tasks.refreshQuotaForAuthIndex(id))
  expectFailure()
  if (source === 'manual') {
    vi.mocked(fetchUsageQuotaRefreshTask).mockResolvedValue({ authIndex: id, status: 'completed', quota: newQuota })
    await act(async () => latest.tasks.refreshQuotaForAuthIndex(id))
  } else {
    // 巡检完成回调走现有 refreshQuotaCache；另有 wiring 测试验证回调连接。
    vi.mocked(fetchUsageQuotaCache).mockResolvedValue(recoveredCache)
    await act(async () => latest.cache.refreshQuotaCache())
  }
  expect(container.textContent).toContain('20%')
  expect(container.textContent).not.toContain('Your session has ended')
})

it.each(['failed', 'completed'] as const)('ignores a cache read started before a manual task becomes %s', async (status) => {
  await act(async () => root.render(<Harness />))
  let resolveCache!: (value: UsageQuotaCacheResponse) => void
  vi.mocked(fetchUsageQuotaCache).mockImplementationOnce(() => new Promise((resolve) => { resolveCache = resolve }))
  let pending!: Promise<void>
  await act(async () => { pending = latest.cache.refreshQuotaCache() })
  if (status === 'completed') {
    vi.mocked(fetchUsageQuotaRefreshTask).mockResolvedValue({ authIndex: id, status, quota: newQuota })
  }
  await act(async () => latest.tasks.refreshQuotaForAuthIndex(id))
  await act(async () => { resolveCache(successCache); await pending })
  if (status === 'failed') expectFailure()
  else expect(container.textContent).toContain('20%')
})

it('preserves the running task when the cache read has no item, and prevents repeat submission', async () => {
  await act(async () => root.render(<Harness />))
  let resolveTask!: (value: UsageQuotaRefreshTaskResponse) => void
  vi.mocked(fetchUsageQuotaRefreshTask).mockImplementationOnce(() => new Promise((resolve) => { resolveTask = resolve }))
  await act(async () => latest.tasks.refreshQuotaForAuthIndex(id))
  vi.mocked(fetchUsageQuotaCache).mockResolvedValue({ items: [] })
  await act(async () => latest.cache.refreshQuotaCache())
  expect(latest.cache.quotaStateByAuthIndex[id].refreshStatus).toBe('queued')
  await act(async () => latest.tasks.refreshQuotaForAuthIndex(id))
  expect(refreshUsageQuotas).toHaveBeenCalledTimes(1)
  await act(async () => resolveTask(failure))
  expectFailure()
})

it('ignores a cache response begun during a task but returned after its failure', async () => {
  await act(async () => root.render(<Harness />))
  let resolveTask!: (value: UsageQuotaRefreshTaskResponse) => void
  vi.mocked(fetchUsageQuotaRefreshTask).mockImplementationOnce(() => new Promise((resolve) => { resolveTask = resolve }))
  await act(async () => latest.tasks.refreshQuotaForAuthIndex(id))
  let resolveCache!: (value: UsageQuotaCacheResponse) => void
  vi.mocked(fetchUsageQuotaCache).mockImplementationOnce(() => new Promise((resolve) => { resolveCache = resolve }))
  let pending!: Promise<void>
  await act(async () => { pending = latest.cache.refreshQuotaCache() })
  await act(async () => resolveTask(failure))
  await act(async () => { resolveCache({ items: [] }); await pending })
  expectFailure()
})


it('shows a rejected batch refresh over old quota', async () => {
  await act(async () => root.render(<Harness />))
  vi.mocked(refreshUsageQuotas).mockResolvedValue({ tasks: [], rejected: [{ authIndex: id, error: 'not_found' }], accepted: 0, skipped: 1, limit: 1 })
  await act(async () => latest.tasks.refreshQuotaForCurrentAuthFilePage())
  expect(latest.cache.quotaStateByAuthIndex[id].refreshStatus).toBe('failed')
  expect(latest.cache.quotaResponseByAuthIndex[id]).toBeUndefined()
  expect(container.textContent).not.toContain('75%')
  expect(fetchUsageQuotaRefreshTask).not.toHaveBeenCalled()
})

it('clears a cached failure when manual refresh succeeds', async () => {
  vi.mocked(fetchUsageQuotaCache).mockResolvedValue(failedCache)
  await act(async () => root.render(<Harness />))
  expectFailure()
  vi.mocked(fetchUsageQuotaRefreshTask).mockResolvedValue({ authIndex: id, status: 'completed', quota: newQuota })
  await act(async () => latest.tasks.refreshQuotaForAuthIndex(id))
  expect(container.textContent).toContain('20%')
  expect(container.textContent).not.toContain('Your session has ended')
})


it.each([
  ['HTTP 500: upstream unavailable', 500],
  ['Quota refresh timed out. Please try again later.', undefined],
  ['Quota refresh failed. Please try again later.', undefined],
] as const)('retains manual error %s across cache misses and recovers on success', async (error, statusCode) => {
  await act(async () => root.render(<Harness />))
  vi.mocked(fetchUsageQuotaRefreshTask).mockResolvedValue({ authIndex: id, status: 'failed', error, http_status_code: statusCode })
  await act(async () => latest.tasks.refreshQuotaForAuthIndex(id))
  expect(latest.cache.quotaStateByAuthIndex[id].error).toBe(error)
  vi.mocked(fetchUsageQuotaCache).mockResolvedValue({ items: [] })
  await act(async () => vi.advanceTimersByTimeAsync(120_000))
  expect(latest.cache.quotaStateByAuthIndex[id].error).toBe(error)
  expect(latest.cache.quotaResponseByAuthIndex[id]).toBeUndefined()
  expect(container.textContent).not.toContain('5h')
  vi.mocked(fetchUsageQuotaCache).mockResolvedValue(recoveredCache)
  await act(async () => latest.cache.refreshQuotaCache())
  expect(container.textContent).toContain('20%')
  expect(latest.cache.quotaStateByAuthIndex[id].error).toBeUndefined()
  // 成功已经替换手动失败，后来缓存消失不能复活旧错误或保留旧额度。
  vi.mocked(fetchUsageQuotaCache).mockResolvedValue({ items: [] })
  await act(async () => latest.cache.refreshQuotaCache())
  expect(latest.cache.quotaStateByAuthIndex[id]?.error).toBeUndefined()
  expect(latest.cache.quotaResponseByAuthIndex[id]).toBeUndefined()
})

it('clears a cache-only error when the server cache expires', async () => {
  vi.mocked(fetchUsageQuotaCache).mockResolvedValue(failedCache)
  await act(async () => root.render(<Harness />))
  expectFailure()
  vi.mocked(fetchUsageQuotaCache).mockResolvedValue({ items: [] })
  await act(async () => vi.advanceTimersByTimeAsync(60_000))
  expect(latest.cache.quotaStateByAuthIndex[id]?.error).toBeUndefined()
  expect(container.textContent).not.toContain('Your session has ended')
})

it('replaces a manual failure with an explicit cached failure and honors its expiry', async () => {
  await act(async () => root.render(<Harness />))
  vi.mocked(fetchUsageQuotaRefreshTask).mockResolvedValue({ authIndex: id, status: 'failed', error: 'HTTP 500: temporary failure' })
  await act(async () => latest.tasks.refreshQuotaForAuthIndex(id))
  vi.mocked(fetchUsageQuotaCache).mockResolvedValue(failedCache)
  await act(async () => latest.cache.refreshQuotaCache())
  expectFailure()
  vi.mocked(fetchUsageQuotaCache).mockResolvedValue({ items: [] })
  await act(async () => latest.cache.refreshQuotaCache())
  expect(latest.cache.quotaStateByAuthIndex[id]?.error).toBeUndefined()
})

it('does not restore a transient manual error after a full remount', async () => {
  await act(async () => root.render(<Harness />))
  vi.mocked(fetchUsageQuotaRefreshTask).mockResolvedValue({ authIndex: id, status: 'failed', error: 'HTTP 500: temporary failure' })
  await act(async () => latest.tasks.refreshQuotaForAuthIndex(id))
  vi.mocked(fetchUsageQuotaCache).mockResolvedValue({ items: [] })
  await act(async () => root.unmount())
  root = createRoot(container)
  await act(async () => root.render(<Harness />))
  expect(latest.cache.quotaStateByAuthIndex[id]?.error).toBeUndefined()
  expect(latest.cache.quotaResponseByAuthIndex[id]).toBeUndefined()
})

it.each([{ authIndexes: ['other-auth'] }, { authIndexes: [] }])('drops a cache-only error when the query leaves for $authIndexes, even if return cache loading fails', async ({ authIndexes }) => {
  vi.mocked(fetchUsageQuotaCache).mockResolvedValue(failedCache)
  await act(async () => root.render(<Harness />))
  expectFailure()
  vi.mocked(fetchUsageQuotaCache).mockRejectedValue(new Error('Cache read failed'))
  await act(async () => root.render(<Harness authIndexes={authIndexes} />))
  expect(latest.cache.quotaStateByAuthIndex[id]).toBeUndefined()
  await act(async () => root.render(<Harness />))
  expect(latest.cache.quotaStateByAuthIndex[id]).toBeUndefined()
  expect(container.textContent).not.toContain('Your session has ended')
  vi.mocked(fetchUsageQuotaCache).mockResolvedValue(recoveredCache)
  await act(async () => latest.cache.refreshQuotaCache())
  expect(container.textContent).toContain('20%')
})

it('keeps cache errors for credentials still in the query when other credentials leave', async () => {
  const otherId = 'other-auth'
  vi.mocked(fetchUsageQuotaCache).mockResolvedValue({ items: [
    ...failedCache.items,
    { auth_index: otherId, status: 'failed', error: 'HTTP 403: permission denied' },
  ] })
  await act(async () => root.render(<Harness authIndexes={[id, otherId]} />))
  vi.mocked(fetchUsageQuotaCache).mockRejectedValue(new Error('Cache read failed'))
  await act(async () => root.render(<Harness authIndexes={[otherId]} />))
  expect(latest.cache.quotaStateByAuthIndex[id]).toBeUndefined()
  expect(latest.cache.quotaStateByAuthIndex[otherId].error).toBe('HTTP 403: permission denied')
  expect(container.textContent).toContain('permission denied')
})

it('does not clear cache errors when the query is only reordered', async () => {
  vi.mocked(fetchUsageQuotaCache).mockResolvedValue(failedCache)
  await act(async () => root.render(<Harness authIndexes={[id, 'other-auth']} />))
  expectFailure()
  vi.mocked(fetchUsageQuotaCache).mockRejectedValue(new Error('Cache read failed'))
  await act(async () => root.render(<Harness authIndexes={['other-auth', id]} />))
  expectFailure()
})

it('preserves a manual failure across pages, empty queries and cache misses until success', async () => {
  await act(async () => root.render(<Harness />))
  vi.mocked(fetchUsageQuotaRefreshTask).mockResolvedValue({ authIndex: id, status: 'failed', error: 'HTTP 500: temporary failure' })
  await act(async () => latest.tasks.refreshQuotaForAuthIndex(id))
  vi.mocked(fetchUsageQuotaCache).mockResolvedValue({ items: [] })
  await act(async () => root.render(<Harness authIndexes={['other-auth']} />))
  expect(latest.cache.quotaStateByAuthIndex[id].error).toBe('HTTP 500: temporary failure')
  await act(async () => root.render(<Harness authIndexes={[]} />))
  expect(latest.cache.quotaStateByAuthIndex[id].error).toBe('HTTP 500: temporary failure')
  await act(async () => root.render(<Harness />))
  expect(container.textContent).toContain('temporary failure')
  expect(latest.cache.quotaResponseByAuthIndex[id]).toBeUndefined()
  vi.mocked(fetchUsageQuotaCache).mockResolvedValue(recoveredCache)
  await act(async () => latest.cache.refreshQuotaCache())
  expect(container.textContent).toContain('20%')
  expect(container.textContent).not.toContain('temporary failure')
})

it.each(['queued', 'running'] as const)('preserves a %s manual task across pages and lets it finish off-page', async (status) => {
  await act(async () => root.render(<Harness />))
  vi.mocked(fetchUsageQuotaRefreshTask).mockResolvedValue({ authIndex: id, status })
  await act(async () => latest.tasks.refreshQuotaForAuthIndex(id))
  expect(latest.cache.quotaStateByAuthIndex[id].refreshStatus).toBe(status)
  vi.mocked(fetchUsageQuotaCache).mockResolvedValue({ items: [] })
  await act(async () => root.render(<Harness authIndexes={['other-auth']} />))
  await act(async () => root.render(<Harness authIndexes={[]} />))
  expect(latest.cache.quotaStateByAuthIndex[id].refreshStatus).toBe(status)
  await act(async () => root.render(<Harness />))
  expect(latest.cache.quotaStateByAuthIndex[id].refreshStatus).toBe(status)
  await act(async () => latest.tasks.refreshQuotaForAuthIndex(id))
  expect(refreshUsageQuotas).toHaveBeenCalledOnce()
  await act(async () => root.render(<Harness authIndexes={[]} />))
  vi.mocked(fetchUsageQuotaRefreshTask).mockResolvedValue({ authIndex: id, status: 'completed', quota: newQuota })
  await act(async () => vi.advanceTimersByTimeAsync(5_000))
  expect(latest.cache.quotaStateByAuthIndex[id].refreshStatus).toBe('completed')
  expect(latest.cache.quotaResponseByAuthIndex[id]).toEqual(newQuota)
  vi.mocked(fetchUsageQuotaCache).mockRejectedValue(new Error('Cache read failed'))
  await act(async () => root.render(<Harness />))
  expect(container.textContent).toContain('20%')
})

it('keeps successful quota snapshots when their cache state leaves the query', async () => {
  await act(async () => root.render(<Harness />))
  expect(container.textContent).toContain('75%')
  vi.mocked(fetchUsageQuotaCache).mockRejectedValue(new Error('Cache read failed'))
  await act(async () => root.render(<Harness authIndexes={[]} />))
  expect(latest.cache.quotaResponseByAuthIndex[id]).toEqual(quota)
  await act(async () => root.render(<Harness />))
  expect(container.textContent).toContain('75%')
})

it('ignores a late cache response from a departed page instead of restoring its old error', async () => {
  vi.mocked(fetchUsageQuotaCache).mockResolvedValue(failedCache)
  await act(async () => root.render(<Harness />))
  let resolveCache!: (value: UsageQuotaCacheResponse) => void
  vi.mocked(fetchUsageQuotaCache).mockImplementationOnce(() => new Promise((resolve) => { resolveCache = resolve }))
  let pending!: Promise<void>
  await act(async () => { pending = latest.cache.refreshQuotaCache() })
  vi.mocked(fetchUsageQuotaCache).mockRejectedValue(new Error('Cache read failed'))
  await act(async () => root.render(<Harness authIndexes={['other-auth']} />))
  await act(async () => { resolveCache(failedCache); await pending })
  await act(async () => root.render(<Harness />))
  expect(latest.cache.quotaStateByAuthIndex[id]).toBeUndefined()
  expect(container.textContent).not.toContain('Your session has ended')
})
