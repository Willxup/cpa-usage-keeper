import { useEffect, useRef, useState, type Dispatch, type SetStateAction } from 'react'
import { ApiError, fetchUsageQuotaCache } from '@/lib/api'
import type { UsageQuotaRow } from '@/lib/types'

interface UseQuotaCacheOptions {
  enabled: boolean
  authIndexes: string[]
  onAuthRequired?: () => void
}

export interface QuotaCacheState {
  quotaByAuthIndex: Record<string, UsageQuotaRow[]>
  setQuotaByAuthIndex: Dispatch<SetStateAction<Record<string, UsageQuotaRow[]>>>
}

export function useQuotaCache({ enabled, authIndexes, onAuthRequired }: UseQuotaCacheOptions): QuotaCacheState {
  const [quotaByAuthIndex, setQuotaByAuthIndex] = useState<Record<string, UsageQuotaRow[]>>({})
  const requestControllerRef = useRef<AbortController | null>(null)

  useEffect(() => {
    if (!enabled) {
      requestControllerRef.current?.abort()
      requestControllerRef.current = null
      return
    }
    requestControllerRef.current?.abort()
    if (authIndexes.length === 0) {
      return
    }

    const controller = new AbortController()
    requestControllerRef.current = controller
    // 缓存接口不会刷新限额；当前页有多少 auth_index 就查询多少缓存。
    void fetchUsageQuotaCache(authIndexes, controller.signal).then((response) => {
      if (controller.signal.aborted || requestControllerRef.current !== controller) {
        return
      }
      setQuotaByAuthIndex((current) => {
        let changed = false
        const next = { ...current }
        const returnedAuthIndexes = new Set(response.items.map((item) => item.id))
        // 返回的数据写入本地缓存，未返回的条目保持未知状态，不显示假限额。
        for (const item of response.items) {
          if (next[item.id] !== item.quota) {
            next[item.id] = item.quota ?? []
            changed = true
          }
        }
        for (const authIndex of authIndexes) {
          if (!returnedAuthIndexes.has(authIndex) && next[authIndex] !== undefined) {
            delete next[authIndex]
            changed = true
          }
        }
        return changed ? next : current
      })
    }).catch((nextError) => {
      if (controller.signal.aborted) {
        return
      }
      if (nextError instanceof ApiError && nextError.status === 401) {
        onAuthRequired?.()
      }
    }).finally(() => {
      if (requestControllerRef.current === controller) {
        requestControllerRef.current = null
      }
    })

    return () => {
      controller.abort()
    }
  }, [enabled, onAuthRequired, authIndexes])

  return { quotaByAuthIndex, setQuotaByAuthIndex }
}
