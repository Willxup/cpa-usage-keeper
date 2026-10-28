import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, fetchUsageIdentitiesPage, resetUsageIdentityStats, type UsageIdentityPageSort } from '@/lib/api'
import type { UsageIdentity, UsageIdentityTypeCount } from '@/lib/types'
import { stripAppBasePath } from '@/lib/usageNavigation'
import { credentialProviderFilterTypes, resolveCredentialProviderFilterFromSearch, type CredentialProviderFilterKey } from './credentialProviderFilters'
import { loadCredentialListPreferences, persistCredentialListPreferences } from './credentialListPreferences'

interface UseCredentialPagesOptions {
  enabledCredentials: boolean
  enabledAiProviders: boolean
  onAuthRequired?: () => void
}

export const CREDENTIAL_PAGES_REFRESH_INTERVAL_MS = 60 * 1000

const CREDENTIAL_ACTIVE_ONLY_STORAGE_KEY = 'cpa-usage-keeper-credentials-active-only'
const AI_PROVIDER_ACTIVE_ONLY_STORAGE_KEY = 'cpa-usage-keeper-ai-providers-active-only'

export function mergeUsageIdentityAliasUpdate(current: UsageIdentity, updated: UsageIdentity): UsageIdentity {
  return {
    ...current,
    alias: updated.alias ?? null,
    displayName: updated.displayName ?? current.displayName,
  }
}

const getInitialCredentialActiveOnly = () => {
  if (typeof window === 'undefined') return false
  return window.localStorage.getItem(CREDENTIAL_ACTIVE_ONLY_STORAGE_KEY) === 'true'
}

const getInitialAiProviderActiveOnly = () => {
  if (typeof window === 'undefined') return false
  return window.localStorage.getItem(AI_PROVIDER_ACTIVE_ONLY_STORAGE_KEY) === 'true'
}

const getInitialListPreferences = () => {
  const credential = loadCredentialListPreferences('credentials')
  const aiProvider = loadCredentialListPreferences('ai-provider')
  if (typeof window !== 'undefined') {
    const path = stripAppBasePath(window.location.pathname, window.__APP_BASE_PATH__)
    // 只覆盖当前路由的初始筛选，不调用会持久化的 setter，也不污染另一分区。
    if (path === '/credentials') {
      credential.providerFilter = resolveCredentialProviderFilterFromSearch('credentials', window.location.search) ?? credential.providerFilter
    } else if (path === '/ai-provider') {
      aiProvider.providerFilter = resolveCredentialProviderFilterFromSearch('ai-provider', window.location.search) ?? aiProvider.providerFilter
    }
  }
  return { credential, aiProvider }
}

export interface CredentialPagesState {
  credentialIdentities: UsageIdentity[]
  aiProviderIdentities: UsageIdentity[]
  credentialTypeCounts: UsageIdentityTypeCount[]
  aiProviderTypeCounts: UsageIdentityTypeCount[]
  credentialTotal: number
  aiProviderTotal: number
  credentialTotalPages: number
  aiProviderTotalPages: number
  credentialPage: number
  aiProviderPage: number
  credentialPageSize: number
  aiProviderPageSize: number
  credentialActiveOnly: boolean
  aiProviderActiveOnly: boolean
  credentialProviderFilter: CredentialProviderFilterKey
  aiProviderProviderFilter: CredentialProviderFilterKey
  credentialSort: UsageIdentityPageSort
  aiProviderSort: UsageIdentityPageSort
  setCredentialPage: (page: number) => void
  setAiProviderPage: (page: number) => void
  setCredentialPageSize: (pageSize: number) => void
  setAiProviderPageSize: (pageSize: number) => void
  setCredentialActiveOnly: (activeOnly: boolean) => void
  setAiProviderActiveOnly: (activeOnly: boolean) => void
  setCredentialProviderFilter: (filter: CredentialProviderFilterKey) => void
  setAiProviderProviderFilter: (filter: CredentialProviderFilterKey) => void
  setCredentialSort: (sort: UsageIdentityPageSort) => void
  setAiProviderSort: (sort: UsageIdentityPageSort) => void
  replaceUsageIdentity: (identity: UsageIdentity) => void
  resetStats: (id: string) => Promise<UsageIdentity>
  loading: boolean
  error: string
  refresh: () => Promise<void>
}

export function useCredentialPages({ enabledCredentials, enabledAiProviders, onAuthRequired }: UseCredentialPagesOptions): CredentialPagesState {
  const [credentialIdentities, setCredentialIdentities] = useState<UsageIdentity[]>([])
  const [aiProviderIdentities, setAiProviderIdentities] = useState<UsageIdentity[]>([])
  const [credentialTypeCounts, setCredentialTypeCounts] = useState<UsageIdentityTypeCount[]>([])
  const [aiProviderTypeCounts, setAiProviderTypeCounts] = useState<UsageIdentityTypeCount[]>([])
  const [credentialTotal, setCredentialTotal] = useState(0)
  const [aiProviderTotal, setAiProviderTotal] = useState(0)
  const [credentialTotalPages, setCredentialTotalPages] = useState(0)
  const [aiProviderTotalPages, setAiProviderTotalPages] = useState(0)
  const [credentialsError, setCredentialsError] = useState('')
  const [aiProvidersError, setAiProvidersError] = useState('')
  const [credentialPage, setCredentialPage] = useState(1)
  const [aiProviderPage, setAiProviderPage] = useState(1)
  // 排序/每页条数/供应商筛选在挂载时一次性读回，避免每个字段各查一次 localStorage。
  const [initialListPreferences] = useState(getInitialListPreferences)
  const [credentialPageSize, setCredentialPageSizeState] = useState(initialListPreferences.credential.pageSize)
  const [aiProviderPageSize, setAiProviderPageSizeState] = useState(initialListPreferences.aiProvider.pageSize)
  const [credentialActiveOnly, setCredentialActiveOnlyState] = useState(getInitialCredentialActiveOnly)
  const [aiProviderActiveOnly, setAiProviderActiveOnlyState] = useState(getInitialAiProviderActiveOnly)
  const [credentialProviderFilter, setCredentialProviderFilterState] = useState<CredentialProviderFilterKey>(initialListPreferences.credential.providerFilter)
  const [aiProviderProviderFilter, setAiProviderProviderFilterState] = useState<CredentialProviderFilterKey>(initialListPreferences.aiProvider.providerFilter)
  const [credentialSort, setCredentialSortState] = useState<UsageIdentityPageSort>(initialListPreferences.credential.sort)
  const [aiProviderSort, setAiProviderSortState] = useState<UsageIdentityPageSort>(initialListPreferences.aiProvider.sort)
  const [credentialsLoading, setCredentialsLoading] = useState(false)
  const [aiProvidersLoading, setAiProvidersLoading] = useState(false)
  const credentialsRequestControllerRef = useRef<AbortController | null>(null)
  const aiProvidersRequestControllerRef = useRef<AbortController | null>(null)

  const setCredentialPageSize = useCallback((pageSize: number) => {
    setCredentialPage(1)
    setCredentialPageSizeState(pageSize)
    persistCredentialListPreferences('credentials', { pageSize })
  }, [])
  const setAiProviderPageSize = useCallback((pageSize: number) => {
    setAiProviderPage(1)
    setAiProviderPageSizeState(pageSize)
    persistCredentialListPreferences('ai-provider', { pageSize })
  }, [])
  const setCredentialActiveOnly = useCallback((activeOnly: boolean) => {
    setCredentialPage(1)
    setCredentialActiveOnlyState(activeOnly)
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(CREDENTIAL_ACTIVE_ONLY_STORAGE_KEY, String(activeOnly))
    }
  }, [])
  const setAiProviderActiveOnly = useCallback((activeOnly: boolean) => {
    setAiProviderPage(1)
    setAiProviderActiveOnlyState(activeOnly)
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(AI_PROVIDER_ACTIVE_ONLY_STORAGE_KEY, String(activeOnly))
    }
  }, [])
  const setCredentialProviderFilter = useCallback((filter: CredentialProviderFilterKey) => {
    setCredentialPage(1)
    setCredentialProviderFilterState(filter)
    persistCredentialListPreferences('credentials', { providerFilter: filter })
  }, [])
  const setAiProviderProviderFilter = useCallback((filter: CredentialProviderFilterKey) => {
    setAiProviderPage(1)
    setAiProviderProviderFilterState(filter)
    persistCredentialListPreferences('ai-provider', { providerFilter: filter })
  }, [])
  const setCredentialSort = useCallback((sort: UsageIdentityPageSort) => {
    setCredentialPage(1)
    setCredentialSortState(sort)
    persistCredentialListPreferences('credentials', { sort })
  }, [])
  const setAiProviderSort = useCallback((sort: UsageIdentityPageSort) => {
    setAiProviderPage(1)
    setAiProviderSortState(sort)
    persistCredentialListPreferences('ai-provider', { sort })
  }, [])
  const replaceUsageIdentity = useCallback((identity: UsageIdentity) => {
    const replaceByID = (items: UsageIdentity[]) => items.map((item) => (item.id === identity.id ? mergeUsageIdentityAliasUpdate(item, identity) : item))
    if (identity.auth_type === 1) {
      setCredentialIdentities(replaceByID)
      return
    }
    if (identity.auth_type === 2) {
      setAiProviderIdentities(replaceByID)
    }
  }, [])

  const refreshCredentials = useCallback(async () => {
    credentialsRequestControllerRef.current?.abort()
    const controller = new AbortController()
    credentialsRequestControllerRef.current = controller

    setCredentialsLoading(true)
    setCredentialsError('')
    try {
      const response = await fetchUsageIdentitiesPage(controller.signal, { authType: 1, activeOnly: credentialActiveOnly ? true : undefined, types: credentialProviderFilterTypes('credentials', credentialProviderFilter), sort: credentialSort, page: credentialPage, pageSize: credentialPageSize })
      if (credentialsRequestControllerRef.current !== controller) {
        return
      }
      setCredentialIdentities(response.identities ?? [])
      setCredentialTypeCounts(response.type_counts ?? [])
      setCredentialTotal(response.total_count ?? 0)
      setCredentialTotalPages(response.total_pages ?? 0)
    } catch (nextError) {
      if (controller.signal.aborted) {
        return
      }
      if (nextError instanceof ApiError && nextError.status === 401) {
        onAuthRequired?.()
        return
      }
      if (credentialsRequestControllerRef.current === controller) {
        setCredentialIdentities([])
        setCredentialTypeCounts([])
        setCredentialTotal(0)
        setCredentialTotalPages(0)
      }
      setCredentialsError(nextError instanceof Error ? nextError.message : 'Failed to load usage identities')
    } finally {
      if (credentialsRequestControllerRef.current === controller) {
        setCredentialsLoading(false)
        credentialsRequestControllerRef.current = null
      }
    }
  }, [credentialActiveOnly, credentialPage, credentialPageSize, credentialProviderFilter, credentialSort, onAuthRequired])

  const refreshAiProviders = useCallback(async () => {
    aiProvidersRequestControllerRef.current?.abort()
    const controller = new AbortController()
    aiProvidersRequestControllerRef.current = controller

    setAiProvidersLoading(true)
    setAiProvidersError('')
    try {
      const response = await fetchUsageIdentitiesPage(controller.signal, { authType: 2, activeOnly: aiProviderActiveOnly ? true : undefined, types: credentialProviderFilterTypes('ai-provider', aiProviderProviderFilter), sort: aiProviderSort, page: aiProviderPage, pageSize: aiProviderPageSize })
      if (aiProvidersRequestControllerRef.current !== controller) {
        return
      }
      setAiProviderIdentities(response.identities ?? [])
      setAiProviderTypeCounts(response.type_counts ?? [])
      setAiProviderTotal(response.total_count ?? 0)
      setAiProviderTotalPages(response.total_pages ?? 0)
    } catch (nextError) {
      if (controller.signal.aborted) {
        return
      }
      if (nextError instanceof ApiError && nextError.status === 401) {
        onAuthRequired?.()
        return
      }
      if (aiProvidersRequestControllerRef.current === controller) {
        setAiProviderIdentities([])
        setAiProviderTypeCounts([])
        setAiProviderTotal(0)
        setAiProviderTotalPages(0)
      }
      setAiProvidersError(nextError instanceof Error ? nextError.message : 'Failed to load usage identities')
    } finally {
      if (aiProvidersRequestControllerRef.current === controller) {
        setAiProvidersLoading(false)
        aiProvidersRequestControllerRef.current = null
      }
    }
  }, [aiProviderActiveOnly, aiProviderPage, aiProviderPageSize, aiProviderProviderFilter, aiProviderSort, onAuthRequired])

  const refresh = useCallback(async () => {
    // 两个凭证页已经拆成独立 tab，手动刷新只触发当前可见列表。
    const tasks = []
    if (enabledCredentials) tasks.push(refreshCredentials())
    if (enabledAiProviders) tasks.push(refreshAiProviders())
    await Promise.all(tasks)
  }, [enabledAiProviders, enabledCredentials, refreshAiProviders, refreshCredentials])

  const resetStats = useCallback(async (id: string) => {
    let updated: UsageIdentity
    try {
      updated = await resetUsageIdentityStats(id)
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) onAuthRequired?.()
      throw error
    }
    // 旧列表请求可能仍在返回中，先使其失效再应用重置结果，随后重新获取服务端排序。
    const controllerRef = updated.auth_type === 1 ? credentialsRequestControllerRef : aiProvidersRequestControllerRef
    controllerRef.current?.abort()
    controllerRef.current = null
    const replace = (items: UsageIdentity[]) => items.map((item) => item.id === updated.id
      ? { ...updated, credential_health: item.credential_health }
      : item)
    if (updated.auth_type === 1) {
      setCredentialIdentities(replace)
      setCredentialsLoading(false)
      void refreshCredentials()
    } else {
      setAiProviderIdentities(replace)
      setAiProvidersLoading(false)
      void refreshAiProviders()
    }
    return updated
  }, [onAuthRequired, refreshCredentials, refreshAiProviders])

  useEffect(() => {
    if (!enabledCredentials) {
      credentialsRequestControllerRef.current?.abort()
      credentialsRequestControllerRef.current = null
      setCredentialsLoading(false)
      return
    }
    void refreshCredentials()
    const intervalID = window.setInterval(() => {
      void refreshCredentials()
    }, CREDENTIAL_PAGES_REFRESH_INTERVAL_MS)
    return () => {
      window.clearInterval(intervalID)
      credentialsRequestControllerRef.current?.abort()
      credentialsRequestControllerRef.current = null
    }
  }, [enabledCredentials, refreshCredentials])

  useEffect(() => {
    if (!enabledAiProviders) {
      aiProvidersRequestControllerRef.current?.abort()
      aiProvidersRequestControllerRef.current = null
      setAiProvidersLoading(false)
      return
    }
    void refreshAiProviders()
    const intervalID = window.setInterval(() => {
      void refreshAiProviders()
    }, CREDENTIAL_PAGES_REFRESH_INTERVAL_MS)
    return () => {
      window.clearInterval(intervalID)
      aiProvidersRequestControllerRef.current?.abort()
      aiProvidersRequestControllerRef.current = null
    }
  }, [enabledAiProviders, refreshAiProviders])

  return {
    credentialIdentities,
    aiProviderIdentities,
    credentialTypeCounts,
    aiProviderTypeCounts,
    credentialTotal,
    aiProviderTotal,
    credentialTotalPages,
    aiProviderTotalPages,
    credentialPage,
    aiProviderPage,
    credentialPageSize,
    aiProviderPageSize,
    credentialActiveOnly,
    aiProviderActiveOnly,
    credentialProviderFilter,
    aiProviderProviderFilter,
    credentialSort,
    aiProviderSort,
    setCredentialPage,
    setAiProviderPage,
    setCredentialPageSize,
    setAiProviderPageSize,
    setCredentialActiveOnly,
    setAiProviderActiveOnly,
    setCredentialProviderFilter,
    setAiProviderProviderFilter,
    setCredentialSort,
    setAiProviderSort,
    replaceUsageIdentity,
    resetStats,
    loading: (enabledCredentials && credentialsLoading) || (enabledAiProviders && aiProvidersLoading),
    error: enabledCredentials ? credentialsError : enabledAiProviders ? aiProvidersError : '',
    refresh,
  }
}
