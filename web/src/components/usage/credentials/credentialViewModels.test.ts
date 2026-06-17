import { describe, expect, it, vi } from 'vitest'
import type { UsageIdentity, UsageQuotaCheckResponse, UsageQuotaRow } from '@/lib/types'
import {
  CREDENTIALS_PAGE_SIZE,
  buildAiProviderCredentialRows,
  buildAuthFileCredentialRows,
  paginateCredentials,
  selectQuotaEligibleAuthIndexes,
  splitCredentialIdentities,
} from './credentialViewModels'


function quotaResponse(authIndex: string, quota: UsageQuotaRow[], rateLimitResetCreditsAvailableCount?: number | null): UsageQuotaCheckResponse {
  return {
    id: authIndex,
    quota,
    rateLimitResetCreditsAvailableCount,
  }
}

function identity(overrides: Partial<UsageIdentity>): UsageIdentity {
  return {
    id: overrides.id ?? '1',
    name: overrides.name ?? '',
    auth_type: overrides.auth_type ?? 1,
    auth_type_name: overrides.auth_type_name ?? 'Auth File',
    identity: overrides.identity ?? 'auth-1',
    type: overrides.type ?? 'claude',
    provider: overrides.provider ?? 'claude',
    priority: overrides.priority,
    plan_type: overrides.plan_type,
    total_requests: overrides.total_requests ?? 0,
    success_count: overrides.success_count ?? 0,
    failure_count: overrides.failure_count ?? 0,
    input_tokens: overrides.input_tokens ?? 0,
    output_tokens: overrides.output_tokens ?? 0,
    reasoning_tokens: overrides.reasoning_tokens ?? 0,
    cached_tokens: overrides.cached_tokens ?? 0,
    total_tokens: overrides.total_tokens ?? 0,
    last_aggregated_usage_event_id: overrides.last_aggregated_usage_event_id ?? '0',
    first_used_at: overrides.first_used_at,
    last_used_at: overrides.last_used_at,
    stats_updated_at: overrides.stats_updated_at,
    credential_health: overrides.credential_health,
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
      identity({ id: '1', auth_type: 1, identity: 'auth-file' }),
      identity({ id: '2', auth_type: 2, identity: 'api-key' }),
      identity({ id: '3', auth_type: 1, identity: 'deleted-auth-file', is_deleted: true }),
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
    const quotas = new Map<string, UsageQuotaCheckResponse>([
      ['auth-1', quotaResponse('auth-1', [
        { key: 'rate_limit.primary_window', planType: 'pro' },
      ])],
    ])

    const rows = buildAuthFileCredentialRows([
      identity({ identity: 'auth-1', plan_type: 'plus' }),
    ], quotas)

    expect(rows[0].planTypeLabel).toBe('Pro')
    expect(rows[0].planTypeTone).toBe('pro')
  })

  it('formats unknown refreshed quota plan types in the frontend', () => {
    const quotas = new Map<string, UsageQuotaCheckResponse>([
      ['auth-1', quotaResponse('auth-1', [
        { key: 'rate_limit.primary_window', planType: ' enterprise ' },
      ])],
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
      identity({ id: '1', auth_type: 1, identity: 'active-auth-file' }),
      identity({ id: '2', auth_type: 1, identity: 'deleted-auth-file', is_deleted: true }),
      identity({ id: '3', auth_type: 2, identity: 'api-key' }),
    ]

    expect(selectQuotaEligibleAuthIndexes(rows)).toEqual(['active-auth-file'])
  })

  it('paginates credentials with a fixed page size of ten', () => {
    const identities = Array.from({ length: 25 }, (_, index) => identity({ id: String(index + 1), identity: `auth-${index + 1}` }))

    const firstPage = paginateCredentials(identities, 1)
    const thirdPage = paginateCredentials(identities, 3)

    expect(CREDENTIALS_PAGE_SIZE).toBe(10)
    expect(firstPage.items).toHaveLength(10)
    expect(firstPage.total).toBe(25)
    expect(firstPage.totalPages).toBe(3)
    expect(thirdPage.items.map((item) => item.identity)).toEqual(['auth-21', 'auth-22', 'auth-23', 'auth-24', 'auth-25'])
  })

  it('builds auth file rows with displayable quota bars and ignores quota without progress', () => {
    const quotas = new Map<string, UsageQuotaCheckResponse>([
      ['auth-1', quotaResponse('auth-1', [
        { key: 'rate_limit.primary_window', label: '5h', remainingFraction: 0.72, remaining: 72, resetAt: '2026-05-09T12:00:00Z', window_usage_tokens: 1_500_000, window_usage_cost: 12.34 },
        { key: 'rate_limit.secondary_window', label: 'Weekly', used: 40, limit: 100 },
        { key: 'rate_limit.gpt_codex_spark_5h', label: 'GPT-5.3-Codex-Spark 5h', usedPercent: 83 },
        { key: 'code_assist.current_tier.GOOGLE_ONE_AI', label: 'Code Assist Credit', remaining: 10 },
      ])],
    ])

    const rows = buildAuthFileCredentialRows([identity({ identity: 'auth-1', displayName: 'Claude Auth', total_requests: 10, success_count: 9, input_tokens: 1000, cached_tokens: 250, total_tokens: 1500 })], quotas)

    expect(rows[0].displayName).toBe('Claude Auth')
    expect(rows[0].typeLabel).toBe('claude')
    expect(rows[0].totalRequests).toBe(10)
    expect(rows[0].successCount).toBe(9)
    expect(rows[0].failureCount).toBe(0)
    expect(rows[0].totalTokens).toBe(1500)
    expect(rows[0].cacheRate).toBe(25)
    expect(rows[0].displayQuotas.map((quota) => quota.label)).toEqual(['5h', 'Weekly', 'GPT-5.3-Codex-Spark 5h'])
    expect(rows[0].displayQuotas[0]).toMatchObject({
      percent: 72,
      percentKind: 'remaining',
      barPercent: 72,
      status: 'ok',
      windowUsage: { tokens: '1.50M', cost: '$12.34' },
    })
    expect(rows[0].displayQuotas[1]).toMatchObject({
      percent: 40,
      percentKind: 'used',
      barPercent: 60,
    })
    expect(rows[0].displayQuotas[2]).toMatchObject({
      percent: 83,
      percentKind: 'used',
      barPercent: 17,
      status: 'danger',
    })
  })

  it('formats zero quota window cost with two decimals', () => {
    const quotas = new Map<string, UsageQuotaCheckResponse>([
      ['auth-1', quotaResponse('auth-1', [
        { key: 'rate_limit.primary_window', label: '5h', remainingFraction: 0.72, window_usage_tokens: 0, window_usage_cost: 0 },
      ])],
    ])

    const rows = buildAuthFileCredentialRows([identity({ identity: 'auth-1' })], quotas)

    expect(rows[0].displayQuotas[0].windowUsage).toEqual({ tokens: '0', cost: '$0.00' })
  })

  it('formats provider quota window usage with fixed compact units and US dollar decimals', () => {
    const quotas = new Map<string, UsageQuotaCheckResponse>([
      ['auth-1', quotaResponse('auth-1', [
        { key: 'rate_limit.primary_window', label: '5h', usedPercent: 3, window_usage_tokens: 11_368_055, window_usage_cost: 14.83442025 },
        { key: 'additional_rate_limits.GPT-5.3-Codex-Spark.primary_window', label: 'GPT-5.3-Codex-Spark 5h', usedPercent: 0, window_usage_tokens: 393_311, window_usage_cost: 0.458464 },
      ])],
    ])

    const rows = buildAuthFileCredentialRows([identity({ identity: 'auth-1' })], quotas)

    expect(rows[0].displayQuotas.map((quota) => quota.windowUsage)).toEqual([
      { tokens: '11.37M', cost: '$14.83' },
      { tokens: '393.31K', cost: '$0.46' },
    ])
  })

  it('formats xai billing quota cents as dollar spend without token window usage', () => {
    const quotas = new Map<string, UsageQuotaCheckResponse>([
      ['xai-auth', quotaResponse('xai-auth', [
        { key: 'billing.monthly', label: 'Monthly Spend', scope: 'billing', metric: 'usd_cents', used: 167, limit: 20000, remaining: 19833, usedPercent: 0.835, window: { seconds: 2592000 }, resetAt: '2026-07-01T00:00:00+00:00' },
      ])],
    ])

    const rows = buildAuthFileCredentialRows([identity({ identity: 'xai-auth', type: 'xai', provider: 'xAI' })], quotas)

    expect(rows[0].displayQuotas[0]).toMatchObject({
      label: 'Monthly Spend',
      percent: 0.835,
      percentKind: 'used',
      barPercent: 99.165,
      billingUsage: {
        used: '$1.67',
        limit: '$200.00',
        remaining: '$198.33',
      },
      windowUsage: undefined,
      windowUsageEstimate: undefined,
    })
  })

  it('estimates quota window usage only from positive current usage and a partial used percent', () => {
    const quotas = new Map<string, UsageQuotaCheckResponse>([
      ['auth-1', quotaResponse('auth-1', [
        { key: 'rate_limit.primary_window', label: '5h', usedPercent: 25, window_usage_tokens: 1_000_000, window_usage_cost: 2.5 },
        { key: 'rate_limit.secondary_window', label: 'Weekly', remainingFraction: 0.75, window_usage_tokens: 500_000, window_usage_cost: 1 },
      ])],
    ])

    const rows = buildAuthFileCredentialRows([identity({ identity: 'auth-1' })], quotas)

    expect(rows[0].displayQuotas.map((quota) => quota.windowUsageEstimate)).toEqual([
      { tokens: '4.00M', cost: '$10.00' },
      { tokens: '2.00M', cost: '$4.00' },
    ])
  })

  it('keeps current quota window usage when the used percent or current cost cannot be estimated', () => {
    const quotas = new Map<string, UsageQuotaCheckResponse>([
      ['auth-1', quotaResponse('auth-1', [
        { key: 'rate_limit.zero_window', label: 'Zero', usedPercent: 0, window_usage_tokens: 393_311, window_usage_cost: 0.458464 },
        { key: 'rate_limit.full_window', label: 'Full', usedPercent: 100, window_usage_tokens: 1_000, window_usage_cost: 1 },
        { key: 'rate_limit.free_window', label: 'Free', usedPercent: 50, window_usage_tokens: 1_000, window_usage_cost: 0 },
        { key: 'rate_limit.empty_window', label: 'Empty', usedPercent: 50, window_usage_tokens: 0, window_usage_cost: 1 },
      ])],
    ])

    const rows = buildAuthFileCredentialRows([identity({ identity: 'auth-1' })], quotas)

    expect(rows[0].displayQuotas.map((quota) => quota.windowUsage)).toEqual([
      { tokens: '393.31K', cost: '$0.46' },
      { tokens: '1.00K', cost: '$1.00' },
      { tokens: '1.00K', cost: '$0.00' },
      { tokens: '0', cost: '$1.00' },
    ])
    expect(rows[0].displayQuotas.map((quota) => quota.windowUsageEstimate)).toEqual([
      undefined,
      undefined,
      undefined,
      undefined,
    ])
  })

  it('uses an explicit US locale for quota window cost formatting', () => {
    const numberFormatSpy = vi.spyOn(Intl, 'NumberFormat')
    try {
      const quotas = new Map<string, UsageQuotaCheckResponse>([
        ['auth-1', quotaResponse('auth-1', [
          { key: 'rate_limit.primary_window', label: '5h', usedPercent: 3, window_usage_tokens: 11_368_055, window_usage_cost: 14.83442025 },
        ])],
      ])

      buildAuthFileCredentialRows([identity({ identity: 'auth-1' })], quotas)

      expect(numberFormatSpy).toHaveBeenCalledWith('en-US', expect.objectContaining({
        style: 'currency',
        currency: 'USD',
        minimumFractionDigits: 2,
        maximumFractionDigits: 2,
      }))
    } finally {
      numberFormatSpy.mockRestore()
    }
  })

  it('uses normalized input token semantics for auth file cache rate', () => {
    const rows = buildAuthFileCredentialRows([
      identity({ identity: 'auth-claude', type: 'claude', input_tokens: 1000, cached_tokens: 600 }),
    ])

    expect(rows[0].cacheRate).toBe(60)
  })

  it('classifies quota bar colors at 50 and 20 percent remaining thresholds', () => {
    const quotas = new Map<string, UsageQuotaCheckResponse>([
      ['green-auth', quotaResponse('green-auth', [{ key: 'rate_limit.primary_window', label: '5h', remainingFraction: 0.5 }])],
      ['yellow-auth', quotaResponse('yellow-auth', [{ key: 'rate_limit.primary_window', label: '5h', remainingFraction: 0.49 }])],
      ['red-auth', quotaResponse('red-auth', [{ key: 'rate_limit.primary_window', label: '5h', remainingFraction: 0.19 }])],
    ])

    const rows = buildAuthFileCredentialRows([
      identity({ identity: 'green-auth' }),
      identity({ identity: 'yellow-auth' }),
      identity({ identity: 'red-auth' }),
    ], quotas)

    expect(rows.map((row) => row.displayQuotas[0]?.status)).toEqual(['ok', 'warning', 'danger'])
  })

  it('uses quota window duration instead of raw key when classifying Codex windows', () => {
    const quotas = new Map<string, UsageQuotaCheckResponse>([
      ['auth-1', quotaResponse('auth-1', [
        { key: 'rate_limit.primary_window', label: '5h', usedPercent: 10, window: { seconds: 604800 } },
      ])],
    ])

    const rows = buildAuthFileCredentialRows([identity({ identity: 'auth-1' })], quotas)

    expect(rows[0].displayQuotas[0]?.label).toBe('Weekly')
    expect(rows[0].displayQuotas[0]?.barPercent).toBe(90)
  })

  it('uses monthly quota labels for monthly Codex windows', () => {
    const quotas = new Map<string, UsageQuotaCheckResponse>([
      ['auth-1', quotaResponse('auth-1', [
        { key: 'rate_limit.primary_window', label: '5h', usedPercent: 20, window: { seconds: 2628000 } },
        { key: 'code_review_rate_limit.primary_window', label: 'Code Review Weekly', usedPercent: 40, window: { seconds: 2592000 } },
        { key: 'additional_rate_limits.GPT-5.3-Codex-Spark.primary_window', label: 'GPT-5.3-Codex-Spark 5h', usedPercent: 60, window: { seconds: 2628000 } },
      ])],
    ])

    const rows = buildAuthFileCredentialRows([identity({ identity: 'auth-1' })], quotas)

    expect(rows[0].displayQuotas.map((quota) => quota.label)).toEqual(['Monthly', 'Code Review Monthly', 'GPT-5.3-Codex-Spark Monthly'])
    expect(rows[0].displayQuotas.map((quota) => quota.barPercent)).toEqual([80, 60, 40])
  })

  it('keeps unknown Codex windows displayable without showing a generic Window quota', () => {
    const quotas = new Map<string, UsageQuotaCheckResponse>([
      ['auth-1', quotaResponse('auth-1', [
        { key: 'rate_limit.primary_window', label: 'Window', usedPercent: 10, window: { seconds: 3600 } },
        { key: 'additional_rate_limits.GPT-5.3-Codex-Spark.primary_window', label: 'GPT-5.3-Codex-Spark 5h', usedPercent: 83, window: { seconds: 3600 } },
      ])],
    ])

    const rows = buildAuthFileCredentialRows([identity({ identity: 'auth-1' })], quotas)

    expect(rows[0].displayQuotas.map((quota) => quota.label)).toEqual(['Primary', 'GPT-5.3-Codex-Spark Primary'])
    expect(rows[0].displayQuotas.map((quota) => quota.barPercent)).toEqual([90, 17])
  })

  it('maps legacy generic Window quota labels by Codex window role even without seconds', () => {
    const quotas = new Map<string, UsageQuotaCheckResponse>([
      ['auth-1', quotaResponse('auth-1', [
        { key: 'rate_limit.primary_window', label: 'Window', usedPercent: 10 },
        { key: 'code_review_rate_limit.secondary_window', label: 'Code Review Window', usedPercent: 30 },
      ])],
    ])

    const rows = buildAuthFileCredentialRows([identity({ identity: 'auth-1' })], quotas)

    expect(rows[0].displayQuotas.map((quota) => quota.label)).toEqual(['Primary', 'Code Review Secondary'])
    expect(rows[0].displayQuotas.map((quota) => quota.barPercent)).toEqual([90, 70])
  })


  it('derives auth file quota rows and reset credits from full quota responses', () => {
    const quotas = new Map<string, UsageQuotaCheckResponse>([
      ['auth-1', quotaResponse('auth-1', [{ key: 'rate_limit.primary_window', label: '5h', usedPercent: 20 }], 2)],
      ['auth-2', quotaResponse('auth-2', [{ key: 'rate_limit.primary_window', label: '5h', usedPercent: 20 }], 0)],
    ])

    const rows = buildAuthFileCredentialRows([
      identity({ identity: 'auth-1' }),
      identity({ identity: 'auth-2' }),
    ], quotas)

    expect(rows.map((row) => row.quotaResetCreditsAvailableCount)).toEqual([2, 0])
    expect(rows[0].quota).toEqual(quotas.get('auth-1')?.quota)
  })

  it('builds compact priority labels for auth files and AI providers', () => {
    const authFileRows = buildAuthFileCredentialRows([
      identity({ identity: 'priority-auth', priority: 5 }),
      identity({ identity: 'zero-priority-auth', priority: 0 }),
      identity({ identity: 'default-auth' }),
    ])
    const aiProviderRows = buildAiProviderCredentialRows([
      identity({ auth_type: 2, identity: 'priority-provider', priority: 7 }),
    ])

    expect(authFileRows.map((row) => row.priorityLabel)).toEqual(['P5', 'P0', undefined])
    expect(aiProviderRows[0].priorityLabel).toBe('P7')
  })

  it('keeps credential health snapshots on Auth Files and AI Provider rows', () => {
    const credentialHealth = {
      window_seconds: 18_000,
      bucket_seconds: 600,
      window_start: '2026-05-10T05:30:00Z',
      window_end: '2026-05-10T10:30:00Z',
      total_success: 2,
      total_failure: 1,
      success_rate: 66.6667,
      buckets: [],
    }

    const authFileRows = buildAuthFileCredentialRows([
      identity({ identity: 'auth-1', credential_health: credentialHealth }),
    ])
    const aiProviderRows = buildAiProviderCredentialRows([
      identity({ auth_type: 2, identity: 'provider-1', credential_health: credentialHealth }),
    ])

    expect(authFileRows[0].credentialHealth).toBe(credentialHealth)
    expect(aiProviderRows[0].credentialHealth).toBe(credentialHealth)
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
    expect('displayQuotas' in rows[0]).toBe(false)
  })
})
