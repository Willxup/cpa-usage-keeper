import { describe, expect, it } from 'vitest';
import i18n from '../index';

describe('range filter labels', () => {
  it('uses the compact Range label in every supported language', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.range_filter')).toBe('Range');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.range_filter')).toBe('范围');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.range_filter')).toBe('範圍');
  });

  it('uses singular and plural English copy for rolling values', () => {
    expect(i18n.t('usage_stats.range_value_day', { lng: 'en', count: 1 })).toBe('Day');
    expect(i18n.t('usage_stats.range_value_day', { lng: 'en', count: 2 })).toBe('Days');
    expect(i18n.t('usage_stats.range_last_days', { lng: 'en', count: 1 })).toBe('Last 1 day');
    expect(i18n.t('usage_stats.range_last_days', { lng: 'en', count: 2 })).toBe('Last 2 days');
    expect(i18n.t('usage_stats.range_value_hour', { lng: 'en', count: 5 })).toBe('Hours');
    expect(i18n.t('usage_stats.range_last_hours', { lng: 'en', count: 5 })).toBe('Last 5 hours');
  });
});
