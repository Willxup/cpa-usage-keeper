// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import { AiProviderCredentialsSection } from '../AiProviderCredentialsSection'
import { CredentialsSection } from '../CredentialsSection'
import { buildAiProviderCredentialRows, buildCredentialRows } from '../credentialViewModels'
import { createAiProviderSectionProps, createCredentialSectionProps, createSectionIdentity } from './credentialSectionFixtures'

const identities = [
  createSectionIdentity({ id: 'unlinked', identity: 'auth-unlinked', siwc_bound: false, use_siwc: false, siwc_status: 'off' }),
  createSectionIdentity({ id: 'disabled', identity: 'auth-disabled', siwc_bound: true, use_siwc: false, siwc_status: 'off' }),
  createSectionIdentity({ id: 'enabled', identity: 'auth-enabled', siwc_bound: true, use_siwc: true, siwc_status: 'valid' }),
  createSectionIdentity({ id: 'invalid', identity: 'auth-invalid', siwc_bound: false, use_siwc: true, siwc_status: 'invalid' }),
  createSectionIdentity({ id: 'unknown', identity: 'auth-unknown', siwc_bound: true, use_siwc: true, siwc_status: 'future' }),
  createSectionIdentity({ id: 'other', identity: 'auth-other', type: 'claude', siwc_bound: true, use_siwc: true, siwc_status: 'valid' }),
]

describe('Codex SIWC card status', () => {
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
    await i18n.changeLanguage('en')
  })

  it.each([
    ['en', ['Not linked', 'Not enabled', 'Enabled', 'Invalid']],
    ['zh', ['未绑定', '未启用', '已启用', '已失效']],
    ['zh-TW', ['未綁定', '未啟用', '已啟用', '已失效']],
  ])('renders the four localized states in %s without a placeholder or live region', async (language, labels) => {
    await i18n.changeLanguage(language)
    const rows = buildCredentialRows(identities)
    await act(async () => root.render(<CredentialsSection {...createCredentialSectionProps({ rows, total: rows.length })} />))
    for (const [index, id] of ['unlinked', 'disabled', 'enabled', 'invalid'].entries()) {
      const card = container.querySelector(`[data-credential-identity="${id}"]`)!
      const indicator = card.querySelector('[data-siwc-state]')!
      expect(indicator.textContent).toBe(`SIWC ${labels[index]}`)
      expect(indicator.querySelector('[aria-hidden="true"]')).not.toBeNull()
      expect(indicator.hasAttribute('role')).toBe(false)
      expect(indicator.hasAttribute('aria-live')).toBe(false)
      expect(indicator.querySelector('button, a, [tabindex]')).toBeNull()
    }
    for (const id of ['unknown', 'other']) {
      const card = container.querySelector(`[data-credential-identity="${id}"]`)!
      expect(card.querySelector('[data-siwc-state]')).toBeNull()
      expect(card.textContent).not.toContain('SIWC')
    }
  })

  it('leaves edit, refresh, reset, and the independent enabled switch usable', async () => {
    await i18n.changeLanguage('en')
    const row = buildCredentialRows([identities[2]])[0]
    row.quotaResetCreditsAvailableCount = 1
    const onEdit = vi.fn()
    const onRefreshQuotaForAuthIndex = vi.fn(async () => undefined)
    const onToggleStatus = vi.fn()
    await act(async () => root.render(<CredentialsSection {...createCredentialSectionProps({ rows: [row], total: 1, onEdit, onRefreshQuotaForAuthIndex, onToggleStatus })} />))
    const card = container.querySelector('[data-credential-identity="enabled"]')!
    const indicator = card.querySelector('[data-siwc-state="enabled"]')!
    const footer = indicator.parentElement!
    const edit = footer.querySelector<HTMLButtonElement>(`button[aria-label="${i18n.t('usage_stats.credentials_edit_title')}"]`)!
    const refresh = footer.querySelector<HTMLButtonElement>(`button[aria-label="${i18n.t('usage_stats.credentials_refresh_single', { name: row.displayName })}"]`)!
    const reset = footer.querySelector<HTMLButtonElement>(`button[aria-label="${i18n.t('usage_stats.credentials_quota_reset_button', { count: '1' })}"]`)!
    const toggle = card.querySelector<HTMLButtonElement>('[data-credential-status-toggle="true"]')!
    expect(footer.querySelectorAll('button')).toHaveLength(3)
    expect(reset.disabled).toBe(false)
    expect(toggle.getAttribute('aria-checked')).toBe('true')
    await act(async () => { edit.click(); refresh.click(); toggle.click() })
    expect(onEdit).toHaveBeenCalledWith(row)
    expect(onRefreshQuotaForAuthIndex).toHaveBeenCalledWith('auth-enabled')
    expect(onToggleStatus).toHaveBeenCalledWith('enabled', 'auth-enabled', true)
  })

  it('does not show SIWC in AI provider cards even when the data contains SIWC values', async () => {
    const provider = createSectionIdentity({ auth_type: 2, type: 'codex', siwc_bound: true, use_siwc: true, siwc_status: 'valid' })
    const rows = buildAiProviderCredentialRows([provider])
    await act(async () => root.render(<AiProviderCredentialsSection {...createAiProviderSectionProps({ rows, total: 1 })} />))
    expect(container.querySelector('[data-credential-card="true"]')).not.toBeNull()
    expect(container.querySelector('[data-siwc-state]')).toBeNull()
    expect(container.textContent).not.toContain('SIWC')
  })
})
