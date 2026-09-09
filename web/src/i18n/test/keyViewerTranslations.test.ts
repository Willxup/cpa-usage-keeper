import { describe, expect, it } from 'vitest';
import i18n from '../index';

describe('API Key viewer translations', () => {
  it.each(['en', 'zh', 'zh-TW'] as const)('provides viewer Analysis copy in %s', (language) => {
    expect(i18n.t('key_analysis.load_failed', { lng: language })).not.toBe('key_analysis.load_failed');
    expect(i18n.t('key_analysis.latency_load_failed', { lng: language })).not.toBe('key_analysis.latency_load_failed');
    expect(i18n.t('key_overview.tabs_aria_label', { lng: language })).not.toBe('key_overview.tabs_aria_label');
  });
});
