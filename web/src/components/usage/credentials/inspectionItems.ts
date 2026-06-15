import type { UsageQuotaInspectionResult } from '@/lib/types'
import type { CooldownDisableLimitedRequestItem } from '@/lib/api'

const INVALID_INSPECTION_ACCOUNT_STATUSES = new Set([
  'unauthorized_401',
  'payment_required_402',
  'other_failed',
])

export type InspectionAccountAction = 'disable_invalid' | 'delete_invalid' | 'disable_limited'

// InspectionAccountItem 是巡检结果中可被批量操作（禁用/删除/临时禁用限额）选中的账号项。
export interface InspectionAccountItem {
  key: string
  label: string
  fileName?: string
  authIndex?: string
  // 以下字段仅供 disable_limited 使用：携带恢复时间信息。
  upstreamMessage?: string
  sourceRequestId?: string
  recoverAt?: string
  resetsAt?: string
  resetsInSeconds?: number
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

// buildLimitedInspectionAccountItems 从巡检结果构建"临时禁用限额账号"的可选列表。
// 巡检结果可能不携带 recover_at，此时后端会使用默认持续时长兜底。
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
      recoverAt: result.recover_at,
      resetsAt: result.resets_at,
      resetsInSeconds: result.resets_in_seconds,
      upstreamMessage: result.error ?? 'usage_limit_reached (inspection)',
      sourceRequestId: result.request_id,
    })
  }
  return items
}

// toDisableLimitedRequestItems 把选中的账号项转成 API 请求的 items 数组。
// 没有恢复时间的项也允许提交，后端会使用 COOLDOWN_INSPECTION_DEFAULT_DURATION_MINUTES 兜底。
export function toDisableLimitedRequestItems(items: InspectionAccountItem[]): CooldownDisableLimitedRequestItem[] {
  return items.map((item) => ({
    auth_index: item.authIndex ?? item.key,
    recover_at: item.recoverAt,
    resets_at: item.resetsAt,
    resets_in_seconds: item.resetsInSeconds,
    upstream_message: item.upstreamMessage ?? 'usage_limit_reached (inspection)',
    request_id: item.sourceRequestId,
  }))
}
