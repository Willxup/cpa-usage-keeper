import { describe, expect, it } from 'vitest'
import i18n, { SUPPORTED_LANGUAGES } from '../index'

describe('AI provider active-only translations', () => {
  it('defines the switch and OpenAI compatibility help in every supported language', () => {
    for (const language of SUPPORTED_LANGUAGES) {
      const usageStats = i18n.getResourceBundle(language, 'translation').usage_stats
      for (const key of [
        'credentials_ai_providers_active_only',
        'credentials_ai_providers_active_only_help_label',
        'credentials_ai_providers_active_only_help',
      ]) {
        expect(usageStats[key], `${language}.${key}`).toBeTruthy()
      }
      expect(usageStats.credentials_ai_providers_active_only_help).toContain('OpenAI')
    }
  })
})
