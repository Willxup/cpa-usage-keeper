// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import { AiProviderCredentialsSection } from '../AiProviderCredentialsSection'
import { CredentialsSection } from '../CredentialsSection'
import { buildAiProviderCredentialRows, buildCredentialRows } from '../credentialViewModels'
import { createAiProviderSectionProps, createCredentialSectionProps, createSectionIdentity } from './credentialSectionFixtures'

vi.mock('@/lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/lib/api')>(),
  fetchUsageQuotaResetCredits: async (authIndex: string) => ({ authIndex, availableCount: 2, credits: [] }),
}))

const fileName = 'claude-account-with-a-very-long-workspace-and-profile-name.json'
const identities = (authType: 1 | 2, deleted = false) => [1, 2].map((index) => createSectionIdentity({
  id: `identity-${index}`,
  identity: `auth-${index}`,
  displayName: 'Shared Account',
  file_name: fileName,
  auth_type: authType,
  type: authType === 1 ? 'codex' : 'claude',
  is_deleted: deleted,
}))

describe('credential card detail triggers', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    await i18n.changeLanguage('en')
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
  })

  it.each([1, 2] as const)('opens the correct auth-type %s identity only from its title, even with duplicate names', async (authType) => {
    const onOpenDetails = vi.fn()
    const credentialRows = buildCredentialRows(identities(1))
    const aiRows = buildAiProviderCredentialRows(identities(2))
    const rows = authType === 1 ? credentialRows : aiRows
    await act(async () => root.render(authType === 1
      ? <CredentialsSection {...createCredentialSectionProps({ rows: credentialRows, total: 2, onOpenDetails })} />
      : <AiProviderCredentialsSection {...createAiProviderSectionProps({ rows: aiRows, total: 2, onOpenDetails })} />))

    const cards = container.querySelectorAll<HTMLElement>('[data-credential-card="true"]')
    expect(cards).toHaveLength(2)
    for (const [index, card] of Array.from(cards).entries()) {
      const title = card.querySelector<HTMLButtonElement>('[data-credential-detail-entry="title"]')!
      await act(async () => {
        title.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }))
        title.focus()
      })
      // 名称交互不暴露原始文件名；不约束标题内部的标签和样式结构。
      expect(document.body.textContent).not.toContain(fileName)
      // 原生提示继承最近祖先的 title；空 title 会阻断继续继承。
      const nativeTooltip = title.closest('[title]')?.getAttribute('title') ?? ''
      expect(nativeTooltip).not.toContain(fileName)
      await act(async () => title.click())
      expect(onOpenDetails.mock.calls.slice(index, index + 1)).toEqual([[rows[index]]])
    }
  })

  it.each([1, 2] as const)('keeps auth-type %s card contents and mutations separate from opening details', async (authType) => {
    const identity = identities(authType)[1]
    const onOpenDetails = vi.fn()
    const onEdit = vi.fn()
    const onToggleStatus = vi.fn()
    const onRefreshQuotaForAuthIndex = vi.fn(async () => undefined)
    const onSavePriority = vi.fn(async () => undefined)
    const credentialRows = buildCredentialRows([identity], new Map([[identity.identity, {
      id: identity.identity,
      rateLimitResetCreditsAvailableCount: 2,
      quota: [{ key: 'rate_limit.primary_window', window: { seconds: 18_000 }, remainingFraction: 0.75 }],
    }]]))
    const aiRows = buildAiProviderCredentialRows([identity])
    const rows = authType === 1 ? credentialRows : aiRows
    await act(async () => root.render(authType === 1
      ? <CredentialsSection {...createCredentialSectionProps({ rows: credentialRows, total: 1, onOpenDetails, onEdit, onToggleStatus, onSavePriority, onRefreshQuotaForAuthIndex })} />
      : <AiProviderCredentialsSection {...createAiProviderSectionProps({ rows: aiRows, total: 1, onOpenDetails, onEdit, onToggleStatus, onSavePriority })} />))

    const card = container.querySelector<HTMLElement>('[data-credential-card="true"]')!
    const action = (label: string) => Array.from(card.querySelectorAll<HTMLButtonElement>('button')).find((button) => button.getAttribute('aria-label') === label)!
    await act(async () => {
      card.click()
      card.querySelector<HTMLElement>('[role="listitem"]')!.click()
      Array.from(card.querySelectorAll<HTMLElement>('span')).find((node) => node.textContent === '97.24%')!.click()
      card.querySelector<HTMLElement>('[data-quota-key]')?.click()
      action(i18n.t('usage_stats.credentials_edit_title')).click()
      card.querySelector<HTMLButtonElement>('[role="switch"]')!.click()
      action(i18n.t('usage_stats.credentials_priority_edit', { name: identity.displayName, priority: 1 })).click()
    })

    expect(onEdit).toHaveBeenCalledWith(rows[0])
    expect(onToggleStatus).toHaveBeenCalledWith(identity.id, identity.identity, true)
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })))

    if (authType === 1) {
      await act(async () => action(i18n.t('usage_stats.credentials_refresh_single', { name: identity.displayName })).click())
      expect(onRefreshQuotaForAuthIndex).toHaveBeenCalledWith(identity.identity)
      const reset = action(i18n.t('usage_stats.credentials_quota_reset_button', { count: '2' }))
      await act(async () => reset.click())
      expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    }
    expect(onOpenDetails).not.toHaveBeenCalled()
  })

  it.each([1, 2] as const)('keeps deleted auth-type %s history readable without mutation controls', async (authType) => {
    const onOpenDetails = vi.fn()
    const onEdit = vi.fn()
    const onToggleStatus = vi.fn()
    const onRefreshQuotaForAuthIndex = vi.fn(async () => undefined)
    const credentialRows = buildCredentialRows(identities(1, true))
    const aiRows = buildAiProviderCredentialRows(identities(2, true))
    const rows = authType === 1 ? credentialRows : aiRows
    await act(async () => root.render(authType === 1
      ? <CredentialsSection {...createCredentialSectionProps({ rows: credentialRows, total: 2, onOpenDetails, onEdit, onToggleStatus, onRefreshQuotaForAuthIndex })} />
      : <AiProviderCredentialsSection {...createAiProviderSectionProps({ rows: aiRows, total: 2, onOpenDetails, onEdit, onToggleStatus })} />))

    const card = container.querySelector<HTMLElement>('[data-credential-card="true"]')!
    const triggers = card.querySelectorAll<HTMLButtonElement>('[data-credential-detail-trigger="true"]')
    expect(triggers).toHaveLength(1)
    for (const trigger of triggers) await act(async () => trigger.click())
    expect(onOpenDetails.mock.calls).toEqual([[rows[0]]])
    expect(card.querySelector('[role="switch"]')).toBeNull()
    for (const button of card.querySelectorAll<HTMLButtonElement>('button:not([data-credential-detail-trigger])')) {
      await act(async () => button.click())
    }
    expect(onEdit).not.toHaveBeenCalled()
    expect(onToggleStatus).not.toHaveBeenCalled()
    expect(onRefreshQuotaForAuthIndex).not.toHaveBeenCalled()
  })
})
