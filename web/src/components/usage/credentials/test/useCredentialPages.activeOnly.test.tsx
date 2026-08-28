// @vitest-environment happy-dom

import { act, useEffect } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useCredentialPages } from '../useCredentialPages'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

let latest: ReturnType<typeof useCredentialPages> | null = null

function Harness() {
  const result = useCredentialPages({ enabledAuthFiles: false, enabledAiProviders: true })
  useEffect(() => { latest = result }, [result])
  return null
}

describe('AI Provider active-only state', () => {
  let container: HTMLDivElement
  let root: Root
  let fetchMock: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    window.localStorage.clear()
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
    fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ identities: [], total_count: 0, page: 1, page_size: 10, total_pages: 0, type_counts: [] }),
    } as Response)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    latest = null
    fetchMock.mockRestore()
  })

  it('persists an independent switch and sends active_only only when enabled', async () => {
    window.localStorage.setItem('cpa-usage-keeper-auth-files-active-only', 'true')
    await act(async () => root.render(<Harness />))

    expect(latest?.authFileActiveOnly).toBe(true)
    expect(latest?.aiProviderActiveOnly).toBe(false)
    let parsed = new URL(String(fetchMock.mock.calls.at(-1)?.[0]), 'http://localhost')
    expect(parsed.searchParams.get('auth_type')).toBe('2')
    expect(parsed.searchParams.has('active_only')).toBe(false)

    await act(async () => latest?.setAiProviderPage(3))
    expect(latest?.aiProviderPage).toBe(3)

    await act(async () => latest?.setAiProviderActiveOnly(true))
    expect(latest?.authFileActiveOnly).toBe(true)
    expect(latest?.aiProviderActiveOnly).toBe(true)
    expect(latest?.aiProviderPage).toBe(1)
    expect(window.localStorage.getItem('cpa-usage-keeper-ai-providers-active-only')).toBe('true')

    parsed = new URL(String(fetchMock.mock.calls.at(-1)?.[0]), 'http://localhost')
    expect(parsed.searchParams.get('auth_type')).toBe('2')
    expect(parsed.searchParams.get('active_only')).toBe('true')
  })
})
