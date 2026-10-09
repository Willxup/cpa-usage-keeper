import type { ComponentProps } from 'react'
import type { AiProviderCredentialsSection } from '../AiProviderCredentialsSection'
import type { AuthFileCredentialsSection } from '../AuthFileCredentialsSection'

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

export const createAuthFileSectionProps = (
  overrides: Partial<ComponentProps<typeof AuthFileCredentialsSection>> = {},
): ComponentProps<typeof AuthFileCredentialsSection> => ({
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
