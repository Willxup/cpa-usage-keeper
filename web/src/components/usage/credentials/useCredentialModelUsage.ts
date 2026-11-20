import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, fetchUsageIdentityModels, isCostsBusy } from '@/lib/api'
import type { UsageIdentityModelsRange, UsageIdentityModelsResponse } from '@/lib/types'

type ModelUsageResult =
  | { key: string; data: UsageIdentityModelsResponse; error: null }
  | { key: string; data: null; error: 'costs_busy' | 'load_failed' }

export function useCredentialModelUsage(identityId: string, range: UsageIdentityModelsRange, onAuthRequired?: () => void) {
  const [attempt, setAttempt] = useState(0)
  const [result, setResult] = useState<ModelUsageResult | null>(null)
  const onAuthRequiredRef = useRef(onAuthRequired)
  const key = JSON.stringify([identityId, range, attempt])

  useEffect(() => { onAuthRequiredRef.current = onAuthRequired }, [onAuthRequired])
  useEffect(() => {
    const controller = new AbortController()
    void fetchUsageIdentityModels(identityId, range, controller.signal).then(data => {
      if (!controller.signal.aborted) setResult({ key, data, error: null })
    }).catch((error: unknown) => {
      if (controller.signal.aborted) return
      if (error instanceof ApiError && error.status === 401) onAuthRequiredRef.current?.()
      setResult({ key, data: null, error: isCostsBusy(error) ? 'costs_busy' : 'load_failed' })
    })
    return () => controller.abort()
  }, [identityId, range, key])

  const retry = useCallback(() => setAttempt(value => value + 1), [])
  // 输入或重试轮次变化时立即隔离旧数据，即使 effect 清理尚未运行。
  const current = result?.key === key ? result : null
  return { data: current?.data ?? null, error: current?.error ?? null, loading: !current, retry }
}
