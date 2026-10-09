import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { OverviewRealtimeBlock } from '@/lib/types'
import { useUsageStatsStore } from '../useUsageStatsStore'

const apiMocks = vi.hoisted(() => ({
  fetchUsageOverview: vi.fn(),
  fetchUsageOverviewRealtime: vi.fn(),
}))

vi.mock('@/lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/lib/api')>(),
  ...apiMocks,
}))

const realtime: OverviewRealtimeBlock = {
  window: '15m',
  bucket_seconds: 30,
  token_velocity: [],
  latency_scatter: { points: [], total_points: 0, p95_ttft_ms: 0, p95_latency_ms: 0, max_ttft_ms: 0, max_latency_ms: 0 },
  current_usage: { models: [], api_keys: [], auth_files: [], ai_providers: [] },
  request_level: [],
  cache_level: [],
}

describe('useUsageStatsStore', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useUsageStatsStore.getState().clearUsageStats()
  })

  it('keeps overview and realtime loaders independent', async () => {
    const overview = Promise.withResolvers<unknown>()
    apiMocks.fetchUsageOverview.mockReturnValue(overview.promise)
    apiMocks.fetchUsageOverviewRealtime.mockResolvedValue({ ...realtime, window: '30m', bucket_seconds: 60 })

    const overviewLoad = useUsageStatsStore.getState().loadUsageStats({
      force: true,
      range: '24h',
      apiKeyId: '9007199254740993',
    })
    const realtimeLoad = useUsageStatsStore.getState().loadUsageStatsRealtime({
      force: true,
      apiKeyId: '9007199254740993',
      realtimeWindow: '30m',
    })

    await Promise.resolve()

    expect(apiMocks.fetchUsageOverview).toHaveBeenCalledTimes(1)
    expect(apiMocks.fetchUsageOverviewRealtime).toHaveBeenCalledTimes(1)
    expect(apiMocks.fetchUsageOverviewRealtime).toHaveBeenCalledWith({
      signal: expect.any(AbortSignal),
      apiKeyId: '9007199254740993',
      window: '30m',
    })

    overview.resolve({
      usage: { total_requests: 1, success_count: 1, failure_count: 0, total_tokens: 20 },
    })
    await Promise.all([overviewLoad, realtimeLoad])

    const state = useUsageStatsStore.getState()
    expect(state.usage?.usage.total_requests).toBe(1)
    expect(state.usage).not.toHaveProperty('realtime')
    expect(state.realtime?.window).toBe('30m')
  })

  it('does not reload overview when only the realtime window changes', async () => {
    apiMocks.fetchUsageOverview.mockResolvedValue({
      usage: { total_requests: 1, success_count: 1, failure_count: 0, total_tokens: 20 },
    })
    apiMocks.fetchUsageOverviewRealtime.mockResolvedValue({ ...realtime, window: '60m', bucket_seconds: 120 })

    await useUsageStatsStore.getState().loadUsageStats({
      force: true,
      range: '24h',
      apiKeyId: '9007199254740993',
    })
    await useUsageStatsStore.getState().loadUsageStatsRealtime({
      force: true,
      apiKeyId: '9007199254740993',
      realtimeWindow: '60m',
    })

    expect(apiMocks.fetchUsageOverview).toHaveBeenCalledTimes(1)
    expect(apiMocks.fetchUsageOverviewRealtime).toHaveBeenCalledTimes(1)
    expect(apiMocks.fetchUsageOverviewRealtime).toHaveBeenCalledWith({
      signal: expect.any(AbortSignal),
      apiKeyId: '9007199254740993',
      window: '60m',
    })
  })

  it('does not keep an error from a different realtime query when cached data is fresh', async () => {
    apiMocks.fetchUsageOverviewRealtime.mockResolvedValueOnce(realtime)

    await useUsageStatsStore.getState().loadUsageStatsRealtime({
      force: true,
      realtimeWindow: '15m',
    })

    apiMocks.fetchUsageOverviewRealtime.mockRejectedValueOnce(new Error('60m failed'))
    await useUsageStatsStore.getState().loadUsageStatsRealtime({
      force: true,
      realtimeWindow: '60m',
    })

    expect(useUsageStatsStore.getState().realtimeError).toBe('60m failed')

    await useUsageStatsStore.getState().loadUsageStatsRealtime({
      realtimeWindow: '15m',
    })

    const state = useUsageStatsStore.getState()
    expect(state.realtime?.window).toBe('15m')
    expect(state.realtimeError).toBe('')
  })
})
