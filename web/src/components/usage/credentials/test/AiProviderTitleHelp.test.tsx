// @vitest-environment happy-dom
import { act, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import { AiProviderCredentialsSection } from '../AiProviderCredentialsSection'
import type { CredentialViewMode } from '../credentialListPreferences'
import { createAiProviderSectionProps } from './credentialSectionFixtures'

const copy = {
  en: {
    label: 'About AI provider views',
    cards: 'Click a name for details or the edit button for settings; priority badges offer quick adjustments.',
    list: 'Click a name for details or the edit button for settings; icons toggle status and priority badges offer quick adjustments.',
    note: 'OpenAI-compatible credentials cannot be enabled or disabled here.',
  },
  zh: {
    label: 'AI 供应商视图说明',
    cards: '点击名称查看详情，编辑按钮修改设置；优先级标签快速调整。',
    list: '点击名称查看详情，编辑按钮修改设置；图标切换启停，优先级标签快速调整。',
    note: 'OpenAI 兼容项不支持在此启停。',
  },
  'zh-TW': {
    label: 'AI 供應商檢視說明',
    cards: '點擊名稱查看詳情，編輯按鈕修改設定；優先級標籤快速調整。',
    list: '點擊名稱查看詳情，編輯按鈕修改設定；圖示切換啟用狀態，優先級標籤快速調整。',
    note: 'OpenAI 相容項不支援在此啟用或停用。',
  },
} as const

globalThis.IS_REACT_ACT_ENVIRONMENT = true

function Section() {
  const [viewMode, setViewMode] = useState<CredentialViewMode>('cards')
  return <AiProviderCredentialsSection {...createAiProviderSectionProps({ viewMode, onViewModeChange: setViewMode })} />
}

describe('AI provider title help', () => {
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
    await i18n.changeLanguage('en')
  })

  it.each(['en', 'zh', 'zh-TW'] as const)('shows the selected mode and a separate concise note in %s', async (language) => {
    await i18n.changeLanguage(language)
    await act(async () => root.render(<Section />))
    const title = container.querySelector('h3')!
    const help = container.querySelector<HTMLButtonElement>(`button[aria-label="${copy[language].label}"]`)!
    expect(help).not.toBeNull()
    const filter = container.querySelector<HTMLInputElement>('input[type="checkbox"]')!
    const view = container.querySelector('[role="group"]')!
    expect(title.parentElement).toBe(help.parentElement?.parentElement)
    expect(title.compareDocumentPosition(help) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(help.compareDocumentPosition(filter) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(filter.compareDocumentPosition(view) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(help.closest('label')).toBeNull()
    const description = document.getElementById(help.getAttribute('aria-describedby')!)!
    const tooltip = document.getElementById(help.getAttribute('aria-controls')!)!
    const checkCopy = (mode: CredentialViewMode) => {
      expect(description.textContent).toBe(`${copy[language][mode]} ${copy[language].note}`)
      expect([...tooltip.children].map((child) => child.textContent)).toEqual([copy[language][mode], copy[language].note])
    }
    checkCopy('cards')
    await act(async () => help.focus())
    expect(tooltip.getAttribute('aria-hidden')).toBe('false')
    checkCopy('cards')
    await act(async () => (view.querySelectorAll('button')[1] as HTMLButtonElement).click())
    expect(tooltip.getAttribute('aria-hidden')).toBe('false')
    checkCopy('list')
  })

  it('updates open help on language change and picks up a mode changed while closed', async () => {
    await i18n.changeLanguage('en')
    await act(async () => root.render(<Section />))
    const help = container.querySelector<HTMLButtonElement>('[data-question-mark-help]')!
    await act(async () => help.focus())
    const description = document.getElementById(help.getAttribute('aria-describedby')!)!
    const tooltip = document.getElementById(help.getAttribute('aria-controls')!)!
    await act(async () => { await i18n.changeLanguage('zh-TW') })
    expect(help.getAttribute('aria-label')).toBe(copy['zh-TW'].label)
    expect(description.textContent).toBe(`${copy['zh-TW'].cards} ${copy['zh-TW'].note}`)
    expect([...tooltip.children].map((child) => child.textContent)).toEqual([copy['zh-TW'].cards, copy['zh-TW'].note])
    await act(async () => help.blur())
    expect(tooltip.getAttribute('aria-hidden')).toBe('true')
    const view = container.querySelector('[role="group"]')!
    await act(async () => (view.querySelectorAll('button')[1] as HTMLButtonElement).click())
    await act(async () => help.focus())
    expect(description.textContent).toBe(`${copy['zh-TW'].list} ${copy['zh-TW'].note}`)
    expect([...tooltip.children].map((child) => child.textContent)).toEqual([copy['zh-TW'].list, copy['zh-TW'].note])
  })
})
