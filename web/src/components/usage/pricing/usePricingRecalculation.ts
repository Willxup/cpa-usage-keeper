import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, fetchCurrentPricingRecalculation, startPricingRecalculation } from '@/lib/api'
import type { PricingRecalculationTask, StartPricingRecalculationResponse } from '@/lib/types'

export interface UsePricingRecalculationOptions {
  enabled?: boolean
  onAuthRequired?: () => void
  onSettled?: (task: PricingRecalculationTask) => void
}

export interface UsePricingRecalculationReturn {
  task: PricingRecalculationTask | null
  loading: boolean
  starting: boolean
  connectionError: string
  error: string
  errorCode: string
  refreshCurrent: () => Promise<PricingRecalculationTask | null>
  start: (startAt: string, configRevision: number) => Promise<StartPricingRecalculationResponse | null>
}

const POLL_INTERVAL_MS = 2000
const messageOf = (error: unknown): string => error instanceof Error ? error.message : 'Pricing recalculation request failed'
type CurrentRead = { confirmed: boolean; task: PricingRecalculationTask | null }

// usePricingRecalculation 只跟踪当前进程的单个任务；进入页面及运行期间读取 current，不持久化或自动重试 POST。
// 请求取消和身份比对阻止旧响应覆盖新状态；本页观察的任务终态只通知一次，入页旧结果保持安静。
export function usePricingRecalculation(options: UsePricingRecalculationOptions = {}): UsePricingRecalculationReturn {
  const { enabled = true, onAuthRequired, onSettled } = options
  const [task, setTask] = useState<PricingRecalculationTask | null>(null)
  const [loading, setLoading] = useState(false)
  const [starting, setStarting] = useState(false)
  const [connectionError, setConnectionError] = useState('')
  const [error, setError] = useState('')
  const [errorCode, setErrorCode] = useState('')
  const mountedRef = useRef(false)
  const enabledRef = useRef(enabled)
  const taskRef = useRef<PricingRecalculationTask | null>(null)
  const currentKnownRef = useRef(false)
  const currentRequestRef = useRef<AbortController | null>(null)
  const startRequestRef = useRef<AbortController | null>(null)
  const startingRef = useRef(false)
  const observedRunningTaskIDRef = useRef<string | null>(null)
  const lastSettledTaskIDRef = useRef<string | null>(null)
  const onAuthRequiredRef = useRef(onAuthRequired)
  const onSettledRef = useRef(onSettled)
  enabledRef.current = enabled
  onAuthRequiredRef.current = onAuthRequired
  onSettledRef.current = onSettled

  // applyTask 仅发布仍有效的请求结果；旧终态不触发刷新回调，已确认的新任务终态只通知一次。
  const applyTask = useCallback((next: PricingRecalculationTask | null, confirmedStart = false) => {
    taskRef.current = next
    setTask(next)
    if (!next) {
      observedRunningTaskIDRef.current = null
      return
    }
    if (next.status === 'running') {
      observedRunningTaskIDRef.current = next.task_id
      return
    }
    if ((confirmedStart || observedRunningTaskIDRef.current === next.task_id) && lastSettledTaskIDRef.current !== next.task_id) {
      lastSettledTaskIDRef.current = next.task_id
      onSettledRef.current?.(next)
    }
  }, [])

  // loadCurrent 保留断网前任务并在单次读取失败后继续原轮询；新请求先取消旧 current 防晚响应。
  const loadCurrent = useCallback(async (duringStart: boolean): Promise<CurrentRead> => {
    if (!mountedRef.current || !enabledRef.current) return { confirmed: false, task: null }
    if (startingRef.current && !duringStart) return { confirmed: false, task: taskRef.current }
    currentRequestRef.current?.abort()
    const controller = new AbortController()
    currentRequestRef.current = controller
    setLoading(true)
    try {
      const current = await fetchCurrentPricingRecalculation(controller.signal)
      if (currentRequestRef.current !== controller || controller.signal.aborted || !mountedRef.current || !enabledRef.current) return { confirmed: false, task: null }
      applyTask(current)
      currentKnownRef.current = true
      setConnectionError('')
      return { confirmed: true, task: current }
    } catch (readError) {
      if (currentRequestRef.current !== controller || controller.signal.aborted || !mountedRef.current || !enabledRef.current) return { confirmed: false, task: null }
      if (readError instanceof ApiError && readError.status === 401) onAuthRequiredRef.current?.()
      setConnectionError(messageOf(readError))
      return { confirmed: false, task: taskRef.current }
    } finally {
      if (currentRequestRef.current === controller) {
        currentRequestRef.current = null
        if (mountedRef.current && enabledRef.current) setLoading(false)
      }
    }
  }, [applyTask])

  const refreshCurrent = useCallback(async () => (await loadCurrent(false)).task, [loadCurrent])

  // start 同步占用本页提交槽，防双击；POST 回执未知时只查 current 确认现状，不重发或推断幂等成功。
  const start = useCallback(async (startAt: string, configRevision: number): Promise<StartPricingRecalculationResponse | null> => {
    if (!mountedRef.current || !enabledRef.current || startingRef.current) return null
    const previousTaskID = taskRef.current?.task_id
    const previousSlotKnown = currentKnownRef.current
    startingRef.current = true
    setStarting(true)
    setError('')
    setErrorCode('')
    currentRequestRef.current?.abort()
    currentRequestRef.current = null
    setLoading(false)
    const controller = new AbortController()
    startRequestRef.current = controller
    try {
      const result = await startPricingRecalculation({ start_at: startAt, config_revision: configRevision }, controller.signal)
      if (startRequestRef.current !== controller || controller.signal.aborted || !mountedRef.current || !enabledRef.current) return null
      applyTask(result.task, result.started)
      setConnectionError('')
      return result
    } catch (startError) {
      if (startRequestRef.current !== controller || controller.signal.aborted || !mountedRef.current || !enabledRef.current) return null
      if (startError instanceof ApiError && startError.status === 401) onAuthRequiredRef.current?.()
      const knownRejection = startError instanceof ApiError && startError.status >= 400 && startError.status < 500
      if (!knownRejection) {
        const observed = await loadCurrent(true)
        if (startRequestRef.current !== controller || controller.signal.aborted || !mountedRef.current || !enabledRef.current) return null
        // 旧 completed 任务或读取失败都不能证明这次 POST 已受理；只采用新增任务或仍运行的真实槽状态。
        if (observed.confirmed && observed.task && (observed.task.status === 'running' || (previousSlotKnown && observed.task.task_id !== previousTaskID))) {
          if (observed.task.status !== 'running' && previousSlotKnown && observed.task.task_id !== previousTaskID) {
            applyTask(observed.task, true)
          }
          setError('')
          setErrorCode('')
          return { started: false, task: observed.task }
        }
      }
      setError(messageOf(startError))
      setErrorCode(startError instanceof ApiError ? startError.code ?? '' : '')
      return null
    } finally {
      if (startRequestRef.current === controller) {
        startRequestRef.current = null
        startingRef.current = false
        if (mountedRef.current && enabledRef.current) setStarting(false)
      }
    }
  }, [applyTask, loadCurrent])

  // 启用时读取 current；停用/卸载只取消本页请求，服务端已经受理的任务继续执行。
  useEffect(() => {
    mountedRef.current = true
    if (enabled) {
      void refreshCurrent()
    } else {
      currentKnownRef.current = false
      setLoading(false)
      setStarting(false)
    }
    return () => {
      mountedRef.current = false
      currentRequestRef.current?.abort()
      currentRequestRef.current = null
      startRequestRef.current?.abort()
      startRequestRef.current = null
      startingRef.current = false
    }
  }, [enabled, refreshCurrent])

  // 运行中按上一次 current 结束后约两秒再查，网络慢时仍只有一个 current 请求在途。
  useEffect(() => {
    if (!enabled || (task?.status !== 'running' && !connectionError)) return
    let active = true
    let timer: ReturnType<typeof setTimeout>
    const poll = async () => {
      if (!active) return
      if (currentRequestRef.current) {
        timer = setTimeout(poll, POLL_INTERVAL_MS)
        return
      }
      await refreshCurrent()
      if (active) timer = setTimeout(poll, POLL_INTERVAL_MS)
    }
    timer = setTimeout(poll, POLL_INTERVAL_MS)
    return () => {
      active = false
      clearTimeout(timer)
    }
  }, [enabled, task?.task_id, task?.status, connectionError, refreshCurrent])

  return { task, loading, starting, connectionError, error, errorCode, refreshCurrent, start }
}
