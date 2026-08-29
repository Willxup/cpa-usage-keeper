// @vitest-environment happy-dom

import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import { AuthFileCredentialsSection } from '../AuthFileCredentialsSection'
import type { AuthFileCredentialRow } from '../credentialViewModels'

const row = (id: string, remainingDaysLabel: string, expiresAtLabel: string) => ({
  identity: { id, identity: id, is_deleted: false },
  displayName: `Codex ${id}`,
  maskedIdentity: id,
  providerLabel: 'Codex',
  typeLabel: 'codex',
  authTypeLabel: 'oauth',
  remainingDaysLabel,
  expiresAtLabel,
  totalRequests: 0,
  successCount: 0,
  failureCount: 0,
  successRate: null,
  totalTokens: 0,
  cacheReadRate: null,
  quota: [],
  quotaLoading: false,
  displayQuotas: [],
}) as AuthFileCredentialRow

const defaultRows = [
  row('auth-a', '13d', '2026-08-01 00:00:00 UTC+08:00'),
  row('auth-b', '14d', '2026-08-02 00:00:00 UTC+08:00'),
]

const sectionProps = {
  total: 2,
  page: 1,
  totalPages: 1,
  pageSize: 10,
  activeOnly: false,
  sort: 'priority' as const,
  loading: false,
  quotaRefreshing: false,
  quotaRefreshError: '',
  quotaInspectionStatus: null,
  quotaInspectionLoading: false,
  quotaInspectionStarting: false,
  quotaInspectionError: '',
  onPageChange: () => undefined,
  onPageSizeChange: () => undefined,
  onActiveOnlyChange: () => undefined,
  onSortChange: () => undefined,
  onRefreshQuota: async () => undefined,
  onRefreshQuotaForAuthIndex: async () => undefined,
  onResetQuotaForAuthIndex: async () => undefined,
  onRefreshInspectionStatus: async () => undefined,
  onStartInspection: async () => undefined,
}

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

afterEach(async () => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
  await i18n.changeLanguage('en')
})

describe('AuthFileCredentialsSection expiry tooltip', () => {
  it('keeps one live, single-line tooltip positioned within a mobile viewport', async () => {
    vi.stubGlobal('innerWidth', 320)
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const renderRows = async (rows: AuthFileCredentialRow[]) => {
      await act(async () => root.render(<AuthFileCredentialsSection {...sectionProps} rows={rows} />))
    }
    await renderRows(defaultRows)
    const firstBadge = container.querySelector('[aria-label^="13d;"]') as HTMLSpanElement
    const secondBadge = container.querySelector('[aria-label^="14d;"]') as HTMLSpanElement
    firstBadge.getBoundingClientRect = () => rectAt(290, 50, 20)

    expect(firstBadge.getAttribute('title')).toBeNull()
    await act(async () => firstBadge.focus())
    let tooltip = document.body.querySelector('[role="tooltip"]') as HTMLDivElement
    expect(tooltip.textContent).toBe('Expires at: 2026-08-01 00:00:00 UTC+08:00')
    expect(tooltip.style.left).toBe('162px')

    await act(async () => {
      secondBadge.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }))
    })
    expect(document.body.querySelectorAll('[role="tooltip"]')).toHaveLength(1)
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toContain('2026-08-02')

    await act(async () => {
      secondBadge.dispatchEvent(new MouseEvent('mouseout', { bubbles: true }))
      await i18n.changeLanguage('zh')
    })
    tooltip = document.body.querySelector('[role="tooltip"]') as HTMLDivElement
    expect(tooltip.textContent).toBe('过期时间：2026-08-01 00:00:00 UTC+08:00')

    await renderRows([
      row('auth-a', '13d', '2026-09-01 00:00:00 UTC+08:00'),
      defaultRows[1],
    ])
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toBe(
      '过期时间：2026-09-01 00:00:00 UTC+08:00',
    )

    await act(async () => root.unmount())
    container.remove()
  })
})
