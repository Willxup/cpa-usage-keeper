import { calculateCacheRate, formatDurationMs } from '@/utils/usage'

// toNumber 把任意值安全转成数字，非法值返回 0。
export const toNumber = (value: unknown): number => {
  const parsed = Number(value)
  if (!Number.isFinite(parsed)) return 0
  return parsed
}

// formatRequestEventTimestamp 把 ISO 时间串格式化成 YYYY/MM/DD HH:MM:SS 便于表格展示。
export const formatRequestEventTimestamp = (timestamp: string): string => {
  const match = timestamp.match(/^(\d{4})-(\d{2})-(\d{2})[T\s](\d{2}):(\d{2}):(\d{2})/)
  if (!match) return timestamp || '-'
  return `${match[1]}/${match[2]}/${match[3]} ${match[4]}:${match[5]}:${match[6]}`
}

// formatCacheRate 计算缓存命中率百分比，无输入时返回 '-'。
export const formatCacheRate = (cachedTokens: number, inputTokens: number): string => {
  const rate = calculateCacheRate({ inputTokens, cachedTokens })
  return rate === null ? '-' : `${rate.toFixed(2)}%`
}

// formatTTFTMs 格式化首字延迟，无效值返回 '-'。
export const formatTTFTMs = (ttftMs: number | null): string => {
  if (ttftMs === null || ttftMs <= 0) {
    return '-'
  }
  return formatDurationMs(ttftMs)
}

// formatSpeedTPS 格式化生成速度，无效值返回 '-'。
export const formatSpeedTPS = (speedTPS: number | null): string => {
  if (speedTPS === null || speedTPS <= 0) {
    return '-'
  }
  return `${speedTPS.toFixed(1)} t/s`
}

// parseRequestEndpoint 从原始 endpoint 文本解析出请求类型（SSE/WS）和路径。
export const parseRequestEndpoint = (rawEndpoint: unknown): { requestType: string; endpoint: string } => {
  const raw = String(rawEndpoint ?? '').trim().replace(/\s+/g, ' ')
  if (!raw) {
    return { requestType: '-', endpoint: '-' }
  }
  const [first, ...rest] = raw.split(' ')
  const upperMethod = first.toUpperCase()
  const hasMethod = ['GET', 'POST'].includes(upperMethod)
  const requestType = upperMethod === 'POST' ? 'SSE' : upperMethod === 'GET' ? 'WS' : '-'
  const path = hasMethod ? rest.join(' ').trim() : raw
  const normalizedPath = path.startsWith('/v1/') ? path.slice(3) : path === '/v1' ? '/' : path
  return { requestType, endpoint: normalizedPath || '-' }
}
