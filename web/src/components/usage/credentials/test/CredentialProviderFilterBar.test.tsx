import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { CredentialProviderFilterBar } from '../CredentialProviderFilterBar'

// i18n mock 直接返回 key，让测试精确观察筛选标签选择。
vi.mock('react-i18next', () => ({
  // initReactI18next 满足组件加载时的插件接口。
  initReactI18next: { type: '3rdParty', init: () => undefined },
  // useTranslation 返回稳定的 key passthrough。
  useTranslation: () => ({
    // t 不引入任何语言环境差异。
    t: (key: string) => key,
  }),
}))

// 当前测试组验证新原始 type 复用既有品牌标签与图标。
describe('CredentialProviderFilterBar', () => {
  // Gemini CLI 与 Interactions 单独存在时也应聚合显示 Gemini 品牌按钮。
  it('renders Gemini branding for Gemini CLI and Interactions provider rows', () => {
    // html 使用真实组件和仅包含两个兼容 type 的后端计数。
    const html = renderToStaticMarkup(
      // AI Provider scope 应把 Gemini CLI 与 Interactions 聚合到 Gemini。
      <CredentialProviderFilterBar scope="ai-provider" typeCounts={[{ type: 'gemini-cli', count: 2 }, { type: 'gemini-interactions', count: 3 }]} value="all" onChange={() => undefined} />,
    )
    // Gemini 品牌标签必须可见。
    expect(html).toContain('usage_stats.credentials_filter_gemini')
    // 两个兼容 type 复用统一的 Gemini 图标。
    expect(html).toContain('data-provider-brand-icon="gemini"')
    // 聚合后的按钮展示两个兼容 type 的总计数。
    expect(html).toContain('>5</span>')
  })

  // xAI API Key 需要在 AI Provider scope 显示统一的 Grok Avatar。
  it('renders the xAI label and Grok Avatar for xAI provider rows', () => {
    // html 使用仅包含 xAI API Key 的 AI Provider 计数。
    const html = renderToStaticMarkup(
      // xAI provider 使用独立筛选按钮。
      <CredentialProviderFilterBar scope="ai-provider" typeCounts={[{ type: 'xai', count: 2 }]} value="all" onChange={() => undefined} />,
    )
    // 复用现有 xAI 翻译 key。
    expect(html).toContain('usage_stats.credentials_filter_xai')
    // 使用统一组件中的 Lobe Icons Grok Avatar。
    expect(html).toContain('data-provider-brand-icon="xai"')
    expect(html).toContain('data-provider-brand-icon-tone="avatar"')
    // 按钮展示两行计数。
    expect(html).toContain('>2</span>')
  })

  // Auth Files xAI 行为必须保持不变。
  it('keeps the xAI Auth Files filter available', () => {
    // html 使用 Auth Files scope 的 xAI OAuth 行。
    const html = renderToStaticMarkup(
      // Auth Files 仍使用相同 xai 原始 type。
      <CredentialProviderFilterBar scope="auth-files" typeCounts={[{ type: 'xai', count: 1 }]} value="all" onChange={() => undefined} />,
    )
    // xAI Auth Files 标签继续显示。
    expect(html).toContain('usage_stats.credentials_filter_xai')
    // xAI Auth Files 与 AI Provider 复用同一个图标组件。
    expect(html).toContain('data-provider-brand-icon="xai"')
  })

  // Auth Files 显示 Kimi、Vertex 与 Gemini CLI 兼容品牌，同时不暴露 iFlow 按钮。
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

  // 没有任何正计数时组件继续返回空 markup。
  it('hides the whole filter bar when no credentials are loaded', () => {
    // html 使用空后端计数。
    const html = renderToStaticMarkup(
      // 空数据不应渲染 toolbar。
      <CredentialProviderFilterBar scope="ai-provider" typeCounts={[]} value="all" onChange={() => undefined} />,
    )
    // 空数据保持原有隐藏行为。
    expect(html).toBe('')
  })

})
