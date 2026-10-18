export const OVERVIEW_REALTIME_WINDOWS = ['1m', '5m', '10m', '15m', '30m', '60m'] as const

export type OverviewRealtimeWindow = (typeof OVERVIEW_REALTIME_WINDOWS)[number]

export const DEFAULT_OVERVIEW_REALTIME_WINDOW: OverviewRealtimeWindow = '15m'

// 后端每个窗口固定 30 个可见 bucket，bucket 宽度 = 窗口长度 / 30。
export const OVERVIEW_REALTIME_BUCKET_SECONDS: Record<OverviewRealtimeWindow, number> = {
  '1m': 2,
  '5m': 10,
  '10m': 20,
  '15m': 30,
  '30m': 60,
  '60m': 120,
}

export const overviewRealtimeBucketSeconds = (window: OverviewRealtimeWindow): number =>
  OVERVIEW_REALTIME_BUCKET_SECONDS[window]

export const isOverviewRealtimeWindow = (value: unknown): value is OverviewRealtimeWindow =>
  typeof value === 'string' && (OVERVIEW_REALTIME_WINDOWS as readonly string[]).includes(value)
