// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { UsageCredentialHealth } from '@/lib/types'
import type { AiProviderCredentialRow, AuthFileCredentialRow, CredentialDetailSelection } from '../credentialViewModels'
import { CredentialDetailDrawer } from '../CredentialDetailDrawer'

const fetchUsageEvents = vi.fn()
const fetchErrorEvents = vi.fn()
const fetchCodexQuotaHistory = vi.fn()

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    fetchCodexQuotaHistory: (...args: unknown[]) => fetchCodexQuotaHistory(...args),
    fetchErrorEvents: (...args: unknown[]) => fetchErrorEvents(...args),
    fetchUsageEvents: (...args: unknown[]) => fetchUsageEvents(...args),
  }
})

vi.mock('react-chartjs-2', () => ({
  Bar: () => <div data-testid="quota-efficiency-chart" />,
}))

vi.mock('react-i18next', () => {
  const t = (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key
  return {
    initReactI18next: { type: '3rdParty', init: () => undefined },
    useTranslation: () => ({ t }),
  }
})

const credentialHealth: UsageCredentialHealth = {
  window_seconds: 18_000,
  bucket_seconds: 600,
  window_start: '2026-08-17T05:00:00Z',
  window_end: '2026-08-17T10:00:00Z',
  total_success: 9,
  total_failure: 1,
  success_rate: 90,
  input_tokens: 400,
  cache_read_tokens: 170,
  buckets: [],
}

const row = {
  identity: {
    id: 'provider-1',
    name: 'Provider One',
    auth_type: 2,
    auth_type_name: 'apikey',
    identity: 'auth-provider-1',
    type: 'openai',
    provider: 'OpenAI',
    total_requests: 10,
    success_count: 9,
    failure_count: 1,
    input_tokens: 100,
    output_tokens: 20,
    reasoning_tokens: 5,
    cache_read_tokens: 10,
    total_tokens: 135,
    last_aggregated_usage_event_id: '10',
    is_deleted: false,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-17T00:00:00Z',
    credential_health: credentialHealth,
  },
  displayName: 'Provider One',
  maskedIdentity: 'auth-provider-1',
  providerLabel: 'OpenAI',
  typeLabel: 'openai',
  authTypeLabel: 'apikey',
  priorityLabel: 'P1',
  totalRequests: 10,
  successCount: 9,
  failureCount: 1,
  successRate: 90,
  totalTokens: 135,
  cacheReadRate: 10,
  windowCacheReadRate: 42.5,
  credentialHealth,
} as AiProviderCredentialRow

const selection: CredentialDetailSelection = { kind: 'ai-provider', row }

const secondSelection: CredentialDetailSelection = {
  kind: 'ai-provider',
  row: {
    ...row,
    identity: {
      ...row.identity,
      id: 'provider-2',
      identity: 'auth-provider-2',
    },
    displayName: 'Provider Two',
    maskedIdentity: 'auth-provider-2',
  },
}

const authFileRow = {
  ...row,
  identity: {
    ...row.identity,
    id: 'auth-file-1',
    name: 'Auth File Alias',
    identity: 'auth-file-identity-1',
    file_name: 'user106@edu.sso.monsterx.it.com.json',
    auth_type: 1,
    auth_type_name: 'oauth',
    type: 'codex',
    provider: 'codex',
  },
  displayName: 'Auth File Alias',
  maskedIdentity: 'auth-file-identity-1',
  providerLabel: 'codex',
  typeLabel: 'codex',
  authTypeLabel: 'oauth',
  quota: [],
  quotaLoading: false,
  displayQuotas: [],
} as AuthFileCredentialRow

const authFileSelection: CredentialDetailSelection = { kind: 'auth-file', row: authFileRow }

const nonCodexAuthFileSelection: CredentialDetailSelection = {
  kind: 'auth-file',
  row: {
    ...authFileRow,
    identity: { ...authFileRow.identity, id: 'claude-auth-file', identity: 'claude-auth', type: 'claude', provider: 'claude' },
    displayName: 'Claude Auth File',
    typeLabel: 'claude',
    providerLabel: 'claude',
  },
}

const quotaHistoryResponse = {
  generated_at: '2026-08-21T12:00:00Z',
  range_start: '2026-07-22T12:00:00Z',
  windows: [{
    window_role: 'primary' as const,
    window_kind: 'weekly' as const,
    window_seconds: 604800,
    has_current_cycle: true,
    last_observed_at: '2026-08-21T11:50:00Z',
  }],
  selected_window: {
    window_role: 'primary' as const,
    window_kind: 'weekly' as const,
    window_seconds: 604800,
    has_current_cycle: true,
    last_observed_at: '2026-08-21T11:50:00Z',
  },
  cycles: [],
}

const response = (id: string, cursor?: string) => ({
  events: [{
    id,
    timestamp: '2026-08-17T10:00:00Z',
    model: `model-${id}`,
    source: 'Provider One',
    auth_index: 'auth-provider-1',
    failed: false,
    latency_ms: 100,
    tokens: {
      input_tokens: 10,
      output_tokens: 5,
      reasoning_tokens: 0,
      cache_read_tokens: 0,
      cache_creation_tokens: 0,
      total_tokens: 15,
    },
  }],
  total_count: 2,
  page: 1,
  page_size: 50,
  total_pages: 1,
  next_cursor: cursor,
  has_more: Boolean(cursor),
})

const errorResponse = (id: string, cursor?: string) => ({
  events: [{
    id,
    timestamp: '2026-08-17T10:00:00Z',
    provider: 'codex',
    model: `error-model-${id}`,
    status_code: 429,
    body_summary: `quota exceeded ${id}`,
    body_truncated: false,
    code: 'rate_limit',
    retryable: true,
    credential_retry_after: '2026-08-17T10:05:00Z',
    model_retry_after: '2026-08-17T10:03:00Z',
  }],
  next_cursor: cursor,
  has_more: Boolean(cursor),
})

describe('CredentialDetailDrawer', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    fetchUsageEvents.mockReset()
    fetchUsageEvents.mockResolvedValueOnce(response('1', 'cursor-1')).mockResolvedValueOnce(response('2'))
    fetchErrorEvents.mockReset()
    fetchErrorEvents.mockResolvedValueOnce(errorResponse('error-1', 'error-cursor-1')).mockResolvedValueOnce(errorResponse('error-2'))
    fetchCodexQuotaHistory.mockReset()
    fetchCodexQuotaHistory.mockResolvedValue(quotaHistoryResponse)
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    // TanStack Virtual 默认在 150ms 后发送滚动结束更新，销毁 Happy DOM 前先等待该更新完成。
    await act(async () => {
      const { promise: settleVirtualizer, resolve } = Promise.withResolvers<void>()
      window.setTimeout(resolve, 200)
      await settleVirtualizer
    })
    await act(async () => root.unmount())
    container.remove()
    document.body.innerHTML = ''
    vi.restoreAllMocks()
  })

  it('shows the concrete Auth File filename as the subtitle without a cumulative heading', async () => {
    await act(async () => {
      root.render(
        <CredentialDetailDrawer
          open
          selection={authFileSelection}
          onClose={() => undefined}
        />,
      )
      await Promise.resolve()
    })

    expect(document.body.querySelector('[data-credential-detail-subtitle]')?.textContent)
      .toBe('user106@edu.sso.monsterx.it.com.json')
    expect(document.body.textContent).not.toContain('usage_stats.credentials_detail_cumulative')
  })

  it('shows and lazily loads quota history only for a Codex Auth File', async () => {
    await act(async () => {
      root.render(<CredentialDetailDrawer open selection={authFileSelection} onClose={() => undefined} />)
      await Promise.resolve()
    })

    const quotaTab = document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="quota-history"]')
    expect(quotaTab).not.toBeNull()
    expect(fetchCodexQuotaHistory).not.toHaveBeenCalled()
    await act(async () => {
      quotaTab?.click()
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(fetchCodexQuotaHistory).toHaveBeenCalledWith('auth-file-identity-1', {}, expect.any(AbortSignal))
    expect(document.body.querySelector('[data-codex-quota-history-panel="true"]')).not.toBeNull()
    expect(document.body.textContent).toContain('usage_stats.credentials_quota_history_no_current')
    expect(document.body.textContent).not.toContain('usage_stats.credentials_quota_history_window_selector')

    await act(async () => {
      root.render(<CredentialDetailDrawer open selection={nonCodexAuthFileSelection} onClose={() => undefined} />)
      await Promise.resolve()
    })
    expect(document.body.querySelector('[data-credential-detail-tab="quota-history"]')).toBeNull()
    expect(document.body.querySelector('[data-credential-detail-tab="overview"]')?.getAttribute('aria-selected')).toBe('true')
  })

  it('includes the Codex quota history tab in roving keyboard order', async () => {
    await act(async () => {
      root.render(<CredentialDetailDrawer open selection={authFileSelection} onClose={() => undefined} />)
      await Promise.resolve()
    })
    const overviewTab = document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="overview"]')
    const quotaTab = document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="quota-history"]')
    const requestsTab = document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="requests"]')
    overviewTab?.focus()
    await act(async () => {
      overviewTab?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
      await Promise.resolve()
    })
    expect(document.activeElement).toBe(quotaTab)
    await act(async () => {
      quotaTab?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
      await Promise.resolve()
    })
    expect(document.activeElement).toBe(requestsTab)
  })

  it('uses the credential list tones for Overview metrics', async () => {
    await act(async () => {
      root.render(
        <CredentialDetailDrawer
          open
          selection={selection}
          onClose={() => undefined}
        />,
      )
      await Promise.resolve()
    })

    const overviewMetrics = [...document.body.querySelectorAll<HTMLElement>('[class*="summaryMetric"]')]
    const tonedMetrics = overviewMetrics.filter((metric) => metric.hasAttribute('data-credential-detail-metric-tone'))
    const tones = tonedMetrics.map((metric) => metric.dataset.credentialDetailMetricTone)

    expect(overviewMetrics).toHaveLength(4)
    expect(overviewMetrics.map((metric) => metric.querySelector('strong')?.className)).toEqual([
      expect.stringContaining('credentialMetricValueNeutral'),
      expect.stringContaining('credentialMetricValueWarning'),
      expect.stringContaining('credentialMetricValueNeutral'),
      expect.stringContaining('credentialMetricValueNeutral'),
    ])
    expect(tones).toEqual(['warning', 'neutral'])
    expect(document.body.querySelector('small [class*="credentialMetricValueSuccess"]')?.textContent).toBe('usage_stats.success 9')
    expect(document.body.querySelector('small [class*="credentialMetricValueDanger"]')?.textContent).toBe('usage_stats.failure 1')
    expect(document.body.textContent).toContain('usage_stats.credentials_health_cache_rate_5h')
    expect(document.body.textContent).toContain('42.50%')
  })

  it.each([
    { label: 'success boundaries', successRate: 95, cacheReadRate: 50, expectedTones: ['success', 'success'] },
    { label: 'warning boundaries', successRate: 80, cacheReadRate: 20, expectedTones: ['warning', 'warning'] },
    { label: 'below warning boundaries', successRate: 79.99, cacheReadRate: 19.99, expectedTones: ['danger', 'neutral'] },
    { label: 'missing rates', successRate: null, cacheReadRate: null, expectedTones: ['neutral', 'neutral'] },
  ])('uses the shared list thresholds for $label', async ({ successRate, cacheReadRate, expectedTones }) => {
    await act(async () => {
      root.render(
        <CredentialDetailDrawer
          open
          selection={{ kind: 'ai-provider', row: { ...row, successRate, cacheReadRate } }}
          onClose={() => undefined}
        />,
      )
      await Promise.resolve()
    })

    const tones = [...document.body.querySelectorAll<HTMLElement>('[data-credential-detail-metric-tone]')]
      .map((metric) => metric.dataset.credentialDetailMetricTone)

    expect(tones).toEqual(expectedTones)
  })

  it('loads the dedicated latest-event list lazily and appends the next cursor page on scroll', async () => {
    await act(async () => {
      root.render(
        <CredentialDetailDrawer
          open
          selection={selection}
          onClose={() => undefined}
        />,
      )
      await Promise.resolve()
    })

    expect(fetchUsageEvents).not.toHaveBeenCalled()
    const requestTab = document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="requests"]')
    await act(async () => {
      requestTab?.click()
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(fetchUsageEvents).toHaveBeenCalledWith(
      undefined,
      expect.any(AbortSignal),
      {
        authType: 2,
        cursorMode: true,
        pageSize: 50,
        source: 'auth-provider-1',
      },
    )
    expect(document.body.textContent).toContain('model-1')
    expect(document.body.querySelector('[data-credential-request-events-list="true"]')).not.toBeNull()
    expect(document.body.textContent).not.toContain('usage_stats.request_events_title')
    expect(document.body.textContent).not.toContain('usage_stats.request_events_columns')
    expect(document.body.textContent).not.toContain('usage_stats.request_events_filter_model')

    const scroller = document.body.querySelector<HTMLElement>('[class*="scroller"]')
    expect(scroller).not.toBeNull()
    Object.defineProperties(scroller, {
      clientHeight: { configurable: true, value: 600 },
      scrollHeight: { configurable: true, value: 1800 },
    })
    scroller!.scrollTop = 1_300
    await act(async () => {
      scroller?.dispatchEvent(new Event('scroll', { bubbles: true }))
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(fetchUsageEvents).toHaveBeenLastCalledWith(
      undefined,
      expect.any(AbortSignal),
      {
        authType: 2,
        cursor: 'cursor-1',
        cursorMode: true,
        pageSize: 50,
        source: 'auth-provider-1',
      },
    )
    expect(document.body.textContent).toContain('model-2')
  })

  it('clears the previous credential request state before the drawer reopens', async () => {
    fetchUsageEvents.mockReset()
    fetchUsageEvents.mockResolvedValue(response('1'))
    await act(async () => {
      root.render(
        <CredentialDetailDrawer
          open
          selection={selection}
          onClose={() => undefined}
        />,
      )
      await Promise.resolve()
    })
    await act(async () => {
      document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="requests"]')?.click()
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(document.body.textContent).toContain('model-1')

    await act(async () => {
      root.render(
        <CredentialDetailDrawer
          open={false}
          selection={selection}
          onClose={() => undefined}
        />,
      )
      await Promise.resolve()
    })
    await act(async () => {
      root.render(
        <CredentialDetailDrawer
          open
          selection={secondSelection}
          onClose={() => undefined}
        />,
      )
      await Promise.resolve()
    })

    expect(document.body.textContent).toContain('Provider Two')
    expect(document.body.textContent).not.toContain('model-1')
    expect(document.body.querySelector('[data-credential-detail-tab="overview"]')?.getAttribute('aria-selected')).toBe('true')
    expect(fetchUsageEvents).toHaveBeenCalledTimes(1)
  })

  it('loads credential errors lazily by Keeper identity id', async () => {
    await act(async () => {
      root.render(
        <CredentialDetailDrawer
          open
          selection={selection}
          onClose={() => undefined}
        />,
      )
      await Promise.resolve()
    })

    expect(fetchErrorEvents).not.toHaveBeenCalled()
    await act(async () => {
      document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="errors"]')?.click()
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(fetchErrorEvents).toHaveBeenCalledWith('provider-1', expect.any(AbortSignal), undefined, 50)
    expect(document.body.querySelector('[data-credential-error-event-id="error-1"]')).not.toBeNull()
    expect(document.body.textContent).toContain('error-model-error-1')
    expect(document.body.textContent).toContain('quota exceeded error-1')
    expect(document.body.textContent).toContain('usage_stats.credentials_error_next_retry')
    expect(document.body.textContent).toContain('usage_stats.credentials_error_model_next_retry')
    expect(document.body.textContent).not.toContain('usage_stats.credentials_error_auth_state')
    expect(document.body.textContent).not.toContain('usage_stats.credentials_error_quota')

    const scroller = document.body.querySelector<HTMLElement>('[data-credential-error-events-scroller="true"]')
    expect(scroller).not.toBeNull()
    Object.defineProperties(scroller, {
      clientHeight: { configurable: true, value: 600 },
      scrollHeight: { configurable: true, value: 1800 },
    })
    scroller!.scrollTop = 1_300
    await act(async () => {
      scroller?.dispatchEvent(new Event('scroll', { bubbles: true }))
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(fetchErrorEvents).toHaveBeenLastCalledWith('provider-1', expect.any(AbortSignal), 'error-cursor-1', 50)
    expect(document.body.querySelector('[data-credential-error-event-id="error-2"]')).not.toBeNull()
  })

  it('uses roving focus and arrow keys for the detail tabs', async () => {
    fetchUsageEvents.mockReset()
    fetchUsageEvents.mockResolvedValue(response('1'))
    await act(async () => {
      root.render(
        <CredentialDetailDrawer
          open
          selection={selection}
          onClose={() => undefined}
        />,
      )
      await Promise.resolve()
    })

    const overviewTab = document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="overview"]')
    const requestsTab = document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="requests"]')
    const errorsTab = document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="errors"]')
    expect(overviewTab?.tabIndex).toBe(0)
    expect(requestsTab?.tabIndex).toBe(-1)
    expect(errorsTab?.tabIndex).toBe(-1)

    overviewTab?.focus()
    await act(async () => {
      overviewTab?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(document.activeElement).toBe(requestsTab)
    expect(requestsTab?.getAttribute('aria-selected')).toBe('true')
    expect(overviewTab?.tabIndex).toBe(-1)
    expect(requestsTab?.tabIndex).toBe(0)

    await act(async () => {
      requestsTab?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(document.activeElement).toBe(errorsTab)
    expect(errorsTab?.getAttribute('aria-selected')).toBe('true')

    await act(async () => {
      errorsTab?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Home', bubbles: true }))
      await Promise.resolve()
    })
    expect(document.activeElement).toBe(overviewTab)
    expect(overviewTab?.getAttribute('aria-selected')).toBe('true')
  })

  it('pauses automatic cursor retries after a load-more failure', async () => {
    fetchUsageEvents.mockReset()
    fetchUsageEvents.mockResolvedValueOnce(response('1', 'cursor-1')).mockRejectedValueOnce(new Error('load more failed'))
    await act(async () => {
      root.render(
        <CredentialDetailDrawer
          open
          selection={selection}
          onClose={() => undefined}
        />,
      )
      await Promise.resolve()
    })
    await act(async () => {
      document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="requests"]')?.click()
      await Promise.resolve()
      await Promise.resolve()
    })

    const scroller = document.body.querySelector<HTMLElement>('[class*="scroller"]')
    Object.defineProperties(scroller!, {
      clientHeight: { configurable: true, value: 600 },
      scrollHeight: { configurable: true, value: 1800 },
    })
    scroller!.scrollTop = 1_300
    await act(async () => {
      scroller?.dispatchEvent(new Event('scroll', { bubbles: true }))
      await Promise.resolve()
      await Promise.resolve()
    })
    await act(async () => {
      scroller?.dispatchEvent(new Event('scroll', { bubbles: true }))
      await Promise.resolve()
    })

    expect(fetchUsageEvents).toHaveBeenCalledTimes(2)
    expect(document.body.textContent).toContain('load more failed')
  })

  it('offers an initial-load retry at the right side of the tab row', async () => {
    fetchUsageEvents.mockReset()
    fetchUsageEvents.mockRejectedValueOnce(new Error('initial load failed')).mockResolvedValueOnce(response('1'))
    await act(async () => {
      root.render(
        <CredentialDetailDrawer
          open
          selection={selection}
          onClose={() => undefined}
        />,
      )
      await Promise.resolve()
    })
    await act(async () => {
      document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="requests"]')?.click()
      await Promise.resolve()
      await Promise.resolve()
    })

    const tabBar = document.body.querySelector('[data-credential-detail-tab-bar]')
    const tabList = document.body.querySelector('[role="tablist"]')
    const retryButton = document.body.querySelector<HTMLButtonElement>('[data-credential-detail-retry]')
    expect(tabBar?.contains(retryButton)).toBe(true)
    expect(tabList?.contains(retryButton)).toBe(false)
    expect(retryButton?.className).toContain('credentialRowRefreshButton')
    expect(retryButton?.querySelector('svg')).not.toBeNull()
    expect(retryButton?.textContent).toBe('')
    expect(retryButton?.getAttribute('aria-label')).toBe('common.retry')
    expect(document.body.textContent).toContain('initial load failed')
    expect(document.body.textContent).not.toContain('usage_stats.request_events_empty_title')

    await act(async () => {
      retryButton?.click()
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(fetchUsageEvents).toHaveBeenCalledTimes(2)
    expect(document.body.textContent).toContain('model-1')
    expect(document.body.querySelector('[data-credential-detail-retry]')).toBeNull()
  })

  it('does not keep the request-log modal mounted after the drawer closes', async () => {
    await act(async () => {
      root.render(
        <CredentialDetailDrawer
          open={false}
          selection={selection}
          requestLogResponse={{
            event_id: '1',
            available: true,
            previewable: true,
            sections: [{ title: 'RAW LOG', content: 'request log content' }],
          }}
          onClose={() => undefined}
        />,
      )
      await Promise.resolve()
    })

    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect(document.body.textContent).not.toContain('request log content')
  })
})
