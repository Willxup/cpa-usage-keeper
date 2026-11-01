// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { beforeEach, afterEach, expect, it, vi } from 'vitest'
import type { CredentialRow } from '../credentialViewModels'
import { CredentialQuotaPanel } from '../CredentialsSection'

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({ t: (s: string) => s }),
}))

function baseRow(): CredentialRow {
  return {
    identity: {
      id: 'cred-1',
      identity: 'auth-1',
      name: 'Claude Test',
      type: 'claude',
      provider: 'claude',
      auth_type: 1,
      auth_type_name: 'oauth',
      prefix: '',
      disabled: false,
      is_deleted: false,
      total_requests: 0,
      success_count: 0,
      failure_count: 0,
      input_tokens: 0,
      output_tokens: 0,
      reasoning_tokens: 0,
      cache_read_tokens: 0,
      total_tokens: 0,
      last_aggregated_usage_event_id: '',
      created_at: '',
      updated_at: '',
    },
    displayName: 'Claude Test',
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
    windowCacheReadRate: null,
    quota: [{ key: 'five_hour', label: '5h', used: 25, limit: 100, remaining: 75 }],
    displayQuotas: [
      {
        key: 'five_hour',
        label: '5h',
        percent: 25,
        barPercent: 75,
        percentKind: 'used',
        status: 'ok',
      },
    ],
    quotaLoading: false,
  }
}

let root: Root
let container: HTMLDivElement

beforeEach(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
})

afterEach(() => {
  act(() => { root.unmount() })
  container.remove()
})

it('renders stale badge when observation is stale', () => {
  const row: CredentialRow = {
    ...baseRow(),
    observation: {
      provider: 'claude',
      identity_type: 'claude',
      auth_index: 'auth-1',
      source: 'adapter',
      observed_at: '2026-10-26T12:00:00Z',
      last_success_at: '2026-10-26T10:00:00Z',
      reset_semantics: 'absolute',
      freshness: 'stale',
      confidence: 0.5,
      status: 'failed',
      windows: [],
    },
  }

  act(() => {
    root.render(<CredentialQuotaPanel row={row} quotaUsageMode="remaining" />)
  })

  const staleBadge = container.querySelector('span[title*="Last updated: 2026-10-26T10:00:00Z"]')
  expect(staleBadge).not.toBeNull()
  expect(staleBadge?.textContent).toBe('Stale')
})

it('renders partial badge when observation status is partial', () => {
  const row: CredentialRow = {
    ...baseRow(),
    observation: {
      provider: 'claude',
      identity_type: 'claude',
      auth_index: 'auth-1',
      source: 'adapter',
      observed_at: '2026-10-26T12:00:00Z',
      reset_semantics: 'absolute',
      freshness: 'fresh',
      confidence: 0.85,
      status: 'partial',
      windows: [],
    },
  }

  act(() => {
    root.render(<CredentialQuotaPanel row={row} quotaUsageMode="remaining" />)
  })

  const partialBadge = container.querySelector('span[title*="Partial observation"]')
  expect(partialBadge).not.toBeNull()
  expect(partialBadge?.textContent).toBe('Partial')
})

it('does not render stale or partial badge when observation is complete and fresh', () => {
  const row: CredentialRow = {
    ...baseRow(),
    observation: {
      provider: 'claude',
      identity_type: 'claude',
      auth_index: 'auth-1',
      source: 'adapter',
      observed_at: '2026-10-26T12:00:00Z',
      reset_semantics: 'absolute',
      freshness: 'fresh',
      confidence: 1.0,
      status: 'complete',
      windows: [],
    },
  }

  act(() => {
    root.render(<CredentialQuotaPanel row={row} quotaUsageMode="remaining" />)
  })

  expect(container.textContent).not.toContain('Stale')
  expect(container.textContent).not.toContain('Partial')
})
