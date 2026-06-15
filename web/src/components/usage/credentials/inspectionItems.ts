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
  // 以下字段仅供 disable_limited 使用：携带恢复时间信息，决定该项是否可被临时禁用。
  upstreamMessage?: string
  sourceRequestId?: string
  recoverAt?: string
  resetsAt?: string
  resetsInSeconds?: number
  // canDisableLimited=false 时 disabledReason 说明为什么不能禁用（供 UI 展示）。
  canDisableLimited?: boolean
  disabledReason?: string
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
// 巡检结果（UsageQuotaInspectionResult）本身不携带 recover_at，所以 recoverAt/resetsAt/resetsInSeconds
// 通常为空。此时 canDisableLimited=false，前端在弹窗中禁用该项的选择并显示 disabledReason，
// 而不是让用户点击提交后才看到 skipped_missing_recover_at。
export function buildLimitedInspectionAccountItems(results: UsageQuotaInspectionResult[]): InspectionAccountItem[] {
  const seen = new Set<string>()
  const items: InspectionAccountItem[] = []
  for (const result of results) {
    if (result.status !== 'limit_reached' && result.http_status_code !== 429) continue
    const idx = (result.auth_index ?? '').trim()
    if (!idx || seen.has(idx)) continue
    seen.add(idx)
    const item: InspectionAccountItem = {
      key: idx,
      label: result.file_name ?? idx,
      fileName: result.file_name,
      authIndex: idx,
      upstreamMessage: result.error ?? 'usage_limit_reached (inspection)',
      sourceRequestId: result.request_id,
    }
    // 巡检结果不携带 recover_at/resets_at，这些字段通常为空。
    // canDisableLimited 取决于是否有恢复时间。当前后端不支持默认 duration，没有恢复时间就不能禁用。
    const hasRecoverTime = Boolean(item.recoverAt || item.resetsAt || (item.resetsInSeconds && item.resetsInSeconds > 0))
    if (hasRecoverTime) {
      item.canDisableLimited = true
    } else {
      item.canDisableLimited = false
      item.disabledReason = 'no_recover_time'
    }
    items.push(item)
  }
  return items
}

// toDisableLimitedRequestItems 把选中的账号项转成 API 请求的 items 数组。
// 只传 canDisableLimited=true 的项，过滤掉无恢复时间的项，避免后端返回 skipped_missing_recover_at。
export function toDisableLimitedRequestItems(items: InspectionAccountItem[]): CooldownDisableLimitedRequestItem[] {
  return items
    .filter((item) => item.canDisableLimited !== false)
    .map((item) => ({
      auth_index: item.authIndex ?? item.key,
      recover_at: item.recoverAt,
      resets_at: item.resetsAt,
      resets_in_seconds: item.resetsInSeconds,
      upstream_message: item.upstreamMessage ?? 'usage_limit_reached (inspection)',
      request_id: item.sourceRequestId,
    }))
}
