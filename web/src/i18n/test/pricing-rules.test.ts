import { describe, expect, it } from 'vitest'
import i18n, { SUPPORTED_LANGUAGES } from '../index'

describe('pricing rule translations', () => {
  it('defines the Rules UI copy in every supported language', () => {
    for (const language of SUPPORTED_LANGUAGES) {
      const usageStats = i18n.getResourceBundle(language, 'translation').usage_stats
      for (const key of [
        'model_price_rules',
        'model_price_rules_title',
        'model_price_rules_key',
        'model_price_rules_value',
        'model_price_rules_multiplier',
        'model_price_rules_add',
        'model_price_rules_remove',
        'model_price_rules_help',
        'model_price_rules_help_examples',
        'model_price_rules_help_service_tier',
        'model_price_rules_help_reasoning_effort',
      ]) {
        expect(usageStats[key], `${language}.${key}`).toBeTruthy()
      }
    }
  })

  it('keeps the help copy limited to the two approved examples', () => {
    for (const language of SUPPORTED_LANGUAGES) {
      const usageStats = i18n.getResourceBundle(language, 'translation').usage_stats
      const help = [
        usageStats.model_price_rules_help,
        usageStats.model_price_rules_help_service_tier,
        usageStats.model_price_rules_help_reasoning_effort,
      ].join(' ')
      expect(help).toContain('service_tier')
      expect(help).toContain('priority')
      expect(help).toContain('reasoning_effort')
      expect(help).toContain('xhigh')
      expect(help).not.toContain('api_group_key')
      expect(help).not.toContain('response_service_tier')
      expect(help).not.toContain('executor_type')
    }
  })

  it('uses a compact remove label in every supported language', () => {
    expect(i18n.getResourceBundle('en', 'translation').usage_stats.model_price_rules_remove).toBe('Remove')
    expect(i18n.getResourceBundle('zh', 'translation').usage_stats.model_price_rules_remove).toBe('删除')
    expect(i18n.getResourceBundle('zh-TW', 'translation').usage_stats.model_price_rules_remove).toBe('刪除')
  })

  it('localizes rule field labels and validation in Chinese', () => {
    const zh = i18n.getResourceBundle('zh', 'translation').usage_stats
    const zhTW = i18n.getResourceBundle('zh-TW', 'translation').usage_stats

    expect(zh.model_price_rules_key).toBe('字段')
    expect(zh.model_price_rules_value).toBe('设定值')
    expect(zh.model_price_rules_key_required).toBe('请输入字段。')
    expect(zh.model_price_rules_value_required).toBe('请输入设定值。')
    expect(zhTW.model_price_rules_key).toBe('欄位')
    expect(zhTW.model_price_rules_value).toBe('設定值')
    expect(zhTW.model_price_rules_key_required).toBe('請輸入欄位。')
    expect(zhTW.model_price_rules_value_required).toBe('請輸入設定值。')
  })

  it('labels the approved help examples in every supported language', () => {
    expect(i18n.getResourceBundle('en', 'translation').usage_stats.model_price_rules_help_examples).toBe('Examples:')
    expect(i18n.getResourceBundle('zh', 'translation').usage_stats.model_price_rules_help_examples).toBe('示例：')
    expect(i18n.getResourceBundle('zh-TW', 'translation').usage_stats.model_price_rules_help_examples).toBe('範例：')
  })

  it('capitalizes every word in English price sync actions only', () => {
    expect(i18n.t('usage_stats.model_price_sync_select_all', { lng: 'en' })).toBe('Select All')
    expect(i18n.t('usage_stats.model_price_sync_select_none', { lng: 'en' })).toBe('Clear Selection')
    expect(i18n.t('usage_stats.model_price_sync_update_selected', { lng: 'en', count: 3 })).toBe('Update Selected (3)')
    expect(i18n.t('usage_stats.model_price_sync_update_selected', { lng: 'zh', count: 3 })).toBe('更新所选（3）')
    expect(i18n.t('usage_stats.model_price_sync_update_selected', { lng: 'zh-TW', count: 3 })).toBe('更新所選（3）')
  })
})
