import { describe, expect, it } from 'vitest'
import type { UsageIdentityTypeCount } from '@/lib/types'
import { buildCredentialProviderFilterOptions, credentialProviderFilterTypes } from '../credentialProviderFilters'
describe('credentialProviderFilters', () => {
  // 未知类型计入 All；Gemini 合并兼容 type。
  it('keeps CPA built-in Auth Files filters and Gemini CLI compatibility', () => {
    const counts: UsageIdentityTypeCount[] = [
      { type: 'antigravity', count: 1 },
      { type: 'claude', count: 2 },
      { type: 'codex', count: 3 },
      { type: 'devin', count: 9 },
      { type: 'gemini', count: 2 },
      { type: 'kimi', count: 4 },
      { type: 'kimi-ai', count: 2 },
      { type: 'kimi.ai', count: 3 },
      { type: 'kimi.com', count: 1 },
      { type: 'gemini-cli', count: 4 },
      { type: 'iflow', count: 5 },
      { type: 'xai', count: 6 },
      { type: 'vertex', count: 7 },
      { type: 'meta', count: 10 },
      { type: 'gemini-interactions', count: 3 },
      { type: 'unknown-auth', count: 8 },
    ]
    const options = buildCredentialProviderFilterOptions('auth-files', counts)
    expect(options.map((option) => [option.key, option.count])).toEqual([
      ['all', 70],
      ['antigravity', 1],
      ['claude', 2],
      ['codex', 3],
      ['devin', 9],
      ['gemini', 6],
      ['kimi', 10],
      ['xai', 6],
      ['vertex', 7],
    ])
    expect(credentialProviderFilterTypes('auth-files', 'gemini')).toEqual(['gemini', 'gemini-cli'])
    expect(credentialProviderFilterTypes('auth-files', 'devin')).toEqual(['devin'])
    expect(credentialProviderFilterTypes('auth-files', 'kimi')).toEqual(['kimi', 'kimi-ai', 'kimi.ai', 'kimi.com'])
    expect(credentialProviderFilterTypes('auth-files', 'xai')).toEqual(['xai'])
    expect(credentialProviderFilterTypes('auth-files', 'vertex')).toEqual(['vertex'])
  })
  // AI Provider 的 Gemini 还包括 Interactions，顺序沿用 CPA registry。
  it('uses the CPA built-in AI Provider registry order', () => {
    const counts: UsageIdentityTypeCount[] = [
      { type: 'codex', count: 6 },
      { type: 'gemini', count: 2 },
      { type: 'gemini-cli', count: 4 },
      { type: 'gemini-interactions', count: 3 },
      { type: 'xai', count: 4 },
      { type: 'claude', count: 1 },
      { type: 'vertex', count: 3 },
      { type: 'meta', count: 8 },
      { type: 'openai', count: 5 },
      { type: 'future-provider', count: 7 },
    ]
    const options = buildCredentialProviderFilterOptions('ai-provider', counts)
    expect(options.map((option) => [option.key, option.count, option.labelKey])).toEqual([
      ['all', 43, 'usage_stats.credentials_filter_all'],
      ['codex', 6, 'usage_stats.credentials_filter_codex'],
      ['xai', 4, 'usage_stats.credentials_filter_xai'],
      ['gemini', 9, 'usage_stats.credentials_filter_gemini'],
      ['claude', 1, 'usage_stats.credentials_filter_claude'],
      ['vertex', 3, 'usage_stats.credentials_filter_vertex'],
      ['meta', 8, 'usage_stats.credentials_filter_meta'],
      ['openai', 5, 'usage_stats.credentials_filter_openai'],
    ])
    expect(credentialProviderFilterTypes('ai-provider', 'gemini')).toEqual(['gemini', 'gemini-cli', 'gemini-interactions'])
    expect(credentialProviderFilterTypes('ai-provider', 'xai')).toEqual(['xai'])
    expect(credentialProviderFilterTypes('ai-provider', 'vertex')).toEqual(['vertex'])
    expect(credentialProviderFilterTypes('ai-provider', 'meta')).toEqual(['meta'])
    expect(credentialProviderFilterTypes('ai-provider', 'openai')).toEqual(['openai'])
  })
  it('returns no options when every backend count is unusable', () => {
    const counts: UsageIdentityTypeCount[] = [
      { type: 'gemini', count: 0 },
      { type: 'gemini-interactions', count: -1 },
      { type: 'xai', count: Number.NaN },
    ]
    expect(buildCredentialProviderFilterOptions('ai-provider', counts)).toEqual([])
    expect(credentialProviderFilterTypes('ai-provider', 'all')).toEqual([])
  })
})
