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
  const onAuthRequiredRef = useRef(onAuthRequired)
  const onInspectionCompletedRef = useRef(onInspectionCompleted)
  const requestGenerationRef = useRef(0)
  const statusControllerRef = useRef<AbortController | null>(null)
  const retireStatusRequest = useCallback(() => {
    statusControllerRef.current?.abort()
    statusControllerRef.current = null
  }, [])

  const resetQuotaInspection = useCallback(() => {
    // 清除旧轮次展示并取消状态 GET；已提交的巡检 POST 只丢弃晚到页面响应。
    requestGenerationRef.current += 1
    retireStatusRequest()
    setQuotaInspectionStatus(null)
    setQuotaInspectionLoading(false)
    setQuotaInspectionStarting(false)
    setQuotaInspectionError('')
    setInspectionPollingActive(false)
  }, [retireStatusRequest])

  useEffect(() => {
    if (!enabled) {
      resetQuotaInspection()
    }
  }, [enabled, resetQuotaInspection])

  useEffect(() => () => {
    // 离开页面时退休旧 GET/POST 的响应归属，已受理的巡检后台工作继续运行。
    requestGenerationRef.current += 1
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
      setInspectionPollingActive(false)
      return
    }
    const controller = new AbortController()
    const loadInitialInspectionStatus = async () => {
      const generation = requestGenerationRef.current
      const response = await loadQuotaInspectionStatus(controller)
      if (!controller.signal.aborted && requestGenerationRef.current === generation) {
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
      const generation = requestGenerationRef.current
      const response = await loadQuotaInspectionStatus(controller)
      if (cancelled || controller.signal.aborted || requestGenerationRef.current !== generation) {
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
  }, [enabled, inspectionPollingActive, inspectionPollEpoch, loadQuotaInspectionStatus])

  const refreshQuotaInspectionStatus = useCallback(async () => {
    if (!enabled) return
    const generation = requestGenerationRef.current
    // 手动读取会替换旧轮询 GET；即使本次读取失败，也应保留下一轮定时查询。
    setInspectionPollEpoch((current) => current + 1)
    const response = await loadQuotaInspectionStatus()
    if (requestGenerationRef.current !== generation || response === null) return
    setInspectionPollingActive(shouldContinueQuotaInspectionPolling(response))
  }, [enabled, loadQuotaInspectionStatus])

  const startQuotaInspection = useCallback(async () => {
    const generation = requestGenerationRef.current
    const wasPolling = inspectionPollingActive
    // 新轮次启动前使旧状态读取失效；POST 受理期间出现的 GET 也不能盖过启动响应。
    retireStatusRequest()
    setInspectionPollingActive(false)
    setQuotaInspectionStarting(true)
    setQuotaInspectionError('')
    try {
      const response = await startUsageQuotaInspection()
      if (requestGenerationRef.current !== generation) return
      retireStatusRequest()
      setQuotaInspectionStatus(response)
      setInspectionPollingActive(shouldContinueQuotaInspectionPolling(response))
      setInspectionPollEpoch((current) => current + 1)
      if (shouldNotifyQuotaInspectionCompleted(response)) {
        onInspectionCompletedRef.current?.()
      }
    } catch (error) {
      if (requestGenerationRef.current === generation) {
        handleInspectionError(error)
        if (wasPolling) {
          setInspectionPollingActive(true)
          setInspectionPollEpoch((current) => current + 1)
        }
      }
    } finally {
      if (requestGenerationRef.current === generation) setQuotaInspectionStarting(false)
    }
  }, [handleInspectionError, inspectionPollingActive, retireStatusRequest])

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
