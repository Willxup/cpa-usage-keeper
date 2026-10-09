// @vitest-environment happy-dom
import { act, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { resolve } from 'node:path'
import { compile } from 'sass'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { QuotaAutoRefreshSettingsModal, QuotaInspectionModal } from '../AuthFileCredentialsSection'
import credentialStyles from '../CredentialSections.module.scss'

globalThis.IS_REACT_ACT_ENVIRONMENT = true
vi.mock('react-i18next', async (importOriginal) => ({
  ...await importOriginal<typeof import('react-i18next')>(),
  useTranslation: () => ({ t: (key: string) => key }),
}))
const credentialCSS = compile(resolve(process.cwd(), 'src/components/usage/credentials/CredentialSections.module.scss')).css
  .replace(/\.credentialInlineError\b/g, `.${credentialStyles.credentialInlineError}`)

describe('quota inspection select accessibility', () => {
  let container: HTMLDivElement
  let root: Root
  let stylesheet: HTMLStyleElement

  beforeEach(() => {
    document.documentElement.style.setProperty('--failure-badge-text', '#f1b0a6')
    stylesheet = document.createElement('style')
    stylesheet.textContent = credentialCSS
    document.head.appendChild(stylesheet)
    container = document.createElement('div')
    document.body.append(container)
    root = createRoot(container)
  })
  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    stylesheet.remove()
    document.documentElement.style.removeProperty('--failure-badge-text')
  })

  it('exposes the selected page size before and after changing it', async () => {
    const results = Array.from({ length: 12 }, (_, index) => ({
      auth_index: `auth-${index}`, name: `Account ${index}`, type: 'codex', status: 'normal' as const,
    }))
    await act(async () => root.render(<QuotaInspectionModal
      open status={{ total: 12, cached: 12, running: false, completed: true, normal: 12,
        limit_reached: 0, unauthorized_401: 0, payment_required_402: 0, unauthorized_401_402: 0,
        other_failed: 0, unknown: 0, results }}
      loading={false} starting={false} error="" onClose={() => undefined}
      onStart={async () => undefined} onRefreshStatus={async () => undefined}
    />))
    const trigger = document.querySelector<HTMLButtonElement>('button[aria-haspopup="listbox"]')!
    expect(trigger.getAttribute('aria-label')).toBe('usage_stats.rows_per_page: 10')
    await act(async () => trigger.click())
    const option = [...document.querySelectorAll<HTMLButtonElement>('[role="option"]')].find(item => item.textContent === '20')!
    await act(async () => option.click())
    expect(trigger.getAttribute('aria-label')).toBe('usage_stats.rows_per_page: 20')
    expect(trigger.textContent).toBe('20')
  })

  it.each([
    ['usage_stats.credentials_inspection_disable_invalid', 'btn-primary'],
    ['usage_stats.credentials_inspection_delete_invalid', 'btn-danger'],
  ])('uses shared action buttons for %s confirmation', async (actionLabel, confirmVariant) => {
    await act(async () => root.render(<QuotaInspectionModal
      open status={{ total: 1, cached: 1, running: false, completed: true, normal: 0,
        limit_reached: 0, unauthorized_401: 1, payment_required_402: 0, unauthorized_401_402: 1,
        other_failed: 0, unknown: 0, results: [{ auth_index: 'auth-1', file_name: 'account-1.json', name: 'Account 1', type: 'codex', status: 'unauthorized_401' }] }}
      loading={false} starting={false} error="" onClose={() => undefined}
      onStart={async () => undefined} onRefreshStatus={async () => undefined}
    />))
    const action = [...document.querySelectorAll<HTMLButtonElement>('button')]
      .find((button) => button.textContent === actionLabel)!
    await act(async () => action.click())

    const footers = document.querySelectorAll<HTMLElement>('.modal-footer')
    const footer = footers[footers.length - 1]
    const buttons = footer.querySelectorAll<HTMLButtonElement>('button')
    expect(buttons).toHaveLength(2)
    expect(buttons[0].className).toContain('btn-secondary')
    expect(buttons[0].className).toContain('btn-action')
    expect(buttons[1].className).toContain(confirmVariant)
    expect(buttons[1].className).toContain('btn-action')
  })

  it('uses the theme-aware failure color in quota workflow errors', async () => {
    await act(async () => root.render(<QuotaInspectionModal
      open status={null} loading={false} starting={false} error="Inspection failed" onClose={() => undefined}
      onStart={async () => undefined} onRefreshStatus={async () => undefined}
    />))
    let error = [...document.querySelectorAll<HTMLElement>('div')]
      .find((element) => element.textContent === 'Inspection failed')!
    expect(error.className).toContain(credentialStyles.credentialInlineError)
    expect(getComputedStyle(error).color).toBe('#f1b0a6')

    await act(async () => root.render(<QuotaAutoRefreshSettingsModal
      open enabled unit="hour" value="6" loading={false} saving={false} loaded error="Settings failed"
      onClose={() => undefined} onEnabledChange={() => undefined} onUnitChange={() => undefined}
      onValueChange={() => undefined} onSave={async () => undefined}
    />))
    error = document.querySelector<HTMLElement>('[role="alert"]')!
    expect(error.className).toContain(credentialStyles.credentialInlineError)
    expect(getComputedStyle(error).color).toBe('#f1b0a6')
  })

  it.each(['', '1'])('exposes the weekday when changing initial value "%s"', async (initialValue) => {
    const onValueChange = vi.fn()
    function Settings() {
      const [value, setValue] = useState(initialValue)
      return <QuotaAutoRefreshSettingsModal
        open enabled unit="week" value={value} loading={false} saving={false} loaded error=""
        onClose={() => undefined} onEnabledChange={() => undefined} onUnitChange={() => undefined}
        onValueChange={next => { onValueChange(next); setValue(next) }} onSave={async () => undefined}
      />
    }
    await act(async () => root.render(<Settings />))
    const trigger = document.querySelector<HTMLButtonElement>('button[aria-haspopup="listbox"]')!
    const initialLabel = initialValue ? 'usage_stats.credentials_auto_refresh_weekday_1' : 'usage_stats.credentials_auto_refresh_select'
    expect(trigger.getAttribute('aria-label')).toBe(`usage_stats.credentials_auto_refresh_weekday: ${initialLabel}`)
    await act(async () => trigger.click())
    const option = [...document.querySelectorAll<HTMLButtonElement>('[role="option"]')]
      .find(item => item.textContent === 'usage_stats.credentials_auto_refresh_weekday_3')!
    await act(async () => option.click())
    expect(onValueChange).toHaveBeenCalledWith('3')
    expect(trigger.getAttribute('aria-label')).toBe('usage_stats.credentials_auto_refresh_weekday: usage_stats.credentials_auto_refresh_weekday_3')
    expect(trigger.textContent).toBe('usage_stats.credentials_auto_refresh_weekday_3')
  })
})
