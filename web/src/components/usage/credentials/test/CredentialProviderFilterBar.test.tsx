import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { CredentialProviderFilterBar } from '../CredentialProviderFilterBar'

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({
    t: (key: string) => key,
  }),
}))

describe('CredentialProviderFilterBar', () => {
  it('renders Gemini branding for Gemini CLI and Interactions provider rows', () => {
    const html = renderToStaticMarkup(
      <CredentialProviderFilterBar scope="ai-provider" typeCounts={[{ type: 'gemini-cli', count: 2 }, { type: 'gemini-interactions', count: 3 }]} value="all" onChange={() => undefined} />,
    )
    expect(html).toContain('usage_stats.credentials_filter_gemini')
    expect(html).toContain('data-provider-brand-icon="gemini"')
    expect(html).toContain('>5</span>')
  })

  it('renders xAI branding and count for provider rows', () => {
    const html = renderToStaticMarkup(
      <CredentialProviderFilterBar scope="ai-provider" typeCounts={[{ type: 'xai', count: 2 }]} value="all" onChange={() => undefined} />,
    )
    expect(html).toContain('usage_stats.credentials_filter_xai')
    expect(html).toContain('data-provider-brand-icon="xai"')
    expect(html).toContain('>2</span>')
  })

  it('keeps the xAI Auth Files filter available', () => {
    const html = renderToStaticMarkup(
      <CredentialProviderFilterBar scope="auth-files" typeCounts={[{ type: 'xai', count: 1 }]} value="all" onChange={() => undefined} />,
    )
    expect(html).toContain('usage_stats.credentials_filter_xai')
    expect(html).toContain('data-provider-brand-icon="xai"')
  })

  it('renders Devin for Auth Files and Meta for AI Providers', () => {
    const authFilesHtml = renderToStaticMarkup(
      <CredentialProviderFilterBar scope="auth-files" typeCounts={[{ type: 'devin', count: 2 }]} value="all" onChange={() => undefined} />,
    )
    const providersHtml = renderToStaticMarkup(
      <CredentialProviderFilterBar scope="ai-provider" typeCounts={[{ type: 'meta', count: 3 }]} value="all" onChange={() => undefined} />,
    )

    expect(authFilesHtml).toContain('usage_stats.credentials_filter_devin')
    expect(authFilesHtml).toContain('data-provider-brand-icon="devin"')
    expect(providersHtml).toContain('usage_stats.credentials_filter_meta')
    expect(providersHtml).toContain('data-provider-brand-icon="meta"')
  })

  it('renders Kimi, Vertex, and Gemini CLI branding without iFlow', () => {
    const html = renderToStaticMarkup(
      <CredentialProviderFilterBar
        scope="auth-files"
        typeCounts={[
          { type: 'kimi', count: 2 },
          { type: 'vertex', count: 3 },
          { type: 'gemini-cli', count: 4 },
          { type: 'iflow', count: 5 },
        ]}
        value="all"
        onChange={() => undefined}
      />,
    )

    expect(html).toContain('usage_stats.credentials_filter_kimi')
    expect(html).toContain('usage_stats.credentials_filter_vertex')
    expect(html).toContain('usage_stats.credentials_filter_gemini')
    expect(html).toContain('data-provider-brand-icon="kimi"')
    expect(html).toContain('data-provider-brand-icon="vertex"')
    expect(html).toContain('data-provider-brand-icon="gemini"')
    expect(html).not.toContain('usage_stats.credentials_filter_iflow')
  })

  it('hides the whole filter bar when no credentials are loaded', () => {
    const html = renderToStaticMarkup(
      <CredentialProviderFilterBar scope="ai-provider" typeCounts={[]} value="all" onChange={() => undefined} />,
    )
    expect(html).toBe('')
  })
})
