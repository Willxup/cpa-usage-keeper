// @vitest-environment happy-dom

import { afterEach, describe, expect, it, vi } from 'vitest'
import { getStartupStatus } from '../startup'

describe('getStartupStatus', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    delete window.__APP_BASE_PATH__
    window.history.replaceState(null, '', '/')
  })

  it('uses the public API path and preserves CPAMC request headers', async () => {
    window.__APP_BASE_PATH__ = '/cpa/'
    window.history.replaceState(null, '', '/cpa/analysis?embed=cpamc')
    const fetchMock = vi.fn().mockResolvedValue(Response.json({ phase: 'migrating', message: 'Updating data', progress: { processed_count: 3, total_count: null } }))
    vi.stubGlobal('fetch', fetchMock)
    expect(await getStartupStatus()).toMatchObject({ phase: 'migrating', progress: { processed_count: 3, total_count: null } })
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/cpa/api/v1/startup/status')
    expect(init.cache).toBe('no-store')
    expect(init.credentials).toBe('include')
    expect(new Headers(init.headers).get('X-CPA-Usage-Keeper-Embed')).toBe('cpamc')
  })

  it('rejects malformed status instead of assuming ready', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json({ phase: 'readyish', message: 'Ready', progress: null })))
    await expect(getStartupStatus()).rejects.toThrow('Invalid startup status')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json({ phase: 'ready', message: 'Ready' })))
    await expect(getStartupStatus()).rejects.toThrow('Invalid startup status')
  })
})
