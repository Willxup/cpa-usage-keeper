import { describe, expect, it } from 'vitest'
import i18n, { SUPPORTED_LANGUAGES } from '../index'

const SUBSCRIPTION_KEYS = [
  'usage_stats.credentials_subscription_codex_free',
  'usage_stats.credentials_subscription_codex_plus',
  'usage_stats.credentials_subscription_codex_team',
  'usage_stats.credentials_subscription_codex_pro_5x',
  'usage_stats.credentials_subscription_codex_pro_20x',
  'usage_stats.credentials_subscription_codex_enterprise',
  'usage_stats.credentials_subscription_claude_free',
  'usage_stats.credentials_subscription_claude_pro',
  'usage_stats.credentials_subscription_claude_max',
  'usage_stats.credentials_subscription_claude_team',
  'usage_stats.credentials_subscription_antigravity_unknown',
  'usage_stats.credentials_subscription_antigravity_free',
  'usage_stats.credentials_subscription_antigravity_pro',
  'usage_stats.credentials_subscription_antigravity_ultra_lite',
  'usage_stats.credentials_subscription_antigravity_ultra',
] as const

const CLAUDE_LABELS = {
  'usage_stats.credentials_subscription_claude_free': 'Free',
  'usage_stats.credentials_subscription_claude_pro': 'Pro',
  'usage_stats.credentials_subscription_claude_max': 'Max',
  'usage_stats.credentials_subscription_claude_team': 'Team',
} as const

const ANTIGRAVITY_LABELS = {
  'usage_stats.credentials_subscription_antigravity_free': 'Free',
  'usage_stats.credentials_subscription_antigravity_pro': 'Pro',
  'usage_stats.credentials_subscription_antigravity_ultra_lite': 'Ultra Lite',
  'usage_stats.credentials_subscription_antigravity_ultra': 'Ultra',
} as const

describe('credential subscription translations', () => {
  it('publishes every known subscription label in each supported language', () => {
    for (const language of SUPPORTED_LANGUAGES) {
      for (const key of SUBSCRIPTION_KEYS) {
        const value = i18n.getResource(language, 'translation', key)
        expect(value, `${language}:${key}`).toEqual(expect.any(String))
        expect(value.trim(), `${language}:${key}`).not.toBe('')
      }
    }
  })

  it('keeps official Claude plan names unchanged across languages', () => {
    for (const language of SUPPORTED_LANGUAGES) {
      for (const [key, label] of Object.entries(CLAUDE_LABELS)) {
        expect(i18n.getResource(language, 'translation', key), `${language}:${key}`).toBe(label)
      }
    }
  })

  it('keeps official Antigravity plan names unchanged across languages', () => {
    for (const language of SUPPORTED_LANGUAGES) {
      for (const [key, label] of Object.entries(ANTIGRAVITY_LABELS)) {
        expect(i18n.getResource(language, 'translation', key), `${language}:${key}`).toBe(label)
      }
    }
  })

  it('localizes the unknown Antigravity fallback label', () => {
    const key = 'usage_stats.credentials_subscription_antigravity_unknown'
    expect(i18n.getResource('en', 'translation', key)).toBe('Unknown')
    expect(i18n.getResource('zh', 'translation', key)).toBe('未知')
    expect(i18n.getResource('zh-TW', 'translation', key)).toBe('未知')
  })
})
