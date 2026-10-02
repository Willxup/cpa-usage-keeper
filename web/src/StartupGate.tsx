import { useCallback, useEffect, useRef, useState } from 'react'
import App from './App'
import { notifyCPAMCEmbedReady } from './embed/cpamcEmbed'
import { MIGRATION_IN_PROGRESS_EVENT, latestApiRequestSequence, type MigrationInProgressEventDetail } from './lib/api'
import { getStartupStatus, type StartupStatus } from './lib/startup'
import { StartupPage } from './pages/StartupPage'

const POLL_INTERVAL_MS = 2500

// 启动门禁先确认业务就绪，再挂载会立即请求会话的 App；迁移期间只查询公开状态。
// 轮询代数和业务请求序号分别防止重试晚响应及旧标签页 API 响应覆盖新状态。
export function StartupGate() {
  const [status, setStatus] = useState<StartupStatus | null>(null)
  const [connectionIssue, setConnectionIssue] = useState(false)
  const [retryKey, setRetryKey] = useState(0)
  const requestGeneration = useRef(0)
  const readyAfterSequence = useRef(Number.POSITIVE_INFINITY)
  const ready = status?.phase === 'ready'

  const retry = useCallback(() => setRetryKey((value) => value + 1), [])

  useEffect(() => {
    // CPAMC 的 ready 消息表示 iframe 页面已显示；迁移期间也必须及时解除父页面的加载遮罩。
    notifyCPAMCEmbedReady()
  }, [])

  useEffect(() => {
    const onMigration = (event: Event) => {
      const { requestSequence } = (event as CustomEvent<MigrationInProgressEventDetail>).detail
      // 只有已挂载业务页后发出的请求才能触发退回；状态接口自身的 503 和旧响应均忽略。
      if (requestSequence <= readyAfterSequence.current) return
      readyAfterSequence.current = Number.POSITIVE_INFINITY
      requestGeneration.current += 1
      setStatus({ phase: 'opening', message: '', progress: null })
      setConnectionIssue(false)
    }
    window.addEventListener(MIGRATION_IN_PROGRESS_EVENT, onMigration)
    return () => window.removeEventListener(MIGRATION_IN_PROGRESS_EVENT, onMigration)
  }, [])

  useEffect(() => {
    if (ready) return
    let active = true
    let timer: ReturnType<typeof setTimeout> | undefined
    let controller: AbortController | undefined

    const poll = async () => {
      const generation = ++requestGeneration.current
      controller = new AbortController()
      try {
        const next = await getStartupStatus(controller.signal)
        if (!active || generation !== requestGeneration.current) return
        if (next.phase === 'ready') readyAfterSequence.current = latestApiRequestSequence()
        setStatus(next)
        setConnectionIssue(false)
      } catch {
        if (!active || generation !== requestGeneration.current) return
        // 断线时保留上次可信状态和进度，绝不推断业务已经就绪。
        setConnectionIssue(true)
      } finally {
        if (active && generation === requestGeneration.current) timer = setTimeout(poll, POLL_INTERVAL_MS)
      }
    }

    void poll()
    return () => {
      active = false
      if (timer) clearTimeout(timer)
      controller?.abort()
      requestGeneration.current += 1
    }
  }, [ready, retryKey])

  return ready ? <App /> : <StartupPage status={status} connectionIssue={connectionIssue} onRetry={retry} />
}
