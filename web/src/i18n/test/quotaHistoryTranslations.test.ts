import { describe, expect, it } from 'vitest'
import i18n from '../index'

describe('Codex quota history translations', () => {
  it('keeps English card headings and cycle count in project title case', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_quota_history_current_title')).toBe('Current Cycle Efficiency')
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_quota_history_records_title')).toBe('Cycle Records In The Last 30 Days')
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_quota_history_cycle_count')).toBe('{{count}} Cycles')
  })

  it.each([
    ['en', '1-point sample', 'Multi-point average', '{{count}}-point average', 'Used', 'Full-quota estimate', 'Median per 1%', 'Estimated unused', 'Unused percentage'],
    ['zh', '单百分点样本', '多百分点均摊', '{{count}} 个百分点均摊', '已用', '满额估算', '每 1% 中位数', '估算未用', '未用百分比'],
    ['zh-TW', '單百分點樣本', '多百分點均攤', '{{count}} 個百分點均攤', '已用', '滿額估算', '每 1% 中位數', '估算未用', '未用百分比'],
  ])('describes quota samples and summaries clearly in %s', (language, direct, cross, crossPoints, used, fullEstimate, medianPerPoint, estimatedUnused, unusedPercentage) => {
    expect(i18n.getResource(language, 'translation', 'usage_stats.credentials_quota_history_direct')).toBe(direct)
    expect(i18n.getResource(language, 'translation', 'usage_stats.credentials_quota_history_cross')).toBe(cross)
    expect(i18n.getResource(language, 'translation', 'usage_stats.credentials_quota_history_cross_points')).toBe(crossPoints)
    expect(i18n.getResource(language, 'translation', 'usage_stats.credentials_quota_history_used')).toBe(used)
    expect(i18n.getResource(language, 'translation', 'usage_stats.credentials_quota_history_full_estimate')).toBe(fullEstimate)
    expect(i18n.getResource(language, 'translation', 'usage_stats.credentials_quota_history_median_per_point')).toBe(medianPerPoint)
    expect(i18n.getResource(language, 'translation', 'usage_stats.credentials_quota_history_estimated_unused')).toBe(estimatedUnused)
    expect(i18n.getResource(language, 'translation', 'usage_stats.credentials_quota_history_unused_percentage')).toBe(unusedPercentage)
  })
})
