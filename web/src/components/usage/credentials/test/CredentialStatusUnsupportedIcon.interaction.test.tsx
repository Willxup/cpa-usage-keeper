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

  it.each([undefined, 'info'] as const)('explains the limitation on keyboard focus for the %s variant instead of relying on a native title', async (variant) => {
    await act(async () => root.render(<CredentialStatusUnsupportedIcon providerType="openai" displayName="OpenAI Compatibility" variant={variant} />))

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

  it.each([undefined, 'info'] as const)('never renders a clickable status toggle for the unsupported %s variant', async (variant) => {
    await act(async () => root.render(<CredentialStatusUnsupportedIcon providerType="openai" displayName="OpenAI Compatibility" variant={variant} />))

    expect(container.querySelector('[data-credential-status-toggle="true"]')).toBeNull()
    expect(container.querySelector('button')).toBeNull()
  })
})
