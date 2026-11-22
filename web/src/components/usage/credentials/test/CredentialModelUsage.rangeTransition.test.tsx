// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import type { UsageIdentityModel, UsageIdentityModelsRange, UsageIdentityModelsResponse } from '@/lib/types'
import { CredentialModelUsage } from '../CredentialModelUsage'

const model = (index: number): UsageIdentityModel => ({
  model: `upstream-${index}`, request_count: 1,
  input_tokens: 1000, output_tokens: 500, reasoning_tokens: 100,
  cache_read_tokens: 200, cache_creation_tokens: 100, total_tokens: 1500,
  cost_usd: 1, unavailable_cost_count: 0, cost_available: true,
})
const modelsResponse = (range: UsageIdentityModelsRange, count: number, identityId = '1'): UsageIdentityModelsResponse => ({
  identity_id: identityId, range, timezone: 'UTC', range_start: '2026-08-01T00:00:00Z', range_end: '2026-08-08T00:00:00Z',
  models: Array.from({ length: count }, (_, index) => model(index)),
  summary: { ...model(0), request_count: count, total_tokens: count * 1500, cost_usd: count },
})
const json = (body: unknown, status = 200) => Response.json(body, { status })

describe('CredentialModelUsage range transitions', () => {
  let container: HTMLDivElement
  let root: Root
  let unexpectedRequests: string[]
  beforeEach(async () => {
    unexpectedRequests = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      unexpectedRequests.push(String(input))
      throw new Error('Unexpected fetch')
    })
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    await i18n.changeLanguage('en')
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })
  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    vi.restoreAllMocks()
    await i18n.changeLanguage('en')
    expect(unexpectedRequests).toEqual([])
  })
  const render = async (identityId = '1', enabled = true, onAuthRequired?: () => void) => {
    await act(async () => root.render(<div className="modal-body"><CredentialModelUsage identityId={identityId} enabled={enabled} onAuthRequired={onAuthRequired} /></div>))
  }
  const selectRange = async (range: UsageIdentityModelsRange) => {
    await act(async () => container.querySelector<HTMLButtonElement>(`[data-model-range="${range}"]`)!.click())
  }
  const pressed = (range: UsageIdentityModelsRange) => container.querySelector(`[data-model-range="${range}"]`)?.getAttribute('aria-pressed')

  it('preserves a scrolled viewport and the displayed rows through slow many-to-few-to-empty transitions', async () => {
    const today = Promise.withResolvers<Response>()
    const month = Promise.withResolvers<Response>()
    vi.mocked(globalThis.fetch).mockResolvedValueOnce(json(modelsResponse('7d', 8))).mockReturnValueOnce(today.promise).mockReturnValueOnce(month.promise)
    await render()
    const scroller = container.querySelector<HTMLElement>('.modal-body')!
    const panel = container.querySelector<HTMLElement>('[data-credential-model-usage]')!
    // happy-dom has no layout: model the real scroll container and clamp scrollTop when content shrinks.
    const panelHeight = () => {
      const natural = panel.querySelector('table') ? 100 + panel.querySelectorAll('tbody tr').length * 100 : 100
      return Math.max(natural, Number.parseFloat(panel.style.minHeight) || 0)
    }
    vi.spyOn(panel, 'getBoundingClientRect').mockImplementation(() => new DOMRect(0, 0, 800, panelHeight()))
    let scrollTop = 0
    Object.defineProperties(scroller, {
      clientHeight: { configurable: true, get: () => 400 },
      scrollHeight: { configurable: true, get: () => 1000 + panelHeight() },
      scrollTop: { configurable: true, get: () => { scrollTop = Math.min(scrollTop, Math.max(0, scroller.scrollHeight - scroller.clientHeight)); return scrollTop }, set: (value: number) => { scrollTop = value } },
    })
    scroller.scrollTop = 900
    expect(scroller.scrollTop).toBe(900)
    const originalTable = panel.querySelector('table')
    const originalRow = panel.querySelector('tbody tr')
    await act(async () => panel.querySelector<HTMLElement>('[data-token-breakdown]')!.focus())
    expect(document.body.querySelector('[role="tooltip"]')).not.toBeNull()

    await selectRange('today')
    expect(scroller.scrollTop).toBe(900)
    expect(panel.querySelector('table')).toBe(originalTable)
    expect(panel.querySelectorAll('tbody tr')).toHaveLength(8)
    expect(panel.querySelector('tfoot')?.textContent).toContain('12.00K')
    expect(pressed('7d')).toBe('true')
    expect(pressed('today')).toBe('false')
    expect(container.querySelector('[data-model-range="today"]')?.getAttribute('aria-busy')).toBe('true')
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()

    const oneModel = modelsResponse('today', 1)
    oneModel.models[0] = { ...oneModel.models[0], input_tokens: 7000, total_tokens: 7500 }
    oneModel.summary = { ...oneModel.summary, total_tokens: 7500 }
    await act(async () => today.resolve(json(oneModel)))
    expect(pressed('today')).toBe('true')
    expect(panel.querySelectorAll('tbody tr')).toHaveLength(1)
    expect(panel.querySelector('tbody tr')).toBe(originalRow)
    expect(panel.querySelector('tfoot')?.textContent).toContain('7.50K')
    expect(scroller.scrollTop).toBe(900)
    expect(scroller.scrollHeight).toBe(1300)
    const target = panel.querySelector<HTMLElement>('[data-token-breakdown]')!
    await act(async () => { target.blur(); target.focus() })
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toContain('Input: 7.00K')
    await selectRange('30d')
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
    expect(pressed('today')).toBe('true')
    expect(panel.querySelectorAll('tbody tr')).toHaveLength(1)
    await act(async () => month.resolve(json(modelsResponse('30d', 0))))
    expect(pressed('30d')).toBe('true')
    expect(panel.textContent).toContain('No aggregated usage yet.')
    expect(scroller.scrollTop).toBe(900)
    expect(scroller.scrollHeight).toBe(1300)
    await act(async () => { scroller.scrollTop = 800; scroller.dispatchEvent(new Event('scroll')) })
    expect(scroller.scrollTop).toBe(800)
    expect(scroller.scrollHeight).toBe(1200)
    await act(async () => { scroller.scrollTop = 0; scroller.dispatchEvent(new Event('scroll')) })
    expect(scroller.scrollHeight).toBe(1100)
  })

  it('does not reserve height when the viewport is already at the top', async () => {
    const pending = Promise.withResolvers<Response>()
    vi.mocked(globalThis.fetch).mockResolvedValueOnce(json(modelsResponse('7d', 2))).mockReturnValueOnce(pending.promise)
    await render()
    const panel = container.querySelector<HTMLElement>('[data-credential-model-usage]')!
    const scroller = panel.closest<HTMLElement>('.modal-body')!
    vi.spyOn(panel, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, 800, 250))
    Object.defineProperties(scroller, {
      clientHeight: { configurable: true, value: 600 },
      scrollHeight: { configurable: true, value: 600 },
      scrollTop: { configurable: true, value: 0 },
    })
    await selectRange('today')
    await act(async () => pending.resolve(json(modelsResponse('today', 0))))
    expect(panel.style.minHeight).toBe('')
    expect(panel.textContent).toContain('No aggregated usage yet.')
  })

  it('accepts only the last of rapid range clicks and ignores an aborted late response', async () => {
    const today = Promise.withResolvers<Response>()
    const month = Promise.withResolvers<Response>()
    const fetchMock = vi.mocked(globalThis.fetch).mockResolvedValueOnce(json(modelsResponse('7d', 2))).mockReturnValueOnce(today.promise).mockReturnValueOnce(month.promise)
    const onAuthRequired = vi.fn()
    await render('1', true, onAuthRequired)
    await selectRange('today')
    await selectRange('30d')
    expect(fetchMock.mock.calls[1][1]?.signal?.aborted).toBe(true)
    expect(pressed('7d')).toBe('true')
    expect(container.querySelector('[data-model-range="30d"]')?.getAttribute('aria-busy')).toBe('true')
    await act(async () => month.resolve(json(modelsResponse('30d', 1))))
    expect(pressed('30d')).toBe('true')
    await act(async () => today.resolve(json({ error: 'expired' }, 401)))
    expect(pressed('30d')).toBe('true')
    expect(container.querySelectorAll('tbody tr')).toHaveLength(1)
    expect(onAuthRequired).not.toHaveBeenCalled()
  })

  it('marks a fresh request pending when rapid clicks return to the displayed range', async () => {
    const today = Promise.withResolvers<Response>()
    const week = Promise.withResolvers<Response>()
    const fetchMock = vi.mocked(globalThis.fetch).mockResolvedValueOnce(json(modelsResponse('7d', 2))).mockReturnValueOnce(today.promise).mockReturnValueOnce(week.promise)
    await render()
    await selectRange('today')
    await selectRange('7d')
    expect(fetchMock.mock.calls[1][1]?.signal?.aborted).toBe(true)
    expect(pressed('7d')).toBe('true')
    expect(container.querySelector('[data-model-range="7d"]')?.getAttribute('aria-busy')).toBe('true')
    expect(container.querySelectorAll('tbody tr')).toHaveLength(2)
    await act(async () => week.resolve(json(modelsResponse('7d', 1))))
    await act(async () => today.resolve(json(modelsResponse('today', 3))))
    expect(pressed('7d')).toBe('true')
    expect(container.querySelectorAll('tbody tr')).toHaveLength(1)
    expect(container.querySelector('[data-model-range="7d"]')?.getAttribute('aria-busy')).toBeNull()
  })

  it('keeps the last confirmed data on ordinary failure and retry, but removes it for the cost gate and 401', async () => {
    const retry = Promise.withResolvers<Response>()
    vi.mocked(globalThis.fetch).mockResolvedValueOnce(json(modelsResponse('7d', 2)))
      .mockResolvedValueOnce(json({ error: 'offline' }, 500))
      .mockReturnValueOnce(retry.promise)
      .mockResolvedValueOnce(json({ error: 'busy', code: 'costs_busy' }, 503))
      .mockResolvedValueOnce(json({ error: 'expired' }, 401))
    const onAuthRequired = vi.fn()
    await render('1', true, onAuthRequired)
    await selectRange('today')
    expect(container.textContent).toContain('Failed to load model usage.')
    expect(container.querySelectorAll('tbody tr')).toHaveLength(2)
    expect(pressed('7d')).toBe('true')
    await act(async () => [...container.querySelectorAll('button')].find(button => button.textContent === 'Retry')!.click())
    expect(container.querySelectorAll('tbody tr')).toHaveLength(2)
    expect(pressed('7d')).toBe('true')
    await act(async () => retry.resolve(json(modelsResponse('today', 1))))
    expect(pressed('today')).toBe('true')
    expect(container.querySelectorAll('tbody tr')).toHaveLength(1)
    await selectRange('30d')
    expect(container.textContent).toContain('Cost statistics are being updated')
    expect(container.querySelector('tbody')).toBeNull()
    expect(container.textContent).not.toContain('upstream-0')
    await act(async () => [...container.querySelectorAll('button')].find(button => button.textContent === 'Retry')!.click())
    expect(onAuthRequired).toHaveBeenCalledTimes(1)
    expect(container.querySelector('tbody')).toBeNull()
  })

  it.each([401, 403])('removes previously displayed data on HTTP %s without changing authentication callbacks', async status => {
    vi.mocked(globalThis.fetch).mockResolvedValueOnce(json(modelsResponse('7d', 2))).mockResolvedValueOnce(json({ error: 'denied' }, status))
    const onAuthRequired = vi.fn()
    await render('1', true, onAuthRequired)
    expect(container.querySelectorAll('tbody tr')).toHaveLength(2)
    await selectRange('today')
    expect(onAuthRequired).toHaveBeenCalledTimes(status === 401 ? 1 : 0)
    expect(container.querySelector('tbody')).toBeNull()
    expect(container.textContent).not.toContain('upstream-0')
  })

  it('does not retain old data during a startup migration gate', async () => {
    vi.mocked(globalThis.fetch).mockResolvedValueOnce(json(modelsResponse('7d', 2))).mockResolvedValueOnce(json({ error: 'updating', code: 'migration_in_progress' }, 503))
    await render()
    await selectRange('today')
    expect(container.querySelector('tbody')).toBeNull()
    expect(container.textContent).not.toContain('upstream-0')
  })

  it('isolates identities and discards delayed results after close', async () => {
    const old = Promise.withResolvers<Response>()
    const next = Promise.withResolvers<Response>()
    const closed = Promise.withResolvers<Response>()
    const fetchMock = vi.mocked(globalThis.fetch).mockResolvedValueOnce(json(modelsResponse('7d', 2)))
      .mockReturnValueOnce(old.promise).mockReturnValueOnce(next.promise).mockReturnValueOnce(closed.promise)
    const onAuthRequired = vi.fn()
    await render('1', true, onAuthRequired)
    await selectRange('today')
    await render('2', true, onAuthRequired)
    expect(fetchMock.mock.calls[1][1]?.signal?.aborted).toBe(true)
    expect(container.querySelector('tbody')).toBeNull()
    expect(container.textContent).not.toContain('upstream-0')
    await act(async () => next.resolve(json(modelsResponse('7d', 1, '2'))))
    await act(async () => old.resolve(json({ error: 'expired' }, 401)))
    expect(onAuthRequired).not.toHaveBeenCalled()
    expect(container.querySelectorAll('tbody tr')).toHaveLength(1)
    await selectRange('30d')
    await render('2', false, onAuthRequired)
    expect(fetchMock.mock.calls[3][1]?.signal?.aborted).toBe(true)
    await act(async () => closed.resolve(json(modelsResponse('30d', 3, '2'))))
    expect(container.querySelectorAll('tbody tr')).toHaveLength(1)
    expect(pressed('7d')).toBe('true')
  })
})
