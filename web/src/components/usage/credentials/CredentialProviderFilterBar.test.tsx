import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { CredentialProviderFilterBar } from './CredentialProviderFilterBar'
import type { CredentialProviderRowLike } from './credentialProviderFilters'

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({
    t: (key: string) => key,
  }),
}))

const row = (type: string, provider = type): CredentialProviderRowLike => ({
  identity: {
    name: `${type} credential`,
    displayName: `${type} credential`,
    type,
    provider,
  },
})

describe('CredentialProviderFilterBar', () => {
  it('hides provider buttons with zero current-page credentials', () => {
    const html = renderToStaticMarkup(
      <CredentialProviderFilterBar rows={[row('codex'), row('claude', 'anthropic')]} value="all" onChange={() => undefined} />,
    )

    expect(html).toContain('usage_stats.credentials_filter_all')
    expect(html).toContain('usage_stats.credentials_filter_codex')
    expect(html).toContain('usage_stats.credentials_filter_claude')
    expect(html).not.toContain('usage_stats.credentials_filter_antigravity')
    expect(html).not.toContain('usage_stats.credentials_filter_gemini_cli')
    expect(html).not.toContain('usage_stats.credentials_filter_iflow')
  })

  it('hides the whole filter bar when no credentials are loaded', () => {
    const html = renderToStaticMarkup(
      <CredentialProviderFilterBar rows={[]} value="all" onChange={() => undefined} />,
    )

    expect(html).toBe('')
  })
})
