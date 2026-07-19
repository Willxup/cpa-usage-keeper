import { describe, expect, it } from 'vitest'
import type { UsageIdentityTypeCount } from '@/lib/types'
import { buildCredentialProviderFilterOptions, credentialProviderFilterTypes } from '../credentialProviderFilters'

// 当前测试组只验证品牌筛选到后端原始 type 的稳定映射。
describe('credentialProviderFilters', () => {
  // Auth Files 的 xAI/GeminiCLI 行为不能被 AI Provider 新来源改变。
  it('keeps the existing Auth Files provider filters unchanged', () => {
    // counts 同时包含已知、未知和 AI Provider 专属 type。
    const counts: UsageIdentityTypeCount[] = [
      // Claude Auth File 应生成独立按钮。
      { type: 'claude', count: 2 },
      // GeminiCLI Auth File 应继续使用 gemini-cli key。
      { type: 'gemini-cli', count: 4 },
      // xAI OAuth/Auth File 应继续生成 xai 按钮。
      { type: 'xai', count: 6 },
      // Gemini Interactions 不是 Auth File 专用筛选项，只计入 All。
      { type: 'gemini-interactions', count: 3 },
      // 未知类型只计入 All。
      { type: 'unknown-auth', count: 5 },
    ]
    // options 构造 Auth Files scope 的可见按钮。
    const options = buildCredentialProviderFilterOptions('auth-files', counts)
    // All 保留全部原始行计数，专用按钮只显示已有 Auth File 品牌。
    expect(options.map((option) => [option.key, option.count])).toEqual([
      // All 包含 2+4+6+3+5。
      ['all', 20],
      // Claude 只计算 claude。
      ['claude', 2],
      // GeminiCLI 只计算 gemini-cli。
      ['gemini-cli', 4],
      // xAI 只计算 Auth File xai。
      ['xai', 6],
    ])
    // Auth Files 的 xAI 查询仍只发送原始 xai type。
    expect(credentialProviderFilterTypes('auth-files', 'xai')).toEqual(['xai'])
    // Auth Files 的 GeminiCLI 查询不吸收 Interactions。
    expect(credentialProviderFilterTypes('auth-files', 'gemini-cli')).toEqual(['gemini-cli'])
  })

  // AI Provider Gemini 品牌需要聚合两种原始 type，并保留 xAI 与 OpenAI 既有筛选。
  it('aggregates Gemini Interactions and keeps xAI and OpenAI in AI Provider scope', () => {
    // counts 覆盖两个 Gemini 原始 type、新 xAI、既有 OpenAI 与未知类型。
    const counts: UsageIdentityTypeCount[] = [
      // 普通 Gemini 行贡献两个。
      { type: 'gemini', count: 2 },
      // Interactions 行贡献三个。
      { type: 'gemini-interactions', count: 3 },
      // xAI API Key 行贡献四个。
      { type: 'xai', count: 4 },
      // Claude 保持既有按钮。
      { type: 'claude', count: 1 },
      // OpenAI Compatibility 保持既有 OpenAI 按钮。
      { type: 'openai', count: 5 },
      // 未知 provider 只进入 All。
      { type: 'future-provider', count: 7 },
    ]
    // options 构造 AI Provider scope 的品牌按钮。
    const options = buildCredentialProviderFilterOptions('ai-provider', counts)
    // Gemini count 必须是两种原始 type 之和，xAI 与 OpenAI 使用独立按钮。
    expect(options.map((option) => [option.key, option.count, option.labelKey])).toEqual([
      // All 包含全部 22 条原始 provider 行。
      ['all', 22, 'usage_stats.credentials_filter_all'],
      // Claude 保持原顺序和计数。
      ['claude', 1, 'usage_stats.credentials_filter_claude'],
      // Gemini 品牌聚合 2+3。
      ['gemini', 5, 'usage_stats.credentials_filter_gemini'],
      // xAI 使用现有翻译 key。
      ['xai', 4, 'usage_stats.credentials_filter_xai'],
      // OpenAI Compatibility 保持既有 OpenAI 翻译 key 和计数。
      ['openai', 5, 'usage_stats.credentials_filter_openai'],
    ])
    // Gemini 品牌查询必须同时发送普通 Gemini 与 Interactions 原始 type。
    expect(credentialProviderFilterTypes('ai-provider', 'gemini')).toEqual(['gemini', 'gemini-interactions'])
    // xAI AI Provider 查询只发送 xai。
    expect(credentialProviderFilterTypes('ai-provider', 'xai')).toEqual(['xai'])
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
