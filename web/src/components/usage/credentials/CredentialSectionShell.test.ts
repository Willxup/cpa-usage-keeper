import { describe, expect, it } from 'vitest'
import { formatCredentialNumber } from './CredentialSectionShell'

describe('CredentialSectionShell formatting', () => {
  it('uses the shared compact K/M/B number format', () => {
    expect(formatCredentialNumber(950)).toBe('950')
    expect(formatCredentialNumber(12_345)).toBe('12.35K')
    expect(formatCredentialNumber(1_234_567)).toBe('1.23M')
  })
})
