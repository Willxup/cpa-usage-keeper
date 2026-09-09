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

describe('CredentialProviderFilterBar reset behaviour', () => {
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

  it('keeps a restored filter while the type counts are still empty', async () => {
    // 首帧计数为空，此时重置会让持久化的供应商筛选活不过一次刷新。
    const onChange = vi.fn()
    await render([], 'openai', onChange)

    expect(onChange).not.toHaveBeenCalled()
  })

  it('resets a filter that loaded counts no longer offer', async () => {
    const onChange = vi.fn()
    await render([{ type: 'claude', count: 3 }], 'openai', onChange)

    expect(onChange).toHaveBeenCalledWith('all')
  })

  it('keeps a filter that loaded counts still offer', async () => {
    const onChange = vi.fn()
    await render([{ type: 'openai', count: 2 }, { type: 'claude', count: 3 }], 'openai', onChange)

    expect(onChange).not.toHaveBeenCalled()
  })
})
