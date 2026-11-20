import { describe, expect, it } from 'vitest'
import i18n, { SUPPORTED_LANGUAGES } from '../index'

const OFFICIAL_PLAN_LABELS = {
  'usage_stats.credentials_subscription_codex_pro_10x': 'Pro 10x',
  'usage_stats.credentials_subscription_codex_pro_20x': 'Pro 20x',
  'usage_stats.credentials_subscription_codex_pro_50x': 'Pro 50x',
  'usage_stats.credentials_subscription_codex_team': 'Team',
  'usage_stats.credentials_subscription_codex_business': 'Business',
  'usage_stats.credentials_subscription_codex_go': 'Go',
  'usage_stats.credentials_subscription_codex_edu': 'Edu',
  'usage_stats.credentials_subscription_claude_free': 'Free',
  'usage_stats.credentials_subscription_claude_pro': 'Pro',
  'usage_stats.credentials_subscription_claude_max': 'Max',
  'usage_stats.credentials_subscription_claude_max_5x': 'Max',
  'usage_stats.credentials_subscription_claude_max_20x': 'Ultra',
  'usage_stats.credentials_subscription_claude_team': 'Team',
  'usage_stats.credentials_subscription_antigravity_free': 'Free',
  'usage_stats.credentials_subscription_antigravity_pro': 'Pro',
  'usage_stats.credentials_subscription_antigravity_ultra_lite': 'Ultra Lite',
  'usage_stats.credentials_subscription_antigravity_ultra': 'Ultra',
  'usage_stats.credentials_subscription_meta_everyday': 'Everyday',
  'usage_stats.credentials_subscription_meta_high': 'High',
  'usage_stats.credentials_subscription_meta_power': 'Power',
  'usage_stats.credentials_subscription_meta_maximum': 'Maximum',
} as const

describe('credential subscription translations', () => {
  it('keeps official plan names unchanged across languages', () => {
    for (const language of SUPPORTED_LANGUAGES) {
      for (const [key, label] of Object.entries(OFFICIAL_PLAN_LABELS)) {
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
