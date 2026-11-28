import type { CredentialDetailSelection, CredentialRow } from '../credentialViewModels'

const QUOTA_PROVIDER_TYPES = new Set(['antigravity', 'codex', 'gemini-cli', 'claude', 'kimi', 'meta', 'xai'])
const QUOTA_HISTORY_PROVIDER_TYPES = new Set(['codex', 'claude', 'antigravity', 'xai'])

export function isRowRefreshing(row: CredentialRow): boolean {
  return row.refreshStatus === 'queued' || row.refreshStatus === 'running'
}

export function credentialResetCapabilities(row: CredentialRow) {
  const normalizedProvider = row.identity.provider?.trim().toLowerCase() ?? ''
  const quotaProvider = QUOTA_PROVIDER_TYPES.has(normalizedProvider) ? normalizedProvider : row.identity.type?.trim().toLowerCase()
  const isClaude = quotaProvider === 'claude'
  const resetCredits = (isClaude ? row.claudeResetGrants?.availableCount : row.quotaResetCreditsAvailableCount) ?? 0
  return { isClaude, resetCredits, canResetQuota: resetCredits > 0 && !row.identity.is_deleted && !isRowRefreshing(row) && !row.quotaResetting }
}

// 限额历史能力严格以 identity.type 为权威口径，与后端 QuotaHistoryProvider 注册表保持一致。
export function resolveCredentialQuotaHistoryProvider(identity: Pick<CredentialRow['identity'], 'type'> | null | undefined): string | undefined {
  const normalizedType = identity?.type?.trim().toLowerCase() ?? ''
  return QUOTA_HISTORY_PROVIDER_TYPES.has(normalizedType) ? normalizedType : undefined
}

export function supportsCredentialQuotaHistory(selection: CredentialDetailSelection | null | undefined): boolean {
  if (selection?.kind !== 'credential') return false
  return resolveCredentialQuotaHistoryProvider(selection.row.identity) !== undefined
}
