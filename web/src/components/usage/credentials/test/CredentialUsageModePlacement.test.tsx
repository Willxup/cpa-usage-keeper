// @vitest-environment happy-dom
import { act, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import { CredentialsSection } from '../CredentialsSection'
import { AiProviderCredentialsSection } from '../AiProviderCredentialsSection'
import { buildCredentialRows } from '../credentialViewModels'
import type { CredentialViewMode } from '../credentialListPreferences'
import { createAiProviderSectionProps, createCredentialSectionProps, createSectionIdentity } from './credentialSectionFixtures'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

describe('credential usage mode placement', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(async () => {
    await i18n.changeLanguage('en')
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })
  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    localStorage.clear()
    vi.restoreAllMocks()
  })

  it('keeps the usage switch last in the header and preserves its selection across modes', async () => {
    const row = buildCredentialRows([createSectionIdentity()], new Map())[0]
    row.displayQuotas = [{
      key: 'q', label: '5h', percent: 25, barPercent: 75, percentKind: 'used', status: 'ok', scope: 'quota_group',
      windowUsage: { tokens: '1K', cost: '$1.00' }, windowUsageEstimate: { tokens: '4K', cost: '$4.00' },
    }]
    const refresh = vi.fn(async () => undefined)
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
    function Harness() {
      const [mode, setMode] = useState<CredentialViewMode>('list')
      return <CredentialsSection {...createCredentialSectionProps({ rows: [row], total: 1, onRefreshQuota: refresh, onRefreshQuotaForAuthIndex: refresh, onResetQuotaForAuthIndex: refresh, onRefreshInspectionStatus: refresh, onStartInspection: refresh })} viewMode={mode} onViewModeChange={setMode} />
    }
    const click = async (label: string) => {
      const button = container.querySelector<HTMLButtonElement>(`button[aria-label="${label}"]`)
      expect(button, `missing ${label}`).not.toBeNull()
      await act(async () => button!.click())
    }
    const assertHeader = (names: string[]) => {
      const header = container.querySelector<HTMLElement>('[class*="credentialSectionHeader"]')!
      const title = header.querySelector('h3')!
      const help = header.querySelector('button[aria-label="About credential views"]')!
      const filter = header.querySelector('input[type="checkbox"]')!
      const groups = [...header.querySelectorAll('[role="group"]')]
      expect(groups.map((group) => group.getAttribute('aria-label'))).toEqual(names)
      for (const [before, after] of [[title, help], [help, filter], [filter, groups[0]]]) {
        expect(before.compareDocumentPosition(after) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
      }
      const controls = header.querySelector('[class*="credentialsTitleControls"]')!
      const children = [...controls.children]
      expect(children.at(-1)?.contains(groups.at(-1)!)).toBe(true)
      expect(children).toHaveLength(1 + names.length * 2)
      groups.forEach((group, index) => {
        expect(children[index * 2 + 1].className).toContain('Divider')
        expect(children[index * 2 + 2].contains(group)).toBe(true)
      })
      expect(container.querySelector('[class*="credentialPagination"] [aria-label="Quota usage display mode"]')).toBeNull()
      expect(container.querySelectorAll('[aria-label="Quota usage display mode"]'))
        .toHaveLength(names.includes('Quota usage display mode') ? 1 : 0)
    }

    await act(async () => root.render(<Harness />))
    assertHeader(['View layout', 'List side panel', 'Quota usage display mode'])
    expect(container.querySelector('[data-credential-quota-variant="full"]')?.textContent).toContain('1K')
    await click('Estimated usage')
    expect(container.querySelector('[data-credential-quota-variant="full"]')?.textContent).toContain('4K')
    await click('Health')
    assertHeader(['View layout', 'List side panel'])
    expect(container.querySelector('[data-credential-health-variant="full"]')).not.toBeNull()
    await click('Quota')
    assertHeader(['View layout', 'List side panel', 'Quota usage display mode'])
    expect(container.querySelector('button[aria-label="Estimated usage"]')?.getAttribute('aria-pressed')).toBe('true')
    expect(container.querySelector('[data-credential-quota-variant="full"]')?.textContent).toContain('4K')
    await click('Health')
    assertHeader(['View layout', 'List side panel'])
    await click('Cards view')
    assertHeader(['View layout', 'Quota usage display mode'])
    expect(container.querySelector('button[aria-label="Estimated usage"]')?.getAttribute('aria-pressed')).toBe('true')
    await click('List view')
    assertHeader(['View layout', 'List side panel'])
    expect(container.querySelector('[data-credential-health-variant="full"]')).not.toBeNull()
    await click('Quota')
    assertHeader(['View layout', 'List side panel', 'Quota usage display mode'])
    expect(container.querySelector('button[aria-label="Estimated usage"]')?.getAttribute('aria-pressed')).toBe('true')
    expect(container.querySelector('[data-credential-quota-variant="full"]')?.textContent).toContain('4K')
    expect(refresh).not.toHaveBeenCalled()
    expect(fetchSpy).not.toHaveBeenCalled()
  })

  it.each([0, 1])('hides usage and its divider for empty list health with total %i', async (total) => {
    localStorage.setItem('cpa.credentials.displayMode', 'health')
    await act(async () => root.render(<CredentialsSection {...createCredentialSectionProps({ viewMode: 'list', total, loading: total > 0 })} />))
    const controls = container.querySelector('[class*="credentialsTitleControls"]')!
    expect([...controls.querySelectorAll('[role="group"]')].map((group) => group.getAttribute('aria-label')))
      .toEqual(['View layout', 'List side panel'])
    expect(controls.children).toHaveLength(5)
    expect(container.querySelector('[aria-label="Quota usage display mode"]')).toBeNull()
  })

  it.each(['cards', 'list'] as const)('keeps %s usage control when rows load but total is nonzero', async (viewMode) => {
    await act(async () => root.render(<CredentialsSection {...createCredentialSectionProps({ viewMode, total: 1, loading: true })} />))
    expect(container.querySelectorAll('[aria-label="Quota usage display mode"]')).toHaveLength(1)
  })

  it.each(['cards', 'list'] as const)('preserves empty %s controls and does not add usage controls to AI providers', async (mode) => {
    await act(async () => root.render(<CredentialsSection {...createCredentialSectionProps({ viewMode: mode })} />))
    const header = container.querySelector('[class*="credentialSectionHeader"]')!
    expect([...header.querySelectorAll('[role="group"]')].map((group) => group.getAttribute('aria-label')))
      .toEqual(mode === 'list' ? ['View layout', 'List side panel'] : ['View layout'])
    expect(container.querySelector('[aria-label="Quota usage display mode"]')).toBeNull()
    await act(async () => root.render(<AiProviderCredentialsSection {...createAiProviderSectionProps({ viewMode: mode, total: 1 })} />))
    expect([...container.querySelectorAll('[class*="credentialSectionHeader"] [role="group"]')].map((group) => group.getAttribute('aria-label'))).toEqual(['View layout'])
    expect(container.querySelector('[aria-label="Quota usage display mode"]')).toBeNull()
  })
})
