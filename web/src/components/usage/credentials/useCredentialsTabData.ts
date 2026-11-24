import { useCallback, useMemo } from 'react'
import {
  buildAiProviderCredentialRows,
  buildCredentialRows,
  selectQuotaEligibleAuthIndexes,
  type AiProviderCredentialRow,
  type CredentialEditChange,
  type CredentialRow,
} from './credentialViewModels'
import { useCredentialPages } from './useCredentialPages'
import { useQuotaCache } from './useQuotaCache'
import { useQuotaInspection } from './useQuotaInspection'
import { useQuotaReset, type CredentialResetState } from './useQuotaReset'
import { useCredentialMutations } from './useCredentialMutations'
import { type CredentialStatusKind, type UsageIdentityPageSort } from '@/lib/api'
import type { UsageIdentity, UsageIdentityTypeCount, UsageQuotaInspectionStatusResponse, UsageQuotaResetResponse } from '@/lib/types'
import { quotaRefreshDisplayError, useQuotaRefreshTasks, type QuotaState } from './useQuotaRefreshTasks'
import type { CredentialProviderFilterKey } from './credentialProviderFilters'
import type { CredentialViewMode } from './credentialListPreferences'

type CredentialQuotaState = Pick<CredentialRow, 'quotaLoading' | 'quotaError' | 'refreshStatus' | 'quotaResetting'>

interface UseCredentialsTabDataOptions {
  enabledCredentials: boolean
  enabledAiProviders: boolean
  onAuthRequired?: () => void
  onNotice?: (kind: 'success' | 'info' | 'error', message: string) => void
  onPrioritySaved?: () => void
}

export interface CredentialsTabData {
  credentialRows: CredentialRow[]
  aiProviderRows: AiProviderCredentialRow[]
  credentialTypeCounts: UsageIdentityTypeCount[]
  aiProviderTypeCounts: UsageIdentityTypeCount[]
  credentialTotal: number
  aiProviderTotal: number
  credentialPageSize: number
  aiProviderPageSize: number
  credentialPage: number
  aiProviderPage: number
  credentialTotalPages: number
  aiProviderTotalPages: number
  credentialActiveOnly: boolean
  aiProviderActiveOnly: boolean
  credentialProviderFilter: CredentialProviderFilterKey
  aiProviderProviderFilter: CredentialProviderFilterKey
  credentialSort: UsageIdentityPageSort
  aiProviderSort: UsageIdentityPageSort
  credentialViewMode: CredentialViewMode
  aiProviderViewMode: CredentialViewMode
  setCredentialViewMode: (mode: CredentialViewMode) => void
  setAiProviderViewMode: (mode: CredentialViewMode) => void
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
  loading: boolean
  error: string
  quotaRefreshing: boolean
  quotaRefreshError: string
  quotaInspectionStatus: UsageQuotaInspectionStatusResponse | null
  quotaInspectionLoading: boolean
  quotaInspectionStarting: boolean
  quotaInspectionError: string
  /** 正在写入上游状态的 Keeper identity id 集合，两个列表共用同一份进行中状态。 */
  credentialStatusPendingIdentityIds: ReadonlySet<string>
  toggleCredentialStatus: (identityId: string, authIndex: string, disabled: boolean) => void
  toggleAiProviderStatus: (identityId: string, authIndex: string, disabled: boolean) => void
  saveCredentialPriority: (identityId: string, authIndex: string, priority: number) => Promise<void>
  saveAiProviderPriority: (identityId: string, authIndex: string, priority: number) => Promise<void>
  refresh: () => Promise<void>
  refreshAfterRecalculation: () => Promise<void>
  saveCredentialField: (kind: CredentialStatusKind, id: string, authIndex: string, change: CredentialEditChange) => Promise<void>
  resetUsageIdentityStats: (id: string) => Promise<UsageIdentity>
  refreshQuotaForCurrentCredentialPage: () => Promise<void>
  refreshQuotaForAuthIndex: (authIndex: string) => Promise<void>
  resetQuotaForAuthIndex: (authIndex: string, grantId?: string, organizationId?: string) => Promise<UsageQuotaResetResponse | void>
  refreshQuotaInspectionStatus: () => Promise<void>
  startQuotaInspection: () => Promise<void>
}

export function useCredentialsTabData({ enabledCredentials, enabledAiProviders, onAuthRequired, onNotice, onPrioritySaved }: UseCredentialsTabDataOptions): CredentialsTabData {
  const credentialPages = useCredentialPages({ enabledCredentials, enabledAiProviders, onAuthRequired })
  const currentAuthIndexes = useMemo(
    () => selectQuotaEligibleAuthIndexes(credentialPages.credentialIdentities),
    [credentialPages.credentialIdentities],
  )
  const { quotaResponseByAuthIndex, quotaStateByAuthIndex, applyRefreshUpdates, refreshQuotaCache, resetQuotaCache } = useQuotaCache({
    enabled: enabledCredentials,
    authIndexes: currentAuthIndexes,
    onAuthRequired,
  })
  const quotaRefreshTasks = useQuotaRefreshTasks({
    enabled: enabledCredentials,
    currentAuthIndexes,
    quotaStateByAuthIndex,
    applyRefreshUpdates,
    onAuthRequired,
  })
  const { refreshQuotaForAuthIndex, resetQuotaRefreshTasks } = quotaRefreshTasks
  const { quotaResetStateByAuthIndex, resetQuotaForAuthIndex } = useQuotaReset({ refreshQuotaForAuthIndex, onNotice })
  const quotaInspection = useQuotaInspection({
    enabled: enabledCredentials,
    onAuthRequired,
    onInspectionCompleted: refreshQuotaCache,
  })
  const { refreshQuotaInspectionStatus, resetQuotaInspection } = quotaInspection

  const quotaResponsesByAuthIndex = useMemo(() => new Map(Object.entries(quotaResponseByAuthIndex)), [quotaResponseByAuthIndex])
  const quotaStates = useMemo(
    () => buildCredentialQuotaStateMap(quotaStateByAuthIndex, quotaResetStateByAuthIndex),
    [quotaStateByAuthIndex, quotaResetStateByAuthIndex],
  )

  const credentialRows = useMemo(
    () => buildCredentialRows(credentialPages.credentialIdentities, quotaResponsesByAuthIndex, quotaStates),
    [credentialPages.credentialIdentities, quotaResponsesByAuthIndex, quotaStates],
  )
  const aiProviderRows = useMemo(
    () => buildAiProviderCredentialRows(credentialPages.aiProviderIdentities),
    [credentialPages.aiProviderIdentities],
  )
  const refreshCredentialPages = credentialPages.refresh
  const refresh = useCallback(async () => {
    await Promise.all([refreshCredentialPages(), refreshQuotaCache()])
  }, [refreshCredentialPages, refreshQuotaCache])

  const refreshAfterRecalculation = useCallback(async () => {
    // 当前页观察到重算终态后，先清三类旧响应归属，再只读取 Keeper 缓存与巡检状态。
    resetQuotaCache()
    resetQuotaRefreshTasks()
    resetQuotaInspection()
    if (enabledCredentials) {
      await Promise.all([refreshQuotaCache(), refreshQuotaInspectionStatus()])
    }
  }, [enabledCredentials, refreshQuotaCache, refreshQuotaInspectionStatus, resetQuotaCache, resetQuotaInspection, resetQuotaRefreshTasks])

  const { credentialStatusPendingIdentityIds, toggleCredentialStatus, toggleAiProviderStatus,
    saveCredentialPriority, saveAiProviderPriority, saveCredentialField } = useCredentialMutations({
    refreshCredentialPages, replaceUsageIdentity: credentialPages.replaceUsageIdentity, onAuthRequired, onNotice, onPrioritySaved,
  })

  return {
    credentialRows,
    aiProviderRows,
    credentialTypeCounts: credentialPages.credentialTypeCounts,
    aiProviderTypeCounts: credentialPages.aiProviderTypeCounts,
    credentialTotal: credentialPages.credentialTotal,
    aiProviderTotal: credentialPages.aiProviderTotal,
    credentialPageSize: credentialPages.credentialPageSize,
    aiProviderPageSize: credentialPages.aiProviderPageSize,
    credentialPage: credentialPages.credentialPage,
    aiProviderPage: credentialPages.aiProviderPage,
    credentialTotalPages: credentialPages.credentialTotalPages,
    aiProviderTotalPages: credentialPages.aiProviderTotalPages,
    credentialActiveOnly: credentialPages.credentialActiveOnly,
    aiProviderActiveOnly: credentialPages.aiProviderActiveOnly,
    credentialProviderFilter: credentialPages.credentialProviderFilter,
    aiProviderProviderFilter: credentialPages.aiProviderProviderFilter,
    credentialSort: credentialPages.credentialSort,
    aiProviderSort: credentialPages.aiProviderSort,
    credentialViewMode: credentialPages.credentialViewMode,
    aiProviderViewMode: credentialPages.aiProviderViewMode,
    setCredentialViewMode: credentialPages.setCredentialViewMode,
    setAiProviderViewMode: credentialPages.setAiProviderViewMode,
    setCredentialPage: credentialPages.setCredentialPage,
    setAiProviderPage: credentialPages.setAiProviderPage,
    setCredentialPageSize: credentialPages.setCredentialPageSize,
    setAiProviderPageSize: credentialPages.setAiProviderPageSize,
    setCredentialActiveOnly: credentialPages.setCredentialActiveOnly,
    setAiProviderActiveOnly: credentialPages.setAiProviderActiveOnly,
    setCredentialProviderFilter: credentialPages.setCredentialProviderFilter,
    setAiProviderProviderFilter: credentialPages.setAiProviderProviderFilter,
    setCredentialSort: credentialPages.setCredentialSort,
    setAiProviderSort: credentialPages.setAiProviderSort,
    loading: credentialPages.loading,
    error: credentialPages.error,
    quotaRefreshing: quotaRefreshTasks.quotaRefreshing,
    quotaRefreshError: quotaRefreshTasks.quotaRefreshError,
    quotaInspectionStatus: quotaInspection.quotaInspectionStatus,
    quotaInspectionLoading: quotaInspection.quotaInspectionLoading,
    quotaInspectionStarting: quotaInspection.quotaInspectionStarting,
    quotaInspectionError: quotaInspection.quotaInspectionError,
    credentialStatusPendingIdentityIds,
    toggleCredentialStatus,
    toggleAiProviderStatus,
    saveCredentialPriority,
    saveAiProviderPriority,
    refresh: refresh,
    refreshAfterRecalculation,
    saveCredentialField,
    resetUsageIdentityStats: credentialPages.resetStats,
    refreshQuotaForCurrentCredentialPage: quotaRefreshTasks.refreshQuotaForCurrentCredentialPage,
    refreshQuotaForAuthIndex: quotaRefreshTasks.refreshQuotaForAuthIndex,
    resetQuotaForAuthIndex,
    refreshQuotaInspectionStatus: quotaInspection.refreshQuotaInspectionStatus,
    startQuotaInspection: quotaInspection.startQuotaInspection,
  }
}

export { quotaRefreshDisplayError }

export function buildCredentialQuotaStateMap(
  quotaStateByAuthIndex: Record<string, QuotaState>,
  resetStateByAuthIndex: Record<string, CredentialResetState> = {},
): Map<string, CredentialQuotaState> {
  const authIndexes = new Set([
    ...Object.keys(quotaStateByAuthIndex),
    ...Object.keys(resetStateByAuthIndex),
  ])
  return new Map(Array.from(authIndexes).map((authIndex) => {
    const state = quotaStateByAuthIndex[authIndex] ?? {}
    const resetState = resetStateByAuthIndex[authIndex] ?? {}
    return [authIndex, {
      quotaLoading: state.loading ?? false,
      quotaError: state.error,
      refreshStatus: state.refreshStatus,
      quotaResetting: resetState.quotaResetting ?? false,
    }]
  }))
}
