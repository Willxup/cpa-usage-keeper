import type { UsageIdentity, UsageQuotaRow } from '@/lib/types'

export const CREDENTIALS_PAGE_SIZE = 10

type QuotaStatus = 'ok' | 'warning' | 'danger' | 'unknown'
export type PlanTypeTone = 'free' | 'team' | 'plus' | 'pro' | 'neutral'

export interface DisplayQuota {
  key: string
  label: string
  percent: number | null
  barPercent: number | null
  percentKind: 'used' | 'remaining' | 'unknown'
  used?: number
  limit?: number
  remaining?: number
  resetText?: string
  windowSeconds?: number
  status: QuotaStatus
}

export interface AuthFileCredentialRow {
  identity: UsageIdentity
  displayName: string
  maskedIdentity: string
  providerLabel: string
  typeLabel: string
  authTypeLabel: string
  planTypeLabel?: string
  planTypeTone?: PlanTypeTone
  remainingDaysLabel?: string
  totalRequests: number
  successCount: number
  failureCount: number
  successRate: number | null
  totalTokens: number
  cacheRate: number | null
  quota: UsageQuotaRow[]
  quotaLoading: boolean
  quotaError?: string
  refreshTaskId?: string
  refreshStatus?: 'queued' | 'running' | 'completed' | 'failed'
  primaryQuota?: DisplayQuota
  secondaryQuota?: DisplayQuota
  extraQuota: DisplayQuota[]
}

export interface AiProviderCredentialRow {
  identity: UsageIdentity
  displayName: string
  maskedIdentity: string
  providerLabel: string
  typeLabel: string
  authTypeLabel: string
  totalRequests: number
  successCount: number
  failureCount: number
  successRate: number | null
  totalTokens: number
  cacheRate: number | null
  lastUsedText?: string
  statsUpdatedText?: string
}

export interface CredentialIdentityGroups {
  authFiles: UsageIdentity[]
  aiProviders: UsageIdentity[]
}

export interface CredentialsPage<T> {
  items: T[]
  page: number
  pageSize: number
  total: number
  totalPages: number
}

export function splitCredentialIdentities(identities: UsageIdentity[]): CredentialIdentityGroups {
  return identities.reduce<CredentialIdentityGroups>((groups, identity) => {
    if (identity.auth_type === 1) {
      groups.authFiles.push(identity)
    } else if (identity.auth_type === 2) {
      groups.aiProviders.push(identity)
    }
    return groups
  }, { authFiles: [], aiProviders: [] })
}

export function selectQuotaEligibleAuthIndexes(identities: UsageIdentity[]): string[] {
  return identities
    .filter((identity) => identity.auth_type === 1 && !identity.is_deleted)
    .map((identity) => identity.identity)
}

export function paginateCredentials<T>(items: T[], page: number, pageSize = CREDENTIALS_PAGE_SIZE): CredentialsPage<T> {
  const normalizedPageSize = Math.max(1, Math.floor(pageSize))
  const totalPages = Math.max(1, Math.ceil(items.length / normalizedPageSize))
  const normalizedPage = Math.min(Math.max(1, Math.floor(page)), totalPages)
  const start = (normalizedPage - 1) * normalizedPageSize

  return {
    items: items.slice(start, start + normalizedPageSize),
    page: normalizedPage,
    pageSize: normalizedPageSize,
    total: items.length,
    totalPages,
  }
}

export function buildAuthFileCredentialRows(
  // Auth Files 行合并 usage identity、缓存 quota 和刷新任务状态，组件不再重复拼装字段。
  identities: UsageIdentity[],
  quotas: Map<string, UsageQuotaRow[]> = new Map(),
  quotaStates: Map<string, Pick<AuthFileCredentialRow, 'quotaLoading' | 'quotaError' | 'refreshTaskId' | 'refreshStatus'>> = new Map(),
): AuthFileCredentialRow[] {
  return identities.map((identity) => {
    const quota = quotas.get(identity.identity) ?? []
    const state = quotaStates.get(identity.identity)
    const displayQuotas = quota.map(toDisplayQuota)
    const planType = firstNonEmpty(...quota.map((row) => row.planType), identity.plan_type)
    // 先挑 5h 主窗口，再挑 Weekly 次窗口，其余限额保留到 chips 中展示。
    const primaryQuota = displayQuotas.find(isPrimaryQuota)
    const secondaryQuota = displayQuotas.find((item) => item !== primaryQuota && isSecondaryQuota(item))
    const extraQuota = displayQuotas.filter((item) => item !== primaryQuota && item !== secondaryQuota)

    return {
      identity,
      displayName: credentialDisplayName(identity),
      maskedIdentity: identity.identity,
      providerLabel: credentialProviderLabel(identity),
      typeLabel: credentialTypeLabel(identity),
      authTypeLabel: credentialAuthTypeLabel(identity),
      planTypeLabel: credentialPlanTypeLabel(planType),
      planTypeTone: credentialPlanTypeTone(planType),
      remainingDaysLabel: remainingDaysLabel(identity.active_until),
      totalRequests: safeNumber(identity.total_requests),
      successCount: safeNumber(identity.success_count),
      failureCount: safeNumber(identity.failure_count),
      successRate: successRate(identity),
      totalTokens: safeNumber(identity.total_tokens),
      cacheRate: cacheRate(identity),
      quota,
      quotaLoading: state?.quotaLoading ?? false,
      quotaError: state?.quotaError,
      refreshTaskId: state?.refreshTaskId,
      refreshStatus: state?.refreshStatus,
      primaryQuota,
      secondaryQuota,
      extraQuota,
    }
  })
}

export function buildAiProviderCredentialRows(identities: UsageIdentity[]): AiProviderCredentialRow[] {
  return identities.map((identity) => ({
    identity,
    displayName: credentialDisplayName(identity),
    maskedIdentity: identity.identity,
    providerLabel: credentialProviderLabel(identity),
    typeLabel: credentialTypeLabel(identity),
    authTypeLabel: credentialAuthTypeLabel(identity),
    totalRequests: safeNumber(identity.total_requests),
    successCount: safeNumber(identity.success_count),
    failureCount: safeNumber(identity.failure_count),
    successRate: successRate(identity),
    totalTokens: safeNumber(identity.total_tokens),
    cacheRate: cacheRate(identity),
    lastUsedText: identity.last_used_at,
    statsUpdatedText: identity.stats_updated_at,
  }))
}

function toDisplayQuota(row: UsageQuotaRow): DisplayQuota {
  // 后端 quota row 可能是 used、remaining 或 remainingFraction，这里统一成展示进度。
  const used = finiteNumber(row.used)
  const limit = finiteNumber(row.limit)
  const remaining = finiteNumber(row.remaining)
  const percentDisplay = quotaPercent(row, used, limit)

  const windowSeconds = finiteNumber(row.window?.seconds)
  const label = quotaLabel(row, windowSeconds)

  return {
    key: row.key,
    label,
    percent: percentDisplay.percent,
    barPercent: quotaBarPercent(percentDisplay.percent, percentDisplay.kind),
    percentKind: percentDisplay.kind,
    used,
    limit,
    remaining,
    resetText: row.resetAt,
    windowSeconds,
    status: quotaStatus(row, percentDisplay.percent, percentDisplay.kind),
  }
}

function quotaLabel(row: UsageQuotaRow, windowSeconds?: number): string {
  // 对已知窗口按秒数纠正标签；未知窗口不猜 5h/Weekly，避免误导用户。
  const label = row.label || row.metric || row.scope || row.key
  if (windowSeconds === 604800 && label.includes('5h')) {
    return label.replace('5h', 'Weekly')
  }
  if (windowSeconds === 18000 && label.includes('Weekly')) {
    return label.replace('Weekly', '5h')
  }
  if (windowSeconds !== undefined && windowSeconds !== 18000 && windowSeconds !== 604800) {
    return unknownWindowLabel(label)
  }
  return label
}

function unknownWindowLabel(label: string): string {
  if (label === '5h' || label === 'Weekly') {
    return 'Window'
  }
  if (label.includes('5h')) {
    return label.replace('5h', 'Window')
  }
  if (label.includes('Weekly')) {
    return label.replace('Weekly', 'Window')
  }
  return label
}

function quotaPercent(row: UsageQuotaRow, used?: number, limit?: number): { percent: number | null; kind: DisplayQuota['percentKind'] } {
  // 优先使用 provider 已给出的百分比；没有时才用 used/limit 推导。
  const usedPercent = finiteNumber(row.usedPercent)
  if (usedPercent !== undefined) {
    return { percent: clampPercent(usedPercent), kind: 'used' }
  }
  const remainingFraction = finiteNumber(row.remainingFraction)
  if (remainingFraction !== undefined) {
    return { percent: clampPercent(remainingFraction * 100), kind: 'remaining' }
  }
  if (used !== undefined && limit !== undefined && limit > 0) {
    return { percent: clampPercent((used / limit) * 100), kind: 'used' }
  }
  return { percent: null, kind: 'unknown' }
}

function quotaStatus(row: UsageQuotaRow, percent: number | null, kind: DisplayQuota['percentKind']): QuotaStatus {
  if (row.limitReached) {
    return 'danger'
  }
  const remainingPercent = quotaBarPercent(percent, kind)
  if (remainingPercent === null) {
    return 'unknown'
  }
  if (remainingPercent < 20) {
    return 'danger'
  }
  if (remainingPercent < 50) {
    return 'warning'
  }
  return 'ok'
}

function quotaBarPercent(percent: number | null, kind: DisplayQuota['percentKind']): number | null {
  // 进度条表达“剩余额度”：已用越高条越短，剩余比例则直接使用。
  if (percent === null) {
    return null
  }
  return kind === 'used' ? clampPercent(100 - percent) : percent
}

function isPrimaryQuota(quota: DisplayQuota): boolean {
  if (quota.windowSeconds !== undefined) {
    return quota.windowSeconds === 18000
  }
  const haystack = `${quota.key} ${quota.label}`.toLowerCase()
  return haystack.includes('5h') || haystack.includes('five_hour')
}

function isSecondaryQuota(quota: DisplayQuota): boolean {
  if (quota.windowSeconds !== undefined) {
    return quota.windowSeconds === 604800
  }
  const haystack = `${quota.key} ${quota.label}`.toLowerCase()
  return haystack.includes('weekly') || haystack.includes('seven_day')
}

function credentialDisplayName(identity: UsageIdentity): string {
  return firstNonEmpty(identity.displayName, identity.name, identity.identity) ?? '-'
}

function credentialProviderLabel(identity: UsageIdentity): string {
  return firstNonEmpty(identity.provider, identity.type) ?? '-'
}

function credentialTypeLabel(identity: UsageIdentity): string {
  return firstNonEmpty(identity.type, identity.provider) ?? '-'
}

function credentialAuthTypeLabel(identity: UsageIdentity): string {
  return firstNonEmpty(identity.auth_type_name) ?? (identity.auth_type === 1 ? 'Auth file' : 'AI provider')
}

function credentialPlanTypeLabel(planType?: string): string | undefined {
  const tone = credentialPlanTypeTone(planType)
  if (!tone) {
    return undefined
  }
  const label = tone === 'neutral' ? firstNonEmpty(planType) : tone
  return label ? label.charAt(0).toUpperCase() + label.slice(1) : undefined
}

function credentialPlanTypeTone(planType?: string): PlanTypeTone | undefined {
  // planType 展示只做宽松匹配和样式分类，不改变后端原始字段。
  const normalized = planType?.trim().toLowerCase()
  if (!normalized) {
    return undefined
  }
  if (normalized.includes('pro')) {
    return 'pro'
  }
  if (normalized === 'plus') {
    return 'plus'
  }
  if (normalized === 'team') {
    return 'team'
  }
  if (normalized === 'free') {
    return 'free'
  }
  return 'neutral'
}

function remainingDaysLabel(activeUntil?: string): string | undefined {
  if (!activeUntil) {
    return undefined
  }
  const untilMs = Date.parse(activeUntil)
  if (!Number.isFinite(untilMs)) {
    return undefined
  }
  const dayMs = 24 * 60 * 60 * 1000
  return `${Math.max(0, Math.ceil((untilMs - Date.now()) / dayMs))}d`
}

function successRate(identity: UsageIdentity): number | null {
  const total = safeNumber(identity.total_requests)
  if (total <= 0) {
    return null
  }
  return (safeNumber(identity.success_count) / total) * 100
}

function cacheRate(identity: UsageIdentity): number | null {
  const inputTokens = safeNumber(identity.input_tokens)
  if (inputTokens <= 0) {
    return null
  }
  return (safeNumber(identity.cached_tokens) / inputTokens) * 100
}

function firstNonEmpty(...values: Array<string | undefined>): string | undefined {
  for (const value of values) {
    const trimmed = value?.trim()
    if (trimmed) {
      return trimmed
    }
  }
  return undefined
}

function safeNumber(value: number | undefined): number {
  return Number.isFinite(value) ? Number(value) : 0
}

function finiteNumber(value: number | undefined): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

function clampPercent(value: number): number {
  return Math.max(0, Math.min(100, value))
}
