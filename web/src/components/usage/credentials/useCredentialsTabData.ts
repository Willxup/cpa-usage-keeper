import { useMemo } from 'react'
import {
  buildAiProviderCredentialRows,
  buildAuthFileCredentialRows,
  selectQuotaEligibleAuthIndexes,
  type AiProviderCredentialRow,
  type AuthFileCredentialRow,
} from './credentialViewModels'
import { useCredentialPages } from './useCredentialPages'
import { useQuotaCache } from './useQuotaCache'
import type { UsageIdentityPageSort } from '@/lib/api'
import type { UsageIdentity } from '@/lib/types'
import { quotaRefreshDisplayError, useQuotaRefreshTasks } from './useQuotaRefreshTasks'

interface UseCredentialsTabDataOptions {
  enabled: boolean
  onAuthRequired?: () => void
}

export interface CredentialsTabData {
  authFileRows: AuthFileCredentialRow[]
  aiProviderRows: AiProviderCredentialRow[]
  allIdentitiesForFilter: UsageIdentity[]
  authFileTotal: number
  aiProviderTotal: number
  authFilePageSize: number
  aiProviderPageSize: number
  authFilePage: number
  aiProviderPage: number
  authFileTotalPages: number
  aiProviderTotalPages: number
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
  quotaRefreshing: boolean
  quotaRefreshError: string
  refresh: () => Promise<void>
  refreshQuotaForCurrentAuthFilePage: () => Promise<void>
  refreshQuotaForAuthIndex: (authIndex: string) => Promise<void>
}

export function useCredentialsTabData({ enabled, onAuthRequired }: UseCredentialsTabDataOptions): CredentialsTabData {
  // 页面 hook 只编排分页、缓存和刷新任务三层数据，不直接发散 API 调用。
  const credentialPages = useCredentialPages({ enabled, onAuthRequired })
  const currentAuthIndexes = useMemo(
    // quota 只对当前 Auth Files 页生效，AI Provider 不参与缓存读取和刷新。
    () => selectQuotaEligibleAuthIndexes(credentialPages.authFileIdentities),
    [credentialPages.authFileIdentities],
  )
  const { quotaByAuthIndex, setQuotaByAuthIndex } = useQuotaCache({
    enabled,
    authIndexes: currentAuthIndexes,
    onAuthRequired,
  })
  const quotaRefreshTasks = useQuotaRefreshTasks({
    enabled,
    currentAuthIndexes,
    setQuotaByAuthIndex,
    onAuthRequired,
  })

  // 把对象状态转成 Map 后交给纯 view model，组件层只消费已组合好的行数据。
  const quotaRowsByAuthIndex = useMemo(() => new Map(Object.entries(quotaByAuthIndex)), [quotaByAuthIndex])
  const quotaStates = useMemo(() => new Map(Object.entries(quotaRefreshTasks.quotaStateByAuthIndex).map(([authIndex, state]) => [authIndex, {
    quotaLoading: state.loading ?? false,
    quotaError: state.error,
    refreshTaskId: state.refreshTaskId,
    refreshStatus: state.refreshStatus,
  }])), [quotaRefreshTasks.quotaStateByAuthIndex])

  const authFileRows = useMemo(
    () => buildAuthFileCredentialRows(credentialPages.authFileIdentities, quotaRowsByAuthIndex, quotaStates),
    [credentialPages.authFileIdentities, quotaRowsByAuthIndex, quotaStates],
  )
  const aiProviderRows = useMemo(
    () => buildAiProviderCredentialRows(credentialPages.aiProviderIdentities),
    [credentialPages.aiProviderIdentities],
  )

  return {
    authFileRows,
    aiProviderRows,
    allIdentitiesForFilter: credentialPages.allIdentitiesForFilter,
    authFileTotal: credentialPages.authFileTotal,
    aiProviderTotal: credentialPages.aiProviderTotal,
    authFilePageSize: credentialPages.authFilePageSize,
    aiProviderPageSize: credentialPages.aiProviderPageSize,
    authFilePage: credentialPages.authFilePage,
    aiProviderPage: credentialPages.aiProviderPage,
    authFileTotalPages: credentialPages.authFileTotalPages,
    aiProviderTotalPages: credentialPages.aiProviderTotalPages,
    authFileActiveOnly: credentialPages.authFileActiveOnly,
    authFileSort: credentialPages.authFileSort,
    aiProviderSort: credentialPages.aiProviderSort,
    setAuthFilePage: credentialPages.setAuthFilePage,
    setAiProviderPage: credentialPages.setAiProviderPage,
    setAuthFilePageSize: credentialPages.setAuthFilePageSize,
    setAiProviderPageSize: credentialPages.setAiProviderPageSize,
    setAuthFileActiveOnly: credentialPages.setAuthFileActiveOnly,
    setAuthFileSort: credentialPages.setAuthFileSort,
    setAiProviderSort: credentialPages.setAiProviderSort,
    loading: credentialPages.loading,
    error: credentialPages.error,
    quotaRefreshing: quotaRefreshTasks.quotaRefreshing,
    quotaRefreshError: quotaRefreshTasks.quotaRefreshError,
    refresh: credentialPages.refresh,
    refreshQuotaForCurrentAuthFilePage: quotaRefreshTasks.refreshQuotaForCurrentAuthFilePage,
    refreshQuotaForAuthIndex: quotaRefreshTasks.refreshQuotaForAuthIndex,
  }
}

export { quotaRefreshDisplayError }
