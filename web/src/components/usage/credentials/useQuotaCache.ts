import { useCallback, useEffect, useMemo, useRef, useState, type Dispatch, type SetStateAction } from 'react'
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
  cachedQuotaStateByAuthIndex: Record<string, QuotaState>
  setQuotaResponseByAuthIndex: Dispatch<SetStateAction<Record<string, UsageQuotaCheckResponse>>>
  refreshQuotaCache: () => Promise<void>
}

export function useQuotaCache({ enabled, authIndexes, onAuthRequired }: UseQuotaCacheOptions): QuotaCacheState {
  const [quotaResponseByAuthIndex, setQuotaResponseByAuthIndex] = useState<Record<string, UsageQuotaCheckResponse>>({})
  const [cachedQuotaStateByAuthIndex, setCachedQuotaStateByAuthIndex] = useState<Record<string, QuotaState>>({})
  const requestControllerRef = useRef<AbortController | null>(null)

  const authIndexesKey = buildQuotaCacheAuthIndexesKey(authIndexes)
  const stableAuthIndexes = useMemo(() => JSON.parse(authIndexesKey) as string[], [authIndexesKey])

  const refreshQuotaCache = useCallback(async () => {
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
    try {
      // 缓存接口不会刷新限额；当前页有多少 auth_index 就查询多少缓存。
      const response = await fetchUsageQuotaCache(stableAuthIndexes, controller.signal)
      if (controller.signal.aborted || requestControllerRef.current !== controller) {
        return
      }
      const returnedAuthIndexes = new Set(response.items.map((item) => item.auth_index))
      setQuotaResponseByAuthIndex((current) => {
        let changed = false
        const next = { ...current }
        // cache 接口现在同时返回成功 quota 和可恢复错误；只有 completed 才写入 quota 数据。
        for (const item of response.items) {
          if (item.status !== 'completed' || !item.quota) {
            continue
          }
          if (next[item.auth_index] !== item.quota) {
            next[item.auth_index] = item.quota
            changed = true
          }
        }
        for (const authIndex of stableAuthIndexes) {
          if (!returnedAuthIndexes.has(authIndex) && next[authIndex] !== undefined) {
            delete next[authIndex]
            changed = true
          }
        }
        return changed ? next : current
      })
      setCachedQuotaStateByAuthIndex(() => {
        const next: Record<string, QuotaState> = {}
        // failed 缓存项只来自后端配置允许恢复展示的 HTTP 错误，刷新页面后要恢复到行错误状态。
        for (const item of response.items) {
          if (item.status !== 'failed') {
            continue
          }
          next[item.auth_index] = {
            refreshStatus: 'failed',
            error: quotaRefreshDisplayError(item.error),
          }
        }
        return next
      })
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
    if (!enabled) {
      requestControllerRef.current?.abort()
      requestControllerRef.current = null
      return
    }
    void refreshQuotaCache()
    const intervalID = window.setInterval(refreshQuotaCache, QUOTA_CACHE_REFRESH_INTERVAL_MS)
    return () => {
      window.clearInterval(intervalID)
      requestControllerRef.current?.abort()
      requestControllerRef.current = null
    }
  }, [enabled, refreshQuotaCache])

  return { quotaResponseByAuthIndex, cachedQuotaStateByAuthIndex, setQuotaResponseByAuthIndex, refreshQuotaCache }
}
