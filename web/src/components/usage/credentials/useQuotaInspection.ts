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
  resetQuotaInspection: () => void
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
  const [inspectionPollEpoch, setInspectionPollEpoch] = useState(0)
  const inspectionPollingActiveRef = useRef(false)
  const onAuthRequiredRef = useRef(onAuthRequired)
  const onInspectionCompletedRef = useRef(onInspectionCompleted)
  const requestGenerationRef = useRef(0)
  const statusControllerRef = useRef<AbortController | null>(null)
  const pendingManualRefreshRef = useRef<Promise<void> | null>(null)
  const updateInspectionPollingActive = useCallback((active: boolean) => {
    inspectionPollingActiveRef.current = active
    setInspectionPollingActive(active)
  }, [])
  const retireStatusRequest = useCallback(() => {
    statusControllerRef.current?.abort()
    statusControllerRef.current = null
  }, [])

  const resetQuotaInspection = useCallback(() => {
    // 清除旧轮次展示并取消状态 GET；已提交的巡检 POST 只丢弃晚到页面响应。
    requestGenerationRef.current += 1
    pendingManualRefreshRef.current = null
    retireStatusRequest()
    setQuotaInspectionStatus(null)
    setQuotaInspectionLoading(false)
    setQuotaInspectionStarting(false)
    setQuotaInspectionError('')
    updateInspectionPollingActive(false)
  }, [retireStatusRequest, updateInspectionPollingActive])

  useEffect(() => {
    if (!enabled) {
      resetQuotaInspection()
    }
  }, [enabled, resetQuotaInspection])

  useEffect(() => () => {
    // 离开页面时退休旧 GET/POST 的响应归属，已受理的巡检后台工作继续运行。
    requestGenerationRef.current += 1
    pendingManualRefreshRef.current = null
    inspectionPollingActiveRef.current = false
    retireStatusRequest()
  }, [retireStatusRequest])

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

  const loadQuotaInspectionStatus = useCallback(async (controller = new AbortController()): Promise<UsageQuotaInspectionStatusResponse | null> => {
    statusControllerRef.current?.abort()
    statusControllerRef.current = controller
    const generation = requestGenerationRef.current
    const isCurrent = () => !controller.signal.aborted && statusControllerRef.current === controller && requestGenerationRef.current === generation
    setQuotaInspectionLoading(true)
    setQuotaInspectionError('')
    try {
      const response = await fetchUsageQuotaInspectionStatus(controller.signal)
      if (!isCurrent()) return null
      setQuotaInspectionStatus(response)
      return response
    } catch (error) {
      if (!isCurrent()) {
        return null
      }
      handleInspectionError(error)
      return null
    } finally {
      if (isCurrent()) {
        setQuotaInspectionLoading(false)
        statusControllerRef.current = null
      }
    }
  }, [handleInspectionError])

  useEffect(() => {
    if (!enabled) {
      updateInspectionPollingActive(false)
      return
    }
    const controller = new AbortController()
    const loadInitialInspectionStatus = async () => {
      const generation = requestGenerationRef.current
      const response = await loadQuotaInspectionStatus(controller)
      if (!controller.signal.aborted && requestGenerationRef.current === generation) {
        updateInspectionPollingActive(shouldContinueQuotaInspectionPolling(response))
      }
    }
    void loadInitialInspectionStatus()
    return () => {
      controller.abort()
    }
  }, [enabled, loadQuotaInspectionStatus, updateInspectionPollingActive])

  useEffect(() => {
    if (!enabled || !inspectionPollingActive) {
      return
    }
    let cancelled = false
    let timer: number | undefined
    const controller = new AbortController()
    const pollQuotaInspectionStatus = async () => {
      // 手动刷新由操作会话等待，轮询不能抢占；仍按原间隔检查下一轮。
      if (pendingManualRefreshRef.current) {
        timer = window.setTimeout(() => { void pollQuotaInspectionStatus() }, QUOTA_INSPECTION_REFRESH_INTERVAL_MS)
        return
      }
      const generation = requestGenerationRef.current
      const response = await loadQuotaInspectionStatus(controller)
      if (cancelled || controller.signal.aborted || requestGenerationRef.current !== generation) {
        return
      }
      if (!shouldContinueQuotaInspectionPolling(response)) {
        updateInspectionPollingActive(false)
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
  }, [enabled, inspectionPollingActive, inspectionPollEpoch, loadQuotaInspectionStatus, updateInspectionPollingActive])

  const refreshQuotaInspectionStatus = useCallback(async () => {
    if (!enabled) return
    const generation = requestGenerationRef.current
    const previous = pendingManualRefreshRef.current
    // 后来的手动读取顺序执行，既不提前解除前一会话的等待，也不复用可能早于本次操作的快照。
    setInspectionPollEpoch((current) => current + 1)
    const refresh = (async () => {
      if (previous) await previous
      if (requestGenerationRef.current !== generation) return
      const response = await loadQuotaInspectionStatus()
      if (requestGenerationRef.current !== generation || response === null) return
      const wasPolling = inspectionPollingActiveRef.current
      const nextPolling = shouldContinueQuotaInspectionPolling(response)
      updateInspectionPollingActive(nextPolling)
      // 仅在巡检从运行态转入完成态时通知一次缓存刷新，避免静态已完成快照或串行手动读取重复回调。
      if (wasPolling && !nextPolling && shouldNotifyQuotaInspectionCompleted(response)) {
        onInspectionCompletedRef.current?.()
      }
    })()
    pendingManualRefreshRef.current = refresh
    try {
      await refresh
    } finally {
      if (pendingManualRefreshRef.current === refresh) pendingManualRefreshRef.current = null
    }
  }, [enabled, loadQuotaInspectionStatus, updateInspectionPollingActive])

  const startQuotaInspection = useCallback(async () => {
    const generation = requestGenerationRef.current
    const wasPolling = inspectionPollingActiveRef.current
    updateInspectionPollingActive(false)
    setQuotaInspectionStarting(true)
    setQuotaInspectionError('')
    try {
      // 新轮次和 POST 响应不能取消正由会话等待的手动读取；普通旧 GET 仍须退休。
      while (pendingManualRefreshRef.current && requestGenerationRef.current === generation) {
        await pendingManualRefreshRef.current
      }
      if (requestGenerationRef.current !== generation) return
      updateInspectionPollingActive(false)
      retireStatusRequest()
      const response = await startUsageQuotaInspection()
      while (pendingManualRefreshRef.current && requestGenerationRef.current === generation) {
        await pendingManualRefreshRef.current
      }
      if (requestGenerationRef.current !== generation) return
      retireStatusRequest()
      setQuotaInspectionStatus(response)
      updateInspectionPollingActive(shouldContinueQuotaInspectionPolling(response))
      setInspectionPollEpoch((current) => current + 1)
      if (shouldNotifyQuotaInspectionCompleted(response)) {
        onInspectionCompletedRef.current?.()
      }
    } catch (error) {
      if (requestGenerationRef.current === generation) {
        handleInspectionError(error)
        if (wasPolling) {
          updateInspectionPollingActive(true)
          setInspectionPollEpoch((current) => current + 1)
        }
      }
    } finally {
      if (requestGenerationRef.current === generation) setQuotaInspectionStarting(false)
    }
  }, [handleInspectionError, retireStatusRequest, updateInspectionPollingActive])

  return {
    quotaInspectionStatus,
    quotaInspectionLoading,
    quotaInspectionStarting,
    quotaInspectionError,
    refreshQuotaInspectionStatus,
    startQuotaInspection,
    resetQuotaInspection,
  }
}
