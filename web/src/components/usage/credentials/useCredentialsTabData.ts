import { useCallback, useMemo, useState } from 'react'
import {
  buildAiProviderCredentialRows,
  buildAuthFileCredentialRows,
  selectQuotaEligibleAuthIndexes,
  type AiProviderCredentialRow,
  type AuthFileCredentialRow,
} from './credentialViewModels'
import { useCredentialPages } from './useCredentialPages'
import { useQuotaCache } from './useQuotaCache'
import { useQuotaInspection } from './useQuotaInspection'
import { resetUsageQuota, type UsageIdentityPageSort } from '@/lib/api'
import i18n from '@/i18n'
import type { UsageIdentityTypeCount, UsageQuotaCheckResponse, UsageQuotaInspectionStatusResponse } from '@/lib/types'
import { quotaRefreshDisplayError, useQuotaRefreshTasks, type QuotaState } from './useQuotaRefreshTasks'
import type { CredentialProviderFilterKey } from './credentialProviderFilters'

type CredentialQuotaState = Pick<AuthFileCredentialRow, 'quotaLoading' | 'quotaError' | 'refreshStatus' | 'quotaResetting'>

interface CredentialResetState {
  quotaResetting?: boolean
}

interface UseCredentialsTabDataOptions {
  enabledAuthFiles: boolean
  enabledAiProviders: boolean
  quotaAutoRefreshEnabled: boolean
  onAuthRequired?: () => void
  onNotice?: (kind: 'success' | 'info' | 'error', message: string) => void
}

export interface CredentialsTabData {
  authFileRows: AuthFileCredentialRow[]
  aiProviderRows: AiProviderCredentialRow[]
  authFileTypeCounts: UsageIdentityTypeCount[]
  aiProviderTypeCounts: UsageIdentityTypeCount[]
  authFileTotal: number
  aiProviderTotal: number
  authFilePageSize: number
  aiProviderPageSize: number
  authFilePage: number
  aiProviderPage: number
  authFileTotalPages: number
  aiProviderTotalPages: number
  authFileActiveOnly: boolean
  authFileProviderFilter: CredentialProviderFilterKey
  aiProviderProviderFilter: CredentialProviderFilterKey
  authFileSort: UsageIdentityPageSort
  aiProviderSort: UsageIdentityPageSort
  setAuthFilePage: (page: number) => void
  setAiProviderPage: (page: number) => void
  setAuthFilePageSize: (pageSize: number) => void
  setAiProviderPageSize: (pageSize: number) => void
  setAuthFileActiveOnly: (activeOnly: boolean) => void
  setAuthFileProviderFilter: (filter: CredentialProviderFilterKey) => void
  setAiProviderProviderFilter: (filter: CredentialProviderFilterKey) => void
  setAuthFileSort: (sort: UsageIdentityPageSort) => void
  setAiProviderSort: (sort: UsageIdentityPageSort) => void
  loading: boolean
  error: string
  quotaRefreshing: boolean
  quotaRefreshError: string
  quotaInspectionStatus: UsageQuotaInspectionStatusResponse | null
  quotaInspectionLoading: boolean
  quotaInspectionStarting: boolean
  quotaInspectionError: string
  refresh: () => Promise<void>
  refreshQuotaForCurrentAuthFilePage: () => Promise<void>
  refreshQuotaForAuthIndex: (authIndex: string) => Promise<void>
  resetQuotaForAuthIndex: (authIndex: string) => Promise<void>
  refreshQuotaInspectionStatus: () => Promise<void>
  startQuotaInspection: () => Promise<void>
}

export function useCredentialsTabData({ enabledAuthFiles, enabledAiProviders, quotaAutoRefreshEnabled, onAuthRequired, onNotice }: UseCredentialsTabDataOptions): CredentialsTabData {
  const credentialPages = useCredentialPages({ enabledAuthFiles, enabledAiProviders, onAuthRequired })
  const currentAuthIndexes = useMemo(
    () => selectQuotaEligibleAuthIndexes(credentialPages.authFileIdentities),
    [credentialPages.authFileIdentities],
  )
  const { quotaResponseByAuthIndex, cachedQuotaStateByAuthIndex, setQuotaResponseByAuthIndex, refreshQuotaCache } = useQuotaCache({
    enabled: enabledAuthFiles,
    authIndexes: currentAuthIndexes,
    onAuthRequired,
  })
  const quotaRefreshTasks = useQuotaRefreshTasks({
    enabled: enabledAuthFiles,
    currentAuthIndexes,
    setQuotaResponseByAuthIndex,
    onAuthRequired,
  })
  const { refreshQuotaForAuthIndex } = quotaRefreshTasks
  const [quotaResetStateByAuthIndex, setQuotaResetStateByAuthIndex] = useState<Record<string, CredentialResetState>>({})
  const quotaInspection = useQuotaInspection({
    enabled: enabledAuthFiles,
    onAuthRequired,
    onInspectionCompleted: refreshQuotaCache,
  })

  const quotaResponsesByAuthIndex = useMemo(() => new Map(Object.entries(quotaResponseByAuthIndex)), [quotaResponseByAuthIndex])
  const quotaStates = useMemo(
    () => buildCredentialQuotaStateMap(cachedQuotaStateByAuthIndex, quotaRefreshTasks.quotaStateByAuthIndex, quotaResponseByAuthIndex, quotaResetStateByAuthIndex),
    [cachedQuotaStateByAuthIndex, quotaRefreshTasks.quotaStateByAuthIndex, quotaResponseByAuthIndex, quotaResetStateByAuthIndex],
  )

  const authFileRows = useMemo(
    () => buildAuthFileCredentialRows(credentialPages.authFileIdentities, quotaResponsesByAuthIndex, quotaStates),
    [credentialPages.authFileIdentities, quotaResponsesByAuthIndex, quotaStates],
  )
  const aiProviderRows = useMemo(
    () => buildAiProviderCredentialRows(credentialPages.aiProviderIdentities),
    [credentialPages.aiProviderIdentities],
  )
  const refreshCredentialPages = credentialPages.refresh
  const refresh = useCallback(async () => {
    await Promise.all([refreshCredentialPages(), refreshQuotaCache()])
  }, [refreshCredentialPages, refreshQuotaCache])

  const resetQuotaForAuthIndex = useCallback(async (authIndex: string) => {
    setQuotaResetStateByAuthIndex((current) => ({
      ...current,
      [authIndex]: { quotaResetting: true },
    }))
    try {
      const outcome = await runQuotaResetForAuthIndex(authIndex, {
        resetUsageQuota,
        refreshQuotaForAuthIndex,
      })
      setQuotaResetStateByAuthIndex((current) => ({
        ...current,
        [authIndex]: { quotaResetting: false },
      }))
      if (outcome.kind === 'error') {
        onNotice?.('error', outcome.message)
      }
    } catch {
      setQuotaResetStateByAuthIndex((current) => ({
        ...current,
        [authIndex]: { quotaResetting: false },
      }))
      onNotice?.('error', quotaResetDisplayError())
    }
  }, [onNotice, refreshQuotaForAuthIndex])

  return {
    authFileRows,
    aiProviderRows,
    authFileTypeCounts: credentialPages.authFileTypeCounts,
    aiProviderTypeCounts: credentialPages.aiProviderTypeCounts,
    authFileTotal: credentialPages.authFileTotal,
    aiProviderTotal: credentialPages.aiProviderTotal,
    authFilePageSize: credentialPages.authFilePageSize,
    aiProviderPageSize: credentialPages.aiProviderPageSize,
    authFilePage: credentialPages.authFilePage,
    aiProviderPage: credentialPages.aiProviderPage,
    authFileTotalPages: credentialPages.authFileTotalPages,
    aiProviderTotalPages: credentialPages.aiProviderTotalPages,
    authFileActiveOnly: credentialPages.authFileActiveOnly,
    authFileProviderFilter: credentialPages.authFileProviderFilter,
    aiProviderProviderFilter: credentialPages.aiProviderProviderFilter,
    authFileSort: credentialPages.authFileSort,
    aiProviderSort: credentialPages.aiProviderSort,
    setAuthFilePage: credentialPages.setAuthFilePage,
    setAiProviderPage: credentialPages.setAiProviderPage,
    setAuthFilePageSize: credentialPages.setAuthFilePageSize,
    setAiProviderPageSize: credentialPages.setAiProviderPageSize,
    setAuthFileActiveOnly: credentialPages.setAuthFileActiveOnly,
    setAuthFileProviderFilter: credentialPages.setAuthFileProviderFilter,
    setAiProviderProviderFilter: credentialPages.setAiProviderProviderFilter,
    setAuthFileSort: credentialPages.setAuthFileSort,
    setAiProviderSort: credentialPages.setAiProviderSort,
    loading: credentialPages.loading,
    error: credentialPages.error,
    quotaRefreshing: quotaRefreshTasks.quotaRefreshing,
    quotaRefreshError: quotaRefreshTasks.quotaRefreshError,
    quotaInspectionStatus: quotaInspection.quotaInspectionStatus,
    quotaInspectionLoading: quotaInspection.quotaInspectionLoading,
    quotaInspectionStarting: quotaInspection.quotaInspectionStarting,
    quotaInspectionError: quotaInspection.quotaInspectionError,
    refresh: refresh,
    refreshQuotaForCurrentAuthFilePage: quotaRefreshTasks.refreshQuotaForCurrentAuthFilePage,
    refreshQuotaForAuthIndex: quotaRefreshTasks.refreshQuotaForAuthIndex,
    resetQuotaForAuthIndex,
    refreshQuotaInspectionStatus: quotaInspection.refreshQuotaInspectionStatus,
    startQuotaInspection: quotaAutoRefreshEnabled ? async () => undefined : quotaInspection.startQuotaInspection,
  }
}

export { quotaRefreshDisplayError }

export type QuotaResetOutcome =
  | { kind: 'success' }
  | { kind: 'error'; message: string }

export async function runQuotaResetForAuthIndex(
  authIndex: string,
  deps: {
    resetUsageQuota: (authIndex: string) => Promise<unknown>
    refreshQuotaForAuthIndex: (authIndex: string) => Promise<void>
  },
): Promise<QuotaResetOutcome> {
  try {
    // reset 只负责消费官方次数；失败时不写行内限额缓存，也不触发刷新任务。
    await deps.resetUsageQuota(authIndex)
  } catch {
    return {
      kind: 'error',
      message: quotaResetDisplayError(),
    }
  }

  try {
    // reset 成功后复用现有单行刷新，让缓存继续以官方刷新结果为准；刷新失败走原有行内错误链路。
    await deps.refreshQuotaForAuthIndex(authIndex)
  } catch {
    // reset 已成功消费官方次数，后续刷新失败不影响本次 reset 的成功提示。
  }
  return { kind: 'success' }
}

export function quotaResetDisplayError(): string {
  return i18n.t('usage_stats.credentials_quota_reset_failed', { defaultValue: 'Quota reset failed. Please try again later.' })
}

export function buildCredentialQuotaStateMap(
  cachedQuotaStateByAuthIndex: Record<string, QuotaState>,
  quotaStateByAuthIndex: Record<string, QuotaState>,
  quotaResponseByAuthIndex: Record<string, UsageQuotaCheckResponse>,
  resetStateByAuthIndex: Record<string, CredentialResetState> = {},
): Map<string, CredentialQuotaState> {
  const mergedStates = { ...cachedQuotaStateByAuthIndex, ...quotaStateByAuthIndex }
  const authIndexes = new Set([
    ...Object.keys(mergedStates),
    ...Object.keys(resetStateByAuthIndex),
  ])
  return new Map(Array.from(authIndexes).map((authIndex) => {
    const state = mergedStates[authIndex] ?? {}
    const resetState = resetStateByAuthIndex[authIndex] ?? {}
    const hasCachedQuota = Object.prototype.hasOwnProperty.call(quotaResponseByAuthIndex, authIndex)
    const staleFailedState = hasCachedQuota && state.refreshStatus === 'failed'
    return [authIndex, {
      quotaLoading: state.loading ?? false,
      quotaError: staleFailedState ? undefined : state.error,
      refreshStatus: staleFailedState ? undefined : state.refreshStatus,
      quotaResetting: resetState.quotaResetting ?? false,
    }]
  }))
}
