import { describe, expect, it } from 'vitest'
import type { UsageIdentityTypeCount } from '@/lib/types'
import { buildCredentialProviderFilterOptions, credentialProviderFilterTypes } from '../credentialProviderFilters'

// 当前测试组只验证品牌筛选到后端原始 type 的稳定映射。
describe('credentialProviderFilters', () => {
  // Auth Files 为 CPA 内置类型和仍可定位 identity 的 Gemini CLI 兼容行生成品牌按钮。
  it('keeps CPA built-in Auth Files filters and Gemini CLI compatibility', () => {
    // counts 同时包含内置、插件来源、未知和 AI Provider 专属 type。
    const counts: UsageIdentityTypeCount[] = [
      // Antigravity Auth File 应生成独立按钮。
      { type: 'antigravity', count: 1 },
      // Claude Auth File 应生成独立按钮。
      { type: 'claude', count: 2 },
      // Codex Auth File 应生成独立按钮。
      { type: 'codex', count: 3 },
      // 普通 Gemini Auth File 与 Gemini CLI 归入同一个品牌按钮。
      { type: 'gemini', count: 2 },
      // Kimi Auth File 应生成独立按钮。
      { type: 'kimi', count: 4 },
      // Gemini CLI 兼容行复用 Gemini 品牌按钮。
      { type: 'gemini-cli', count: 4 },
      // iFlow 不是 CPA 内置 identity 来源，不生成专属按钮。
      { type: 'iflow', count: 5 },
      // xAI OAuth/Auth File 应继续生成 xai 按钮。
      { type: 'xai', count: 6 },
      // Vertex 导入凭证应生成独立按钮。
      { type: 'vertex', count: 7 },
      // Gemini Interactions 不是 Auth File 专用筛选项，只计入 All。
      { type: 'gemini-interactions', count: 3 },
      // 未知类型只计入 All。
      { type: 'unknown-auth', count: 8 },
    ]
    // options 构造 Auth Files scope 的可见按钮。
    const options = buildCredentialProviderFilterOptions('auth-files', counts)
    // All 保留全部原始行计数，专用按钮只显示 CPA 内置 Auth File 品牌。
    expect(options.map((option) => [option.key, option.count])).toEqual([
      // All 不隐藏插件或未来类型的数据。
      ['all', 45],
      ['antigravity', 1],
      ['claude', 2],
      ['codex', 3],
      ['gemini', 6],
      ['kimi', 4],
      ['xai', 6],
      ['vertex', 7],
    ])
    expect(credentialProviderFilterTypes('auth-files', 'gemini')).toEqual(['gemini', 'gemini-cli'])
    expect(credentialProviderFilterTypes('auth-files', 'kimi')).toEqual(['kimi'])
    expect(credentialProviderFilterTypes('auth-files', 'xai')).toEqual(['xai'])
    expect(credentialProviderFilterTypes('auth-files', 'vertex')).toEqual(['vertex'])
  })

  // AI Provider 顺序跟随 CPA provider metadata 内置来源，Gemini 品牌聚合两种原始 type。
  it('uses the CPA built-in AI Provider registry order', () => {
    // counts 覆盖全部内置来源与未知类型。
    const counts: UsageIdentityTypeCount[] = [
      { type: 'codex', count: 6 },
      // 普通 Gemini 行贡献两个。
      { type: 'gemini', count: 2 },
      // Gemini CLI 兼容行也归入 Gemini 品牌。
      { type: 'gemini-cli', count: 4 },
      // Interactions 行贡献三个。
      { type: 'gemini-interactions', count: 3 },
      // xAI API Key 行贡献四个。
      { type: 'xai', count: 4 },
      // Claude 保持既有按钮。
      { type: 'claude', count: 1 },
      // Vertex 使用独立按钮。
      { type: 'vertex', count: 3 },
      // OpenAI Compatibility 保持既有 OpenAI 按钮。
      { type: 'openai', count: 5 },
      // 未知 provider 只进入 All。
      { type: 'future-provider', count: 7 },
    ]
    // options 构造 AI Provider scope 的品牌按钮。
    const options = buildCredentialProviderFilterOptions('ai-provider', counts)
    // Gemini count 是两种原始 type 之和，其余来源按 CPA registry 顺序排列。
    expect(options.map((option) => [option.key, option.count, option.labelKey])).toEqual([
      ['all', 35, 'usage_stats.credentials_filter_all'],
      ['codex', 6, 'usage_stats.credentials_filter_codex'],
      ['xai', 4, 'usage_stats.credentials_filter_xai'],
      ['gemini', 9, 'usage_stats.credentials_filter_gemini'],
      ['claude', 1, 'usage_stats.credentials_filter_claude'],
      ['vertex', 3, 'usage_stats.credentials_filter_vertex'],
      ['openai', 5, 'usage_stats.credentials_filter_openai'],
    ])
    // Gemini 品牌查询必须同时发送普通、CLI 与 Interactions 原始 type。
    expect(credentialProviderFilterTypes('ai-provider', 'gemini')).toEqual(['gemini', 'gemini-cli', 'gemini-interactions'])
    // xAI AI Provider 查询只发送 xai。
    expect(credentialProviderFilterTypes('ai-provider', 'xai')).toEqual(['xai'])
    expect(credentialProviderFilterTypes('ai-provider', 'vertex')).toEqual(['vertex'])
    // OpenAI AI Provider 查询继续只发送 openai。
    expect(credentialProviderFilterTypes('ai-provider', 'openai')).toEqual(['openai'])
  })

  // 空列表与无效计数继续隐藏整个筛选栏。
  it('returns no options when every backend count is unusable', () => {
    // counts 覆盖零值、负数和非有限值。
    const counts: UsageIdentityTypeCount[] = [
      // 零值不贡献 All。
      { type: 'gemini', count: 0 },
      // 负数不贡献 All。
      { type: 'gemini-interactions', count: -1 },
      // 非有限值不贡献 All。
      { type: 'xai', count: Number.NaN },
    ]
    // 没有正计数时不生成任何 option。
    expect(buildCredentialProviderFilterOptions('ai-provider', counts)).toEqual([])
    // All 查询仍表示不添加后端 type filter。
    expect(credentialProviderFilterTypes('ai-provider', 'all')).toEqual([])
  })
})
