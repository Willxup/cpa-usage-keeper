// @vitest-environment happy-dom

import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import { CredentialStatusToggle } from '../CredentialStatusToggle'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

afterEach(async () => {
  document.body.innerHTML = ''
  await i18n.changeLanguage('en')
})

const renderToggle = async (props: Partial<Parameters<typeof CredentialStatusToggle>[0]> = {}) => {
  const onToggle = vi.fn()
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  const toggleProps = {
    providerType: 'claude',
    displayName: 'Claude Team',
    disabled: false,
    onToggle,
    ...props,
  }

  await act(async () => root.render(<CredentialStatusToggle {...toggleProps} />))

  return {
    onToggle,
    container,
    button: () => container.querySelector('[data-credential-status-toggle="true"]') as HTMLButtonElement,
    tooltip: () => container.querySelector('[data-credential-status-tooltip="true"]'),
    rerender: async (next: Partial<Parameters<typeof CredentialStatusToggle>[0]>) => {
      await act(async () => root.render(<CredentialStatusToggle {...toggleProps} {...next} />))
    },
    unmount: async () => {
      await act(async () => root.unmount())
      container.remove()
    },
  }
}

describe('CredentialStatusToggle', () => {
  it('toggles an enabled credential and exposes an always-mounted description', async () => {
    const view = await renderToggle()

    // 说明节点常驻 DOM，聚焦当刻就能作为可访问描述被计算，而不是聚焦后才插入。
    const tooltip = view.tooltip()
    expect(tooltip?.getAttribute('role')).toBe('tooltip')
    expect(view.button().getAttribute('aria-describedby')).toBe(tooltip?.id)
    expect(tooltip?.textContent).toBe('Click to disable this credential')
    expect(view.button().getAttribute('aria-pressed')).toBe('true')
    expect(view.button().getAttribute('aria-label')).toBe('Claude Team')

    await act(async () => view.button().click())
    expect(view.onToggle).toHaveBeenCalledWith(true)

    await view.unmount()
  })

  it('keeps the toggle focusable while pending and blocks repeated activation', async () => {
    const view = await renderToggle({ providerType: 'gemini', displayName: 'Gemini Team', disabled: true })

    await act(async () => view.button().focus())
    await view.rerender({ pending: true })

    // 原生 disabled 会把键盘焦点弹回 body，因此进行中必须用 aria-disabled 表达。
    expect(view.button().disabled).toBe(false)
    expect(view.button().hasAttribute('disabled')).toBe(false)
    expect(view.button().getAttribute('aria-disabled')).toBe('true')
    expect(view.button().getAttribute('aria-busy')).toBe('true')
    expect(view.button().getAttribute('aria-pressed')).toBe('false')
    expect(view.tooltip()?.textContent).toBe('Updating...')

    await act(async () => view.button().click())
    expect(view.onToggle).not.toHaveBeenCalled()

    await act(async () => view.button().focus())
    await view.rerender({ pending: false })
    expect(view.button().hasAttribute('aria-disabled')).toBe(false)
    await act(async () => view.button().click())
    expect(view.onToggle).toHaveBeenCalledWith(false)

    await view.unmount()
  })

  it('renders a static icon for read-only credentials', async () => {
    const view = await renderToggle({ providerType: 'codex', displayName: 'Codex Team', readOnly: true })

    expect(view.button()).toBeNull()
    expect(view.container.querySelector('[data-provider-brand-icon="codex"]')).not.toBeNull()

    await view.unmount()
  })
})
