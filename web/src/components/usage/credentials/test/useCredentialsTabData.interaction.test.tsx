// @vitest-environment happy-dom

import { act, useEffect } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import { useCredentialsTabData } from '../useCredentialsTabData'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

type TabData = ReturnType<typeof useCredentialsTabData>

interface Deferred<T> {
  promise: Promise<T>
  resolve: (value: T) => void
}

function createDeferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((next) => {
    resolve = next
  })
  return { promise, resolve }
}

let latest: TabData | null = null

function Harness({ onNotice }: { onNotice: (kind: 'success' | 'info' | 'error', message: string) => void }) {
  const result = useCredentialsTabData({ enabledAuthFiles: true, enabledAiProviders: true, onNotice })
  useEffect(() => {
    latest = result
  }, [result])
  return null
}

const identitiesResponse = (url: string) => {
  const parsed = new URL(url, 'http://localhost')
  return {
    ok: true,
    status: 200,
    json: async () => ({
      identities: [],
      total_count: 0,
      page: Number(parsed.searchParams.get('page') ?? 1),
      page_size: Number(parsed.searchParams.get('page_size') ?? 10),
      total_pages: 1,
      type_counts: [],
    }),
  } as unknown as Response
}

const isCredentialStatusPatch = (input: unknown, init?: RequestInit) =>
  String(input).includes('/status') && init?.method === 'PATCH'

const pageCalls = (mock: ReturnType<typeof vi.spyOn>, authType: number) =>
  mock.mock.calls
    .filter(([input, init]) => String(input).includes('/usage/identities/page') && init?.method !== 'PATCH')
    .map(([input]) => new URL(String(input), 'http://localhost'))
    .filter((url) => url.searchParams.get('auth_type') === String(authType))

const statusPatchCalls = (mock: ReturnType<typeof vi.spyOn>) =>
  mock.mock.calls.filter(([input, init]) => isCredentialStatusPatch(input, init as RequestInit | undefined)).map(([input]) => String(input))

const flush = async () => {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}

// 开关回调是 void 签名，无法 await 它的 Promise，因此用条件轮询等待异步链路收敛。
const waitFor = async (check: () => boolean) => {
  for (let attempt = 0; attempt < 50; attempt += 1) {
    if (check()) {
      return
    }
    await flush()
  }
  expect(check()).toBe(true)
}

let container: HTMLDivElement
let root: Root
let fetchMock: ReturnType<typeof vi.spyOn>
let statusDeferred: Deferred<Response>

beforeEach(() => {
  window.localStorage.clear()
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  statusDeferred = createDeferred<Response>()
  fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation((input, init) => {
    if (isCredentialStatusPatch(input, init as RequestInit | undefined)) {
      return statusDeferred.promise
    }
    return Promise.resolve(identitiesResponse(String(input)))
  })
})

afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
  latest = null
  fetchMock.mockRestore()
})

describe('credential status refresh after toggle', () => {
  it('refreshes with the page the user moved to while the status request was pending', async () => {
    await act(async () => root.render(<Harness onNotice={() => undefined} />))
    await flush()
    expect(pageCalls(fetchMock, 2).at(-1)?.searchParams.get('page')).toBe('1')

    // 开关请求挂起期间用户切到第 2 页，刷新必须使用最新页码而不是点击时捕获的旧闭包。
    await act(async () => {
      latest?.toggleAiProviderStatus('1', 'idx-1', true)
    })
    await act(async () => latest?.setAiProviderPage(2))
    await flush()
    expect(pageCalls(fetchMock, 2).at(-1)?.searchParams.get('page')).toBe('2')

    const pageRequestsBeforeResolution = pageCalls(fetchMock, 2).length
    await act(async () => {
      statusDeferred.resolve({ ok: true, status: 200, json: async () => ({ auth_index: 'idx-1', disabled: true }) } as unknown as Response)
    })
    await waitFor(() => pageCalls(fetchMock, 2).length > pageRequestsBeforeResolution)

    expect(pageCalls(fetchMock, 2).at(-1)?.searchParams.get('page')).toBe('2')
  })

  it('refreshes the stale list after a 404 using the latest filter', async () => {
    const notices: string[] = []
    await act(async () => root.render(<Harness onNotice={(kind, message) => notices.push(`${kind}:${message}`)} />))
    await flush()

    await act(async () => {
      latest?.toggleAuthFileStatus('1', 'idx-a', true)
    })
    // 列表被判定过期时，用户已经打开了「仅启用」，刷新必须带上该筛选。
    await act(async () => latest?.setAuthFileActiveOnly(true))
    await flush()

    const requestsBeforeResolution = pageCalls(fetchMock, 1).length
    await act(async () => {
      statusDeferred.resolve({ ok: false, status: 404, json: async () => ({ error: 'credential not found' }) } as unknown as Response)
    })
    await waitFor(() => pageCalls(fetchMock, 1).length > requestsBeforeResolution)

    const last = pageCalls(fetchMock, 1).at(-1)
    expect(last?.searchParams.get('active_only')).toBe('true')
    expect(notices.at(-1)).toBe(`error:${i18n.t('usage_stats.credentials_status_stale_target')}`)
  })

  it('ignores repeated activation of the same credential while the first request is in flight', async () => {
    await act(async () => root.render(<Harness onNotice={() => undefined} />))
    await flush()

    // 开关按钮用 aria-disabled，浏览器不再拦截重复点击，这里锁定 hook 自身的兜底。
    await act(async () => {
      latest?.toggleAiProviderStatus('identity-7', 'idx-1', true)
      latest?.toggleAiProviderStatus('identity-7', 'idx-1', true)
    })
    await flush()
    expect(statusPatchCalls(fetchMock)).toHaveLength(1)

    await act(async () => {
      statusDeferred.resolve({ ok: true, status: 200, json: async () => ({ auth_index: 'idx-1', disabled: true }) } as unknown as Response)
    })
    await waitFor(() => !latest!.credentialStatusPendingIdentityIds.has('identity-7'))
  })
})

describe('credential status pending identity', () => {
  it('isolates pending state by Keeper identity id while still sending auth_index upstream', async () => {
    await act(async () => root.render(<Harness onNotice={() => undefined} />))
    await flush()

    await act(async () => latest?.toggleAiProviderStatus('identity-7', 'idx-1', true))

    expect(Array.from(latest!.credentialStatusPendingIdentityIds)).toEqual(['identity-7'])
    expect(statusPatchCalls(fetchMock).at(-1)).toContain('/ai-providers/idx-1/status')

    await act(async () => {
      statusDeferred.resolve({ ok: true, status: 200, json: async () => ({ auth_index: 'idx-1', disabled: true }) } as unknown as Response)
    })
    await waitFor(() => latest!.credentialStatusPendingIdentityIds.size === 0)
  })

  it('falls back to auth_index when a row has no Keeper identity id', async () => {
    await act(async () => root.render(<Harness onNotice={() => undefined} />))
    await flush()

    await act(async () => {
      latest?.toggleAuthFileStatus('', 'idx-9', true)
    })

    expect(Array.from(latest!.credentialStatusPendingIdentityIds)).toEqual(['idx-9'])
    expect(statusPatchCalls(fetchMock).at(-1)).toContain('/auth-files/idx-9/status')

    await act(async () => {
      statusDeferred.resolve({ ok: true, status: 200, json: async () => ({ auth_index: 'idx-9', disabled: true }) } as unknown as Response)
    })
    await waitFor(() => latest!.credentialStatusPendingIdentityIds.size === 0)
  })
})
