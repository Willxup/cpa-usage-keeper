import { ApiError, apiFetch, apiPath } from './api'

export type StartupPhase = 'opening' | 'migrating' | 'failed' | 'ready'

export interface StartupStatus {
  phase: StartupPhase
  message: string
  progress: { processed_count: number; total_count: number | null } | null
}

// 读取公开启动状态供 App 门禁使用；无效合同或网络错误必须保持业务未就绪。
export async function getStartupStatus(signal?: AbortSignal): Promise<StartupStatus> {
  const response = await apiFetch(apiPath('/startup/status'), { signal, cache: 'no-store' })
  if (!response.ok) throw new ApiError('Startup status is unavailable', response.status)

  const body: unknown = await response.json()
  if (typeof body !== 'object' || body === null) throw new Error('Invalid startup status')
  const status = body as Partial<StartupStatus>
  if (!['opening', 'migrating', 'failed', 'ready'].includes(status.phase ?? '') ||
    typeof status.message !== 'string' || !('progress' in status)) {
    throw new Error('Invalid startup status')
  }
  const progress = status.progress
  if (progress !== null && progress !== undefined && (
    typeof progress !== 'object' ||
    !Number.isSafeInteger(progress.processed_count) || progress.processed_count < 0 ||
    (progress.total_count !== null && (!Number.isSafeInteger(progress.total_count) || progress.total_count < 0))
  )) {
    throw new Error('Invalid startup progress')
  }
  return { phase: status.phase as StartupPhase, message: status.message, progress: progress ?? null }
}
