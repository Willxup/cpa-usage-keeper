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

  it('forces a new overview request and ignores the late pre-recalculation response', async () => {
    const old = Promise.withResolvers<unknown>()
    const fresh = Promise.withResolvers<unknown>()
    apiMocks.fetchUsageOverview.mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise)
    const oldLoad = useUsageStatsStore.getState().loadUsageStats({ range: '24h' })
    const oldSignal = apiMocks.fetchUsageOverview.mock.calls[0][1] as AbortSignal
    const freshLoad = useUsageStatsStore.getState().loadUsageStats({ range: '24h', force: true })

    expect(apiMocks.fetchUsageOverview).toHaveBeenCalledTimes(2)
    expect(oldSignal.aborted).toBe(true)
    fresh.resolve({ usage: { total_requests: 1, total_cost: 2 } })
    await freshLoad
    // 模拟传输层没有响应 abort：旧成功响应也不得覆盖新费用。
    old.resolve({ usage: { total_requests: 1, total_cost: 1 } })
    await oldLoad
    expect(useUsageStatsStore.getState().usage?.usage.total_cost).toBe(2)
    expect(useUsageStatsStore.getState().loading).toBe(false)
  })

  it('forces a new realtime request without accepting the late previous result', async () => {
    const old = Promise.withResolvers<OverviewRealtimeBlock>()
    const fresh = Promise.withResolvers<OverviewRealtimeBlock>()
    apiMocks.fetchUsageOverviewRealtime.mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise)
    const oldLoad = useUsageStatsStore.getState().loadUsageStatsRealtime({ realtimeWindow: '15m' })
    const oldSignal = apiMocks.fetchUsageOverviewRealtime.mock.calls[0][0].signal as AbortSignal
    const freshLoad = useUsageStatsStore.getState().loadUsageStatsRealtime({ realtimeWindow: '15m', force: true })

    expect(apiMocks.fetchUsageOverviewRealtime).toHaveBeenCalledTimes(2)
    expect(oldSignal.aborted).toBe(true)
    const freshValue = { ...realtime, bucket_seconds: 60 }
    fresh.resolve(freshValue)
    await freshLoad
    old.resolve(realtime)
    await oldLoad
    expect(useUsageStatsStore.getState().realtime).toEqual(freshValue)
    expect(useUsageStatsStore.getState().realtimeLoading).toBe(false)
  })
})
