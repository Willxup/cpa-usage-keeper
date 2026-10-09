// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AuthFileCredentialsSection, QuotaResetAction } from '../AuthFileCredentialsSection'
import type { ClaudeResetGrantsResponse, UsageQuotaResetResponse } from '@/lib/types'
import type { AuthFileCredentialRow } from '../credentialViewModels'
import { createAuthFileSectionProps } from './credentialSectionFixtures'
import * as api from '@/lib/api'
import styles from '../CredentialSections.module.scss'

globalThis.IS_REACT_ACT_ENVIRONMENT = true
vi.mock('react-i18next', () => ({ initReactI18next: { type: '3rdParty', init: () => undefined }, useTranslation: () => ({ t: (key: string, params?: Record<string, string | number>) => `${key}${params ? ':' + JSON.stringify(params) : ''}` }) }))

const status = (): ClaudeResetGrantsResponse => ({
  authIndex: 'claude',
  status: {
    eligible: true, atLimit: true, availableCount: 3, nextGrantId: 'spring',
    grants: [
      { id: 'spring', resetsLeft: 2, resetsTotal: 3, usableNow: true, paused: false, useRequiresLimit: true, clears: ['five_hour'], endsAt: '2026-10-20T00:00:00Z' },
      { id: 'winter', resetsLeft: 1, resetsTotal: 1, usableNow: true, paused: false, useRequiresLimit: true, clears: ['seven_day'] },
    ],
  },
  selectedGrantId: 'spring',
  organizationId: '11111111-1111-1111-1111-111111111111',
})

describe('Claude reset grants in the shared Keeper action', () => {
  let container: HTMLDivElement
  let root: Root
  beforeEach(() => { localStorage.clear(); container = document.createElement('div'); document.body.append(container); root = createRoot(container) })
  afterEach(async () => { await act(async () => root.unmount()); container.remove(); vi.restoreAllMocks() })
  const button = () => container.querySelector<HTMLButtonElement>('[role="dialog"] button[aria-busy]')!
  async function render(read: (auth: string, signal?: AbortSignal) => Promise<ClaudeResetGrantsResponse>, confirm: (grantId?: string, organizationId?: string) => Promise<UsageQuotaResetResponse | void> = async () => undefined, count = 3) {
    await act(async () => { root.render(<QuotaResetAction authIndex="claude" provider="claude" resetCredits={count} disabled={false} loading={false} fetchClaudeGrants={read} onConfirm={confirm} />) })
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-haspopup="dialog"]')!.click())
  }
  it('shows multi-use grants, GMT+8 expiry and only the selected reset scope', async () => {
    const confirm = vi.fn()
    await render(async () => status(), confirm)
    expect(container.textContent).toContain('claude_reset_grant_count:{"left":2}')
    expect(container.textContent).toContain('2026-10-20 08:00:00')
    expect(container.textContent).toContain('claude_reset_no_expiry')
    expect(container.textContent).toContain('five_hour')
    expect(container.querySelector('[data-claude-grant="spring"]')?.textContent).not.toContain('seven_day')
    expect(container.querySelector('[data-claude-grant="winter"]')?.textContent).toContain('seven_day')
    expect(container.textContent).not.toContain('claude_reset_recommended')
    expect(container.textContent).not.toContain('claude_reset_selected')
    expect(container.querySelector<HTMLInputElement>('input[type="radio"]')?.checked).toBe(true)
    expect(container.textContent).toContain('claude_reset_grants_title')
    await act(async () => button().click())
    expect(confirm).toHaveBeenCalledExactlyOnceWith('spring', '11111111-1111-1111-1111-111111111111')
  })
  it('blocks fresh consumption when eligibility reads fail', async () => {
    const confirm = vi.fn()
    await render(async () => { throw new Error('mock read failure') }, confirm)
    expect(button().disabled).toBe(true)
    expect(container.textContent).toContain('claude_reset_status_unavailable')
    expect(container.textContent).not.toContain('credentials_quota_reset_expiry_failed')
    expect(container.querySelector('[role="dialog"] [role="status"]')?.classList.contains(styles.credentialQuotaResetExpiryWarning)).toBe(true)
    expect(confirm).not.toHaveBeenCalled()
  })
  it('shows a neutral empty state after a successful query without enabling consumption', async () => {
    await render(async () => ({ ...status(), selectedGrantId: undefined, status: { ...status().status!, atLimit: false, availableCount: 0, grants: [] } }))
    expect(button().disabled).toBe(true)
    expect(container.textContent).toContain('claude_reset_unavailable')
    expect(container.querySelector('[role="dialog"] [role="status"]')?.classList.contains(styles.credentialQuotaResetExpiryWarning)).toBe(false)
  })
  it('keeps a successful reset successful when recovery failed', async () => {
    await render(async () => status(), async () => ({ authIndex: 'claude', code: 'reset', recoveryFailed: true }))
    await act(async () => button().click())
    expect(container.querySelector('[role="dialog"]')).toBeNull()
  })
  it('prevents double clicks while a claim is pending', async () => {
    const claim = Promise.withResolvers<UsageQuotaResetResponse>()
    const confirm = vi.fn(() => claim.promise)
    await render(async () => status(), confirm)
    await act(async () => { button().click(); button().click() })
    expect(confirm).toHaveBeenCalledTimes(1)
    await act(async () => { claim.resolve({ authIndex: 'claude', code: 'reset' }); await claim.promise })
  })
  it('selects another available grant without another lookup and submits that grant', async () => {
    const read = vi.fn(async () => status())
    const confirm = vi.fn(async () => ({ authIndex: 'claude', code: 'reset' }))
    await render(read, confirm)
    const radios = container.querySelectorAll<HTMLInputElement>('input[type="radio"]')
    expect(radios[0].checked).toBe(true)
    await act(async () => radios[1].click())
    expect(radios[1].checked).toBe(true)
    expect(read).toHaveBeenCalledTimes(1)
    const scope = container.querySelector('[role="radiogroup"]')?.nextElementSibling
    expect(scope?.textContent).toBe('seven_day')
    await act(async () => button().click())
    expect(confirm).toHaveBeenCalledExactlyOnceWith('winter', '11111111-1111-1111-1111-111111111111')
  })
  it('disables selection during submission and restores the recommended default on reopen', async () => {
    const claim = Promise.withResolvers<UsageQuotaResetResponse>()
    const confirm = vi.fn(() => claim.promise)
    const read = vi.fn(async () => status())
    await render(read, confirm)
    await act(async () => container.querySelectorAll<HTMLInputElement>('input[type="radio"]')[1].click())
    await act(async () => button().click())
    const radios = container.querySelectorAll<HTMLInputElement>('input[type="radio"]')
    expect(Array.from(radios).every((radio) => radio.disabled)).toBe(true)
    await act(async () => radios[0].click())
    expect(radios[1].checked).toBe(true)
    await act(async () => { claim.resolve({ authIndex: 'claude', code: 'reset' }); await claim.promise })
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-haspopup="dialog"]')!.click())
    expect(container.querySelectorAll<HTMLInputElement>('input[type="radio"]')[0].checked).toBe(true)
    expect(read).toHaveBeenCalledTimes(2)
  })
  it('preserves upstream labels and every scope value in order including duplicates', async () => {
    const response = status()
    response.status!.grants[0].label = 'Upstream 自由名称 <example>'
    response.status!.grants[0].clears = ['five_hour', 'seven_day', 'seven_day_overage_included', 'future_window', 'five_hour']
    await render(async () => response)
    const scope = 'five_hour / seven_day / seven_day_overage_included / future_window / five_hour'
    expect(container.querySelector('[data-claude-grant="spring"]')?.textContent).toContain(scope)
    expect(container.querySelector('[role="radiogroup"]')?.nextElementSibling?.textContent).toBe(scope)
    expect(container.textContent).toContain('Upstream 自由名称 <example>')
    expect(container.querySelector('example')).toBeNull()
  })
  it('blocks confirmation when profile did not provide an organization UUID', async () => {
    await render(async () => ({ ...status(), organizationId: undefined, status: { ...status().status!, grants: [] } }))
    expect(button().disabled).toBe(true)
    expect(container.textContent).toContain('claude_reset_status_unavailable')
    expect(container.querySelector('[role="dialog"] [role="status"]')?.classList.contains(styles.credentialQuotaResetExpiryWarning)).toBe(true)
  })
  it('shows unknown without automatic retry, persistence or another enabled confirmation', async () => {
    const read = vi.fn(async () => status())
    const confirm = vi.fn(async () => ({ authIndex: 'claude', code: 'unknown' }))
    await render(read, confirm)
    await act(async () => button().click())
    expect(container.textContent).toContain('claude_reset_unknown')
    expect(button().disabled).toBe(true)
    expect(confirm).toHaveBeenCalledTimes(1)
    expect(read).toHaveBeenCalledTimes(1)
    expect(localStorage.length).toBe(0)
    expect(container.textContent).not.toContain('claude_reset_retry')
  })
  it('leaves official refusals visible without consuming or selecting another grant', async () => {
    const confirm = vi.fn(async () => ({ authIndex: 'claude', code: 'not_limited' }))
    await render(async () => status(), confirm)
    await act(async () => button().click())
    expect(container.textContent).toContain('claude_reset_not_limited')
    expect(container.querySelector('[role="dialog"] [role="status"]')?.classList.contains(styles.credentialQuotaResetExpiryWarning)).toBe(true)
    expect(button().disabled).toBe(true)
    expect(confirm).toHaveBeenCalledExactlyOnceWith('spring', '11111111-1111-1111-1111-111111111111')
  })
  async function renderSection(count: number, identity = { provider: 'claude', type: 'claude' }, onConfirm = async () => undefined) {
    const row = { identity: { id: 'identity-1', identity: 'claude', ...identity, disabled: false, is_deleted: false }, displayName: 'Claude', typeLabel: 'Claude', quotaLoading: false, displayQuotas: [], quota: [], totalRequests: 1, successCount: 1, failureCount: 0, successRate: 100, totalTokens: 1, cacheReadRate: 0, windowCacheReadRate: 0, quotaResetCreditsAvailableCount: 7, claudeResetGrants: { ...status().status!, availableCount: count } } as unknown as AuthFileCredentialRow
    await act(async () => { root.render(<AuthFileCredentialsSection {...createAuthFileSectionProps({ rows: [row], total: 1, onResetQuotaForAuthIndex: onConfirm })} />) })
  }
  it('only shows the shared reset icon in the real section with a positive known count', async () => {
    await renderSection(0)
    expect(container.querySelector('[aria-haspopup="dialog"]')).toBeNull()
    await renderSection(3)
    expect(container.querySelector('[aria-haspopup="dialog"]')?.getAttribute('aria-label')).toContain('"count":"3"')
  })
  it.each([
    { provider: 'unknown-oauth', type: ' ClAuDe ', claude: true },
    { provider: '   ', type: 'claude', claude: true },
    { provider: ' CoDeX ', type: 'claude', claude: false },
    { provider: ' ClAuDe ', type: 'codex', claude: true },
    { provider: 'gemini', type: 'claude', claude: true },
  ])('uses the backend quota provider precedence for $provider / $type in the real section', async ({ provider, type, claude }) => {
    const readClaude = vi.spyOn(api, 'fetchClaudeResetGrants').mockResolvedValue(status())
    const readCodex = vi.spyOn(api, 'fetchUsageQuotaResetCredits').mockResolvedValue({ authIndex: 'claude', availableCount: 7, credits: [] })
    const confirm = vi.fn(async () => undefined)
    await renderSection(3, { provider, type }, confirm)
    const trigger = container.querySelector<HTMLButtonElement>('[aria-haspopup="dialog"]')!
    expect(trigger.getAttribute('aria-label')).toContain(`"count":"${claude ? 3 : 7}"`)
    await act(async () => trigger.click())
    expect(readClaude).toHaveBeenCalledTimes(claude ? 1 : 0)
    expect(readCodex).toHaveBeenCalledTimes(claude ? 0 : 1)
    await act(async () => button().click())
    if (claude) expect(confirm).toHaveBeenCalledExactlyOnceWith('claude', 'spring', status().organizationId)
    else expect(confirm).toHaveBeenCalledExactlyOnceWith('claude')
  })
})
