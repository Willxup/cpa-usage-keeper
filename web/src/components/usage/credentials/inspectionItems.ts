import type { UsageQuotaInspectionResult } from '@/lib/types'
import type { CooldownDisableLimitedRequestItem } from '@/lib/api'

const INVALID_INSPECTION_ACCOUNT_STATUSES = new Set([
  'unauthorized_401',
  'payment_required_402',
  'other_failed',
])

export type InspectionAccountAction = 'disable_invalid' | 'delete_invalid' | 'disable_limited'

export interface InspectionAccountItem {
  key: string
  label: string
  fileName?: string
  authIndex?: string
}

export function buildInvalidInspectionAccountItems(results: UsageQuotaInspectionResult[]): InspectionAccountItem[] {
  const seen = new Set<string>()
  const items: InspectionAccountItem[] = []
  for (const result of results) {
    if (!INVALID_INSPECTION_ACCOUNT_STATUSES.has(result.status)) continue
    const fileName = (result.file_name ?? '').trim()
    if (!fileName || seen.has(fileName)) continue
    seen.add(fileName)
    items.push({
      key: fileName,
      label: fileName,
      fileName,
      authIndex: result.auth_index,
    })
  }
  return items
}

export function buildLimitedInspectionAccountItems(results: UsageQuotaInspectionResult[]): InspectionAccountItem[] {
  const seen = new Set<string>()
  const items: InspectionAccountItem[] = []
  for (const result of results) {
    if (result.status !== 'limit_reached' && result.http_status_code !== 429) continue
    const idx = (result.auth_index ?? '').trim()
    if (!idx || seen.has(idx)) continue
    seen.add(idx)
    items.push({
      key: idx,
      label: result.file_name ?? idx,
      fileName: result.file_name,
      authIndex: idx,
    })
  }
  return items
}

export function toDisableLimitedRequestItems(items: InspectionAccountItem[]): CooldownDisableLimitedRequestItem[] {
  return items.map((item) => ({
    auth_index: item.authIndex ?? item.key,
    upstream_message: 'usage_limit_reached (inspection)',
  }))
}
