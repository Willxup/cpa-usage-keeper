import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, fetchUsageIdentitiesPage, type UsageIdentityPageSort } from '@/lib/api'
import type { UsageIdentity, UsageIdentityTypeCount } from '@/lib/types'
import { credentialProviderFilterTypes, type CredentialProviderFilterKey } from './credentialProviderFilters'
import { CREDENTIALS_PAGE_SIZE } from './credentialViewModels'

interface UseCredentialPagesOptions {
  enabledAuthFiles: boolean
  enabledAiProviders: boolean
  onAuthRequired?: () => void
}

export const CREDENTIAL_PAGES_REFRESH_INTERVAL_MS = 60 * 1000

const AUTH_FILE_ACTIVE_ONLY_STORAGE_KEY = 'cpa-usage-keeper-auth-files-active-only'
const AI_PROVIDER_ACTIVE_ONLY_STORAGE_KEY = 'cpa-usage-keeper-ai-providers-active-only'

export function mergeUsageIdentityAliasUpdate(current: UsageIdentity, updated: UsageIdentity): UsageIdentity {
  return {
    ...current,
    alias: updated.alias ?? null,
    displayName: updated.displayName ?? current.displayName,
  }
}

const getInitialAuthFileActiveOnly = () => {
  if (typeof window === 'undefined') return false
  return window.localStorage.getItem(AUTH_FILE_ACTIVE_ONLY_STORAGE_KEY) === 'true'
}

const getInitialAiProviderActiveOnly = () => {
  if (typeof window === 'undefined') return false
  return window.localStorage.getItem(AI_PROVIDER_ACTIVE_ONLY_STORAGE_KEY) === 'true'
}

export interface CredentialPagesState {
  authFileIdentities: UsageIdentity[]
  aiProviderIdentities: UsageIdentity[]
  authFileTypeCounts: UsageIdentityTypeCount[]
  aiProviderTypeCounts: UsageIdentityTypeCount[]
  authFileTotal: number
  aiProviderTotal: number
  authFileTotalPages: number
  aiProviderTotalPages: number
  authFilePage: number
  aiProviderPage: number
  authFilePageSize: number
  aiProviderPageSize: number
  authFileActiveOnly: boolean
  aiProviderActiveOnly: boolean
  authFileProviderFilter: CredentialProviderFilterKey
  aiProviderProviderFilter: CredentialProviderFilterKey
  authFileSort: UsageIdentityPageSort
  aiProviderSort: UsageIdentityPageSort
  setAuthFilePage: (page: number) => void
  setAiProviderPage: (page: number) => void
  setAuthFilePageSize: (pageSize: number) => void
  setAiProviderPageSize: (pageSize: number) => void
  setAuthFileActiveOnly: (activeOnly: boolean) => void
  setAiProviderActiveOnly: (activeOnly: boolean) => void
  setAuthFileProviderFilter: (filter: CredentialProviderFilterKey) => void
  setAiProviderProviderFilter: (filter: CredentialProviderFilterKey) => void
  setAuthFileSort: (sort: UsageIdentityPageSort) => void
  setAiProviderSort: (sort: UsageIdentityPageSort) => void
  replaceUsageIdentity: (identity: UsageIdentity) => void
  loading: boolean
  error: string
  refresh: () => Promise<void>
}

export function useCredentialPages({ enabledAuthFiles, enabledAiProviders, onAuthRequired }: UseCredentialPagesOptions): CredentialPagesState {
  const [authFileIdentities, setAuthFileIdentities] = useState<UsageIdentity[]>([])
  const [aiProviderIdentities, setAiProviderIdentities] = useState<UsageIdentity[]>([])
  const [authFileTypeCounts, setAuthFileTypeCounts] = useState<UsageIdentityTypeCount[]>([])
  const [aiProviderTypeCounts, setAiProviderTypeCounts] = useState<UsageIdentityTypeCount[]>([])
  const [authFileTotal, setAuthFileTotal] = useState(0)
  const [aiProviderTotal, setAiProviderTotal] = useState(0)
  const [authFileTotalPages, setAuthFileTotalPages] = useState(0)
  const [aiProviderTotalPages, setAiProviderTotalPages] = useState(0)
  const [authFilesError, setAuthFilesError] = useState('')
  const [aiProvidersError, setAiProvidersError] = useState('')
  const [authFilePage, setAuthFilePage] = useState(1)
  const [aiProviderPage, setAiProviderPage] = useState(1)
  const [authFilePageSize, setAuthFilePageSizeState] = useState(CREDENTIALS_PAGE_SIZE)
  const [aiProviderPageSize, setAiProviderPageSizeState] = useState(CREDENTIALS_PAGE_SIZE)
  const [authFileActiveOnly, setAuthFileActiveOnlyState] = useState(getInitialAuthFileActiveOnly)
  const [aiProviderActiveOnly, setAiProviderActiveOnlyState] = useState(getInitialAiProviderActiveOnly)
  const [authFileProviderFilter, setAuthFileProviderFilterState] = useState<CredentialProviderFilterKey>('all')
  const [aiProviderProviderFilter, setAiProviderProviderFilterState] = useState<CredentialProviderFilterKey>('all')
  const [authFileSort, setAuthFileSortState] = useState<UsageIdentityPageSort>('priority')
  const [aiProviderSort, setAiProviderSortState] = useState<UsageIdentityPageSort>('total_requests')
  const [authFilesLoading, setAuthFilesLoading] = useState(false)
  const [aiProvidersLoading, setAiProvidersLoading] = useState(false)
  const authFilesRequestControllerRef = useRef<AbortController | null>(null)
  const aiProvidersRequestControllerRef = useRef<AbortController | null>(null)

  const setAuthFilePageSize = useCallback((pageSize: number) => {
    setAuthFilePage(1)
    setAuthFilePageSizeState(pageSize)
  }, [])
  const setAiProviderPageSize = useCallback((pageSize: number) => {
    setAiProviderPage(1)
    setAiProviderPageSizeState(pageSize)
  }, [])
  const setAuthFileActiveOnly = useCallback((activeOnly: boolean) => {
    setAuthFilePage(1)
    setAuthFileActiveOnlyState(activeOnly)
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(AUTH_FILE_ACTIVE_ONLY_STORAGE_KEY, String(activeOnly))
    }
  }, [])
  const setAiProviderActiveOnly = useCallback((activeOnly: boolean) => {
    setAiProviderPage(1)
    setAiProviderActiveOnlyState(activeOnly)
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(AI_PROVIDER_ACTIVE_ONLY_STORAGE_KEY, String(activeOnly))
    }
  }, [])
  const setAuthFileProviderFilter = useCallback((filter: CredentialProviderFilterKey) => {
    setAuthFilePage(1)
    setAuthFileProviderFilterState(filter)
  }, [])
  const setAiProviderProviderFilter = useCallback((filter: CredentialProviderFilterKey) => {
    setAiProviderPage(1)
    setAiProviderProviderFilterState(filter)
  }, [])
  const setAuthFileSort = useCallback((sort: UsageIdentityPageSort) => {
    setAuthFilePage(1)
    setAuthFileSortState(sort)
  }, [])
  const setAiProviderSort = useCallback((sort: UsageIdentityPageSort) => {
    setAiProviderPage(1)
    setAiProviderSortState(sort)
  }, [])
  const replaceUsageIdentity = useCallback((identity: UsageIdentity) => {
    const replaceByID = (items: UsageIdentity[]) => items.map((item) => (item.id === identity.id ? mergeUsageIdentityAliasUpdate(item, identity) : item))
    if (identity.auth_type === 1) {
      setAuthFileIdentities(replaceByID)
      return
    }
    if (identity.auth_type === 2) {
      setAiProviderIdentities(replaceByID)
    }
  }, [])

  const refreshAuthFiles = useCallback(async () => {
    authFilesRequestControllerRef.current?.abort()
    const controller = new AbortController()
    authFilesRequestControllerRef.current = controller

    setAuthFilesLoading(true)
    setAuthFilesError('')
    try {
      const response = await fetchUsageIdentitiesPage(controller.signal, { authType: 1, activeOnly: authFileActiveOnly ? true : undefined, types: credentialProviderFilterTypes('auth-files', authFileProviderFilter), sort: authFileSort, page: authFilePage, pageSize: authFilePageSize })
      if (authFilesRequestControllerRef.current !== controller) {
        return
      }
      setAuthFileIdentities(response.identities ?? [])
      setAuthFileTypeCounts(response.type_counts ?? [])
      setAuthFileTotal(response.total_count ?? 0)
      setAuthFileTotalPages(response.total_pages ?? 0)
    } catch (nextError) {
      if (controller.signal.aborted) {
        return
      }
      if (nextError instanceof ApiError && nextError.status === 401) {
        onAuthRequired?.()
        return
      }
      if (authFilesRequestControllerRef.current === controller) {
        setAuthFileIdentities([])
        setAuthFileTypeCounts([])
        setAuthFileTotal(0)
        setAuthFileTotalPages(0)
      }
      setAuthFilesError(nextError instanceof Error ? nextError.message : 'Failed to load usage identities')
    } finally {
      if (authFilesRequestControllerRef.current === controller) {
        setAuthFilesLoading(false)
        authFilesRequestControllerRef.current = null
      }
    }
  }, [authFileActiveOnly, authFilePage, authFilePageSize, authFileProviderFilter, authFileSort, onAuthRequired])

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
    if (enabledAuthFiles) tasks.push(refreshAuthFiles())
    if (enabledAiProviders) tasks.push(refreshAiProviders())
    await Promise.all(tasks)
  }, [enabledAiProviders, enabledAuthFiles, refreshAiProviders, refreshAuthFiles])

  useEffect(() => {
    if (!enabledAuthFiles) {
      authFilesRequestControllerRef.current?.abort()
      authFilesRequestControllerRef.current = null
      setAuthFilesLoading(false)
      return
    }
    void refreshAuthFiles()
    const intervalID = window.setInterval(() => {
      void refreshAuthFiles()
    }, CREDENTIAL_PAGES_REFRESH_INTERVAL_MS)
    return () => {
      window.clearInterval(intervalID)
      authFilesRequestControllerRef.current?.abort()
      authFilesRequestControllerRef.current = null
    }
  }, [enabledAuthFiles, refreshAuthFiles])

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
    authFileIdentities,
    aiProviderIdentities,
    authFileTypeCounts,
    aiProviderTypeCounts,
    authFileTotal,
    aiProviderTotal,
    authFileTotalPages,
    aiProviderTotalPages,
    authFilePage,
    aiProviderPage,
    authFilePageSize,
    aiProviderPageSize,
    authFileActiveOnly,
    aiProviderActiveOnly,
    authFileProviderFilter,
    aiProviderProviderFilter,
    authFileSort,
    aiProviderSort,
    setAuthFilePage,
    setAiProviderPage,
    setAuthFilePageSize,
    setAiProviderPageSize,
    setAuthFileActiveOnly,
    setAiProviderActiveOnly,
    setAuthFileProviderFilter,
    setAiProviderProviderFilter,
    setAuthFileSort,
    setAiProviderSort,
    replaceUsageIdentity,
    loading: (enabledAuthFiles && authFilesLoading) || (enabledAiProviders && aiProvidersLoading),
    error: enabledAuthFiles ? authFilesError : enabledAiProviders ? aiProvidersError : '',
    refresh,
  }
}
