import { describe, expect, it } from 'vitest'
import { ApiError } from '@/lib/api'
import { buildCredentialQuotaStateMap, quotaRefreshDisplayError, quotaResetDisplayError, runQuotaResetForAuthIndex } from '../useCredentialsTabData'
import { CREDENTIAL_PAGES_REFRESH_INTERVAL_MS, mergeUsageIdentityAliasUpdate } from '../useCredentialPages'
import { buildQuotaCacheAuthIndexesKey, QUOTA_CACHE_REFRESH_INTERVAL_MS } from '../useQuotaCache'
import { buildQuotaRefreshSubmissionUpdate, buildQuotaRefreshTaskErrorUpdate } from '../useQuotaRefreshTasks'
import type { UsageIdentity } from '@/lib/types'

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

describe('Credentials quota inspection cache refresh', () => {
  it('keeps reset loading separate from quota panel errors', () => {
    const states = buildCredentialQuotaStateMap(
      { 'auth-1': { error: 'refresh failed' } },
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

describe('mergeUsageIdentityAliasUpdate', () => {
  it('updates alias display fields without dropping cached credential health', () => {
    const credentialHealth = {
      window_seconds: 18_000,
      bucket_seconds: 600,
      window_start: '2026-05-10T05:30:00Z',
      window_end: '2026-05-10T10:30:00Z',
      total_success: 0,
      total_failure: 0,
      success_rate: 0,
      input_tokens: 120,
      cache_read_tokens: 45,
      buckets: [],
    }
    const current = {
      id: '1',
      auth_type: 1,
      name: 'Original Auth',
      displayName: 'Original Auth',
      alias: null,
      credential_health: credentialHealth,
    } as UsageIdentity
    const updated = {
      id: '1',
      auth_type: 1,
      name: 'Original Auth',
      displayName: 'Friendly 🚀',
      alias: 'Friendly 🚀',
    } as UsageIdentity

    const merged = mergeUsageIdentityAliasUpdate(current, updated)

    expect(merged.displayName).toBe('Friendly 🚀')
    expect(merged.alias).toBe('Friendly 🚀')
    expect(merged.credential_health).toBe(credentialHealth)
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
        return { authIndex: 'auth-1' }
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
        return { authIndex: 'auth-1' }
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


describe('Claude manual quota reset outcomes', () => {
  it('does not refresh or retry when the official result is unknown or refused', async () => {
    for (const code of ['unknown', 'not_limited', 'cooldown', 'ineligible', 'unavailable', 'rate_limited', 'auth_error']) {
      const calls: string[] = []
      const result = { authIndex: 'claude', code }
      const outcome = await runQuotaResetForAuthIndex('claude', {
        grantId: 'spring',
        resetUsageQuota: async () => { calls.push('claim'); return result },
        refreshQuotaForAuthIndex: async () => { calls.push('refresh') },
      })
      expect(calls).toEqual(['claim'])
      expect(outcome.kind).toBe('warning')
      expect(outcome.result).toEqual(result)
    }
  })
  it('reports a lost management response as unknown without a quota refresh', async () => {
    let refreshed = false
    const outcome = await runQuotaResetForAuthIndex('claude', {
      grantId: 'spring',
      resetUsageQuota: async () => { throw new Error('connection lost') },
      refreshQuotaForAuthIndex: async () => { refreshed = true },
    })
    expect(refreshed).toBe(false)
    expect(outcome.result?.code).toBe('unknown')
    expect('message' in outcome ? outcome.message : '').toContain('check quota before deciding whether to submit again')
  })
  it('uses the existing recovery warning and single-row refresh after confirmed success', async () => {
    const calls: string[] = []
    const result = { authIndex: 'claude', code: 'reset', recoveryFailed: true }
    const outcome = await runQuotaResetForAuthIndex('claude', {
      grantId: 'spring',
      resetUsageQuota: async () => { calls.push('claim'); return result },
      refreshQuotaForAuthIndex: async () => { calls.push('refresh') },
    })
    expect(calls).toEqual(['claim', 'refresh'])
    expect(outcome.kind).toBe('warning')
    expect(outcome.result).toEqual(result)
    expect('message' in outcome ? outcome.message : '').toContain('CPA')
  })
})
