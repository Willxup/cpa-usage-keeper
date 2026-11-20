import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { ApiError, fetchUsageIdentityModels, isCostsBusy } from '@/lib/api'
import type { UsageIdentityModelsRange, UsageIdentityModelsResponse } from '@/lib/types'

type ModelUsageResult = {
  key: string
  data: UsageIdentityModelsResponse | null
  error: 'costs_busy' | 'load_failed' | null
}

export function useCredentialModelUsage(identityId: string, range: UsageIdentityModelsRange, onAuthRequired?: () => void, enabled = true, beforeReplace?: () => void, rangeRevision = 0) {
  const [attempt, setAttempt] = useState(0)
  const [result, setResult] = useState<ModelUsageResult | null>(null)
  const onAuthRequiredRef = useRef(onAuthRequired)
  const activeKeyRef = useRef<string | null>(null)
  const key = JSON.stringify([identityId, range, rangeRevision, attempt])

  useEffect(() => { onAuthRequiredRef.current = onAuthRequired }, [onAuthRequired])
  useLayoutEffect(() => {
    if (!enabled) return
    activeKeyRef.current = key
    return () => { if (activeKeyRef.current === key) activeKeyRef.current = null }
  }, [enabled, key])
  useEffect(() => {
    if (!enabled) return
    const controller = new AbortController()
    const isCurrent = () => !controller.signal.aborted && activeKeyRef.current === key
    void fetchUsageIdentityModels(identityId, range, controller.signal).then(data => {
      if (!isCurrent()) return
      beforeReplace?.()
      setResult({ key, data, error: null })
    }).catch((error: unknown) => {
      if (!isCurrent()) return
      const authRequired = error instanceof ApiError && error.status === 401
      const accessDenied = error instanceof ApiError && (error.status === 401 || error.status === 403)
      const migrationInProgress = error instanceof ApiError && error.status === 503 && error.code === 'migration_in_progress'
      const costsBusy = isCostsBusy(error)
      if (accessDenied || costsBusy || migrationInProgress) beforeReplace?.()
      setResult(previous => ({
        key,
        data: accessDenied || costsBusy || migrationInProgress ? null : previous?.data ?? null,
        error: costsBusy ? 'costs_busy' : 'load_failed',
      }))
      if (authRequired) onAuthRequiredRef.current?.()
    })
    return () => controller.abort()
  }, [identityId, range, key, enabled, beforeReplace])

  const retry = useCallback(() => setAttempt(value => value + 1), [])
  const current = result?.key === key ? result : null
  // 普通失败和新请求期间保留同一身份的已确认数据；鉴权、费用或迁移 gate 必须清空。
  const data = result?.data ?? null
  const error = current?.error ?? (data && result?.error === 'load_failed' ? result.error : null)
  return { data, error, loading: !current, retry }
}
