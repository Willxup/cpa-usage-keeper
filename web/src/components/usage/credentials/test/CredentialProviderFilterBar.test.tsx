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

// Provider 筛选使用标准 Ant Design Tabs，仍复用既有 provider 聚合规则。
describe('CredentialProviderFilterBar', () => {
  // Interactions 单独存在时也应显示 Gemini 分类 tab。
  it('renders a Gemini tab for Gemini Interactions provider rows', () => {
    // html 使用真实组件和仅包含 Interactions 的后端计数。
    const html = renderToStaticMarkup(
      // AI Provider scope 应把 Interactions 聚合到 Gemini。
      <CredentialProviderFilterBar scope="ai-provider" typeCounts={[{ type: 'gemini-interactions', count: 3 }]} value="all" onChange={() => undefined} />,
    )
    // Gemini 分类标签和聚合计数必须可见。
    expect(html).toContain('usage_stats.credentials_filter_gemini (3)')
    expect(html).toContain('role="tablist"')
    // GeminiCLI 是 Auth Files 标签，不能出现在 AI Provider scope。
    expect(html).not.toContain('usage_stats.credentials_filter_gemini_cli')
  })

  // xAI API Key 需要在 AI Provider scope 显示标准分类 tab。
  it('renders the xAI tab for xAI provider rows', () => {
    // html 使用仅包含 xAI API Key 的 AI Provider 计数。
    const html = renderToStaticMarkup(
      // xAI provider 使用独立筛选按钮。
      <CredentialProviderFilterBar scope="ai-provider" typeCounts={[{ type: 'xai', count: 2 }]} value="all" onChange={() => undefined} />,
    )
    // 保留 xAI 翻译 key 和计数。
    expect(html).toContain('usage_stats.credentials_filter_xai (2)')
    expect(html).toContain('role="tab"')
  })

  // Auth Files xAI 行为必须保持不变。
  it('keeps the xAI Auth Files filter available', () => {
    // html 使用 Auth Files scope 的 xAI OAuth 行。
    const html = renderToStaticMarkup(
      // Auth Files 仍使用相同 xai 原始 type。
      <CredentialProviderFilterBar scope="auth-files" typeCounts={[{ type: 'xai', count: 1 }]} value="all" onChange={() => undefined} />,
    )
    // xAI Auth Files 标签和计数继续显示。
    expect(html).toContain('usage_stats.credentials_filter_xai (1)')
    expect(html).toContain('role="tab"')
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
