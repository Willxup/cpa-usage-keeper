// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import type { UsageIdentityModel, UsageIdentityModelsRange, UsageIdentityModelsResponse } from '@/lib/types'
import { CredentialModelUsage } from '../CredentialModelUsage'

const model = (overrides: Partial<UsageIdentityModel> = {}): UsageIdentityModel => ({
  model: 'upstream', model_alias: 'public', request_count: 2,
  input_tokens: 1000, output_tokens: 2000, reasoning_tokens: 500,
  cache_read_tokens: 200, cache_creation_tokens: 300, total_tokens: 3000,
  cost_usd: 0.4, unavailable_cost_count: 1, cost_available: false, ...overrides,
})
const response = (overrides: Partial<UsageIdentityModelsResponse> = {}): UsageIdentityModelsResponse => ({
  identity_id: '1', range: '7d', timezone: 'UTC', range_start: '2026-08-01T00:00:00Z', range_end: '2026-08-08T00:00:00Z',
  models: [model(), model({ model_alias: 'another', request_count: 1, input_tokens: 1000, output_tokens: 0, reasoning_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, total_tokens: 1000, cost_usd: 0, unavailable_cost_count: 0, cost_available: true })],
  summary: { request_count: 3, input_tokens: 2000, output_tokens: 2000, reasoning_tokens: 500, cache_read_tokens: 200, cache_creation_tokens: 300, total_tokens: 4000, cost_usd: 0.4, unavailable_cost_count: 1, cost_available: false }, ...overrides,
})
const json = (body: unknown, status = 200) => Response.json(body, { status })

function pointer(target: Element, type: string, pointerType: string) {
  target.dispatchEvent(new PointerEvent(type, { bubbles: true, pointerType }))
}

describe('CredentialModelUsage', () => {
  let container: HTMLDivElement
  let root: Root
  let unexpectedRequests: string[]
  beforeEach(async () => {
    unexpectedRequests = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      unexpectedRequests.push(String(input))
      throw new Error('Unexpected fetch: configure the request in this test')
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
  const render = async (id = '1', kind: 'credential' | 'ai-provider' = 'ai-provider', onAuthRequired?: () => void) => {
    await act(async () => root.render(<CredentialModelUsage identityId={id} kind={kind} onAuthRequired={onAuthRequired} />))
  }
  const selectRange = async (range: UsageIdentityModelsRange) => {
    await act(async () => (container.querySelector(`[data-model-range="${range}"]`) as HTMLButtonElement).click())
  }

  it('rejects any unconfigured request without reaching the network', async () => {
    expect(vi.isMockFunction(globalThis.fetch)).toBe(true)
    await expect(globalThis.fetch('/unexpected-test-request')).rejects.toThrow('Unexpected fetch')
    expect(unexpectedRequests).toEqual(['/unexpected-test-request'])
    unexpectedRequests.length = 0
  })

  it('renders real alias tuples, exclusive token parts, share, totals and incomplete calculated costs', async () => {
    vi.mocked(globalThis.fetch).mockResolvedValueOnce(json(response()))
    await render()
    expect((container.querySelector('[data-model-range="7d"]') as HTMLButtonElement).getAttribute('aria-pressed')).toBe('true')
    const rows = [...container.querySelectorAll('tbody tr')]
    expect(rows).toHaveLength(2)
    expect(rows[0].textContent).toContain('public')
    expect(rows[0].textContent).toContain('→ upstream')
    expect(rows[1].textContent).toContain('another')
    expect(rows[0].textContent).toContain('75.0%')
    expect(rows[0].textContent).toContain('$0.4000')
    expect(rows[1].textContent).toContain('$0.0000')
    expect(rows[1].querySelector('abbr')).toBeNull()
    expect(rows[0].querySelector('abbr')?.getAttribute('title')).toContain('zero does not mean free')
    expect(container.querySelector('tfoot')?.textContent).toContain('4.00K')
    expect(container.querySelector('tfoot abbr')).not.toBeNull()
    const parts = [...rows[0].querySelectorAll('[data-token-part]')]
    expect(parts.map(part => [part.getAttribute('data-token-part'), part.getAttribute('data-token-value')])).toEqual([
      ['input', '500'], ['cacheRead', '200'], ['cacheCreation', '300'], ['output', '1500'], ['reasoning', '500'],
    ])
    expect(parts.map(part => parseFloat((part as HTMLElement).style.width)).reduce((a, b) => a + b, 0)).toBeCloseTo(100)
  })

  it('cleans old requests and data across range and identity changes, even if fetch ignores abort', async () => {
    const oldRange = Promise.withResolvers<Response>()
    const oldIdentity = Promise.withResolvers<Response>()
    const current = Promise.withResolvers<Response>()
    const lastRange = Promise.withResolvers<Response>()
    const fetchMock = vi.mocked(globalThis.fetch).mockReturnValueOnce(oldRange.promise).mockReturnValueOnce(oldIdentity.promise).mockReturnValueOnce(current.promise).mockReturnValueOnce(lastRange.promise)
    const onAuthRequired = vi.fn()
    await render('1', 'credential', onAuthRequired)
    expect(container.textContent).toContain('Loading...')
    expect(new URL(String(fetchMock.mock.calls[0][0]), 'http://localhost').searchParams.get('range')).toBe('7d')
    await selectRange('today')
    expect(fetchMock.mock.calls[0][1]?.signal?.aborted).toBe(true)
    expect(container.textContent).toContain('Loading...')
    await render('2', 'ai-provider', onAuthRequired)
    expect(fetchMock.mock.calls[1][1]?.signal?.aborted).toBe(true)
    const newURL = new URL(String(fetchMock.mock.calls[2][0]), 'http://localhost')
    expect(newURL.pathname).toBe('/api/v1/usage/identities/2/models')
    expect(newURL.searchParams.get('range')).toBe('7d')
    expect(container.querySelector('tbody')).toBeNull()
    await act(async () => { current.resolve(json(response({ identity_id: '2', models: [model({ model_alias: 'current' })] }))) })
    expect(container.textContent).toContain('current')
    await act(async () => { oldRange.resolve(json(response())); oldIdentity.resolve(json({ error: 'expired' }, 401)) })
    expect(onAuthRequired).not.toHaveBeenCalled()
    expect(container.textContent).toContain('current')
    expect(container.textContent).not.toContain('public')
    await selectRange('30d')
    expect(fetchMock.mock.calls).toHaveLength(4)
    expect(new URL(String(fetchMock.mock.calls[3][0]), 'http://localhost').searchParams.get('range')).toBe('30d')
  })

  it('shows loading, cost gate and ordinary error with retry, then aggregated empty state', async () => {
    const pending = Promise.withResolvers<Response>()
    const fetchMock = vi.mocked(globalThis.fetch).mockReturnValueOnce(pending.promise)
      .mockResolvedValueOnce(json({ error: 'unavailable' }, 500))
      .mockResolvedValueOnce(json(response({ models: [] })))
    await render()
    expect(container.textContent).toContain('Loading...')
    await act(async () => { pending.resolve(json({ error: 'unavailable', code: 'costs_busy' }, 503)) })
    expect(container.textContent).toContain('Cost statistics are being updated')
    await act(async () => [...container.querySelectorAll('button')].find(button => button.textContent === 'Retry')!.click())
    expect(container.textContent).toContain('Failed to load model usage.')
    await act(async () => [...container.querySelectorAll('button')].find(button => button.textContent === 'Retry')!.click())
    expect(container.textContent).toContain('No aggregated usage yet.')
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('distinguishes unpriced zero from free, and keeps the displayed range while a new range loads', async () => {
    const pending = Promise.withResolvers<Response>()
    const data = response({ models: [model({ cost_usd: 0, unavailable_cost_count: 2, cost_available: false })] })
    vi.mocked(globalThis.fetch).mockResolvedValueOnce(json(data)).mockReturnValueOnce(pending.promise)
    await render()
    const row = container.querySelector('tbody tr')!
    expect(row.textContent).toContain('$0.0000')
    expect(row.querySelector('abbr')?.getAttribute('title')).toContain('zero does not mean free')
    await selectRange('today')
    expect(container.textContent).toContain('public')
    expect(container.querySelector('[data-model-range="7d"]')?.getAttribute('aria-pressed')).toBe('true')
    expect(container.querySelector('[data-model-range="today"]')?.getAttribute('aria-pressed')).toBe('false')
    expect(container.querySelector('[data-model-range="today"]')?.getAttribute('aria-busy')).toBe('true')
    await act(async () => { pending.resolve(json(response({ range: 'today', models: [] }))) })
    expect(container.textContent).toContain('No aggregated usage yet.')
    expect(container.querySelector('[data-model-range="today"]')?.getAttribute('aria-pressed')).toBe('true')
  })

  it('notifies authentication failure and aborts an in-flight request on unmount', async () => {
    const pending = Promise.withResolvers<Response>()
    const fetchMock = vi.mocked(globalThis.fetch).mockResolvedValueOnce(json({ error: 'expired' }, 401)).mockReturnValueOnce(pending.promise)
    const onAuthRequired = vi.fn()
    await render('1', 'credential', onAuthRequired)
    expect(onAuthRequired).toHaveBeenCalledTimes(1)
    await selectRange('today')
    await act(async () => root.unmount())
    expect(fetchMock.mock.calls[1][1]?.signal?.aborted).toBe(true)
    await act(async () => pending.resolve(json({ error: 'expired' }, 401)))
    expect(onAuthRequired).toHaveBeenCalledTimes(1)
    root = createRoot(container)
  })

  it('uses Portal tooltip on hover, mouse click/leave, keyboard, Escape, touch and outside press', async () => {
    vi.mocked(globalThis.fetch).mockResolvedValueOnce(json(response()))
    await render()
    const target = container.querySelector('[data-token-breakdown]') as HTMLElement
    await act(async () => pointer(target, 'pointerover', 'mouse'))
    expect(document.body.querySelector('[role="tooltip"]')?.parentElement).toBe(document.body)
    const tooltip = document.body.querySelector('[role="tooltip"]')!
    expect(tooltip.textContent).toContain('Total Tokens: 3.00K')
    expect(tooltip.textContent).toContain('Input: 1.00K')
    expect(tooltip.textContent).toContain('Cache Read: 200')
    expect(tooltip.textContent).toContain('Cache Write: 300')
    expect(tooltip.textContent).toContain('Output: 2.00K')
    expect(tooltip.textContent).toContain('Reasoning: 500')
    expect(tooltip.textContent).not.toContain('(')
    await act(async () => { pointer(target, 'pointerdown', 'mouse'); target.focus(); target.click(); pointer(target, 'pointerout', 'mouse') })
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
    await act(async () => { target.blur(); target.focus() })
    expect(document.body.querySelector('[role="tooltip"]')).not.toBeNull()
    const bubble = vi.fn()
    document.addEventListener('keydown', bubble)
    await act(async () => target.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })))
    document.removeEventListener('keydown', bubble)
    expect(bubble).not.toHaveBeenCalled()
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
    await act(async () => { target.blur(); pointer(target, 'pointerdown', 'touch'); target.focus(); target.click() })
    expect(document.body.querySelector('[role="tooltip"]')).not.toBeNull()
    await act(async () => pointer(document.body, 'pointerdown', 'touch'))
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
    await act(async () => { target.blur(); target.focus() })
    expect(document.body.querySelector('[role="tooltip"]')).not.toBeNull()
    await act(async () => target.blur())
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
  })

  it('cleans the tooltip on range or identity switch and displays zero-valued tooltip fields', async () => {
    vi.mocked(globalThis.fetch).mockResolvedValueOnce(json(response())).mockResolvedValueOnce(json(response({ range: '30d' }))).mockResolvedValueOnce(json(response({ identity_id: '2' })))
    await render()
    const targets = container.querySelectorAll<HTMLElement>('[data-token-breakdown]')
    await act(async () => targets[1].focus())
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toContain('Reasoning: 0')
    await selectRange('30d')
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
    await act(async () => container.querySelector<HTMLElement>('[data-token-breakdown]')!.focus())
    expect(document.body.querySelector('[role="tooltip"]')).not.toBeNull()
    await render('2')
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
    await act(async () => container.querySelector<HTMLElement>('[data-token-breakdown]')!.focus())
    expect(document.body.querySelector('[role="tooltip"]')).not.toBeNull()
    await act(async () => root.unmount())
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
    root = createRoot(container)
  })

  it.each(['zh', 'zh-TW'] as const)('renders translated empty and range controls in %s', async language => {
    vi.mocked(globalThis.fetch).mockResolvedValueOnce(json(response({ models: [] })))
    await i18n.changeLanguage(language)
    await act(async () => root.render(<CredentialModelUsage identityId="1" kind="credential" />))
    expect(container.textContent).toContain(language === 'zh' ? '暂无已汇总用量' : '暫無已彙總用量')
    expect(container.textContent).toContain('7')
  })
})
