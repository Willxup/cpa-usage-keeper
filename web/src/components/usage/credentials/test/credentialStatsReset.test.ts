import { describe, expect, it } from 'vitest'
import type { UsageIdentity } from '@/lib/types'
import { buildAiProviderCredentialRows, buildAuthFileCredentialRows } from '../credentialViewModels'

describe('credential period statistics', () => {
  const identity = {
    id: '1', auth_type: 1, identity: 'fixture', total_requests: 100, success_count: 90, failure_count: 10,
    total_tokens: 5000, input_tokens: 1000, cache_read_tokens: 500,
    period_stats: { total_requests: 4, success_count: 3, failure_count: 1, total_tokens: 120, input_tokens: 80, cache_read_tokens: 20 },
  } as UsageIdentity

  it('uses period counts and recalculates both ratios for both credential pages', () => {
    for (const row of [buildAuthFileCredentialRows([identity])[0], buildAiProviderCredentialRows([identity])[0]]) {
      expect(row).toMatchObject({ totalRequests: 4, successCount: 3, failureCount: 1, totalTokens: 120, successRate: 75, cacheReadRate: 25 })
      expect(row.identity.total_requests).toBe(100)
    }
  })
  it('renders empty ratios after reset and keeps lifetime behavior without period data', () => {
    const empty = { ...identity, period_stats: { total_requests: 0, success_count: 0, failure_count: 0, total_tokens: 0, input_tokens: 0, cache_read_tokens: 0 } }
    expect(buildAiProviderCredentialRows([empty])[0]).toMatchObject({ totalRequests: 0, totalTokens: 0, successRate: null, cacheReadRate: null })
    expect(buildAiProviderCredentialRows([{ ...identity, period_stats: undefined }])[0]).toMatchObject({ totalRequests: 100, successRate: 90, cacheReadRate: 50 })
  })
})
