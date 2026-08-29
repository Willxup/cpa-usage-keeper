// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { CredentialAliasEditor } from '../CredentialAliasEditor'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
  }),
}))

describe('CredentialAliasEditor interactions', () => {
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

  it('keeps every other open editor disabled while one alias is saving', async () => {
    const onSaveAlias = vi.fn(async () => undefined)
    const renderEditors = (savingId = '') => (
      <>
        <CredentialAliasEditor
          identityId="auth-file"
          displayName="Auth File"
          alias="Office Auth"
          saving={savingId === 'auth-file'}
          disabled={Boolean(savingId && savingId !== 'auth-file')}
          onSaveAlias={onSaveAlias}
        />
        <CredentialAliasEditor
          identityId="ai-provider"
          displayName="AI Provider"
          alias="Office Provider"
          saving={savingId === 'ai-provider'}
          disabled={Boolean(savingId && savingId !== 'ai-provider')}
          onSaveAlias={onSaveAlias}
        />
      </>
    )

    await act(async () => root.render(renderEditors()))
    const editButtons = container.querySelectorAll<HTMLButtonElement>('button[aria-label="usage_stats.credentials_alias_edit"]')
    await act(async () => {
      editButtons[0]?.click()
      editButtons[1]?.click()
    })
    const inputs = container.querySelectorAll<HTMLInputElement>('input[aria-label="usage_stats.credentials_alias_placeholder"]')
    await act(async () => {
      const valueSetter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
      valueSetter?.call(inputs[1], 'Travel Provider')
      inputs[1]?.dispatchEvent(new Event('input', { bubbles: true }))
      root.render(renderEditors('auth-file'))
    })

    const openInputs = container.querySelectorAll<HTMLInputElement>('input[aria-label="usage_stats.credentials_alias_placeholder"]')
    const secondSaveButton = container.querySelector<HTMLButtonElement>('button[aria-label="usage_stats.credentials_alias_save"]')
    expect(openInputs[0]?.disabled).toBe(true)
    expect(openInputs[1]?.disabled).toBe(true)
    expect(secondSaveButton?.disabled).toBe(true)
    await act(async () => secondSaveButton?.click())
    expect(onSaveAlias).not.toHaveBeenCalled()
  })

  it('restores focus to the edit button after cancelling or saving', async () => {
    const onSaveAlias = vi.fn(async () => undefined)
    await act(async () => {
      root.render(
        <CredentialAliasEditor
          identityId="auth-file"
          displayName="Auth File"
          alias="Office Auth"
          saving={false}
          onSaveAlias={onSaveAlias}
        />,
      )
    })

    await act(async () => {
      container.querySelector<HTMLButtonElement>('button[aria-label="usage_stats.credentials_alias_edit"]')?.click()
    })
    const cancelInput = container.querySelector<HTMLInputElement>('input[aria-label="usage_stats.credentials_alias_placeholder"]')
    expect(document.activeElement).toBe(cancelInput)
    await act(async () => {
      cancelInput?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    })
    expect(document.activeElement).toBe(container.querySelector('button[aria-label="usage_stats.credentials_alias_edit"]'))

    await act(async () => {
      container.querySelector<HTMLButtonElement>('button[aria-label="usage_stats.credentials_alias_edit"]')?.click()
    })
    const saveInput = container.querySelector<HTMLInputElement>('input[aria-label="usage_stats.credentials_alias_placeholder"]')
    await act(async () => {
      const valueSetter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
      valueSetter?.call(saveInput, 'Home Auth')
      saveInput?.dispatchEvent(new Event('input', { bubbles: true }))
    })
    const saveButton = container.querySelector<HTMLButtonElement>('button[aria-label="usage_stats.credentials_alias_save"]')
    await act(async () => {
      saveButton?.focus()
      saveButton?.click()
      await Promise.resolve()
    })
    expect(onSaveAlias).toHaveBeenCalledWith('auth-file', 'Home Auth')
    expect(document.activeElement).toBe(container.querySelector('button[aria-label="usage_stats.credentials_alias_edit"]'))
  })
})
