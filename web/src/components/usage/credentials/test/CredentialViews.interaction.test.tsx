// @vitest-environment happy-dom
import { act, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import { CredentialsSection } from '../CredentialsSection'
import { AiProviderCredentialsSection } from '../AiProviderCredentialsSection'
import { buildAiProviderCredentialRows, buildCredentialRows } from '../credentialViewModels'
import type { CredentialViewMode } from '../credentialListPreferences'
import { createAiProviderSectionProps, createCredentialSectionProps, createSectionIdentity } from './credentialSectionFixtures'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

describe('credential view switching', () => {
  let container: HTMLDivElement
  let root: Root
  beforeEach(() => { container = document.createElement('div'); document.body.appendChild(container); root = createRoot(container) })
  afterEach(async () => { await act(async () => root.unmount()); container.remove(); localStorage.clear() })

  it('keeps the layout controls available and keyboard tooltip dismissible with no rows', async () => {
    await i18n.changeLanguage('en')
    function Empty() {
      const [mode, setMode] = useState<CredentialViewMode>('cards')
      return <CredentialsSection {...createCredentialSectionProps({ loading: true })} viewMode={mode} onViewModeChange={setMode} />
    }
    await act(async () => root.render(<Empty />))
    const choice = container.querySelector<HTMLButtonElement>('button[aria-label="List view"]')!
    await act(async () => choice.focus())
    const tooltipId = choice.getAttribute('aria-describedby')!
    expect(document.getElementById(tooltipId)?.textContent).toBe('List view')
    await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })))
    expect(choice.hasAttribute('aria-describedby')).toBe(false)
    expect(document.getElementById(tooltipId)).toBeNull()
    await act(async () => choice.click())
    expect(choice.getAttribute('aria-pressed')).toBe('true')
    expect(container.querySelector('[data-view-mode="list"]')).not.toBeNull()
  })

  it('keeps multiple Argon quotas and row-specific actions in the historical list', async () => {
    await i18n.changeLanguage('en')
    const identity = createSectionIdentity({ type: 'antigravity', provider: 'antigravity' })
    const row = buildCredentialRows([identity], new Map())[0]
    row.displayQuotas = ['a', 'b', 'c'].map((key) => ({
      key, label: `Argon ${key}`, percent: 25, barPercent: 75, percentKind: 'used' as const,
      status: 'ok' as const, scope: 'quota_group' as const,
      groupKey: 'antigravity-gemini-4-argon', groupLabel: 'Argon',
    }))
    const refresh = vi.fn(async () => undefined)
    await act(async () => root.render(<CredentialsSection {...createCredentialSectionProps({ rows: [row], total: 1, viewMode: 'list', onRefreshQuotaForAuthIndex: refresh })} />))
    const listRow = container.querySelector<HTMLElement>('[data-credential-list-row]')!
    expect(listRow.querySelector('[data-quota-group="antigravity-gemini-4-argon"]')).not.toBeNull()
    expect(listRow.querySelectorAll('[data-quota-key]')).toHaveLength(3)
    await act(async () => listRow.querySelector<HTMLButtonElement>(`button[aria-label*="${row.displayName}"][aria-busy]`)!.click())
    expect(refresh).toHaveBeenCalledWith(identity.identity)
  })

  it.each(['credentials', 'providers'] as const)('switches %s between distinct rows and cards', async (kind) => {
    await i18n.changeLanguage('en')
    const identity = createSectionIdentity()
    const credentialRows = buildCredentialRows([identity], new Map([[identity.identity, { id: identity.identity, quota: [{ key: 'q', label: '5h', usedPercent: 25 }] }]]))
    const providerRows = buildAiProviderCredentialRows([{ ...identity, auth_type: 2 }])
    function Harness() {
      const [mode, setMode] = useState<CredentialViewMode>('cards')
      return kind === 'credentials'
        ? <CredentialsSection {...createCredentialSectionProps({ rows: credentialRows, total: 1 })} viewMode={mode} onViewModeChange={setMode} />
        : <AiProviderCredentialsSection {...createAiProviderSectionProps({ rows: providerRows, total: 1 })} viewMode={mode} onViewModeChange={setMode} />
    }
    await act(async () => root.render(<Harness />))
    expect(container.querySelector('[data-credential-card]')).not.toBeNull()
    const list = container.querySelector<HTMLButtonElement>('button[aria-label="List view"]')!
    await act(async () => list.click())
    expect(list.getAttribute('aria-pressed')).toBe('true')
    expect(container.querySelector('[data-credential-card]')).toBeNull()
    expect(container.querySelector('[data-credential-identity]')).not.toBeNull()
    if (kind === 'providers') expect(container.querySelector('[data-credential-health-variant="full"]')).not.toBeNull()
    if (kind === 'credentials') {
      expect(container.querySelector('[data-credential-health-variant="full"]')).toBeNull()
      expect(container.querySelector('[data-credential-quota-variant="full"]')).not.toBeNull()
      const health = container.querySelector<HTMLButtonElement>('button[aria-label="Health"]')!
      await act(async () => health.click())
      expect(container.querySelector('[data-credential-health-variant="full"]')).not.toBeNull()
      expect(localStorage.getItem('cpa.credentials.displayMode')).toBe('health')
    }
    await act(async () => container.querySelector<HTMLButtonElement>('button[aria-label="Cards view"]')!.click())
    expect(container.querySelector('[data-credential-card]')).not.toBeNull()
  })
})
