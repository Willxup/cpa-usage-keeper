import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { ApiError } from '@/lib/api'
import { buildCredentialQuotaStateMap, quotaRefreshDisplayError, quotaResetDisplayError, runQuotaResetForAuthIndex } from './useCredentialsTabData'
import { CREDENTIAL_PAGES_REFRESH_INTERVAL_MS } from './useCredentialPages'
import { buildQuotaCacheAuthIndexesKey, QUOTA_CACHE_REFRESH_INTERVAL_MS } from './useQuotaCache'
import { buildQuotaRefreshSubmissionUpdate, buildQuotaRefreshTaskErrorUpdate } from './useQuotaRefreshTasks'

const credentialsTabDataSource = readFileSync(new URL('./useCredentialsTabData.ts', import.meta.url), 'utf8').replace(/\r\n/g, '\n')
const quotaCacheSource = readFileSync(new URL('./useQuotaCache.ts', import.meta.url), 'utf8').replace(/\r\n/g, '\n')

describe('Credentials polling intervals', () => {
  it('keeps list data on a 1 minute refresh interval', () => {
    expect(CREDENTIAL_PAGES_REFRESH_INTERVAL_MS).toBe(60 * 1000)
  })

  it('keeps quota cache on a 1 minute refresh interval', () => {
    expect(QUOTA_CACHE_REFRESH_INTERVAL_MS).toBe(60 * 1000)
  })
})

describe('buildQuotaCacheAuthIndexesKey', () => {
  it('keeps equal auth index lists stable across array references', () => {
    expect(buildQuotaCacheAuthIndexesKey(['auth-1', 'auth-2'])).toBe(buildQuotaCacheAuthIndexesKey(['auth-1', 'auth-2']))
  })

  it('changes when auth index contents or order changes', () => {
    expect(buildQuotaCacheAuthIndexesKey(['auth-1', 'auth-2'])).not.toBe(buildQuotaCacheAuthIndexesKey(['auth-2', 'auth-1']))
  })
})

describe('useQuotaCache interval lifecycle', () => {
  it('does not register the cache interval while disabled', () => {
    const start = quotaCacheSource.indexOf('useEffect(() => {')
    const end = quotaCacheSource.indexOf('const intervalID = window.setInterval')

    expect(start).toBeGreaterThanOrEqual(0)
    expect(end).toBeGreaterThan(start)

    const beforeInterval = quotaCacheSource.slice(start, end)
    expect(beforeInterval).toContain('if (!enabled)')
    expect(beforeInterval).toContain('return')
  })
})

describe('Credentials quota inspection cache refresh', () => {
  it('refreshes identities and quota cache together for manual page refresh', () => {
    expect(credentialsTabDataSource).toContain('const refreshCredentialPages = credentialPages.refresh')
    expect(credentialsTabDataSource).toMatch(/const\s+refresh\s*=\s*useCallback\(\s*async\s*\(\)\s*=>\s*\{[\s\S]*refreshCredentialPages\(\)[\s\S]*refreshQuotaCache\(\)[\s\S]*\}/)
    expect(credentialsTabDataSource).toMatch(/refresh:\s*refresh,/)
  })

  it('refreshes the current Auth Files quota cache when inspection completes', () => {
    expect(credentialsTabDataSource).toContain('refreshQuotaCache')
    expect(credentialsTabDataSource).toMatch(/useQuotaInspection\(\{[\s\S]*?onInspectionCompleted:\s*refreshQuotaCache[\s\S]*?\}\)/)
  })

  it('lets completed cache quota clear stale row refresh failures after inspection', () => {
    const states = buildCredentialQuotaStateMap(
      {},
      { 'auth-1': { refreshStatus: 'failed', error: 'HTTP 401: stale failure' } },
      { 'auth-1': { id: 'auth-1', quota: [{ key: 'rate_limit.primary_window', label: '5h' }] } },
    )

    expect(states.get('auth-1')).toEqual({
      quotaLoading: false,
      quotaError: undefined,
      refreshStatus: undefined,
      quotaResetting: false,
    })
  })

  it('keeps reset loading separate from quota panel errors', () => {
    const states = buildCredentialQuotaStateMap(
      {},
      { 'auth-1': { error: 'refresh failed' } },
      {},
      { 'auth-1': { quotaResetting: true } },
    )

    expect(states.get('auth-1')).toEqual({
      quotaLoading: false,
      quotaError: 'refresh failed',
      refreshStatus: undefined,
      quotaResetting: true,
    })
  })

  it('keeps reset loading separate from quota panel loading', () => {
    const states = buildCredentialQuotaStateMap(
      {},
      {},
      {},
      { 'auth-1': { quotaResetting: true } },
    )

    expect(states.get('auth-1')).toEqual({
      quotaLoading: false,
      quotaError: undefined,
      refreshStatus: undefined,
      quotaResetting: true,
    })
  })
})

describe('quotaRefreshDisplayError', () => {
  it('turns refresh rejection codes into friendly messages', () => {
    expect(quotaRefreshDisplayError('duplicate')).toBe('Quota refresh is already running for this credential.')
    expect(quotaRefreshDisplayError('duplicate_request')).toBe('This credential was already included in the refresh request.')
    expect(quotaRefreshDisplayError('not_auth_file')).toBe('Quota refresh only supports local auth files.')
    expect(quotaRefreshDisplayError('unsupported')).toBe('Quota refresh is not supported for this credential type.')
  })

  it('keeps backend friendly refresh failures displayable', () => {
    expect(quotaRefreshDisplayError('Quota refresh timed out. Please try again later.')).toBe('Quota refresh timed out. Please try again later.')
  })
})

describe('buildQuotaRefreshSubmissionUpdate', () => {
  it('keeps duplicate refresh rejections in the polling queue', () => {
    const update = buildQuotaRefreshSubmissionUpdate({
      tasks: [{ authIndex: 'auth-1' }],
      rejected: [
        { authIndex: 'auth-2', error: 'duplicate' },
        { authIndex: 'auth-3', error: 'duplicate_request' },
      ],
      accepted: 1,
      skipped: 2,
      limit: 3,
    }, 'batch')

    expect(update.pendingTasks).toEqual([
      { authIndex: 'auth-1', source: 'batch' },
      { authIndex: 'auth-2', source: 'batch' },
    ])
    expect(update.stateUpdates['auth-2']).toEqual({ refreshStatus: 'queued', error: undefined })
    expect(update.stateUpdates['auth-3']).toEqual({ refreshStatus: 'failed', error: 'This credential was already included in the refresh request.' })
  })
})

describe('buildQuotaRefreshTaskErrorUpdate', () => {
  it('settles 401 polling failures and asks the page to re-authenticate', () => {
    let authRequiredCalls = 0

    const update = buildQuotaRefreshTaskErrorUpdate('auth-1', new ApiError('unauthorized', 401), () => {
      authRequiredCalls += 1
    })

    expect(authRequiredCalls).toBe(1)
    expect(update).toEqual({
      authIndex: 'auth-1',
      settled: true,
      stateUpdate: {
        refreshStatus: 'failed',
        error: 'Please sign in again to refresh quota.',
      },
    })
  })
})

describe('quotaResetDisplayError', () => {
  it('always returns the generic localized reset failure message', () => {
    expect(quotaResetDisplayError()).toBe('Quota reset failed. Please try again later.')
  })
})

describe('runQuotaResetForAuthIndex', () => {
  it('refreshes quota only after reset succeeds', async () => {
    const calls: string[] = []
    const outcome = await runQuotaResetForAuthIndex('auth-1', {
      resetUsageQuota: async () => {
        calls.push('reset')
      },
      refreshQuotaForAuthIndex: async () => {
        calls.push('refresh')
      },
    })

    expect(outcome).toEqual({ kind: 'success' })
    expect(calls).toEqual(['reset', 'refresh'])
  })

  it('keeps reset successful when the follow-up quota refresh fails', async () => {
    const calls: string[] = []
    const outcome = await runQuotaResetForAuthIndex('auth-1', {
      resetUsageQuota: async () => {
        calls.push('reset')
      },
      refreshQuotaForAuthIndex: async () => {
        calls.push('refresh')
        throw new Error('refresh failed')
      },
    })

    expect(outcome).toEqual({ kind: 'success' })
    expect(calls).toEqual(['reset', 'refresh'])
  })

  it('does not refresh quota when reset fails', async () => {
    const refreshCalls: string[] = []
    const outcome = await runQuotaResetForAuthIndex('auth-1', {
      resetUsageQuota: async () => {
        throw new ApiError('quota_reset_failed', 429)
      },
      refreshQuotaForAuthIndex: async () => {
        refreshCalls.push('refresh')
      },
    })

    expect(outcome).toEqual({ kind: 'error', message: 'Quota reset failed. Please try again later.' })
    expect(refreshCalls).toEqual([])
  })

  it('treats provider 401 mapped to 502 as a reset failure instead of dashboard auth logout', async () => {
    const refreshCalls: string[] = []
    const outcome = await runQuotaResetForAuthIndex('auth-1', {
      resetUsageQuota: async () => {
        throw new ApiError('quota_reset_failed', 502)
      },
      refreshQuotaForAuthIndex: async () => {
        refreshCalls.push('refresh')
      },
    })

    expect(outcome).toEqual({ kind: 'error', message: 'Quota reset failed. Please try again later.' })
    expect(refreshCalls).toEqual([])
  })

  it('treats dashboard session expiry as a reset failure notice in this action', async () => {
    const outcome = await runQuotaResetForAuthIndex('auth-1', {
      resetUsageQuota: async () => {
        throw new ApiError('unauthorized', 401)
      },
      refreshQuotaForAuthIndex: async () => undefined,
    })

    expect(outcome).toEqual({ kind: 'error', message: 'Quota reset failed. Please try again later.' })
  })
})

describe('useCredentialsTabData quota response contract', () => {
  it('narrows reset callback dependencies to refreshQuotaForAuthIndex and notice handler', () => {
    expect(credentialsTabDataSource).toMatch(/}, \[onNotice, refreshQuotaForAuthIndex\]\)/)
  })

  it('routes reset outcomes through the shared helper and top notice', () => {
    expect(credentialsTabDataSource).toContain('runQuotaResetForAuthIndex(authIndex, {')
    expect(credentialsTabDataSource).toContain("onNotice?.('error', outcome.message)")
    expect(credentialsTabDataSource).not.toContain("onAuthRequired?.()")
    expect(credentialsTabDataSource).not.toContain('quotaResetError')
  })
})
