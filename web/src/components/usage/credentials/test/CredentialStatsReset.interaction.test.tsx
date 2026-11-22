// @vitest-environment happy-dom

import { act, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import type { UsageCredentialHealth, UsageIdentity } from '@/lib/types'
import { CredentialDetailDrawer } from '../CredentialDetailDrawer'
import { buildAiProviderCredentialRows, buildCredentialRows, updateCredentialDetailStats, type CredentialDetailSelection } from '../credentialViewModels'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

const identity = { id: '1', identity: 'fixture', auth_type: 2, name: 'Fixture', displayName: 'Fixture', type: 'openai', provider: 'OpenAI', total_requests: 10, success_count: 8, failure_count: 2, total_tokens: 1000, input_tokens: 600, cache_read_tokens: 300 } as UsageIdentity
const resetIdentity = { ...identity, stats_reset_at: '2026-09-10T16:00:00+08:00', period_stats: { total_requests: 0, success_count: 0, failure_count: 0, total_tokens: 0, input_tokens: 0, cache_read_tokens: 0 } }

function Harness({ reset }: { reset: (id: string) => Promise<UsageIdentity> }) {
  const [selection, setSelection] = useState<CredentialDetailSelection>({ kind: 'ai-provider', row: buildAiProviderCredentialRows([identity])[0] })
  return <CredentialDetailDrawer open selection={selection} onClose={() => undefined} onResetStats={async (id) => {
    const updated = await reset(id)
    setSelection((current) => updateCredentialDetailStats(current, updated))
  }} />
}

function button(label: string): HTMLButtonElement {
  const result = Array.from(document.querySelectorAll('button')).find((item) => item.textContent?.trim() === label)
  if (!result) throw new Error(`Missing button ${label}`)
  return result
}

describe('credential stats reset confirmation', () => {
  let root: Root
  let container: HTMLDivElement
  let unexpectedRequests: string[]
  beforeEach(async () => {
    unexpectedRequests = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const url = new URL(String(input), 'http://localhost')
      if (url.pathname === '/api/v1/usage/identities/1/models' && url.searchParams.get('range') === '7d') return Response.json({ models: [], summary: { total_tokens: 0 } })
      unexpectedRequests.push(url.pathname)
      throw new Error('Unexpected fetch')
    })
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

  it.each([
    ['en', 'Cumulative statistics', 'Reset statistics', 'Reset', 'Since reset:'],
    ['zh', '累计统计', '重置统计', '确认重置', '上次重置：'],
    ['zh-TW', '累計統計', '重置統計', '確認重置', '上次重置：'],
  ])('shows one cumulative heading before reset and the reset origin afterward in %s', async (language, heading, openLabel, confirmLabel, sinceLabel) => {
    await i18n.changeLanguage(language)
    const reset = vi.fn().mockResolvedValue(resetIdentity)
    await act(async () => root.render(<Harness reset={reset} />))
    const toolbar = document.querySelector<HTMLElement>('[data-credential-overview-section="cumulative"] [class*="summaryToolbar"]')!
    expect(toolbar.querySelector('h3')?.textContent).toBe(heading)
    expect([...toolbar.children].filter(child => child.tagName === 'SPAN')).toHaveLength(0)
    expect(toolbar.textContent?.split(heading)).toHaveLength(2)
    await act(async () => button(openLabel).click())
    await act(async () => button(confirmLabel).click())
    const origin = [...toolbar.children].find(child => child.tagName === 'SPAN')
    expect(origin?.textContent).toContain(sinceLabel)
    expect(origin?.textContent).toContain('2026-09-10 16:00:00 UTC+08:00')
    expect(toolbar.querySelector('h3')?.textContent).toBe(heading)
    expect(reset).toHaveBeenCalledExactlyOnceWith('1')
  })

  it('confirms once, disables repeat submission and displays the server reset time and empty period', async () => {
    const pending = Promise.withResolvers<UsageIdentity>()
    const reset = vi.fn(() => pending.promise)
    await act(async () => root.render(<Harness reset={reset} />))
    await act(async () => button('Reset statistics').click())
    expect(document.body.textContent).toContain('Fixture')
    expect(reset).not.toHaveBeenCalled()
    await act(async () => button('Reset').click())
    expect(button('Reset').disabled).toBe(true)
    expect(button('Cancel').disabled).toBe(true)
    await act(async () => button('Reset').click())
    expect(reset).toHaveBeenCalledExactlyOnceWith('1')
    await act(async () => pending.resolve(resetIdentity))
    expect(document.body.textContent).toContain('2026-09-10 16:00:00 UTC+08:00')
    const metrics = document.querySelector('[role="tabpanel"]')?.querySelectorAll('strong')
    expect(Array.from(metrics ?? []).slice(0, 4).map((node) => node.textContent)).toEqual(['0', '—', '0', '—'])
  })

  it('keeps existing statistics on cancel or failure and supports retry', async () => {
    const reset = vi.fn().mockRejectedValueOnce(new Error('unavailable')).mockResolvedValueOnce(resetIdentity)
    await act(async () => root.render(<Harness reset={reset} />))
    await act(async () => button('Reset statistics').click())
    await act(async () => button('Cancel').click())
    expect(reset).not.toHaveBeenCalled()
    await act(async () => button('Reset statistics').click())
    await act(async () => button('Reset').click())
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('Could not reset statistics')
    expect(document.querySelector('[role="tabpanel"] strong')?.textContent).toBe('10')
    await act(async () => button('Reset').click())
    expect(reset).toHaveBeenCalledTimes(2)
    expect(document.querySelector('[role="tabpanel"] strong')?.textContent).toBe('0')
  })

  it('updates cached activity labels when the language changes', async () => {
    const health: UsageCredentialHealth = {
      window_seconds: 18_000, bucket_seconds: 600,
      window_start: '2026-09-10T11:00:00+08:00', window_end: '2026-09-10T16:00:00+08:00',
      total_success: 610, total_failure: 12, success_rate: 98.1,
      input_tokens: 400, cache_read_tokens: 200, buckets: [],
    }
    const row = buildAiProviderCredentialRows([{ ...identity, credential_health: health }])[0]
    await act(async () => root.render(<CredentialDetailDrawer open selection={{ kind: 'ai-provider', row }} onClose={() => undefined} />))
    const activity = () => document.querySelector('[data-credential-overview-section="activity"]')!
    const metricTexts = () => [...activity().querySelectorAll('[data-credential-activity-metric]')].map(node => node.textContent)
    expect(metricTexts()).toEqual(['Requests622', 'Fail12', 'Cache rate50.00%'])
    expect(activity().textContent).toContain('Last 5h')
    await act(async () => i18n.changeLanguage('zh'))
    expect(metricTexts()).toEqual(['请求数622', '失败12', '缓存率50.00%'])
    expect(activity().textContent).toContain('最近 5 小时')
    await act(async () => i18n.changeLanguage('zh-TW'))
    expect(metricTexts()).toEqual(['請求數622', '失敗12', '快取率50.00%'])
    expect(activity().querySelectorAll('[role="listitem"]')).toHaveLength(30)
  })

  it.each([
    ['en', 'credential', 'Model usage', 'Model'],
    ['en', 'ai-provider', 'Model usage', 'Model'],
    ['zh', 'credential', '模型用量', '模型'],
    ['zh', 'ai-provider', '模型用量', '模型'],
    ['zh-TW', 'credential', '模型用量', '模型'],
    ['zh-TW', 'ai-provider', '模型用量', '模型'],
  ] as const)('shows the same model names in %s for %s details', async (language, kind, heading, column) => {
    const totals = { request_count: 1, input_tokens: 10, output_tokens: 0, reasoning_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, total_tokens: 10, cost_usd: 0, unavailable_cost_count: 0, cost_available: true }
    const models = [
      { ...totals, model: ' upstream-one ' },
      { ...totals, model: 'upstream-two', input_tokens: 5, total_tokens: 5 },
      { ...totals, model: '', input_tokens: 1, total_tokens: 1 },
    ]
    vi.mocked(globalThis.fetch).mockImplementation(async input => {
      const url = new URL(String(input), 'http://localhost')
      if (url.pathname === '/api/v1/usage/identities/1/models' && url.searchParams.get('range') === '7d') {
        return Response.json({ identity_id: '1', range: '7d', models, summary: { ...totals, request_count: 3, input_tokens: 16, total_tokens: 16 } })
      }
      unexpectedRequests.push(url.pathname)
      throw new Error('Unexpected fetch')
    })
    await i18n.changeLanguage(language)
    const selection: CredentialDetailSelection = kind === 'credential'
      ? { kind, row: buildCredentialRows([{ ...identity, auth_type: 1 }])[0] }
      : { kind, row: buildAiProviderCredentialRows([identity])[0] }
    await act(async () => root.render(<CredentialDetailDrawer open selection={selection} onClose={() => undefined} />))

    const panel = document.querySelector('[data-credential-model-usage]')!
    expect(panel.querySelector('h3')?.textContent).toBe(heading)
    expect(panel.querySelector('thead th:first-child')?.textContent).toBe(column)
    const names = [...panel.querySelectorAll('tbody th')]
    expect(names.map(name => name.textContent)).toEqual(['upstream-one', 'upstream-two', '—'])
    expect(names.map(name => name.querySelectorAll('[data-model-name]').length)).toEqual([1, 1, 1])
    expect(panel.textContent).not.toContain('→')
  })
})
