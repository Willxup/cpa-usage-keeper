import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { CredentialProviderFilterBar, CredentialProviderFilterIcon } from '../CredentialProviderFilterBar'

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
  // Interactions 单独存在时也应显示 Gemini 品牌按钮。
  it('renders Gemini branding for Gemini Interactions provider rows', () => {
    // html 使用真实组件和仅包含 Interactions 的后端计数。
    const html = renderToStaticMarkup(
      // AI Provider scope 应把 Interactions 聚合到 Gemini。
      <CredentialProviderFilterBar scope="ai-provider" typeCounts={[{ type: 'gemini-interactions', count: 3 }]} value="all" onChange={() => undefined} />,
    )
    // Gemini 品牌标签必须可见。
    expect(html).toContain('usage_stats.credentials_filter_gemini')
    // Interactions 复用现有 Gemini 图片，不增加新 icon asset。
    expect(html).toContain('<img')
    // GeminiCLI 是 Auth Files 标签，不能出现在 AI Provider scope。
    expect(html).not.toContain('usage_stats.credentials_filter_gemini_cli')
    // 聚合后的按钮展示 Interactions 三行计数。
    expect(html).toContain('>3</span>')
  })

  // xAI API Key 需要在 AI Provider scope 显示现有 xAI 品牌。
  it('renders the existing xAI label and icon for xAI provider rows', () => {
    // html 使用仅包含 xAI API Key 的 AI Provider 计数。
    const html = renderToStaticMarkup(
      // xAI provider 使用独立筛选按钮。
      <CredentialProviderFilterBar scope="ai-provider" typeCounts={[{ type: 'xai', count: 2 }]} value="all" onChange={() => undefined} />,
    )
    // 复用现有 xAI 翻译 key。
    expect(html).toContain('usage_stats.credentials_filter_xai')
    // 复用现有 xAI SVG 图片。
    expect(html).toContain('<img')
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
    // xAI Auth Files 继续使用现有图标。
    expect(html).toContain('<img')
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

  // 直接 icon helper 也要继续识别 xAI key。
  it('renders the xAI provider icon as an image', () => {
    // html 直接渲染 xAI icon helper。
    const html = renderToStaticMarkup(<CredentialProviderFilterIcon provider="xai" />)
    // xAI key 必须解析为图片。
    expect(html).toContain('<img')
  })
})
