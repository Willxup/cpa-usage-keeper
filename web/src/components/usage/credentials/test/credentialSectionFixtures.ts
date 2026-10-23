import type { ComponentProps } from 'react'
import type { UsageCredentialHealth, UsageIdentity } from '@/lib/types'
import type { AiProviderCredentialsSection } from '../AiProviderCredentialsSection'
import type { CredentialsSection } from '../CredentialsSection'

const paginationDefaults = {
  total: 0, page: 1, totalPages: 1, pageSize: 10,
  activeOnly: false, sort: 'priority' as const, loading: false,
  onPageChange: () => undefined,
  onPageSizeChange: () => undefined,
  onActiveOnlyChange: () => undefined,
  onSortChange: () => undefined,
}

export const createAiProviderSectionProps = (
  overrides: Partial<ComponentProps<typeof AiProviderCredentialsSection>> = {},
): ComponentProps<typeof AiProviderCredentialsSection> => ({ rows: [], ...paginationDefaults, ...overrides })

export const createCredentialSectionProps = (
  overrides: Partial<ComponentProps<typeof CredentialsSection>> = {},
): ComponentProps<typeof CredentialsSection> => ({
  rows: [],
  timeZone: 'Asia/Shanghai',
  ...paginationDefaults,
  quotaRefreshing: false,
  quotaRefreshError: '',
  quotaInspectionStatus: null,
  quotaInspectionLoading: false,
  quotaInspectionStarting: false,
  quotaInspectionError: '',
  onRefreshQuota: async () => undefined,
  onRefreshQuotaForAuthIndex: async () => undefined,
  onResetQuotaForAuthIndex: async () => undefined,
  onRefreshInspectionStatus: async () => undefined,
  onStartInspection: async () => undefined,
  ...overrides,
})

export const createSectionHealth = (overrides: Partial<UsageCredentialHealth> = {}): UsageCredentialHealth => ({
  window_seconds: 18_000,
  bucket_seconds: 600,
  window_start: '2026-07-16T10:00:00+08:00',
  window_end: '2026-07-16T15:00:00+08:00',
  total_success: 2,
  total_failure: 1,
  success_rate: 66.6667,
  input_tokens: 800,
  cache_read_tokens: 300,
  buckets: [{
    start_time: '2026-07-16T10:00:00+08:00',
    end_time: '2026-07-16T10:10:00+08:00',
    success: 2,
    failure: 1,
    rate: 2 / 3,
  }],
  ...overrides,
})

export const createSectionIdentity = (overrides: Partial<UsageIdentity> = {}): UsageIdentity => ({
  id: 'credential-1',
  name: 'Credential One',
  displayName: 'Credential One',
  auth_type: 1,
  auth_type_name: 'oauth',
  identity: 'auth-1',
  type: 'codex',
  provider: 'OpenAI',
  prefix: '',
  disabled: false,
  priority: 1,
  total_requests: 1_234,
  success_count: 1_200,
  failure_count: 34,
  input_tokens: 320_000,
  output_tokens: 136_789,
  reasoning_tokens: 0,
  cache_read_tokens: 132_800,
  total_tokens: 456_789,
  last_aggregated_usage_event_id: '1',
  last_used_at: '2026-07-16T14:58:00+08:00',
  stats_updated_at: '2026-07-16T15:00:00+08:00',
  credential_health: createSectionHealth(),
  is_deleted: false,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-16T07:00:00Z',
  ...overrides,
})
