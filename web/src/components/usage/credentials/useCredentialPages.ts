import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, fetchUsageIdentities, fetchUsageIdentitiesPage, type UsageIdentityPageSort } from '@/lib/api'
import type { UsageIdentity } from '@/lib/types'
import { CREDENTIALS_PAGE_SIZE } from './credentialViewModels'

interface UseCredentialPagesOptions {
  enabled: boolean
  onAuthRequired?: () => void
}

const AUTH_FILE_ACTIVE_ONLY_STORAGE_KEY = 'cpa-usage-keeper-auth-files-active-only'

const getInitialAuthFileActiveOnly = () => {
  if (typeof window === 'undefined') return false
  return window.localStorage.getItem(AUTH_FILE_ACTIVE_ONLY_STORAGE_KEY) === 'true'
}

export interface CredentialPagesState {
  authFileIdentities: UsageIdentity[]
  aiProviderIdentities: UsageIdentity[]
  allIdentitiesForFilter: UsageIdentity[]
  authFileTotal: number
  aiProviderTotal: number
  authFileTotalPages: number
  aiProviderTotalPages: number
  authFilePage: number
  aiProviderPage: number
  authFilePageSize: number
  aiProviderPageSize: number
  authFileActiveOnly: boolean
  authFileSort: UsageIdentityPageSort
  aiProviderSort: UsageIdentityPageSort
  setAuthFilePage: (page: number) => void
  setAiProviderPage: (page: number) => void
  setAuthFilePageSize: (pageSize: number) => void
  setAiProviderPageSize: (pageSize: number) => void
  setAuthFileActiveOnly: (activeOnly: boolean) => void
  setAuthFileSort: (sort: UsageIdentityPageSort) => void
  setAiProviderSort: (sort: UsageIdentityPageSort) => void
  loading: boolean
  error: string
  refresh: () => Promise<void>
}

export function useCredentialPages({ enabled, onAuthRequired }: UseCredentialPagesOptions): CredentialPagesState {
  const [authFileIdentities, setAuthFileIdentities] = useState<UsageIdentity[]>([])
  const [aiProviderIdentities, setAiProviderIdentities] = useState<UsageIdentity[]>([])
  const [allIdentitiesForFilter, setAllIdentitiesForFilter] = useState<UsageIdentity[]>([])
  const [authFileTotal, setAuthFileTotal] = useState(0)
  const [aiProviderTotal, setAiProviderTotal] = useState(0)
  const [authFileTotalPages, setAuthFileTotalPages] = useState(0)
  const [aiProviderTotalPages, setAiProviderTotalPages] = useState(0)
  const [error, setError] = useState('')
  const [authFilePage, setAuthFilePage] = useState(1)
  const [aiProviderPage, setAiProviderPage] = useState(1)
  const [authFilePageSize, setAuthFilePageSizeState] = useState(CREDENTIALS_PAGE_SIZE)
  const [aiProviderPageSize, setAiProviderPageSizeState] = useState(CREDENTIALS_PAGE_SIZE)
  const [authFileActiveOnly, setAuthFileActiveOnlyState] = useState(getInitialAuthFileActiveOnly)
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
  const setAuthFileSort = useCallback((sort: UsageIdentityPageSort) => {
    setAuthFilePage(1)
    setAuthFileSortState(sort)
  }, [])
  const setAiProviderSort = useCallback((sort: UsageIdentityPageSort) => {
    setAiProviderPage(1)
    setAiProviderSortState(sort)
  }, [])

  const refreshAuthFiles = useCallback(async () => {
    authFilesRequestControllerRef.current?.abort()
    const controller = new AbortController()
    authFilesRequestControllerRef.current = controller

    setAuthFilesLoading(true)
    setError('')
    try {
      const response = await fetchUsageIdentitiesPage(controller.signal, { authType: 1, activeOnly: authFileActiveOnly ? true : undefined, sort: authFileSort, page: authFilePage, pageSize: authFilePageSize })
      if (authFilesRequestControllerRef.current !== controller) {
        return
      }
      setAuthFileIdentities(response.identities ?? [])
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
        setAuthFileTotal(0)
        setAuthFileTotalPages(0)
      }
      setError(nextError instanceof Error ? nextError.message : 'Failed to load usage identities')
    } finally {
      if (authFilesRequestControllerRef.current === controller) {
        setAuthFilesLoading(false)
        authFilesRequestControllerRef.current = null
      }
    }
  }, [authFileActiveOnly, authFilePage, authFilePageSize, authFileSort, onAuthRequired])

  const refreshAiProviders = useCallback(async () => {
    aiProvidersRequestControllerRef.current?.abort()
    const controller = new AbortController()
    aiProvidersRequestControllerRef.current = controller

    setAiProvidersLoading(true)
    setError('')
    try {
      const response = await fetchUsageIdentitiesPage(controller.signal, { authType: 2, sort: aiProviderSort, page: aiProviderPage, pageSize: aiProviderPageSize })
      if (aiProvidersRequestControllerRef.current !== controller) {
        return
      }
      setAiProviderIdentities(response.identities ?? [])
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
        setAiProviderTotal(0)
        setAiProviderTotalPages(0)
      }
      setError(nextError instanceof Error ? nextError.message : 'Failed to load usage identities')
    } finally {
      if (aiProvidersRequestControllerRef.current === controller) {
        setAiProvidersLoading(false)
        aiProvidersRequestControllerRef.current = null
      }
    }
  }, [aiProviderPage, aiProviderPageSize, aiProviderSort, onAuthRequired])

  const allIdentitiesControllerRef = useRef<AbortController | null>(null)
  const refreshAllIdentitiesForFilter = useCallback(async () => {
    allIdentitiesControllerRef.current?.abort()
    const controller = new AbortController()
    allIdentitiesControllerRef.current = controller
    try {
      const response = await fetchUsageIdentities(controller.signal)
      if (allIdentitiesControllerRef.current === controller) {
        setAllIdentitiesForFilter(response.identities ?? [])
      }
    } catch (nextError) {
      if (controller.signal.aborted) return
      if (nextError instanceof ApiError && nextError.status === 401) {
        onAuthRequired?.()
      }
    } finally {
      if (allIdentitiesControllerRef.current === controller) {
        allIdentitiesControllerRef.current = null
      }
    }
  }, [onAuthRequired])

  const refresh = useCallback(async () => {
    await Promise.all([refreshAuthFiles(), refreshAiProviders(), refreshAllIdentitiesForFilter()])
  }, [refreshAiProviders, refreshAllIdentitiesForFilter, refreshAuthFiles])

  useEffect(() => {
    if (!enabled) {
      allIdentitiesControllerRef.current?.abort()
      allIdentitiesControllerRef.current = null
      return
    }
    void refreshAllIdentitiesForFilter()
    return () => {
      allIdentitiesControllerRef.current?.abort()
      allIdentitiesControllerRef.current = null
    }
  }, [enabled, refreshAllIdentitiesForFilter])

  useEffect(() => {
    if (!enabled) {
      authFilesRequestControllerRef.current?.abort()
      authFilesRequestControllerRef.current = null
      setAuthFilesLoading(false)
      return
    }
    void refreshAuthFiles()
    return () => {
      authFilesRequestControllerRef.current?.abort()
      authFilesRequestControllerRef.current = null
    }
  }, [enabled, refreshAuthFiles])

  useEffect(() => {
    if (!enabled) {
      aiProvidersRequestControllerRef.current?.abort()
      aiProvidersRequestControllerRef.current = null
      setAiProvidersLoading(false)
      return
    }
    void refreshAiProviders()
    return () => {
      aiProvidersRequestControllerRef.current?.abort()
      aiProvidersRequestControllerRef.current = null
    }
  }, [enabled, refreshAiProviders])

  return {
    authFileIdentities,
    aiProviderIdentities,
    allIdentitiesForFilter,
    authFileTotal,
    aiProviderTotal,
    authFileTotalPages,
    aiProviderTotalPages,
    authFilePage,
    aiProviderPage,
    authFilePageSize,
    aiProviderPageSize,
    authFileActiveOnly,
    authFileSort,
    aiProviderSort,
    setAuthFilePage,
    setAiProviderPage,
    setAuthFilePageSize,
    setAiProviderPageSize,
    setAuthFileActiveOnly,
    setAuthFileSort,
    setAiProviderSort,
    loading: authFilesLoading || aiProvidersLoading,
    error,
    refresh,
  }
}
