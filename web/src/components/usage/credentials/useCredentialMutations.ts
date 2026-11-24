import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ApiError, setCredentialDisabled, setCredentialPriority, updateUsageIdentityAlias, type CredentialStatusKind } from '@/lib/api'
import type { UsageIdentity } from '@/lib/types'
import i18n from '@/i18n'
import type { CredentialEditChange } from './credentialViewModels'

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
 * 409 分两种来源：凭证的插件多账号子行，以及不支持启停的供应商类型。
 */
export function resolveCredentialStatusFailure(kind: CredentialStatusKind, error: unknown): CredentialStatusFailureHandling {
  if (error instanceof ApiError && error.status === 401) {
    return { authRequired: true, noticeKey: 'usage_stats.credentials_status_update_failed', refresh: false }
  }
  if (error instanceof ApiError && error.status === 404) {
    return { authRequired: false, noticeKey: 'usage_stats.credentials_status_stale_target', refresh: true }
  }
  if (error instanceof ApiError && error.status === 412) {
    return { authRequired: false, noticeKey: 'usage_stats.credentials_config_changed', refresh: false }
  }
  if (error instanceof ApiError && error.status === 409) {
    return {
      authRequired: false,
      noticeKey: kind === 'credential' ? 'usage_stats.credentials_status_conflict_credential' : 'usage_stats.credentials_status_conflict_ai_provider',
      refresh: false,
    }
  }
  return { authRequired: false, noticeKey: 'usage_stats.credentials_status_update_failed', refresh: false }
}

export function useCredentialMutations({ refreshCredentialPages, replaceUsageIdentity, onAuthRequired, onNotice, onPrioritySaved }: {
  refreshCredentialPages: () => Promise<void>
  replaceUsageIdentity: (identity: UsageIdentity) => void
  onAuthRequired?: () => void
  onNotice?: (kind: 'success' | 'info' | 'error', message: string) => void
  onPrioritySaved?: () => void
}) {
  const [credentialStatusPending, setCredentialStatusPending] = useState<Record<string, boolean>>({})
  // 开关请求返回时页面参数可能已经变化，ref 始终指向最新 refresh，避免用旧页码或旧筛选覆盖用户当前视图。
  const refreshCredentialPagesRef = useRef(refreshCredentialPages)
  useEffect(() => {
    refreshCredentialPagesRef.current = refreshCredentialPages
  }, [refreshCredentialPages])
  // 开关按钮改用 aria-disabled 后不再由浏览器拦截重复点击，这里用 ref 做与渲染时序无关的兜底。
  const credentialStatusInFlightRef = useRef<Set<string>>(new Set())
  const credentialStatusPendingIdentityIds = useMemo(
    () => new Set(Object.keys(credentialStatusPending).filter((identityId) => credentialStatusPending[identityId])),
    [credentialStatusPending],
  )

  const toggleStatusByKind = useCallback(async (kind: CredentialStatusKind, identityId: string, authIndex: string, disabled: boolean) => {
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

  const toggleCredentialStatus = useCallback((identityId: string, authIndex: string, disabled: boolean) => {
    void toggleStatusByKind('credential', identityId, authIndex, disabled)
  }, [toggleStatusByKind])

  const toggleAiProviderStatus = useCallback((identityId: string, authIndex: string, disabled: boolean) => {
    void toggleStatusByKind('ai-provider', identityId, authIndex, disabled)
  }, [toggleStatusByKind])

  const savePriorityByKind = useCallback(async (kind: CredentialStatusKind, _identityId: string, authIndex: string, priority: number) => {
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
        : error instanceof ApiError && error.status === 412
          ? 'usage_stats.credentials_config_changed'
          : error instanceof ApiError && error.status === 409 && kind === 'credential'
            ? 'usage_stats.credentials_priority_conflict_credential'
            : 'usage_stats.credentials_priority_save_failed'
      onNotice?.('error', i18n.t(key))
      throw error
    }
  }, [onAuthRequired, onNotice, onPrioritySaved])

  const saveCredentialPriority = useCallback((identityId: string, authIndex: string, priority: number) =>
    savePriorityByKind('credential', identityId, authIndex, priority), [savePriorityByKind])
  const saveAiProviderPriority = useCallback((identityId: string, authIndex: string, priority: number) =>
    savePriorityByKind('ai-provider', identityId, authIndex, priority), [savePriorityByKind])

  const saveCredentialField = useCallback(async (kind: CredentialStatusKind, id: string, authIndex: string, change: CredentialEditChange) => {
    try {
      if (change.field === 'alias') {
        const updated = await updateUsageIdentityAlias(id, change.value)
        replaceUsageIdentity(updated)
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
  }, [replaceUsageIdentity, onAuthRequired, onPrioritySaved])


  return { credentialStatusPendingIdentityIds, toggleCredentialStatus, toggleAiProviderStatus,
    saveCredentialPriority, saveAiProviderPriority, saveCredentialField }
}
