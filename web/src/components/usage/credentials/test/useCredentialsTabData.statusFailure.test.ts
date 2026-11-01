import { describe, expect, it } from 'vitest'
import { ApiError } from '@/lib/api'
import { resolveCredentialStatusFailure } from '../useCredentialsTabData'

describe('resolveCredentialStatusFailure', () => {
  it('refreshes the list after a 404 because the local page is stale', () => {
    const failure = resolveCredentialStatusFailure('credential', new ApiError('credential not found', 404))

    expect(failure.refresh).toBe(true)
    expect(failure.noticeKey).toBe('usage_stats.credentials_status_stale_target')
    expect(failure.authRequired).toBe(false)
  })

  it('explains the multi-account file rule for credential conflicts without refreshing', () => {
    const failure = resolveCredentialStatusFailure('credential', new ApiError('credential status target cannot be changed on its own', 409))

    expect(failure.refresh).toBe(false)
    expect(failure.noticeKey).toBe('usage_stats.credentials_status_conflict_credential')
  })

  it('explains the unsupported provider type for AI provider conflicts without refreshing', () => {
    const failure = resolveCredentialStatusFailure('ai-provider', new ApiError('credential status is not supported', 409))

    expect(failure.refresh).toBe(false)
    expect(failure.noticeKey).toBe('usage_stats.credentials_status_conflict_ai_provider')
  })

  it('asks for sign-in on 401', () => {
    expect(resolveCredentialStatusFailure('credential', new ApiError('unauthorized', 401))).toEqual({
      authRequired: true,
      noticeKey: 'usage_stats.credentials_status_update_failed',
      refresh: false,
    })
  })

  it('explains a changed configuration without retrying or refreshing automatically', () => {
    expect(resolveCredentialStatusFailure('ai-provider', new ApiError('credential configuration changed', 412))).toEqual({
      authRequired: false,
      noticeKey: 'usage_stats.credentials_config_changed',
      refresh: false,
    })
  })

  it('keeps retry wording for unexpected failures', () => {
    expect(resolveCredentialStatusFailure('credential', new Error('boom'))).toEqual({
      authRequired: false,
      noticeKey: 'usage_stats.credentials_status_update_failed',
      refresh: false,
    })
  })
})
