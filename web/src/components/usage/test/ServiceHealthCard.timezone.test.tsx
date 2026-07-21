import { createElement } from 'react';
import { readFileSync } from 'node:fs';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import '@/i18n';
import type { UsageActivityResponse } from '@/lib/types';
import { ServiceHealthCard } from '../ServiceHealthCard';

const usagePageStyles = readFileSync(new URL('../../../pages/UsagePage.module.scss', import.meta.url), 'utf8');

describe('ServiceHealthCard project timezone', () => {
  it('formats backend Activity boundaries in Asia/Shanghai when the test process runs in UTC', () => {
    // 测试命令固定 TZ=UTC；响应 timezone 才是窗口与 tooltip 标签的唯一显示时区。
    const activity: UsageActivityResponse = {
      window: '24h',
      grain: 'short',
      timezone: 'Asia/Shanghai',
      total_success: 1,
      total_failure: 0,
      success_rate: 100,
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
      }],
    };

    const html = renderToStaticMarkup(createElement(ServiceHealthCard, { activity, loading: false, requestIdentity: 'admin::24h:::' }));

    expect(html).toContain('07/20 08:00 – 07/21 08:00');
    expect(html).toContain('07/20 08:00 – 07/20 08:04');
    expect(html).not.toContain('07/20 00:00 – 07/21 00:00');
  });
});

describe('ServiceHealthCard dense grid containment', () => {
  it('contains the mobile 364-cell grid inside the card scroller', () => {
    const healthCardRule = usagePageStyles.match(/\.healthCard\s*\{([\s\S]*?)\n\}/)?.[1] ?? '';
    const mobileRules = usagePageStyles.slice(usagePageStyles.lastIndexOf('@include mobile'));

    expect(healthCardRule).toMatch(/min-width:\s*0;/);
    expect(healthCardRule).toMatch(/--health-grid-columns:\s*52;/);
    expect(mobileRules).toMatch(/\.healthGridScroller\s*\{[\s\S]*?overflow-x:\s*auto;/);
    expect(mobileRules).toMatch(/\.healthGrid\s*\{[\s\S]*?min-width:\s*540px;/);
  });
});
