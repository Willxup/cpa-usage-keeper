// @vitest-environment happy-dom
import { act, createRef } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import { ApiError } from '@/lib/api'
import { ModelCatalog, type ModelCatalogHandle } from '../ModelCatalog'
import { loadModelProviderFilter, modelProvider, MODEL_CATALOG_PREFERENCES_KEY, normalizeCatalog } from '../modelCatalog'

const mocks = vi.hoisted(() => ({ fetch: vi.fn(), copy: vi.fn() }))
vi.mock('@/lib/api', async (original) => ({ ...await original<typeof import('@/lib/api')>(), fetchAvailableModels: mocks.fetch }))
vi.mock('@/utils/clipboard', () => ({ copyTextToClipboard: mocks.copy }))

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
      { id: 'claude-model', owned_by: 'anthropic' }, { id: 'gpt-model', owned_by: 'openai' }, { id: 'alias', owned_by: 'custom' },
    ] })
    container = document.createElement('div'); document.body.appendChild(container); root = createRoot(container)
  })
  afterEach(async () => { await act(async () => root.unmount()); container.remove(); vi.restoreAllMocks() })
  const button = (label: string) => [...container.querySelectorAll<HTMLButtonElement>('button')].find((item) => item.textContent?.includes(label))!
  const ids = () => [...container.querySelectorAll('h3')].map((node) => node.textContent)

  it('defaults to All, filters by real owner, copies the exact ID and remembers only its own filter', async () => {
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

  it('supports unknown owners, empty matches and reset', async () => {
    await act(async () => root.render(<ModelCatalog />))
    await act(async () => button('custom').click())
    expect(ids()).toEqual(['alias'])
    const input = container.querySelector('input')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, 'missing')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    expect(container.textContent).toContain('No matching models')
    await act(async () => button('Clear filters').click())
    expect(ids()).toHaveLength(3)
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

  it('validates local storage and never guesses owners from model names', () => {
    localStorage.setItem(MODEL_CATALOG_PREFERENCES_KEY, '{bad')
    expect(loadModelProviderFilter()).toBe('all')
    localStorage.setItem(MODEL_CATALOG_PREFERENCES_KEY, JSON.stringify({ version: 99, providerFilter: 'provider:claude' }))
    expect(loadModelProviderFilter()).toBe('all')
    expect(modelProvider('google').icon).toBe('gemini')
    expect(normalizeCatalog([{ id: 'gpt-alias', owned_by: 'custom' }, { id: 'gpt-alias' }, { id: '' }])).toEqual([
      { id: 'gpt-alias', owned_by: 'custom', provider: { key: 'owner:custom', label: 'custom' } },
    ])
  })
})
