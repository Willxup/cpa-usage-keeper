// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AiProviderCredentialsSection } from '../AiProviderCredentialsSection'
import { AuthFileCredentialsSection } from '../AuthFileCredentialsSection'
import type { AiProviderCredentialRow, AuthFileCredentialRow } from '../credentialViewModels'

vi.mock('react-i18next', () => {
  const t = (key: string, params?: { count?: number }) => key === 'usage_stats.credentials_count' ? String(params?.count ?? 0) : key
  return {
    initReactI18next: { type: '3rdParty', init: () => undefined },
    useTranslation: () => ({ t }),
  }
})

const identity = {
  id: 'credential-1',
  name: 'Credential One',
  auth_type: 1 as const,
  auth_type_name: 'oauth',
  identity: 'auth-1',
  type: 'openai',
  provider: 'OpenAI',
  total_requests: 1,
  success_count: 1,
  failure_count: 0,
  input_tokens: 10,
  output_tokens: 5,
  reasoning_tokens: 0,
  cache_read_tokens: 0,
  total_tokens: 15,
  last_aggregated_usage_event_id: '1',
  is_deleted: false,
  created_at: '2026-08-01T00:00:00Z',
  updated_at: '2026-08-17T00:00:00Z',
}

const commonRow = {
  displayName: 'Credential One',
  maskedIdentity: 'auth-1',
  providerLabel: 'OpenAI',
  typeLabel: 'openai',
  authTypeLabel: 'oauth',
  priorityLabel: 'P1',
  totalRequests: 1,
  successCount: 1,
  failureCount: 0,
  successRate: 100,
  totalTokens: 15,
  cacheReadRate: 0,
}

const authFileRow = {
  ...commonRow,
  identity,
  quota: [],
  quotaLoading: false,
  displayQuotas: [],
} as AuthFileCredentialRow

const aiProviderRow = {
  ...commonRow,
  identity: { ...identity, auth_type: 2 as const, auth_type_name: 'apikey' },
  authTypeLabel: 'apikey',
} as AiProviderCredentialRow

describe('credential detail name triggers', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    document.body.innerHTML = ''
  })

  it('opens details from an auth-file name while keeping alias editing separate', async () => {
    const onOpenDetails = vi.fn()
    await act(async () => root.render(
      <AuthFileCredentialsSection
        rows={[authFileRow]}
        total={1}
        page={1}
        totalPages={1}
        pageSize={10}
        activeOnly={false}
        sort="priority"
        loading={false}
        quotaRefreshing={false}
        quotaRefreshError=""
        quotaInspectionStatus={null}
        quotaInspectionLoading={false}
        quotaInspectionStarting={false}
        quotaInspectionError=""
        onPageChange={() => undefined}
        onPageSizeChange={() => undefined}
        onActiveOnlyChange={() => undefined}
        onSortChange={() => undefined}
        onRefreshQuota={async () => undefined}
        onRefreshQuotaForAuthIndex={async () => undefined}
        onResetQuotaForAuthIndex={async () => undefined}
        onSaveAlias={async () => undefined}
        onOpenDetails={onOpenDetails}
        onRefreshInspectionStatus={async () => undefined}
        onStartInspection={async () => undefined}
      />,
    ))

    const authFileTrigger = container.querySelector<HTMLButtonElement>('[data-credential-detail-trigger="true"]')
    const authFileRowElement = authFileTrigger?.closest('article')
    expect(authFileTrigger?.querySelector('[class*="credentialDetailNameArrow"]')).not.toBeNull()
    await act(async () => authFileRowElement?.click())
    expect(onOpenDetails).not.toHaveBeenCalled()
    await act(async () => authFileTrigger?.click())
    expect(onOpenDetails).toHaveBeenCalledWith(authFileRow)
    expect(container.querySelector('[aria-label="usage_stats.credentials_alias_edit"]')).not.toBeNull()
  })

  it('opens details from an AI-provider name', async () => {
    const onOpenDetails = vi.fn()
    await act(async () => root.render(
      <AiProviderCredentialsSection
        rows={[aiProviderRow]}
        total={1}
        page={1}
        totalPages={1}
        pageSize={10}
        activeOnly={false}
        sort="priority"
        loading={false}
        onOpenDetails={onOpenDetails}
        onPageChange={() => undefined}
        onPageSizeChange={() => undefined}
        onActiveOnlyChange={() => undefined}
        onSortChange={() => undefined}
      />,
    ))

    const aiProviderTrigger = container.querySelector<HTMLButtonElement>('[data-credential-detail-trigger="true"]')
    expect(aiProviderTrigger?.querySelector('[class*="credentialDetailNameArrow"]')).not.toBeNull()
    await act(async () => aiProviderTrigger?.click())
    expect(onOpenDetails).toHaveBeenCalledWith(aiProviderRow)
  })
})
