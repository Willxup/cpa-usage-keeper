import type { UsageCredentialHealth, UsageIdentity, UsageQuotaCheckResponse, UsageQuotaRow } from '@/lib/types'
import { calculateCacheRate, formatCompactTokenValue } from '@/utils/usage'

export const CREDENTIALS_PAGE_SIZE = 10
const FIVE_HOUR_WINDOW_SECONDS = 5 * 60 * 60
const WEEKLY_WINDOW_SECONDS = 7 * 24 * 60 * 60
const THIRTY_DAY_WINDOW_SECONDS = 30 * 24 * 60 * 60
const AVERAGE_MONTH_WINDOW_SECONDS = 365 * 24 * 60 * 60 / 12

type QuotaStatus = 'ok' | 'warning' | 'danger' | 'unknown'
export type PlanTypeTone = 'free' | 'team' | 'plus' | 'pro' | 'neutral'

export interface QuotaWindowUsageDisplay {
  tokens: string
  cost: string
}

export interface QuotaBillingUsageDisplay {
  used?: string
  limit?: string
  remaining?: string
}

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
  windowUsage?: QuotaWindowUsageDisplay
  windowUsageEstimate?: QuotaWindowUsageDisplay
  billingUsage?: QuotaBillingUsageDisplay
  status: QuotaStatus
}

export interface AuthFileCredentialRow {
  identity: UsageIdentity
  displayName: string
  maskedIdentity: string
  providerLabel: string
  typeLabel: string
  authTypeLabel: string
  priorityLabel?: string
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
  quotaResetCreditsAvailableCount?: number | null
  quotaLoading: boolean
  quotaError?: string
  refreshStatus?: 'queued' | 'running' | 'completed' | 'failed'
  quotaResetting?: boolean
  displayQuotas: DisplayQuota[]
  credentialHealth?: UsageCredentialHealth
}

export interface AiProviderCredentialRow {
  identity: UsageIdentity
  displayName: string
  maskedIdentity: string
  providerLabel: string
  typeLabel: string
  authTypeLabel: string
  priorityLabel?: string
  totalRequests: number
  successCount: number
  failureCount: number
  successRate: number | null
  totalTokens: number
  cacheRate: number | null
  lastUsedText?: string
  statsUpdatedText?: string
  credentialHealth?: UsageCredentialHealth
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
  quotas: Map<string, UsageQuotaCheckResponse> = new Map(),
  quotaStates: Map<string, Pick<AuthFileCredentialRow, 'quotaLoading' | 'quotaError' | 'refreshStatus' | 'quotaResetting'>> = new Map(),
): AuthFileCredentialRow[] {
  return identities.map((identity) => {
    const quotaResponse = quotas.get(identity.identity)
    const quota = quotaResponse?.quota ?? []
    const state = quotaStates.get(identity.identity)
    const displayQuotas = quota.map(toDisplayQuota).filter(isDisplayableQuota)
    const planType = firstNonEmpty(...quota.map((row) => row.planType), identity.plan_type)

    return {
      identity,
      displayName: credentialDisplayName(identity),
      maskedIdentity: identity.identity,
      providerLabel: credentialProviderLabel(identity),
      typeLabel: credentialTypeLabel(identity),
      authTypeLabel: credentialAuthTypeLabel(identity),
      priorityLabel: credentialPriorityLabel(identity.priority),
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
      quotaResetCreditsAvailableCount: quotaResponse?.rateLimitResetCreditsAvailableCount,
      quotaLoading: state?.quotaLoading ?? false,
      quotaError: state?.quotaError,
      refreshStatus: state?.refreshStatus,
      quotaResetting: state?.quotaResetting ?? false,
      displayQuotas,
      credentialHealth: identity.credential_health,
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
    priorityLabel: credentialPriorityLabel(identity.priority),
    totalRequests: safeNumber(identity.total_requests),
    successCount: safeNumber(identity.success_count),
    failureCount: safeNumber(identity.failure_count),
    successRate: successRate(identity),
    totalTokens: safeNumber(identity.total_tokens),
    cacheRate: cacheRate(identity),
    lastUsedText: identity.last_used_at,
    statsUpdatedText: identity.stats_updated_at,
    credentialHealth: identity.credential_health,
  }))
}

function toDisplayQuota(row: UsageQuotaRow): DisplayQuota | undefined {
  // 后端 quota row 可能是 used、remaining 或 remainingFraction，这里统一成展示进度。
  const used = finiteNumber(row.used)
  const limit = finiteNumber(row.limit)
  const remaining = finiteNumber(row.remaining)
  const percentDisplay = quotaPercent(row, used, limit)

  const windowSeconds = finiteNumber(row.window?.seconds)
  const label = quotaLabel(row, windowSeconds)
  if (!label) {
    return undefined
  }

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
    windowUsage: quotaWindowUsage(row),
    windowUsageEstimate: quotaWindowUsageEstimate(row, percentDisplay),
    billingUsage: quotaBillingUsage(row),
    status: quotaStatus(row, percentDisplay.percent, percentDisplay.kind),
  }
}

function quotaBillingUsage(row: UsageQuotaRow): QuotaBillingUsageDisplay | undefined {
  if (row.metric !== 'usd_cents') {
    return undefined
  }
  const used = finiteNumber(row.used)
  const limit = finiteNumber(row.limit)
  const remaining = finiteNumber(row.remaining)
  if (used === undefined && limit === undefined && remaining === undefined) {
    return undefined
  }
  return {
    used: used === undefined ? undefined : formatUSDCents(used),
    limit: limit === undefined ? undefined : formatUSDCents(limit),
    remaining: remaining === undefined ? undefined : formatUSDCents(remaining),
  }
}

function formatUSDCents(cents: number): string {
  return formatQuotaWindowCost(cents / 100)
}

function quotaWindowUsage(row: UsageQuotaRow): QuotaWindowUsageDisplay | undefined {
  const tokens = finiteNumber(row.window_usage_tokens)
  const cost = finiteNumber(row.window_usage_cost)
  if (tokens === undefined || cost === undefined) {
    return undefined
  }
  return {
    tokens: formatCompactTokenValue(tokens),
    cost: formatQuotaWindowCost(cost),
  }
}

function quotaWindowUsageEstimate(row: UsageQuotaRow, percentDisplay: { percent: number | null; kind: DisplayQuota['percentKind'] }): QuotaWindowUsageDisplay | undefined {
  // 估算只在已用百分比可外推时生效；0%、满额或免费窗口都继续展示当前值。
  const tokens = finiteNumber(row.window_usage_tokens)
  const cost = finiteNumber(row.window_usage_cost)
  const usedPercent = quotaUsedPercent(percentDisplay)
  if (tokens === undefined || cost === undefined || usedPercent === undefined) {
    return undefined
  }
  if (tokens <= 0 || cost <= 0 || usedPercent <= 0 || usedPercent >= 100) {
    return undefined
  }
  const ratio = usedPercent / 100
  return {
    tokens: formatCompactTokenValue(tokens / ratio),
    cost: formatQuotaWindowCost(cost / ratio),
  }
}

function formatQuotaWindowCost(cost: number): string {
  // 限额条下方空间很紧，窗口成本统一展示两位小数，避免 0 显示成 0.0000。
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(cost || 0).replace(/^US\$/, '$')
}

function quotaLabel(row: UsageQuotaRow, windowSeconds?: number): string | undefined {
  // 对已知窗口按秒数纠正标签；未知窗口不展示 Window 占位，避免误导用户。
  const label = row.label || row.metric || row.scope || row.key
  if (windowSeconds === FIVE_HOUR_WINDOW_SECONDS) {
    return knownWindowLabel(label, '5h')
  }
  if (windowSeconds === WEEKLY_WINDOW_SECONDS) {
    return knownWindowLabel(label, 'Weekly')
  }
  if (windowSeconds === THIRTY_DAY_WINDOW_SECONDS || windowSeconds === AVERAGE_MONTH_WINDOW_SECONDS) {
    return knownWindowLabel(label, 'Monthly')
  }
  if (windowSeconds !== undefined) {
    return unknownWindowLabel(row, label)
  }
  if (genericWindowLabel(label)) {
    return unknownWindowLabel(row, label)
  }
  return label
}

function knownWindowLabel(label: string, replacement: string): string {
  if (label === '5h' || label === 'Weekly' || label === 'Monthly' || label === 'Window') {
    return replacement
  }
  for (const candidate of ['5h', 'Weekly', 'Monthly', 'Window']) {
    if (label.includes(candidate)) {
      return label.replace(candidate, replacement)
    }
  }
  return label
}

function genericWindowLabel(label: string): boolean {
  return /\bWindow\b/.test(label)
}

function unknownWindowLabel(row: UsageQuotaRow, label: string): string | undefined {
  const role = quotaWindowRole(row.key)
  if (!role) {
    return genericWindowLabel(label) ? undefined : label
  }
  for (const candidate of ['5h', 'Weekly', 'Monthly', 'Window']) {
    if (label === candidate) {
      return role
    }
    if (label.includes(candidate)) {
      return label.replace(candidate, role)
    }
  }
  return quotaRoleLabel(row)
}

function quotaRoleLabel(row: UsageQuotaRow): string | undefined {
  const role = quotaWindowRole(row.key)
  if (!role) {
    return undefined
  }
  const prefix = quotaWindowRolePrefix(row)
  return prefix ? `${prefix} ${role}` : role
}

function quotaWindowRole(key: string): string | undefined {
  if (key.endsWith('.primary_window')) {
    return 'Primary'
  }
  if (key.endsWith('.secondary_window')) {
    return 'Secondary'
  }
  return undefined
}

function quotaWindowRolePrefix(row: UsageQuotaRow): string {
  if (row.key.startsWith('code_review_rate_limit.')) {
    return 'Code Review'
  }
  if (!row.key.startsWith('additional_rate_limits.')) {
    return ''
  }
  const keyName = row.key
    .replace(/^additional_rate_limits\./, '')
    .replace(/\.(primary|secondary)_window$/, '')
  return firstNonEmpty(row.metric, keyName) ?? ''
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

function quotaUsedPercent(percentDisplay: { percent: number | null; kind: DisplayQuota['percentKind'] }): number | undefined {
  if (percentDisplay.percent === null) {
    return undefined
  }
  if (percentDisplay.kind === 'used') {
    return clampPercent(percentDisplay.percent)
  }
  if (percentDisplay.kind === 'remaining') {
    return clampPercent(100 - percentDisplay.percent)
  }
  return undefined
}

function isDisplayableQuota(quota: DisplayQuota | undefined): quota is DisplayQuota {
  return quota !== undefined && quota.barPercent !== null
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

function credentialPriorityLabel(priority: number | undefined): string | undefined {
  if (typeof priority !== 'number' || !Number.isFinite(priority)) {
    return undefined
  }
  return `P${priority}`
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
  return calculateCacheRate({
    inputTokens: identity.input_tokens,
    cachedTokens: identity.cached_tokens,
  })
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
