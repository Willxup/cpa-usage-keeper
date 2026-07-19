import { describe, expect, it } from 'vitest'
import i18n from '../index'

describe('credential health translations', () => {
  it('distinguishes degraded and unhealthy health states in every language', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_health_status_failure')).toBe('unhealthy')
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_health_summary_unhealthy')).toBe('Unhealthy')
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_health_failures_5h')).toBe('Failed requests in 5h: {{count}} · latest {{timeRange}}')

    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_health_status_failure')).toBe('异常')
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_health_summary_degraded')).toBe('波动')
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_health_summary_unhealthy')).toBe('异常')
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_health_failures_5h')).toBe('5 小时内失败 {{count}} 次 · 最近 {{timeRange}}')

    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_health_status_failure')).toBe('異常')
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_health_summary_degraded')).toBe('波動')
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_health_summary_unhealthy')).toBe('異常')
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_health_failures_5h')).toBe('5 小時內失敗 {{count}} 次 · 最近 {{timeRange}}')
  })

  it('does not retain the superseded last-failure translation key', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_health_last_failure')).toBeUndefined()
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_health_last_failure')).toBeUndefined()
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_health_last_failure')).toBeUndefined()
  })
})
