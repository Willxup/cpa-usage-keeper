// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import { AiProviderCredentialsSection } from '../AiProviderCredentialsSection'
import { CredentialsSection } from '../CredentialsSection'
import { AiProviderCardsView } from '../cards/AiProviderCardsView'
import { CredentialCardsView } from '../cards/CredentialCardsView'
import { buildAiProviderCredentialRows, buildCredentialRows } from '../credentialViewModels'
import { createAiProviderSectionProps, createCredentialSectionProps, createSectionIdentity } from './credentialSectionFixtures'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

const identity = createSectionIdentity({ displayName: `Account ${'long-name-'.repeat(20)}` })
const credential = () => buildCredentialRows([identity], new Map())[0]
const provider = () => buildAiProviderCredentialRows([{ ...identity, auth_type: 2 }])[0]

function describedTooltip(button: HTMLButtonElement) {
  const id = button.getAttribute('aria-describedby')
  expect(id).toBeTruthy()
  const tooltip = document.getElementById(id!)
  expect(tooltip?.getAttribute('role')).toBe('tooltip')
  expect(button.hasAttribute('title')).toBe(false)
  return tooltip!
}

describe('card action tooltips', () => {
  let container: HTMLDivElement
  let root: Root
  beforeEach(() => { localStorage.clear(); container = document.createElement('div'); document.body.appendChild(container); root = createRoot(container) })
  afterEach(async () => { await act(async () => root.unmount()); container.remove(); localStorage.clear() })

  it.each([
    ['en', 'Edit credential', 'Refresh quota'],
    ['zh', '编辑凭证', '刷新限额'],
    ['zh-TW', '編輯憑證', '重新整理限額'],
  ])('describes both credential actions in %s without the account name and executes each once', async (language, editText, refreshText) => {
    await i18n.changeLanguage(language)
    const row = credential()
    row.quotaResetCreditsAvailableCount = 2
    const onEdit = vi.fn()
    const onRefresh = vi.fn(async () => undefined)
    const onReset = vi.fn(async () => undefined)
    await act(async () => root.render(<CredentialCardsView rows={[row]} quotaUsageMode="current" onEdit={onEdit} onRefreshQuotaForAuthIndex={onRefresh} onResetQuotaForAuthIndex={onReset} />))
    const edit = container.querySelector<HTMLButtonElement>(`button[aria-label="${editText}"]`)!
    const refresh = container.querySelector<HTMLButtonElement>('button[aria-busy][aria-label*="Account "]')!
    expect(describedTooltip(edit).textContent).toBe(editText)
    expect(describedTooltip(refresh).textContent).toBe(refreshText)
    expect(edit.getAttribute('aria-describedby')).not.toBe(refresh.getAttribute('aria-describedby'))
    expect(refresh.getAttribute('aria-label')).toContain(identity.displayName)
    await act(async () => { edit.click(); refresh.click() })
    expect(onEdit).toHaveBeenCalledTimes(1)
    expect(onEdit).toHaveBeenCalledWith(row)
    expect(onRefresh).toHaveBeenCalledTimes(1)
    expect(onRefresh).toHaveBeenCalledWith(identity.identity)
    expect(onReset).not.toHaveBeenCalled()
  })

  it('adds only the existing edit action to AI cards', async () => {
    await i18n.changeLanguage('en')
    const row = provider()
    const onEdit = vi.fn()
    await act(async () => root.render(<AiProviderCardsView rows={[row]} onEdit={onEdit} />))
    const card = container.querySelector<HTMLElement>('[data-credential-card]')!
    const edit = card.querySelector<HTMLButtonElement>('button[aria-label="Edit credential"]')!
    expect(describedTooltip(edit).textContent).toBe('Edit credential')
    expect(card.querySelector('button[aria-label^="Refresh quota"]')).toBeNull()
    await act(async () => edit.click())
    expect(onEdit).toHaveBeenCalledTimes(1)
    expect(onEdit).toHaveBeenCalledWith(row)
  })

  it('preserves disabled and pending refresh/reset behavior', async () => {
    await i18n.changeLanguage('en')
    const row = credential()
    row.refreshStatus = 'running'
    row.quotaResetCreditsAvailableCount = 2
    const onRefresh = vi.fn(async () => undefined)
    const onReset = vi.fn(async () => undefined)
    const render = () => root.render(<CredentialCardsView rows={[row]} quotaUsageMode="current" onEdit={() => undefined} onRefreshQuotaForAuthIndex={onRefresh} onResetQuotaForAuthIndex={onReset} />)
    await act(async () => render())
    let refresh = container.querySelector<HTMLButtonElement>('button[aria-label^="Refresh quota for"]')!
    let reset = container.querySelector<HTMLButtonElement>('button[aria-label^="Reset quota"]')!
    expect(refresh.disabled).toBe(true)
    expect(refresh.getAttribute('aria-busy')).toBe('true')
    expect(describedTooltip(refresh).textContent).toBe('Refresh quota')
    expect(reset.disabled).toBe(true)
    await act(async () => { refresh.click(); reset.click() })
    expect(onRefresh).not.toHaveBeenCalled()
    expect(onReset).not.toHaveBeenCalled()
    row.refreshStatus = undefined
    row.identity = { ...row.identity, is_deleted: true }
    await act(async () => render())
    refresh = container.querySelector<HTMLButtonElement>('button[aria-label^="Refresh quota for"]')!
    reset = container.querySelector<HTMLButtonElement>('button[aria-label^="Reset quota"]')!
    expect(refresh.disabled).toBe(true)
    expect(refresh.getAttribute('aria-busy')).toBe('false')
    expect(reset.disabled).toBe(true)
    expect(container.querySelector('button[aria-label="Edit credential"]')).toBeNull()
    await act(async () => { refresh.click(); reset.click() })
    expect(onRefresh).not.toHaveBeenCalled()
    expect(onReset).not.toHaveBeenCalled()
  })

  it.each(['loading', 'error', 'empty'] as const)('does not add actions in the %s quota state', async (state) => {
    await i18n.changeLanguage('en')
    const row = credential()
    row.quotaLoading = state === 'loading'
    row.quotaError = state === 'error' ? 'Quota unavailable' : undefined
    await act(async () => root.render(<CredentialCardsView rows={[row]} quotaUsageMode="current" onEdit={() => undefined} onRefreshQuotaForAuthIndex={async () => undefined} onResetQuotaForAuthIndex={async () => undefined} />))
    const footer = container.querySelector<HTMLElement>('[data-credential-card] [class*="credentialCardFooter"]')!
    expect(footer.querySelectorAll('button')).toHaveLength(2)
    expect(describedTooltip(footer.querySelector<HTMLButtonElement>('button[aria-label="Edit credential"]')!).textContent).toBe('Edit credential')
    expect(describedTooltip(footer.querySelector<HTMLButtonElement>('button[aria-label^="Refresh quota for"]')!).textContent).toBe('Refresh quota')
  })

  it.each(['credentials', 'providers'] as const)('leaves the %s list edit button native tooltip unchanged', async (kind) => {
    await i18n.changeLanguage('en')
    const onEdit = vi.fn()
    await act(async () => root.render(kind === 'credentials'
      ? <CredentialsSection {...createCredentialSectionProps({ rows: [credential()], total: 1, viewMode: 'list', onEdit })} />
      : <AiProviderCredentialsSection {...createAiProviderSectionProps({ rows: [provider()], total: 1, viewMode: 'list', onEdit })} />))
    const row = container.querySelector<HTMLElement>('[data-credential-list-row]')!
    const edit = row.querySelector<HTMLButtonElement>('button[aria-label="Edit credential"]')!
    expect(edit.getAttribute('title')).toBe('Edit credential')
    expect(edit.hasAttribute('aria-describedby')).toBe(false)
    await act(async () => edit.click())
    expect(onEdit).toHaveBeenCalledTimes(1)
  })
})
