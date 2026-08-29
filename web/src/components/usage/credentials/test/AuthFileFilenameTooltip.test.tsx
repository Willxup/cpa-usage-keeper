// @vitest-environment happy-dom

import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { AuthFileCredentialsSection } from '../AuthFileCredentialsSection'
import type { AuthFileCredentialRow } from '../credentialViewModels'

const fileName = 'claude-account-with-a-very-long-workspace-and-profile-name.json'

const row = {
  identity: {
    id: 'auth-1',
    identity: 'auth-1',
    file_name: fileName,
    is_deleted: false,
  },
  displayName: 'same@example.com',
  maskedIdentity: 'auth-1',
  providerLabel: 'Claude',
  typeLabel: 'claude',
  authTypeLabel: 'oauth',
  totalRequests: 0,
  successCount: 0,
  failureCount: 0,
  successRate: null,
  totalTokens: 0,
  cacheReadRate: null,
  quota: [],
  quotaLoading: false,
  displayQuotas: [],
} as AuthFileCredentialRow

const sectionProps = {
  rows: [row],
  total: 1,
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
  onSaveAlias: async () => undefined,
  onRefreshInspectionStatus: async () => undefined,
  onStartInspection: async () => undefined,
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('AuthFileCredentialsSection filename interaction', () => {
  it('opens details from the name without exposing the filename in a tooltip', async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const onOpenDetails = vi.fn()
    await act(async () => root.render(
      <AuthFileCredentialsSection
        {...sectionProps}
        onOpenDetails={onOpenDetails}
      />,
    ))

    const name = container.querySelector('[data-credential-detail-trigger="true"]') as HTMLButtonElement
    expect(name.textContent).toContain('same@example.com')
    expect(name.getAttribute('data-auth-file-name-tooltip-target')).toBeNull()
    expect(name.getAttribute('aria-label')).toBeNull()
    expect(container.textContent).not.toContain(fileName)

    await act(async () => name.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()

    await act(async () => name.focus())
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()

    await act(async () => name.click())
    expect(onOpenDetails).toHaveBeenCalledWith(row)

    await act(async () => root.unmount())
    container.remove()
  })
})
