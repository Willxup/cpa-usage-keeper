import type { UsageIdentityTypeCount } from '@/lib/types'

export type CredentialProviderFilterScope = 'auth-files' | 'ai-provider'
export type KnownCredentialProviderFilterKey = 'antigravity' | 'claude' | 'codex' | 'gemini' | 'gemini-cli' | 'iflow' | 'openai' | 'xai'
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
  { key: 'gemini-cli', labelKey: 'usage_stats.credentials_filter_gemini_cli', types: ['gemini-cli'] },
  { key: 'iflow', labelKey: 'usage_stats.credentials_filter_iflow', types: ['iflow'] },
  { key: 'xai', labelKey: 'usage_stats.credentials_filter_xai', types: ['xai'] },
]

const AI_PROVIDER_FILTERS: KnownCredentialProviderFilter[] = [
  { key: 'claude', labelKey: 'usage_stats.credentials_filter_claude', types: ['claude'] },
  { key: 'codex', labelKey: 'usage_stats.credentials_filter_codex', types: ['codex'] },
  // Gemini 品牌按钮同时查询普通 Gemini 与独立的 Gemini Interactions 原始类型。
  { key: 'gemini', labelKey: 'usage_stats.credentials_filter_gemini', types: ['gemini', 'gemini-interactions'] },
  // xAI API Key 使用已有 xAI 标签和图标，但保持独立 xai 原始类型查询。
  { key: 'xai', labelKey: 'usage_stats.credentials_filter_xai', types: ['xai'] },
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

export function buildCredentialProviderFilterOptions(scope: CredentialProviderFilterScope, typeCounts: UsageIdentityTypeCount[]): CredentialProviderFilterOption[] {
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

  if (allCount <= 0) {
    return []
  }

  const options: CredentialProviderFilterOption[] = [{ key: 'all', labelKey: 'usage_stats.credentials_filter_all', count: allCount }]

  // 每个品牌按钮可以聚合多个原始 type；未知 type 仍只计入 All，不单独生成按钮。
  for (const filter of credentialProviderFiltersForScope(scope)) {
    const count = filter.types.reduce((sum, type) => sum + (countsByType.get(type) ?? 0), 0)
    if (count <= 0) {
      continue
    }
    options.push({ key: filter.key, labelKey: filter.labelKey, count, knownKey: filter.key })
  }

  return options
}

function finiteCount(value: number): number {
  return Number.isFinite(value) && value > 0 ? value : 0
}
