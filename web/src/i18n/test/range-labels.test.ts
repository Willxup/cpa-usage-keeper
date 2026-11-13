import { describe, expect, it } from 'vitest';
import i18n from '../index';

describe('range filter labels', () => {
  it('uses singular and plural English copy for rolling values', () => {
    expect(i18n.t('usage_stats.range_value_day', { lng: 'en', count: 1 })).toBe('Day');
    expect(i18n.t('usage_stats.range_value_day', { lng: 'en', count: 2 })).toBe('Days');
    expect(i18n.t('usage_stats.range_last_days', { lng: 'en', count: 1 })).toBe('Last 1 day');
    expect(i18n.t('usage_stats.range_last_days', { lng: 'en', count: 2 })).toBe('Last 2 days');
    expect(i18n.t('usage_stats.range_value_hour', { lng: 'en', count: 5 })).toBe('Hours');
    expect(i18n.t('usage_stats.range_last_hours', { lng: 'en', count: 5 })).toBe('Last 5 hours');
  });
});
