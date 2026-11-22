import type { CredentialRow } from '../credentialViewModels'
const QUOTA_PROVIDER_TYPES = new Set(['antigravity', 'codex', 'gemini-cli', 'claude', 'kimi', 'meta', 'xai'])
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
