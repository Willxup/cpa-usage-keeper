// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { renderToStaticMarkup } from 'react-dom/server'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/lib/api'
import type { UsageCredentialHealth } from '@/lib/types'
import { buildAiProviderCredentialRows, buildCredentialRows, type AiProviderCredentialRow, type CredentialRow, type CredentialDetailSelection } from '../credentialViewModels'
import { CredentialDetailDrawer } from '../CredentialDetailDrawer'
import { CredentialHealthPanel } from '../CredentialHealthPanel'

const fetchUsageEvents = vi.fn()
const fetchErrorEvents = vi.fn()
const fetchCodexQuotaHistory = vi.fn()
const fetchUsageIdentityActivity = vi.fn()
const fetchUsageIdentityModels = vi.fn()

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    fetchCodexQuotaHistory: (...args: unknown[]) => fetchCodexQuotaHistory(...args),
    fetchUsageIdentityActivity: (...args: unknown[]) => fetchUsageIdentityActivity(...args),
    fetchUsageIdentityModels: (...args: unknown[]) => fetchUsageIdentityModels(...args),
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

const credentialRow = {
  ...row,
  identity: {
    ...row.identity,
    id: 'credential-1',
    name: 'Credential Alias',
    identity: 'credential-identity-1',
    file_name: 'user106@edu.sso.monsterx.it.com.json',
    auth_type: 1,
    auth_type_name: 'oauth',
    type: 'codex',
    provider: 'codex',
  },
  displayName: 'Credential Alias',
  maskedIdentity: 'credential-identity-1',
  providerLabel: 'codex',
  typeLabel: 'codex',
  authTypeLabel: 'oauth',
  quota: [],
  quotaLoading: false,
  displayQuotas: [],
} as CredentialRow

const credentialSelection: CredentialDetailSelection = { kind: 'credential', row: credentialRow }

const claudeCredentialSelection: CredentialDetailSelection = {
  kind: 'credential',
  row: {
    ...credentialRow,
    identity: { ...credentialRow.identity, id: 'claude-credential', identity: 'claude-auth', type: 'claude', provider: 'claude' },
    displayName: 'Claude Credential',
    typeLabel: 'claude',
    providerLabel: 'claude',
  },
}

const unsupportedCredentialSelection: CredentialDetailSelection = {
  kind: 'credential',
  row: {
    ...credentialRow,
    identity: { ...credentialRow.identity, id: 'gemini-credential', identity: 'gemini-auth', type: 'gemini', provider: 'gemini' },
    typeLabel: 'gemini',
    providerLabel: 'gemini',
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
  let unexpectedRequests: string[]

  beforeEach(() => {
    unexpectedRequests = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      unexpectedRequests.push(String(input))
      throw new Error('Unexpected fetch')
    })
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    vi.useFakeTimers()
    fetchUsageEvents.mockReset()
    fetchUsageEvents.mockResolvedValueOnce(response('1', 'cursor-1')).mockResolvedValueOnce(response('2'))
    fetchErrorEvents.mockReset()
    fetchErrorEvents.mockResolvedValueOnce(errorResponse('error-1', 'error-cursor-1')).mockResolvedValueOnce(errorResponse('error-2'))
    fetchCodexQuotaHistory.mockReset()
    fetchCodexQuotaHistory.mockResolvedValue(quotaHistoryResponse)
    fetchUsageIdentityActivity.mockReset().mockResolvedValue({ identity_id: 'provider-1', timezone: 'UTC', window_start: '2026-08-17T05:00:00Z', window_end: '2026-08-17T10:00:00Z', latest: null, last_success: null, last_failure: null, streak: 0, streak_at_least: false })
    fetchUsageIdentityModels.mockReset().mockResolvedValue({ identity_id: 'provider-1', range: '7d', timezone: 'UTC', models: [], summary: { total_tokens: 0 } })
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    // 完成虚拟列表的滚动结束更新再销毁抽屉。
    await act(async () => vi.advanceTimersByTimeAsync(200))
    await act(async () => root.unmount())
    container.remove()
    vi.restoreAllMocks()
    vi.useRealTimers()
    expect(unexpectedRequests).toEqual([])
  })

  const renderDrawer = async (props: Partial<Parameters<typeof CredentialDetailDrawer>[0]> = {}) => {
    await act(async () => root.render(
      <CredentialDetailDrawer open selection={selection} onClose={() => undefined} {...props} />,
    ))
  }

  it.each([
    ['kimi', 'china'],
    ['kimi-ai', 'international'],
    ['kimi.ai', 'international'],
    ['kimi.com', 'china'],
  ])('shows the %s site in the Credential detail badges', async (type, site) => {
    await renderDrawer({ selection: {
      kind: 'credential',
      row: { ...credentialRow, identity: { ...credentialRow.identity, type }, typeLabel: type },
    } })

    const badge = document.body.querySelector(`[data-kimi-site="${site}"]`)
    expect(badge?.textContent).toBe(`usage_stats.credentials_kimi_site_${site}`)
    expect(badge?.parentElement?.textContent).toContain('P1')
  })

  it('keeps site badges restricted to Kimi Credentials', async () => {
    await renderDrawer({ selection: { kind: 'ai-provider', row: { ...row, identity: { ...row.identity, type: 'kimi-ai' } } } })
    expect(document.body.querySelector('[data-kimi-site]')).toBeNull()
    await renderDrawer({ selection: { kind: 'credential', row: { ...credentialRow, identity: { ...credentialRow.identity, type: 'generic', provider: 'kimi-ai' } } } })
    expect(document.body.querySelector('[data-kimi-site]')).toBeNull()
  })

  it('shows Muse brand details only for authentication files and preserves account names', async () => {
    const metaIdentity = { ...credentialRow.identity, type: 'meta', provider: 'Meta', displayName: 'My Meta account' }
    const [metaCredential] = buildCredentialRows([metaIdentity])
    await renderDrawer({ selection: { kind: 'credential', row: metaCredential } })
    expect(document.body.querySelector('[data-provider-brand-icon="muse"]')?.getAttribute('aria-label')).toBe('Muse')
    expect(document.body.querySelector('[data-credential-detail-subtitle]')?.textContent).toBe(metaIdentity.file_name)
    expect(document.body.textContent).toContain('My Meta account')
    expect(document.body.querySelector('[data-provider-brand-icon="meta"]')).toBeNull()

    const [metaProvider] = buildAiProviderCredentialRows([{ ...metaIdentity, auth_type: 2 }])
    await renderDrawer({ selection: { kind: 'ai-provider', row: metaProvider } })
    expect(document.body.querySelector('[data-provider-brand-icon="meta"]')?.getAttribute('aria-label')).toBe('meta')
    expect(document.body.querySelector('[data-provider-brand-icon="muse"]')).toBeNull()
    expect(document.body.querySelector('[data-credential-detail-subtitle]')?.textContent).toContain('Meta · meta')
    expect(document.body.textContent).toContain('My Meta account')
  })

  it('shows the concrete Credential filename as the subtitle', async () => {
    await renderDrawer({ selection: credentialSelection })

    expect(document.body.querySelector('[data-credential-detail-subtitle]')?.textContent)
      .toBe('user106@edu.sso.monsterx.it.com.json')
  })

  it('shows the shared quota history for Codex and Claude Credentials', async () => {
    await renderDrawer({ selection: credentialSelection })

    const quotaTab = document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="quota-history"]')!
    expect(fetchCodexQuotaHistory).not.toHaveBeenCalled()
    await act(async () => {
      quotaTab.click()
    })
    expect(fetchCodexQuotaHistory).toHaveBeenCalledWith('credential-identity-1', {}, expect.any(AbortSignal))
    expect(document.body.querySelector('[data-codex-quota-history-panel="true"]')).not.toBeNull()
    expect(document.body.textContent).toContain('usage_stats.credentials_quota_history_empty')
    expect(document.body.textContent).not.toContain('usage_stats.credentials_quota_history_window_selector')

    await renderDrawer({ selection: claudeCredentialSelection })
    const claudeTab = document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="quota-history"]')!
    expect(claudeTab).not.toBeNull()
    await act(async () => { claudeTab.click() })
    expect(fetchCodexQuotaHistory).toHaveBeenCalledWith('claude-auth', {}, expect.any(AbortSignal))
    expect(document.body.querySelector('[data-codex-quota-history-panel="true"]')).not.toBeNull()

    await renderDrawer({ selection: unsupportedCredentialSelection })
    expect(document.body.querySelector('[data-credential-detail-tab="quota-history"]')).toBeNull()
    expect(document.body.querySelector('[data-credential-detail-tab="overview"]')?.getAttribute('aria-selected')).toBe('true')

    const callsBeforeMeta = fetchCodexQuotaHistory.mock.calls.length
    await renderDrawer({ selection: { kind: 'credential', row: { ...credentialRow, identity: { ...credentialRow.identity, type: 'meta', provider: 'meta' } } } })
    expect(document.body.querySelector('[data-credential-detail-tab="quota-history"]')).toBeNull()
    expect(fetchCodexQuotaHistory).toHaveBeenCalledTimes(callsBeforeMeta)
  })

  it('loads stored cycle cost again when the Codex history tab is reopened', async () => {
    const historyWithCost = (cost: number) => ({
      ...quotaHistoryResponse,
      cycles: [{
        id: 1,
        status: 'current',
        window_seconds: 604800,
        window_started_at: '2026-08-17T00:00:00Z',
        reset_at: '2026-08-24T00:00:00Z',
        effective_started_at: '2026-08-17T00:00:00Z',
        effective_ended_at: '2026-08-24T00:00:00Z',
        first_observed_at: '2026-08-17T02:00:00Z',
        last_observed_at: '2026-08-21T11:50:00Z',
        first_remaining_percent: 90,
        last_remaining_percent: 86,
        observation_count: 2,
        usage: {
          requests: 1, successful_requests: 1, failed_requests: 0,
          input_tokens: 100, output_tokens: 0, reasoning_tokens: 0,
          cache_read_tokens: 0, cache_creation_tokens: 0, total_tokens: 100,
          total_cost_usd: cost, cost_available: true,
        },
        transitions: [],
      }],
    })
    fetchCodexQuotaHistory.mockReset()
      .mockResolvedValueOnce(historyWithCost(1.25))
      .mockResolvedValueOnce(historyWithCost(2.75))
    await renderDrawer({ selection: credentialSelection })
    const costText = () => document.body.querySelector(
      '[data-codex-quota-cycle-id="1"] [data-codex-quota-summary="used"] [data-codex-quota-summary-metric="cost"]',
    )?.textContent
    expect(fetchCodexQuotaHistory).not.toHaveBeenCalled()

    await act(async () => document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="quota-history"]')!.click())
    expect(fetchCodexQuotaHistory).toHaveBeenCalledTimes(1)
    expect(costText()).toBe('$1.25')

    // 离开再进入沿原标签挂载流程发起新请求，展示服务端最新的已存费用。
    await act(async () => document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="overview"]')!.click())
    expect(document.body.querySelector('[data-codex-quota-history-panel]')).toBeNull()
    await act(async () => document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="quota-history"]')!.click())
    expect(fetchCodexQuotaHistory).toHaveBeenCalledTimes(2)
    expect(costText()).toBe('$2.75')
  })

  it('reloads quota history when the same Credential changes provider type', async () => {
    await renderDrawer({ selection: credentialSelection })
    await act(async () => { document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="quota-history"]')!.click() })
    expect(fetchCodexQuotaHistory).toHaveBeenCalledTimes(1)

    const changedType: CredentialDetailSelection = {
      kind: 'credential',
      row: {
        ...credentialRow,
        identity: { ...credentialRow.identity, type: 'claude', provider: 'claude' },
        typeLabel: 'claude',
        providerLabel: 'claude',
      },
    }
    await renderDrawer({ selection: changedType })
    expect(document.body.querySelector('[data-credential-detail-tab="quota-history"]')?.getAttribute('aria-selected')).toBe('true')
    expect(fetchCodexQuotaHistory).toHaveBeenCalledTimes(2)
    expect(fetchCodexQuotaHistory).toHaveBeenLastCalledWith('credential-identity-1', {}, expect.any(AbortSignal))
  })

  it('includes the Codex quota history tab in roving keyboard order', async () => {
    await renderDrawer({ selection: credentialSelection })
    const overviewTab = document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="overview"]')!
    const quotaTab = document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="quota-history"]')!
    const requestsTab = document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="requests"]')!
    overviewTab.focus()
    await act(async () => {
      overviewTab.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
    })
    expect(document.activeElement).toBe(quotaTab)
    await act(async () => {
      quotaTab.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
    })
    expect(document.activeElement).toBe(requestsTab)
  })

  it('renders Overview metrics with the list thresholds and recent cache rate', async () => {
    await renderDrawer()

    const overviewMetrics = [...document.body.querySelectorAll<HTMLElement>('[class*="summaryMetric"]')]
    const tonedMetrics = overviewMetrics.filter((metric) => metric.hasAttribute('data-credential-detail-metric-tone'))
    const tones = tonedMetrics.map((metric) => metric.dataset.credentialDetailMetricTone)

    expect(overviewMetrics).toHaveLength(4)
    expect(overviewMetrics.map((metric) => metric.querySelector('strong')?.textContent)).toEqual(['10', '90.00%', '135', '10.00%'])
    expect(tones).toEqual(['warning', 'neutral'])
    expect(document.body.textContent).toContain('usage_stats.success 9')
    expect(document.body.textContent).toContain('usage_stats.failure 1')
    expect(document.body.querySelector('[data-credential-health-variant="compact"]')).not.toBeNull()
  })

  it('renders the provider overview in order without identity or quota placeholders', async () => {
    await renderDrawer()
    const sections = [...document.body.querySelectorAll('[role="tabpanel"] > section')]
    expect(sections.map(section => section.getAttribute('data-credential-overview-section') ?? (section.hasAttribute('data-credential-model-usage') ? 'models' : null))).toEqual(['cumulative', 'activity', 'models'])
    expect(document.body.textContent).not.toContain('usage_stats.credentials_detail_identity')
    expect(document.body.querySelector('[data-credential-overview-section="quota"]')).toBeNull()
    expect(fetchUsageIdentityActivity).toHaveBeenCalledWith('provider-1', expect.any(AbortSignal))
    expect(fetchUsageIdentityModels).toHaveBeenCalledWith('provider-1', '7d', expect.any(AbortSignal))
  })

  it('keeps native quota window data and independently shows recent activity and bounded streak', async () => {
    const failure = { timestamp: '2026-08-17T09:59:00Z', failed: true, status_code: 429 }
    fetchUsageIdentityActivity.mockResolvedValue({ identity_id: 'credential-1', latest: failure, last_success: { timestamp: '2026-08-17T09:30:00Z', failed: false }, last_failure: failure, streak: 100, streak_at_least: true })
    const quotas = [
      { key: 'primary', label: '5h', percent: 25, barPercent: 75, percentKind: 'used' as const, status: 'ok' as const, windowUsage: { tokens: '1.00M', cost: '$2.50' } },
      { key: 'weekly', label: 'Weekly', percent: 50, barPercent: 50, percentKind: 'used' as const, status: 'warning' as const },
    ]
    await renderDrawer({ selection: { kind: 'credential', row: { ...credentialRow, displayQuotas: quotas } } })
    const sections = [...document.body.querySelectorAll('[role="tabpanel"] > section')]
    expect(sections.map(section => section.getAttribute('data-credential-overview-section') ?? (section.hasAttribute('data-credential-model-usage') ? 'models' : null))).toEqual(['cumulative', 'activity', 'quota', 'models'])
    const activity = document.body.querySelector('[data-credential-overview-section="activity"]')!
    expect(activity.textContent).toContain('HTTP 429')
    expect(activity.textContent).toContain('2026-08-17 09:59:00')
    const latestTime = activity.querySelector('time[datetime="2026-08-17T09:59:00Z"]')!
    expect(latestTime.textContent).toBe('2026-08-17 09:59:00')
    expect(latestTime.getAttribute('aria-label')).toBe('2026-08-17 09:59:00 UTC')
    expect(activity.textContent).toContain('usage_stats.credentials_health_summary_healthy')
    expect(activity.textContent).toContain('usage_stats.credentials_activity_at_least:{"count":100}')
    expect(activity.textContent).not.toContain('quota exhausted')
    expect(activity.querySelector('[data-credential-health-variant="compact"]')).not.toBeNull()
    const quota = document.body.querySelector('[data-credential-overview-section="quota"]')!
    expect(quota.querySelectorAll('[data-quota-key]')).toHaveLength(2)
    expect(quota.textContent).toContain('1.00M')
    expect(quota.textContent).toContain('$2.50')
    expect(quota.textContent).not.toContain('Gemini Models')
    await act(async () => document.body.querySelector<HTMLButtonElement>('[data-model-range="today"]')!.click())
    expect(fetchUsageIdentityModels).toHaveBeenLastCalledWith('credential-1', 'today', expect.any(AbortSignal))
    expect(fetchUsageIdentityActivity).toHaveBeenCalledTimes(1)
    expect(quota.textContent).toContain('1.00M')
    expect(quota.textContent).toContain('$2.50')
  })

  it.each([
    [95, 5, 'healthy'],
    [90, 10, 'degraded'],
    [1, 2, 'unhealthy'],
    [0, 0, 'quiet'],
  ] as const)('matches the external card health with %i successes and %i failures', async (success, failure, tone) => {
    const latest = { timestamp: '2026-08-17T17:59:00+08:00', failed: false }
    const health = { ...credentialHealth, total_success: success, total_failure: failure }
    fetchUsageIdentityActivity.mockResolvedValue({ identity_id: 'provider-1', latest, last_success: latest, last_failure: null, streak: 1, streak_at_least: false })
    await renderDrawer({ selection: { kind: 'ai-provider', row: { ...row, credentialHealth: health } } })

    const card = document.createElement('div')
    card.innerHTML = renderToStaticMarkup(<CredentialHealthPanel variant="compact" showCompactMeta displayName={row.displayName} health={health} />)
    const cardState = card.querySelector('[class*="credentialHealthMetaStatus"]')!
    const activityState = document.querySelector<HTMLElement>('[data-credential-overview-section="activity"] [class*="activityState"]')!
    expect(activityState.textContent).toBe(cardState.textContent)
    expect(activityState.dataset.tone).toBe(tone)
    const latestTime = document.querySelector<HTMLElement>('[data-credential-overview-section="activity"] time[datetime="2026-08-17T17:59:00+08:00"]')!
    expect(latestTime.textContent).toBe('2026-08-17 17:59:00')
    expect(latestTime.getAttribute('aria-label')).toBe('2026-08-17 17:59:00 UTC+08:00')
  })

  it('does not report an unknown health snapshot as quiet while activity is loading or failed', async () => {
    const pending = Promise.withResolvers<unknown>()
    fetchUsageIdentityActivity.mockReturnValueOnce(pending.promise)
    await renderDrawer({ selection: { kind: 'ai-provider', row: { ...row, credentialHealth: undefined } } })
    const activity = document.querySelector('[data-credential-overview-section="activity"]')!
    expect(activity.textContent).toContain('common.loading')
    expect(activity.querySelector('[class*="activityState"]')).toBeNull()
    await act(async () => pending.reject(new Error('unavailable')))
    expect(activity.textContent).toContain('usage_stats.credentials_activity_load_failed')
    expect(activity.querySelector('[class*="activityState"]')).toBeNull()
  })

  it.each(['flat', 'group'] as const)('formats the detail reset date in the configured timezone for %s quotas', async (kind) => {
    const quota = {
      key: 'quota-5h', label: '5h', percent: 25, barPercent: 75, percentKind: 'used' as const, status: 'ok' as const,
      resetText: '2026-05-12T03:15:00-07:00',
      ...(kind === 'group' ? { scope: 'quota_group', groupKey: 'antigravity-gemini-models', groupLabel: 'Gemini Models' } : {}),
    }
    await renderDrawer({ timeZone: 'Asia/Shanghai', selection: { kind: 'credential', row: { ...credentialRow, displayQuotas: [quota] } } })
    const quotaRow = document.querySelector('[data-credential-overview-section="quota"] [data-quota-key]')!
    const inline = quotaRow.querySelector('[data-quota-reset-inline] time')!
    expect(inline.textContent).toBe('05-12 18:15')
    expect(inline.getAttribute('datetime')).toBe('2026-05-12T03:15:00-07:00')
    expect(inline.getAttribute('aria-label')).toBe('usage_stats.credentials_quota_resets_at:{"time":"05-12 18:15"}')
    expect(quotaRow.querySelector('[data-quota-reset-time]')?.textContent).toBe('05-12 18:15')
  })

  it('retains the displayed model list inside a scrolled drawer while the next range loads', async () => {
    const pending = Promise.withResolvers<unknown>()
    const totals = { request_count: 1, input_tokens: 10, output_tokens: 5, reasoning_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, total_tokens: 15, cost_usd: 1, unavailable_cost_count: 0, cost_available: true }
    fetchUsageIdentityModels.mockReset().mockResolvedValueOnce({ identity_id: 'credential-1', range: '7d', models: [{ ...totals, model: 'upstream', model_alias: 'public' }], summary: totals }).mockReturnValueOnce(pending.promise)
    await renderDrawer({ selection: credentialSelection })
    const panel = document.body.querySelector<HTMLElement>('[data-credential-model-usage]')!
    const scroller = panel.closest<HTMLElement>('.modal-body')!
    vi.spyOn(panel, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, 800, 900))
    Object.defineProperties(scroller, {
      clientHeight: { configurable: true, value: 400 },
      scrollHeight: { configurable: true, value: 1900 },
      scrollTop: { configurable: true, writable: true, value: 900 },
    })
    const table = panel.querySelector('table')
    const quota = document.body.querySelector('[data-credential-overview-section="quota"]')!
    await act(async () => panel.querySelector<HTMLButtonElement>('[data-model-range="today"]')!.click())
    expect(scroller.scrollTop).toBe(900)
    expect(panel.querySelector('table')).toBe(table)
    expect(panel.textContent).toContain('public')
    expect(panel.querySelector('[data-model-range="7d"]')?.getAttribute('aria-pressed')).toBe('true')
    expect(panel.querySelector('[data-model-range="today"]')?.getAttribute('aria-busy')).toBe('true')
    expect(fetchUsageIdentityActivity).toHaveBeenCalledTimes(1)
    expect(document.body.querySelector('[data-credential-overview-section="quota"]')).toBe(quota)
    await act(async () => pending.resolve({ identity_id: 'credential-1', range: 'today', models: [], summary: totals }))
    expect(panel.querySelector('[data-model-range="today"]')?.getAttribute('aria-pressed')).toBe('true')
    expect(panel.textContent).toContain('usage_stats.credentials_models_empty')
    expect(panel.style.minHeight).toBe('300px')
    expect(scroller.scrollTop).toBe(900)
  })

  it('retains only the native Antigravity quota group headings and their window usage', async () => {
    const base = { label: '5h', percent: 25, barPercent: 75, percentKind: 'used' as const, status: 'ok' as const, scope: 'quota_group', windowUsage: { tokens: '1.00M', cost: '$2.80' } }
    const displayQuotas = [
      { ...base, key: 'gemini-5h', groupKey: 'antigravity-gemini-models', groupLabel: 'Gemini Models' },
      { ...base, key: 'gemini-weekly', label: 'Weekly', groupKey: 'antigravity-gemini-models', groupLabel: 'Gemini Models' },
      { ...base, key: 'claude-5h', groupKey: 'antigravity-claude-and-gpt-models', groupLabel: 'Claude and GPT models' },
    ]
    await renderDrawer({ selection: { kind: 'credential', row: { ...credentialRow, displayQuotas } } })
    const quota = document.body.querySelector('[data-credential-overview-section="quota"]')!
    expect([...quota.querySelectorAll('[data-quota-group]')].map(group => group.getAttribute('data-quota-group'))).toEqual(['antigravity-gemini-models', 'antigravity-claude-and-gpt-models'])
    expect(quota.querySelectorAll('[data-quota-key]')).toHaveLength(3)
    expect(quota.textContent?.match(/Gemini Models/g)).toHaveLength(1)
    expect(quota.textContent?.match(/Claude and GPT models/g)).toHaveLength(1)
    expect(quota.textContent).toContain('1.00M')
    expect(quota.textContent).toContain('$2.80')
  })

  it('shows exact streaks and retry without coupling the model section', async () => {
    const latest = { timestamp: '2026-08-17T09:59:00Z', failed: false }
    fetchUsageIdentityActivity.mockRejectedValueOnce(new Error('unavailable')).mockResolvedValueOnce({ identity_id: 'provider-1', latest, last_success: latest, last_failure: null, streak: 100, streak_at_least: false })
    await renderDrawer()
    const activity = document.body.querySelector('[data-credential-overview-section="activity"]')!
    expect(activity.textContent).toContain('usage_stats.credentials_activity_load_failed')
    expect(activity.querySelector('[data-tone="healthy"]')?.textContent).toBe('usage_stats.credentials_health_summary_healthy')
    expect(document.body.querySelector('[data-credential-model-usage]')?.textContent).toContain('usage_stats.credentials_models_empty')
    await act(async () => activity.querySelector<HTMLButtonElement>('button')!.click())
    expect(activity.textContent).toContain('usage_stats.credentials_activity_count:{"count":100}')
    expect(activity.textContent).not.toContain('usage_stats.credentials_activity_at_least')
    expect(fetchUsageIdentityModels).toHaveBeenCalledTimes(1)
  })

  it('aborts activity and models immediately on close and discards late activity from another identity', async () => {
    const old = Promise.withResolvers<unknown>()
    const next = Promise.withResolvers<unknown>()
    const models = Promise.withResolvers<unknown>()
    fetchUsageIdentityActivity.mockReturnValueOnce(old.promise).mockReturnValueOnce(next.promise)
    fetchUsageIdentityModels.mockReturnValue(models.promise)
    const onAuthRequired = vi.fn()
    await renderDrawer({ onAuthRequired })
    const firstActivitySignal = fetchUsageIdentityActivity.mock.calls[0][1] as AbortSignal
    const firstModelsSignal = fetchUsageIdentityModels.mock.calls[0][2] as AbortSignal
    await renderDrawer({ selection: secondSelection, onAuthRequired })
    expect(firstActivitySignal.aborted).toBe(true)
    expect(firstModelsSignal.aborted).toBe(true)
    const secondActivitySignal = fetchUsageIdentityActivity.mock.calls[1][1] as AbortSignal
    const secondModelsSignal = fetchUsageIdentityModels.mock.calls[1][2] as AbortSignal
    await act(async () => old.reject(new ApiError('expired', 401)))
    expect(onAuthRequired).not.toHaveBeenCalled()
    await renderDrawer({ selection: secondSelection, open: false, onAuthRequired })
    expect(secondActivitySignal.aborted).toBe(true)
    expect(secondModelsSignal.aborted).toBe(true)
    await act(async () => next.reject(new ApiError('expired', 401)))
    expect(onAuthRequired).not.toHaveBeenCalled()
  })

  it('cancels a pending model range and cleans its tooltip when leaving the Overview tab', async () => {
    const pending = Promise.withResolvers<unknown>()
    const totals = { request_count: 1, input_tokens: 10, output_tokens: 5, reasoning_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, total_tokens: 15, cost_usd: 1, unavailable_cost_count: 0, cost_available: true }
    const initial = { identity_id: 'provider-1', range: '7d', models: [{ ...totals, model: 'upstream', model_alias: 'public' }], summary: totals }
    fetchUsageIdentityModels.mockReset().mockResolvedValueOnce(initial).mockReturnValueOnce(pending.promise).mockResolvedValueOnce(initial)
    await renderDrawer()
    const target = document.body.querySelector<HTMLElement>('[data-token-breakdown]')!
    await act(async () => target.focus())
    expect(document.body.querySelector('[role="tooltip"]:not([aria-hidden])')).not.toBeNull()
    await act(async () => document.body.querySelector<HTMLButtonElement>('[data-model-range="today"]')!.click())
    expect(document.body.querySelector('[role="tooltip"]:not([aria-hidden])')).toBeNull()
    const signal = fetchUsageIdentityModels.mock.calls[1][2] as AbortSignal
    await act(async () => document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="requests"]')!.click())
    expect(signal.aborted).toBe(true)
    await act(async () => pending.resolve({ ...initial, range: 'today', models: [] }))
    await act(async () => document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="overview"]')!.click())
    expect(fetchUsageIdentityModels).toHaveBeenLastCalledWith('provider-1', '7d', expect.any(AbortSignal))
    expect(document.body.querySelector('[data-model-range="7d"]')?.getAttribute('aria-pressed')).toBe('true')
    expect(document.body.querySelector('[data-credential-model-usage]')?.textContent).toContain('public')
  })

  it('cleans an open model tooltip on close while preserving the drawer animation snapshot', async () => {
    const totals = { request_count: 1, input_tokens: 10, output_tokens: 5, reasoning_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, total_tokens: 15, cost_usd: 1, unavailable_cost_count: 0, cost_available: true }
    fetchUsageIdentityModels.mockResolvedValue({ identity_id: 'provider-1', models: [{ ...totals, model: 'upstream', model_alias: 'public' }], summary: totals })
    await renderDrawer()
    await act(async () => document.body.querySelector<HTMLElement>('[data-token-breakdown]')!.focus())
    expect(document.body.querySelector('[role="tooltip"]:not([aria-hidden])')).not.toBeNull()
    await renderDrawer({ open: false })
    expect(document.body.querySelector('[role="tooltip"]:not([aria-hidden])')).toBeNull()
    expect(document.body.querySelector('[data-credential-overview-section="cumulative"]')).not.toBeNull()
  })

  it.each([
    { label: 'success boundaries', successRate: 95, cacheReadRate: 50, expectedTones: ['success', 'success'] },
    { label: 'warning boundaries', successRate: 80, cacheReadRate: 20, expectedTones: ['warning', 'warning'] },
    { label: 'below warning boundaries', successRate: 79.99, cacheReadRate: 19.99, expectedTones: ['danger', 'neutral'] },
    { label: 'missing rates', successRate: null, cacheReadRate: null, expectedTones: ['neutral', 'neutral'] },
  ])('uses the shared list thresholds for $label', async ({ successRate, cacheReadRate, expectedTones }) => {
    await renderDrawer({ selection: { kind: 'ai-provider', row: { ...row, successRate, cacheReadRate } } })

    const tones = [...document.body.querySelectorAll<HTMLElement>('[data-credential-detail-metric-tone]')]
      .map((metric) => metric.dataset.credentialDetailMetricTone)

    expect(tones).toEqual(expectedTones)
  })

  it('loads the dedicated latest-event list lazily and appends the next cursor page on scroll', async () => {
    await renderDrawer()

    expect(fetchUsageEvents).not.toHaveBeenCalled()
    const requestTab = document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="requests"]')!
    await act(async () => {
      requestTab.click()
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

    const scroller = document.body.querySelector<HTMLElement>('[class*="scroller"]')!
    Object.defineProperties(scroller, {
      clientHeight: { configurable: true, value: 600 },
      scrollHeight: { configurable: true, value: 1800 },
    })
    scroller.scrollTop = 1_300
    await act(async () => {
      scroller.dispatchEvent(new Event('scroll', { bubbles: true }))
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

  it('keeps the same credential request rows visible while stored costs are busy', async () => {
    fetchUsageEvents.mockReset()
      .mockResolvedValueOnce(response('1'))
      .mockRejectedValueOnce(new ApiError('busy', 503, 'costs_busy'))
    await renderDrawer()
    await act(async () => document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="requests"]')!.click())
    expect(document.body.textContent).toContain('model-1')
    await act(async () => document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="overview"]')!.click())
    await act(async () => document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="requests"]')!.click())
    expect(fetchUsageEvents).toHaveBeenCalledTimes(2)
    expect(document.body.textContent).toContain('model-1')
    expect(document.body.textContent).toContain('usage_stats.costs_busy')
  })

  it('clears the previous credential request state before the drawer reopens', async () => {
    fetchUsageEvents.mockReset()
    fetchUsageEvents.mockResolvedValue(response('1'))
    await renderDrawer()
    await act(async () => {
      document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="requests"]')!.click()
    })
    expect(document.body.textContent).toContain('model-1')

    await renderDrawer({ open: false })
    await renderDrawer({ selection: secondSelection })

    expect(document.body.textContent).toContain('Provider Two')
    expect(document.body.textContent).not.toContain('model-1')
    expect(document.body.querySelector('[data-credential-detail-tab="overview"]')?.getAttribute('aria-selected')).toBe('true')
    expect(fetchUsageEvents).toHaveBeenCalledTimes(1)
  })

  it('loads credential errors lazily by Keeper identity id', async () => {
    await renderDrawer()

    expect(fetchErrorEvents).not.toHaveBeenCalled()
    await act(async () => {
      document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="errors"]')!.click()
    })

    expect(fetchErrorEvents).toHaveBeenCalledWith('provider-1', expect.any(AbortSignal), undefined, 50)
    expect(document.body.querySelector('[data-credential-error-event-id="error-1"]')).not.toBeNull()
    expect(document.body.textContent).toContain('error-model-error-1')
    expect(document.body.textContent).toContain('quota exceeded error-1')
    expect(document.body.textContent).toContain('usage_stats.credentials_error_next_retry')
    expect(document.body.textContent).toContain('usage_stats.credentials_error_model_next_retry')
    expect(document.body.textContent).not.toContain('usage_stats.credentials_error_auth_state')
    expect(document.body.textContent).not.toContain('usage_stats.credentials_error_quota')

    const scroller = document.body.querySelector<HTMLElement>('[data-credential-error-events-scroller="true"]')!
    Object.defineProperties(scroller, {
      clientHeight: { configurable: true, value: 600 },
      scrollHeight: { configurable: true, value: 1800 },
    })
    scroller.scrollTop = 1_300
    await act(async () => {
      scroller.dispatchEvent(new Event('scroll', { bubbles: true }))
    })

    expect(fetchErrorEvents).toHaveBeenLastCalledWith('provider-1', expect.any(AbortSignal), 'error-cursor-1', 50)
    expect(document.body.querySelector('[data-credential-error-event-id="error-2"]')).not.toBeNull()
  })

  it('uses roving focus and arrow keys for the detail tabs', async () => {
    fetchUsageEvents.mockReset()
    fetchUsageEvents.mockResolvedValue(response('1'))
    await renderDrawer()

    const overviewTab = document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="overview"]')!
    const requestsTab = document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="requests"]')!
    const errorsTab = document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="errors"]')!
    expect(overviewTab.tabIndex).toBe(0)
    expect(requestsTab.tabIndex).toBe(-1)
    expect(errorsTab.tabIndex).toBe(-1)

    overviewTab.focus()
    await act(async () => {
      overviewTab.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
    })
    expect(document.activeElement).toBe(requestsTab)
    expect(requestsTab.getAttribute('aria-selected')).toBe('true')
    expect(overviewTab.tabIndex).toBe(-1)
    expect(requestsTab.tabIndex).toBe(0)

    await act(async () => {
      requestsTab.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
    })
    expect(document.activeElement).toBe(errorsTab)
    expect(errorsTab.getAttribute('aria-selected')).toBe('true')

    await act(async () => {
      errorsTab.dispatchEvent(new KeyboardEvent('keydown', { key: 'Home', bubbles: true }))
    })
    expect(document.activeElement).toBe(overviewTab)
    expect(overviewTab.getAttribute('aria-selected')).toBe('true')
  })

  it('pauses automatic cursor retries after a load-more failure', async () => {
    fetchUsageEvents.mockReset()
    fetchUsageEvents.mockResolvedValueOnce(response('1', 'cursor-1')).mockRejectedValueOnce(new Error('load more failed'))
    await renderDrawer()
    await act(async () => {
      document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="requests"]')!.click()
    })

    const scroller = document.body.querySelector<HTMLElement>('[class*="scroller"]')!
    Object.defineProperties(scroller, {
      clientHeight: { configurable: true, value: 600 },
      scrollHeight: { configurable: true, value: 1800 },
    })
    scroller.scrollTop = 1_300
    await act(async () => {
      scroller.dispatchEvent(new Event('scroll', { bubbles: true }))
    })
    await act(async () => {
      scroller.dispatchEvent(new Event('scroll', { bubbles: true }))
    })

    expect(fetchUsageEvents).toHaveBeenCalledTimes(2)
    expect(document.body.textContent).toContain('load more failed')
  })

  it('offers an initial-load retry at the right side of the tab row', async () => {
    fetchUsageEvents.mockReset()
    fetchUsageEvents.mockRejectedValueOnce(new Error('initial load failed')).mockResolvedValueOnce(response('1'))
    await renderDrawer()
    await act(async () => {
      document.body.querySelector<HTMLButtonElement>('[data-credential-detail-tab="requests"]')!.click()
    })

    const tabBar = document.body.querySelector('[data-credential-detail-tab-bar]')
    const tabList = document.body.querySelector('[role="tablist"]')
    const retryButton = document.body.querySelector<HTMLButtonElement>('[data-credential-detail-retry]')!
    expect(tabBar?.contains(retryButton)).toBe(true)
    expect(tabList?.contains(retryButton)).toBe(false)
    expect(retryButton.getAttribute('aria-label')).toBe('common.retry')
    expect(document.body.textContent).toContain('initial load failed')
    expect(document.body.textContent).not.toContain('usage_stats.request_events_empty_title')

    await act(async () => {
      retryButton.click()
    })

    expect(fetchUsageEvents).toHaveBeenCalledTimes(2)
    expect(document.body.textContent).toContain('model-1')
    expect(document.body.querySelector('[data-credential-detail-retry]')).toBeNull()
  })

  it('does not keep the request-log modal mounted after the drawer closes', async () => {
    await renderDrawer({
      open: false,
      requestLogResponse: {
        event_id: '1',
        available: true,
        previewable: true,
        sections: [{ title: 'RAW LOG', content: 'request log content' }],
      },
    })

    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect(document.body.textContent).not.toContain('request log content')
  })
})
