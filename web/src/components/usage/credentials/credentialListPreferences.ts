import { USAGE_IDENTITY_PAGE_SORTS, type UsageIdentityPageSort } from '@/lib/api'
import { CREDENTIALS_PAGE_SIZE, CREDENTIAL_PAGE_SIZE_OPTIONS } from './credentialViewModels'
import { normalizeCredentialProviderFilterKey, type CredentialProviderFilterKey, type CredentialProviderFilterScope } from './credentialProviderFilters'

export const CREDENTIAL_LIST_PREFERENCES_VERSION = 1

export const CREDENTIAL_LIST_PREFERENCES_STORAGE_KEYS: Record<CredentialProviderFilterScope, string> = {
  'auth-files': 'cpa-usage-keeper-auth-files-list-preferences-v1',
  'ai-provider': 'cpa-usage-keeper-ai-providers-list-preferences-v1',
}

// Auth 文件默认按优先级排，AI 供应商默认按请求数排；与持久化前的初始值保持一致。
const DEFAULT_SORT_BY_SCOPE: Record<CredentialProviderFilterScope, UsageIdentityPageSort> = {
  'auth-files': 'priority',
  'ai-provider': 'total_requests',
}

export interface CredentialListPreferences {
  sort: UsageIdentityPageSort
  pageSize: number
  providerFilter: CredentialProviderFilterKey
}

interface CredentialListPreferencesStorage {
  getItem: (key: string) => string | null
  setItem: (key: string, value: string) => void
}

const defaultStorage = (): CredentialListPreferencesStorage | undefined => (
  typeof localStorage === 'undefined' ? undefined : localStorage
)

export const defaultCredentialListPreferences = (scope: CredentialProviderFilterScope): CredentialListPreferences => ({
  sort: DEFAULT_SORT_BY_SCOPE[scope],
  pageSize: CREDENTIALS_PAGE_SIZE,
  providerFilter: 'all',
})

const normalizeSort = (scope: CredentialProviderFilterScope, value: unknown): UsageIdentityPageSort => (
  USAGE_IDENTITY_PAGE_SORTS.includes(value as UsageIdentityPageSort)
    ? value as UsageIdentityPageSort
    : DEFAULT_SORT_BY_SCOPE[scope]
)

const normalizePageSize = (value: unknown): number => (
  CREDENTIAL_PAGE_SIZE_OPTIONS.includes(value as typeof CREDENTIAL_PAGE_SIZE_OPTIONS[number])
    ? value as number
    : CREDENTIALS_PAGE_SIZE
)

export const normalizeCredentialListPreferences = (
  scope: CredentialProviderFilterScope,
  value: unknown,
): CredentialListPreferences => {
  // 排序、每页条数、供应商筛选都会直接进请求参数，读回来必须逐项按白名单校验。
  const stored = typeof value === 'object' && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {}
  if (stored.version !== CREDENTIAL_LIST_PREFERENCES_VERSION) {
    return defaultCredentialListPreferences(scope)
  }
  return {
    sort: normalizeSort(scope, stored.sort),
    pageSize: normalizePageSize(stored.pageSize),
    providerFilter: normalizeCredentialProviderFilterKey(scope, stored.providerFilter),
  }
}

export const loadCredentialListPreferences = (
  scope: CredentialProviderFilterScope,
  storage: CredentialListPreferencesStorage | undefined = defaultStorage(),
): CredentialListPreferences => {
  try {
    const raw = storage?.getItem(CREDENTIAL_LIST_PREFERENCES_STORAGE_KEYS[scope])
    if (!raw) {
      return defaultCredentialListPreferences(scope)
    }
    return normalizeCredentialListPreferences(scope, JSON.parse(raw))
  } catch {
    return defaultCredentialListPreferences(scope)
  }
}

export const persistCredentialListPreferences = (
  scope: CredentialProviderFilterScope,
  patch: Partial<CredentialListPreferences>,
  storage: CredentialListPreferencesStorage | undefined = defaultStorage(),
): boolean => {
  try {
    if (!storage) {
      return false
    }
    // 每个 setter 只知道自己那一项，先读回当前值再合并，避免互相覆盖。
    const next = { ...loadCredentialListPreferences(scope, storage), ...patch }
    storage.setItem(CREDENTIAL_LIST_PREFERENCES_STORAGE_KEYS[scope], JSON.stringify({
      version: CREDENTIAL_LIST_PREFERENCES_VERSION,
      ...normalizeCredentialListPreferences(scope, { version: CREDENTIAL_LIST_PREFERENCES_VERSION, ...next }),
    }))
    return true
  } catch {
    return false
  }
}
