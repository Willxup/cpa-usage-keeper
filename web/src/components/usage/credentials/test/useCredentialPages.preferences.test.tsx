// @vitest-environment happy-dom

import { act, useEffect } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useCredentialPages } from '../useCredentialPages'
import { CREDENTIAL_LIST_PREFERENCES_STORAGE_KEYS } from '../credentialListPreferences'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

let latest: ReturnType<typeof useCredentialPages> | null = null

function Harness() {
  const result = useCredentialPages({ enabledAuthFiles: true, enabledAiProviders: true })
  useEffect(() => { latest = result }, [result])
  return null
}

const requestFor = (fetchMock: ReturnType<typeof vi.spyOn>, authType: string) => {
  const call = fetchMock.mock.calls
    .map((args) => new URL(String(args[0]), 'http://localhost'))
    .filter((url) => url.searchParams.get('auth_type') === authType)
    .at(-1)
  return call?.searchParams
}

const storedPreferences = (scope: 'auth-files' | 'ai-provider') => (
  JSON.parse(String(window.localStorage.getItem(CREDENTIAL_LIST_PREFERENCES_STORAGE_KEYS[scope])))
)

describe('credential list preferences wiring', () => {
  let container: HTMLDivElement
  let root: Root
  let fetchMock: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    window.localStorage.clear()
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
    fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ identities: [], total_count: 0, page: 1, page_size: 10, total_pages: 0, type_counts: [] }),
    } as Response)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    latest = null
    fetchMock.mockRestore()
  })

  it('starts from each section default when nothing is stored', async () => {
    await act(async () => root.render(<Harness />))

    expect(latest?.authFileSort).toBe('priority')
    expect(latest?.aiProviderSort).toBe('total_requests')
    expect(latest?.authFilePageSize).toBe(10)
    expect(latest?.aiProviderProviderFilter).toBe('all')
    expect(requestFor(fetchMock, '1')?.get('sort')).toBe('priority')
    expect(requestFor(fetchMock, '2')?.get('sort')).toBe('total_requests')
  })

  it('restores sort, page size and provider filter into the first request', async () => {
    window.localStorage.setItem(CREDENTIAL_LIST_PREFERENCES_STORAGE_KEYS['ai-provider'], JSON.stringify({
      version: 1,
      sort: 'last_used_at',
      pageSize: 50,
      providerFilter: 'openai',
    }))
    await act(async () => root.render(<Harness />))

    expect(latest?.aiProviderSort).toBe('last_used_at')
    expect(latest?.aiProviderPageSize).toBe(50)
    expect(latest?.aiProviderProviderFilter).toBe('openai')

    const aiProviderRequest = requestFor(fetchMock, '2')
    expect(aiProviderRequest?.get('sort')).toBe('last_used_at')
    expect(aiProviderRequest?.get('page_size')).toBe('50')
    expect(aiProviderRequest?.get('type')).toBe('openai')

    // Auth 文件分区没有存过任何偏好，必须保持自己的默认值。
    expect(latest?.authFileSort).toBe('priority')
    expect(latest?.authFilePageSize).toBe(10)
    expect(latest?.authFileProviderFilter).toBe('all')
  })

  it('persists each selection and returns to the first page', async () => {
    await act(async () => root.render(<Harness />))
    await act(async () => latest?.setAiProviderPage(4))
    expect(latest?.aiProviderPage).toBe(4)

    await act(async () => latest?.setAiProviderSort('total_tokens'))
    expect(latest?.aiProviderSort).toBe('total_tokens')
    expect(latest?.aiProviderPage).toBe(1)
    expect(storedPreferences('ai-provider').sort).toBe('total_tokens')

    await act(async () => latest?.setAiProviderPageSize(20))
    await act(async () => latest?.setAiProviderProviderFilter('claude'))
    expect(storedPreferences('ai-provider')).toEqual({
      version: 1,
      sort: 'total_tokens',
      pageSize: 20,
      providerFilter: 'claude',
    })
  })

  it('keeps the two sections in separate records', async () => {
    await act(async () => root.render(<Harness />))
    await act(async () => latest?.setAuthFileSort('last_used_at'))

    expect(latest?.authFileSort).toBe('last_used_at')
    expect(latest?.aiProviderSort).toBe('total_requests')
    expect(storedPreferences('auth-files').sort).toBe('last_used_at')
    expect(window.localStorage.getItem(CREDENTIAL_LIST_PREFERENCES_STORAGE_KEYS['ai-provider'])).toBeNull()
  })

  it('ignores a damaged record instead of sending it to the server', async () => {
    window.localStorage.setItem(CREDENTIAL_LIST_PREFERENCES_STORAGE_KEYS['ai-provider'], JSON.stringify({
      version: 1,
      sort: 'total_cost',
      pageSize: 999,
      providerFilter: 'antigravity',
    }))
    await act(async () => root.render(<Harness />))

    const aiProviderRequest = requestFor(fetchMock, '2')
    expect(aiProviderRequest?.get('sort')).toBe('total_requests')
    expect(aiProviderRequest?.get('page_size')).toBe('10')
    expect(aiProviderRequest?.has('type')).toBe(false)
  })
})
