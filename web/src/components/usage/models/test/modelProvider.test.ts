import { describe, expect, it } from 'vitest'
import { modelProvider, normalizeCatalog } from '../modelCatalogUtils'

describe('model provider prefix priority', () => {
  it.each([
    ['gpt-6-astra', 'anthropic', 'provider:openai', 'OpenAI'],
    ['xxx/gpt-6-astra', 'anthropic', 'provider:openai', 'OpenAI'],
    [' team/route/GPT-6-ASTRA ', 'google', 'provider:openai', 'OpenAI'],
    ['gpt-5.3-codex', 'codex', 'provider:openai', 'OpenAI'],
    ['chatgpt-4o-latest', 'custom', 'provider:openai', 'OpenAI'],
    ['codex-mini-latest', 'custom', 'provider:openai', 'OpenAI'],
    ['o1', 'anthropic', 'provider:openai', 'OpenAI'],
    ['o3-mini', 'google', 'provider:openai', 'OpenAI'],
    ['team/o4-mini', 'custom', 'provider:openai', 'OpenAI'],
    ['claude-sonnet-4-6', 'openai', 'provider:claude', 'Claude'],
    ['openai/claude-opus-4-6', 'codex', 'provider:claude', 'Claude'],
    ['gemini-3.1-pro-preview', 'vertex', 'provider:gemini', 'Gemini'],
    ['grok-4-fast', 'openai', 'provider:xai', 'xAI'],
    ['kimi-k2.5', 'openai', 'provider:kimi', 'Kimi'],
    ['moonshot-v1-32k', 'openai', 'provider:kimi', 'Kimi'],
    ['meta-llama/Llama-3.1-70B-Instruct', 'openai', 'provider:meta', 'Meta'],
    ['Meta-Llama-3.1-70B', 'openai', 'provider:meta', 'Meta'],
    ['llama3.1', 'openai', 'provider:meta', 'Meta'],
    ['deepseek-chat', 'openai', 'provider:deepseek', 'DeepSeek'],
    ['xxx/deepseek-reasoner', 'openai', 'provider:deepseek', 'DeepSeek'],
    ['Qwen/Qwen3.5-Plus', 'openai', 'provider:qwen', 'Qwen'],
    ['muse-spark', 'meta', 'provider:muse', 'Muse'],
    ['xxx/muse-spark', 'openai', 'provider:muse', 'Muse'],
    ['mistralai/Mistral-Small', 'openai', 'owner:mistral', 'Mistral'],
    ['codestral-latest', 'openai', 'owner:mistral', 'Mistral'],
    ['MiniMax-M2.5', 'openai', 'owner:minimax', 'MiniMax'],
    ['z-ai/GLM-5', 'openai', 'provider:zai', 'Z.ai'],
    ['chatglm3-6b', 'openai', 'provider:zai', 'Z.ai'],
  ])('classifies %s by its model family despite owned_by=%s', (model, owner, key, label) => {
    expect(modelProvider(model, owner)).toMatchObject({ key, label })
  })

  it.each<[string, string | undefined, string, string]>([
    ['route/custom-model', ' anthropic ', 'provider:claude', 'Claude'],
    ['custom-model', 'GOOGLE', 'provider:gemini', 'Gemini'],
    ['custom-model', 'gemini-cli', 'provider:gemini', 'Gemini'],
    ['claudette', 'openai', 'provider:openai', 'OpenAI'],
    ['museum', 'meta', 'provider:meta', 'Meta'],
    ['custom-model', 'glm', 'provider:zai', 'Z.ai'],
    ['gptish', 'anthropic', 'provider:claude', 'Claude'],
    ['o365', 'anthropic', 'provider:claude', 'Claude'],
    ['my-gpt-6-astra', ' Team Gateway ', 'owner:team gateway', 'Team Gateway'],
    ['openai/custom-alias', 'custom', 'owner:custom', 'custom'],
    ['custom-model', 'constructor', 'owner:constructor', 'constructor'],
    ['custom-model', undefined, 'unknown', ''],
    ['', '   ', 'unknown', ''],
  ])('falls back to owned_by for an unrecognized family: %s', (model, owner, key, label) => {
    expect(modelProvider(model, owner)).toMatchObject({ key, label })
  })

  it('recognizes prefixes without owner metadata and preserves the full request ID', () => {
    const id = 'xxx/gpt-6-astra'
    expect(normalizeCatalog([{ id }, { id }, { id: '' }])).toEqual([
      { id, provider: { key: 'provider:openai', label: 'OpenAI', icon: 'openai' } },
    ])
  })

  it('groups prefix matches with equivalent owned_by fallback values', () => {
    const [prefixed, fallback] = normalizeCatalog([
      { id: 'deepseek-chat', owned_by: 'openai' },
      { id: 'fast-alias', owned_by: 'deepseek' },
    ])
    expect(prefixed.provider).toEqual(fallback.provider)
  })
})
