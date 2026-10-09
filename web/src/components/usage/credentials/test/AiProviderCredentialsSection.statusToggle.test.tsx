// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AiProviderCredentialsSection } from '../AiProviderCredentialsSection'
import { createAiProviderSectionProps } from './credentialSectionFixtures'
import type { AiProviderCredentialRow } from '../credentialViewModels'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({
    t: (key: string, params?: { count?: number }) => (key === 'usage_stats.credentials_count' ? `${params?.count ?? 0}` : key),
  }),
}))

const createRow = (overrides: { id?: string; identity?: string; type?: string; isDeleted?: boolean } = {}): AiProviderCredentialRow => ({
  identity: {
    id: overrides.id ?? '7',
    name: 'Provider Key',
    auth_type: 2,
    auth_type_name: 'apikey',
    // id 与 identity 刻意取不同值，任何把两个参数写反的实现都会在这里暴露。
    identity: overrides.identity ?? 'idx-provider',
    type: overrides.type ?? 'claude',
    provider: 'anthropic',
    is_deleted: overrides.isDeleted ?? false,
  },
  displayName: 'Provider Key',
  maskedIdentity: 'sk-provider',
  providerLabel: 'Claude',
  typeLabel: 'claude',
  authTypeLabel: 'apikey',
  priorityLabel: 'P1',
  totalRequests: 1,
  successCount: 1,
  failureCount: 0,
  successRate: 100,
  totalTokens: 10,
  cacheReadRate: null,
  windowCacheReadRate: null,
  lastUsedText: 'just now',
  statsUpdatedText: 'just now',
} as unknown as AiProviderCredentialRow)

describe('AiProviderCredentialsSection status toggle wiring', () => {
  let container: HTMLDivElement
  let root: Root

  const render = async (rows: AiProviderCredentialRow[], props: { onToggleStatus?: (identityId: string, authIndex: string, disabled: boolean) => void; statusPendingIdentityIds?: ReadonlySet<string> } = {}) => {
    await act(async () => root.render(
      <AiProviderCredentialsSection
        {...createAiProviderSectionProps({
          rows,
          total: rows.length,
          onToggleStatus: props.onToggleStatus,
          statusPendingIdentityIds: props.statusPendingIdentityIds,
        })}
      />,
    ))
  }

  beforeEach(() => {
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
  })

  it('passes the Keeper identity id and the CPA auth_index in that order', async () => {
    const onToggleStatus = vi.fn()
    await render([createRow()], { onToggleStatus })

    const button = container.querySelector('[data-credential-status-toggle="true"]') as HTMLButtonElement
    expect(button).not.toBeNull()

    await act(async () => button.click())

    // 参数顺序颠倒会让后端按错误的 auth_index 查询，因此这里必须顺序敏感。
    expect(onToggleStatus).toHaveBeenCalledWith('7', 'idx-provider', true)
  })

  it('keeps pending state keyed by the Keeper identity id', async () => {
    await render([createRow({ id: '7', identity: 'idx-provider' })], { statusPendingIdentityIds: new Set(['7']) })

    const button = container.querySelector('[data-credential-status-toggle="true"]') as HTMLButtonElement
    expect(button.getAttribute('aria-disabled')).toBe('true')
    expect(button.getAttribute('aria-describedby')).toBeTruthy()
  })

  it('does not render a toggle for provider types without single-credential disable support', async () => {
    await render([createRow({ type: 'openai' })])

    expect(container.querySelector('[data-credential-status-toggle="true"]')).toBeNull()
    expect(container.querySelector('[data-credential-status-unsupported="true"]')).not.toBeNull()
  })
})
