import { describe, expect, it, vi } from 'vitest'
import { formatInspectionCompletedAt, formatInspectionProgressPercent, formatQuotaErrorDisplay, formatQuotaResetDuration, formatQuotaResetLabel, formatQuotaWindowUsageAriaLabel, inspectionIndicatorTone, isInspectionStartDisabled } from './AuthFileCredentialsSection'

const formatLocalResetTime = (resetAt: string) => {
  const resetTime = new Date(resetAt)
  const month = String(resetTime.getMonth() + 1).padStart(2, '0')
  const day = String(resetTime.getDate()).padStart(2, '0')
  const hour = String(resetTime.getHours()).padStart(2, '0')
  const minute = String(resetTime.getMinutes()).padStart(2, '0')
  return `${month}/${day} ${hour}:${minute}`
}

describe('AuthFileCredentialsSection quota reset formatting', () => {
  it('formats reset labels with days when remaining time exceeds 24 hours', () => {
    vi.setSystemTime(new Date('2026-05-10T10:00:00Z'))
    try {
      const resetAt = '2026-05-12T10:15:00Z'
      expect(formatQuotaResetLabel(resetAt)).toBe(formatLocalResetTime(resetAt))
      expect(formatQuotaResetDuration(resetAt)).toBe('2d0h15m')
    } finally {
      vi.useRealTimers()
    }
  })

  it('formats reset labels without days when remaining time is under 24 hours', () => {
    vi.setSystemTime(new Date('2026-05-10T10:00:00Z'))
    try {
      const resetAt = '2026-05-10T14:15:00Z'
      expect(formatQuotaResetLabel(resetAt)).toBe(formatLocalResetTime(resetAt))
      expect(formatQuotaResetDuration(resetAt)).toBe('4h15m')
    } finally {
      vi.useRealTimers()
    }
  })
})

describe('AuthFileCredentialsSection quota window usage accessibility', () => {
  it('labels token and cost metrics for assistive technology', () => {
    const t = (key: string, options?: Record<string, string>) => `${key}:${options?.tokens}:${options?.cost}`

    expect(formatQuotaWindowUsageAriaLabel(t, { tokens: '1.2M', cost: '$0.42' })).toBe('usage_stats.credentials_quota_window_usage_aria:1.2M:$0.42')
  })
})

describe('AuthFileCredentialsSection quota error display', () => {
  it('summarizes HTTP quota errors without exposing the full backend string inline', () => {
    expect(formatQuotaErrorDisplay('HTTP 401: expired token for account user@example.com')).toEqual({
      code: '401',
      message: 'expired token for account user@example.com',
      title: 'HTTP 401: expired token for account user@example.com',
    })
  })

  it('extracts message fields from structured HTTP error bodies', () => {
    expect(formatQuotaErrorDisplay('HTTP 402: {"error":{"message":"Payment required. Please upgrade billing."}}')).toEqual({
      code: '402',
      message: 'Payment required. Please upgrade billing.',
      title: 'HTTP 402: {"error":{"message":"Payment required. Please upgrade billing."}}',
    })
  })

  it('extracts message fields from real cached HTTP JSON errors', () => {
    const rawError = `HTTP 401: {
  "error": {
    "message": "Provided authentication token is expired. Please try signing in again.",
    "type": null,
    "code": "token_expired",
    "param": null
  },
  "status": 401
}`

    expect(formatQuotaErrorDisplay(rawError)).toEqual({
      code: '401',
      message: 'Provided authentication token is expired. Please try signing in again.',
      title: rawError,
    })
  })

  it('extracts HTTP code and message when the cached error is a JSON string', () => {
    expect(formatQuotaErrorDisplay('{"statusCode":401,"body":"{\\"error\\":{\\"message\\":\\"Session expired. Please sign in again.\\"}}" }')).toEqual({
      code: '401',
      message: 'Session expired. Please sign in again.',
      title: '{"statusCode":401,"body":"{\\"error\\":{\\"message\\":\\"Session expired. Please sign in again.\\"}}" }',
    })
  })

  it('prefers nested upstream error messages over generic wrapper messages', () => {
    expect(formatQuotaErrorDisplay('HTTP 401: {"message":"Request failed","body":"{\\"error\\":{\\"message\\":\\"Token expired\\"}}","status":401}')).toEqual({
      code: '401',
      message: 'Token expired',
      title: 'HTTP 401: {"message":"Request failed","body":"{\\"error\\":{\\"message\\":\\"Token expired\\"}}","status":401}',
    })
    expect(formatQuotaErrorDisplay('{"statusCode":402,"message":"fetch failed","error":{"message":"Payment required"}}')).toEqual({
      code: '402',
      message: 'Payment required',
      title: '{"statusCode":402,"message":"fetch failed","error":{"message":"Payment required"}}',
    })
  })

  it('truncates long quota error messages for stable row layout', () => {
    const display = formatQuotaErrorDisplay(`HTTP 401: ${'token '.repeat(30)}`)

    expect(display.code).toBe('401')
    expect(display.message.length).toBeLessThanOrEqual(99)
    expect(display.message.endsWith('...')).toBe(true)
  })

  it('does not treat larger leading numbers as HTTP status codes', () => {
    const display = formatQuotaErrorDisplay('123456')

    expect(display.code).toBeUndefined()
    expect(display.message).toBe('123456')
    expect(display.title).toBe('123456')
  })
})

describe('AuthFileCredentialsSection inspection controls', () => {
  it('calculates progress from cached quota results and total active auth files', () => {
    expect(formatInspectionProgressPercent({ total: 5, cached: 2 })).toBe(40)
    expect(formatInspectionProgressPercent({ total: 0, cached: 2 })).toBe(0)
    expect(formatInspectionProgressPercent({ total: 5, cached: 9 })).toBe(100)
  })

  it('disables manual inspection while auto refresh or an inspection round is active', () => {
    expect(isInspectionStartDisabled({ quotaAutoRefreshEnabled: true, starting: false, total: 5, running: false })).toBe(true)
    expect(isInspectionStartDisabled({ quotaAutoRefreshEnabled: false, starting: true, total: 5, running: false })).toBe(true)
    expect(isInspectionStartDisabled({ quotaAutoRefreshEnabled: false, starting: false, total: 5, running: true })).toBe(true)
    expect(isInspectionStartDisabled({ quotaAutoRefreshEnabled: false, starting: false, total: 0, running: false })).toBe(true)
    expect(isInspectionStartDisabled({ quotaAutoRefreshEnabled: false, starting: false, total: 5, running: false })).toBe(false)
  })

  it('uses running and completed status dots for the Auth Files inspection button', () => {
    expect(inspectionIndicatorTone({ running: true, completed: false })).toBe('running')
    expect(inspectionIndicatorTone({ running: false, completed: true })).toBe('completed')
    expect(inspectionIndicatorTone(null)).toBe('idle')
  })

  it('formats the cached inspection completion time', () => {
    expect(formatInspectionCompletedAt(undefined)).toBe('')
    expect(formatInspectionCompletedAt('invalid')).toBe('')
    expect(formatInspectionCompletedAt('2026-06-03T10:30:00Z')).toContain('2026')
  })
})
