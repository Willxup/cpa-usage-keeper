import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, fetchUsageIdentitiesPage } from '@/lib/api'
import type { UsageIdentity } from '@/lib/types'
import { CREDENTIALS_PAGE_SIZE } from './credentialViewModels'

interface UseCredentialPagesOptions {
  enabled: boolean
  onAuthRequired?: () => void
}

export interface CredentialPagesState {
  authFileIdentities: UsageIdentity[]
  aiProviderIdentities: UsageIdentity[]
  authFileTotal: number
  aiProviderTotal: number
  authFileTotalPages: number
  aiProviderTotalPages: number
  authFilePage: number
  aiProviderPage: number
  authFilePageSize: number
  aiProviderPageSize: number
  setAuthFilePage: (page: number) => void
  setAiProviderPage: (page: number) => void
  setAuthFilePageSize: (pageSize: number) => void
  setAiProviderPageSize: (pageSize: number) => void
  loading: boolean
  error: string
  refresh: () => Promise<void>
}

export function useCredentialPages({ enabled, onAuthRequired }: UseCredentialPagesOptions): CredentialPagesState {
  const [authFileIdentities, setAuthFileIdentities] = useState<UsageIdentity[]>([])
  const [aiProviderIdentities, setAiProviderIdentities] = useState<UsageIdentity[]>([])
  const [authFileTotal, setAuthFileTotal] = useState(0)
  const [aiProviderTotal, setAiProviderTotal] = useState(0)
  const [authFileTotalPages, setAuthFileTotalPages] = useState(0)
  const [aiProviderTotalPages, setAiProviderTotalPages] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [authFilePage, setAuthFilePage] = useState(1)
  const [aiProviderPage, setAiProviderPage] = useState(1)
  const [authFilePageSize, setAuthFilePageSizeState] = useState(CREDENTIALS_PAGE_SIZE)
  const [aiProviderPageSize, setAiProviderPageSizeState] = useState(CREDENTIALS_PAGE_SIZE)
  const requestControllerRef = useRef<AbortController | null>(null)

  const setAuthFilePageSize = useCallback((pageSize: number) => {
    setAuthFilePage(1)
    setAuthFilePageSizeState(pageSize)
  }, [])
  const setAiProviderPageSize = useCallback((pageSize: number) => {
    setAiProviderPage(1)
    setAiProviderPageSizeState(pageSize)
  }, [])

  const refresh = useCallback(async () => {
    // 每次刷新先取消旧请求，避免切页后旧响应覆盖新页数据。
    requestControllerRef.current?.abort()
    const controller = new AbortController()
    requestControllerRef.current = controller

    setLoading(true)
    setError('')
    try {
      // Auth Files 和 AI Provider 分别按 auth_type 请求，分页互不影响。
      const [authFiles, aiProviders] = await Promise.all([
        fetchUsageIdentitiesPage(controller.signal, { authType: 1, page: authFilePage, pageSize: authFilePageSize }),
        fetchUsageIdentitiesPage(controller.signal, { authType: 2, page: aiProviderPage, pageSize: aiProviderPageSize }),
      ])
      if (requestControllerRef.current !== controller) {
        return
      }
      setAuthFileIdentities(authFiles.identities ?? [])
      setAiProviderIdentities(aiProviders.identities ?? [])
      setAuthFileTotal(authFiles.total_count ?? 0)
      setAiProviderTotal(aiProviders.total_count ?? 0)
      setAuthFileTotalPages(authFiles.total_pages ?? 0)
      setAiProviderTotalPages(aiProviders.total_pages ?? 0)
    } catch (nextError) {
      if (controller.signal.aborted) {
        return
      }
      if (nextError instanceof ApiError && nextError.status === 401) {
        onAuthRequired?.()
        return
      }
      if (requestControllerRef.current === controller) {
        // 只有当前请求失败才清空页面数据，过期请求失败不影响最新状态。
        setAuthFileIdentities([])
        setAiProviderIdentities([])
        setAuthFileTotal(0)
        setAiProviderTotal(0)
        setAuthFileTotalPages(0)
        setAiProviderTotalPages(0)
      }
      setError(nextError instanceof Error ? nextError.message : 'Failed to load usage identities')
    } finally {
      if (requestControllerRef.current === controller) {
        setLoading(false)
        requestControllerRef.current = null
      }
    }
  }, [aiProviderPage, aiProviderPageSize, authFilePage, authFilePageSize, onAuthRequired])

  useEffect(() => {
    if (!enabled) {
      requestControllerRef.current?.abort()
      requestControllerRef.current = null
      setLoading(false)
      return
    }
    void refresh()
    return () => {
      requestControllerRef.current?.abort()
      requestControllerRef.current = null
    }
  }, [enabled, refresh])

  return {
    authFileIdentities,
    aiProviderIdentities,
    authFileTotal,
    aiProviderTotal,
    authFileTotalPages,
    aiProviderTotalPages,
    authFilePage,
    aiProviderPage,
    authFilePageSize,
    aiProviderPageSize,
    setAuthFilePage,
    setAiProviderPage,
    setAuthFilePageSize,
    setAiProviderPageSize,
    loading,
    error,
    refresh,
  }
}
