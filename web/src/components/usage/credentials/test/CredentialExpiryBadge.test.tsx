// @vitest-environment happy-dom

import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import { CredentialExpiryBadge } from '../CredentialExpiryBadge'

const rectAt = (left: number, top: number, width = 40, height = 20): DOMRect => ({
  x: left,
  y: top,
  left,
  top,
  right: left + width,
  bottom: top + height,
  width,
  height,
  toJSON: () => ({}),
})

const mountBadge = async () => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  await act(async () => {
    root.render(
      <CredentialExpiryBadge
        remainingDaysLabel="13d"
        expiresAtLabel="2026-08-01 00:00:00 UTC+08:00"
      />,
    )
  })
  return {
    badge: container.querySelector('span') as HTMLSpanElement,
    root,
    container,
  }
}

afterEach(async () => {
  document.body.innerHTML = ''
  await i18n.changeLanguage('en')
})

describe('CredentialExpiryBadge', () => {
  it('shows an immediate localized non-native tooltip with the exact expiry time', async () => {
    const localizedLabels = [
      ['en', 'Expires at: 2026-08-01 00:00:00 UTC+08:00'],
      ['zh', '过期时间：2026-08-01 00:00:00 UTC+08:00'],
      ['zh-TW', '到期時間：2026-08-01 00:00:00 UTC+08:00'],
    ] as const

    for (const [language, expectedTooltip] of localizedLabels) {
      await i18n.changeLanguage(language)
      const mounted = await mountBadge()
      mounted.badge.getBoundingClientRect = () => rectAt(100, 50)

      expect(mounted.badge.getAttribute('title')).toBeNull()
      expect(mounted.badge.getAttribute('aria-label')).toBe(`13d; ${expectedTooltip}`)

      await act(async () => {
        mounted.badge.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }))
      })

      const tooltip = document.body.querySelector('[role="tooltip"]') as HTMLDivElement
      expect(tooltip.textContent).toBe(expectedTooltip)
      expect(tooltip.style.left).toBe('148px')
      expect(tooltip.style.top).toBe('80px')

      await act(async () => {
        mounted.badge.dispatchEvent(new MouseEvent('mouseout', { bubbles: true }))
        mounted.root.unmount()
      })
      mounted.container.remove()
    }
  })

  it('keeps the tooltip visible for keyboard focus and repositions it on scroll', async () => {
    await i18n.changeLanguage('en')
    const mounted = await mountBadge()
    let currentRect = rectAt(100, 50)
    mounted.badge.getBoundingClientRect = () => currentRect

    await act(async () => mounted.badge.focus())
    const tooltip = document.body.querySelector('[role="tooltip"]') as HTMLDivElement
    expect(tooltip).not.toBeNull()

    await act(async () => {
      mounted.badge.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }))
      mounted.badge.dispatchEvent(new MouseEvent('mouseout', { bubbles: true }))
    })
    expect(document.body.querySelector('[role="tooltip"]')).not.toBeNull()

    currentRect = rectAt(300, 200)
    await act(async () => window.dispatchEvent(new Event('scroll')))
    expect(tooltip.style.left).toBe('320px')
    expect(tooltip.style.top).toBe('230px')

    await act(async () => mounted.badge.blur())
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()

    await act(async () => mounted.root.unmount())
    mounted.container.remove()
  })
})
