import type { DisplayQuota } from '../credentialViewModels'
import type { QuotaUsageMode } from './CredentialQuotaPanel'
type Translate = (key: string, options?: Record<string, string>) => string
type QuotaErrorDisplay = { code?: string; message: string; title: string }
type QuotaErrorDetails = { code?: string; message?: string }
const QUOTA_ERROR_MESSAGE_MAX_LENGTH = 96
const QUOTA_ERROR_PARSE_MAX_DEPTH = 10
export const ANTIGRAVITY_ARGON_GROUP_KEY = 'antigravity-gemini-4-argon'
const ANTIGRAVITY_QUOTA_GROUP_KEYS = new Set(['antigravity-gemini-models', 'antigravity-claude-and-gpt-models', ANTIGRAVITY_ARGON_GROUP_KEY])

type CredentialQuotaPanelItem =
  | { kind: 'quota'; quota: DisplayQuota; tooltipAlignRight: boolean }
  | AntigravityQuotaGroupItem

export type AntigravityQuotaGroupItem = {
  kind: 'group'
  renderKey: string
  groupKey: string
  groupLabel: string
  groupDescription?: string
  quotas: DisplayQuota[]
}

export function credentialQuotaPanelItems(quotas: DisplayQuota[]): CredentialQuotaPanelItem[] {
  const items: CredentialQuotaPanelItem[] = []
  let flatColumn = 0
  for (const quota of quotas) {
    const groupKey = quota.groupKey?.trim() ?? ''
    const groupLabel = quota.groupLabel?.trim() ?? ''
    if (quota.scope !== 'quota_group' || !ANTIGRAVITY_QUOTA_GROUP_KEYS.has(groupKey) || !groupLabel) {
      items.push({ kind: 'quota', quota, tooltipAlignRight: flatColumn === 1 })
      flatColumn = (flatColumn + 1) % 2
      continue
    }
    // 分组块横跨两列；只合并相邻同组行，并让后续扁平行重新从左列开始。
    flatColumn = 0
    const previous = items.at(-1)
    if (previous?.kind === 'group' && previous.groupKey === groupKey) {
      previous.quotas.push(quota)
      if (!previous.groupDescription && quota.groupDescription?.trim()) {
        previous.groupDescription = quota.groupDescription
      }
      continue
    }
    const group: AntigravityQuotaGroupItem = {
      kind: 'group',
      renderKey: `${groupKey}:${quota.key}`,
      groupKey,
      groupLabel,
      groupDescription: quota.groupDescription,
      quotas: [quota],
    }
    items.push(group)
  }
  return items
}

export function formatQuotaErrorDisplay(error: string | undefined): QuotaErrorDisplay {
  const title = (error || '').trim()
  const raw = title || 'Quota refresh failed. Please try again later.'
  const { code, message } = splitHTTPStatus(raw)
  const structured = quotaErrorDetailsFromStructuredValue(message || raw)
  const displayCode = code || structured.code
  const sourceMessage = structured.message || (isStructuredQuotaErrorValue(message || raw) ? '' : (message || raw))
  const readableMessage = readableQuotaErrorMessage(sourceMessage, displayCode ? `HTTP ${displayCode}` : 'Quota refresh failed. Please try again later.')
  return {
    code: displayCode,
    message: readableMessage,
    title: raw,
  }
}

function splitHTTPStatus(value: string): { code?: string; message: string } {
  const trimmed = value.trim()
  const match = trimmed.match(/^HTTP\s+(\d{3})(?=\D|$)(?::|\s+-)?\s*([\s\S]*)$/i) ?? trimmed.match(/^(\d{3})(?=\D|$)(?::|\s+-)?\s*([\s\S]*)$/)
  if (!match) {
    return { message: trimmed }
  }
  return { code: match[1], message: match[2].trim() }
}

function readableQuotaErrorMessage(value: string, fallback: string): string {
  const normalized = (value || fallback).replace(/\s+/g, ' ').trim() || fallback
  return truncateQuotaErrorMessage(normalized)
}

function quotaErrorDetailsFromStructuredValue(value: string, depth = 0): QuotaErrorDetails {
  const trimmed = value.trim()
  if (!trimmed || depth > QUOTA_ERROR_PARSE_MAX_DEPTH || !isStructuredQuotaErrorValue(trimmed)) {
    return {}
  }
  try {
    return quotaErrorDetailsFromParsedValue(JSON.parse(trimmed), depth + 1)
  } catch {
    return {}
  }
}

function quotaErrorDetailsFromParsedValue(value: unknown, depth: number): QuotaErrorDetails {
  if (depth > QUOTA_ERROR_PARSE_MAX_DEPTH) {
    return {}
  }
  if (typeof value === 'string') {
    const trimmed = value.trim()
    if (!trimmed) {
      return {}
    }
    if (isStructuredQuotaErrorValue(trimmed)) {
      const structured = quotaErrorDetailsFromStructuredValue(trimmed, depth + 1)
      if (structured.code || structured.message) {
        return structured
      }
    }
    const httpStatus = splitHTTPStatus(trimmed)
    if (httpStatus.code) {
      return mergeQuotaErrorDetails({ code: httpStatus.code }, quotaErrorDetailsFromStructuredValue(httpStatus.message, depth + 1), { message: httpStatus.message })
    }
    return { message: trimmed }
  }
  if (Array.isArray(value)) {
    return value.reduce<QuotaErrorDetails>((current, item) => mergeQuotaErrorDetails(current, quotaErrorDetailsFromParsedValue(item, depth + 1)), {})
  }
  if (!value || typeof value !== 'object') {
    return {}
  }
  const record = value as Record<string, unknown>
  let details: QuotaErrorDetails = { code: quotaHTTPStatusCodeFromRecord(record) }
  const nestedKeys = ['body', 'body_text', 'bodyText', 'response', 'data', 'payload', 'error', 'errors']
  // provider 错误常带一层通用 message，真实上游错误在 body/error 等字段里，先解析内层响应体。
  for (const key of nestedKeys) {
    if (!isPreferredNestedQuotaErrorValue(key, record[key])) {
      continue
    }
    details = mergeQuotaErrorDetails(details, quotaErrorDetailsFromParsedValue(record[key], depth + 1))
    if (details.message) {
      break
    }
  }
  if (!details.message) {
    for (const key of ['message', 'error_description', 'detail', 'description', 'title', 'reason']) {
      const value = record[key]
      if (typeof value !== 'string') {
        continue
      }
      const nested = quotaErrorDetailsFromParsedValue(value, depth + 1)
      details = mergeQuotaErrorDetails(details, nested.message === value.trim() ? { message: value.trim() } : nested)
      if (details.message) {
        break
      }
    }
  }
  for (const key of nestedKeys) {
    if (record[key] === undefined) {
      continue
    }
    details = mergeQuotaErrorDetails(details, quotaErrorDetailsFromParsedValue(record[key], depth + 1))
    if (details.code && details.message) {
      break
    }
  }
  return details
}

function isPreferredNestedQuotaErrorValue(key: string, value: unknown): boolean {
  if (value === undefined || value === null) {
    return false
  }
  if (typeof value !== 'string') {
    return typeof value === 'object'
  }
  const trimmed = value.trim()
  if (!trimmed) {
    return false
  }
  if (['body', 'body_text', 'bodyText', 'response', 'data', 'payload'].includes(key)) {
    return true
  }
  return isStructuredQuotaErrorValue(trimmed) || Boolean(splitHTTPStatus(trimmed).code)
}

function isStructuredQuotaErrorValue(value: string): boolean {
  const trimmed = value.trim()
  return ['{', '[', '"'].includes(trimmed[0] ?? '')
}

function quotaHTTPStatusCodeFromRecord(record: Record<string, unknown>): string | undefined {
  for (const key of ['http_status_code', 'status_code', 'statusCode', 'status', 'code']) {
    const code = quotaHTTPStatusCode(record[key])
    if (code) {
      return code
    }
  }
  return undefined
}

function quotaHTTPStatusCode(value: unknown): string | undefined {
  if (typeof value === 'number' && Number.isInteger(value) && value >= 100 && value <= 599) {
    return String(value)
  }
  if (typeof value !== 'string') {
    return undefined
  }
  const match = value.trim().match(/^(?:HTTP\s+)?(\d{3})(?:\D|$)/i)
  if (!match) {
    return undefined
  }
  const status = Number(match[1])
  if (status < 100 || status > 599) {
    return undefined
  }
  return match[1]
}

function mergeQuotaErrorDetails(...items: QuotaErrorDetails[]): QuotaErrorDetails {
  return items.reduce<QuotaErrorDetails>((current, item) => ({
    code: current.code || item.code,
    message: current.message || item.message,
  }), {})
}

function truncateQuotaErrorMessage(value: string): string {
  if (value.length <= QUOTA_ERROR_MESSAGE_MAX_LENGTH) {
    return value
  }
  return `${value.slice(0, QUOTA_ERROR_MESSAGE_MAX_LENGTH).trimEnd()}...`
}

export function formatQuotaResetLabel(resetAt: string, timeZone?: string): string {
  const resetTime = new Date(resetAt)
  const resetMs = resetTime.getTime()
  if (!Number.isFinite(resetMs)) {
    return ''
  }
  const normalizedTimeZone = timeZone?.trim()
  if (!normalizedTimeZone || normalizedTimeZone === 'Local') {
    return ''
  }
  try {
    // resetAt 保留 CPA 给出的绝对时刻，只用 Keeper 项目时区生成墙上时间，避免受浏览器时区影响。
    const parts = new Intl.DateTimeFormat('en-CA', {
      timeZone: normalizedTimeZone,
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hourCycle: 'h23',
    }).formatToParts(resetTime)
    const values = Object.fromEntries(parts.map((part) => [part.type, part.value]))
    return `${values.month}-${values.day} ${values.hour}:${values.minute}`
  } catch {
    // Go 的 Local 或浏览器不认识的 IANA 名称无法可靠映射到 Keeper 墙上时间，隐藏标签而不是猜浏览器时区。
    return ''
  }
}

export function formatQuotaResetDuration(resetAt: string): string {
  const resetMs = new Date(resetAt).getTime()
  if (!Number.isFinite(resetMs)) {
    return ''
  }
  const remainingMinutes = Math.max(0, Math.ceil((resetMs - Date.now()) / 60_000))
  const days = Math.floor(remainingMinutes / 1_440)
  const hours = Math.floor((remainingMinutes % 1_440) / 60)
  const minutes = remainingMinutes % 60
  return days > 0 ? `${days}d${hours}h${minutes}m` : `${hours}h${minutes}m`
}

export function formatQuotaWindowUsageAriaLabel(t: Translate, windowUsage: NonNullable<DisplayQuota['windowUsage']>): string {
  return t('usage_stats.credentials_quota_window_usage_aria', {
    tokens: windowUsage.tokens,
    cost: windowUsage.cost,
  })
}

export function formatQuotaBillingUsageAriaLabel(t: Translate, billingUsage: NonNullable<DisplayQuota['billingUsage']>): string {
  return t('usage_stats.credentials_quota_billing_usage_aria', {
    used: billingUsage.used ?? '-',
    limit: billingUsage.limit ?? '-',
    remaining: billingUsage.remaining ?? '-',
  })
}

export function formatQuotaBillingUsageText(billingUsage: NonNullable<DisplayQuota['billingUsage']>): string {
  if (billingUsage.used && billingUsage.limit) {
    return `${billingUsage.used} / ${billingUsage.limit}`
  }
  return billingUsage.used ?? billingUsage.remaining ?? billingUsage.limit ?? ''
}

export function quotaWindowUsageForMode(quota: DisplayQuota, mode: QuotaUsageMode): DisplayQuota['windowUsage'] {
  if (mode === 'estimated' && quota.windowUsageEstimate) {
    return quota.windowUsageEstimate
  }
  return quota.windowUsage
}
