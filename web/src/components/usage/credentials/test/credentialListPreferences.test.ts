import { describe, expect, it, vi } from 'vitest'
import {
  CREDENTIAL_LIST_PREFERENCES_STORAGE_KEYS,
  defaultCredentialListPreferences,
  loadCredentialListPreferences,
  normalizeCredentialListPreferences,
  persistCredentialListPreferences,
  readCredentialListContentMode,
  persistCredentialListContentMode,
} from '../credentialListPreferences'

const createStorage = (value: string | null = null) => ({
  getItem: vi.fn(() => value),
  setItem: vi.fn(),
})

const storedValue = (storage: ReturnType<typeof createStorage>) => (
  JSON.parse(String(storage.setItem.mock.calls.at(-1)?.[1]))
)

describe('credential list preferences', () => {
  it('keeps each section on its own default sort', () => {
    expect(defaultCredentialListPreferences('credentials')).toEqual({
      viewMode: 'cards',
      sort: 'priority',
      pageSize: 10,
      providerFilter: 'all',
    })
    expect(defaultCredentialListPreferences('ai-provider')).toEqual({
      viewMode: 'cards',
      sort: 'total_requests',
      pageSize: 10,
      providerFilter: 'all',
    })
  })

  it('restores a complete saved selection', () => {
    const storage = createStorage(JSON.stringify({
      version: 1,
      sort: 'last_used_at',
      pageSize: 50,
      providerFilter: 'codex',
    }))

    expect(loadCredentialListPreferences('ai-provider', storage)).toEqual({
      viewMode: 'cards',
      sort: 'last_used_at',
      pageSize: 50,
      providerFilter: 'codex',
    })
    expect(storage.getItem).toHaveBeenCalledWith(CREDENTIAL_LIST_PREFERENCES_STORAGE_KEYS['ai-provider'])
  })

  it('drops values that are not on the request whitelist', () => {
    expect(normalizeCredentialListPreferences('credentials', {
      version: 1,
      sort: 'total_cost',
      pageSize: 1000,
      providerFilter: 'not-a-provider',
    })).toEqual({
      viewMode: 'cards',
      sort: 'priority',
      pageSize: 10,
      providerFilter: 'all',
    })
  })

  it('rejects a provider filter that belongs to the other section', () => {
    // antigravity 只存在于 凭证分区，AI 供应商恢复时必须回落到 all。
    expect(normalizeCredentialListPreferences('credentials', {
      version: 1,
      providerFilter: 'antigravity',
    }).providerFilter).toBe('antigravity')
    expect(normalizeCredentialListPreferences('ai-provider', {
      version: 1,
      providerFilter: 'antigravity',
    }).providerFilter).toBe('all')
    expect(normalizeCredentialListPreferences('ai-provider', {
      version: 1,
      providerFilter: 'openai',
    }).providerFilter).toBe('openai')
  })

  it('resets everything when the stored version is missing or stale', () => {
    expect(normalizeCredentialListPreferences('ai-provider', {
      sort: 'last_used_at',
      pageSize: 50,
    })).toEqual(defaultCredentialListPreferences('ai-provider'))
    expect(normalizeCredentialListPreferences('ai-provider', {
      version: 0,
      sort: 'last_used_at',
    })).toEqual(defaultCredentialListPreferences('ai-provider'))
  })

  it('falls back to defaults for unusable storage', () => {
    expect(loadCredentialListPreferences('credentials', createStorage('not json'))).toEqual(defaultCredentialListPreferences('credentials'))
    expect(loadCredentialListPreferences('credentials', {
      getItem: () => { throw new Error('blocked') },
      setItem: vi.fn(),
    })).toEqual(defaultCredentialListPreferences('credentials'))
  })

  it('merges a single field into the existing record instead of overwriting it', () => {
    const storage = createStorage(JSON.stringify({
      version: 1,
      sort: 'last_used_at',
      pageSize: 20,
      providerFilter: 'claude',
    }))

    expect(persistCredentialListPreferences('ai-provider', { sort: 'total_tokens' }, storage)).toBe(true)
    expect(storage.setItem).toHaveBeenCalledWith(
      CREDENTIAL_LIST_PREFERENCES_STORAGE_KEYS['ai-provider'],
      expect.any(String),
    )
    expect(storedValue(storage)).toEqual({
      version: 1,
      viewMode: 'cards',
      sort: 'total_tokens',
      pageSize: 20,
      providerFilter: 'claude',
    })
  })

  it('never writes a value the loader would reject', () => {
    const storage = createStorage()
    persistCredentialListPreferences('ai-provider', {
      sort: 'total_cost' as never,
      pageSize: 33,
      providerFilter: 'antigravity',
    }, storage)

    expect(storedValue(storage)).toEqual({
      version: 1,
      viewMode: 'cards',
      sort: 'total_requests',
      pageSize: 10,
      providerFilter: 'all',
    })
  })

  it('preserves v1 selections for missing and invalid layout fields', () => {
    const existing = { version: 1, sort: 'last_used_at', pageSize: 50, providerFilter: 'claude' }
    const expected = { sort: 'last_used_at', pageSize: 50, providerFilter: 'claude', viewMode: 'cards' }
    expect(normalizeCredentialListPreferences('credentials', existing)).toEqual(expected)
    expect(normalizeCredentialListPreferences('credentials', { ...existing, viewMode: 'invalid' })).toEqual(expected)
    expect(normalizeCredentialListPreferences('credentials', { ...existing, viewMode: 'list' })).toEqual({ ...expected, viewMode: 'list' })
  })

  it('keeps historical list content mode separate from layout mode', () => {
    const storage = createStorage('health')
    expect(readCredentialListContentMode(storage)).toBe('health')
    persistCredentialListContentMode('quota', storage)
    expect(storage.setItem).toHaveBeenCalledWith('cpa.credentials.displayMode', 'quota')
    expect(readCredentialListContentMode(createStorage('list'))).toBe('quota')
    expect(readCredentialListContentMode({ getItem: () => { throw Error('blocked') }, setItem: vi.fn() })).toBe('quota')
  })

  it('reports a failed write without throwing', () => {
    expect(persistCredentialListPreferences('credentials', { pageSize: 20 }, {
      getItem: vi.fn(() => null),
      setItem: () => { throw new Error('blocked') },
    })).toBe(false)
  })
})
