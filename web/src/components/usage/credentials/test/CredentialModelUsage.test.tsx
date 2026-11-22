// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import type { UsageIdentityModel, UsageIdentityModelsRange, UsageIdentityModelsResponse } from '@/lib/types'
import { USAGE_CHART_TOKEN_COLORS } from '@/utils/usage/chartConfig'
import { CredentialModelUsage } from '../CredentialModelUsage'
import credentialStyles from '../CredentialSections.module.scss'

const model = (overrides: Partial<UsageIdentityModel> = {}): UsageIdentityModel => ({
  model: 'upstream', request_count: 2,
  input_tokens: 1000, output_tokens: 2000, reasoning_tokens: 500,
  cache_read_tokens: 200, cache_creation_tokens: 300, total_tokens: 3000,
  cost_usd: 0.4, unavailable_cost_count: 1, cost_available: false, ...overrides,
})
const response = (overrides: Partial<UsageIdentityModelsResponse> = {}): UsageIdentityModelsResponse => ({
  identity_id: '1', range: '7d', timezone: 'UTC', range_start: '2026-08-01T00:00:00Z', range_end: '2026-08-08T00:00:00Z',
  models: [model(), model({ model: 'another', request_count: 1, input_tokens: 1000, output_tokens: 0, reasoning_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, total_tokens: 1000, cost_usd: 0, unavailable_cost_count: 0, cost_available: true })],
  summary: { request_count: 3, input_tokens: 2000, output_tokens: 2000, reasoning_tokens: 500, cache_read_tokens: 200, cache_creation_tokens: 300, total_tokens: 4000, cost_usd: 0.4, unavailable_cost_count: 1, cost_available: false }, ...overrides,
})
const json = (body: unknown, status = 200) => Response.json(body, { status })

function pointer(target: Element, type: string, pointerType: string) {
  target.dispatchEvent(new PointerEvent(type, { bubbles: true, pointerType }))
}

function setNameWidths(target: Element, scrollWidth: number, clientWidth: number) {
  Object.defineProperties(target, {
    scrollWidth: { configurable: true, value: scrollWidth },
    clientWidth: { configurable: true, value: clientWidth },
  })
}

function expectTokenTooltipColors(tooltip: Element) {
  const parts = [
    ['input', USAGE_CHART_TOKEN_COLORS.input.base],
    ['cacheRead', USAGE_CHART_TOKEN_COLORS.cacheRead.base],
    ['cacheCreation', USAGE_CHART_TOKEN_COLORS.cacheWrite.base],
    ['output', USAGE_CHART_TOKEN_COLORS.output.base],
    ['reasoning', USAGE_CHART_TOKEN_COLORS.reasoning.base],
  ] as const
  const dots = [...tooltip.querySelectorAll<HTMLElement>('[data-token-tooltip-part]')]
  expect(dots).toHaveLength(parts.length)
  parts.forEach(([key, color], index) => {
    const reference = document.createElement('i')
    reference.style.background = color
    expect(dots[index].dataset.tokenTooltipPart).toBe(key)
    expect(dots[index].style.background).toBe(reference.style.background)
    expect(dots[index].getAttribute('aria-hidden')).toBe('true')
  })
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
    vi.unstubAllGlobals()
    await i18n.changeLanguage('en')
    expect(unexpectedRequests).toEqual([])
  })
  const render = async (id = '1', onAuthRequired?: () => void) => {
    await act(async () => root.render(<CredentialModelUsage identityId={id} onAuthRequired={onAuthRequired} />))
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

  it('renders model names, exclusive token parts, share, totals and incomplete calculated costs', async () => {
    vi.mocked(globalThis.fetch).mockResolvedValueOnce(json(response()))
    await render()
    expect((container.querySelector('[data-model-range="7d"]') as HTMLButtonElement).getAttribute('aria-pressed')).toBe('true')
    const rows = [...container.querySelectorAll('tbody tr')]
    expect(rows.map(row => row.querySelector('th')?.textContent)).toEqual(['upstream', 'another'])
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
    const summaryTarget = container.querySelector('tfoot [data-token-breakdown]')!
    expect(summaryTarget.getAttribute('aria-label')).toContain('Range total; Total Tokens: 4.00K')
    for (const line of ['Input: 2.00K', 'Cache Read: 200', 'Cache Write: 300', 'Output: 2.00K', 'Reasoning: 500']) {
      expect(summaryTarget.getAttribute('aria-label')).toContain(line)
    }
    expect([...summaryTarget.querySelectorAll('[data-token-part]')].map(part => [part.getAttribute('data-token-part'), part.getAttribute('data-token-value')])).toEqual([
      ['input', '1500'], ['cacheRead', '200'], ['cacheCreation', '300'], ['output', '1500'], ['reasoning', '500'],
    ])
    expect([...summaryTarget.querySelectorAll<HTMLElement>('[data-token-part]')].map(part => parseFloat(part.style.width)).reduce((a, b) => a + b, 0)).toBeCloseTo(100)
    expect(container.querySelector('tfoot')?.textContent).not.toContain('100.0%')
  })

  it('places a single summary breakdown after the four totals on mobile', async () => {
    vi.spyOn(window, 'matchMedia').mockImplementation(query => ({
      matches: query === '(max-width: 768px)', media: query,
      addEventListener: vi.fn(), removeEventListener: vi.fn(),
    }) as unknown as MediaQueryList)
    vi.mocked(globalThis.fetch).mockResolvedValueOnce(json(response()))
    await render()
    const footerRows = container.querySelectorAll('tfoot tr')
    expect(footerRows).toHaveLength(2)
    expect(footerRows[0].querySelectorAll('td')).toHaveLength(4)
    expect(footerRows[0].querySelector('[data-token-breakdown]')).toBeNull()
    expect(footerRows[1].querySelector<HTMLTableCellElement>('td')?.colSpan).toBe(5)
    expect(container.querySelectorAll('tfoot [data-token-breakdown]')).toHaveLength(1)
    expect(footerRows[1].querySelector('[data-token-breakdown]')?.getAttribute('aria-label')).toContain('Range total; Total Tokens: 4.00K')
  })

  it('shows zero summary composition without a synthetic percentage', async () => {
    const empty = { ...response().summary, input_tokens: 0, output_tokens: 0, reasoning_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, total_tokens: 0 }
    vi.mocked(globalThis.fetch).mockResolvedValueOnce(json(response({ models: [model({ ...empty })], summary: empty })))
    await render()
    const footer = container.querySelector('tfoot')!
    const summaryTarget = footer.querySelector('[data-token-breakdown]')!
    expect(summaryTarget.getAttribute('aria-label')).toContain('Range total; Total Tokens: 0')
    expect([...summaryTarget.querySelectorAll<HTMLElement>('[data-token-part]')].map(part => part.style.width)).toEqual(['0%', '0%', '0%', '0%', '0%'])
    expect(footer.textContent).not.toContain('100.0%')
  })

  it('updates the open summary tooltip language and dismisses it on range changes and disable', async () => {
    const pending = Promise.withResolvers<Response>()
    vi.mocked(globalThis.fetch).mockResolvedValueOnce(json(response())).mockReturnValueOnce(pending.promise)
    await render()
    const target = () => container.querySelector<HTMLElement>('tfoot [data-token-breakdown]')!
    await act(async () => target().focus())
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toContain('Range total')
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toContain('Cache Read: 200')
    expect(document.body.querySelector('[role="tooltip"] strong')?.textContent).toBe('Range total')
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toContain('Input: 2.00K')
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toContain('Output: 2.00K')
    expectTokenTooltipColors(document.body.querySelector('[role="tooltip"]')!)
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.body.querySelector('[role="tooltip"] strong')?.textContent).toBe('时段合计')
    expectTokenTooltipColors(document.body.querySelector('[role="tooltip"]')!)
    await selectRange('today')
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
    expect(target().tabIndex).toBe(-1)
    await act(async () => pending.resolve(json(response({ range: 'today' }))))
    await act(async () => { target().blur(); target().focus() })
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toContain('时段合计')
    await act(async () => root.render(<CredentialModelUsage identityId="1" enabled={false} />))
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
    expect(target().tabIndex).toBe(-1)
  })

  it.each([
    ['en', 'Token share', 'Cache Rate'],
    ['zh', 'Token 占比', '缓存率'],
    ['zh-TW', 'Token 占比', '快取率'],
  ] as const)('shows translated cache labels and accessible token shares in %s', async (language, label, cacheLabel) => {
    vi.mocked(globalThis.fetch).mockResolvedValueOnce(json(response()))
    await i18n.changeLanguage(language)
    await render()
    const shares = [...container.querySelectorAll('tbody tr')].map(row => row.querySelector('td:nth-child(3) > div > span:last-child')!)
    expect(shares.map(share => share.querySelector('[aria-hidden="true"]')?.textContent)).toEqual(['75.0%', '25.0%'])
    expect(shares[0].firstElementChild?.textContent).toBe(`${label}: 75.0%`)
    expect(container.querySelector('thead th:nth-child(4)')?.textContent).toBe(cacheLabel)
    expect(container.querySelector('tbody td:nth-child(4) > span:first-child')?.textContent?.trim()).toBe(cacheLabel)
    expect(container.querySelector('tfoot td:nth-child(4) > span:first-child')?.textContent?.trim()).toBe(cacheLabel)
  })

  it('shows per-model cache rates and the input-weighted total with shared color thresholds', async () => {
    const cacheModel = (name: string, input: number, cached: number, output = 0) => model({
      model: name, input_tokens: input, cache_read_tokens: cached,
      cache_creation_tokens: 0, output_tokens: output, reasoning_tokens: 0, total_tokens: input + output,
    })
    const data = response({
      models: [cacheModel('eighty', 100, 80), cacheModel('fifty', 900, 450), cacheModel('zero', 100, 0), cacheModel('no-input', 0, 0, 50)],
      summary: { ...response().summary, request_count: 8, input_tokens: 1100, cache_read_tokens: 530, cache_creation_tokens: 0, output_tokens: 50, reasoning_tokens: 0, total_tokens: 1150, cost_usd: 1.6, unavailable_cost_count: 4 },
    })
    vi.mocked(globalThis.fetch).mockResolvedValueOnce(json(data))
    await render()

    const rateFor = (name: string) => [...container.querySelectorAll('tbody tr')].find(row => row.querySelector('th')?.textContent === name)?.querySelector('[data-model-cache-rate]')
    expect([rateFor('eighty')?.textContent, rateFor('fifty')?.textContent, rateFor('zero')?.textContent, rateFor('no-input')?.textContent]).toEqual(['80.00%', '50.00%', '0.00%', '—'])
    expect(rateFor('eighty')?.className).toBe(credentialStyles.credentialMetricValueSuccess)
    expect(rateFor('fifty')?.className).toBe(credentialStyles.credentialMetricValueWarning)
    expect(rateFor('zero')?.className).toBe(credentialStyles.credentialMetricValueDanger)
    expect(rateFor('no-input')?.className).toBe(credentialStyles.credentialMetricValueNeutral)
    const total = container.querySelector('tfoot [data-model-cache-rate]')
    expect(total?.textContent).toBe('48.18%')
    expect(total?.className).toBe(credentialStyles.credentialMetricValueDanger)
  })

  it('cleans old requests and data across range and identity changes, even if fetch ignores abort', async () => {
    const oldRange = Promise.withResolvers<Response>()
    const oldIdentity = Promise.withResolvers<Response>()
    const current = Promise.withResolvers<Response>()
    const lastRange = Promise.withResolvers<Response>()
    const fetchMock = vi.mocked(globalThis.fetch).mockReturnValueOnce(oldRange.promise).mockReturnValueOnce(oldIdentity.promise).mockReturnValueOnce(current.promise).mockReturnValueOnce(lastRange.promise)
    const onAuthRequired = vi.fn()
    await render('1', onAuthRequired)
    expect(container.textContent).toContain('Loading...')
    expect(new URL(String(fetchMock.mock.calls[0][0]), 'http://localhost').searchParams.get('range')).toBe('7d')
    await selectRange('today')
    expect(fetchMock.mock.calls[0][1]?.signal?.aborted).toBe(true)
    expect(container.textContent).toContain('Loading...')
    await render('2', onAuthRequired)
    expect(fetchMock.mock.calls[1][1]?.signal?.aborted).toBe(true)
    const newURL = new URL(String(fetchMock.mock.calls[2][0]), 'http://localhost')
    expect(newURL.pathname).toBe('/api/v1/usage/identities/2/models')
    expect(newURL.searchParams.get('range')).toBe('7d')
    expect(container.querySelector('tbody')).toBeNull()
    await act(async () => { current.resolve(json(response({ identity_id: '2', models: [model({ model: 'current' })] }))) })
    expect(container.textContent).toContain('current')
    await act(async () => { oldRange.resolve(json(response())); oldIdentity.resolve(json({ error: 'expired' }, 401)) })
    expect(onAuthRequired).not.toHaveBeenCalled()
    expect(container.textContent).toContain('current')
    expect(container.textContent).not.toContain('upstream')
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
    expect(container.textContent).toContain('upstream')
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
    await render('1', onAuthRequired)
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
    expect(tooltip.querySelector('strong')?.textContent).toBe('upstream')
    expectTokenTooltipColors(tooltip)
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

  it('only exposes overflowing names and supports mouse, keyboard, touch and resize dismissal', async () => {
    const observers = new Map<Element, () => void>()
    class TestResizeObserver {
      private target: Element | null = null
      constructor(private readonly callback: ResizeObserverCallback) {}
      observe(target: Element) {
        this.target = target
        observers.set(target, () => this.callback([], this as unknown as ResizeObserver))
      }
      disconnect() { if (this.target) observers.delete(this.target) }
    }
    vi.stubGlobal('ResizeObserver', TestResizeObserver)
    const longName = 'a-very-long-upstream-model-identifier'
    vi.mocked(globalThis.fetch).mockResolvedValueOnce(json(response({ models: [model({ model: longName }), model({ model: 'short' })] })))
    await render()
    const rows = [...container.querySelectorAll('tbody tr')]
    const primary = rows[0].querySelector<HTMLElement>('[data-model-name="model"]')!
    const short = rows[1].querySelector<HTMLElement>('[data-model-name="model"]')!
    expect(primary.textContent).toBe(longName)
    expect(short.getAttribute('tabindex')).toBeNull()
    await act(async () => pointer(short, 'pointerover', 'mouse'))
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()

    setNameWidths(primary, 300, 100)
    await act(async () => observers.get(primary)?.())
    expect(primary.getAttribute('tabindex')).toBe('0')
    await act(async () => pointer(primary, 'pointerover', 'mouse'))
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toBe(longName)
    await act(async () => { pointer(primary, 'pointerdown', 'mouse'); primary.focus(); primary.click(); pointer(primary, 'pointerout', 'mouse') })
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()

    await act(async () => { primary.blur(); primary.focus() })
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toBe(longName)
    const bubble = vi.fn()
    document.addEventListener('keydown', bubble)
    await act(async () => primary.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })))
    document.removeEventListener('keydown', bubble)
    expect(bubble).not.toHaveBeenCalled()
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()

    await act(async () => { primary.blur(); pointer(primary, 'pointerdown', 'touch'); primary.focus(); primary.click() })
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toBe(longName)
    await act(async () => { pointer(primary, 'pointerdown', 'touch'); primary.click() })
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
    await act(async () => { pointer(primary, 'pointerdown', 'touch'); primary.click() })
    await act(async () => pointer(document.body, 'pointerdown', 'touch'))
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()

    await act(async () => { primary.blur(); primary.focus() })
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toBe(longName)
    setNameWidths(primary, 80, 100)
    await act(async () => observers.get(primary)?.())
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
    expect(primary.getAttribute('tabindex')).toBeNull()
    setNameWidths(primary, 300, 100)
    await act(async () => window.dispatchEvent(new Event('resize')))
    expect(primary.getAttribute('tabindex')).toBe('0')
    setNameWidths(primary, 80, 100)
    await act(async () => window.dispatchEvent(new Event('resize')))
    expect(primary.getAttribute('tabindex')).toBeNull()
  })

  it('cleans name tooltips while ranges load, on disable and across identities', async () => {
    const pending = Promise.withResolvers<Response>()
    const data = response({ models: [model({ model: 'long-upstream' })] })
    vi.mocked(globalThis.fetch).mockResolvedValueOnce(json(data)).mockReturnValueOnce(pending.promise)
      .mockResolvedValueOnce(json({ ...data, range: 'today' })).mockResolvedValueOnce(json({ ...data, identity_id: '2' }))
    await render()
    const name = () => container.querySelector<HTMLElement>('[data-model-name="model"]')!
    setNameWidths(name(), 300, 100)
    await act(async () => window.dispatchEvent(new Event('resize')))
    await act(async () => name().focus())
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toBe('long-upstream')
    await selectRange('today')
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
    expect(name().getAttribute('tabindex')).toBeNull()
    await act(async () => pending.resolve(json({ ...data, range: 'today' })))
    await act(async () => { window.dispatchEvent(new Event('resize')); name().blur(); name().focus() })
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toBe('long-upstream')
    await act(async () => root.render(<CredentialModelUsage identityId="1" enabled={false} />))
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
    expect(name().getAttribute('tabindex')).toBeNull()
    await render()
    await act(async () => { window.dispatchEvent(new Event('resize')); name().blur(); name().focus() })
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toBe('long-upstream')
    const old = name()
    await render('2')
    expect(old.isConnected).toBe(false)
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
  })

  it.each(['zh', 'zh-TW'] as const)('renders translated empty and range controls in %s', async language => {
    vi.mocked(globalThis.fetch).mockResolvedValueOnce(json(response({ models: [] })))
    await i18n.changeLanguage(language)
    await act(async () => root.render(<CredentialModelUsage identityId="1" />))
    expect(container.textContent).toContain(language === 'zh' ? '暂无已汇总用量' : '暫無已彙總用量')
    expect(container.textContent).toContain('7')
  })
})
