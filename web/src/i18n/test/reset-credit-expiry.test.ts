import { describe, expect, it } from 'vitest'
import i18n from '../index'

describe('reset credit expiry labels', () => {
  it('provides expiry and fallback labels in every supported language', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_quota_reset_expiry_title')).toBe('Manual reset expiry (GMT+8)')
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_quota_reset_expiry_failed')).toBe('Could not load expiry times. You can still reset using the cached count.')
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_quota_reset_expiry_title')).toBe('主动重置过期时间（GMT+8）')
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_quota_reset_expiry_failed')).toBe('过期时间获取失败，仍可按缓存次数继续重置。')
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_quota_reset_expiry_title')).toBe('主動重置過期時間（GMT+8）')
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_quota_reset_expiry_failed')).toBe('到期時間取得失敗，仍可依快取次數繼續重置。')
  })
})
