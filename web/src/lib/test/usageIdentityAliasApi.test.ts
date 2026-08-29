import { afterEach, describe, expect, it, vi } from 'vitest'
import { updateUsageIdentityAlias } from '../api'

const headerValue = (init: RequestInit | undefined, name: string): string | null => new Headers(init?.headers).get(name)

describe('updateUsageIdentityAlias', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('patches usage identity aliases through the usage identities endpoint', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined })
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ id: '123', alias: 'Friendly Auth', displayName: 'Friendly Auth' }),
    } as Response)

    const updated = await updateUsageIdentityAlias('123', ' Friendly Auth ')

    const [url, init] = fetchMock.mock.calls[0]
    expect(new URL(String(url), 'http://localhost').pathname).toBe('/api/v1/usage/identities/123')
    expect(init).toMatchObject({ credentials: 'include', method: 'PATCH' })
    expect(headerValue(init, 'Content-Type')).toBe('application/json')
    expect(init?.body).toBe(JSON.stringify({ alias: ' Friendly Auth ' }))
    expect(updated.displayName).toBe('Friendly Auth')
  })
})
