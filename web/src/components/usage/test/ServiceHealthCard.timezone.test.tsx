import { createElement } from 'react';
import { readFileSync } from 'node:fs';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it, vi } from 'vitest';
import '@/i18n';
import type { UsageActivityResponse } from '@/lib/types';
import { RecentActivityPanel } from '../RecentActivityPanel';

const usagePageStyles = readFileSync(new URL('../../../pages/UsagePage.module.scss', import.meta.url), 'utf8');

describe('Recent Activity project timezone', () => {
  it('formats the shared window and both grids in Asia/Shanghai when the test process runs in UTC', () => {
    // 测试命令固定 TZ=UTC；响应 timezone 才是窗口与 tooltip 标签的唯一显示时区。
    const activity: UsageActivityResponse = {
      window: '24h',
      grain: 'short',
      timezone: 'Asia/Shanghai',
      total_success: 1,
      total_failure: 0,
      success_rate: 100,
      input_tokens: 0,
      output_tokens: 0,
      reasoning_tokens: 0,
      cache_read_tokens: 0,
      cache_creation_tokens: 0,
      total_tokens: 0,
      rows: 7,
      columns: 52,
      bucket_seconds: 238,
      window_start: '2026-07-20T00:00:00Z',
      window_end: '2026-07-21T00:00:00Z',
      blocks: [{
        start_time: '2026-07-20T00:00:00Z',
        end_time: '2026-07-20T00:04:00Z',
        success: 1,
        failure: 0,
        rate: 1,
        input_tokens: 0,
        output_tokens: 0,
        reasoning_tokens: 0,
        cache_read_tokens: 0,
        cache_creation_tokens: 0,
        total_tokens: 0,
      }],
    };

    const html = renderToStaticMarkup(createElement(RecentActivityPanel, {
      activity,
      loading: false,
      error: '',
      window: '24h',
      requestIdentity: 'admin::24h:::',
      onWindowChange: vi.fn(),
    }));

    expect(html.match(/07\/20 08:00 – 07\/21 08:00/g)).toHaveLength(1);
    expect(html.match(/07\/20 08:00 – 07\/20 08:04/g)).toHaveLength(2);
    expect(html).not.toContain('07/20 00:00 – 07/21 00:00');
  });
});

describe('ServiceHealthCard dense grid containment', () => {
  it('contains the mobile 364-cell grid inside the card scroller', () => {
    const activityCardRule = usagePageStyles.match(/\.activityCard\s*\{([\s\S]*?)\n\}/)?.[1] ?? '';
    const mobileRules = usagePageStyles.slice(usagePageStyles.lastIndexOf('@include mobile'));

    expect(activityCardRule).toMatch(/min-width:\s*0;/);
    expect(usagePageStyles).toMatch(/\.activityHeatmapGrid\s*\{[\s\S]*?grid-template-columns:\s*repeat\(52, minmax\(0, 1fr\)\);/);
    expect(usagePageStyles).toMatch(/\.activityHeatmapGrid\s*\{[\s\S]*?aspect-ratio:\s*52 \/ 7;/);
    expect(mobileRules).not.toMatch(/\.activityHeatmapGrid\s*\{[\s\S]*?min-width:/);
  });
});
