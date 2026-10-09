import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { AiProviderCredentialsSection } from '../AiProviderCredentialsSection'
import { createAiProviderSectionProps } from './credentialSectionFixtures'
import type { AiProviderCredentialRow } from '../credentialViewModels'

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({
    t: (key: string, params?: { count?: number }) => (key === 'usage_stats.credentials_count' ? `${params?.count ?? 0}` : key),
  }),
}))

describe('AiProviderCredentialsSection', () => {
  it('renders the AI Provider title without the Credentials eyebrow', () => {
    const html = renderToStaticMarkup(
      <AiProviderCredentialsSection
        {...createAiProviderSectionProps()}
        rows={[]}
        total={0}
      />,
    )

    expect(html).toContain('usage_stats.credentials_ai_providers_title')
    expect(html).not.toContain('usage_stats.credentials_ai_providers_eyebrow')
  })

  it('renders the shared provider logo before the name without the type badge', () => {
    const row = {
      identity: {
        id: '1',
        name: 'Provider Key',
        auth_type: 2,
        auth_type_name: 'apikey',
        identity: 'sk-provider',
        type: 'claude',
        provider: 'anthropic',
        priority: 5,
        total_requests: 0,
        success_count: 0,
        failure_count: 0,
        input_tokens: 0,
        output_tokens: 0,
        reasoning_tokens: 0,
        cache_read_tokens: 0,
        total_tokens: 0,
        last_aggregated_usage_event_id: '0',
        is_deleted: false,
        created_at: '2026-05-10T00:00:00Z',
        updated_at: '2026-05-10T00:00:00Z',
      },
      displayName: 'Provider Key',
      maskedIdentity: 'sk-provider',
      providerLabel: 'anthropic',
      typeLabel: 'claude',
      authTypeLabel: 'apikey',
      priorityLabel: 'P5',
      totalRequests: 0,
      successCount: 0,
      failureCount: 0,
      successRate: null,
      totalTokens: 0,
      cacheReadRate: null,
      windowCacheReadRate: 61.75,
      lastUsedText: '2026-05-10T10:00:00Z',
      statsUpdatedText: '2026-05-10T10:02:00Z',
      remainingDaysLabel: '25d',
      primaryQuota: { label: '5h' },
      secondaryQuota: { label: 'Weekly' },
    } satisfies AiProviderCredentialRow & Record<string, unknown>

    const html = renderToStaticMarkup(
      <AiProviderCredentialsSection
        {...createAiProviderSectionProps()}
        rows={[row]}
        total={1}
      />,
    )

    expect(html.match(/usage_stats\.total_requests/g)).toHaveLength(1)
    expect(html.match(/usage_stats\.success_rate/g)).toHaveLength(1)
    expect(html.match(/usage_stats\.total_tokens/g)).toHaveLength(1)
    expect(html.match(/usage_stats\.cache_rate/g)).toHaveLength(1)
    expect(html).toContain('usage_stats.credentials_column_name')
    expect(html).toContain('usage_stats.credentials_column_health')
    expect(html).toContain('usage_stats.credentials_health_last_5h')
    // 5h 缓存率只是健康面板 meta 区的一行文字，不再额外画一条柱状图。
    expect(html).toContain('usage_stats.credentials_health_cache_rate_5h')
    expect(html).toContain('61.75%')
    expect(html).toContain('usage_stats.credentials_last_used')
    expect(html).toContain('usage_stats.credentials_stats_updated')
    expect(html).toContain('05/10 10:00')
    expect(html).toContain('05/10 10:02')
    expect(html).toContain('data-provider-brand-icon="claude"')
    expect(html.indexOf('data-provider-brand-icon="claude"')).toBeLessThan(html.lastIndexOf('Provider Key'))
    // 图标本身交给按钮承载语义，按钮名称使用凭证 displayName，状态由 aria-pressed 表达。
    expect(html).toContain('data-credential-status-toggle="true"')
    expect(html).toContain('data-credential-status-disabled="false"')
    expect(html).toContain('aria-pressed="true"')
    expect(html).toContain('aria-label="Provider Key"')
    expect(html).not.toContain('>claude</span>')
    expect(html).toContain('P5')
    expect(html).toContain('usage_stats.credentials_sort_priority')
    expect(html).toContain('aria-label="usage_stats.credentials_sort_label: usage_stats.credentials_sort_priority"')
    expect(html).toContain('usage_stats.credentials_sort_last_used')
    expect(html).not.toContain('Team')
    expect(html).not.toContain('25d')
    expect(html).not.toContain('Weekly')
    expect(html).not.toContain('usage_stats.credentials_column_quota')
    expect(html).not.toContain('usage_stats.credentials_auth_files_display_mode_quota')
    expect(html).not.toContain('usage_stats.credentials_auth_files_display_mode_health')
  })

  it('marks OpenAI-compatible icons as unavailable and keeps them keyboard reachable', () => {
    const row = {
      identity: {
        id: 'openai-1',
        name: 'OpenAI Compatibility',
        auth_type: 2,
        auth_type_name: 'apikey',
        identity: 'openai-auth',
        type: 'openai',
        provider: 'OpenAI Compatibility',
        total_requests: 0,
        success_count: 0,
        failure_count: 0,
        input_tokens: 0,
        output_tokens: 0,
        reasoning_tokens: 0,
        cache_read_tokens: 0,
        total_tokens: 0,
        last_aggregated_usage_event_id: '0',
        is_deleted: false,
        created_at: '2026-05-10T00:00:00Z',
        updated_at: '2026-05-10T00:00:00Z',
      },
      displayName: 'OpenAI Compatibility',
      maskedIdentity: 'openai-auth',
      providerLabel: 'OpenAI Compatibility',
      typeLabel: 'openai',
      authTypeLabel: 'apikey',
      totalRequests: 0,
      successCount: 0,
      failureCount: 0,
      successRate: null,
      totalTokens: 0,
      cacheReadRate: null,
      windowCacheReadRate: null,
      lastUsedText: undefined,
      statsUpdatedText: undefined,
      priorityLabel: undefined,
    } as AiProviderCredentialRow & Record<string, unknown>

    const html = renderToStaticMarkup(
      <AiProviderCredentialsSection
        {...createAiProviderSectionProps({ rows: [row], total: 1 })}
      />,
    )

    expect(html).toContain('data-credential-status-unsupported="true"')
    // 说明不再依赖不可聚焦的原生 title，改为可聚焦元素的 aria-label + 提示浮层。
    expect(html).not.toContain('title="usage_stats.credentials_status_unsupported_tooltip"')
    expect(html).toContain('tabindex="0"')
    expect(html).toContain('aria-label="OpenAI Compatibility"')
    expect(html).not.toContain('data-credential-status-toggle="true"')
  })
})
