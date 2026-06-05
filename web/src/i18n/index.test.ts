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

  it('localizes Analysis tab and composition controls in Chinese', () => {
    expect(i18n.getResource('zh', 'translation', 'usage_stats.tab_analysis')).toBe('分析');
    expect(i18n.getResource('en', 'translation', 'usage_stats.analysis_composition_title')).toBe('Usage Distribution');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.analysis_composition_title')).toBe('用量分布');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.analysis_composition_auth_files_tab')).toBe('认证文件');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.analysis_composition_ai_provider_tab')).toBe('AI 供应商');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.analysis_composition_token_percent')).toBe('Token %');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.tab_analysis')).toBe('分析');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.analysis_composition_title')).toBe('用量分布');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.analysis_composition_auth_files_tab')).toBe('認證檔案');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.analysis_composition_ai_provider_tab')).toBe('AI 供應商');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.analysis_composition_token_percent')).toBe('Token %');
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

  it('keeps Analysis heatmap copy focused on hover details', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.analysis_heatmap_subtitle')).toBe('Token distribution across API keys and models with hover details.');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.analysis_heatmap_subtitle')).toBe('展示 API Key 与模型组合下的 Token 分布，悬浮查看明细。');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.analysis_heatmap_subtitle')).toBe('顯示 API Key 與模型組合下的 Token 分布，懸浮查看明細。');
  });

  it('labels Analysis cost blended rate metrics', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.analysis_cost_per_million_tokens')).toBe('Cost / 1M Tokens');
    expect(i18n.getResource('en', 'translation', 'usage_stats.analysis_blended_rate')).toBe('Blended Rate');
    expect(i18n.getResource('en', 'translation', 'usage_stats.analysis_cost_share')).toBe('Cost Share');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.analysis_blended_rate')).toBe('混合费率');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.analysis_cost_share')).toBe('成本占比');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.analysis_blended_rate')).toBe('混合費率');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.analysis_cost_share')).toBe('成本占比');
  });

  it('removes obsolete Analysis API and model stats labels', () => {
    for (const language of SUPPORTED_LANGUAGES) {
      const usageStats = i18n.getResourceBundle(language, 'translation').usage_stats;
      expect(usageStats).not.toHaveProperty('api_details');
      expect(usageStats).not.toHaveProperty('api_details_title');
      expect(usageStats).not.toHaveProperty('api_details_subtitle');
      expect(usageStats).not.toHaveProperty('api_details_eyebrow');
      expect(usageStats).not.toHaveProperty('model_stats');
      expect(usageStats).not.toHaveProperty('model_stats_title');
      expect(usageStats).not.toHaveProperty('model_stats_subtitle');
      expect(usageStats).not.toHaveProperty('model_stats_eyebrow');
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

  it('labels unknown Auth Files inspection results in every language', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_inspection_unknown')).toBe('Unknown');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_inspection_unknown')).toBe('未知');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_inspection_unknown')).toBe('未知');
  });

  it('labels reached-limit Auth Files inspection results in every language', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_inspection_limit_reached')).toBe('Limit reached');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_inspection_limit_reached')).toBe('达到限额');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_inspection_limit_reached')).toBe('達到限額');
  });

  it('keeps the login product title aligned across languages', () => {
    expect(i18n.getResourceBundle('en', 'translation').auth.login_title).toBe('CPA Usage Statistics Dashboard');
    expect(i18n.getResourceBundle('zh', 'translation').auth.login_title).toBe('CPA 用量统计仪表盘');
    expect(i18n.getResourceBundle('zh-TW', 'translation').auth.login_title).toBe('CPA 用量統計儀表板');
  });
});
