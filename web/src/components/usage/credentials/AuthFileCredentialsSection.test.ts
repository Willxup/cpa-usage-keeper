import { readFileSync } from 'node:fs'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { AuthFileCredentialsSection, AuthFileQuotaPanel, INSPECTION_RESULT_PAGE_SIZE_OPTIONS, buildInspectionResultsPage, buildInvalidInspectionAccountFileNames, formatInspectionCompletedAt, formatInspectionProgressPercent, formatQuotaErrorDisplay, formatQuotaResetDuration, formatQuotaResetLabel, formatQuotaWindowUsageAriaLabel, inspectionIndicatorTone, invertInvalidInspectionAccountFileNames, isInspectionStartDisabled, isSelectableInspectionStatusFilter, nextInspectionResultStatusFilter, persistAuthFileDisplayMode, readStoredAuthFileDisplayMode, selectAllInvalidInspectionAccountFileNames } from './AuthFileCredentialsSection'
import type { AuthFileCredentialRow, DisplayQuota } from './credentialViewModels'
import type { UsageQuotaInspectionResult, UsageQuotaInspectionResultStatus } from '@/lib/types'


const createAuthFileSectionProps = (overrides: Partial<Parameters<typeof AuthFileCredentialsSection>[0]> = {}) => ({
  rows: [],
  total: 0,
  page: 1,
  totalPages: 1,
  pageSize: 10,
  activeOnly: false,
  sort: 'priority' as const,
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
  onResetQuotaForAuthIndex: async () => undefined,
  onRefreshInspectionStatus: async () => undefined,
  onStartInspection: async () => undefined,
  ...overrides,
})

const authFileSectionSource = readFileSync(new URL('./AuthFileCredentialsSection.tsx', import.meta.url), 'utf8').replace(/\r\n/g, '\n')

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
    const html = renderToStaticMarkup(createElement(AuthFileCredentialsSection, createAuthFileSectionProps()))

    expect(html).toContain('usage_stats.credentials_auth_files_title')
    expect(html).not.toContain('usage_stats.credentials_auth_files_eyebrow')
  })

  it('renders shared metric headers without repeating labels in each row', () => {
    const row = {
      identity: { id: '1', identity: 'auth-1', is_deleted: false },
      displayName: 'Very Long Auth File Name For Wrapping',
      maskedIdentity: 'auth-1',
      providerLabel: 'Codex',
      typeLabel: 'codex',
      authTypeLabel: 'oauth',
      priorityLabel: 'P1',
      totalRequests: 1234,
      successCount: 1200,
      failureCount: 34,
      successRate: 97.24,
      totalTokens: 456789,
      cacheRate: 41.5,
      quota: [],
      quotaLoading: false,
      displayQuotas: [],
    } as AuthFileCredentialRow

    const html = renderToStaticMarkup(createElement(AuthFileCredentialsSection, createAuthFileSectionProps({ rows: [row], total: 1 })))

    expect(html.match(/usage_stats\.total_requests/g)).toHaveLength(1)
    expect(html.match(/usage_stats\.success_rate/g)).toHaveLength(1)
    expect(html.match(/usage_stats\.total_tokens/g)).toHaveLength(1)
    expect(html.match(/usage_stats\.cache_rate/g)).toHaveLength(1)
    expect(html).toContain('usage_stats.credentials_column_name')
    expect(html).toContain('usage_stats.credentials_column_quota')
    expect(html).toContain('1.23K')
    expect(html).toContain('97.24%')
  })

  it('keeps Auth Files metric cells aligned when values are unavailable', () => {
    const row = {
      identity: { id: '1', identity: 'auth-1', is_deleted: false },
      displayName: 'Sparse Auth File',
      maskedIdentity: 'auth-1',
      providerLabel: 'Codex',
      typeLabel: 'codex',
      authTypeLabel: 'oauth',
      totalRequests: 0,
      successCount: 0,
      failureCount: 0,
      successRate: null,
      totalTokens: 0,
      cacheRate: null,
      quota: [],
      quotaLoading: false,
      displayQuotas: [],
    } as AuthFileCredentialRow

    const html = renderToStaticMarkup(createElement(AuthFileCredentialsSection, createAuthFileSectionProps({ rows: [row], total: 1 })))

    expect(html.match(/credentialMetricValueCell/g)).toHaveLength(4)
    expect(html).toContain('usage_stats.total_requests')
    expect(html).toContain('usage_stats.success_rate')
    expect(html).toContain('usage_stats.total_tokens')
    expect(html).toContain('usage_stats.cache_rate')
  })
})

describe('AuthFileCredentialsSection quota reset action', () => {
  const baseRow = {
    identity: { id: '1', identity: 'auth-1', is_deleted: false },
    displayName: 'Codex Account',
    maskedIdentity: 'auth-1',
    providerLabel: 'Codex',
    typeLabel: 'codex',
    authTypeLabel: 'oauth',
    totalRequests: 12,
    successCount: 12,
    failureCount: 0,
    successRate: 100,
    totalTokens: 1200,
    cacheRate: 0,
    quota: [],
    quotaLoading: false,
    displayQuotas: [],
  } as AuthFileCredentialRow

  it('renders the quota reset action when reset credits are available', () => {
    const row = {
      ...baseRow,
      quotaResetCreditsAvailableCount: 2,
    } as AuthFileCredentialRow

    const html = renderToStaticMarkup(createElement(AuthFileCredentialsSection, createAuthFileSectionProps({ rows: [row], total: 1 })))

    expect(html).toContain('credentialQuotaActionStack')
    expect(html).toContain('credentialRowResetButton')
    expect(html).toContain('usage_stats.credentials_quota_reset_button')
  })

  it('renders quota reset tooltip copy with an emphasized reset credit count', () => {
    const row = {
      ...baseRow,
      quotaResetCreditsAvailableCount: 3,
    } as AuthFileCredentialRow

    const html = renderToStaticMarkup(createElement(AuthFileCredentialsSection, createAuthFileSectionProps({ rows: [row], total: 1 })))

    expect(html).toContain('role="tooltip"')
    expect(html).toContain('credentialQuotaResetTooltip')
    expect(html).toContain('credentialQuotaResetCount')
    expect(html).toContain('>3</span>')
    expect(html).toContain('usage_stats.credentials_quota_reset_tooltip_suffix')
  })

  it('hides the quota reset action when no reset credits are available', () => {
    const row = {
      ...baseRow,
      quotaResetCreditsAvailableCount: 0,
    } as AuthFileCredentialRow

    const html = renderToStaticMarkup(createElement(AuthFileCredentialsSection, createAuthFileSectionProps({ rows: [row], total: 1 })))

    expect(html).not.toContain('credentialRowResetButton')
    expect(html).not.toContain('usage_stats.credentials_quota_reset_button')
  })

  it('shows reset loading state without replacing the refresh action', () => {
    const row = {
      ...baseRow,
      quotaResetCreditsAvailableCount: 2,
      quotaResetting: true,
    } as AuthFileCredentialRow

    const html = renderToStaticMarkup(createElement(AuthFileCredentialsSection, createAuthFileSectionProps({ rows: [row], total: 1 })))

    expect(html).toContain('credentialRowResetButton')
    expect(html).toContain('aria-busy="true"')
    expect(html).toContain('disabled=""')
    expect(html).toContain('credentialRowRefreshButton')
  })

  it('keeps reset confirmation wired to the auth index and closes the popover after confirm', () => {
    expect(authFileSectionSource).toContain('onConfirm={() => onResetQuotaForAuthIndex(row.identity.identity)}')
    expect(authFileSectionSource).toContain('await onConfirm()')
    expect(authFileSectionSource).toContain('setOpen(false)')
    expect(authFileSectionSource).not.toContain('setLocalError')
    expect(authFileSectionSource).not.toContain('visibleError')
    expect(authFileSectionSource).not.toContain('quotaResetError')
    expect(authFileSectionSource).not.toContain('name: displayName')
  })

  it('closes the reset confirmation when clicking outside or pressing Escape', () => {
    expect(authFileSectionSource).toContain('actionRef')
    expect(authFileSectionSource).toContain("document.addEventListener('pointerdown'")
    expect(authFileSectionSource).toContain("document.addEventListener('keydown'")
    expect(authFileSectionSource).toContain("event.key === 'Escape'")
    expect(authFileSectionSource).toContain('actionRef.current?.contains(target)')
  })

  it('disables reset for deleted or refreshing rows', () => {
    expect(authFileSectionSource).toContain('!row.identity.is_deleted')
    expect(authFileSectionSource).toContain('!rowRefreshing')
    expect(authFileSectionSource).toContain('!row.quotaResetting')
    expect(authFileSectionSource).toContain('disabled={!canResetQuota}')
  })
})

describe('AuthFileCredentialsSection display mode persistence', () => {
  it('stores and restores the Auth Files quota or health display mode', () => {
    const storage = new Map<string, string>()
    const localStorage = {
      getItem: vi.fn((key: string) => storage.get(key) ?? null),
      setItem: vi.fn((key: string, value: string) => {
        storage.set(key, value)
      }),
    }
    vi.stubGlobal('window', { localStorage })
    try {
      expect(readStoredAuthFileDisplayMode()).toBe('quota')

      persistAuthFileDisplayMode('health')

      expect(localStorage.setItem).toHaveBeenCalledWith('cpa.credentials.authFiles.displayMode', 'health')
      expect(readStoredAuthFileDisplayMode()).toBe('health')

      storage.set('cpa.credentials.authFiles.displayMode', 'unexpected')

      expect(readStoredAuthFileDisplayMode()).toBe('quota')
    } finally {
      vi.unstubAllGlobals()
    }
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

describe('AuthFileCredentialsSection inspection results', () => {
  const makeInspectionResult = (index: number, status: UsageQuotaInspectionResultStatus = 'normal'): UsageQuotaInspectionResult => ({
    auth_index: `auth-${String(index).padStart(2, '0')}`,
    name: `Account ${index}`,
    type: 'codex',
    status,
    refreshed_at: `2026-06-03T10:${String(index).padStart(2, '0')}:00Z`,
  })

  it('paginates inspection results with the selectable page sizes instead of a fixed eight rows', () => {
    const results = Array.from({ length: 12 }, (_, index) => makeInspectionResult(index + 1))

    expect(INSPECTION_RESULT_PAGE_SIZE_OPTIONS).toEqual([10, 20, 50])

    const firstPage = buildInspectionResultsPage(results, null, 1, 10)
    expect(firstPage.total).toBe(12)
    expect(firstPage.totalPages).toBe(2)
    expect(firstPage.page).toBe(1)
    expect(firstPage.results.map((result) => result.auth_index)).toEqual([
      'auth-01',
      'auth-02',
      'auth-03',
      'auth-04',
      'auth-05',
      'auth-06',
      'auth-07',
      'auth-08',
      'auth-09',
      'auth-10',
    ])

    const secondPage = buildInspectionResultsPage(results, null, 2, 10)
    expect(secondPage.results.map((result) => result.auth_index)).toEqual(['auth-11', 'auth-12'])

    const expandedPage = buildInspectionResultsPage(results, null, 1, 20)
    expect(expandedPage.totalPages).toBe(1)
    expect(expandedPage.results).toHaveLength(12)
  })

  it('filters inspection results by one selected result card at a time', () => {
    const results = [
      makeInspectionResult(1, 'normal'),
      makeInspectionResult(2, 'limit_reached'),
      makeInspectionResult(3, 'unauthorized_401'),
      makeInspectionResult(4, 'payment_required_402'),
      makeInspectionResult(5, 'other_failed'),
      makeInspectionResult(6, 'unauthorized_401'),
    ]

    expect(nextInspectionResultStatusFilter(null, 'unauthorized_401_402')).toBe('unauthorized_401_402')
    expect(nextInspectionResultStatusFilter('unauthorized_401_402', 'unauthorized_401_402')).toBeNull()
    expect(nextInspectionResultStatusFilter('unauthorized_401_402', 'normal')).toBe('normal')

    const filteredPage = buildInspectionResultsPage(results, 'unauthorized_401_402', 1, 10)
    expect(filteredPage.total).toBe(3)
    expect(filteredPage.results.map((result) => result.auth_index)).toEqual(['auth-03', 'auth-04', 'auth-06'])
  })

  it('keeps unknown out of selectable inspection result filters', () => {
    expect(isSelectableInspectionStatusFilter('normal')).toBe(true)
    expect(isSelectableInspectionStatusFilter('limit_reached')).toBe(true)
    expect(isSelectableInspectionStatusFilter('unauthorized_401_402')).toBe(true)
    expect(isSelectableInspectionStatusFilter('unauthorized_401')).toBe(false)
    expect(isSelectableInspectionStatusFilter('payment_required_402')).toBe(false)
    expect(isSelectableInspectionStatusFilter('other_failed')).toBe(true)
    expect(isSelectableInspectionStatusFilter('unknown')).toBe(false)
    expect(isSelectableInspectionStatusFilter(undefined)).toBe(false)
  })

  it('keeps invalid action buttons in the results header and pagination in a bottom-right footer', () => {
    const headerIndex = authFileSectionSource.indexOf('credentialInspectionResultsHeader')
    const footerIndex = authFileSectionSource.indexOf('credentialInspectionResultsFooter')

    expect(headerIndex).toBeGreaterThanOrEqual(0)
    expect(footerIndex).toBeGreaterThan(headerIndex)

    const headerSlice = authFileSectionSource.slice(headerIndex, footerIndex)
    const footerSlice = authFileSectionSource.slice(footerIndex)

    expect(headerSlice).toContain('credentialInspectionInvalidActions')
    expect(headerSlice).not.toContain('credentialInspectionPageSizeControl')
    expect(headerSlice).not.toContain('credentialInspectionPagination')
    expect(footerSlice).toContain('credentialInspectionPageSizeControl')
    expect(footerSlice).toContain('credentialInspectionPagination')
  })

  it('builds invalid account actions only from cached 401 and 402 file names', () => {
    const results: UsageQuotaInspectionResult[] = [
      { ...makeInspectionResult(1, 'unauthorized_401'), file_name: 'a.json' },
      { ...makeInspectionResult(2, 'payment_required_402'), file_name: 'b.json' },
      { ...makeInspectionResult(3, 'unauthorized_401'), file_name: ' a.json ' },
      { ...makeInspectionResult(4, 'other_failed'), file_name: 'c.json' },
      { ...makeInspectionResult(5, 'normal'), file_name: 'd.json' },
      { ...makeInspectionResult(6, 'payment_required_402'), file_name: ' ' },
    ]

    expect(buildInvalidInspectionAccountFileNames(results)).toEqual(['a.json', 'b.json'])
  })

  it('supports selecting all and inverting invalid account selections', () => {
    const fileNames = ['a.json', 'b.json', 'c.json']

    expect(selectAllInvalidInspectionAccountFileNames(fileNames)).toEqual(fileNames)
    expect(invertInvalidInspectionAccountFileNames(fileNames, ['a.json', 'c.json'])).toEqual(['b.json'])
    expect(invertInvalidInspectionAccountFileNames(fileNames, [])).toEqual(fileNames)
  })

  it('renders invalid account bulk selection controls and async sync tip', () => {
    expect(authFileSectionSource).toContain('credentials_inspection_invalid_accounts_select_all')
    expect(authFileSectionSource).toContain('credentials_inspection_invalid_accounts_invert_selection')
    expect(authFileSectionSource).toContain('credentials_inspection_invalid_accounts_sync_tip')
  })

  it('keeps the invalid account modal open until post-action refresh completes', () => {
    const handlerIndex = authFileSectionSource.indexOf('const handleConfirmInvalidAccountAction = async () => {')
    const catchIndex = authFileSectionSource.indexOf('} catch (nextError)', handlerIndex)

    expect(handlerIndex).toBeGreaterThanOrEqual(0)
    expect(catchIndex).toBeGreaterThan(handlerIndex)

    const successPath = authFileSectionSource.slice(handlerIndex, catchIndex)
    const refreshIndex = successPath.indexOf('await Promise.all([onRefreshStatus(), onAfterInvalidAccountAction?.()])')
    const closeIndex = successPath.indexOf('setInvalidAccountAction(null)')
    const clearSelectionIndex = successPath.indexOf('setSelectedInvalidFileNames([])')

    expect(refreshIndex).toBeGreaterThanOrEqual(0)
    expect(closeIndex).toBeGreaterThan(refreshIndex)
    expect(clearSelectionIndex).toBeGreaterThan(refreshIndex)
  })
})
