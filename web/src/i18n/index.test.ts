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

  it('keeps the compact Request Events column labels aligned in Chinese and English', () => {
    const keys = [
      'request_events_timestamp',
      'api_key_filter',
      'request_events_source',
      'model_name',
      'reasoning_effort',
      'speed_mode',
      'request_events_result',
      'request_events_request',
      'request_events_latency',
      'speed',
      'request_events_tokens',
      'request_events_cache',
      'request_events_cost',
      'credentials_detail_executor',
      'client_ip',
      'x_forwarded_for',
      'user_agent',
    ];
    const labels = (language: string) => keys.map((key) => (
      i18n.getResource(language, 'translation', `usage_stats.${key}`)
    ));

    expect(labels('en')).toEqual([
      'Timestamp', 'API Key', 'Source', 'Model', 'Effort', 'Speed Mode', 'Result', 'Request',
      'Latency', 'Speed', 'Tokens', 'Cache', 'Cost', 'Executor', 'Client IP', 'X-Forwarded-For',
      'User Agent',
    ]);
    expect(labels('zh')).toEqual([
      '时间', 'API Key', '来源', '模型', '推理强度', '速度模式', '结果', '请求', '延时', '生成速度',
      'Tokens', '缓存', '成本', '执行器', '客户端 IP', 'X-Forwarded-For', '用户代理',
    ]);
  });

  it('localizes Token Activity and density labels in every supported language', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.token_activity_title')).toBe('Token Activity');
    expect(i18n.getResource('en', 'translation', 'usage_stats.token_activity_less')).toBe('Less');
    expect(i18n.getResource('en', 'translation', 'usage_stats.token_activity_more')).toBe('More');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.token_activity_title')).toBe('Token 活动');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.token_activity_less')).toBe('较少');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.token_activity_more')).toBe('较多');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.token_activity_title')).toBe('Token 活動');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.token_activity_less')).toBe('較少');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.token_activity_more')).toBe('較多');
  });

  it('keeps Auth Files display mode labels available in every language', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_auth_files_display_mode_aria')).toBe('Auth file display mode');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_auth_files_display_mode_aria')).toBe('认证文件显示模式');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_auth_files_display_mode_aria')).toBe('認證檔案顯示模式');
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_auth_files_display_mode_quota')).toBe('Quota');
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_auth_files_display_mode_health')).toBe('Health');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_auth_files_display_mode_quota')).toBe('限额');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_auth_files_display_mode_health')).toBe('健康');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_auth_files_display_mode_quota')).toBe('限額');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_auth_files_display_mode_health')).toBe('健康');
  });

  it('localizes the Auth Files inspection title in every language', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_inspection_title')).toBe('Auth Files Inspection');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_inspection_title')).toBe('认证文件巡检');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_inspection_title')).toBe('認證檔案巡檢');
  });

  it('localizes session management copy in Chinese', () => {
    const expected = {
      zh: {
        session_settings_title: '会话管理',
        session_settings_subtitle: '查看当前有效的仪表盘会话，并退出不再需要的访问。',
        session_settings_empty: '暂无有效会话。',
        session_settings_admin_label: '管理员会话',
        session_settings_type_admin: '管理员',
        session_settings_alias_edit: '编辑会话别名',
        session_settings_alias_placeholder: '会话别名',
        session_settings_alias_save_success: '会话别名已保存。',
        session_settings_alias_save_failed: '无法保存会话别名。',
        session_settings_current: '正在使用',
        session_settings_logout_one: '退出此会话',
        session_settings_admin_logout_title: '退出管理员会话？',
        session_settings_admin_logout_body: '只会退出此管理员会话，其他管理员会话仍保持有效。',
        session_settings_api_key_logout_title: '退出 API Key 会话？',
        session_settings_api_key_logout_body: '只会退出 {{label}} 的此 API Key 会话，其他会话仍保持有效。',
        session_settings_logout_success: '会话已退出。',
        session_settings_logout_failed: '无法退出会话。',
        logout_confirm_body: '这会退出当前设备上的当前会话。',
      },
      'zh-TW': {
        session_settings_title: '工作階段管理',
        session_settings_subtitle: '查看目前有效的儀表板工作階段，並登出不再需要的存取。',
        session_settings_empty: '尚無有效工作階段。',
        session_settings_admin_label: '管理員工作階段',
        session_settings_type_admin: '管理員',
        session_settings_alias_edit: '編輯工作階段別名',
        session_settings_alias_placeholder: '工作階段別名',
        session_settings_alias_save_success: '工作階段別名已儲存。',
        session_settings_alias_save_failed: '無法儲存工作階段別名。',
        session_settings_current: '使用中',
        session_settings_logout_one: '登出此工作階段',
        session_settings_admin_logout_title: '登出管理員工作階段？',
        session_settings_admin_logout_body: '僅會登出此管理員工作階段，其他管理員工作階段仍保持有效。',
        session_settings_api_key_logout_title: '登出 API Key 工作階段？',
        session_settings_api_key_logout_body: '僅會登出 {{label}} 的此 API Key 工作階段，其他工作階段仍保持有效。',
        session_settings_logout_success: '工作階段已登出。',
        session_settings_logout_failed: '無法登出工作階段。',
        logout_confirm_body: '這會登出目前裝置上的目前工作階段。',
      },
    } as const;

    for (const [language, values] of Object.entries(expected)) {
      for (const [key, value] of Object.entries(values)) {
        expect(i18n.getResource(language, 'translation', `usage_stats.${key}`)).toBe(value);
      }
    }
  });

  it('localizes generic realtime and Analysis terms in Chinese', () => {
    const expected = {
      zh: {
        overview_realtime_latency_distribution: '延迟分布',
        overview_realtime_latency_average: '平均延迟',
        overview_realtime_latency_empty: '暂无延迟样本',
        overview_realtime_tpm: 'Token/分钟',
        overview_realtime_rpm: '请求/分钟',
        overview_realtime_tokens_label: 'Token 数',
        avg_tokens: '平均 Token 数',
        analysis_model_efficiency_subtitle: '按模型比较每 1M 总 Token 的成本。',
        analysis_top_models_subtitle: '在所选时间范围内，比较各模型的总 Token 用量。',
        analysis_top_models_chart_aria: '各模型总 Token 用量随时间变化的堆叠图',
      },
      'zh-TW': {
        overview_realtime_latency_distribution: '延遲分布',
        overview_realtime_latency_average: '平均延遲',
        overview_realtime_latency_empty: '暫無延遲樣本',
        overview_realtime_tpm: 'Token/分鐘',
        overview_realtime_rpm: '請求/分鐘',
        overview_realtime_tokens_label: 'Token 數',
        avg_tokens: '平均 Token 數',
        analysis_model_efficiency_subtitle: '按模型比較每 1M 總 Token 的成本。',
        analysis_top_models_subtitle: '在所選時間範圍內，比較各模型的總 Token 用量。',
        analysis_top_models_chart_aria: '各模型總 Token 用量隨時間變化的堆疊圖',
      },
    } as const;

    for (const [language, values] of Object.entries(expected)) {
      for (const [key, value] of Object.entries(values)) {
        expect(i18n.getResource(language, 'translation', `usage_stats.${key}`)).toBe(value);
      }
    }
  });

  it('keeps credential table column headers available in every language', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_column_name')).toBe('Name');
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_column_quota')).toBe('Quota');
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_column_health')).toBe('Health');
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_column_activity')).toBe('Activity');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_column_name')).toBe('名称');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_column_quota')).toBe('限额');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_column_health')).toBe('健康');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_column_activity')).toBe('活动');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_column_name')).toBe('名稱');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_column_quota')).toBe('限額');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_column_health')).toBe('健康');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_column_activity')).toBe('活動');
  });

  it('keeps session source labels available in every language', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.session_settings_source_standard')).toBe('Standalone');
    expect(i18n.getResource('en', 'translation', 'usage_stats.session_settings_source_embed')).toBe('CPAMC Embed');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.session_settings_source_standard')).toBe('独立访问');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.session_settings_source_embed')).toBe('CPAMC 嵌入');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.session_settings_source_standard')).toBe('獨立訪問');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.session_settings_source_embed')).toBe('CPAMC 嵌入');
  });

  it('keeps credential health chart labels available in every language', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_health_last_5h')).toBe('Last 5h');
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_health_ok')).toBe('OK');
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_health_fail')).toBe('Fail');
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_health_status_success')).toBe('healthy');
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_health_status_warning')).toBe('degraded');
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_health_status_failure')).toBe('unhealthy');
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_health_status_empty')).toBe('no data');
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_health_summary_healthy')).toBe('Healthy');
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_health_summary_degraded')).toBe('Degraded');
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_health_summary_quiet')).toBe('Quiet');
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_health_no_failures_5h')).toBe('No failures in 5h');
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_health_no_requests_5h')).toBe('No requests in 5h');
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_health_time_summary')).toBe('Used {{lastUsed}} · Updated {{statsUpdated}}');
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_health_grid_aria')).toBe('{{name}} request health over the last 5 hours');
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_health_bucket_aria')).toBe('{{timeRange}}: {{status}}, {{successCount}} successful, {{failureCount}} failed, {{rate}}');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_health_last_5h')).toBe('最近 5 小时');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_health_status_warning')).toBe('部分失败');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_health_summary_healthy')).toBe('健康');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_health_summary_degraded')).toBe('波动');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_health_summary_quiet')).toBe('安静');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_health_no_failures_5h')).toBe('5 小时内无失败');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_health_bucket_aria')).toBe('{{timeRange}}：{{status}}，成功 {{successCount}}，失败 {{failureCount}}，{{rate}}');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_health_last_5h')).toBe('最近 5 小時');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_health_status_empty')).toBe('無資料');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_health_summary_healthy')).toBe('健康');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_health_summary_degraded')).toBe('波動');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_health_summary_quiet')).toBe('安靜');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_health_no_failures_5h')).toBe('5 小時內無失敗');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_health_bucket_aria')).toBe('{{timeRange}}：{{status}}，成功 {{successCount}}，失敗 {{failureCount}}，{{rate}}');
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

  it('describes request event speed using full output tokens', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.speed_hint')).toBe('Average output tokens per second after TTFT');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.speed_hint')).toBe('首字后输出 token 的平均速度');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.speed_hint')).toBe('首字後輸出 token 的平均速度');
  });

  it('labels request event client metadata across languages', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.client_ip')).toBe('Client IP');
    expect(i18n.getResource('en', 'translation', 'usage_stats.x_forwarded_for')).toBe('X-Forwarded-For');
    expect(i18n.getResource('en', 'translation', 'usage_stats.user_agent')).toBe('User Agent');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.client_ip')).toBe('客户端 IP');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.user_agent')).toBe('用户代理');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.client_ip')).toBe('用戶端 IP');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.user_agent')).toBe('使用者代理');
  });

  it('uses compact Chinese labels for request event type column', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.request_type')).toBe('Type');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.request_type')).toBe('类型');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.request_type')).toBe('類型');
  });

  it('labels the request speed mode column across languages', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.speed_mode')).toBe('Speed Mode');
    expect(i18n.getResource('en', 'translation', 'usage_stats.response_speed_mode')).toBe('Response Speed Mode');
    expect(i18n.getResource('en', 'translation', 'usage_stats.speed_mode_auto')).toBe('Auto');
    expect(i18n.getResource('en', 'translation', 'usage_stats.speed_mode_standard')).toBe('Standard');
    expect(i18n.getResource('en', 'translation', 'usage_stats.speed_mode_fast')).toBe('Fast');
    expect(i18n.getResource('en', 'translation', 'usage_stats.speed_mode_flex')).toBe('Flex');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.speed_mode')).toBe('速度模式');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.response_speed_mode')).toBe('响应速度模式');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.speed_mode_auto')).toBe('自动');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.speed_mode_standard')).toBe('标准');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.speed_mode_fast')).toBe('快速');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.speed_mode_flex')).toBe('弹性');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.speed_mode')).toBe('速度模式');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.response_speed_mode')).toBe('回應速度模式');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.speed_mode_auto')).toBe('自動');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.speed_mode_standard')).toBe('標準');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.speed_mode_fast')).toBe('快速');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.speed_mode_flex')).toBe('彈性');
  });

  it('keeps Analysis heatmap copy focused on hover details', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.analysis_heatmap_subtitle')).toBe('Token distribution across API keys and models with hover details.');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.analysis_heatmap_subtitle')).toBe('展示 API Key 与模型组合下的 Token 分布，悬浮查看明细。');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.analysis_heatmap_subtitle')).toBe('顯示 API Key 與模型組合下的 Token 分布，懸浮查看明細。');
    expect(i18n.getResource('en', 'translation', 'usage_stats.analysis_heatmap_total')).toBeUndefined();
    expect(i18n.getResource('zh', 'translation', 'usage_stats.analysis_heatmap_total')).toBeUndefined();
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.analysis_heatmap_total')).toBeUndefined();
  });

  it('labels Analysis cost metrics', () => {
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

  it('localizes realtime overview fallback errors', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.overview_realtime_load_failed')).toBe('Failed to load realtime overview');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.overview_realtime_load_failed')).toBe('实时概览加载失败');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.overview_realtime_load_failed')).toBe('即時概覽載入失敗');
  });

  it('labels the realtime overview section as metrics', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.overview_realtime_section_title')).toBe('Realtime Metrics');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.overview_realtime_section_title')).toBe('实时指标');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.overview_realtime_section_title')).toBe('即時指標');
  });

  it('localizes realtime overview sample and rolling hints', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.overview_realtime_ttft_empty')).toBe('No TTFT samples');
    expect(i18n.getResource('en', 'translation', 'usage_stats.overview_realtime_latency_empty')).toBe('No latency samples');
    expect(i18n.getResource('en', 'translation', 'usage_stats.overview_realtime_cache_empty')).toBe('No calculable cache rate');
    expect(i18n.getResource('en', 'translation', 'usage_stats.overview_realtime_rolling_metric_hint')).toBe('Latest, average and trend use rolling aggregation for the selected window.');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.overview_realtime_ttft_empty')).toBe('暂无 TTFT 样本');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.overview_realtime_cache_empty')).toBe('暂无可计算的缓存率');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.overview_realtime_latency_empty')).toBe('暫無延遲樣本');
  });

  it('uses a token share label for the realtime current-usage card', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.overview_realtime_current_usage')).toBe('Token Share');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.overview_realtime_current_usage')).toBe('Token 占比');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.overview_realtime_current_usage')).toBe('Token 占比');
  });

  it('labels realtime window deltas as trend instead of period-over-period change', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.overview_realtime_trend')).toBe('Trend');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.overview_realtime_trend')).toBe('趋势');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.overview_realtime_trend')).toBe('趨勢');
  });

  it('removes obsolete realtime response-level labels', () => {
    for (const language of SUPPORTED_LANGUAGES) {
      const usageStats = i18n.getResourceBundle(language, 'translation').usage_stats;
      expect(usageStats).not.toHaveProperty('overview_realtime_response_level');
      expect(usageStats).not.toHaveProperty('overview_realtime_ttft_p95');
      expect(usageStats).not.toHaveProperty('overview_realtime_latency_p95');
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
      expect(i18n.getResource(language, 'translation', 'usage_stats.credentials_inspection_401_402')).toBe('401/402');
    }
  });

  it('labels unknown Auth Files inspection results in every language', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_inspection_unknown')).toBe('Unknown');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_inspection_unknown')).toBe('未知');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_inspection_unknown')).toBe('未知');
  });

  it('uses concise invalid-account selection copy in every language', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_inspection_invalid_accounts_select_all')).toBe('Select all');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_inspection_invalid_accounts_select_all')).toBe('全选');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_inspection_invalid_accounts_select_all')).toBe('全選');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_inspection_invalid_accounts_invert_selection')).toBe('反选');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_inspection_invalid_accounts_sync_tip')).toBe('账号状态从 CPA 同步到 Keeper 可能存在短暂延迟。操作后如果列表暂未更新，请稍候再手动刷新。');
  });

  it('labels reached-limit Auth Files inspection results in every language', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_inspection_limit_reached')).toBe('Limit reached');
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_inspection_limit_reached')).toBe('达到限额');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_inspection_limit_reached')).toBe('達到限額');
  });


  it('keeps Auth Files quota reset copy available in every language', () => {
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_quota_reset_title')).toBe('Reset Codex quota')
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_quota_reset_button')).toBe('Reset quota, {{count}} available')
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_quota_reset_tooltip_suffix')).toBe('reset credits available')
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_quota_reset_message')).toBe('{{count}} reset credits are available. Consume 1 credit to reset now?')
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_quota_reset_message_suffix')).toBe('reset credits are available.')
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_quota_reset_message_prompt')).toBe('Consume 1 credit to reset now?')
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_quota_reset_confirm')).toBe('Reset')
    expect(i18n.getResource('en', 'translation', 'usage_stats.credentials_quota_reset_failed')).toBe('Quota reset failed. Please try again later.')
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_quota_reset_title')).toBe('重置 Codex 限额')
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_quota_reset_button')).toBe('重置限额，可用 {{count}} 次')
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_quota_reset_tooltip_suffix')).toBe('次重置机会')
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_quota_reset_message')).toBe('当前有 {{count}} 次可用重置次数。是否消耗 1 次立即重置？')
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_quota_reset_message_suffix')).toBe('次可用重置次数。')
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_quota_reset_message_prompt')).toBe('是否消耗 1 次立即重置？')
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_quota_reset_confirm')).toBe('确认重置')
    expect(i18n.getResource('zh', 'translation', 'usage_stats.credentials_quota_reset_failed')).toBe('重置限额失败，请稍后重试。')
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_quota_reset_title')).toBe('重置 Codex 限額')
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_quota_reset_button')).toBe('重置限額，可用 {{count}} 次')
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_quota_reset_tooltip_suffix')).toBe('次重置機會')
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_quota_reset_message')).toBe('目前有 {{count}} 次可用重置次數。是否消耗 1 次立即重置？')
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_quota_reset_message_suffix')).toBe('次可用重置次數。')
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_quota_reset_message_prompt')).toBe('是否消耗 1 次立即重置？')
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_quota_reset_confirm')).toBe('確認重置')
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.credentials_quota_reset_failed')).toBe('重置限額失敗，請稍後重試。')
  })

  it('keeps the login product identity aligned across languages', () => {
    expect(i18n.getResourceBundle('en', 'translation').auth.login_title).toBe('CPA USAGE KEEPER');
    expect(i18n.getResourceBundle('en', 'translation').auth.login_subtitle).toBe('Every flow leaves a trace.');
    expect(i18n.getResourceBundle('zh', 'translation').auth.login_title).toBe('CPA USAGE KEEPER');
    expect(i18n.getResourceBundle('zh', 'translation').auth.login_subtitle).toBe('万千流转，皆有迹可循。');
    expect(i18n.getResourceBundle('zh-TW', 'translation').auth.login_title).toBe('CPA USAGE KEEPER');
    expect(i18n.getResourceBundle('zh-TW', 'translation').auth.login_subtitle).toBe('萬千流轉，皆有跡可循。');
  });
});
