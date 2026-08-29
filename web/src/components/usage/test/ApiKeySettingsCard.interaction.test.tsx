// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { CpaApiKeySettingsItem } from '@/lib/types'
import { ApiKeySettingsCard } from '../ApiKeySettingsCard'

globalThis.IS_REACT_ACT_ENVIRONMENT = true

const translations: Record<string, string> = {
  'common.save': 'Save',
  'usage_stats.api_key_settings_title': 'API Key Settings',
  'usage_stats.api_key_settings_subtitle': 'Set display aliases.',
  'usage_stats.api_key_settings_display_key': 'API Key',
  'usage_stats.api_key_settings_alias': 'Alias',
  'usage_stats.api_key_settings_show_full': 'Show full API keys',
  'usage_stats.api_key_settings_copy': 'Copy',
  'usage_stats.api_key_settings_copied': 'Copied',
  'usage_stats.api_key_settings_copy_success': 'API Key copied.',
  'usage_stats.api_key_settings_copy_failed': 'Unable to copy API Key.',
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => translations[key] ?? key,
  }),
}))

const apiKey: CpaApiKeySettingsItem = {
  id: '9007199254740993',
  apiKey: 'sk-alpha123456',
  keyAlias: 'Primary',
  displayKey: 'sk-*********123456',
  label: 'Primary',
  lastSyncedAt: '2026-05-13T00:00:00Z',
}

describe('ApiKeySettingsCard copy action', () => {
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

  const renderCard = async (onNotice: ReturnType<typeof vi.fn>) => {
    await act(async () => {
      root.render(
        <ApiKeySettingsCard
          apiKeys={[apiKey]}
          onSaveAlias={() => undefined}
          onNotice={onNotice}
        />,
      )
    })
  }

  it('copies the raw key, shows the success icon, and resets its label', async () => {
    vi.useFakeTimers()
    const writeText = vi.fn(async () => undefined)
    const onNotice = vi.fn()
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    })
    await renderCard(onNotice)

    const copyButton = container.querySelector<HTMLButtonElement>('button[aria-label="Copy"]')
    expect(copyButton).not.toBeNull()
    expect(container.textContent).toContain(apiKey.displayKey)
    expect(container.textContent).not.toContain(apiKey.apiKey)
    const copyIcon = copyButton?.querySelector('svg')?.innerHTML

    await act(async () => {
      copyButton?.click()
      await Promise.resolve()
    })

    expect(writeText).toHaveBeenCalledWith(apiKey.apiKey)
    expect(onNotice).toHaveBeenCalledWith('success', 'API Key copied.')
    const copiedButton = container.querySelector<HTMLButtonElement>('button[aria-label="Copied"]')
    expect(copiedButton).not.toBeNull()
    expect(copiedButton?.querySelector('svg')?.innerHTML).not.toBe(copyIcon)

    await act(async () => vi.advanceTimersByTime(1600))
    expect(container.querySelector('button[aria-label="Copy"]')).not.toBeNull()
    expect(container.querySelector('button[aria-label="Copied"]')).toBeNull()
  })

  it('keeps the copy icon and reports an error when both copy paths fail', async () => {
    const writeText = vi.fn(async () => { throw new Error('blocked') })
    const execCommand = vi.fn(() => false)
    const onNotice = vi.fn()
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    })
    Object.defineProperty(document, 'execCommand', {
      configurable: true,
      value: execCommand,
    })
    await renderCard(onNotice)

    const copyButton = container.querySelector<HTMLButtonElement>('button[aria-label="Copy"]')
    expect(copyButton).not.toBeNull()

    await act(async () => {
      copyButton?.click()
      await Promise.resolve()
    })

    expect(writeText).toHaveBeenCalledWith(apiKey.apiKey)
    expect(execCommand).toHaveBeenCalledWith('copy')
    expect(onNotice).toHaveBeenCalledWith('error', 'Unable to copy API Key.')
    expect(container.querySelector('button[aria-label="Copy"]')).not.toBeNull()
    expect(container.querySelector('button[aria-label="Copied"]')).toBeNull()
  })
})
