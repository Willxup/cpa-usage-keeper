import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  buildAiProviderCredentialRows,
  buildAuthFileCredentialRows,
  selectQuotaEligibleAuthIndexes,
  type AiProviderCredentialRow,
  type CredentialEditChange,
  type AuthFileCredentialRow,
} from './credentialViewModels'
import { useCredentialPages } from './useCredentialPages'
import { useQuotaCache } from './useQuotaCache'
import { useQuotaInspection } from './useQuotaInspection'
import { ApiError, resetUsageQuota, setCredentialDisabled, setCredentialPriority, updateUsageIdentityAlias, type CredentialStatusKind, type UsageIdentityPageSort } from '@/lib/api'
import i18n from '@/i18n'
import type { UsageIdentity, UsageIdentityTypeCount, UsageQuotaCheckResponse, UsageQuotaInspectionStatusResponse, UsageQuotaResetResponse } from '@/lib/types'
import { quotaRefreshDisplayError, useQuotaRefreshTasks, type QuotaState } from './useQuotaRefreshTasks'
import type { CredentialProviderFilterKey } from './credentialProviderFilters'

type CredentialQuotaState = Pick<AuthFileCredentialRow, 'quotaLoading' | 'quotaError' | 'refreshStatus' | 'quotaResetting'>

interface CredentialResetState {
  quotaResetting?: boolean
}

export interface CredentialStatusFailureHandling {
  /** 401 需要页面重新登录，其它状态只提示。 */
  authRequired: boolean
  /** 顶层提示使用的 i18n key。 */
  noticeKey: string
  /** 只有列表确实过期（404）才值得重新拉取；409 是凭证本身不可单独操作。 */
  refresh: boolean
}

/**
 * 把凭证开关失败映射为提示与刷新决策。
 * 409 分两种来源：认证文件的插件多账号子行，以及不支持启停的供应商类型。
 */
export function resolveCredentialStatusFailure(kind: CredentialStatusKind, error: unknown): CredentialStatusFailureHandling {
  if (error instanceof ApiError && error.status === 401) {
    return { authRequired: true, noticeKey: 'usage_stats.credentials_status_update_failed', refresh: false }
  }
  if (error instanceof ApiError && error.status === 404) {
    return { authRequired: false, noticeKey: 'usage_stats.credentials_status_stale_target', refresh: true }
  }
  if (error instanceof ApiError && error.status === 409) {
    return {
      authRequired: false,
      noticeKey: kind === 'auth-file' ? 'usage_stats.credentials_status_conflict_auth_file' : 'usage_stats.credentials_status_conflict_ai_provider',
      refresh: false,
    }
  }
  return { authRequired: false, noticeKey: 'usage_stats.credentials_status_update_failed', refresh: false }
}

interface UseCredentialsTabDataOptions {
  enabledAuthFiles: boolean
  enabledAiProviders: boolean
  onAuthRequired?: () => void
  onNotice?: (kind: 'success' | 'info' | 'error', message: string) => void
  onPrioritySaved?: () => void
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
  toggleAuthFileStatus: (identityId: string, authIndex: string, disabled: boolean) => void
  toggleAiProviderStatus: (identityId: string, authIndex: string, disabled: boolean) => void
  saveAuthFilePriority: (identityId: string, authIndex: string, priority: number) => Promise<void>
  saveAiProviderPriority: (identityId: string, authIndex: string, priority: number) => Promise<void>
  refresh: () => Promise<void>
  saveCredentialField: (kind: CredentialStatusKind, id: string, authIndex: string, change: CredentialEditChange) => Promise<void>
  resetUsageIdentityStats: (id: string) => Promise<UsageIdentity>
  refreshQuotaForCurrentAuthFilePage: () => Promise<void>
  refreshQuotaForAuthIndex: (authIndex: string) => Promise<void>
  resetQuotaForAuthIndex: (authIndex: string) => Promise<void>
  refreshQuotaInspectionStatus: () => Promise<void>
  startQuotaInspection: () => Promise<void>
}

export function useCredentialsTabData({ enabledAuthFiles, enabledAiProviders, onAuthRequired, onNotice, onPrioritySaved }: UseCredentialsTabDataOptions): CredentialsTabData {
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
  const [credentialStatusPending, setCredentialStatusPending] = useState<Record<string, boolean>>({})
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
  // 开关请求返回时页面参数可能已经变化，ref 始终指向最新 refresh，避免用旧页码或旧筛选覆盖用户当前视图。
  const refreshCredentialPagesRef = useRef(refreshCredentialPages)
  useEffect(() => {
    refreshCredentialPagesRef.current = refreshCredentialPages
  }, [refreshCredentialPages])
  const refresh = useCallback(async () => {
    await Promise.all([refreshCredentialPages(), refreshQuotaCache()])
  }, [refreshCredentialPages, refreshQuotaCache])

  // 开关按钮改用 aria-disabled 后不再由浏览器拦截重复点击，这里用 ref 做与渲染时序无关的兜底。
  const credentialStatusInFlightRef = useRef<Set<string>>(new Set())
  const credentialStatusPendingIdentityIds = useMemo(
    () => new Set(Object.keys(credentialStatusPending).filter((identityId) => credentialStatusPending[identityId])),
    [credentialStatusPending],
  )

  const toggleCredentialStatus = useCallback(async (kind: CredentialStatusKind, identityId: string, authIndex: string, disabled: boolean) => {
    // Keeper identity id 是全局唯一的本地主键；auth_index 只用于后端调用，不参与两个列表的 pending 隔离。
    const pendingIdentityId = identityId || authIndex
    if (credentialStatusInFlightRef.current.has(pendingIdentityId)) {
      return
    }
    credentialStatusInFlightRef.current.add(pendingIdentityId)
    setCredentialStatusPending((current) => ({ ...current, [pendingIdentityId]: true }))
    try {
      await setCredentialDisabled(kind, authIndex, disabled)
      // 上游成功后刷新列表，让 enabled only 过滤与图标状态都来自后端结果。
      await refreshCredentialPagesRef.current()
      onNotice?.('success', i18n.t(disabled ? 'usage_stats.credentials_status_disable_success' : 'usage_stats.credentials_status_enable_success'))
    } catch (error) {
      const failure = resolveCredentialStatusFailure(kind, error)
      if (failure.authRequired) {
        onAuthRequired?.()
      }
      onNotice?.('error', i18n.t(failure.noticeKey))
      // 404 说明本地列表已经过期，用最新筛选与页码重新拉取，让用户看到真实状态。
      if (failure.refresh) {
        await refreshCredentialPagesRef.current()
      }
    } finally {
      credentialStatusInFlightRef.current.delete(pendingIdentityId)
      setCredentialStatusPending((current) => {
        if (!current[pendingIdentityId]) {
          return current
        }
        const next = { ...current }
        delete next[pendingIdentityId]
        return next
      })
    }
    // 刷新只经由 refreshCredentialPagesRef，避免把点击时的旧闭包固化进依赖列表。
  }, [onAuthRequired, onNotice])

  const toggleAuthFileStatus = useCallback((identityId: string, authIndex: string, disabled: boolean) => {
    void toggleCredentialStatus('auth-file', identityId, authIndex, disabled)
  }, [toggleCredentialStatus])

  const toggleAiProviderStatus = useCallback((identityId: string, authIndex: string, disabled: boolean) => {
    void toggleCredentialStatus('ai-provider', identityId, authIndex, disabled)
  }, [toggleCredentialStatus])

  const saveCredentialPriority = useCallback(async (kind: CredentialStatusKind, _identityId: string, authIndex: string, priority: number) => {
    try {
      await setCredentialPriority(kind, authIndex, priority)
      // 重新读取当前筛选和排序；OpenAI provider 的其它 key 也通过服务端结果对齐。
      await refreshCredentialPagesRef.current()
      onPrioritySaved?.()
      onNotice?.('success', i18n.t('usage_stats.credentials_priority_save_success'))
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) onAuthRequired?.()
      if (error instanceof ApiError && error.status === 404) {
        await refreshCredentialPagesRef.current()
      }
      const key = error instanceof ApiError && error.status === 404
        ? 'usage_stats.credentials_priority_stale_target'
        : error instanceof ApiError && error.status === 409 && kind === 'auth-file'
          ? 'usage_stats.credentials_priority_conflict_auth_file'
          : 'usage_stats.credentials_priority_save_failed'
      onNotice?.('error', i18n.t(key))
      throw error
    }
  }, [onAuthRequired, onNotice, onPrioritySaved])

  const saveAuthFilePriority = useCallback((identityId: string, authIndex: string, priority: number) =>
    saveCredentialPriority('auth-file', identityId, authIndex, priority), [saveCredentialPriority])
  const saveAiProviderPriority = useCallback((identityId: string, authIndex: string, priority: number) =>
    saveCredentialPriority('ai-provider', identityId, authIndex, priority), [saveCredentialPriority])

  const saveCredentialField = useCallback(async (kind: CredentialStatusKind, id: string, authIndex: string, change: CredentialEditChange) => {
    try {
      if (change.field === 'alias') {
        const updated = await updateUsageIdentityAlias(id, change.value)
        credentialPages.replaceUsageIdentity(updated)
      } else if (change.field === 'priority') {
        await setCredentialPriority(kind, authIndex, change.value)
      } else {
        await setCredentialDisabled(kind, authIndex, change.value)
      }
      // 每项成功立即对齐列表和详情；弹框在页面层保留，不随当前行卸载。
      await refreshCredentialPagesRef.current()
      onPrioritySaved?.()
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) onAuthRequired?.()
      if (error instanceof ApiError && error.status === 404) await refreshCredentialPagesRef.current()
      throw error
    }
  }, [credentialPages, onAuthRequired, onPrioritySaved])

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
      } else if (outcome.kind === 'warning') {
        onNotice?.('info', outcome.message)
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
    aiProviderActiveOnly: credentialPages.aiProviderActiveOnly,
    authFileProviderFilter: credentialPages.authFileProviderFilter,
    aiProviderProviderFilter: credentialPages.aiProviderProviderFilter,
    authFileSort: credentialPages.authFileSort,
    aiProviderSort: credentialPages.aiProviderSort,
    setAuthFilePage: credentialPages.setAuthFilePage,
    setAiProviderPage: credentialPages.setAiProviderPage,
    setAuthFilePageSize: credentialPages.setAuthFilePageSize,
    setAiProviderPageSize: credentialPages.setAiProviderPageSize,
    setAuthFileActiveOnly: credentialPages.setAuthFileActiveOnly,
    setAiProviderActiveOnly: credentialPages.setAiProviderActiveOnly,
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
    credentialStatusPendingIdentityIds,
    toggleAuthFileStatus,
    toggleAiProviderStatus,
    saveAuthFilePriority,
    saveAiProviderPriority,
    refresh: refresh,
    saveCredentialField,
    resetUsageIdentityStats: credentialPages.resetStats,
    refreshQuotaForCurrentAuthFilePage: quotaRefreshTasks.refreshQuotaForCurrentAuthFilePage,
    refreshQuotaForAuthIndex: quotaRefreshTasks.refreshQuotaForAuthIndex,
    resetQuotaForAuthIndex,
    refreshQuotaInspectionStatus: quotaInspection.refreshQuotaInspectionStatus,
    startQuotaInspection: quotaInspection.startQuotaInspection,
  }
}

export { quotaRefreshDisplayError }

export type QuotaResetOutcome =
  | { kind: 'success' }
  | { kind: 'warning'; message: string }
  | { kind: 'error'; message: string }

export async function runQuotaResetForAuthIndex(
  authIndex: string,
  deps: {
    resetUsageQuota: (authIndex: string) => Promise<UsageQuotaResetResponse>
    refreshQuotaForAuthIndex: (authIndex: string) => Promise<void>
  },
): Promise<QuotaResetOutcome> {
  let result: UsageQuotaResetResponse
  try {
    // 后端在官方重置后恢复 CPA 路由；只有官方重置失败才中止额度刷新。
    result = await deps.resetUsageQuota(authIndex)
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
  if (result.recoveryFailed) {
    return { kind: 'warning', message: i18n.t('usage_stats.credentials_quota_reset_recovery_failed') }
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
