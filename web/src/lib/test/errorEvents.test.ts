import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchErrorEvents } from '../api'

describe('fetchErrorEvents', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('loads one credential error cursor page by Keeper identity id', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper/' })
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ events: [], next_cursor: 'next', has_more: true }),
    } as Response)
    const signal = new AbortController().signal

    const response = await fetchErrorEvents('9007199254740993', signal, 'cursor value', 50)

    const [url, init] = fetchMock.mock.calls[0]
    const parsed = new URL(String(url), 'http://localhost')
    expect(response.next_cursor).toBe('next')
    expect(parsed.pathname).toBe('/keeper/api/v1/usage/identities/9007199254740993/errors')
    expect(parsed.searchParams.get('cursor')).toBe('cursor value')
    expect(parsed.searchParams.get('page_size')).toBe('50')
    expect(init).toMatchObject({ credentials: 'include', signal, cache: 'no-store' })
  })
})
