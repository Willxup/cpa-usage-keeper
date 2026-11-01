// @vitest-environment happy-dom

import { act, type ComponentProps } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Modal } from '@/components/ui/Modal'
import { credentialEventListDefaults } from './credentialEventFixtures'
import type { UsageEvent } from '@/lib/types'
import {
  CredentialRequestEventsList,
  shouldLoadMoreCredentialRequestEvents,
} from '../CredentialRequestEventsList'

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({
    t: (key: string, options?: Record<string, string>) => options
      ? `${options.label}: ${options.value}`
      : key,
  }),
}))

const event: UsageEvent = {
  id: '1',
  request_id: 'request-1',
  timestamp: '2026-08-17T10:00:00Z',
  api_key: 'Team Alpha',
  model: 'gpt-5.6',
  model_alias: 'keeper-gpt',
  reasoning_effort: 'high',
  service_tier: 'priority',
  effective_service_tier: 'ultrafast',
  response_service_tier: 'flex',
  executor_type: 'OpenAIResponsesExecutor',
  endpoint: 'POST /v1/responses',
  failed: false,
  latency_ms: 1_240,
  ttft_ms: 320,
  speed_tps: 42.5,
  client_ip: '192.0.2.10',
  x_forwarded_for: '198.51.100.7, 192.0.2.10',
  user_agent: 'Codex CLI/1.2.3',
  cost_available: true,
  cost_usd: 0.012345,
  pricing_style: 'openai',
  tokens: {
    input_tokens: 1_000,
    output_tokens: 300,
    reasoning_tokens: 80,
    cache_read_tokens: 600,
    cache_creation_tokens: 100,
    total_tokens: 1_300,
  },
}

const longApiKey = 'team-alpha-special-production-api-key-with-long-context-string-2026-08-21'

const mockOverflow = (longUserAgent = '') => {
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(100)
  vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockImplementation(function scrollWidth() {
    return this.textContent === longApiKey ? 360 : 80
  })
  vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(20)
  vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockImplementation(function scrollHeight() {
    return this.textContent === longUserAgent && longUserAgent !== '' ? 40 : 20
  })
}

const CREDENTIAL_REQUEST_TEST_ROW_HEIGHT = 80

const buildEvent = (index: number): UsageEvent => ({
  ...event,
  id: String(index + 1),
  request_id: `request-${index + 1}`,
  model: `model-${index}`,
})

class TestResizeObserver implements ResizeObserver {
  private static readonly instances = new Set<TestResizeObserver>()
  private readonly targets = new Set<Element>()

  constructor(private readonly callback: ResizeObserverCallback) {
    TestResizeObserver.instances.add(this)
  }

  private emit(target: Element) {
    const contentRect = target.getBoundingClientRect()
    this.callback([{
      target,
      contentRect,
      borderBoxSize: [{ inlineSize: contentRect.width, blockSize: contentRect.height }],
      contentBoxSize: [{ inlineSize: contentRect.width, blockSize: contentRect.height }],
      devicePixelContentBoxSize: [],
    }], this)
  }

  observe(target: Element) {
    this.targets.add(target)
    this.emit(target)
  }

  disconnect() {
    this.targets.clear()
    TestResizeObserver.instances.delete(this)
  }

  unobserve(target: Element) {
    this.targets.delete(target)
  }

  static flush() {
    for (const instance of TestResizeObserver.instances) {
      for (const target of instance.targets) {
        if (target.isConnected) instance.emit(target)
      }
    }
  }

  static reset() {
    TestResizeObserver.instances.clear()
  }
}

const readVirtualContentHeight = (scroller: HTMLElement): number => {
  const spacerHeight = Array.from(
    scroller.querySelectorAll<HTMLTableRowElement>('[data-credential-request-events-spacer]'),
  ).reduce((total, spacer) => total + (Number.parseFloat(spacer.style.height) || 0), 0)
  const renderedHeight = Array.from(
    scroller.querySelectorAll<HTMLElement>('[data-credential-request-event-group]'),
  ).reduce((total, group) => total + group.getBoundingClientRect().height, 0)
  return spacerHeight + renderedHeight
}

it('detects the dedicated list load-more boundary', () => {
  expect(shouldLoadMoreCredentialRequestEvents({ scrollTop: 1_000, clientHeight: 500, scrollHeight: 1_700 })).toBe(true)
  expect(shouldLoadMoreCredentialRequestEvents({ scrollTop: 100, clientHeight: 500, scrollHeight: 1_700 })).toBe(false)
})

describe('CredentialRequestEventsList', () => {
  let container: HTMLDivElement
  let root: Root

  const list = (props: Partial<ComponentProps<typeof CredentialRequestEventsList>> = {}) => (
    <CredentialRequestEventsList {...credentialEventListDefaults} events={[event]} {...props} />
  )
  const renderList = async (props: Partial<ComponentProps<typeof CredentialRequestEventsList>> = {}) => {
    await act(async () => root.render(list(props)))
  }
  const renderVirtualList = async (events: UsageEvent[]) => {
    await act(async () => {
      root.render(list({ events }))
      await Promise.resolve()
    })
  }
  const scrollList = async (scroller: HTMLElement, top: number) => {
    scroller.scrollTop = top
    await act(async () => {
      scroller.dispatchEvent(new Event('scroll'))
      await vi.advanceTimersByTimeAsync(0)
    })
  }
  const tooltip = () => document.body.querySelector('[role="tooltip"]')

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    vi.useFakeTimers()
    TestResizeObserver.reset()
    vi.stubGlobal('ResizeObserver', TestResizeObserver)
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
      callback(performance.now())
      return 0
    })
    vi.stubGlobal('cancelAnimationFrame', () => undefined)
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function getBoundingClientRect() {
      if (this.dataset.credentialRequestEventGroup) {
        const expanded = this.querySelector('[data-credential-request-event-details]') !== null
        return new DOMRect(0, 0, 920, expanded ? 220 : CREDENTIAL_REQUEST_TEST_ROW_HEIGHT)
      }
      if (this instanceof HTMLTableRowElement) {
        const spacerHeight = Number.parseFloat(this.style.height)
        return new DOMRect(0, 0, 920, Number.isFinite(spacerHeight) ? spacerHeight : 52)
      }
      return new DOMRect(0, 0, 920, 600)
    })
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    // 推进滚动结束回调后再销毁虚拟器，避免真实等待。
    await act(async () => vi.advanceTimersByTimeAsync(200))
    await act(async () => root.unmount())
    container.remove()
    TestResizeObserver.reset()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  it('normalizes a Codex endpoint alias in credential events', async () => {
    await renderList({ events: [{ ...event, endpoint: 'POST /backend-api/codex/responses' }] })
    expect(container.textContent).toContain('usage_stats.request_endpoint: /v1/responses')
  })

  it('renders the compact credential event columns with stacked request metadata', async () => {
    await renderList()

    expect(container.querySelector('[data-credential-request-events-list="true"]')).not.toBeNull()
    const apiKeyCell = container.querySelector('tbody tr:first-child td:nth-child(2)')
    expect(apiKeyCell?.textContent).toBe('Team Alpha')
    expect(container.textContent).toContain('SSE')
    expect(container.textContent).not.toContain('priority')
    expect(container.textContent).not.toContain('ultrafast')
    expect(container.textContent).not.toContain('flex')
    expect(container.textContent).toContain('1.30K')
    expect(container.textContent).toContain('1.00K')
    expect(container.textContent).toContain('300')
    expect(container.textContent).toContain('80')
    expect(container.textContent).toContain('60.00%')
    expect(container.textContent).toContain('600')
    expect(container.textContent).toContain('100')
    expect(container.textContent).not.toContain('usage_stats.credentials_detail_cache_read')
    expect(container.textContent).not.toContain('usage_stats.credentials_detail_cache_write')
    expect(container.textContent).not.toContain('usage_stats.reasoning_tokens 80')
    expect(container.textContent).toContain('42.5 t/s')
    expect(container.textContent).toContain('usage_stats.credentials_detail_pricing_style_openai')
    expect(container.textContent).not.toContain('usage_stats.model_price_style usage_stats.model_price_style_openai')
    expect(container.querySelector('[data-credential-request-timestamp="1"]')?.textContent)
      .toBe('10:00:002026/08/17')
    expect(container.querySelector('[data-credential-request-model="1"]')?.textContent)
      .toBe('gpt-5.6keeper-gpt')
    expect(container.querySelector('[data-credential-request-model="1"]')?.textContent)
      .not.toContain('usage_stats.reasoning_effort')
    expect(container.textContent).toContain('usage_stats.request_endpoint: /v1/responses')
    expect(container.textContent).toContain('usage_stats.reasoning_effort: high')
    expect(container.textContent).not.toContain('usage_stats.model_alias')
    expect(container.querySelector('[data-credential-request-model="1"]')?.getAttribute('title')).toBeNull()
    expect(Array.from(container.querySelectorAll('[data-credential-request-sub-label]')).map((label) => label.textContent))
      .toEqual(expect.arrayContaining([
        'usage_stats.request_endpoint:',
        'usage_stats.reasoning_effort:',
        'usage_stats.ttft:',
        'usage_stats.speed:',
      ]))
    const metricCells = container.querySelectorAll<HTMLTableCellElement>('tbody tr:first-child td')
    const tokenCell = metricCells[5]
    const cacheCell = metricCells[6]
    expect(tokenCell.tabIndex).toBe(0)
    expect(cacheCell.tabIndex).toBe(0)
    expect(tokenCell.getAttribute('aria-label')).toContain('usage_stats.total_tokens: 1,300')
    expect(cacheCell.getAttribute('aria-label')).toContain('usage_stats.cache_rate: 60.00%')
    expect(Array.from(container.querySelectorAll('thead th')).map((cell) => cell.textContent)).toEqual([
      'usage_stats.request_events_timestamp',
      'usage_stats.api_key_filter',
      'usage_stats.model_name',
      'usage_stats.request_type',
      'usage_stats.request_events_result',
      'usage_stats.request_events_tokens',
      'usage_stats.credentials_detail_cache_column',
      'usage_stats.latency',
      'usage_stats.request_events_cost',
    ])
    expect(container.textContent).not.toContain('usage_stats.request_events_title')
    expect(container.textContent).not.toContain('usage_stats.request_events_subtitle')
    expect(container.textContent).not.toContain('usage_stats.request_events_columns')
    expect(container.textContent).not.toContain('usage_stats.request_events_filter_model')
    expect(container.textContent).not.toContain('usage_stats.request_events_filter_source')
    expect(container.textContent).not.toContain('usage_stats.request_events_filter_result')
  })

  it('stacks a distinct response model between the requested model and alias', async () => {
    await renderList({ events: [{ ...event, response_model: 'gpt-5.6-luna' }] })

    const modelCell = container.querySelector<HTMLElement>('[data-credential-request-model="1"]')
    expect(modelCell?.textContent).toBe('gpt-5.6↳ usage_stats.upstream_response_model: gpt-5.6-lunakeeper-gpt')
    expect(modelCell?.getAttribute('aria-label')).toBe(
      'usage_stats.model_name: gpt-5.6; usage_stats.upstream_response_model: gpt-5.6-luna; usage_stats.model_alias: keeper-gpt',
    )
  })

  it('keeps matching response and alias values in the whole-field model tooltip', async () => {
    await renderList({ events: [{ ...event, response_model: 'gpt-5.6', model_alias: 'GPT-5.6' }] })

    const modelCell = container.querySelector<HTMLElement>('[data-credential-request-model="1"]')
    expect(modelCell?.textContent).toBe('gpt-5.6')
    expect(modelCell?.getAttribute('aria-label')).toBe(
      'usage_stats.model_name: gpt-5.6; usage_stats.upstream_response_model: gpt-5.6; usage_stats.model_alias: GPT-5.6',
    )

    await act(async () => modelCell?.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    const tooltipText = tooltip()?.textContent
    expect(tooltipText).toContain('usage_stats.model_name: gpt-5.6')
    expect(tooltipText).toContain('usage_stats.upstream_response_model: gpt-5.6')
    expect(tooltipText).toContain('usage_stats.model_alias: GPT-5.6')
  })

  it.each([undefined, 0, 3000])('shows the API speed independently of TTFT %s', async (ttft) => {
    await renderList({ events: [{ ...event, ttft_ms: ttft }] })

    expect(container.textContent).toContain('42.5 t/s')
  })

  it('uses compact token units in details while keeping full values in the metric tooltip', async () => {
    const largeEvent: UsageEvent = {
      ...event,
      tokens: {
        input_tokens: 1_234_567,
        output_tokens: 2_345_678,
        reasoning_tokens: 12_345,
        cache_read_tokens: 3_456_789,
        cache_creation_tokens: 4_567_890,
        total_tokens: 5_678_901,
      },
    }

    await renderList({ events: [largeEvent] })

    const cells = container.querySelectorAll<HTMLTableCellElement>('tbody tr:first-child td')
    const tokenCell = cells[5]
    const cacheCell = cells[6]
    expect(tokenCell.textContent).toContain('5.68M')
    expect(tokenCell.textContent).toContain('1.23M')
    expect(tokenCell.textContent).toContain('2.35M')
    expect(tokenCell.textContent).toContain('12.35K')
    expect(cacheCell.textContent).toContain('3.46M')
    expect(cacheCell.textContent).toContain('4.57M')
    expect(tokenCell.getAttribute('aria-label')).toBe(
      'usage_stats.total_tokens: 5,678,901; usage_stats.input_tokens: 1,234,567; usage_stats.output_tokens: 2,345,678; usage_stats.reasoning_tokens: 12,345',
    )
    expect(cacheCell.getAttribute('aria-label')).toBe(
      'usage_stats.cache_rate: 280.00%; usage_stats.cache_read_tokens: 3,456,789; usage_stats.cache_creation_tokens: 4,567,890',
    )
    expect(container.querySelector('[data-token-direction="input"]')?.getAttribute('aria-label'))
      .toBe('usage_stats.input_tokens: 1,234,567')
    expect(container.querySelector('[data-cache-operation="read"]')?.getAttribute('aria-label'))
      .toBe('usage_stats.cache_read_tokens: 3,456,789')

    await act(async () => tokenCell.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    expect(Array.from(
      document.body.querySelectorAll<HTMLElement>('[role="tooltip"] span'),
      (line) => line.textContent,
    )).toEqual([
      'usage_stats.total_tokens: 5,678,901',
      'usage_stats.input_tokens: 1,234,567',
      'usage_stats.output_tokens: 2,345,678',
      'usage_stats.reasoning_tokens: 12,345',
    ])
  })

  it('expands only metadata that is absent from the compact row', async () => {
    await renderList()

    const toggle = container.querySelector<HTMLButtonElement>('[data-credential-request-event-toggle="1"]')
    expect(toggle!.getAttribute('aria-expanded')).toBe('false')
    expect(container.querySelector('[data-credential-request-event-details="1"]')).toBeNull()

    await act(async () => toggle!.click())

    const details = container.querySelector('[data-credential-request-event-details="1"]')
    expect(toggle!.getAttribute('aria-expanded')).toBe('true')
    expect(details?.querySelectorAll('[data-credential-request-detail-group]')).toHaveLength(2)
    expect(details?.querySelectorAll('[data-credential-request-detail-item]')).toHaveLength(7)
    expect(details?.textContent).toContain('usage_stats.credentials_detail_request_context')
    expect(details?.textContent).toContain('usage_stats.credentials_detail_client_context')
    expect(details?.textContent).not.toContain('Team Alpha')
    expect(Array.from(details!.querySelectorAll('[data-credential-request-detail-group="request"] strong'), (node) => node.textContent))
      .toEqual(['usage_stats.speed_mode_ultrafast (ultrafast)', 'usage_stats.speed_mode_fast (priority)', 'usage_stats.speed_mode_flex (flex)', 'OpenAIResponsesExecutor'])
    expect(Array.from(details!.querySelectorAll('[data-credential-request-detail-group="request"] [data-credential-request-detail-item] > span'), (node) => node.textContent))
      .toEqual([
        'usage_stats.credentials_detail_effective_tier',
        'usage_stats.credentials_detail_request_tier',
        'usage_stats.credentials_detail_response_tier',
        'usage_stats.credentials_detail_executor',
      ])
    expect(details?.textContent).toContain('OpenAIResponsesExecutor')
    expect(details?.textContent).toContain('192.0.2.10')
    expect(details?.textContent).toContain('198.51.100.7, 192.0.2.10')
    expect(details?.textContent).toContain('Codex CLI/1.2.3')
    expect(details?.textContent).not.toContain('request-1')
    expect(details?.textContent).not.toContain('keeper-gpt')
    expect(details?.textContent).not.toContain('42.5 t/s')
  })

  it('does not expand a row when API Key is the only extra value', async () => {
    await renderList({ events: [{
      ...event,
      service_tier: '',
      effective_service_tier: '',
      response_service_tier: '',
      executor_type: '',
      client_ip: '',
      x_forwarded_for: '',
      user_agent: '',
    }] })

    expect(container.textContent).toContain('Team Alpha')
    expect(container.querySelector('[data-credential-request-event-toggle="1"]')).toBeNull()
    expect(container.querySelector('[data-credential-request-event-details="1"]')).toBeNull()
  })

  it.each([
    ['CustomTier', 'CustomTier (CustomTier)'],
    ['ultrafast', 'usage_stats.speed_mode_ultrafast (ultrafast)'],
    ['', '-'], [undefined, '-'],
  ])(
    'shows the mapped effective tier %j with its raw value and dashes without parentheses', async (effectiveTier, expected) => {
      await act(async () => root.render(
        <CredentialRequestEventsList {...credentialEventListDefaults} events={[{
          ...event, service_tier: '', effective_service_tier: effectiveTier, response_service_tier: undefined,
        }]} />,
      ))
      await act(async () => container.querySelector<HTMLButtonElement>('[data-credential-request-event-toggle="1"]')!.click())
      const values = Array.from(container.querySelectorAll('[data-credential-request-detail-group="request"] strong'), (node) => node.textContent)
      expect(values).toEqual([expected, '-', '-', 'OpenAIResponsesExecutor'])
    },
  )

  it.each([
    ['ultrafast', 'usage_stats.speed_mode_ultrafast (ultrafast)'],
    ['-', '- (-)'],
    [' - ', '- (-)'],
  ])('allows expansion when only the outbound tier %j is present', async (effectiveTier, expected) => {
    await act(async () => root.render(
      <CredentialRequestEventsList {...credentialEventListDefaults} events={[{
        ...event, service_tier: '', effective_service_tier: effectiveTier, response_service_tier: '', executor_type: '',
        client_ip: '', x_forwarded_for: '', user_agent: '',
      }]} />,
    ))
    const toggle = container.querySelector<HTMLButtonElement>('[data-credential-request-event-toggle="1"]')!
    expect(toggle).not.toBeNull()
    await act(async () => toggle.click())
    expect(container.querySelector('[data-credential-request-event-details="1"]')?.textContent).toContain(expected)
  })

  it('shows the shared tooltip only when compact or detail text is actually truncated', async () => {
    const longUserAgent = 'codex-cli/0.42.0 (linux; x86_64) long-user-agent-preview-with-extra-runtime-metadata'
    mockOverflow(longUserAgent)

    await renderList({ events: [{ ...event, api_key: longApiKey, user_agent: longUserAgent }] })

    const findValue = (value: string) => Array.from(
      container.querySelectorAll<HTMLElement>('[data-credential-request-overflow-target]'),
    ).find((element) => element.textContent === value)

    const apiKeyTarget = findValue(longApiKey)
    expect(apiKeyTarget!.tabIndex).toBe(0)

    await act(async () => apiKeyTarget!.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    expect(tooltip()?.textContent).toBe(longApiKey)
    await act(async () => apiKeyTarget!.dispatchEvent(new MouseEvent('mouseout', { bubbles: true })))

    expect(tooltip()).toBeNull()

    await act(async () => container.querySelector<HTMLButtonElement>('[data-credential-request-event-toggle="1"]')!.click())
    const userAgentTarget = findValue(longUserAgent)
    const clientIPTarget = findValue('192.0.2.10')
    expect(userAgentTarget!.tabIndex).toBe(0)
    expect(clientIPTarget!.tabIndex).toBe(-1)

    await act(async () => userAgentTarget!.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    expect(tooltip()?.textContent).toBe(longUserAgent)
    await act(async () => userAgentTarget!.dispatchEvent(new MouseEvent('mouseout', { bubbles: true })))

    await act(async () => clientIPTarget!.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    expect(tooltip()).toBeNull()
  })

  it('dismisses focused and hovered overflow tooltips before Escape reaches the drawer', async () => {
    const onClose = vi.fn()
    mockOverflow()

    await act(async () => {
      root.render(
        <Modal open title="Credential" variant="drawer" onClose={onClose}>
          <CredentialRequestEventsList
            {...credentialEventListDefaults}
            events={[{ ...event, api_key: longApiKey }]}
          />
        </Modal>,
      )
      await vi.advanceTimersByTimeAsync(0)
    })

    const apiKeyTarget = Array.from(
      document.body.querySelectorAll<HTMLElement>('[data-credential-request-overflow-target]'),
    ).find((element) => element.textContent === longApiKey)
    await act(async () => apiKeyTarget!.focus())
    expect(tooltip()?.textContent).toBe(longApiKey)

    await act(async () => apiKeyTarget!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })))
    expect(tooltip()).toBeNull()
    expect(onClose).not.toHaveBeenCalled()

    const rowToggle = document.body.querySelector<HTMLButtonElement>('[data-credential-request-event-toggle="1"]')
    await act(async () => rowToggle!.focus())
    await act(async () => apiKeyTarget!.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    expect(tooltip()?.textContent).toBe(longApiKey)

    await act(async () => rowToggle!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })))
    expect(tooltip()).toBeNull()
    expect(onClose).not.toHaveBeenCalled()

    await act(async () => rowToggle!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('opens the selected request log from the result badge', async () => {
    const logEvent = { ...event, api_key: longApiKey }
    const onRequestLogOpen = vi.fn()
    mockOverflow()
    await renderList({ events: [logEvent], requestLogAccessEnabled: true, onRequestLogOpen })

    const apiKeyTarget = Array.from(
      container.querySelectorAll<HTMLElement>('[data-credential-request-overflow-target]'),
    ).find((element) => element.textContent === longApiKey)
    await act(async () => apiKeyTarget!.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    expect(tooltip()?.textContent).toBe(longApiKey)

    await act(async () => container.querySelector<HTMLButtonElement>('[data-credential-request-log="1"]')!.click())
    expect(onRequestLogOpen).toHaveBeenCalledWith(logEvent)
    expect(tooltip()).toBeNull()
    expect(container.querySelector('[data-credential-request-event-details="1"]')).toBeNull()
  })

  it('keeps the first full cursor page virtualized while appending the next page', async () => {
    const firstPage = Array.from({ length: 50 }, (_, index) => buildEvent(index))
    const secondPage = Array.from({ length: 100 }, (_, index) => buildEvent(index))
    await renderVirtualList(firstPage)

    const scroller = container.querySelector<HTMLElement>('[data-credential-request-events-scroller="true"]')!
    expect(scroller.dataset.virtualized).toBe('true')
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-credential-request-event-toggle="1"]')!.click()
    })
    await act(async () => TestResizeObserver.flush())
    expect(readVirtualContentHeight(scroller)).toBe(4_140)

    await scrollList(scroller, 2_800)
    expect(container.querySelector('[data-credential-request-event-toggle="1"]')).toBeNull()
    const scrollTopBeforeAppend = scroller.scrollTop

    await renderVirtualList(secondPage)
    expect(readVirtualContentHeight(scroller)).toBe(8_140)
    expect(scroller.scrollTop).toBe(scrollTopBeforeAppend)
  })

  it('keeps a large loaded history bounded in the DOM and advances the virtual window', async () => {
    const events = Array.from({ length: 1000 }, (_, index) => buildEvent(index))
    await renderVirtualList(events)

    const scroller = container.querySelector<HTMLElement>('[data-credential-request-events-scroller="true"]')!
    expect(scroller.dataset.virtualized).toBe('true')
    expect(scroller.querySelector('table')?.getAttribute('aria-rowcount')).toBe('1001')
    const initialRows = Array.from(scroller.querySelectorAll<HTMLTableRowElement>('tbody tr[data-index]'))
    const initialIndexes = initialRows.map((row) => Number(row.dataset.index))
    expect(initialRows.length).toBeGreaterThan(0)
    expect(initialRows.length).toBeLessThan(100)

    await scrollList(scroller, 26_000)

    const scrolledRows = Array.from(scroller.querySelectorAll<HTMLTableRowElement>('tbody tr[data-index]'))
    const scrolledIndexes = scrolledRows.map((row) => Number(row.dataset.index))
    expect(scrolledRows.length).toBeGreaterThan(0)
    expect(scrolledRows.length).toBeLessThan(100)
    expect(Math.min(...scrolledIndexes)).toBeGreaterThan(Math.min(...initialIndexes))
  })

  it('clears an overflow tooltip when its virtual row leaves the window', async () => {
    const events = Array.from({ length: 100 }, (_, index) => (
      index === 0 ? { ...buildEvent(index), api_key: longApiKey } : buildEvent(index)
    ))
    const onClose = vi.fn()
    mockOverflow()

    await act(async () => {
      root.render(
        <Modal open title="Credential" variant="drawer" onClose={onClose}>
          <CredentialRequestEventsList
            {...credentialEventListDefaults}
            events={events}
          />
        </Modal>,
      )
      await vi.advanceTimersByTimeAsync(0)
    })

    const apiKeyTarget = Array.from(
      document.body.querySelectorAll<HTMLElement>('[data-credential-request-overflow-target]'),
    ).find((element) => element.textContent === longApiKey)
    await act(async () => apiKeyTarget!.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    expect(tooltip()?.textContent).toBe(longApiKey)

    const scroller = document.body.querySelector<HTMLElement>('[data-credential-request-events-scroller="true"]')!
    await scrollList(scroller, 3_500)
    expect(apiKeyTarget!.isConnected).toBe(false)
    expect(tooltip()).toBeNull()

    const visibleToggle = document.body.querySelector<HTMLButtonElement>('[data-credential-request-event-toggle]')
    await act(async () => visibleToggle!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it.each([
    ['model', '[data-credential-request-model="1"]'],
    ['token', '[data-token-direction="input"]'],
  ])('clears a %s tooltip when its virtual row leaves the window', async (_kind, selector) => {
    const events = Array.from({ length: 100 }, (_, index) => buildEvent(index))
    await renderVirtualList(events)

    const cell = container.querySelector(selector)!.closest('td')!
    await act(async () => cell.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    expect(tooltip()).not.toBeNull()

    const scroller = container.querySelector<HTMLElement>('[data-credential-request-events-scroller="true"]')!
    await scrollList(scroller, 3_500)

    expect(cell.isConnected).toBe(false)
    expect(tooltip()).toBeNull()
  })

  it('drops the stale height of an expanded row after it leaves the virtual window', async () => {
    const events = Array.from({ length: 100 }, (_, index) => buildEvent(index))
    await renderVirtualList(events)

    const scroller = container.querySelector<HTMLElement>('[data-credential-request-events-scroller="true"]')!
    scroller.scrollTo = ((options: ScrollToOptions) => {
      if (typeof options.top === 'number') scroller.scrollTop = options.top
    }) as typeof scroller.scrollTo
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-credential-request-event-toggle="1"]')!.click()
    })
    await act(async () => {
      TestResizeObserver.flush()
    })
    expect(readVirtualContentHeight(scroller)).toBe(8_140)

    await scrollList(scroller, 3_500)
    expect(container.querySelector('[data-credential-request-event-toggle="1"]')).toBeNull()

    const nextToggle = container.querySelector<HTMLButtonElement>(
      'tbody[data-index="50"] [data-credential-request-event-toggle]',
    )
    const scrollTopBeforeSwitch = scroller.scrollTop
    await act(async () => {
      nextToggle!.click()
    })
    await act(async () => {
      TestResizeObserver.flush()
    })
    expect(readVirtualContentHeight(scroller)).toBe(8_140)
    expect(scroller.scrollTop).toBe(scrollTopBeforeSwitch - 140)
  })

  it('fully renders a small event page without virtual spacer rows', async () => {
    const events = Array.from({ length: 3 }, (_, index) => buildEvent(index))
    await renderList({ events })

    const scroller = container.querySelector<HTMLElement>('[data-credential-request-events-scroller="true"]')!
    expect(scroller.dataset.virtualized).toBe('false')
    expect(scroller.querySelectorAll('tbody tr')).toHaveLength(3)
    expect(scroller.querySelector('[data-credential-request-events-spacer]')).toBeNull()
  })
})
