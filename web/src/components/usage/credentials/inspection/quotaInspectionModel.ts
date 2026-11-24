import type { UsageQuotaInspectionResult, UsageQuotaInspectionResultStatus, UsageQuotaInspectionStatusResponse } from '@/lib/types'

type InspectionIndicatorTone = 'idle' | 'running' | 'completed'
export type InspectionResultStatusFilter = 'normal' | 'limit_reached' | 'unauthorized_401_402' | 'other_failed'
export type InspectionResultStatusFilterState = InspectionResultStatusFilter | null
export type InspectionStatTone = 'normal' | 'limitReached' | 'unauthorized' | 'failed' | 'unknown'
export const INSPECTION_RESULT_PAGE_SIZE_OPTIONS = [10, 20, 50] as const
export const DEFAULT_INSPECTION_RESULT_PAGE_SIZE = INSPECTION_RESULT_PAGE_SIZE_OPTIONS[0]
const INSPECTION_SELECTABLE_RESULT_STATUSES = new Set<InspectionResultStatusFilter>(['normal', 'limit_reached', 'unauthorized_401_402', 'other_failed'])
const INVALID_INSPECTION_ACCOUNT_STATUSES = new Set<UsageQuotaInspectionResultStatus>(['unauthorized_401', 'payment_required_402'])

export function inspectionProgressTotal(status: Pick<UsageQuotaInspectionStatusResponse, 'total' | 'unknown'> | null): number {
  // 弹框进度条展示的是“巡检刷新任务进度”，不是 Credentials 总账号覆盖率。
  if (!status) {
    return 0
  }
  // unknown 代表未参与刷新或没有可解析缓存的账号，不参与进度百分比分母。
  return Math.max(0, status.total - status.unknown)
}

export function formatInspectionProgressPercent(status: Pick<UsageQuotaInspectionStatusResponse, 'total' | 'cached' | 'unknown'> | null): number {
  // 初始加载或接口失败时没有状态，进度保持 0，避免前端自行 fallback total。
  if (!status) {
    return 0
  }
  // 分母统一走 inspectionProgressTotal，保证显示文本和进度条使用同一口径。
  const progressTotal = inspectionProgressTotal(status)
  if (progressTotal <= 0) {
    return 0
  }
  // cached 可能因为缓存恢复或并发刷新短暂超过分母，最终百分比要钳制在 0-100。
  return Math.max(0, Math.min(100, Math.round((status.cached / progressTotal) * 100)))
}

export function isInspectionStartDisabled({ starting, total, running }: { starting: boolean; total: number; running: boolean }): boolean {
  // running 只代表显式巡检轮次；定时刷新和手动刷新共用队列但不禁用手动巡检入口。
  return starting || running || total <= 0
}

export function inspectionIndicatorTone(status: Pick<UsageQuotaInspectionStatusResponse, 'running' | 'completed' | 'completed_at'> | null): InspectionIndicatorTone {
  // 黄色点只看显式巡检 running，不响应普通手动刷新/自动刷新。
  if (status?.running) {
    return 'running'
  }
  // 绿色点必须有 completed_at；completed 没有时间时不展示完成态，避免共享缓存误点亮。
  if (status?.completed_at) {
    return 'completed'
  }
  return 'idle'
}

export function isSelectableInspectionStatusFilter(status: unknown): status is InspectionResultStatusFilter {
  return typeof status === 'string' && INSPECTION_SELECTABLE_RESULT_STATUSES.has(status as InspectionResultStatusFilter)
}

export function nextInspectionResultStatusFilter(current: InspectionResultStatusFilterState, next: InspectionResultStatusFilter): InspectionResultStatusFilterState {
  return current === next ? null : next
}

export function buildInspectionResultsPage(results: UsageQuotaInspectionResult[], statusFilter: InspectionResultStatusFilterState, page: number, pageSize: number): { results: UsageQuotaInspectionResult[]; total: number; totalPages: number; page: number; pageSize: number } {
  const safePageSize = INSPECTION_RESULT_PAGE_SIZE_OPTIONS.includes(pageSize as (typeof INSPECTION_RESULT_PAGE_SIZE_OPTIONS)[number])
    ? pageSize
    : DEFAULT_INSPECTION_RESULT_PAGE_SIZE
  const filteredResults = statusFilter ? results.filter((result) => matchesInspectionResultStatusFilter(result.status, statusFilter)) : results
  const total = filteredResults.length
  const totalPages = Math.max(1, Math.ceil(total / safePageSize))
  const safePage = Math.max(1, Math.min(Math.floor(page) || 1, totalPages))
  const start = (safePage - 1) * safePageSize
  return {
    results: filteredResults.slice(start, start + safePageSize),
    total,
    totalPages,
    page: safePage,
    pageSize: safePageSize,
  }
}

function matchesInspectionResultStatusFilter(status: UsageQuotaInspectionResultStatus, filter: InspectionResultStatusFilter): boolean {
  // 摘要卡把 401/402 合并，但结果行仍保留原始状态，方便禁用/删除按行处理。
  if (filter === 'unauthorized_401_402') {
    return status === 'unauthorized_401' || status === 'payment_required_402'
  }
  return status === filter
}

export function isQuotaInspectionCloseDisabled({ invalidAccountActionOpen, settingsModalOpen }: {
  invalidAccountActionOpen: boolean
  settingsModalOpen: boolean
}): boolean {
  return invalidAccountActionOpen || settingsModalOpen
}

export function buildInvalidInspectionAccountFileNames(results: UsageQuotaInspectionResult[]): string[] {
  const seen = new Set<string>()
  const names: string[] = []
  for (const result of results) {
    if (!INVALID_INSPECTION_ACCOUNT_STATUSES.has(result.status)) {
      continue
    }
    const fileName = (result.file_name ?? '').trim()
    if (!fileName || seen.has(fileName)) {
      continue
    }
    seen.add(fileName)
    names.push(fileName)
  }
  return names
}

export function selectAllInvalidInspectionAccountFileNames(fileNames: string[]): string[] {
  return [...fileNames]
}

export function invertInvalidInspectionAccountFileNames(fileNames: string[], selectedFileNames: string[]): string[] {
  const selected = new Set(selectedFileNames)
  return fileNames.filter((fileName) => !selected.has(fileName))
}

export function formatInspectionCompletedAt(value: string | undefined): string {
  if (!value) {
    return ''
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  return date.toLocaleString()
}

