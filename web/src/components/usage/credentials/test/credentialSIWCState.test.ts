import { describe, expect, it } from 'vitest'
import type { UsageIdentity } from '@/lib/types'
import { credentialSIWCState, type CredentialSIWCState } from '../credentialViewModels'
import { createSectionIdentity } from './credentialSectionFixtures'

describe('credentialSIWCState', () => {
  it.each<[string, Partial<UsageIdentity>, CredentialSIWCState | null]>([
    ['unlinked with off status', { siwc_bound: false, use_siwc: false, siwc_status: 'off' }, 'not_linked'],
    ['unlinked with missing use and status', { siwc_bound: false }, 'not_linked'],
    ['unlinked with null optional fields', { siwc_bound: false, use_siwc: null, siwc_status: null, siwc_bind_supported: null, siwc_expires_at: null }, 'not_linked'],
    ['disabled with off status', { siwc_bound: true, use_siwc: false, siwc_status: 'off' }, 'not_enabled'],
    ['disabled without optional status', { siwc_bound: true, use_siwc: false }, 'not_enabled'],
    ['enabled', { siwc_bound: true, use_siwc: true, siwc_status: 'valid' }, 'enabled'],
    ['capability and expiry are not gates', { siwc_bind_supported: false, siwc_bound: true, use_siwc: true, siwc_status: 'valid', siwc_expires_at: null }, 'enabled'],
    ['invalid with bound', { siwc_bound: true, use_siwc: true, siwc_status: 'invalid' }, 'invalid'],
    ['invalid takes priority over unbound', { siwc_bound: false, use_siwc: true, siwc_status: 'invalid' }, 'invalid'],
    ['invalid even if bound is missing', { use_siwc: true, siwc_status: 'invalid' }, 'invalid'],
    ['no fields', {}, null],
    ['bound without use', { siwc_bound: true, use_siwc: null, siwc_status: 'off' }, null],
    ['enabled without status', { siwc_bound: true, use_siwc: true }, null],
    ['enabled without bound', { use_siwc: true, siwc_status: 'valid' }, null],
    ['unbound but enabled and valid', { siwc_bound: false, use_siwc: true, siwc_status: 'valid' }, null],
    ['enabled but off', { siwc_bound: true, use_siwc: true, siwc_status: 'off' }, null],
    ['disabled but valid', { siwc_bound: true, use_siwc: false, siwc_status: 'valid' }, null],
    ['disabled but invalid', { siwc_bound: true, use_siwc: false, siwc_status: 'invalid' }, null],
    ['unlinked but valid', { siwc_bound: false, use_siwc: false, siwc_status: 'valid' }, null],
    ['unknown status while unbound', { siwc_bound: false, use_siwc: false, siwc_status: 'future' }, null],
    ['unknown status while disabled', { siwc_bound: true, use_siwc: false, siwc_status: 'future' }, null],
    ['unknown status while enabled', { siwc_bound: true, use_siwc: true, siwc_status: 'future' }, null],
    ['empty status is not known', { siwc_bound: false, use_siwc: false, siwc_status: '' }, null],
    ['another credential type', { type: 'claude', siwc_bound: true, use_siwc: true, siwc_status: 'valid' }, null],
    ['codex AI provider', { auth_type: 2, siwc_bound: true, use_siwc: true, siwc_status: 'valid' }, null],
    ['normalized codex type', { type: ' CoDeX ', siwc_bound: true, use_siwc: true, siwc_status: 'valid' }, 'enabled'],
  ])('%s', (_name, values, expected) => {
    expect(credentialSIWCState(createSectionIdentity(values))).toBe(expected)
  })

  it('does not classify by buffered expiry or the overall credential toggle', () => {
    expect(credentialSIWCState(createSectionIdentity({ disabled: true, siwc_bound: true, use_siwc: true, siwc_status: 'valid', siwc_expires_at: '2000-01-01T00:00:00Z' }))).toBe('enabled')
    expect(credentialSIWCState(createSectionIdentity({ disabled: false, siwc_bound: false, use_siwc: true, siwc_status: 'invalid', siwc_expires_at: '2999-01-01T00:00:00Z' }))).toBe('invalid')
  })
})
