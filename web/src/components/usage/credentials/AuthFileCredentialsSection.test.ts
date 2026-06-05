import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { AuthFileCredentialsSection, AuthFileQuotaPanel, formatInspectionCompletedAt, formatInspectionProgressPercent, formatQuotaErrorDisplay, formatQuotaResetDuration, formatQuotaResetLabel, formatQuotaWindowUsageAriaLabel, inspectionIndicatorTone, isInspectionStartDisabled } from './AuthFileCredentialsSection'
import type { AuthFileCredentialRow, DisplayQuota } from './credentialViewModels'

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({
    t: (key: string, params?: Record<string, string>) => `${key}:${params?.tokens ?? ''}:${params?.cost ?? ''}`,
  }),
}))

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

describe('AuthFileCredentialsSection title', () => {
  it('renders the Auth Files title without the Credentials eyebrow', () => {
    const html = renderToStaticMarkup(createElement(AuthFileCredentialsSection, {
      rows: [],
      total: 0,
      page: 1,
      totalPages: 1,
      pageSize: 10,
      activeOnly: false,
      sort: 'priority',
      loading: false,
      quotaRefreshing: false,
      quotaRefreshError: '',
      quotaAutoRefreshEnabled: false,
      quotaInspectionStatus: null,
      quotaInspectionLoading: false,
      quotaInspectionStarting: false,
      quotaInspectionError: '',
      onPageChange: () => undefined,
      onPageSizeChange: () => undefined,
      onActiveOnlyChange: () => undefined,
      onSortChange: () => undefined,
      onRefreshQuota: async () => undefined,
      onRefreshQuotaForAuthIndex: async () => undefined,
      onRefreshInspectionStatus: async () => undefined,
      onStartInspection: async () => undefined,
    }))

    expect(html).toContain('usage_stats.credentials_auth_files_title')
    expect(html).not.toContain('usage_stats.credentials_auth_files_eyebrow')
  })
})

describe('AuthFileCredentialsSection quota window usage accessibility', () => {
  it('labels token and cost metrics for assistive technology', () => {
    const t = (key: string, options?: Record<string, string>) => `${key}:${options?.tokens}:${options?.cost}`

    expect(formatQuotaWindowUsageAriaLabel(t, { tokens: '1.2M', cost: '$0.42' })).toBe('usage_stats.credentials_quota_window_usage_aria:1.2M:$0.42')
  })
})

describe('AuthFileCredentialsSection quota usage mode rendering', () => {
  const quota: DisplayQuota = {
    key: 'rate_limit.primary_window',
    label: '5h',
    percent: 25,
    barPercent: 75,
    percentKind: 'used',
    windowUsage: { tokens: '1.00M', cost: '$2.50' },
    windowUsageEstimate: { tokens: '4.00M', cost: '$10.00' },
    status: 'ok',
  }
  const row = {
    identity: { identity: 'auth-1', is_deleted: false },
    displayQuotas: [quota],
    quota: [],
    quotaLoading: false,
  } as AuthFileCredentialRow

  it('renders current quota usage by default and estimated usage when requested', () => {
    const currentHtml = renderToStaticMarkup(createElement(AuthFileQuotaPanel, { row, quotaUsageMode: 'current' }))
    const estimatedHtml = renderToStaticMarkup(createElement(AuthFileQuotaPanel, { row, quotaUsageMode: 'estimated' }))

    expect(currentHtml).toContain('1.00M')
    expect(currentHtml).toContain('$2.50')
    expect(currentHtml).not.toContain('4.00M')
    expect(currentHtml).not.toContain('$10.00')
    expect(estimatedHtml).toContain('4.00M')
    expect(estimatedHtml).toContain('$10.00')
  })

  it('falls back to current quota usage when estimated usage is unavailable', () => {
    const currentOnlyRow = {
      ...row,
      displayQuotas: [{ ...quota, windowUsageEstimate: undefined }],
    } as AuthFileCredentialRow
    const estimatedHtml = renderToStaticMarkup(createElement(AuthFileQuotaPanel, { row: currentOnlyRow, quotaUsageMode: 'estimated' }))

    expect(estimatedHtml).toContain('1.00M')
    expect(estimatedHtml).toContain('$2.50')
  })

  it('renders xai billing spend without token usage metrics', () => {
    const billingRow = {
      ...row,
      displayQuotas: [{
        key: 'billing.monthly',
        label: 'Monthly Spend',
        percent: 0.835,
        barPercent: 99.165,
        percentKind: 'used',
        billingUsage: { used: '$1.67', limit: '$200.00', remaining: '$198.33' },
        status: 'ok',
      }],
    } as AuthFileCredentialRow

    const html = renderToStaticMarkup(createElement(AuthFileQuotaPanel, { row: billingRow, quotaUsageMode: 'current' }))

    expect(html).toContain('Monthly Spend')
    expect(html).toContain('$1.67')
    expect(html).toContain('$200.00')
    expect(html.match(/<img/g)).toHaveLength(1)
    expect(html.indexOf('<img')).toBeLessThan(html.indexOf('$1.67'))
    expect(html).not.toContain('1.00M')
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
  it('calculates progress from cached quota results and inspectable auth files', () => {
    expect(formatInspectionProgressPercent({ total: 5, cached: 2, unknown: 1 })).toBe(50)
    expect(formatInspectionProgressPercent({ total: 5, cached: 2, unknown: 3 })).toBe(100)
    expect(formatInspectionProgressPercent({ total: 0, cached: 2, unknown: 0 })).toBe(0)
    expect(formatInspectionProgressPercent({ total: 5, cached: 9, unknown: 1 })).toBe(100)
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
    expect(inspectionIndicatorTone({ running: false, completed: true, completed_at: '2026-06-03T10:30:00Z' })).toBe('completed')
    expect(inspectionIndicatorTone({ running: false, completed: true })).toBe('idle')
    expect(inspectionIndicatorTone(null)).toBe('idle')
  })

  it('formats the cached inspection completion time', () => {
    expect(formatInspectionCompletedAt(undefined)).toBe('')
    expect(formatInspectionCompletedAt('invalid')).toBe('')
    expect(formatInspectionCompletedAt('2026-06-03T10:30:00Z')).toContain('2026')
  })
})
