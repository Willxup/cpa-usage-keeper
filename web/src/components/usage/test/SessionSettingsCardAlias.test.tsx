// @vitest-environment happy-dom

import { act, type ComponentProps } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AuthManagedSessionItem } from '@/lib/types'
import { SessionSettingsCard } from '../SessionSettingsCard'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

const translations: Record<string, string> = {
  'usage_stats.session_settings_title': 'Session Management',
  'usage_stats.session_settings_subtitle': 'Review active sessions.',
  'usage_stats.session_settings_admin_label': 'Admin Session',
  'usage_stats.session_settings_type_admin': 'Admin',
  'usage_stats.session_settings_type_api_key': 'API Key',
  'usage_stats.session_settings_source_standard': 'Standalone',
  'usage_stats.session_settings_source_embed': 'CPAMC Embed',
  'usage_stats.session_settings_alias_edit': 'Edit session alias',
  'usage_stats.session_settings_alias_placeholder': 'Session alias',
  'usage_stats.session_settings_alias_save': 'Save session alias',
  'usage_stats.session_settings_alias_cancel': 'Cancel session alias edit',
  'usage_stats.session_settings_alias_saving': 'Saving session alias',
  'usage_stats.session_settings_current': 'In use',
  'usage_stats.session_settings_user_agent': 'User-Agent',
  'usage_stats.session_settings_unknown_value': 'Unknown',
  'usage_stats.session_settings_login_ip': 'Login IP',
  'usage_stats.session_settings_last_seen_at': 'Last active',
  'usage_stats.session_settings_login_at': 'Login',
  'usage_stats.session_settings_expires_at': 'Expires',
  'usage_stats.session_settings_logout_one': 'Sign out this session',
  'common.logout': 'Sign out',
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => translations[key] ?? key,
  }),
}))

const sessions: AuthManagedSessionItem[] = [
  {
    id: 'admin-session',
    kind: 'admin',
    role: 'admin',
    source: 'standard',
    alias: 'Office Mac',
    current: true,
  },
  {
    id: 'viewer-session',
    kind: 'api_key',
    role: 'api_key_viewer',
    source: 'embed',
    label: 'Team Key',
  },
]

const changeAlias = (input: HTMLInputElement, value: string) => {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}

describe('SessionSettingsCard admin alias editor', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(() => {
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    vi.restoreAllMocks()
  })

  const renderCard = (props: Partial<ComponentProps<typeof SessionSettingsCard>> = {}) => root.render(
    <SessionSettingsCard
      sessions={sessions}
      onLogout={() => undefined}
      onSaveAlias={async () => undefined}
      {...props}
    />,
  )

  it('shows session sources and saves only the admin alias inline', async () => {
    const onSaveAlias = vi.fn(async () => undefined)
    await act(async () => {
      renderCard({ onSaveAlias })
    })

    const editButton = container.querySelector<HTMLButtonElement>('button[aria-label="Edit session alias"]')
    const standardSource = container.querySelector<HTMLElement>('[data-session-source="standard"]')
    const embedSource = container.querySelector<HTMLElement>('[data-session-source="embed"]')
    const currentIndicator = container.querySelector<HTMLElement>('[data-session-current="true"]')
    const currentDot = container.querySelector<HTMLElement>('[data-session-current-dot="true"]')
    expect(editButton).not.toBeNull()
    expect(container.querySelectorAll('button[aria-label="Edit session alias"]')).toHaveLength(1)
    expect(currentIndicator!.textContent).toBe('In use')
    expect(currentDot!.getAttribute('aria-hidden')).toBe('true')
    expect(currentIndicator!.parentElement!.querySelector('button[aria-label="Sign out this session"]')).toBeNull()
    expect(standardSource!.textContent).toBe('Standalone')
    expect(embedSource!.textContent).toBe('CPAMC Embed')
    expect(container.textContent).toContain('Office Mac')

    await act(async () => editButton!.click())
    const input = container.querySelector<HTMLInputElement>('input[aria-label="Session alias"]')!
    expect(input.value).toBe('Office Mac')

    await act(async () => {
      changeAlias(input, 'Home Mac')
    })
    await act(async () => {
      container.querySelector<HTMLButtonElement>('button[aria-label="Save session alias"]')!.click()
      await Promise.resolve()
    })

    expect(onSaveAlias).toHaveBeenCalledWith('admin-session', 'Home Mac')
  })

  it('shows the localized default admin name when the alias is empty', async () => {
    await act(async () => {
      renderCard({ sessions: [{ ...sessions[0], alias: '' }] })
    })

    expect(container.textContent).toContain('Admin Session')
  })

  it('keeps every other open alias editor disabled while one alias is saving', async () => {
    const onSaveAlias = vi.fn(async () => undefined)
    const adminSessions: AuthManagedSessionItem[] = [
      sessions[0],
      {
        id: 'second-admin-session',
        kind: 'admin',
        role: 'admin',
        source: 'standard',
        alias: 'Home PC',
      },
    ]
    await act(async () => {
      renderCard({ sessions: adminSessions, onSaveAlias })
    })

    const editButtons = container.querySelectorAll<HTMLButtonElement>('button[aria-label="Edit session alias"]')
    await act(async () => {
      editButtons[0]!.click()
      editButtons[1]!.click()
    })
    const inputs = container.querySelectorAll<HTMLInputElement>('input[aria-label="Session alias"]')
    await act(async () => {
      changeAlias(inputs[1], 'Travel PC')
      renderCard({ sessions: adminSessions, aliasSavingId: 'admin-session', onSaveAlias })
    })

    const openInputs = container.querySelectorAll<HTMLInputElement>('input[aria-label="Session alias"]')
    const secondSaveButton = container.querySelector<HTMLButtonElement>('button[aria-label="Save session alias"]')
    expect(openInputs[0]!.disabled).toBe(true)
    expect(openInputs[1]!.disabled).toBe(true)
    expect(secondSaveButton!.disabled).toBe(true)
    await act(async () => secondSaveButton!.click())
    expect(onSaveAlias).not.toHaveBeenCalled()
  })

  it('restores focus to the edit button after cancelling or saving', async () => {
    const onSaveAlias = vi.fn(async () => undefined)
    await act(async () => {
      renderCard({ sessions: [sessions[0]], onSaveAlias })
    })

    await act(async () => {
      container.querySelector<HTMLButtonElement>('button[aria-label="Edit session alias"]')!.click()
    })
    const cancelInput = container.querySelector<HTMLInputElement>('input[aria-label="Session alias"]')
    expect(document.activeElement).toBe(cancelInput)
    await act(async () => {
      cancelInput!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    })
    expect(document.activeElement).toBe(container.querySelector('button[aria-label="Edit session alias"]'))

    await act(async () => {
      container.querySelector<HTMLButtonElement>('button[aria-label="Edit session alias"]')!.click()
    })
    const saveInput = container.querySelector<HTMLInputElement>('input[aria-label="Session alias"]')
    await act(async () => {
      changeAlias(saveInput!, 'Home Mac')
    })
    const saveButton = container.querySelector<HTMLButtonElement>('button[aria-label="Save session alias"]')
    await act(async () => {
      saveButton!.focus()
      saveButton!.click()
      await Promise.resolve()
    })
    expect(onSaveAlias).toHaveBeenCalledWith('admin-session', 'Home Mac')
    expect(document.activeElement).toBe(container.querySelector('button[aria-label="Edit session alias"]'))
  })
})
