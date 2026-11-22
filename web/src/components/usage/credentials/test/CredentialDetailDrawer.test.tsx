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
const fetchUsageIdentityModels = vi.fn()

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    fetchCodexQuotaHistory: (...args: unknown[]) => fetchCodexQuotaHistory(...args),
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

function withHealth(base: AiProviderCredentialRow, health: UsageCredentialHealth | undefined): AiProviderCredentialRow {
  const identity = { ...base.identity, credential_health: health }
  const derived = buildAiProviderCredentialRows([identity])[0]
  return { ...base, identity, credentialHealth: derived.credentialHealth, windowCacheReadRate: derived.windowCacheReadRate }
}

function healthSnapshot(success: number, failure: number, input: number, cached: number): UsageCredentialHealth {
  const total = success + failure
  return {
    ...credentialHealth,
    total_success: success, total_failure: failure, success_rate: total ? success / total * 100 : 0,
    input_tokens: input, cache_read_tokens: cached,
    buckets: [{ start_time: '2026-08-17T09:50:00Z', end_time: '2026-08-17T10:00:00Z', success, failure, rate: total ? success / total : 0 }],
  }
}

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
    expect(tones).toEqual(['warning', 'danger'])
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
    expect(fetchUsageIdentityModels).toHaveBeenCalledWith('provider-1', '7d', expect.any(AbortSignal))
  })

  it('keeps native quota window data and independently shows cached recent activity', async () => {
    const quotas = [
      { key: 'primary', label: '5h', percent: 25, barPercent: 75, percentKind: 'used' as const, status: 'ok' as const, windowUsage: { tokens: '1.00M', cost: '$2.50' } },
      { key: 'weekly', label: 'Weekly', percent: 50, barPercent: 50, percentKind: 'used' as const, status: 'warning' as const },
    ]
    await renderDrawer({ selection: { kind: 'credential', row: { ...credentialRow, displayQuotas: quotas } } })
    const sections = [...document.body.querySelectorAll('[role="tabpanel"] > section')]
    expect(sections.map(section => section.getAttribute('data-credential-overview-section') ?? (section.hasAttribute('data-credential-model-usage') ? 'models' : null))).toEqual(['cumulative', 'activity', 'quota', 'models'])
    const activity = document.body.querySelector('[data-credential-overview-section="activity"]')!
    expect(activity.textContent).toContain('usage_stats.credentials_health_summary_healthy')
    expect(activity.querySelector('[data-credential-activity-metric="requests"] strong')?.textContent).toBe('10')
    expect(activity.querySelector('[data-credential-activity-metric="failures"] strong')?.textContent).toBe('1')
    expect(activity.querySelector('[data-credential-activity-metric="cache-rate"] strong')?.textContent).toBe('42.50%')
    expect(activity.textContent).not.toContain('quota exhausted')
    expect(activity.querySelector('[data-credential-health-variant="compact"]')).not.toBeNull()
    const quota = document.body.querySelector('[data-credential-overview-section="quota"]')!
    expect(quota.querySelectorAll('[data-quota-key]')).toHaveLength(2)
    expect(quota.textContent).toContain('1.00M')
    expect(quota.textContent).toContain('$2.50')
    expect(quota.textContent).not.toContain('Gemini Models')
    await act(async () => document.body.querySelector<HTMLButtonElement>('[data-model-range="today"]')!.click())
    expect(fetchUsageIdentityModels).toHaveBeenLastCalledWith('credential-1', 'today', expect.any(AbortSignal))
    expect(quota.textContent).toContain('1.00M')
    expect(quota.textContent).toContain('$2.50')
  })

  it.each([
    [95, 5, 'healthy'],
    [90, 10, 'degraded'],
    [1, 2, 'unhealthy'],
    [0, 0, 'quiet'],
  ] as const)('matches the external card health with %i successes and %i failures', async (success, failure, tone) => {
    const health = healthSnapshot(success, failure, success + failure ? 400 : 0, success + failure ? 170 : 0)
    await renderDrawer({ selection: { kind: 'ai-provider', row: withHealth(row, health) } })

    const card = document.createElement('div')
    card.innerHTML = renderToStaticMarkup(<CredentialHealthPanel variant="compact" showCompactMeta displayName={row.displayName} health={health} />)
    const cardState = card.querySelector('[class*="credentialHealthMetaStatus"]')!
    const activityState = document.querySelector<HTMLElement>('[data-credential-overview-section="activity"] [class*="activityState"]')!
    expect(activityState.textContent).toBe(cardState.textContent)
    expect(activityState.dataset.tone).toBe(tone)
    const cardBuckets = [...card.querySelectorAll('[role="listitem"]')]
    const detailBuckets = [...document.querySelectorAll('[data-credential-overview-section="activity"] [role="listitem"]')]
    expect(detailBuckets).toHaveLength(30)
    expect(detailBuckets.map(bucket => bucket.getAttribute('aria-label'))).toEqual(cardBuckets.map(bucket => bucket.getAttribute('aria-label')))
    expect(detailBuckets.map(bucket => bucket.querySelector('[role="tooltip"]')?.textContent)).toEqual(cardBuckets.map(bucket => bucket.querySelector('[role="tooltip"]')?.textContent))
  })

  it('shows unavailable metrics without a snapshot and zeros for a valid empty snapshot', async () => {
    await renderDrawer({ selection: { kind: 'ai-provider', row: withHealth(row, undefined) } })
    const activity = document.querySelector('[data-credential-overview-section="activity"]')!
    expect(activity.querySelector('[class*="activityState"]')).toBeNull()
    expect([...activity.querySelectorAll('[data-credential-activity-metric] strong')].map(node => node.textContent)).toEqual(['—', '—', '—'])
    expect(activity.querySelectorAll('[role="listitem"]')).toHaveLength(30)
    expect(activity.querySelector('[role="listitem"]')?.getAttribute('aria-label')).toContain('usage_stats.no_data')
    expect(activity.querySelector('[role="tooltip"]')?.textContent).toContain('usage_stats.no_data')
    expect(activity.querySelector('[data-credential-health-variant="compact"] strong')?.textContent).toBe('—')
    expect(activity.textContent).not.toContain('0.0%')
    const emptyHealth = healthSnapshot(0, 0, 0, 0)
    await renderDrawer({ selection: { kind: 'ai-provider', row: withHealth(row, emptyHealth) } })
    expect(activity.querySelector('[data-tone="quiet"]')?.textContent).toBe('usage_stats.credentials_health_summary_quiet')
    expect([...activity.querySelectorAll('[data-credential-activity-metric] strong')].map(node => node.textContent)).toEqual(['0', '0', '—'])
    expect(activity.querySelector('[data-credential-health-variant="compact"] strong')?.textContent).toBe('0.0%')
  })

  it.each(['flat', 'group'] as const)('shows the project-timezone reset tooltip for %s quotas without closing the drawer on Escape', async (kind) => {
    const quota = {
      key: 'quota-5h', label: '5h', percent: 25, barPercent: 75, percentKind: 'used' as const, status: 'ok' as const,
      resetText: '2026-05-12T03:15:00-07:00',
      ...(kind === 'group' ? { scope: 'quota_group', groupKey: 'antigravity-gemini-models', groupLabel: 'Gemini Models' } : {}),
    }
    const onClose = vi.fn()
    await renderDrawer({ timeZone: 'Asia/Shanghai', onClose, selection: { kind: 'credential', row: { ...credentialRow, displayQuotas: [quota] } } })
    const quotaRow = document.querySelector('[data-credential-overview-section="quota"] [data-quota-key]')!
    const countdown = quotaRow.querySelector<HTMLButtonElement>('[data-quota-reset-countdown]')!
    expect(countdown.getAttribute('aria-label')).toBe('usage_stats.credentials_quota_resets_at:{"time":"05-12 18:15"}')
    expect(quotaRow.textContent).not.toContain('05-12 18:15')
    const tooltip = () => document.getElementById(countdown.getAttribute('aria-describedby') ?? '')

    await act(async () => countdown.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    expect(tooltip()?.textContent).toBe('usage_stats.credentials_quota_resets_at:{"time":"05-12 18:15"}')
    await act(async () => countdown.dispatchEvent(new MouseEvent('mouseout', { bubbles: true })))
    expect(tooltip()).toBeNull()
    await act(async () => countdown.click())
    expect(tooltip()).not.toBeNull()
    await act(async () => document.body.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, pointerType: 'touch' })))
    expect(tooltip()).toBeNull()
    await act(async () => countdown.focus())
    expect(tooltip()).not.toBeNull()
    await act(async () => countdown.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })))
    expect(tooltip()).toBeNull()
    expect(onClose).not.toHaveBeenCalled()
    await act(async () => countdown.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })))
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('cleans the reset tooltip on tab, identity and drawer changes', async () => {
    const quota = { key: 'shared-window', label: '5h', percent: 25, barPercent: 75, percentKind: 'used' as const, status: 'ok' as const, resetText: '2026-05-12T03:15:00-07:00' }
    const first: CredentialDetailSelection = { kind: 'credential', row: { ...credentialRow, displayQuotas: [quota] } }
    const second: CredentialDetailSelection = {
      kind: 'credential',
      row: { ...credentialRow, identity: { ...credentialRow.identity, id: 'other', identity: 'other-auth' }, displayQuotas: [{ ...quota, resetText: '2026-05-13T03:15:00-07:00' }] },
    }
    const countdown = () => document.querySelector<HTMLButtonElement>('[data-credential-overview-section="quota"] [data-quota-reset-countdown]')!
    const focusTooltip = async () => {
      const target = countdown()
      await act(async () => target.focus())
      const id = target.getAttribute('aria-describedby')!
      expect(document.getElementById(id)).not.toBeNull()
      return id
    }

    await renderDrawer({ selection: first, timeZone: 'Asia/Shanghai' })
    const firstId = await focusTooltip()
    await act(async () => document.querySelector<HTMLButtonElement>('[data-credential-detail-tab="requests"]')!.click())
    expect(document.getElementById(firstId)).toBeNull()
    await act(async () => document.querySelector<HTMLButtonElement>('[data-credential-detail-tab="overview"]')!.click())
    const secondId = await focusTooltip()
    await renderDrawer({ selection: second, timeZone: 'Asia/Shanghai' })
    expect(document.getElementById(secondId)).toBeNull()
    expect(countdown().getAttribute('aria-label')).toBe('usage_stats.credentials_quota_resets_at:{"time":"05-13 18:15"}')
    const thirdId = await focusTooltip()
    await renderDrawer({ selection: second, timeZone: 'Asia/Shanghai', open: false })
    expect(document.getElementById(thirdId)).toBeNull()
  })

  it('closes the shared quota breakdown tooltip on Escape without closing the drawer and clears it on close', async () => {
    const quota = {
      key: 'billing.weekly', label: 'Weekly', scope: 'billing', percent: 16, barPercent: 84, percentKind: 'used' as const, status: 'ok' as const,
      usageBreakdown: [{ product: 'GrokBuild', usedPercent: 14 }, { product: 'GrokChat', usedPercent: 2 }],
    }
    const onClose = vi.fn()
    const selection: CredentialDetailSelection = { kind: 'credential', row: { ...credentialRow, displayQuotas: [quota] } }
    await renderDrawer({ onClose, selection })
    const infoButton = document.querySelector<HTMLButtonElement>('[data-credential-overview-section="quota"] [data-quota-key] button')!
    const tooltip = () => document.getElementById(infoButton.getAttribute('aria-describedby') ?? '')

    await act(async () => infoButton.focus())
    expect(tooltip()?.textContent).toContain('GrokBuild: 14%')
    await act(async () => infoButton.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })))
    expect(tooltip()).toBeNull()
    expect(onClose).not.toHaveBeenCalled()
    await act(async () => infoButton.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })))
    expect(onClose).toHaveBeenCalledOnce()

    await act(async () => infoButton.click())
    const openedId = infoButton.getAttribute('aria-describedby')!
    expect(document.getElementById(openedId)).not.toBeNull()
    await renderDrawer({ onClose, selection, open: false })
    expect(document.getElementById(openedId)).toBeNull()
    expect(document.body.querySelector('[data-credential-overview-section="quota"]')).not.toBeNull()
  })

  it('retains the displayed model list inside a scrolled drawer while the next range loads', async () => {
    const pending = Promise.withResolvers<unknown>()
    const totals = { request_count: 1, input_tokens: 10, output_tokens: 5, reasoning_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, total_tokens: 15, cost_usd: 1, unavailable_cost_count: 0, cost_available: true }
    fetchUsageIdentityModels.mockReset().mockResolvedValueOnce({ identity_id: 'credential-1', range: '7d', models: [{ ...totals, model: 'upstream' }], summary: totals }).mockReturnValueOnce(pending.promise)
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
    expect(panel.querySelector('tbody th')?.textContent).toBe('upstream')
    expect(panel.querySelector('[data-model-range="7d"]')?.getAttribute('aria-pressed')).toBe('true')
    expect(panel.querySelector('[data-model-range="today"]')?.getAttribute('aria-busy')).toBe('true')
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

  it('updates cached activity when the selected identity or its snapshot changes', async () => {
    await renderDrawer()
    const values = () => [...document.querySelectorAll('[data-credential-overview-section="activity"] [data-credential-activity-metric] strong')].map(node => node.textContent)
    expect(values()).toEqual(['10', '1', '42.50%'])
    const secondHealth = healthSnapshot(31, 4, 1_000, 661)
    const second: CredentialDetailSelection = { kind: 'ai-provider', row: withHealth(secondSelection.row, secondHealth) }
    await renderDrawer({ selection: second })
    expect(values()).toEqual(['35', '4', '66.10%'])
    const updatedHealth = healthSnapshot(50, 2, 400, 300)
    const updated: CredentialDetailSelection = { kind: 'ai-provider', row: withHealth(second.row, updatedHealth) }
    await renderDrawer({ selection: updated })
    expect(values()).toEqual(['52', '2', '75.00%'])
  })

  it('aborts model requests on identity change and close and discards late authorization errors', async () => {
    const old = Promise.withResolvers<unknown>()
    const next = Promise.withResolvers<unknown>()
    fetchUsageIdentityModels.mockReturnValueOnce(old.promise).mockReturnValueOnce(next.promise)
    const onAuthRequired = vi.fn()
    await renderDrawer({ onAuthRequired })
    const firstModelsSignal = fetchUsageIdentityModels.mock.calls[0][2] as AbortSignal
    await renderDrawer({ selection: secondSelection, onAuthRequired })
    expect(firstModelsSignal.aborted).toBe(true)
    const secondModelsSignal = fetchUsageIdentityModels.mock.calls[1][2] as AbortSignal
    await act(async () => old.reject(new ApiError('expired', 401)))
    expect(onAuthRequired).not.toHaveBeenCalled()
    await renderDrawer({ selection: secondSelection, open: false, onAuthRequired })
    expect(secondModelsSignal.aborted).toBe(true)
    await act(async () => next.reject(new ApiError('expired', 401)))
    expect(onAuthRequired).not.toHaveBeenCalled()
  })

  it('cancels a pending model range and cleans its tooltip when leaving the Overview tab', async () => {
    const pending = Promise.withResolvers<unknown>()
    const totals = { request_count: 1, input_tokens: 10, output_tokens: 5, reasoning_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, total_tokens: 15, cost_usd: 1, unavailable_cost_count: 0, cost_available: true }
    const initial = { identity_id: 'provider-1', range: '7d', models: [{ ...totals, model: 'upstream' }], summary: totals }
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
    expect(document.body.querySelector('[data-credential-model-usage] tbody th')?.textContent).toBe('upstream')
  })

  it('cleans an open model tooltip on close while preserving the drawer animation snapshot', async () => {
    const totals = { request_count: 1, input_tokens: 10, output_tokens: 5, reasoning_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, total_tokens: 15, cost_usd: 1, unavailable_cost_count: 0, cost_available: true }
    fetchUsageIdentityModels.mockResolvedValue({ identity_id: 'provider-1', models: [{ ...totals, model: 'upstream' }], summary: totals })
    await renderDrawer()
    await act(async () => document.body.querySelector<HTMLElement>('[data-token-breakdown]')!.focus())
    expect(document.body.querySelector('[role="tooltip"]:not([aria-hidden])')).not.toBeNull()
    await renderDrawer({ open: false })
    expect(document.body.querySelector('[role="tooltip"]:not([aria-hidden])')).toBeNull()
    expect(document.body.querySelector('[data-credential-overview-section="cumulative"]')).not.toBeNull()
  })

  it.each([
    { label: 'success boundaries', successRate: 95, cacheReadRate: 80, expectedTones: ['success', 'success'] },
    { label: 'warning boundaries', successRate: 80, cacheReadRate: 50, expectedTones: ['warning', 'warning'] },
    { label: 'below warning boundaries', successRate: 79.99, cacheReadRate: 49.99, expectedTones: ['danger', 'danger'] },
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
