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

const identities = (authType: 1 | 2, deleted = false) => [1, 2].map((index) => createSectionIdentity({
  id: `identity-${index}`,
  identity: `auth-${index}`,
  displayName: 'Shared Account',
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
      expect(title.tagName).toBe('BUTTON')
      expect(title.closest('h1, h2, h3, h4, h5, h6')).not.toBeNull()
      expect(title.querySelector('span')?.textContent).toBe('Shared Account')
      expect(card.querySelectorAll('[data-credential-detail-trigger="true"]')).toHaveLength(1)
      expect(card.querySelector('[data-credential-detail-entry="footer"]')).toBeNull()
      expect(title.closest('[title], [aria-describedby]')).toBeNull()
      expect(title.parentElement?.querySelector('[role="tooltip"]')).toBeNull()
      expect(Array.from(card.querySelectorAll('button')).some((button) => button.textContent?.trim() === 'Details')).toBe(false)

      await act(async () => title.click())
      expect(onOpenDetails.mock.calls.slice(index, index + 1)).toEqual([[rows[index]]])
      expect(container.querySelector('[role="menu"], [role="menuitem"]')).toBeNull()
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
      expect(reset.textContent?.trim()).toBe('')
      expect(reset.hasAttribute('title')).toBe(false)
      const tooltip = document.getElementById(reset.getAttribute('aria-describedby')!)
      expect(tooltip?.getAttribute('role')).toBe('tooltip')
      expect(tooltip?.querySelector('span')?.textContent).toBe('2')
      expect(tooltip?.textContent).toContain(i18n.t('usage_stats.credentials_quota_reset_tooltip_suffix'))
      await act(async () => reset.click())
      expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    } else {
      expect(action(i18n.t('usage_stats.credentials_refresh_single', { name: identity.displayName }))).toBeUndefined()
      expect(action(i18n.t('usage_stats.credentials_quota_reset_button', { count: '2' }))).toBeUndefined()
      expect(card.querySelector('[data-credential-quota-variant]')).toBeNull()
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
