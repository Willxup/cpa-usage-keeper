import { describe, expect, it, vi } from 'vitest'
import { formatQuotaResetLabel, getQuotaRotationPhase } from './AuthFileCredentialsSection'

const formatLocalResetTime = (resetAt: string) => {
  const resetTime = new Date(resetAt)
  const month = String(resetTime.getMonth() + 1).padStart(2, '0')
  const day = String(resetTime.getDate()).padStart(2, '0')
  const hour = String(resetTime.getHours()).padStart(2, '0')
  const minute = String(resetTime.getMinutes()).padStart(2, '0')
  return `${month}/${day} ${hour}:${minute}`
}

describe('AuthFileCredentialsSection quota value rotation', () => {
  it('uses a shared wall-clock five-second phase for every quota row', () => {
    expect(getQuotaRotationPhase(0)).toBe('percent')
    expect(getQuotaRotationPhase(4_999)).toBe('percent')
    expect(getQuotaRotationPhase(5_000)).toBe('reset')
    expect(getQuotaRotationPhase(9_999)).toBe('reset')
    expect(getQuotaRotationPhase(10_000)).toBe('percent')
  })
})

describe('AuthFileCredentialsSection quota reset formatting', () => {
  it('formats reset labels with days when remaining time exceeds 24 hours', () => {
    vi.setSystemTime(new Date('2026-05-10T10:00:00Z'))
    try {
      const resetAt = '2026-05-12T10:15:00Z'
      expect(formatQuotaResetLabel(resetAt)).toBe(`2d0h15m(${formatLocalResetTime(resetAt)})`)
    } finally {
      vi.useRealTimers()
    }
  })

  it('formats reset labels without days when remaining time is under 24 hours', () => {
    vi.setSystemTime(new Date('2026-05-10T10:00:00Z'))
    try {
      const resetAt = '2026-05-10T14:15:00Z'
      expect(formatQuotaResetLabel(resetAt)).toBe(`4h15m(${formatLocalResetTime(resetAt)})`)
    } finally {
      vi.useRealTimers()
    }
  })
})
