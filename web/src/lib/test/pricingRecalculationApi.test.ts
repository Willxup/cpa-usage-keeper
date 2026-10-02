import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, fetchCurrentPricingRecalculation, fetchPricingRecalculationOptions, startPricingRecalculation } from '../api'
import type { PricingRecalculationOptions, PricingRecalculationTask } from '../types'

const task: PricingRecalculationTask = {
  task_id: 'task-1', status: 'running', stage: 'events',
  start_at: '2026-09-22T10:00:00+08:00', end_at: '2026-09-23T10:30:00+08:00',
  config_revision: 8, processed_count: 12, total_count: 20,
  updated_at: '2026-09-23T10:31:00+08:00', error: null,
}

const options: PricingRecalculationOptions = {
  timezone: 'Asia/Shanghai', earliest_start: '2026-08-24T11:00:00+08:00',
  latest_start: '2026-09-23T10:00:00+08:00', step_seconds: 3600,
  max_days: 30, config_revision: 8,
}

describe('pricing recalculation API', () => {
  beforeEach(() => vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper/' }))
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('loads server-defined hour choices and the one current task through admin GET routes', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(Response.json(options))
      .mockResolvedValueOnce(Response.json(task))
      .mockResolvedValueOnce(Response.json(null))
    const signal = new AbortController().signal

    expect(await fetchPricingRecalculationOptions(signal)).toEqual(options)
    expect(await fetchCurrentPricingRecalculation(signal)).toEqual(task)
    expect(await fetchCurrentPricingRecalculation(signal)).toBeNull()

    expect(fetchMock.mock.calls.map(([rawURL]) => new URL(String(rawURL), 'http://localhost').pathname)).toEqual([
      '/keeper/api/v1/pricing/recalculations/options',
      '/keeper/api/v1/pricing/recalculations/current',
      '/keeper/api/v1/pricing/recalculations/current',
    ])
    for (const [, init] of fetchMock.mock.calls) {
      expect(init).toMatchObject({ credentials: 'include', cache: 'no-store', signal })
    }
  })

  it('starts once with only the confirmed hour and saved configuration revision', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(Response.json({ started: true, task }, { status: 202 }))
      .mockResolvedValueOnce(Response.json({ started: false, task }))
    const request = { start_at: options.latest_start!, config_revision: options.config_revision }
    const signal = new AbortController().signal

    expect(await startPricingRecalculation(request, signal)).toEqual({ started: true, task })
    expect(await startPricingRecalculation(request, signal)).toEqual({ started: false, task })
    for (const [rawURL, init] of fetchMock.mock.calls) {
      expect(new URL(String(rawURL), 'http://localhost').pathname).toBe('/keeper/api/v1/pricing/recalculations')
      expect(init).toMatchObject({ method: 'POST', credentials: 'include', signal })
      expect(new Headers(init?.headers).get('Content-Type')).toBe('application/json')
      expect(new Headers(init?.headers).get('X-CPA-Usage-Keeper-Request')).toBe('fetch')
      expect(JSON.parse(String(init?.body))).toEqual(request)
    }
  })

  it('preserves pricing_changed and field errors for the confirmation UI', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(Response.json({
      code: 'pricing_changed', message: 'Pricing changed',
      fields: [{ path: 'config_revision', code: 'invalid' }],
    }, { status: 409 }))

    await expect(startPricingRecalculation({ start_at: task.start_at, config_revision: 7 })).rejects.toMatchObject({
      name: 'ApiError', status: 409, code: 'pricing_changed', message: 'Pricing changed',
      fields: [{ path: 'config_revision', code: 'invalid' }],
    } satisfies Partial<ApiError>)
  })

  it('distinguishes busy and invalid hour responses from transport failures', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(Response.json({ code: 'pricing_busy', message: 'Recalculation is running' }, { status: 409 }))
      .mockResolvedValueOnce(Response.json({
        code: 'invalid_request', message: 'Invalid start', fields: [{ path: 'start_at', code: 'invalid' }],
      }, { status: 400 }))

    const request = { start_at: task.start_at, config_revision: 8 }
    await expect(startPricingRecalculation(request)).rejects.toMatchObject({
      name: 'ApiError', status: 409, code: 'pricing_busy', message: 'Recalculation is running',
    })
    await expect(startPricingRecalculation(request)).rejects.toMatchObject({
      name: 'ApiError', status: 400, code: 'invalid_request',
      fields: [{ path: 'start_at', code: 'invalid' }],
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('forwards cancellation on all three requests without changing the error', async () => {
    const controller = new AbortController()
    controller.abort()
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (_url, init) => {
      expect(init?.signal).toBe(controller.signal)
      throw new DOMException('aborted', 'AbortError')
    })

    await expect(fetchPricingRecalculationOptions(controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
    await expect(fetchCurrentPricingRecalculation(controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
    await expect(startPricingRecalculation({ start_at: task.start_at, config_revision: 8 }, controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })
})
