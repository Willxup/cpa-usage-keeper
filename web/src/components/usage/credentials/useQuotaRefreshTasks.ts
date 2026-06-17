import { useCallback, useEffect, useMemo, useState, type Dispatch, type SetStateAction } from 'react'
import { ApiError, fetchUsageQuotaRefreshTask, refreshUsageQuotas } from '@/lib/api'
import i18n from '@/i18n'
import type { UsageQuotaCheckResponse, UsageQuotaRefreshResponse } from '@/lib/types'

export interface QuotaState {
  loading?: boolean
  error?: string
  refreshStatus?: 'queued' | 'running' | 'completed' | 'failed'
}

export interface PendingRefreshTask {
  authIndex: string
  source: 'batch' | 'row'
}

interface UseQuotaRefreshTasksOptions {
  enabled: boolean
  currentAuthIndexes: string[]
  setQuotaResponseByAuthIndex: Dispatch<SetStateAction<Record<string, UsageQuotaCheckResponse>>>
  onAuthRequired?: () => void
}

export interface QuotaRefreshTasksState {
  quotaStateByAuthIndex: Record<string, QuotaState>
  quotaRefreshing: boolean
  quotaRefreshError: string
  refreshQuotaForCurrentAuthFilePage: () => Promise<void>
  refreshQuotaForAuthIndex: (authIndex: string) => Promise<void>
}

export function useQuotaRefreshTasks({ enabled, currentAuthIndexes, setQuotaResponseByAuthIndex, onAuthRequired }: UseQuotaRefreshTasksOptions): QuotaRefreshTasksState {
  const [quotaStateByAuthIndex, setQuotaStateByAuthIndex] = useState<Record<string, QuotaState>>({})
  const [pendingRefreshTasks, setPendingRefreshTasks] = useState<PendingRefreshTask[]>([])
  const [batchRefreshSubmitting, setBatchRefreshSubmitting] = useState(false)
  const [quotaRefreshError, setQuotaRefreshError] = useState('')
  const quotaRefreshing = useMemo(
    // 右上角批量按钮只跟批量任务相关；单行刷新不占用全局刷新状态。
    () => batchRefreshSubmitting || pendingRefreshTasks.some((task) => task.source === 'batch'),
    [batchRefreshSubmitting, pendingRefreshTasks],
  )

  useEffect(() => {
    if (!enabled || pendingRefreshTasks.length === 0) {
      return
    }
    let cancelled = false
    let timer: number | undefined
    const controller = new AbortController()
    const poll = async () => {
      // 一轮轮询内同时查询所有未完成 task，再统一合并状态和 quota 缓存。
      const settledAuthIndexes = new Set<string>()
      const stateUpdates: Record<string, QuotaState> = {}
      const quotaResponseUpdates: Record<string, UsageQuotaCheckResponse> = {}

      await Promise.all(pendingRefreshTasks.map(async (task) => {
        try {
          const response = await fetchUsageQuotaRefreshTask(task.authIndex, controller.signal)
          if (cancelled) {
            return
          }
          stateUpdates[task.authIndex] = {
            refreshStatus: response.status,
            error: response.status === 'failed' ? quotaRefreshDisplayError(response.error) : undefined,
          }
          if (response.status === 'completed' || response.status === 'failed') {
            settledAuthIndexes.add(task.authIndex)
          }
          if (response.status === 'completed' && response.quota) {
            quotaResponseUpdates[task.authIndex] = response.quota
          }
        } catch (nextError) {
          if (cancelled || controller.signal.aborted) {
            return
          }
          const errorUpdate = buildQuotaRefreshTaskErrorUpdate(task.authIndex, nextError, onAuthRequired)
          if (errorUpdate.settled) {
            settledAuthIndexes.add(task.authIndex)
          }
          stateUpdates[task.authIndex] = errorUpdate.stateUpdate
        }
      }))

      if (cancelled) {
        return
      }
      if (Object.keys(quotaResponseUpdates).length > 0) {
        // 已完成任务的 quota 直接写入缓存，行视图会自动用最新缓存重算。
        setQuotaResponseByAuthIndex((current) => ({ ...current, ...quotaResponseUpdates }))
      }
      if (Object.keys(stateUpdates).length > 0) {
        setQuotaStateByAuthIndex((current) => mergeQuotaStates(current, stateUpdates))
      }
      if (settledAuthIndexes.size > 0) {
        setPendingRefreshTasks((current) => current.filter((task) => !settledAuthIndexes.has(task.authIndex)))
      }
      // 当前轮完成后再延迟下一轮，避免请求慢时多个轮询批次重叠。
      timer = window.setTimeout(() => {
        void poll()
      }, 5_000)
    }

    void poll()

    return () => {
      cancelled = true
      controller.abort()
      if (timer !== undefined) {
        window.clearTimeout(timer)
      }
    }
  }, [enabled, onAuthRequired, pendingRefreshTasks, setQuotaResponseByAuthIndex])

  const startQuotaRefresh = useCallback(async (authIndexes: string[], source: PendingRefreshTask['source']) => {
    if (authIndexes.length === 0) {
      return
    }
    setQuotaRefreshError('')
    if (source === 'batch') {
      setBatchRefreshSubmitting(true)
    }
    try {
      const response = await refreshUsageQuotas(authIndexes)
      const submission = buildQuotaRefreshSubmissionUpdate(response, source)
      // 后端返回的是每个 auth_index 对应的独立 task，前端按 auth_index 去重保存。
      setPendingRefreshTasks((current) => {
        const nextByAuthIndex = new Map(current.map((task) => [task.authIndex, task]))
        for (const task of submission.pendingTasks) {
          nextByAuthIndex.set(task.authIndex, task)
        }
        return Array.from(nextByAuthIndex.values())
      })
      setQuotaStateByAuthIndex((current) => {
        return mergeQuotaStates(current, submission.stateUpdates)
      })
    } catch (nextError) {
      if (nextError instanceof ApiError && nextError.status === 401) {
        onAuthRequired?.()
        return
      }
      setQuotaRefreshError(quotaErrorMessage(nextError))
    } finally {
      if (source === 'batch') {
        setBatchRefreshSubmitting(false)
      }
    }
  }, [onAuthRequired])

  const refreshQuotaForCurrentAuthFilePage = useCallback(async () => {
    // 批量刷新只提交当前页且未在工作的条目，单行刷新中的任务不会重复入队。
    const refreshableAuthIndexes = currentAuthIndexes.filter((authIndex) => !isQuotaRefreshWorking(quotaStateByAuthIndex[authIndex]))
    await startQuotaRefresh(refreshableAuthIndexes, 'batch')
  }, [currentAuthIndexes, quotaStateByAuthIndex, startQuotaRefresh])

  const refreshQuotaForAuthIndex = useCallback(async (authIndex: string) => {
    if (isQuotaRefreshWorking(quotaStateByAuthIndex[authIndex])) {
      return
    }
    await startQuotaRefresh([authIndex], 'row')
  }, [quotaStateByAuthIndex, startQuotaRefresh])

  return {
    quotaStateByAuthIndex,
    quotaRefreshing,
    quotaRefreshError,
    refreshQuotaForCurrentAuthFilePage,
    refreshQuotaForAuthIndex,
  }
}

export function buildQuotaRefreshSubmissionUpdate(response: UsageQuotaRefreshResponse, source: PendingRefreshTask['source']): { pendingTasks: PendingRefreshTask[]; stateUpdates: Record<string, QuotaState> } {
  const pendingTasks: PendingRefreshTask[] = []
  const stateUpdates: Record<string, QuotaState> = {}
  for (const task of response.tasks) {
    // 新建成功的 task 进入轮询列表，后续由 /quota/refresh/:auth_index 收敛到 completed/failed。
    pendingTasks.push({ authIndex: task.authIndex, source })
    stateUpdates[task.authIndex] = {
      refreshStatus: 'queued',
      error: undefined,
    }
  }
  for (const rejected of response.rejected ?? []) {
    if (rejected.error === 'duplicate') {
      // duplicate 表示后端已有同 auth_index 的 queued/running 任务，前端继续轮询这条现有任务即可。
      pendingTasks.push({ authIndex: rejected.authIndex, source })
      stateUpdates[rejected.authIndex] = {
        refreshStatus: 'queued',
        error: undefined,
      }
      continue
    }
    // 其它拒绝是确定性业务错误，不会有后台任务产出结果，直接展示失败原因。
    stateUpdates[rejected.authIndex] = {
      refreshStatus: 'failed',
      error: quotaRefreshDisplayError(rejected.error),
    }
  }
  return { pendingTasks, stateUpdates }
}

export function buildQuotaRefreshTaskErrorUpdate(authIndex: string, error: unknown, onAuthRequired?: () => void): { authIndex: string; settled: boolean; stateUpdate: QuotaState } {
  if (error instanceof ApiError && error.status === 401) {
    // 认证失效时结束当前行轮询，避免页面停留在 queued/running 假状态。
    onAuthRequired?.()
    return {
      authIndex,
      settled: true,
      stateUpdate: {
        refreshStatus: 'failed',
        error: i18n.t('usage_stats.credentials_refresh_error_unauthorized', { defaultValue: 'Please sign in again to refresh quota.' }),
      },
    }
  }
  return {
    authIndex,
    settled: true,
    stateUpdate: {
      refreshStatus: 'failed',
      error: quotaErrorMessage(error),
    },
  }
}

function isQuotaRefreshWorking(state: QuotaState | undefined): boolean {
  return state?.refreshStatus === 'queued' || state?.refreshStatus === 'running'
}

function mergeQuotaStates(current: Record<string, QuotaState>, updates: Record<string, QuotaState>): Record<string, QuotaState> {
  let changed = false
  const next = { ...current }
  for (const [authIndex, update] of Object.entries(updates)) {
    const previous = current[authIndex] ?? {}
    const merged = { ...previous, ...update }
    if (
      previous.loading !== merged.loading ||
      previous.error !== merged.error ||
      previous.refreshStatus !== merged.refreshStatus
    ) {
      next[authIndex] = merged
      changed = true
    }
  }
  return changed ? next : current
}

export function quotaRefreshDisplayError(error?: string): string {
  switch (error) {
    case 'duplicate':
      return i18n.t('usage_stats.credentials_refresh_error_duplicate', { defaultValue: 'Quota refresh is already running for this credential.' })
    case 'duplicate_request':
      return i18n.t('usage_stats.credentials_refresh_error_duplicate_request', { defaultValue: 'This credential was already included in the refresh request.' })
    case 'not_auth_file':
      return i18n.t('usage_stats.credentials_refresh_error_not_auth_file', { defaultValue: 'Quota refresh only supports local auth files.' })
    case 'unsupported':
      return i18n.t('usage_stats.credentials_refresh_error_unsupported', { defaultValue: 'Quota refresh is not supported for this credential type.' })
    case 'not_found':
      return i18n.t('usage_stats.credentials_refresh_error_not_found', { defaultValue: 'This credential is no longer available.' })
    case 'invalid':
      return i18n.t('usage_stats.credentials_refresh_error_invalid', { defaultValue: 'This credential cannot be refreshed.' })
  }
  return error || i18n.t('usage_stats.credentials_refresh_error_failed', { defaultValue: 'Quota refresh failed. Please try again later.' })
}

function quotaErrorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message
  }
  return 'Quota request failed'
}
