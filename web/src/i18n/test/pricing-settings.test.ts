import { describe, expect, it } from 'vitest'
import i18n, { SUPPORTED_LANGUAGES } from '../index'

describe('complete pricing translations', () => {
  it('explains recalculation, partial failure and busy statistics in every supported language', () => {
    for (const language of SUPPORTED_LANGUAGES) {
      const usage = i18n.getResourceBundle(language, 'translation').usage_stats
      for (const key of ['pricing_recalculation_title', 'pricing_recalculation_impact',
        'pricing_recalculation_partial_kept', 'costs_busy']) {
        expect(usage[key].trim()).not.toBe('')
      }
    }
  })

  it('interpolates the saved model count without falling back to a key', () => {
    for (const language of SUPPORTED_LANGUAGES) {
      const label = i18n.t('usage_stats.pricing_recalculation_model_count', { lng: language, count: 3 })
      expect(label).toContain('3')
      expect(label).not.toContain('pricing_recalculation_model_count')
    }
  })
})
