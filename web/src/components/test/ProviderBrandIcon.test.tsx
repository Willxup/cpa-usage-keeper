import { existsSync, readFileSync } from 'node:fs'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { ProviderBrandIcon, providerBrandIconKey } from '../ProviderBrandIcon'

const providerIconStyles = readFileSync(new URL('../ProviderBrandIcon.module.scss', import.meta.url), 'utf8')

const lobeProviderIconAssets = [
  ['antigravity.svg', 'Antigravity'],
  ['claude.svg', 'Claude'],
  ['codex.svg', 'Codex'],
  ['gemini.svg', 'Gemini'],
  ['kimi.svg', 'Kimi'],
  ['openai.svg', 'OpenAI'],
  ['vertex.svg', 'VertexAI'],
  ['grok.svg', 'Grok'],
] as const

describe('ProviderBrandIcon', () => {
  it('normalizes CPA identity types and supported aliases into the shared brand set', () => {
    expect([
      'antigravity',
      'claude',
      'codex',
      'gemini',
      'kimi',
      'openai',
      'vertex',
      'xai',
    ].map((providerType) => providerBrandIconKey(providerType))).toEqual([
      'antigravity',
      'claude',
      'codex',
      'gemini',
      'kimi',
      'openai',
      'vertex',
      'xai',
    ])
    expect(providerBrandIconKey('gemini-cli')).toBe('gemini')
    expect(providerBrandIconKey('gemini-interactions')).toBe('gemini')
  })

  it('does not assign logos to plugin-only or unsupported identity types', () => {
    expect(providerBrandIconKey('gemini-cli-code-assist')).toBeUndefined()
    expect(providerBrandIconKey('iflow')).toBeUndefined()
    expect(providerBrandIconKey('future-provider')).toBeUndefined()
    expect(providerBrandIconKey(undefined)).toBeUndefined()
  })

  it('renders a decorative icon with an explicit context size by default', () => {
    const html = renderToStaticMarkup(<ProviderBrandIcon providerType="claude" size={30} />)

    expect(html).toContain('data-provider-brand-icon="claude"')
    expect(html).toContain('style="width:30px;height:30px"')
    expect(html).toContain('aria-hidden="true"')
    expect(html).not.toContain('role="img"')
    expect(html).not.toContain('aria-label=')
  })

  it('exposes the provider type when the icon is the only type indicator', () => {
    const html = renderToStaticMarkup(<ProviderBrandIcon providerType="claude" size={30} ariaLabel=" Claude " />)

    expect(html).toContain('role="img"')
    expect(html).toContain('aria-label="Claude"')
    expect(html).not.toContain('aria-hidden=')
  })

  it('accepts a relative size for framed contexts', () => {
    const html = renderToStaticMarkup(<ProviderBrandIcon providerType="claude" size="100%" />)

    expect(html).toContain('style="width:100%;height:100%"')
  })

  it('keeps all eight provider assets on the Lobe Icons 24px SVG canvas', () => {
    for (const [fileName, title] of lobeProviderIconAssets) {
      const assetUrl = new URL(`../../assets/icons/${fileName}`, import.meta.url)
      expect(existsSync(assetUrl), fileName).toBe(true)
      if (!existsSync(assetUrl)) {
        continue
      }
      const source = readFileSync(assetUrl, 'utf8')
      expect(source, fileName).toContain('viewBox="0 0 24 24"')
      expect(source, fileName).toContain(`<title>${title}</title>`)
    }
  })

  it('renders the Lobe Icons Grok asset inside the shared xAI Avatar', () => {
    const html = renderToStaticMarkup(<ProviderBrandIcon providerType="xai" size={30} />)

    expect(html).toContain('data-provider-brand-icon="xai"')
    expect(html).toContain('data-provider-brand-icon-tone="avatar"')
    expect(html).toContain('M9.27%2015.29')
    expect(html.match(/<img/g)).toHaveLength(1)
  })

  it('renders the monochrome Lobe Icons Vertex asset inside the shared Avatar', () => {
    const html = renderToStaticMarkup(<ProviderBrandIcon providerType="vertex" size={30} />)
    const vertexSource = readFileSync(new URL('../../assets/icons/vertex.svg', import.meta.url), 'utf8')

    expect(html).toContain('data-provider-brand-icon-tone="avatar"')
    expect(vertexSource).not.toMatch(/fill="#[0-9A-Fa-f]+"/)
  })

  it('matches the Lobe Icons Avatar treatment for all shared providers', () => {
    for (const providerType of ['antigravity', 'claude', 'codex', 'gemini', 'kimi', 'openai', 'vertex', 'xai']) {
      const html = renderToStaticMarkup(<ProviderBrandIcon providerType={providerType} size={30} />)
      expect(html, providerType).toContain('data-provider-brand-icon-tone="avatar"')
    }

    expect(providerIconStyles).toMatch(/\.providerBrandIconAvatar\s*\{[\s\S]*?border-radius:\s*50%;[\s\S]*?overflow:\s*hidden;/)
    expect(providerIconStyles.match(/--provider-brand-icon-avatar-scale:/g)).toHaveLength(8)
    expect(providerIconStyles).toMatch(/data-provider-brand-icon='kimi'[\s\S]*?--provider-brand-icon-avatar-scale:\s*0\.6;[\s\S]*?background:\s*#000;/)
    expect(providerIconStyles).toMatch(/data-provider-brand-icon='antigravity'[\s\S]*?--provider-brand-icon-avatar-scale:\s*0\.7;[\s\S]*?background:\s*#fff;/)
  })

  it('renders nothing for a type outside the unified set', () => {
    expect(renderToStaticMarkup(<ProviderBrandIcon providerType="iflow" size={30} />)).toBe('')
  })
})
