import { describe, expect, it, vi } from 'vitest'
import type { UsageIdentity, UsageQuotaRow } from '@/lib/types'
import {
  CREDENTIALS_PAGE_SIZE,
  buildAiProviderCredentialRows,
  buildAuthFileCredentialRows,
  paginateCredentials,
  selectQuotaEligibleAuthIndexes,
  splitCredentialIdentities,
} from './credentialViewModels'

function identity(overrides: Partial<UsageIdentity>): UsageIdentity {
  return {
    id: overrides.id ?? 1,
    name: overrides.name ?? '',
    auth_type: overrides.auth_type ?? 1,
    auth_type_name: overrides.auth_type_name ?? 'Auth File',
    identity: overrides.identity ?? 'auth-1',
    type: overrides.type ?? 'claude',
    provider: overrides.provider ?? 'claude',
    plan_type: overrides.plan_type,
    total_requests: overrides.total_requests ?? 0,
    success_count: overrides.success_count ?? 0,
    failure_count: overrides.failure_count ?? 0,
    input_tokens: overrides.input_tokens ?? 0,
    output_tokens: overrides.output_tokens ?? 0,
    reasoning_tokens: overrides.reasoning_tokens ?? 0,
    cached_tokens: overrides.cached_tokens ?? 0,
    total_tokens: overrides.total_tokens ?? 0,
    last_aggregated_usage_event_id: overrides.last_aggregated_usage_event_id ?? 0,
    first_used_at: overrides.first_used_at,
    last_used_at: overrides.last_used_at,
    stats_updated_at: overrides.stats_updated_at,
    active_start: overrides.active_start,
    active_until: overrides.active_until,
    is_deleted: overrides.is_deleted ?? false,
    created_at: overrides.created_at ?? '2026-05-09T00:00:00Z',
    updated_at: overrides.updated_at ?? '2026-05-09T00:00:00Z',
    deleted_at: overrides.deleted_at,
    displayName: overrides.displayName,
  }
}

describe('credentialViewModels', () => {
  it('splits usage identities by auth type while keeping deleted rows for traffic display', () => {
    const groups = splitCredentialIdentities([
      identity({ id: 1, auth_type: 1, identity: 'auth-file' }),
      identity({ id: 2, auth_type: 2, identity: 'api-key' }),
      identity({ id: 3, auth_type: 1, identity: 'deleted-auth-file', is_deleted: true }),
    ])

    expect(groups.authFiles.map((item) => item.identity)).toEqual(['auth-file', 'deleted-auth-file'])
    expect(groups.aiProviders.map((item) => item.identity)).toEqual(['api-key'])
  })

  it('builds auth file plan badges from plan type with case-insensitive matching', () => {
    const rows = buildAuthFileCredentialRows([
      identity({ identity: 'free-auth', plan_type: 'free' }),
      identity({ identity: 'team-auth', plan_type: 'TEAM' }),
      identity({ identity: 'plus-auth', plan_type: 'Plus' }),
      identity({ identity: 'pro-auth', plan_type: 'chatgpt-pro-monthly' }),
    ])

    expect(rows.map((row) => [row.planTypeLabel, row.planTypeTone])).toEqual([
      ['Free', 'free'],
      ['Team', 'team'],
      ['Plus', 'plus'],
      ['Pro', 'pro'],
    ])
  })

  it('prefers refreshed quota plan type over usage identity plan type', () => {
    const quotas = new Map<string, UsageQuotaRow[]>([
      ['auth-1', [
        { key: 'rate_limit.primary_window', planType: 'pro' },
      ]],
    ])

    const rows = buildAuthFileCredentialRows([
      identity({ identity: 'auth-1', plan_type: 'plus' }),
    ], quotas)

    expect(rows[0].planTypeLabel).toBe('Pro')
    expect(rows[0].planTypeTone).toBe('pro')
  })

  it('formats unknown refreshed quota plan types in the frontend', () => {
    const quotas = new Map<string, UsageQuotaRow[]>([
      ['auth-1', [
        { key: 'rate_limit.primary_window', planType: ' enterprise ' },
      ]],
    ])

    const rows = buildAuthFileCredentialRows([
      identity({ identity: 'auth-1', plan_type: 'plus' }),
    ], quotas)

    expect(rows[0].planTypeLabel).toBe('Enterprise')
    expect(rows[0].planTypeTone).toBe('neutral')
  })

  it('builds active-until remaining days badge with zero as the minimum', () => {
    vi.setSystemTime(new Date('2026-05-10T10:00:00Z'))
    try {
      const rows = buildAuthFileCredentialRows([
        identity({ identity: 'future-auth', active_until: '2026-06-04T09:59:59Z' }),
        identity({ identity: 'expired-auth', active_until: '2026-05-09T10:00:00Z' }),
      ])

      expect(rows.map((row) => row.remainingDaysLabel)).toEqual(['25d', '0d'])
    } finally {
      vi.useRealTimers()
    }
  })

  it('selects only active current-page auth files for quota requests', () => {
    const rows = [
      identity({ id: 1, auth_type: 1, identity: 'active-auth-file' }),
      identity({ id: 2, auth_type: 1, identity: 'deleted-auth-file', is_deleted: true }),
      identity({ id: 3, auth_type: 2, identity: 'api-key' }),
    ]

    expect(selectQuotaEligibleAuthIndexes(rows)).toEqual(['active-auth-file'])
  })

  it('paginates credentials with a fixed page size of ten', () => {
    const identities = Array.from({ length: 25 }, (_, index) => identity({ id: index + 1, identity: `auth-${index + 1}` }))

    const firstPage = paginateCredentials(identities, 1)
    const thirdPage = paginateCredentials(identities, 3)

    expect(CREDENTIALS_PAGE_SIZE).toBe(10)
    expect(firstPage.items).toHaveLength(10)
    expect(firstPage.total).toBe(25)
    expect(firstPage.totalPages).toBe(3)
    expect(thirdPage.items.map((item) => item.identity)).toEqual(['auth-21', 'auth-22', 'auth-23', 'auth-24', 'auth-25'])
  })

  it('builds auth file rows with primary secondary and extra quota display data', () => {
    const quotas = new Map<string, UsageQuotaRow[]>([
      ['auth-1', [
        { key: 'rate_limit.primary_window', label: '5h', remainingFraction: 0.72, remaining: 72, resetAt: '2026-05-09T12:00:00Z' },
        { key: 'rate_limit.secondary_window', label: 'Weekly', used: 40, limit: 100 },
        { key: 'code_assist.current_tier.GOOGLE_ONE_AI', label: 'Code Assist Credit', remaining: 10 },
      ]],
    ])

    const rows = buildAuthFileCredentialRows([identity({ identity: 'auth-1', displayName: 'Claude Auth', total_requests: 10, success_count: 9, input_tokens: 1000, cached_tokens: 250, total_tokens: 1500 })], quotas)

    expect(rows[0].displayName).toBe('Claude Auth')
    expect(rows[0].typeLabel).toBe('claude')
    expect(rows[0].totalRequests).toBe(10)
    expect(rows[0].successCount).toBe(9)
    expect(rows[0].failureCount).toBe(0)
    expect(rows[0].totalTokens).toBe(1500)
    expect(rows[0].cacheRate).toBe(25)
    expect(rows[0].primaryQuota?.label).toBe('5h')
    expect(rows[0].primaryQuota?.percent).toBe(72)
    expect(rows[0].primaryQuota?.percentKind).toBe('remaining')
    expect(rows[0].primaryQuota?.barPercent).toBe(72)
    expect(rows[0].primaryQuota?.status).toBe('ok')
    expect(rows[0].secondaryQuota?.label).toBe('Weekly')
    expect(rows[0].secondaryQuota?.percent).toBe(40)
    expect(rows[0].secondaryQuota?.percentKind).toBe('used')
    expect(rows[0].secondaryQuota?.barPercent).toBe(60)
    expect(rows[0].extraQuota.map((quota) => quota.label)).toEqual(['Code Assist Credit'])
  })

  it('uses quota window duration instead of raw key when classifying Codex windows', () => {
    const quotas = new Map<string, UsageQuotaRow[]>([
      ['auth-1', [
        { key: 'rate_limit.primary_window', label: '5h', usedPercent: 10, window: { seconds: 604800 } },
      ]],
    ])

    const rows = buildAuthFileCredentialRows([identity({ identity: 'auth-1' })], quotas)

    expect(rows[0].primaryQuota).toBeUndefined()
    expect(rows[0].secondaryQuota?.label).toBe('Weekly')
    expect(rows[0].secondaryQuota?.barPercent).toBe(90)
  })

  it('does not classify unknown Codex windows as 5h or weekly', () => {
    const quotas = new Map<string, UsageQuotaRow[]>([
      ['auth-1', [
        { key: 'rate_limit.primary_window', label: '5h', usedPercent: 10, window: { seconds: 3600 } },
      ]],
    ])

    const rows = buildAuthFileCredentialRows([identity({ identity: 'auth-1' })], quotas)

    expect(rows[0].primaryQuota).toBeUndefined()
    expect(rows[0].secondaryQuota).toBeUndefined()
    expect(rows[0].extraQuota[0].label).toBe('Window')
  })

  it('builds AI provider rows without quota data', () => {
    const rows = buildAiProviderCredentialRows([
      identity({ auth_type: 2, identity: 'sk-a***1234', displayName: 'Claude API', total_requests: 4, success_count: 3, failure_count: 1 }),
    ])

    expect(rows[0].displayName).toBe('Claude API')
    expect(rows[0].maskedIdentity).toBe('sk-a***1234')
    expect(rows[0].totalRequests).toBe(4)
    expect(rows[0].successCount).toBe(3)
    expect(rows[0].failureCount).toBe(1)
    expect(rows[0].successRate).toBe(75)
    expect(rows[0].totalTokens).toBe(0)
    expect(rows[0].cacheRate).toBeNull()
    expect('primaryQuota' in rows[0]).toBe(false)
  })
})
