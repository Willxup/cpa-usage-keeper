import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { ProviderBrandIcon, providerBrandIconKey } from '../ProviderBrandIcon'

describe('ProviderBrandIcon', () => {
  it.each(['antigravity', 'claude', 'codex', 'devin', 'gemini', 'kimi', 'meta', 'openai', 'vertex', 'xai'])(
    'renders the shared avatar for %s', (providerType) => {
      expect(providerBrandIconKey(providerType)).toBe(providerType)
      const html = renderToStaticMarkup(<ProviderBrandIcon providerType={providerType} size={30} />)
      expect(html).toContain(`data-provider-brand-icon="${providerType}"`)
      expect(html).toContain('data-provider-brand-icon-tone="avatar"')
      expect(html.match(/<img/g)).toHaveLength(1)
    },
  )

  it('normalizes supported Gemini aliases', () => {
    expect(providerBrandIconKey('gemini-cli')).toBe('gemini')
    expect(providerBrandIconKey('gemini-interactions')).toBe('gemini')
  })

  it.each(['kimi', 'kimi-ai', 'kimi.ai', 'kimi.com'])('renders %s with the shared Kimi brand', (providerType) => {
    expect(providerBrandIconKey(` ${providerType.toUpperCase()} `)).toBe('kimi')
    const html = renderToStaticMarkup(<ProviderBrandIcon providerType={providerType} size={30} ariaLabel={providerType} />)
    expect(html).toContain('data-provider-brand-icon="kimi"')
    expect(html).toContain(`aria-label="${providerType}"`)
  })

  it('does not assign logos to plugin-only or unsupported identity types', () => {
    expect(providerBrandIconKey('gemini-cli-code-assist')).toBeUndefined()
    expect(providerBrandIconKey('iflow')).toBeUndefined()
    expect(providerBrandIconKey('future-provider')).toBeUndefined()
    expect(providerBrandIconKey('kimi-future')).toBeUndefined()
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

  it('renders nothing for a type outside the unified set', () => {
    expect(renderToStaticMarkup(<ProviderBrandIcon providerType="iflow" size={30} />)).toBe('')
  })
})
