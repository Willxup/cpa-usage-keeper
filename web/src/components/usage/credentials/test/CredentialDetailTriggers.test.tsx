// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AiProviderCredentialsSection } from '../AiProviderCredentialsSection'
import { AuthFileCredentialsSection } from '../AuthFileCredentialsSection'
import { createAiProviderSectionProps, createAuthFileSectionProps } from './credentialSectionFixtures'
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
  windowCacheReadRate: null,
}

const authFileRow: AuthFileCredentialRow = {
  ...commonRow,
  identity,
  quota: [],
  quotaLoading: false,
  displayQuotas: [],
}

const aiProviderRow: AiProviderCredentialRow = {
  ...commonRow,
  identity: { ...identity, auth_type: 2 as const, auth_type_name: 'apikey' },
  authTypeLabel: 'apikey',
}

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
  })

  it('opens details from an auth-file name while keeping alias editing separate', async () => {
    const onOpenDetails = vi.fn()
    await act(async () => root.render(
      <AuthFileCredentialsSection
        {...createAuthFileSectionProps()}
        rows={[authFileRow]}
        total={1}
        onEdit={async () => undefined}
        onOpenDetails={onOpenDetails}
      />,
    ))

    const authFileTrigger = container.querySelector<HTMLButtonElement>('[data-credential-detail-trigger="true"]')
    const authFileRowElement = authFileTrigger!.closest('article')
    await act(async () => authFileRowElement!.click())
    expect(onOpenDetails).not.toHaveBeenCalled()
    await act(async () => authFileTrigger!.click())
    expect(onOpenDetails).toHaveBeenCalledWith(authFileRow)
    expect(container.querySelector('[aria-label="usage_stats.credentials_edit_title"]')).not.toBeNull()
  })

  it('opens details from an AI-provider name', async () => {
    const onOpenDetails = vi.fn()
    await act(async () => root.render(
      <AiProviderCredentialsSection
        {...createAiProviderSectionProps()}
        rows={[aiProviderRow]}
        total={1}
        onOpenDetails={onOpenDetails}
      />,
    ))

    const aiProviderTrigger = container.querySelector<HTMLButtonElement>('[data-credential-detail-trigger="true"]')
    await act(async () => aiProviderTrigger!.click())
    expect(onOpenDetails).toHaveBeenCalledWith(aiProviderRow)
  })
})
