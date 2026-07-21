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
    const html = renderToStaticMarkup(createElement(ServiceHealthCard, { activity: null, loading: false, requestIdentity: 'admin::8h:::' }));

    expect(html).toContain('Request Health Timeline');
    expect(html).not.toContain('Reliability');
  });
});
