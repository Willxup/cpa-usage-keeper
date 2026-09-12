import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ApiError, fetchUsageOverviewComparisons, isUsageRangeBoundsConflict } from '@/lib/api'
import type { UsageCustomRangeUnit, UsageOverviewComparisons, UsageTimeRange } from '@/lib/types'
import { buildUsageRangeQuery } from '@/utils/usage/rangeQuery'

export interface UseUsageComparisonsDataOptions {
  onAuthRequired?: () => void
  onRangeBoundsConflict?: (error: unknown) => boolean
  enabled?: boolean
  keyViewer?: boolean
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
  const { onAuthRequired, onRangeBoundsConflict, enabled = true, keyViewer = false, apiKeyId, range = 'today', customUnit, customStart, customEnd } = options
  const [comparisons, setComparisons] = useState<UsageOverviewComparisons | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const requestSequence = useRef(0)
  const activeController = useRef<AbortController | null>(null)
  const rangeQuery = useMemo(() => buildUsageRangeQuery({ range, customUnit, customStart, customEnd }), [customEnd, customStart, customUnit, range])

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
      if (sequence === requestSequence.current) setComparisons(nextComparisons)
    } catch (nextError) {
      if (controller.signal.aborted || sequence !== requestSequence.current) return
      if (isUsageRangeBoundsConflict(nextError) && onRangeBoundsConflict?.(nextError)) return
      if (nextError instanceof ApiError && nextError.status === 401) onAuthRequired?.()
      setError(nextError instanceof Error ? nextError.message : 'USAGE_COMPARISONS_LOAD_FAILED')
    } finally {
      if (sequence === requestSequence.current) setLoading(false)
      if (sequence === requestSequence.current) activeController.current = null
    }
  }, [apiKeyId, keyViewer, onAuthRequired, onRangeBoundsConflict, rangeQuery])

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

  return { comparisons, loading, error, loadComparisons }
}
