// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it, vi } from 'vitest'
import { AuthFileQuotaPanel } from '../AuthFileCredentialsSection'
import { buildAuthFileCredentialRows } from '../credentialViewModels'

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({ t: (key: string) => key }),
}))

describe('xAI shared quota tooltip', () => {
  it.each([16, undefined])('shows product used percentages in the common tooltip, total=%s', async (usedPercent) => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const row = buildAuthFileCredentialRows([{ id: '1', identity: 'xai-auth', type: 'xai', is_deleted: false }], new Map([
      ['xai-auth', { id: 'xai-auth', quota: [{ key: 'billing.weekly', label: 'Weekly', scope: 'billing', usedPercent,
        usageBreakdown: [{ product: 'GrokBuild', usedPercent: 14 }, { product: 'GrokChat', usedPercent: 2 }, { product: 'Unknown' }],
      }] }],
    ]))[0]
    try {
      await act(async () => root.render(<AuthFileQuotaPanel row={row} quotaUsageMode="current" />))
      const button = container.querySelector('button')!
      expect(button).not.toBeNull()
      expect(button.hasAttribute('title')).toBe(false)
      expect(container.textContent).not.toContain('GrokBuild')
      expect(container.querySelectorAll('[style*="width:"]')).toHaveLength(usedPercent === undefined ? 0 : 1)
      if (usedPercent !== undefined) expect(container.innerHTML).toContain('width: 84%')
      await act(async () => button.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
      let tooltip = document.body.querySelector('[role="tooltip"]')!
      expect(tooltip.textContent).toContain('GrokBuild: 14%')
      expect(tooltip.textContent).toContain('GrokChat: 2%')
      expect(tooltip.textContent).toContain('Unknown: usage_stats.credentials_quota_usage_unknown')
      expect(tooltip.textContent).toContain(`usage_stats.credentials_quota_total_used: ${usedPercent === undefined ? 'usage_stats.credentials_quota_usage_unknown' : '16%'}`)
      expect(button.getAttribute('aria-describedby')).toBe(tooltip.id)
      expect(container.contains(tooltip)).toBe(false)
      // 后台缓存更新保留同一个 Weekly 组件，已打开提示必须同步读取新快照。
      const updatedRow = { ...row, displayQuotas: row.displayQuotas.map((quota) => ({
        ...quota, percent: 42, barPercent: 58,
        usageBreakdown: [{ product: 'GrokBuild', usedPercent: 40 }, { product: 'GrokChat', usedPercent: 2 }],
      })) }
      await act(async () => root.render(<AuthFileQuotaPanel row={updatedRow} quotaUsageMode="current" />))
      expect(container.querySelector('button')).toBe(button)
      expect(container.innerHTML).toContain('width: 58%')
      tooltip = document.body.querySelector('[role="tooltip"]')!
      expect(tooltip.textContent).toContain('GrokBuild: 40%')
      expect(tooltip.textContent).toContain('usage_stats.credentials_quota_total_used: 42%')
      expect(tooltip.textContent).not.toContain('GrokBuild: 14%')

      await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })))
      expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
      await act(async () => button.focus())
      expect(document.body.querySelector('[role="tooltip"]')).not.toBeNull()
      await act(async () => button.blur())
      expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
      await act(async () => button.click())
      tooltip = document.body.querySelector('[role="tooltip"]')!
      expect(tooltip).not.toBeNull()
      await act(async () => document.body.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true })))
      expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
    } finally {
      await act(async () => root.unmount())
      container.remove()
    }
  })
})
