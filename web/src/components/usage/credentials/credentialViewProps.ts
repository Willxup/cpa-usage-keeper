import type { CredentialRow, AiProviderCredentialRow } from './credentialViewModels'
import type { UsageQuotaResetResponse } from '@/lib/types'
import type { QuotaUsageMode } from './quota/CredentialQuotaPanel'

export interface CredentialViewProps {
  rows: CredentialRow[]
  timeZone?: string
  quotaUsageMode: QuotaUsageMode
  onRefreshQuotaForAuthIndex: (authIndex: string) => Promise<void>
  onResetQuotaForAuthIndex: (authIndex: string, grantId?: string, organizationId?: string) => Promise<UsageQuotaResetResponse | void>
  onEdit?: (row: CredentialRow) => void
  onOpenDetails?: (row: CredentialRow) => void
  statusPendingIdentityIds?: ReadonlySet<string>
  onToggleStatus?: (identityId: string, authIndex: string, disabled: boolean) => void
  onSavePriority?: (identityId: string, authIndex: string, priority: number) => Promise<void>
}
export interface AiProviderViewProps {
  rows: AiProviderCredentialRow[]
  onEdit?: (row: AiProviderCredentialRow) => void
  onOpenDetails?: (row: AiProviderCredentialRow) => void
  statusPendingIdentityIds?: ReadonlySet<string>
  onToggleStatus?: (identityId: string, authIndex: string, disabled: boolean) => void
  onSavePriority?: (identityId: string, authIndex: string, priority: number) => Promise<void>
}
