import { createElement } from 'react';
import '@/i18n';
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { ServiceHealthCard, parseTime } from './ServiceHealthCard';

describe('ServiceHealthCard time parsing', () => {
  it('rounds RFC3339 nanosecond day boundaries consistently across browsers', () => {
    expect(parseTime('2026-05-17T23:59:59.999999999+08:00')).toBe(Date.parse('2026-05-18T00:00:00+08:00'));
    expect(parseTime('2026-05-16T23:59:59.999999999+08:00')).toBe(Date.parse('2026-05-17T00:00:00+08:00'));
  });

  it('keeps ordinary timestamps unchanged', () => {
    expect(parseTime('2026-05-17T12:34:56+08:00')).toBe(Date.parse('2026-05-17T12:34:56+08:00'));
    expect(parseTime('2026-05-17T12:34:56.123456789+08:00')).toBe(Date.parse('2026-05-17T12:34:56.123456789+08:00'));
  });
});

describe('ServiceHealthCard title', () => {
  it('renders the health title without the reliability label', () => {
    const html = renderToStaticMarkup(createElement(ServiceHealthCard, { usage: null, loading: false }));

    expect(html).toContain('Request Health Timeline');
    expect(html).not.toContain('Reliability');
  });

  it('makes every timeline block keyboard focusable with a complete description', () => {
    const html = renderToStaticMarkup(createElement(ServiceHealthCard, {
      loading: false,
      usage: {
        usage: { total_requests: 3, success_count: 2, failure_count: 1, total_tokens: 10 },
        service_health: {
          total_success: 2,
          total_failure: 1,
          success_rate: 66.7,
          rows: 1,
          columns: 1,
          bucket_seconds: 60,
          window_start: '2026-07-18T00:00:00Z',
          window_end: '2026-07-18T00:01:00Z',
          block_details: [{
            start_time: '2026-07-18T00:00:00Z',
            end_time: '2026-07-18T00:01:00Z',
            success: 2,
            failure: 1,
            rate: 2 / 3,
          }],
        },
      },
    }));

    expect(html).toContain('role="listitem"');
    expect(html).toContain('tabindex="0"');
    expect(html).toContain('OK 2, Fail 1');
  });
});
