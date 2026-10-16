import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ApiError, fetchUsageQuotaCache } from '@/lib/api'
import type { UsageQuotaCheckResponse } from '@/lib/types'
import { quotaRefreshDisplayError, type QuotaState } from './useQuotaRefreshTasks'

export const QUOTA_CACHE_REFRESH_INTERVAL_MS = 60 * 1000

export const buildQuotaCacheAuthIndexesKey = (authIndexes: string[]) => JSON.stringify(authIndexes)

interface UseQuotaCacheOptions {
  enabled: boolean
  authIndexes: string[]
  onAuthRequired?: () => void
}

export interface QuotaCacheState {
  quotaResponseByAuthIndex: Record<string, UsageQuotaCheckResponse>
  quotaStateByAuthIndex: Record<string, QuotaState>
  applyRefreshUpdates: (states: Record<string, QuotaState>, quotas?: Record<string, UsageQuotaCheckResponse>) => void
  refreshQuotaCache: () => Promise<void>
}

export function useQuotaCache({ enabled, authIndexes, onAuthRequired }: UseQuotaCacheOptions): QuotaCacheState {
  const [snapshot, setSnapshot] = useState<QuotaSnapshot>({ quotas: {}, states: {} })
  // 每个凭证单独记录手动任务更新；缓存请求只可写回读取期间未变化且未在刷新中的条目。
  const refreshRevisions = useRef<Record<string, number>>({})
  const workingAuthIndexes = useRef(new Set<string>())
  const applyRefreshUpdates = useCallback((states: Record<string, QuotaState>, quotas: Record<string, UsageQuotaCheckResponse> = {}) => {
    for (const [authIndex, state] of Object.entries(states)) {
      refreshRevisions.current[authIndex] = (refreshRevisions.current[authIndex] ?? 0) + 1
      if (state.refreshStatus === 'queued' || state.refreshStatus === 'running') {
        workingAuthIndexes.current.add(authIndex)
      } else {
        workingAuthIndexes.current.delete(authIndex)
      }
    }
    setSnapshot((current) => applyQuotaUpdates(current, states, quotas, 'refresh'))
  }, [])
  const requestControllerRef = useRef<AbortController | null>(null)

  const authIndexesKey = buildQuotaCacheAuthIndexesKey(authIndexes)
  const stableAuthIndexes = useMemo(() => JSON.parse(authIndexesKey) as string[], [authIndexesKey])
  const previousAuthIndexesRef = useRef(stableAuthIndexes)

  const refreshQuotaCache = useCallback(async () => {
    if (previousAuthIndexesRef.current !== stableAuthIndexes) {
      const activeAuthIndexes = new Set(stableAuthIndexes)
      const departedAuthIndexes = previousAuthIndexesRef.current.filter((authIndex) => !activeAuthIndexes.has(authIndex))
      previousAuthIndexesRef.current = stableAuthIndexes
      // 离页只清理缓存来源的状态；手动错误、进行中的任务和成功额度快照继续保留。
      if (departedAuthIndexes.length > 0) {
        setSnapshot((current) => {
          let states = current.states
          for (const authIndex of departedAuthIndexes) {
            if (states[authIndex]?.source !== 'cache') {
              continue
            }
            if (states === current.states) {
              states = { ...current.states }
            }
            delete states[authIndex]
          }
          return states === current.states ? current : { ...current, states }
        })
      }
    }
    if (!enabled) {
      requestControllerRef.current?.abort()
      requestControllerRef.current = null
      return
    }

    requestControllerRef.current?.abort()
    if (stableAuthIndexes.length === 0) {
      requestControllerRef.current = null
      return
    }

    const controller = new AbortController()
    requestControllerRef.current = controller
    const revisions = new Map(stableAuthIndexes.map((authIndex) => [authIndex, refreshRevisions.current[authIndex]]))
    try {
      // 缓存接口不会刷新限额；当前页有多少 auth_index 就查询多少缓存。
      const response = await fetchUsageQuotaCache(stableAuthIndexes, controller.signal)
      if (controller.signal.aborted || requestControllerRef.current !== controller) {
        return
      }
      const items = new Map(response.items.map((item) => [item.auth_index, item]))
      const states: Record<string, QuotaState> = {}
      const quotas: Record<string, UsageQuotaCheckResponse> = {}
      for (const authIndex of stableAuthIndexes) {
        if (workingAuthIndexes.current.has(authIndex) || revisions.get(authIndex) !== refreshRevisions.current[authIndex]) {
          continue
        }
        const item = items.get(authIndex)
        // 缓存成功和失败都更新同一份状态，巡检成功无需再压制另一份手动错误。
        states[authIndex] = item ? {
          refreshStatus: item.status,
          error: item.status === 'failed' ? quotaRefreshDisplayError(item.error) : undefined,
        } : {}
        if (item?.status === 'completed' && item.quota) {
          quotas[authIndex] = item.quota
        }
      }
      setSnapshot((current) => applyQuotaUpdates(current, states, quotas, 'cache'))
    } catch (nextError) {
      if (controller.signal.aborted) {
        return
      }
      if (nextError instanceof ApiError && nextError.status === 401) {
        onAuthRequired?.()
      }
    } finally {
      if (requestControllerRef.current === controller) {
        requestControllerRef.current = null
      }
    }
  }, [enabled, onAuthRequired, stableAuthIndexes])

  useEffect(() => {
    void refreshQuotaCache()
    if (!enabled) {
      return
    }
    const intervalID = window.setInterval(refreshQuotaCache, QUOTA_CACHE_REFRESH_INTERVAL_MS)
    return () => {
      window.clearInterval(intervalID)
      requestControllerRef.current?.abort()
      requestControllerRef.current = null
    }
  }, [enabled, refreshQuotaCache])

  return {
    quotaResponseByAuthIndex: snapshot.quotas,
    quotaStateByAuthIndex: snapshot.states,
    applyRefreshUpdates,
    refreshQuotaCache,
  }
}

interface QuotaSnapshot {
  quotas: Record<string, UsageQuotaCheckResponse>
  states: Record<string, QuotaState & { source: 'refresh' | 'cache' }>
}

function applyQuotaUpdates(current: QuotaSnapshot, states: Record<string, QuotaState>, quotas: Record<string, UsageQuotaCheckResponse>, source: 'refresh' | 'cache'): QuotaSnapshot {
  let next = current
  for (const [authIndex, update] of Object.entries(states)) {
    const previous = current.states[authIndex]
    // 缓存不返回瞬时失败，缺项不能清除本页手动错误；缓存自身的错误仍按缺项过期。
    const state = source === 'cache' && !update.refreshStatus && previous?.source === 'refresh' && previous.refreshStatus === 'failed'
      ? previous
      : { ...update, source }
    const working = state.refreshStatus === 'queued' || state.refreshStatus === 'running'
    const quota = quotas[authIndex] ?? (working ? current.quotas[authIndex] : undefined)
    if (previous?.loading === state.loading && previous?.error === state.error && previous?.refreshStatus === state.refreshStatus && previous?.source === state.source && current.quotas[authIndex] === quota) {
      continue
    }
    if (next === current) {
      next = { quotas: { ...current.quotas }, states: { ...current.states } }
    }
    next.states[authIndex] = state
    // 刷新中保留快照供结束后替换；明确失败或缓存消失时移除旧额度。
    if (quota) {
      next.quotas[authIndex] = quota
    } else {
      delete next.quotas[authIndex]
    }
  }
  return next
}
