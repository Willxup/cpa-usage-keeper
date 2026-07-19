// @vitest-environment happy-dom

import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it } from 'vitest'
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

afterEach(async () => {
  document.body.innerHTML = ''
  await i18n.changeLanguage('en')
})

describe('AuthFileCredentialsSection expiry tooltip', () => {
  it('keeps expiry metadata out of the collapsed table and exposes it after row expansion', async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    await act(async () => root.render(<AuthFileCredentialsSection {...sectionProps} rows={defaultRows} />))

    expect(container.querySelector('[aria-label^="13d;"]')).toBeNull()
    expect(container.querySelector('[aria-label^="14d;"]')).toBeNull()

    const expandButtons = container.querySelectorAll<HTMLButtonElement>('button[aria-expanded="false"]')
    expect(expandButtons).toHaveLength(2)
    await act(async () => expandButtons[0].click())

    const firstBadge = container.querySelector('[aria-label^="13d;"]') as HTMLSpanElement
    expect(firstBadge.textContent).toBe('13d')
    expect(firstBadge.getAttribute('aria-label')).toBe('13d; Expires at: 2026-08-01 00:00:00 UTC+08:00')
    expect(container.querySelector('[aria-label^="14d;"]')).toBeNull()

    await act(async () => root.unmount())
    container.remove()
  })
})
