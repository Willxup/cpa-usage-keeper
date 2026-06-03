import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, fetchUsageQuotaInspectionStatus, startUsageQuotaInspection } from '@/lib/api'
import type { UsageQuotaInspectionStatusResponse } from '@/lib/types'

export const QUOTA_INSPECTION_REFRESH_INTERVAL_MS = 3_000

interface UseQuotaInspectionOptions {
  enabled: boolean
  onAuthRequired?: () => void
  onInspectionCompleted?: () => void
}

export interface QuotaInspectionState {
  quotaInspectionStatus: UsageQuotaInspectionStatusResponse | null
  quotaInspectionLoading: boolean
  quotaInspectionStarting: boolean
  quotaInspectionError: string
  refreshQuotaInspectionStatus: () => Promise<void>
  startQuotaInspection: () => Promise<void>
}

export function shouldContinueQuotaInspectionPolling(status: Pick<UsageQuotaInspectionStatusResponse, 'running' | 'completed'> | null): boolean {
  return Boolean(status?.running && !status.completed)
}

export function shouldNotifyQuotaInspectionCompleted(status: Pick<UsageQuotaInspectionStatusResponse, 'running' | 'completed'> | null): boolean {
  return Boolean(status?.completed && !status.running)
}

export function useQuotaInspection({ enabled, onAuthRequired, onInspectionCompleted }: UseQuotaInspectionOptions): QuotaInspectionState {
  const [quotaInspectionStatus, setQuotaInspectionStatus] = useState<UsageQuotaInspectionStatusResponse | null>(null)
  const [quotaInspectionLoading, setQuotaInspectionLoading] = useState(false)
  const [quotaInspectionStarting, setQuotaInspectionStarting] = useState(false)
  const [quotaInspectionError, setQuotaInspectionError] = useState('')
  const [inspectionPollingActive, setInspectionPollingActive] = useState(false)
  const onAuthRequiredRef = useRef(onAuthRequired)
  const onInspectionCompletedRef = useRef(onInspectionCompleted)

  useEffect(() => {
    onAuthRequiredRef.current = onAuthRequired
  }, [onAuthRequired])

  useEffect(() => {
    onInspectionCompletedRef.current = onInspectionCompleted
  }, [onInspectionCompleted])

  const handleInspectionError = useCallback((error: unknown) => {
    if (error instanceof ApiError && error.status === 401) {
      onAuthRequiredRef.current?.()
      return
    }
    setQuotaInspectionError(error instanceof Error ? error.message : 'Failed to load quota inspection status')
  }, [])

  const loadQuotaInspectionStatus = useCallback(async (signal?: AbortSignal): Promise<UsageQuotaInspectionStatusResponse | null> => {
    setQuotaInspectionLoading(true)
    setQuotaInspectionError('')
    try {
      const response = await fetchUsageQuotaInspectionStatus(signal)
      setQuotaInspectionStatus(response)
      return response
    } catch (error) {
      if (signal?.aborted) {
        return null
      }
      handleInspectionError(error)
      return null
    } finally {
      if (!signal?.aborted) {
        setQuotaInspectionLoading(false)
      }
    }
  }, [handleInspectionError])

  useEffect(() => {
    if (!enabled) {
      setInspectionPollingActive(false)
      return
    }
    const controller = new AbortController()
    const loadInitialInspectionStatus = async () => {
      const response = await loadQuotaInspectionStatus(controller.signal)
      if (!controller.signal.aborted) {
        setInspectionPollingActive(shouldContinueQuotaInspectionPolling(response))
      }
    }
    void loadInitialInspectionStatus()
    return () => {
      controller.abort()
    }
  }, [enabled, loadQuotaInspectionStatus])

  useEffect(() => {
    if (!enabled || !inspectionPollingActive) {
      return
    }
    let cancelled = false
    let timer: number | undefined
    const controller = new AbortController()
    const pollQuotaInspectionStatus = async () => {
      const response = await loadQuotaInspectionStatus(controller.signal)
      if (cancelled) {
        return
      }
      if (!shouldContinueQuotaInspectionPolling(response)) {
        setInspectionPollingActive(false)
        if (shouldNotifyQuotaInspectionCompleted(response)) {
          onInspectionCompletedRef.current?.()
        }
        return
      }
      timer = window.setTimeout(() => {
        void pollQuotaInspectionStatus()
      }, QUOTA_INSPECTION_REFRESH_INTERVAL_MS)
    }
    timer = window.setTimeout(() => {
      void pollQuotaInspectionStatus()
    }, QUOTA_INSPECTION_REFRESH_INTERVAL_MS)
    return () => {
      cancelled = true
      controller.abort()
      if (timer !== undefined) {
        window.clearTimeout(timer)
      }
    }
  }, [enabled, inspectionPollingActive, loadQuotaInspectionStatus])

  const refreshQuotaInspectionStatus = useCallback(async () => {
    const response = await loadQuotaInspectionStatus()
    setInspectionPollingActive(shouldContinueQuotaInspectionPolling(response))
  }, [loadQuotaInspectionStatus])

  const startQuotaInspection = useCallback(async () => {
    setQuotaInspectionStarting(true)
    setQuotaInspectionError('')
    try {
      const response = await startUsageQuotaInspection()
      setQuotaInspectionStatus(response)
      setInspectionPollingActive(shouldContinueQuotaInspectionPolling(response))
      if (shouldNotifyQuotaInspectionCompleted(response)) {
        onInspectionCompletedRef.current?.()
      }
    } catch (error) {
      handleInspectionError(error)
    } finally {
      setQuotaInspectionStarting(false)
    }
  }, [handleInspectionError])

  return {
    quotaInspectionStatus,
    quotaInspectionLoading,
    quotaInspectionStarting,
    quotaInspectionError,
    refreshQuotaInspectionStatus,
    startQuotaInspection,
  }
}
