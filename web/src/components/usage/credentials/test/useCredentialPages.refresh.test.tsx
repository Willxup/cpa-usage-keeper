// @vitest-environment happy-dom
import { act, useEffect } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { UsageQuotaInspectionStatusResponse } from '@/lib/types'
import { QuotaInspectionModal } from '../inspection/QuotaInspectionModal'
import { useCredentialPages } from '../useCredentialPages'
import { useQuotaInspection } from '../useQuotaInspection'

vi.mock('react-i18next', async (original) => ({
  ...await original<typeof import('react-i18next')>(),
  useTranslation: () => ({ t: (key: string) => key }),
}))
globalThis.IS_REACT_ACT_ENVIRONMENT = true

const inspection: UsageQuotaInspectionStatusResponse = {
  total: 2, cached: 2, running: false, completed: true, normal: 0,
  limit_reached: 0, unauthorized_401: 2, payment_required_402: 0,
  unauthorized_401_402: 2, other_failed: 0, unknown: 0,
  results: ['a.json', 'b.json'].map((file, i) => ({ auth_index: `auth-${i}`, file_name: file, name: file, type: 'codex', status: 'unauthorized_401' })),
}
const page = (id: string, disabled = false) => Response.json({
  identities: [{ id, auth_type: 1, disabled }], total_count: 1, total_pages: 1, type_counts: [],
})
const button = (text: string) => [...document.querySelectorAll<HTMLButtonElement>('button')].find((item) => item.textContent === text)!

type PendingListRequest = {
  url: URL
  signal: AbortSignal
  resolve: (response: Response) => void
}
let requests: PendingListRequest[]
let writes: unknown[]
let statusReads: number
let respectAbort: boolean
let latest: ReturnType<typeof useCredentialPages>

function PagesHarness({ credentials = true, providers = false }: { credentials?: boolean; providers?: boolean }) {
  const pages = useCredentialPages({ enabledCredentials: credentials, enabledAiProviders: providers })
  useEffect(() => { latest = pages }, [pages])
  return null
}

function ConnectedInspection({ onAuthRequired }: { onAuthRequired: () => void }) {
  const pages = useCredentialPages({ enabledCredentials: true, enabledAiProviders: false, onAuthRequired })
  const status = useQuotaInspection({ enabled: true })
  return <>
    <output data-testid="list">{pages.credentialIdentities.map((identity) => `${identity.id}:${identity.disabled}`).join(',')}</output>
    <output data-testid="error">{pages.error}</output>
    <QuotaInspectionModal open status={status.quotaInspectionStatus} loading={status.quotaInspectionLoading}
      starting={status.quotaInspectionStarting} error={status.quotaInspectionError} onClose={() => undefined}
      onStart={status.startQuotaInspection} onRefreshStatus={status.refreshQuotaInspectionStatus}
      onAfterInvalidAccountAction={pages.refresh} />
  </>
}

describe('credential list refresh handoff', () => {
  let root: Root | null
  let container: HTMLDivElement

  beforeEach(() => {
    vi.useFakeTimers()
    window.localStorage.clear()
    window.history.replaceState(null, '', '/')
    requests = []
    writes = []
    statusReads = 0
    respectAbort = true
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
    vi.spyOn(globalThis, 'fetch').mockImplementation((input, init) => {
      const url = new URL(String(input), 'http://localhost')
      if (url.pathname.endsWith('/quota/inspection')) {
        statusReads++
        return Promise.resolve(Response.json(inspection))
      }
      if (url.pathname.endsWith('/credentials/status')) {
        writes.push(JSON.parse(String(init?.body)))
        return Promise.resolve(Response.json({ success: true }))
      }
      if (!url.pathname.endsWith('/usage/identities/page')) throw new Error(`Unexpected request: ${url}`)
      const signal = init?.signal as AbortSignal
      const pending = Promise.withResolvers<Response>()
      requests.push({ url, signal, resolve: pending.resolve })
      if (respectAbort) signal.addEventListener('abort', () => pending.reject(new DOMException('Aborted', 'AbortError')), { once: true })
      return pending.promise
    })
  })

  afterEach(async () => {
    if (root) await act(async () => root?.unmount())
    container.remove()
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  it.each(['success', 'failure', 'unauthorized'] as const)(
    'holds the real session across the 60s list timer and recovers after %s',
    async (outcome) => {
      const onAuthRequired = vi.fn()
      await act(async () => root!.render(<ConnectedInspection onAuthRequired={onAuthRequired} />))
      await act(async () => requests[0].resolve(page('b', false)))
      expect(document.querySelector('[data-testid="list"]')?.textContent).toBe('b:false')
      expect(statusReads).toBe(1)

      await act(async () => vi.advanceTimersByTimeAsync(50_000))
      await act(async () => button('usage_stats.credentials_inspection_disable_invalid').click())
      const boxes = document.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')
      await act(async () => boxes[0].click())
      await act(async () => button('usage_stats.credentials_inspection_invalid_accounts_confirm').click())
      expect(writes).toEqual([{ names: ['b.json'], disabled: true }])
      expect(statusReads).toBe(2)
      expect(requests).toHaveLength(2)
      const manual = requests[1]
      await act(async () => vi.advanceTimersByTimeAsync(12_000))

      expect(manual.signal.aborted).toBe(false)
      expect(requests).toHaveLength(2)
      expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(2)
      expect(button('common.cancel').disabled).toBe(true)
      expect(button('usage_stats.credentials_inspection_disable_invalid').disabled).toBe(true)
      expect(document.querySelectorAll<HTMLButtonElement>('[role="dialog"] button[aria-label="common.close"]')[0].disabled).toBe(true)
      expect(boxes[0].checked).toBe(false)
      expect(boxes[1].checked).toBe(true)
      await act(async () => button('usage_stats.credentials_inspection_invalid_accounts_confirm').click())
      expect(writes).toHaveLength(1)
      expect(document.querySelector('[data-testid="list"]')?.textContent).toBe('b:false')

      await act(async () => {
        if (outcome === 'success') manual.resolve(page('b', true))
        else if (outcome === 'failure') manual.resolve(Response.json({ error: 'temporary outage' }, { status: 500 }))
        else manual.resolve(Response.json({ error: 'unauthorized' }, { status: 401 }))
      })
      expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1)
      expect(button('usage_stats.credentials_inspection_disable_invalid').disabled).toBe(false)
      expect(document.querySelectorAll<HTMLButtonElement>('[role="dialog"] button[aria-label="common.close"]')[0].disabled).toBe(false)
      expect(onAuthRequired).toHaveBeenCalledTimes(outcome === 'unauthorized' ? 1 : 0)
      expect(document.querySelector('[data-testid="error"]')?.textContent).toBe(outcome === 'failure' ? 'temporary outage' : '')
      expect(document.querySelector('[data-testid="list"]')?.textContent).toBe(outcome === 'success' ? 'b:true' : outcome === 'failure' ? '' : 'b:false')

      await act(async () => vi.advanceTimersByTimeAsync(58_000))
      expect(requests).toHaveLength(3)
      await act(async () => requests[2].resolve(page('b', true)))
      expect(document.querySelector('[data-testid="list"]')?.textContent).toBe('b:true')
      expect(document.querySelector('[data-testid="error"]')?.textContent).toBe('')
    },
  )

  it.each(['page', 'filter', 'scope', 'disabled', 'unmount'] as const)(
    'preserves cancellation and stale-response protection on %s',
    async (change) => {
      // 模拟不遵守 abort 的传输，验证旧响应本身也不能覆盖新查询。
      respectAbort = false
      await act(async () => root!.render(<PagesHarness />))
      await act(async () => requests[0].resolve(page('initial')))
      let pending!: Promise<void>
      await act(async () => { pending = latest.refresh() })
      const old = requests[1]
      if (change === 'page') await act(async () => latest.setCredentialPage(2))
      if (change === 'filter') await act(async () => latest.setCredentialProviderFilter('codex'))
      if (change === 'scope') await act(async () => root!.render(<PagesHarness credentials={false} providers />))
      if (change === 'disabled') await act(async () => root!.render(<PagesHarness credentials={false} />))
      if (change === 'unmount') {
        await act(async () => root!.unmount())
        root = null
      }
      expect(old.signal.aborted).toBe(true)
      if (change === 'page' || change === 'filter' || change === 'scope') {
        expect(requests).toHaveLength(3)
        if (change === 'page') expect(requests[2].url.searchParams.get('page')).toBe('2')
        if (change === 'filter') expect(requests[2].url.searchParams.get('type')).toBe('codex')
        if (change === 'scope') expect(requests[2].url.searchParams.get('auth_type')).toBe('2')
        await act(async () => requests[2].resolve(page('current')))
      } else {
        expect(requests).toHaveLength(2)
      }
      await act(async () => { old.resolve(page('stale')); await pending })
      if (change !== 'unmount') {
        expect(latest.credentialIdentities[0].id).toBe(change === 'page' || change === 'filter' ? 'current' : 'initial')
        if (change === 'scope') expect(latest.aiProviderIdentities[0].id).toBe('current')
      }
      if (change === 'disabled' || change === 'unmount') {
        await act(async () => vi.advanceTimersByTimeAsync(120_000))
        expect(requests).toHaveLength(2)
      }
    },
  )

  it('still lets a manual refresh replace an earlier list read', async () => {
    await act(async () => root!.render(<PagesHarness />))
    await act(async () => requests[0].resolve(page('initial')))
    let first!: Promise<void>, second!: Promise<void>
    await act(async () => { first = latest.refresh() })
    await act(async () => { second = latest.refresh() })
    expect(requests[1].signal.aborted).toBe(true)
    expect(requests).toHaveLength(3)
    await act(async () => { requests[2].resolve(page('new')); await Promise.all([first, second]) })
    expect(latest.credentialIdentities[0].id).toBe('new')
  })
})
