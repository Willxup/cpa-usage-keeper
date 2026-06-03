import { describe, expect, it } from 'vitest';
import i18n, { SUPPORTED_LANGUAGES } from './index';

const flattenKeys = (value: unknown, prefix = ''): string[] => {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return [prefix];
  return Object.entries(value).flatMap(([key, child]) => {
    const path = prefix ? `${prefix}.${key}` : key;
    return flattenKeys(child, path);
  });
};

describe('i18n resources', () => {
  it('keeps every supported language aligned with English keys', () => {
    const englishKeys = flattenKeys(i18n.getResourceBundle('en', 'translation')).sort();

    for (const language of SUPPORTED_LANGUAGES) {
      expect(flattenKeys(i18n.getResourceBundle(language, 'translation')).sort()).toEqual(englishKeys);
    }
  });

  it('localizes Analysis tab and composition titles in Chinese', () => {
    expect(i18n.getResource('zh', 'translation', 'usage_stats.tab_analysis')).toBe('分析');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.analysis_auth_files_composition_title')).toBe('认证文件构成');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.analysis_ai_provider_composition_title')).toBe('AI 供应商构成');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.tab_analysis')).toBe('分析');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.analysis_auth_files_composition_title')).toBe('認證檔案組成');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.analysis_ai_provider_composition_title')).toBe('AI 供應商組成');
  });

  it('keeps the all option in the API Key filter generic across languages', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.api_key_filter_all')).toBe('All');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.api_key_filter_all')).toBe('全部');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.api_key_filter_all')).toBe('全部');
  });

  it('uses explicit Chinese labels for request event latency columns', () => {
    expect(i18n.getResource('zh', 'translation', 'usage_stats.ttft')).toBe('首字延迟');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.latency')).toBe('总延迟');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.ttft')).toBe('首字延遲');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.latency')).toBe('總延遲');
  });

  it('uses compact Chinese labels for request event type column', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.request_type')).toBe('Type');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.request_type')).toBe('类型');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.request_type')).toBe('類型');
  });

  it('keeps Analysis heatmap cell tooltips focused on model totals', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.analysis_heatmap_cell_title')).toBe('{{model}}: {{tokens}} Tokens, {{requests}} Requests');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.analysis_heatmap_cell_title')).toBe('{{model}}：{{tokens}} Token，{{requests}} 次请求');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.analysis_heatmap_cell_title')).toBe('{{model}}：{{tokens}} Token，{{requests}} 次請求');
  });

  it('localizes compact Analysis heatmap cell prefixes', () => {
    for (const language of SUPPORTED_LANGUAGES) {
      expect(i18n.getResource(language, 'translation', 'usage_stats.analysis_heatmap_tokens_prefix')).toBe('T');
      expect(i18n.getResource(language, 'translation', 'usage_stats.analysis_heatmap_requests_prefix')).toBe('R');
    }
  });

  it('uses natural Chinese and Traditional Chinese copy for API Key viewer text', () => {
    const zh = i18n.getResourceBundle('zh', 'translation');
    const zhTW = i18n.getResourceBundle('zh-TW', 'translation');

    expect(zh.usage_stats.tab_analysis).toBe('分析');
    expect(zhTW.usage_stats.tab_analysis).toBe('分析');
    expect(JSON.stringify(zh)).not.toMatch(/该 key|当前 key|完整 key|打开 Key 概览|API-Key|凭证的只读|当前凭证/);
    expect(JSON.stringify(zhTW)).not.toMatch(/該 key|目前 key|完整 key|開啟 Key 總覽|API-Key|金鑰的唯讀|目前金鑰/);
  });

  it('uses direct API Key error wording in every language', () => {
    expect(i18n.getResource('en', 'translation', 'auth.invalid_api_key')).toBe('API Key is incorrect');
    expect(i18n.getResource('zh', 'translation', 'auth.invalid_api_key')).toBe('API Key 错误');
    expect(i18n.getResource('zh-TW', 'translation', 'auth.invalid_api_key')).toBe('API Key 錯誤');
  });

  it('uses compact status-code labels for Auth Files inspection failures', () => {
    for (const language of SUPPORTED_LANGUAGES) {
      expect(i18n.getResource(language, 'translation', 'usage_stats.credentials_inspection_401')).toBe('401');
      expect(i18n.getResource(language, 'translation', 'usage_stats.credentials_inspection_402')).toBe('402');
    }
  });

  it('keeps the login product title aligned across languages', () => {
    expect(i18n.getResourceBundle('en', 'translation').auth.login_title).toBe('CPA Usage Statistics Dashboard');
    expect(i18n.getResourceBundle('zh', 'translation').auth.login_title).toBe('CPA 用量统计仪表盘');
    expect(i18n.getResourceBundle('zh-TW', 'translation').auth.login_title).toBe('CPA 用量統計儀表板');
  });
});
