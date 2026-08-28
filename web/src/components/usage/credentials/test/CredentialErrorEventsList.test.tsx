// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ErrorEvent } from '@/lib/types'
import { CredentialErrorEventsList } from '../CredentialErrorEventsList'

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({ t: (key: string) => key }),
}))

const buildEvent = (index: number): ErrorEvent => ({
  id: String(index + 1),
  timestamp: '2026-08-20T12:00:00Z',
  provider: 'codex',
  model: `gpt-5.6-${index}`,
  status_code: 429,
  body_summary: `quota exceeded ${index}`,
  body_truncated: false,
  code: 'rate_limit',
  retryable: true,
  credential_retry_after: '2026-08-20T12:05:00Z',
  model_retry_after: '2026-08-20T12:03:00Z',
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
  constructor(private readonly callback: ResizeObserverCallback) {}

  observe(target: Element) {
    const contentRect = target.getBoundingClientRect()
    this.callback([{
      target,
      contentRect,
      borderBoxSize: [{ inlineSize: contentRect.width, blockSize: contentRect.height }],
      contentBoxSize: [{ inlineSize: contentRect.width, blockSize: contentRect.height }],
      devicePixelContentBoxSize: [],
    } as unknown as ResizeObserverEntry], this)
  }

  disconnect() {}
  unobserve() {}
}

describe('CredentialErrorEventsList', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    vi.stubGlobal('ResizeObserver', TestResizeObserver)
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
      callback(performance.now())
      return 0
    })
    vi.stubGlobal('cancelAnimationFrame', () => undefined)
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function getBoundingClientRect() {
      if (this.dataset.credentialErrorEventsScroller === 'true') return rect(920, 600)
      if (this.dataset.index !== undefined) {
        // Error 卡片会随正文和状态字段变化高度，不能用与 estimateSize 相同的固定值掩盖分页切换问题。
        const index = Number(this.dataset.index)
        return rect(920, 180 + (index % 3) * 60)
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
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('virtualizes the full first cursor page before additional pages are appended', async () => {
    const firstPage = Array.from({ length: 50 }, (_, index) => buildEvent(index))
    await act(async () => {
      root.render(
        <CredentialErrorEventsList
          events={firstPage}
          loading={false}
          hasMore
          loadingMore={false}
          autoLoadMore
          onLoadMore={() => undefined}
        />,
      )
      await Promise.resolve()
    })

    const scroller = container.querySelector<HTMLElement>('[data-credential-error-events-scroller="true"]')
    expect(scroller?.dataset.virtualized).toBe('true')
    expect(scroller?.querySelectorAll('[data-credential-error-event-id]').length).toBeLessThan(50)

    const secondPage = Array.from({ length: 100 }, (_, index) => buildEvent(index))
    await act(async () => {
      root.render(
        <CredentialErrorEventsList
          events={secondPage}
          loading={false}
          hasMore={false}
          loadingMore={false}
          autoLoadMore
          onLoadMore={() => undefined}
        />,
      )
      await Promise.resolve()
    })

    expect(scroller?.dataset.virtualized).toBe('true')
    expect(scroller?.dataset.loadedRowCount).toBe('100')
  })

  it('keeps a large loaded error history bounded in the DOM and advances the virtual window', async () => {
    const events = Array.from({ length: 1000 }, (_, index) => buildEvent(index))
    await act(async () => {
      root.render(
        <CredentialErrorEventsList
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

    const scroller = container.querySelector<HTMLElement>('[data-credential-error-events-scroller="true"]')
    expect(scroller?.dataset.virtualized).toBe('true')
    expect(scroller?.dataset.loadedRowCount).toBe('1000')
    const initialCards = Array.from(scroller?.querySelectorAll<HTMLElement>('[data-index]') ?? [])
    const initialIndexes = initialCards.map((card) => Number(card.dataset.index))
    expect(initialCards.length).toBeGreaterThan(0)
    expect(initialCards.length).toBeLessThan(100)

    scroller!.scrollTop = 110_000
    await act(async () => {
      scroller?.dispatchEvent(new Event('scroll'))
      const { promise, resolve } = Promise.withResolvers<void>()
      window.setTimeout(resolve, 0)
      await promise
    })

    const scrolledCards = Array.from(scroller?.querySelectorAll<HTMLElement>('[data-index]') ?? [])
    const scrolledIndexes = scrolledCards.map((card) => Number(card.dataset.index))
    expect(scrolledCards.length).toBeGreaterThan(0)
    expect(scrolledCards.length).toBeLessThan(100)
    expect(Math.min(...scrolledIndexes)).toBeGreaterThan(Math.min(...initialIndexes))
  })

  it('fully renders a small error page without virtual positioning', async () => {
    const events = Array.from({ length: 3 }, (_, index) => buildEvent(index))
    await act(async () => root.render(
      <CredentialErrorEventsList
        events={events}
        loading={false}
        hasMore={false}
        loadingMore={false}
        autoLoadMore
        onLoadMore={() => undefined}
      />,
    ))

    const scroller = container.querySelector<HTMLElement>('[data-credential-error-events-scroller="true"]')
    expect(scroller?.dataset.virtualized).toBe('false')
    expect(scroller?.querySelectorAll('[data-credential-error-event-id]')).toHaveLength(3)
    expect(scroller?.querySelector('[data-index]')).toBeNull()
    expect(scroller?.textContent).toContain('gpt-5.6-0')
    expect(scroller?.textContent).not.toContain('codex')
    expect(scroller?.textContent).not.toContain('usage_stats.credentials_error_retryable')
    expect(scroller?.textContent).not.toContain('usage_stats.credentials_error_not_retryable')
    expect(scroller?.textContent).toContain('usage_stats.credentials_error_next_retry')
    expect(scroller?.textContent).toContain('usage_stats.credentials_error_model_next_retry')
    const firstCard = scroller?.querySelector('[data-credential-error-event-id="1"]')
    const errorCodeBadge = Array.from(firstCard?.querySelectorAll('span') ?? [])
      .find((element) => element.textContent === 'rate_limit')
    expect(errorCodeBadge?.className).toContain('errorCode')
  })
})
