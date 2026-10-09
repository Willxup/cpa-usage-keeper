// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { CredentialProviderFilterBar } from '../CredentialProviderFilterBar'
import type { UsageIdentityTypeCount } from '@/lib/types'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({
    t: (key: string) => key,
  }),
}))

describe('CredentialProviderFilterBar selection preservation', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(() => {
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
  })

  const render = async (typeCounts: UsageIdentityTypeCount[], value: 'all' | 'openai', onChange: (next: string) => void) => {
    await act(async () => root.render(
      <CredentialProviderFilterBar
        scope="ai-provider"
        typeCounts={typeCounts}
        value={value}
        onChange={onChange}
      />,
    ))
  }

  it.each([
    { label: 'not loaded', counts: [], count: 0 },
    { label: 'unavailable after loading', counts: [{ type: 'claude', count: 3 }], count: 0 },
    { label: 'available', counts: [{ type: 'openai', count: 2 }, { type: 'claude', count: 3 }], count: 2 },
  ])('keeps the selected provider when counts are $label and changes only on a click', async ({ counts, count }) => {
    const onChange = vi.fn()
    await render(counts, 'openai', onChange)
    expect(onChange).not.toHaveBeenCalled()
    expect(container.querySelector('[aria-pressed="true"]')?.textContent).toBe(`usage_stats.credentials_filter_openai${count}`)
    const all = container.querySelector<HTMLButtonElement>('button')!
    await act(async () => all.click())
    expect(onChange.mock.calls).toEqual([['all']])
  })
})
