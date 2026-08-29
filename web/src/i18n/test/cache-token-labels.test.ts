import { describe, expect, it } from 'vitest';
import i18n from '../index';

describe('cache token labels', () => {
  it('provides cache read and cache write labels in every supported language', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.cache_read_tokens')).toBe('Cache Read');
    expect(i18n.getResource('en', 'translation', 'usage_stats.cache_creation_tokens')).toBe('Cache Write');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.cache_read_tokens')).toBe('缓存读取');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.cache_creation_tokens')).toBe('缓存写入');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.cache_read_tokens')).toBe('快取讀取');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.cache_creation_tokens')).toBe('快取寫入');
  });
});
