// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Modal } from '@/components/ui/Modal'
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

const CREDENTIAL_REQUEST_TEST_ROW_HEIGHT = 80

const buildEvent = (index: number): UsageEvent => ({
  ...event,
  id: String(index + 1),
  request_id: `request-${index + 1}`,
  model: `model-${index}`,
})

const rect = (width: number, height: number): DOMRect => ({
  x: 0,
  y: 0,
  top: 0,
  right: width,
  bottom: height,
  left: 0,
  width,
  height,
  toJSON: () => ({}),
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
    } as unknown as ResizeObserverEntry], this)
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

describe('CredentialRequestEventsList', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    TestResizeObserver.reset()
    vi.stubGlobal('ResizeObserver', TestResizeObserver)
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
      callback(performance.now())
      return 0
    })
    vi.stubGlobal('cancelAnimationFrame', () => undefined)
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function getBoundingClientRect() {
      if (this.dataset.credentialRequestEventsScroller === 'true') return rect(920, 600)
      if (this.dataset.credentialRequestEventGroup) {
        const expanded = this.querySelector('[data-credential-request-event-details]') !== null
        return rect(920, expanded ? 220 : CREDENTIAL_REQUEST_TEST_ROW_HEIGHT)
      }
      if (this instanceof HTMLTableRowElement) {
        const spacerHeight = Number.parseFloat(this.style.height)
        return rect(920, Number.isFinite(spacerHeight) ? spacerHeight : 52)
      }
      return rect(920, 600)
    })
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => {
      const { promise, resolve } = Promise.withResolvers<void>()
      window.setTimeout(resolve, 200)
      await promise
    })
    await act(async () => root.unmount())
    container.remove()
    document.body.innerHTML = ''
    TestResizeObserver.reset()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('renders the compact credential event columns with stacked request metadata', async () => {
    await act(async () => root.render(
      <CredentialRequestEventsList
        events={[event]}
        loading={false}
        hasMore={false}
        loadingMore={false}
        autoLoadMore
        onLoadMore={() => undefined}
      />,
    ))

    expect(container.querySelector('[data-credential-request-events-list="true"]')).not.toBeNull()
    const apiKeyCell = container.querySelector('tbody tr:first-child td:nth-child(2)')
    expect(apiKeyCell?.textContent).toBe('Team Alpha')
    expect(apiKeyCell?.className).toContain('apiKey')
    expect(container.textContent).toContain('gpt-5.6')
    expect(container.textContent).toContain('keeper-gpt')
    expect(container.textContent).toContain('high')
    expect(container.textContent).toContain('SSE')
    expect(container.textContent).not.toContain('usage_stats.speed_mode_fast')
    expect(container.textContent).not.toContain('usage_stats.speed_mode_flex')
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
      .toBe('gpt-5.6keeper-gptusage_stats.reasoning_effort high')
    expect(container.textContent).not.toContain('usage_stats.model_alias')
    expect(container.querySelector('[data-credential-request-model="1"]')?.getAttribute('title')).toBeNull()
    expect(Array.from(container.querySelectorAll('[data-credential-request-sub-label]')).map((label) => label.textContent)).toEqual([
      'usage_stats.reasoning_effort',
      'usage_stats.ttft',
      'usage_stats.speed',
    ])
    const metricCells = container.querySelectorAll<HTMLTableCellElement>('tbody tr:first-child td')
    const tokenCell = metricCells[5]
    const cacheCell = metricCells[6]
    expect(tokenCell.tabIndex).toBe(0)
    expect(cacheCell.tabIndex).toBe(0)
    expect(tokenCell.getAttribute('aria-label')).toContain('usage_stats.total_tokens: 1,300')
    expect(cacheCell.getAttribute('aria-label')).toContain('usage_stats.cache_rate: 60.00%')
    expect(container.querySelectorAll('[data-token-direction="input"]')).toHaveLength(1)
    expect(container.querySelectorAll('[data-token-direction="output"]')).toHaveLength(1)
    expect(container.querySelectorAll('[data-token-direction="reasoning"]')).toHaveLength(1)
    expect(container.querySelectorAll('[data-cache-operation="read"]')).toHaveLength(1)
    expect(container.querySelectorAll('[data-cache-operation="write"]')).toHaveLength(1)
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

    await act(async () => root.render(
      <CredentialRequestEventsList
        events={[largeEvent]}
        loading={false}
        hasMore={false}
        loadingMore={false}
        autoLoadMore
        onLoadMore={() => undefined}
      />,
    ))

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
    await act(async () => root.render(
      <CredentialRequestEventsList
        events={[event]}
        loading={false}
        hasMore={false}
        loadingMore={false}
        autoLoadMore
        onLoadMore={() => undefined}
      />,
    ))

    const toggle = container.querySelector<HTMLButtonElement>('[data-credential-request-event-toggle="1"]')
    expect(toggle?.getAttribute('aria-expanded')).toBe('false')
    expect(toggle?.querySelector('path')?.getAttribute('d')).toBe('m9 18 6-6-6-6')
    expect(container.querySelector('[data-credential-request-event-details="1"]')).toBeNull()

    await act(async () => toggle?.click())

    const details = container.querySelector('[data-credential-request-event-details="1"]')
    expect(toggle?.getAttribute('aria-expanded')).toBe('true')
    expect(toggle?.querySelector('path')?.getAttribute('d')).toBe('m6 9 6 6 6-6')
    expect(details?.querySelectorAll('[data-credential-request-detail-group]')).toHaveLength(2)
    expect(details?.querySelectorAll('[data-credential-request-detail-item]')).toHaveLength(6)
    for (const item of details?.querySelectorAll('[data-credential-request-detail-item]') ?? []) {
      expect(item.children).toHaveLength(2)
    }
    expect(details?.textContent).toContain('usage_stats.credentials_detail_request_context')
    expect(details?.textContent).toContain('usage_stats.credentials_detail_client_context')
    expect(details?.textContent).not.toContain('Team Alpha')
    expect(details?.textContent).toContain('usage_stats.speed_mode_fast (priority)')
    expect(details?.textContent).toContain('usage_stats.speed_mode_flex (flex)')
    expect(details?.textContent).toContain('OpenAIResponsesExecutor')
    expect(details?.textContent).toContain('192.0.2.10')
    expect(details?.textContent).toContain('198.51.100.7, 192.0.2.10')
    expect(details?.textContent).toContain('Codex CLI/1.2.3')
    expect(details?.textContent).not.toContain('request-1')
    expect(details?.textContent).not.toContain('keeper-gpt')
    expect(details?.textContent).not.toContain('42.5 t/s')
  })

  it('does not expand a row when API Key is the only extra value', async () => {
    await act(async () => root.render(
      <CredentialRequestEventsList
        events={[{
          ...event,
          service_tier: '',
          response_service_tier: '',
          executor_type: '',
          client_ip: '',
          x_forwarded_for: '',
          user_agent: '',
        }]}
        loading={false}
        hasMore={false}
        loadingMore={false}
        autoLoadMore
        onLoadMore={() => undefined}
      />,
    ))

    expect(container.textContent).toContain('Team Alpha')
    expect(container.querySelector('[data-credential-request-event-toggle="1"]')).toBeNull()
    expect(container.querySelector('[data-credential-request-event-details="1"]')).toBeNull()
  })

  it('shows the shared tooltip only when compact or detail text is actually truncated', async () => {
    const longModel = 'gpt-5.4-codex-reasoning-ultra-long-context-preview-2026-08-21'
    const longUserAgent = 'codex-cli/0.42.0 (linux; x86_64) long-user-agent-preview-with-extra-runtime-metadata'
    vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(100)
    vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockImplementation(function scrollWidth() {
      return this.textContent === longModel ? 360 : 80
    })
    vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(20)
    vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockImplementation(function scrollHeight() {
      return this.textContent === longUserAgent ? 40 : 20
    })

    await act(async () => root.render(
      <CredentialRequestEventsList
        events={[{ ...event, model: longModel, user_agent: longUserAgent }]}
        loading={false}
        hasMore={false}
        loadingMore={false}
        autoLoadMore
        onLoadMore={() => undefined}
      />,
    ))

    const findValue = (value: string) => Array.from(
      container.querySelectorAll<HTMLElement>('[data-credential-request-overflow-target]'),
    ).find((element) => element.textContent === value)

    const modelTarget = findValue(longModel)
    const aliasTarget = findValue('keeper-gpt')
    expect(modelTarget?.tabIndex).toBe(0)
    expect(aliasTarget?.tabIndex).toBe(-1)

    await act(async () => modelTarget?.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toBe(longModel)
    await act(async () => modelTarget?.dispatchEvent(new MouseEvent('mouseout', { bubbles: true })))

    await act(async () => aliasTarget?.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()

    await act(async () => container.querySelector<HTMLButtonElement>('[data-credential-request-event-toggle="1"]')?.click())
    const userAgentTarget = findValue(longUserAgent)
    const clientIPTarget = findValue('192.0.2.10')
    expect(userAgentTarget?.className).toContain('detailUserAgentValue')
    expect(userAgentTarget?.tabIndex).toBe(0)
    expect(clientIPTarget?.tabIndex).toBe(-1)

    await act(async () => userAgentTarget?.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toBe(longUserAgent)
    await act(async () => userAgentTarget?.dispatchEvent(new MouseEvent('mouseout', { bubbles: true })))

    await act(async () => clientIPTarget?.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
  })

  it('dismisses focused and hovered overflow tooltips before Escape reaches the drawer', async () => {
    const longModel = 'gpt-5.4-codex-reasoning-ultra-long-context-preview-2026-08-21'
    const onClose = vi.fn()
    vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(100)
    vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockImplementation(function scrollWidth() {
      return this.textContent === longModel ? 360 : 80
    })
    vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(20)
    vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockReturnValue(20)

    await act(async () => {
      root.render(
        <Modal open title="Credential" variant="drawer" onClose={onClose}>
          <CredentialRequestEventsList
            events={[{ ...event, model: longModel }]}
            loading={false}
            hasMore={false}
            loadingMore={false}
            autoLoadMore
            onLoadMore={() => undefined}
          />
        </Modal>,
      )
      const { promise, resolve } = Promise.withResolvers<void>()
      window.setTimeout(resolve, 0)
      await promise
    })

    const modelTarget = Array.from(
      document.body.querySelectorAll<HTMLElement>('[data-credential-request-overflow-target]'),
    ).find((element) => element.textContent === longModel)
    await act(async () => modelTarget?.focus())
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toBe(longModel)

    await act(async () => modelTarget?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })))
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
    expect(onClose).not.toHaveBeenCalled()

    const rowToggle = document.body.querySelector<HTMLButtonElement>('[data-credential-request-event-toggle="1"]')
    await act(async () => rowToggle?.focus())
    await act(async () => modelTarget?.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toBe(longModel)

    await act(async () => rowToggle?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })))
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
    expect(onClose).not.toHaveBeenCalled()

    await act(async () => rowToggle?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('opens the selected request log from the result badge', async () => {
    const longModel = 'gpt-5.4-codex-reasoning-ultra-long-context-preview-2026-08-21'
    const logEvent = { ...event, model: longModel }
    const onRequestLogOpen = vi.fn()
    vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(100)
    vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockImplementation(function scrollWidth() {
      return this.textContent === longModel ? 360 : 80
    })
    vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(20)
    vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockReturnValue(20)
    await act(async () => root.render(
      <CredentialRequestEventsList
        events={[logEvent]}
        loading={false}
        hasMore={false}
        loadingMore={false}
        autoLoadMore
        onLoadMore={() => undefined}
        requestLogAccessEnabled
        onRequestLogOpen={onRequestLogOpen}
      />,
    ))

    const modelTarget = Array.from(
      container.querySelectorAll<HTMLElement>('[data-credential-request-overflow-target]'),
    ).find((element) => element.textContent === longModel)
    await act(async () => modelTarget?.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toBe(longModel)

    await act(async () => container.querySelector<HTMLButtonElement>('[data-credential-request-log="1"]')?.click())
    expect(onRequestLogOpen).toHaveBeenCalledWith(logEvent)
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
    const logButton = container.querySelector<HTMLButtonElement>('[data-credential-request-log="1"]')
    expect(logButton?.className).toContain('requestEventsResultLogButton')
    expect(logButton?.className).toContain('requestEventsResultCompact')
    expect(logButton?.querySelector('[class*="requestEventsResultLogIcon"]')).not.toBeNull()
    expect(logButton?.querySelector('svg')?.getAttribute('width')).toBe('9')
    expect(container.querySelector('[data-credential-request-event-details="1"]')).toBeNull()
  })

  it('detects the dedicated list load-more boundary', () => {
    expect(shouldLoadMoreCredentialRequestEvents({ scrollTop: 1_000, clientHeight: 500, scrollHeight: 1_700 })).toBe(true)
    expect(shouldLoadMoreCredentialRequestEvents({ scrollTop: 100, clientHeight: 500, scrollHeight: 1_700 })).toBe(false)
  })

  it('keeps the first full cursor page virtualized while appending the next page', async () => {
    const firstPage = Array.from({ length: 50 }, (_, index) => buildEvent(index))
    const secondPage = Array.from({ length: 100 }, (_, index) => buildEvent(index))
    const renderList = (events: UsageEvent[]) => (
      <CredentialRequestEventsList
        events={events}
        loading={false}
        hasMore={false}
        loadingMore={false}
        autoLoadMore
        onLoadMore={() => undefined}
      />
    )

    await act(async () => {
      root.render(renderList(firstPage))
      await Promise.resolve()
    })

    const scroller = container.querySelector<HTMLElement>('[data-credential-request-events-scroller="true"]')!
    expect(scroller.dataset.virtualized).toBe('true')
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-credential-request-event-toggle="1"]')?.click()
    })
    await act(async () => TestResizeObserver.flush())
    expect(readVirtualContentHeight(scroller)).toBe(4_140)

    scroller.scrollTop = 2_800
    await act(async () => {
      scroller.dispatchEvent(new Event('scroll'))
      const { promise, resolve } = Promise.withResolvers<void>()
      window.setTimeout(resolve, 0)
      await promise
    })
    expect(container.querySelector('[data-credential-request-event-toggle="1"]')).toBeNull()
    const scrollTopBeforeAppend = scroller.scrollTop

    await act(async () => {
      root.render(renderList(secondPage))
      await Promise.resolve()
    })
    expect(readVirtualContentHeight(scroller)).toBe(8_140)
    expect(scroller.scrollTop).toBe(scrollTopBeforeAppend)
  })

  it('keeps a large loaded history bounded in the DOM and advances the virtual window', async () => {
    const events = Array.from({ length: 1000 }, (_, index) => buildEvent(index))
    await act(async () => {
      root.render(
        <CredentialRequestEventsList
          events={events}
          loading={false}
          hasMore={false}
          loadingMore={false}
          autoLoadMore
          onLoadMore={() => undefined}
        />,
      )
      await Promise.resolve()
    })

    const scroller = container.querySelector<HTMLElement>('[data-credential-request-events-scroller="true"]')
    expect(scroller?.dataset.virtualized).toBe('true')
    expect(scroller?.querySelector('table')?.getAttribute('aria-rowcount')).toBe('1001')
    const initialRows = Array.from(scroller?.querySelectorAll<HTMLTableRowElement>('tbody tr[data-index]') ?? [])
    const initialIndexes = initialRows.map((row) => Number(row.dataset.index))
    expect(initialRows.length).toBeGreaterThan(0)
    expect(initialRows.length).toBeLessThan(100)

    scroller!.scrollTop = 26_000
    await act(async () => {
      scroller?.dispatchEvent(new Event('scroll'))
      const { promise, resolve } = Promise.withResolvers<void>()
      window.setTimeout(resolve, 0)
      await promise
    })

    const scrolledRows = Array.from(scroller?.querySelectorAll<HTMLTableRowElement>('tbody tr[data-index]') ?? [])
    const scrolledIndexes = scrolledRows.map((row) => Number(row.dataset.index))
    expect(scrolledRows.length).toBeGreaterThan(0)
    expect(scrolledRows.length).toBeLessThan(100)
    expect(Math.min(...scrolledIndexes)).toBeGreaterThan(Math.min(...initialIndexes))
  })

  it('clears an overflow tooltip when its virtual row leaves the window', async () => {
    const longModel = 'gpt-5.4-codex-reasoning-ultra-long-context-preview-2026-08-21'
    const events = Array.from({ length: 100 }, (_, index) => (
      index === 0 ? { ...buildEvent(index), model: longModel } : buildEvent(index)
    ))
    const onClose = vi.fn()
    vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(100)
    vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockImplementation(function scrollWidth() {
      return this.textContent === longModel ? 360 : 80
    })
    vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(20)
    vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockReturnValue(20)

    await act(async () => {
      root.render(
        <Modal open title="Credential" variant="drawer" onClose={onClose}>
          <CredentialRequestEventsList
            events={events}
            loading={false}
            hasMore={false}
            loadingMore={false}
            autoLoadMore
            onLoadMore={() => undefined}
          />
        </Modal>,
      )
      const { promise, resolve } = Promise.withResolvers<void>()
      window.setTimeout(resolve, 0)
      await promise
    })

    const modelTarget = Array.from(
      document.body.querySelectorAll<HTMLElement>('[data-credential-request-overflow-target]'),
    ).find((element) => element.textContent === longModel)
    await act(async () => modelTarget?.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toBe(longModel)

    const scroller = document.body.querySelector<HTMLElement>('[data-credential-request-events-scroller="true"]')!
    scroller.scrollTop = 3_500
    await act(async () => {
      scroller.dispatchEvent(new Event('scroll'))
      const { promise, resolve } = Promise.withResolvers<void>()
      window.setTimeout(resolve, 0)
      await promise
    })
    expect(modelTarget?.isConnected).toBe(false)
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()

    const visibleToggle = document.body.querySelector<HTMLButtonElement>('[data-credential-request-event-toggle]')
    await act(async () => visibleToggle?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('clears a token or cache tooltip when its virtual row leaves the window', async () => {
    const events = Array.from({ length: 100 }, (_, index) => buildEvent(index))

    await act(async () => {
      root.render(
        <CredentialRequestEventsList
          events={events}
          loading={false}
          hasMore={false}
          loadingMore={false}
          autoLoadMore
          onLoadMore={() => undefined}
        />,
      )
      await Promise.resolve()
    })

    const tokenCell = container.querySelector('[data-token-direction="input"]')?.closest('td')
    expect(tokenCell).not.toBeNull()
    await act(async () => tokenCell?.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    expect(document.body.querySelector('[role="tooltip"]')).not.toBeNull()

    const scroller = container.querySelector<HTMLElement>('[data-credential-request-events-scroller="true"]')!
    scroller.scrollTop = 3_500
    await act(async () => {
      scroller.dispatchEvent(new Event('scroll'))
      const { promise, resolve } = Promise.withResolvers<void>()
      window.setTimeout(resolve, 0)
      await promise
    })

    expect(tokenCell?.isConnected).toBe(false)
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
  })

  it('drops the stale height of an expanded row after it leaves the virtual window', async () => {
    const events = Array.from({ length: 100 }, (_, index) => buildEvent(index))
    await act(async () => {
      root.render(
        <CredentialRequestEventsList
          events={events}
          loading={false}
          hasMore={false}
          loadingMore={false}
          autoLoadMore
          onLoadMore={() => undefined}
        />,
      )
      await Promise.resolve()
    })

    const scroller = container.querySelector<HTMLElement>('[data-credential-request-events-scroller="true"]')!
    scroller.scrollTo = ((options: ScrollToOptions) => {
      if (typeof options.top === 'number') scroller.scrollTop = options.top
    }) as typeof scroller.scrollTo
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-credential-request-event-toggle="1"]')?.click()
    })
    await act(async () => {
      TestResizeObserver.flush()
    })
    expect(readVirtualContentHeight(scroller)).toBe(8_140)

    scroller.scrollTop = 3_500
    await act(async () => {
      scroller.dispatchEvent(new Event('scroll'))
      const { promise, resolve } = Promise.withResolvers<void>()
      window.setTimeout(resolve, 0)
      await promise
    })
    expect(container.querySelector('[data-credential-request-event-toggle="1"]')).toBeNull()

    const nextToggle = container.querySelector<HTMLButtonElement>(
      'tbody[data-index="50"] [data-credential-request-event-toggle]',
    )
    expect(nextToggle).not.toBeNull()
    const scrollTopBeforeSwitch = scroller.scrollTop
    await act(async () => {
      nextToggle?.click()
    })
    await act(async () => {
      TestResizeObserver.flush()
    })
    expect(readVirtualContentHeight(scroller)).toBe(8_140)
    expect(scroller.scrollTop).toBe(scrollTopBeforeSwitch - 140)
  })

  it('fully renders a small event page without virtual spacer rows', async () => {
    const events = Array.from({ length: 3 }, (_, index) => buildEvent(index))
    await act(async () => root.render(
      <CredentialRequestEventsList
        events={events}
        loading={false}
        hasMore={false}
        loadingMore={false}
        autoLoadMore
        onLoadMore={() => undefined}
      />,
    ))

    const scroller = container.querySelector<HTMLElement>('[data-credential-request-events-scroller="true"]')
    expect(scroller?.dataset.virtualized).toBe('false')
    expect(scroller?.querySelectorAll('tbody tr')).toHaveLength(3)
    expect(scroller?.querySelector('[data-credential-request-events-spacer]')).toBeNull()
  })
})
