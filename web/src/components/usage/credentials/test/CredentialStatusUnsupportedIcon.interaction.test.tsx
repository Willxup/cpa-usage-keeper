// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import { CredentialStatusUnsupportedIcon } from '../CredentialStatusToggle'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

describe('unsupported credential status icon', () => {
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
  })

  const icon = () => container.querySelector<HTMLElement>('[data-credential-status-unsupported="true"]')

  it('explains the limitation on keyboard focus instead of relying on a native title', async () => {
    await act(async () => root.render(<CredentialStatusUnsupportedIcon providerType="openai" displayName="OpenAI Compatibility" />))

    const target = icon()
    expect(target).not.toBeNull()
    expect(target?.getAttribute('tabindex')).toBe('0')
    expect(target?.getAttribute('title')).toBeNull()
    expect(target?.getAttribute('aria-label')).toBe('OpenAI Compatibility')

    // 说明节点常驻 DOM，聚焦当刻就能作为可访问描述被计算，而不是聚焦后才插入。
    const tooltip = container.querySelector('[data-credential-status-tooltip="true"]')
    expect(tooltip?.getAttribute('role')).toBe('tooltip')
    expect(tooltip?.textContent).toBe('OpenAI-compatible credentials cannot be enabled or disabled here.')
    expect(target?.getAttribute('aria-describedby')).toBe(tooltip?.id)

    await act(async () => target?.focus())
    expect(container.querySelector('[data-credential-status-tooltip="true"]')).toBe(tooltip)
  })

  it('never renders a clickable status toggle for the unsupported icon', async () => {
    await act(async () => root.render(<CredentialStatusUnsupportedIcon providerType="openai" displayName="OpenAI Compatibility" />))

    expect(container.querySelector('[data-credential-status-toggle="true"]')).toBeNull()
  })
})
