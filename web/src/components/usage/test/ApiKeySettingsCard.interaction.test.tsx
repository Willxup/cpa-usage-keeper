// @vitest-environment happy-dom

import { act, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { CpaApiKeySettingsItem } from '@/lib/types'
import { ApiKeySettingsCard } from '../ApiKeySettingsCard'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

const translations: Record<string, string> = {
  'common.save': 'Save',
  'common.cancel': 'Cancel',
  'common.close': 'Close',
  'usage_stats.credentials_edit_title': 'Edit credential',
  'usage_stats.api_key_settings_edit_alias': 'Edit alias',
  'usage_stats.api_key_settings_plugin_identity': 'Third-party Identity #{{id}}',
  'usage_stats.api_key_settings_plugin': 'Third-party',
  'usage_stats.api_key_settings_native': 'Native',
  'usage_stats.api_key_settings_hide_key': 'Hide API key',
  'usage_stats.api_key_settings_title': 'API Key Settings',
  'usage_stats.api_key_settings_alias': 'Alias',
  'usage_stats.api_key_settings_show_key': 'Show API key',
  'usage_stats.api_key_settings_copy': 'Copy',
  'usage_stats.api_key_settings_copied': 'Copied',
  'usage_stats.api_key_settings_copy_success': 'API Key copied.',
  'usage_stats.api_key_settings_copy_failed': 'Unable to copy API Key.',
  'usage_stats.api_key_settings_alias_save_failed': 'Unable to save alias.',
}

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => (translations[key] ?? key).replace('{{id}}', values?.id ?? ''),
  }),
}))

const apiKey: CpaApiKeySettingsItem = {
  sourceType: 'native',
  accessProvider: '',
  id: '9007199254740993',
  apiKey: 'sk-alpha123456',
  keyAlias: 'Primary',
  displayKey: 'sk-*********123456',
  label: 'Primary',
  lastSyncedAt: '2026-05-13T00:00:00Z',
}
const longAlias = 'Production API key for a very long customer and application name that exceeds the card width'
const longApiKey: CpaApiKeySettingsItem = {
  ...apiKey,
  apiKey: `sk-${'abcdefghijklmnopqrstuvwxyz'.repeat(4)}123456`,
  keyAlias: longAlias,
  label: longAlias,
}

function ApiKeySettingsSavingHarness({ items, save }: {
  items: CpaApiKeySettingsItem[]
  save: (id: string, alias: string) => Promise<CpaApiKeySettingsItem>
}) {
  const [apiKeys, setApiKeys] = useState(items)
  const [savingId, setSavingId] = useState<string | null>(null)
  const handleSaveAlias = async (id: string, alias: string) => {
    setSavingId(id)
    try {
      const updated = await save(id, alias)
      setApiKeys((current) => current.map((item) => item.id === updated.id ? { ...item, ...updated } : item))
    } finally {
      setSavingId(null)
    }
  }
  return <ApiKeySettingsCard apiKeys={apiKeys} savingId={savingId} onSaveAlias={handleSaveAlias} />
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

describe('ApiKeySettingsCard interactions', () => {
  let container: HTMLDivElement
  let root: Root
  let clipboardDescriptor: PropertyDescriptor | undefined
  let execCommandDescriptor: PropertyDescriptor | undefined

  beforeEach(() => {
    clipboardDescriptor = Object.getOwnPropertyDescriptor(globalThis.navigator, 'clipboard')
    execCommandDescriptor = Object.getOwnPropertyDescriptor(document, 'execCommand')
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    if (clipboardDescriptor) {
      Object.defineProperty(globalThis.navigator, 'clipboard', clipboardDescriptor)
    } else {
      delete (globalThis.navigator as Navigator & { clipboard?: Clipboard }).clipboard
    }
    if (execCommandDescriptor) {
      Object.defineProperty(document, 'execCommand', execCommandDescriptor)
    } else {
      delete (document as Document & { execCommand?: (command: string) => boolean }).execCommand
    }
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  const renderCard = async (onNotice: ReturnType<typeof vi.fn>, items = [apiKey], onSaveAlias: (id: string, alias: string) => void | Promise<void> = () => undefined) => {
    await act(async () => {
      root.render(
        <ApiKeySettingsCard
          apiKeys={items}
          onSaveAlias={onSaveAlias}
          onNotice={onNotice}
        />,
      )
    })
  }

  const renderSavingHarness = async (items: CpaApiKeySettingsItem[], save: (id: string, alias: string) => Promise<CpaApiKeySettingsItem>) => {
    await act(async () => root.render(<ApiKeySettingsSavingHarness items={items} save={save} />))
  }

  const mockClipboard = (writeText: (value: string) => Promise<void>) => {
    Object.defineProperty(globalThis.navigator, 'clipboard', { configurable: true, value: { writeText } })
  }
  const mockLegacyCopy = (execCommand: (command: string) => boolean) => {
    Object.defineProperty(document, 'execCommand', { configurable: true, value: execCommand })
  }
  const button = (label: string) => {
    const element = container.querySelector<HTMLButtonElement>(`button[aria-label="${label}"]`)
    expect(element).not.toBeNull()
    return element!
  }
  const click = async (element: HTMLElement) => {
    await act(async () => { element.click(); await Promise.resolve() })
  }
  const textElements = (text: string) => Array.from(container.querySelectorAll<HTMLSpanElement>('span'))
    .filter((element) => element.children.length === 0 && element.textContent === text)
  const textElement = (text: string) => {
    const elements = textElements(text)
    expect(elements).toHaveLength(1)
    return elements[0]
  }
  const expectNoTextTooltip = async (element: HTMLElement) => {
    expect(element.closest('[title]')).toBeNull()
    await act(async () => element.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
    await act(async () => element.dispatchEvent(new MouseEvent('mouseout', { bubbles: true, relatedTarget: document.body })))
    await act(async () => element.focus())
    expect(document.activeElement).toBe(element)
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
    await act(async () => element.blur())
  }

  it('copies the raw key and resets the accessible success label', async () => {
    vi.useFakeTimers()
    const writeText = vi.fn(async () => undefined)
    const onNotice = vi.fn()
    mockClipboard(writeText)
    await renderCard(onNotice)

    const copyButton = button('Copy')
    expect(container.textContent).toContain(apiKey.displayKey)
    expect(container.textContent).not.toContain(apiKey.apiKey)

    await click(copyButton)

    expect(writeText).toHaveBeenCalledWith(apiKey.apiKey)
    expect(onNotice).toHaveBeenCalledWith('success', 'API Key copied.')
    button('Copied')

    await act(async () => vi.advanceTimersByTime(1600))
    expect(container.querySelector('button[aria-label="Copy"]')).not.toBeNull()
    expect(container.querySelector('button[aria-label="Copied"]')).toBeNull()
  })

  it.each([true, false])('restores the current control after legacy copy, success: %s', async (success) => {
    mockClipboard(async () => { throw new Error('blocked') })
    mockLegacyCopy(() => success)
    await renderCard(vi.fn())
    const copyButton = button('Copy')
    await act(async () => { copyButton.focus(); copyButton.click(); await Promise.resolve() })
    expect(document.activeElement).toBe(copyButton)
    expect(document.querySelector('textarea')).toBeNull()
  })

  it('keeps the copy action available when both copy paths fail', async () => {
    const writeText = vi.fn(async () => { throw new Error('blocked') })
    const execCommand = vi.fn(() => false)
    const onNotice = vi.fn()
    mockClipboard(writeText)
    mockLegacyCopy(execCommand)
    await renderCard(onNotice)

    const copyButton = button('Copy')

    await click(copyButton)

    expect(writeText).toHaveBeenCalledWith(apiKey.apiKey)
    expect(execCommand).toHaveBeenCalledWith('copy')
    expect(onNotice).toHaveBeenCalledWith('error', 'Unable to copy API Key.')
    expect(container.querySelector('button[aria-label="Copy"]')).not.toBeNull()
    expect(container.querySelector('button[aria-label="Copied"]')).toBeNull()
  })

  const pluginKey: CpaApiKeySettingsItem = {
    id: '7', sourceType: 'plugin', accessProvider: 'plugin:external-plugin:auth-provider',
    keyAlias: '', displayKey: 'Third-party Identity #7', label: 'Third-party Identity #7', lastSyncedAt: null,
    apiKey: 'private-principal-that-must-never-render',
  }
  const changeAlias = async (input: HTMLInputElement, value: string) => {
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
  }
  const openPluginEditor = async () => {
    const opener = button('Edit credential')
    await act(async () => { opener.focus(); opener.click() })
    const input = document.querySelector<HTMLInputElement>('[role="dialog"] input')!
    return { opener, input }
  }

  it('does not show a tooltip for a long native alias and still allows renaming', async () => {
    const save = vi.fn(async () => undefined)
    await renderCard(vi.fn(), [longApiKey], save)

    await expectNoTextTooltip(textElement(longAlias))
    const editButton = button('Edit credential')
    expect(editButton.title).toBe('Edit credential')
    await click(editButton)
    const input = document.querySelector<HTMLInputElement>('[role="dialog"] input')!
    expect(input.value).toBe(longAlias)
    await changeAlias(input, 'Renamed key')
    await act(async () => input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })))
    expect(save).toHaveBeenCalledExactlyOnceWith(longApiKey.id, 'Renamed key')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.activeElement).toBe(editButton)
  })

  it('does not show a tooltip for a masked or revealed long key and still toggles and copies it', async () => {
    const writeText = vi.fn(async () => undefined)
    const onNotice = vi.fn()
    mockClipboard(writeText)
    await renderCard(onNotice, [longApiKey])

    expect(container.textContent).not.toContain(longApiKey.apiKey)
    await expectNoTextTooltip(textElement(longApiKey.displayKey))
    await click(button('Copy'))
    expect(writeText).toHaveBeenNthCalledWith(1, longApiKey.apiKey)
    expect(onNotice).toHaveBeenCalledWith('success', 'API Key copied.')

    await click(button('Show API key'))
    expect(button('Hide API key').getAttribute('aria-pressed')).toBe('true')
    expect(container.textContent).not.toContain(longApiKey.displayKey)
    await expectNoTextTooltip(textElement(longAlias))
    await expectNoTextTooltip(textElement(longApiKey.apiKey!))
    await click(button('Copied'))
    expect(writeText).toHaveBeenNthCalledWith(2, longApiKey.apiKey)
    expect(writeText).toHaveBeenCalledTimes(2)

    await click(button('Hide API key'))
    expect(button('Show API key').getAttribute('aria-pressed')).toBe('false')
    expect(container.textContent).not.toContain(longApiKey.apiKey)
    await expectNoTextTooltip(textElement(longAlias))
    await expectNoTextTooltip(textElement(longApiKey.displayKey))
  })

  it('does not show a tooltip when the masked key is also used as the native name', async () => {
    await renderCard(vi.fn(), [{ ...longApiKey, keyAlias: '', label: longApiKey.displayKey }])
    const maskedTexts = textElements(longApiKey.displayKey)
    expect(maskedTexts).toHaveLength(2)
    for (const text of maskedTexts) await expectNoTextTooltip(text)
  })

  it('does not show a tooltip for a third-party name or identity while keeping the source tooltip', async () => {
    await renderCard(vi.fn(), [{ ...pluginKey, keyAlias: longAlias, label: longAlias }])
    await expectNoTextTooltip(textElement(longAlias))
    await expectNoTextTooltip(textElement('Third-party Identity #7'))

    const source = textElement('external-plugin:auth-provider')
    await act(async () => source.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toBe('external-plugin:auth-provider')
    await act(async () => source.dispatchEvent(new MouseEvent('mouseout', { bubbles: true, relatedTarget: document.body })))
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
  })

  it('keeps third-party cards limited to alias editing and shows the complete safe source in a portal tooltip', async () => {
    await renderCard(vi.fn(), [pluginKey])
    expect(container.querySelectorAll('button')).toHaveLength(1)
    expect(container.querySelector('input')).toBeNull()
    expect(container.textContent).toContain('Third-party Identity #7')
    expect(container.textContent).not.toContain(pluginKey.apiKey)
    const source = Array.from(container.querySelectorAll<HTMLSpanElement>('span[tabindex="0"]')).find((el) => el.textContent === 'external-plugin:auth-provider')!
    await act(async () => source.focus())
    expect(document.querySelector('[role="tooltip"]')?.textContent).toBe('external-plugin:auth-provider')
    expect(document.querySelector('[role="tooltip"]')?.textContent).not.toContain(pluginKey.apiKey)
    expect(container.querySelector('[title="external-plugin:auth-provider"]')).toBeNull()
  })

  it('reveals native keys independently and never places a revealed raw key in a tooltip', async () => {
    const second = { ...apiKey, id: 'second', apiKey: 'sk-second654321' }
    await renderCard(vi.fn(), [apiKey, second, pluginKey])
    const toggles = container.querySelectorAll<HTMLButtonElement>('button[aria-label="Show API key"]')
    expect(toggles).toHaveLength(2)
    await act(async () => toggles[0].click())
    expect(container.textContent).toContain(apiKey.apiKey)
    expect(container.textContent).not.toContain(second.apiKey)
    expect(container.textContent).not.toContain(pluginKey.apiKey)
    const raw = Array.from(container.querySelectorAll('span')).find((el) => el.textContent === apiKey.apiKey)!
    await act(async () => raw.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
    expect(document.querySelector('[role="tooltip"]')?.textContent ?? '').not.toContain(apiKey.apiKey)
    await act(async () => container.querySelector<HTMLButtonElement>('button[aria-label="Hide API key"]')!.click())
    expect(container.textContent).not.toContain(apiKey.apiKey)
  })

  it('opens the shared pencil editor with only alias, ignores IME Enter, and saves a trimmed alias on Enter', async () => {
    const save = vi.fn(async () => undefined)
    await renderCard(vi.fn(), [pluginKey], save)
    const { opener, input } = await openPluginEditor()
    expect(input.maxLength).toBe(128)
    expect(document.querySelectorAll('[role="dialog"] input')).toHaveLength(1)
    expect(save).not.toHaveBeenCalled()
    await changeAlias(input, '  External team  ')
    await act(async () => {
      input.focus()
      input.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }))
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, isComposing: true }))
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, isComposing: true }))
    })
    expect(save).not.toHaveBeenCalled()
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    await act(async () => {
      input.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true }))
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    })
    expect(save).toHaveBeenCalledExactlyOnceWith('7', 'External team')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.activeElement).toBe(opener)
  })

  it('restores focus to the edited card after an alias save is pending and succeeds', async () => {
    const pending = deferred<CpaApiKeySettingsItem>()
    const save = vi.fn(() => pending.promise)
    await renderSavingHarness([pluginKey, apiKey], save)
    const opener = container.querySelectorAll<HTMLButtonElement>('button[aria-label="Edit credential"]')[1]
    await act(async () => { opener.focus(); opener.click() })
    const input = document.querySelector<HTMLInputElement>('[role="dialog"] input')!
    await changeAlias(input, 'Renamed native key')
    await act(async () => input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })))

    expect(save).toHaveBeenCalledExactlyOnceWith(apiKey.id, 'Renamed native key')
    expect(input.disabled).toBe(true)
    const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!
    const saveButton = Array.from(dialog.querySelectorAll('button')).find((element) => element.textContent === 'Save')!
    const cancelButton = Array.from(dialog.querySelectorAll('button')).find((element) => element.textContent === 'Cancel')!
    const closeButton = dialog.querySelector<HTMLButtonElement>('button[aria-label="Close"]')!
    expect(saveButton.disabled).toBe(true)
    expect(cancelButton.disabled).toBe(true)
    expect(closeButton.disabled).toBe(true)
    await act(async () => {
      saveButton.click()
      cancelButton.click()
      closeButton.click()
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    })
    expect(document.querySelector('[role="dialog"]')).toBe(dialog)
    expect(save).toHaveBeenCalledTimes(1)
    const openerStayedConnected = opener.isConnected

    await act(async () => pending.resolve({ ...apiKey, keyAlias: 'Renamed native key', label: 'Renamed native key' }))

    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(container.textContent).toContain('Renamed native key')
    expect(container.textContent).toContain('Third-party Identity #7')
    const editedCardButton = container.querySelectorAll<HTMLButtonElement>('button[aria-label="Edit credential"]')[1]
    expect(document.activeElement).toBe(editedCardButton)
    expect(editedCardButton).toBe(opener)
    expect(openerStayedConnected).toBe(true)
  })

  it('retains a failed pending alias draft and restores focus when Escape cancels it', async () => {
    const pending = deferred<CpaApiKeySettingsItem>()
    const save = vi.fn(() => pending.promise)
    await renderSavingHarness([pluginKey], save)
    const { opener, input } = await openPluginEditor()
    await changeAlias(input, 'Draft alias')
    await act(async () => input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })))
    expect(save).toHaveBeenCalledExactlyOnceWith(pluginKey.id, 'Draft alias')
    expect(input.disabled).toBe(true)
    expect(document.querySelector('[role="alert"]')).toBeNull()
    const openerStayedConnected = opener.isConnected

    await act(async () => {
      pending.reject(new Error('save failed'))
      await pending.promise.catch(() => undefined)
    })

    expect(document.querySelector('[role="alert"]')?.textContent).toBe('Unable to save alias.')
    expect(input.disabled).toBe(false)
    expect(input.value).toBe('Draft alias')
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    await act(async () => { input.focus(); input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })) })
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(container.textContent).not.toContain('Draft alias')
    const currentButton = button('Edit credential')
    expect(document.activeElement).toBe(currentButton)
    expect(currentButton).toBe(opener)
    expect(openerStayedConnected).toBe(true)
  })

  it('does not open the alias editor while that card is saving', async () => {
    const save = vi.fn()
    const renderWithSavingId = async (savingId: string | null) => {
      await act(async () => root.render(<ApiKeySettingsCard apiKeys={[apiKey]} savingId={savingId} onSaveAlias={save} />))
    }
    await renderWithSavingId(apiKey.id)
    const opener = button('Edit credential')
    await click(opener)
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(save).not.toHaveBeenCalled()

    await renderWithSavingId(null)
    expect(button('Edit credential')).toBe(opener)
    await click(opener)
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
  })
})
