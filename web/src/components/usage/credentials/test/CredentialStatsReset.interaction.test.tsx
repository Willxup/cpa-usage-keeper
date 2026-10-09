// @vitest-environment happy-dom

import { act, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import type { UsageIdentity } from '@/lib/types'
import { CredentialDetailDrawer } from '../CredentialDetailDrawer'
import { buildAiProviderCredentialRows, updateCredentialDetailStats, type CredentialDetailSelection } from '../credentialViewModels'

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
  beforeEach(async () => {
    await i18n.changeLanguage('en')
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })
  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    vi.restoreAllMocks()
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
})
