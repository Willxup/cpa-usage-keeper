import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ApiError, fetchUsageOverviewComparisons, isCostsBusy, isUsageRangeBoundsConflict } from '@/lib/api'
import type { UsageCustomRangeUnit, UsageOverviewComparisons, UsageTimeRange } from '@/lib/types'
import { buildUsageRangeQuery } from '@/utils/usage/rangeQuery'
import { buildUsageStatsQueryKey } from '@/stores'

export interface UseUsageComparisonsDataOptions {
  onAuthRequired?: () => void
  onRangeBoundsConflict?: (error: unknown) => boolean
  enabled?: boolean
  keyViewer?: boolean
  viewerIdentity?: object
  apiKeyId?: string
  range?: UsageTimeRange
  customUnit?: UsageCustomRangeUnit
  customStart?: string
  customEnd?: string
}

interface LoadComparisonsOptions {
  skipIfInFlight?: boolean
}

export function useUsageComparisonsData(options: UseUsageComparisonsDataOptions = {}) {
  const { onAuthRequired, onRangeBoundsConflict, enabled = true, keyViewer = false, viewerIdentity, apiKeyId, range = 'today', customUnit, customStart, customEnd } = options
  const [comparisons, setComparisons] = useState<UsageOverviewComparisons | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const requestSequence = useRef(0)
  const activeController = useRef<AbortController | null>(null)
  const loadedScope = useRef<{ queryKey: string; viewerIdentity?: object } | null>(null)
  const rangeQuery = useMemo(() => buildUsageRangeQuery({ range, customUnit, customStart, customEnd }), [customEnd, customStart, customUnit, range])
  const queryKey = rangeQuery.valid ? buildUsageStatsQueryKey(rangeQuery, apiKeyId) : null

  const loadComparisons = useCallback(async (options: LoadComparisonsOptions = {}) => {
    if (!rangeQuery.valid) return
    if (options.skipIfInFlight && activeController.current) return
    const sequence = ++requestSequence.current
    activeController.current?.abort()
    const controller = new AbortController()
    activeController.current = controller
    setLoading(true)
    setError('')
    try {
      const nextComparisons = await fetchUsageOverviewComparisons(rangeQuery, { apiKeyId, keyViewer, signal: controller.signal })
      if (sequence === requestSequence.current) {
        loadedScope.current = { queryKey: queryKey!, viewerIdentity }
        setComparisons(nextComparisons)
      }
    } catch (nextError) {
      if (controller.signal.aborted || sequence !== requestSequence.current) return
      if (isUsageRangeBoundsConflict(nextError) && onRangeBoundsConflict?.(nextError)) return
      if (nextError instanceof ApiError && nextError.status === 401) onAuthRequired?.()
      setError(keyViewer && isCostsBusy(nextError) ? 'COSTS_BUSY' : nextError instanceof Error ? nextError.message : 'USAGE_COMPARISONS_LOAD_FAILED')
    } finally {
      if (sequence === requestSequence.current) setLoading(false)
      if (sequence === requestSequence.current) activeController.current = null
    }
  }, [apiKeyId, keyViewer, onAuthRequired, onRangeBoundsConflict, queryKey, rangeQuery, viewerIdentity])

  useEffect(() => {
    if (!enabled || !rangeQuery.valid) return
    setComparisons(null)
    void loadComparisons()
    return () => {
      requestSequence.current += 1
      activeController.current?.abort()
      activeController.current = null
    }
  }, [enabled, loadComparisons, rangeQuery.valid])

  // 只复用同一范围与 Viewer 会话的成功结果；忙响应不会擦掉已展示的同范围金额。
  const visibleComparisons = loadedScope.current?.queryKey === queryKey && (!keyViewer || loadedScope.current.viewerIdentity === viewerIdentity)
    ? comparisons
    : null
  return { comparisons: visibleComparisons, loading, error, loadComparisons }
}
