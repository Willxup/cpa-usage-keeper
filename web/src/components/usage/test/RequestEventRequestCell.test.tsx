// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import type { UsageEvent } from '@/lib/types'
import { RequestEventsTestCard } from './requestEventsFixtures'
import { CredentialRequestEventsList } from '../credentials/CredentialRequestEventsList'
import { credentialEventListDefaults } from '../credentials/test/credentialEventFixtures'

const event: UsageEvent = {
  id: 'stream-request', timestamp: '2026-10-10T02:00:00Z', model: 'gpt-5', source: 'openai',
  endpoint: 'POST /v1/responses', failed: false, latency_ms: 120,
  tokens: { input_tokens: 100, output_tokens: 60, reasoning_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, total_tokens: 160 },
}

describe.each(['log', 'credential'] as const)('Request tooltip in %s', (surface) => {
  let container: HTMLDivElement
  let root: Root
  const render = async (events: UsageEvent[]) => {
    await act(async () => root.render(surface === 'log'
      ? <RequestEventsTestCard events={events} visibleColumnIds={['request_type']} />
      : <CredentialRequestEventsList {...credentialEventListDefaults} events={events} />))
  }
  const requestCell = () => Array.from(container.querySelectorAll<HTMLTableCellElement>('tbody td'))
    .find((cell) => cell.getAttribute('aria-label')?.includes('/v1/'))!
  const tooltipLines = () => Array.from(document.body.querySelectorAll('[role="tooltip"] span'), (el) => el.textContent)

  beforeEach(async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    await i18n.changeLanguage('en')
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    await i18n.changeLanguage('en')
  })

  it.each([
    ['POST', true, 'Streaming'], ['GET', true, 'Streaming'],
    ['POST', false, 'Non-streaming'], ['GET', false, 'Non-streaming'],
    ['POST', null, 'Unknown'], ['GET', undefined, 'Unknown'],
  ] as const)('uses stream metadata for %s / stream=%s', async (method, stream, mode) => {
    await render([{ ...event, endpoint: `${method} /v1/responses`, stream }])
    const cell = requestCell()
    expect(cell.textContent).toContain(method === 'POST' ? 'SSE' : 'WS')
    expect(cell.querySelectorAll('svg')).toHaveLength(stream === true ? 1 : 0)
    if (stream === true) {
      expect(cell.querySelector('svg')?.getAttribute('width')).toBe(surface === 'credential' ? '13' : '14')
      expect(cell.querySelector('svg')?.getAttribute('height')).toBe(surface === 'credential' ? '13' : '14')
    }
    await act(async () => cell.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    expect(tooltipLines()).toEqual([
      `Request: ${method === 'POST' ? 'SSE' : 'WS'}`,
      `Endpoint: ${method} /v1/responses`,
      `Response mode: ${mode}`,
    ])
    await act(async () => cell.dispatchEvent(new MouseEvent('mouseout', { bubbles: true })))
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
  })

  it.each([
    ['en', 'Endpoint: ', 'Response mode: Streaming'],
    ['zh', '端点：', '响应模式：流式响应'],
    ['zh-TW', '端點：', '回應模式：串流回應'],
  ])('retains the full endpoint in the shared focus tooltip (%s)', async (language, endpointLabel, modeLine) => {
    await i18n.changeLanguage(language)
    const path = `/v1/responses/${'long-path-segment/'.repeat(20)}complete`
    await render([{ ...event, endpoint: `POST ${path}`, stream: true }])
    const cell = requestCell()
    expect(cell.textContent).not.toContain('POST ')
    expect(cell.textContent).toContain(path)
    expect(cell.querySelector('[title]')).toBeNull()
    expect(cell.tabIndex).toBe(0)
    await act(async () => cell.focus())
    expect(tooltipLines()).toContain(`${endpointLabel}POST ${path}`)
    expect(tooltipLines()).toContain(modeLine)
    expect(container.querySelector('[role="tooltip"]')).toBeNull()
    await act(async () => cell.blur())
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()

    await act(async () => cell.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    await render([])
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
  })

  it('keeps a missing endpoint and stream state unknown', async () => {
    await render([{ ...event, endpoint: undefined }])
    const cell = Array.from(container.querySelectorAll<HTMLTableCellElement>('tbody td'))
      .find((el) => el.getAttribute('aria-label')?.includes('Response mode: Unknown'))!
    expect(cell.querySelector('svg')).toBeNull()
    await act(async () => cell.focus())
    expect(tooltipLines()).toEqual(['Request: -', 'Endpoint: -', 'Response mode: Unknown'])
  })

  it('includes effort only in the credential detail tooltip', async () => {
    await render([{ ...event, stream: true, reasoning_effort: 'medium' }])
    await act(async () => requestCell().focus())
    expect(tooltipLines().some((line) => line?.includes('Effort: medium'))).toBe(surface === 'credential')
  })
})
