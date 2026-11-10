// @vitest-environment happy-dom
import { act, createRef, forwardRef } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import { ApiError } from '@/lib/api'
import { ModelCatalog as ModelCatalogView, type ModelCatalogHandle } from '../ModelCatalog'
import { useModelCatalogData } from '../useModelCatalogData'
import { loadModelProviderFilter, modelProvider, MODEL_CATALOG_PREFERENCES_KEY, normalizeCatalog } from '../modelCatalogUtils'

const mocks = vi.hoisted(() => ({ fetch: vi.fn(), copy: vi.fn() }))
vi.mock('@/lib/api', async (original) => ({ ...await original<typeof import('@/lib/api')>(), fetchAvailableModels: mocks.fetch }))
vi.mock('@/utils/clipboard', () => ({ copyTextToClipboard: mocks.copy }))

const ModelCatalog = forwardRef<ModelCatalogHandle, { onAuthRequired?: () => void }>(function ModelCatalogFixture({ onAuthRequired }, ref) {
  const catalog = useModelCatalogData({ onAuthRequired })
  return <ModelCatalogView ref={ref} catalog={catalog} />
})

describe('model catalog', () => {
  let container: HTMLDivElement
  let root: Root
  const ref = createRef<ModelCatalogHandle>()
  beforeEach(async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    await i18n.changeLanguage('en')
    localStorage.clear()
    mocks.copy.mockReset().mockResolvedValue(undefined)
    mocks.fetch.mockReset().mockResolvedValue({ data: [
      { id: 'claude-model', owned_by: 'openai' }, { id: 'gpt-model', owned_by: 'anthropic' }, { id: 'alias', owned_by: 'custom' },
    ] })
    container = document.createElement('div'); document.body.appendChild(container); root = createRoot(container)
  })
  afterEach(async () => { await act(async () => root.unmount()); container.remove(); vi.restoreAllMocks() })
  const button = (label: string) => [...container.querySelectorAll<HTMLButtonElement>('button')].find((item) => item.textContent?.includes(label))!
  const ids = () => [...container.querySelectorAll('h3')].map((node) => node.textContent)

  it('defaults to All, filters by model prefix, copies the exact ID and remembers only its own filter', async () => {
    await act(async () => root.render(<ModelCatalog ref={ref} />))
    expect(button('All').getAttribute('aria-pressed')).toBe('true')
    expect(ids()).toEqual(['alias', 'claude-model', 'gpt-model'])
    await act(async () => button('Claude').click())
    expect(ids()).toEqual(['claude-model'])
    expect(loadModelProviderFilter()).toBe('provider:claude')
    expect(localStorage.getItem('cpa-usage-keeper-credentials-list-preferences-v1')).toBeNull()
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="Copy claude-model"]')!.click())
    expect(mocks.copy).toHaveBeenCalledWith('claude-model')
    expect(container.textContent).toContain('Copied claude-model')
    await act(async () => root.unmount())
    root = createRoot(container)
    await act(async () => root.render(<ModelCatalog />))
    expect(ids()).toEqual(['claude-model'])
  })

  it('clears an empty search result and restores All', async () => {
    await act(async () => root.render(<ModelCatalog />))
    await act(async () => button('Claude').click())
    const input = container.querySelector('input')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, 'missing')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    expect(container.textContent).toContain('No matching models')
    const clear = button('Clear filters')
    clear.focus()
    expect(document.activeElement).toBe(clear)
    await act(async () => clear.click())
    expect(document.activeElement).toBe(input)
    expect(input.value).toBe('')
    expect(ids()).toHaveLength(3)
    expect(button('All').getAttribute('aria-pressed')).toBe('true')
  })

  it('does not steal focus on input, refresh or language change', async () => {
    await act(async () => root.render(<ModelCatalog ref={ref} />))
    const selected = button('Claude')
    selected.focus()
    const input = container.querySelector('input')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, 'claude')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    expect(document.activeElement).toBe(selected)
    await act(async () => ref.current!.refresh())
    expect(document.activeElement).toBe(selected)
    await act(async () => i18n.changeLanguage('zh'))
    expect(document.activeElement).toBe(selected)
  })

  it('counts models without a brand icon only in All and preserves their full IDs', async () => {
    mocks.fetch.mockResolvedValue({ data: [
      { id: 'team/no-owner-alias' },
      { id: 'team/blank-alias', owned_by: '   ' },
      { id: 'team/custom-model', owned_by: 'custom-co' },
      { id: 'team/unmapped-alias', owned_by: 'PrivateVendor' },
      { id: 'team/vendor-alias', owned_by: 'mistral' },
      { id: 'team/no-icon-alias', owned_by: 'MiniMax' },
      { id: 'route/mistral-small', owned_by: 'openai' },
      { id: 'route/MiniMax-M2.5', owned_by: 'openai' },
      { id: 'route/gpt-6-astra' },
      { id: 'route/claude-model', owned_by: 'openai' },
      { id: 'route/fast-alias', owned_by: 'google' },
    ] })
    await act(async () => root.render(<ModelCatalog />))
    const filters = [...container.querySelectorAll<HTMLButtonElement>('[role="toolbar"] button')]
    expect(filters.map((item) => item.textContent)).toEqual(['All11', 'Claude1', 'Gemini1', 'OpenAI1'])
    expect(ids()).toHaveLength(11)
    const articles = [...container.querySelectorAll('article')]
    const noOwner = articles.find((item) => item.querySelector('h3')?.textContent === 'team/no-owner-alias')!
    expect(noOwner.textContent).toBe('?team/no-owner-alias')
    expect(noOwner.querySelector('h3')?.getAttribute('title')).toBe('team/no-owner-alias')
    expect(noOwner.querySelector('h3')?.parentElement?.children).toHaveLength(2)
    expect(articles.find((item) => item.querySelector('h3')?.textContent === 'team/blank-alias')?.textContent).toBe('?team/blank-alias')
    expect(container.textContent).not.toContain('Unspecified')
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="Copy team/no-owner-alias"]')!.click())
    expect(mocks.copy).toHaveBeenCalledWith('team/no-owner-alias')
    await act(async () => button('OpenAI').click())
    expect(ids()).toEqual(['route/gpt-6-astra'])
  })

  it('searches full IDs and known brands, but not unknown owners or conflicting metadata', async () => {
    mocks.fetch.mockResolvedValue({ data: [
      { id: 'route/claude-model', owned_by: 'openai' },
      { id: 'route/gpt-model', owned_by: 'anthropic' },
      { id: 'team/custom-model', owned_by: 'PrivateVendor' },
      { id: 'team/vendor-alias', owned_by: 'mistral' },
      { id: 'route/mistral-small', owned_by: 'openai' },
      { id: 'team/no-icon-alias', owned_by: 'MiniMax' },
      { id: 'route/MiniMax-M2.5', owned_by: 'openai' },
    ] })
    await act(async () => root.render(<ModelCatalog />))
    const input = container.querySelector('input')!
    for (const [query, expected] of [
      ['openai', ['route/gpt-model']],
      ['claude', ['route/claude-model']],
      ['privatevendor', []],
      ['mistral', ['route/mistral-small']],
      ['minimax', ['route/MiniMax-M2.5']],
      ['team/custom-model', ['team/custom-model']],
    ] as const) {
      await act(async () => {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, query)
        input.dispatchEvent(new Event('input', { bubbles: true }))
      })
      expect(ids()).toEqual(expected)
    }
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="Copy team/custom-model"]')!.click())
    expect(mocks.copy).toHaveBeenCalledWith('team/custom-model')
  })

  it('retains the last catalog and saved filter on refresh failure', async () => {
    await act(async () => root.render(<ModelCatalog ref={ref} />))
    await act(async () => button('Claude').click())
    mocks.fetch.mockRejectedValueOnce(new Error('upstream secret'))
    await act(async () => ref.current!.refresh())
    expect(ids()).toEqual(['claude-model'])
    expect(container.textContent).toContain('Showing the last loaded catalog')
    expect(container.textContent).not.toContain('secret')
    expect(loadModelProviderFilter()).toBe('provider:claude')
  })

  it('handles auth expiry and a first-load error independently of pricing', async () => {
    const onAuthRequired = vi.fn()
    mocks.fetch.mockRejectedValueOnce(new ApiError('unauthenticated', 401))
    await act(async () => root.render(<ModelCatalog onAuthRequired={onAuthRequired} />))
    expect(onAuthRequired).toHaveBeenCalledOnce()
    expect(container.textContent).toContain('Could not load the CPA model catalog')
    await act(async () => button('Retry').click())
    expect(ids()).toHaveLength(3)
  })

  it('keeps original two-line cards in one scrollable list without descriptive chrome', async () => {
    mocks.fetch.mockResolvedValue({ data: Array.from({ length: 25 }, (_, i) => ({ id: `model-${String(i).padStart(2, '0')}`, owned_by: i === 24 ? 'custom' : 'openai' })) })
    await act(async () => root.render(<ModelCatalog />))
    expect(container.querySelector('h2')?.textContent).toBe('Models')
    expect(ids()).toHaveLength(25)
    expect(button('Next')).toBeUndefined()
    expect(button('Previous')).toBeUndefined()
    const viewport = container.querySelector<HTMLElement>('[role="region"][aria-label="Model list"]')!
    expect(viewport.tabIndex).toBe(0)
    const row = container.querySelector('article')!
    expect(row.textContent).toBe('OpenAImodel-00')
    expect(row.querySelector('h3')?.textContent).toBe('model-00')
    expect(row.querySelectorAll('button')).toHaveLength(1)
    expect(container.textContent).not.toContain('Explore the models')
    expect(container.textContent).not.toContain('/v1/models')
    expect(container.textContent).not.toContain('Listed by CPA')
    expect(container.textContent).not.toContain('25 of 25 models')
    viewport.scrollTop = 200
    await act(async () => button('OpenAI').click())
    expect(ids()).toHaveLength(24)
    expect(ids()[0]).toBe('model-00')
    expect(viewport.scrollTop).toBe(0)
  })

  it.each([
    ['deepseek', 'deepseek', 'xxx/deepseek-chat'],
    ['qwen', 'qwen', 'xxx/qwen3-coder'],
    ['z.ai', 'zai', 'xxx/glm-5'],
    ['glm', 'zai', 'xxx/glm-5'],
    ['muse', 'muse', 'xxx/muse-spark'],
  ])('restores the saved owner:%s filter after its brand icon is added', async (owner, icon, id) => {
    localStorage.setItem(MODEL_CATALOG_PREFERENCES_KEY, JSON.stringify({ version: 1, providerFilter: `owner:${owner}` }))
    mocks.fetch.mockResolvedValue({ data: [{ id, owned_by: 'openai' }] })
    await act(async () => root.render(<ModelCatalog />))
    expect(loadModelProviderFilter()).toBe(`provider:${icon}`)
    expect(ids()).toEqual([id])
    expect(container.querySelector(`button[aria-pressed="true"] [data-provider-brand-icon="${icon}"]`)).not.toBeNull()
    expect(container.querySelector(`article [data-provider-brand-icon="${icon}"] img`)).not.toBeNull()
    await act(async () => container.querySelector<HTMLButtonElement>(`[aria-label="Copy ${id}"]`)!.click())
    expect(mocks.copy).toHaveBeenCalledWith(id)
  })

  it.each([
    'unknown', 'owner:custom', 'owner:mistral', 'owner:minimax', 'owner:   ',
    'provider:custom', 'provider:mistral', 'provider:minimax', 'provider:constructor',
  ])('restores the obsolete %s filter as All without a ghost option', async (savedFilter) => {
    localStorage.setItem(MODEL_CATALOG_PREFERENCES_KEY, JSON.stringify({ version: 1, providerFilter: savedFilter }))
    await act(async () => root.render(<ModelCatalog />))
    expect(loadModelProviderFilter()).toBe('all')
    expect([...container.querySelectorAll<HTMLButtonElement>('[role="toolbar"] button')].map((item) => item.textContent))
      .toEqual(['All3', 'Claude1', 'OpenAI1'])
    expect(button('All').getAttribute('aria-pressed')).toBe('true')
    expect(ids()).toEqual(['alias', 'claude-model', 'gpt-model'])
  })

  it('keeps a selected known brand with its icon and zero count when the catalog has no matches', async () => {
    localStorage.setItem(MODEL_CATALOG_PREFERENCES_KEY, JSON.stringify({ version: 1, providerFilter: 'owner:deepseek' }))
    await act(async () => root.render(<ModelCatalog />))
    const selected = button('DeepSeek')
    expect(selected.textContent).toBe('DeepSeek0')
    expect(selected.getAttribute('aria-pressed')).toBe('true')
    expect(selected.querySelector('[data-provider-brand-icon="deepseek"]')).not.toBeNull()
    expect(ids()).toEqual([])
    await act(async () => button('All').click())
    expect(ids()).toHaveLength(3)
  })

  it('retains a selected brand and its zero count when a refresh removes its models', async () => {
    await act(async () => root.render(<ModelCatalog ref={ref} />))
    await act(async () => button('Claude').click())
    mocks.fetch.mockResolvedValueOnce({ data: [{ id: 'team/gpt-6', owned_by: 'custom' }] })
    await act(async () => ref.current!.refresh())
    expect(ids()).toEqual([])
    expect(button('Claude').textContent).toBe('Claude0')
    expect(button('Claude').getAttribute('aria-pressed')).toBe('true')
    expect(button('Claude').querySelector('[data-provider-brand-icon="claude"]')).not.toBeNull()
    expect(loadModelProviderFilter()).toBe('provider:claude')
    await act(async () => button('All').click())
    expect(ids()).toEqual(['team/gpt-6'])
  })

  it('validates local storage and falls back to owned_by for unknown model names', () => {
    localStorage.setItem(MODEL_CATALOG_PREFERENCES_KEY, '{bad')
    expect(loadModelProviderFilter()).toBe('all')
    localStorage.setItem(MODEL_CATALOG_PREFERENCES_KEY, JSON.stringify({ version: 99, providerFilter: 'provider:claude' }))
    expect(loadModelProviderFilter()).toBe('all')
    expect(modelProvider('custom-model', 'google').icon).toBe('gemini')
    expect(normalizeCatalog([{ id: 'custom-alias', owned_by: 'custom' }, { id: 'custom-alias' }, { id: '' }])).toEqual([
      { id: 'custom-alias', owned_by: 'custom', provider: { key: 'owner:custom', label: 'custom' } },
    ])
  })
})
