import type { UsageIdentityTypeCount } from '@/lib/types'
import type { ProviderBrandIconKey } from '@/components/ProviderBrandIcon'

export type CredentialProviderFilterScope = 'auth-files' | 'ai-provider'
export type KnownCredentialProviderFilterKey = ProviderBrandIconKey
export type CredentialProviderFilterKey = 'all' | KnownCredentialProviderFilterKey

export interface CredentialProviderFilterOption {
  key: CredentialProviderFilterKey
  count: number
  labelKey: string
  knownKey?: KnownCredentialProviderFilterKey
}

interface KnownCredentialProviderFilter {
  key: KnownCredentialProviderFilterKey
  labelKey: string
  types: string[]
}

const AUTH_FILE_PROVIDER_FILTERS: KnownCredentialProviderFilter[] = [
  { key: 'antigravity', labelKey: 'usage_stats.credentials_filter_antigravity', types: ['antigravity'] },
  { key: 'claude', labelKey: 'usage_stats.credentials_filter_claude', types: ['claude'] },
  { key: 'codex', labelKey: 'usage_stats.credentials_filter_codex', types: ['codex'] },
  { key: 'devin', labelKey: 'usage_stats.credentials_filter_devin', types: ['devin'] },
  // Gemini Auth File 兼容 CPA 的原始与 CLI type，并统一复用 Gemini 品牌筛选。
  { key: 'gemini', labelKey: 'usage_stats.credentials_filter_gemini', types: ['gemini', 'gemini-cli'] },
  { key: 'kimi', labelKey: 'usage_stats.credentials_filter_kimi', types: ['kimi', 'kimi-ai', 'kimi.ai', 'kimi.com'] },
  { key: 'xai', labelKey: 'usage_stats.credentials_filter_xai', types: ['xai'] },
  { key: 'vertex', labelKey: 'usage_stats.credentials_filter_vertex', types: ['vertex'] },
]

const AI_PROVIDER_FILTERS: KnownCredentialProviderFilter[] = [
  { key: 'codex', labelKey: 'usage_stats.credentials_filter_codex', types: ['codex'] },
  { key: 'xai', labelKey: 'usage_stats.credentials_filter_xai', types: ['xai'] },
  // AI Provider 的 Gemini 品牌同时兼容普通、CLI 与 Interactions 三种原始 type。
  { key: 'gemini', labelKey: 'usage_stats.credentials_filter_gemini', types: ['gemini', 'gemini-cli', 'gemini-interactions'] },
  { key: 'claude', labelKey: 'usage_stats.credentials_filter_claude', types: ['claude'] },
  { key: 'vertex', labelKey: 'usage_stats.credentials_filter_vertex', types: ['vertex'] },
  { key: 'meta', labelKey: 'usage_stats.credentials_filter_meta', types: ['meta'] },
  { key: 'openai', labelKey: 'usage_stats.credentials_filter_openai', types: ['openai'] },
]

const FILTERS_BY_SCOPE: Record<CredentialProviderFilterScope, KnownCredentialProviderFilter[]> = {
  'auth-files': AUTH_FILE_PROVIDER_FILTERS,
  'ai-provider': AI_PROVIDER_FILTERS,
}

function credentialProviderFiltersForScope(scope: CredentialProviderFilterScope): KnownCredentialProviderFilter[] {
  return FILTERS_BY_SCOPE[scope]
}

export function credentialProviderFilterTypes(scope: CredentialProviderFilterScope, filter: CredentialProviderFilterKey): string[] {
  if (filter === 'all') {
    return []
  }
  return credentialProviderFiltersForScope(scope).find((item) => item.key === filter)?.types ?? []
}

// 恢复持久化筛选时使用：跨分区（例如 Auth 文件独有的 antigravity）或未知的 key 一律回落到 all。
export function normalizeCredentialProviderFilterKey(scope: CredentialProviderFilterScope, value: unknown): CredentialProviderFilterKey {
  if (typeof value !== 'string' || value === 'all') {
    return 'all'
  }
  return credentialProviderFiltersForScope(scope).some((item) => item.key === value)
    ? value as CredentialProviderFilterKey
    : 'all'
}

// null 表示没有有效的链接筛选；显式 all 必须区别于回退到本地偏好。
export function resolveCredentialProviderFilterFromSearch(scope: CredentialProviderFilterScope, search: string): CredentialProviderFilterKey | null {
  const value = new URLSearchParams(search).get('provider')
  return value !== null && normalizeCredentialProviderFilterKey(scope, value) === value
    ? value as CredentialProviderFilterKey
    : null
}

export function buildCredentialProviderSearch(search: string, filter: CredentialProviderFilterKey): string {
  const params = new URLSearchParams(search)
  params.set('provider', filter)
  return `?${params.toString()}`
}

export function buildCredentialProviderFilterOptions(scope: CredentialProviderFilterScope, typeCounts: UsageIdentityTypeCount[], selected: CredentialProviderFilterKey = 'all'): CredentialProviderFilterOption[] {
  const countsByType = new Map<string, number>()
  let allCount = 0

  for (const item of typeCounts) {
    const count = finiteCount(item.count)
    if (count <= 0) {
      continue
    }
    allCount += count
    countsByType.set(item.type, (countsByType.get(item.type) ?? 0) + count)
  }

  if (allCount <= 0 && selected === 'all') {
    return []
  }

  const options: CredentialProviderFilterOption[] = [{ key: 'all', labelKey: 'usage_stats.credentials_filter_all', count: allCount }]

  // 每个品牌按钮可以聚合多个原始 type；未知 type 仍只计入 All，不单独生成按钮。
  for (const filter of credentialProviderFiltersForScope(scope)) {
    const count = filter.types.reduce((sum, type) => sum + (countsByType.get(type) ?? 0), 0)
    // 链接指定的供应商即使暂时没有凭据，也保留选中项和返回 All 的入口。
    if (count <= 0 && filter.key !== selected) {
      continue
    }
    options.push({ key: filter.key, labelKey: filter.labelKey, count, knownKey: filter.key })
  }

  return options
}

function finiteCount(value: number): number {
  return Number.isFinite(value) && value > 0 ? value : 0
}
