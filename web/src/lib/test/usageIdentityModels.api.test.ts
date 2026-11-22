import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, fetchUsageIdentityModels } from '../api'

describe('fetchUsageIdentityModels', () => {
  afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

  it.each(['today', '7d', '30d'] as const)('requests only the identity and %s range under the configured base path', async range => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper/' })
    const payload = { identity_id: '123', range, models: [], summary: {} }
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(Response.json(payload))
    const signal = new AbortController().signal
    expect(await fetchUsageIdentityModels('123/456', range, signal)).toEqual(payload)
    const [url, init] = fetchMock.mock.calls[0]
    const parsed = new URL(String(url), 'http://localhost')
    expect(parsed.pathname).toBe('/keeper/api/v1/usage/identities/123%2F456/models')
    expect([...parsed.searchParams.entries()]).toEqual([['range', range]])
    expect(init).toMatchObject({ credentials: 'include', signal })
  })

  it('preserves the dedicated cost gate error', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined })
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(Response.json({ error: 'unavailable', code: 'costs_busy' }, { status: 503 }))
    await expect(fetchUsageIdentityModels('1', '7d')).rejects.toMatchObject<ApiError>({ status: 503, code: 'costs_busy' } as ApiError)
  })
})
