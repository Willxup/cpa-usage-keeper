import { createElement } from 'react';
import '@/i18n';
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { ServiceHealthCard, calculateHealthActivityLevel, parseTime } from './ServiceHealthCard';
import { buildUsageActivityFixture } from './test/activityFixtures';

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

describe('ServiceHealthCard health levels', () => {
  it('keeps red, orange and yellow below 80 percent', () => {
    expect(calculateHealthActivityLevel(-1, -1)).toBe(0);
    expect(calculateHealthActivityLevel(0, 0)).toBe(0);
    expect(calculateHealthActivityLevel(49, 51)).toBe(1);
    expect(calculateHealthActivityLevel(50, 50)).toBe(2);
    expect(calculateHealthActivityLevel(64, 36)).toBe(2);
    expect(calculateHealthActivityLevel(65, 35)).toBe(3);
    expect(calculateHealthActivityLevel(79, 21)).toBe(3);
  });

  it('uses light green from 80 percent to the sample-aware green threshold', () => {
    expect(calculateHealthActivityLevel(4, 1)).toBe(4);
    expect(calculateHealthActivityLevel(90, 10)).toBe(4);
    expect(calculateHealthActivityLevel(989, 11)).toBe(4);
    expect(calculateHealthActivityLevel(9, 1)).toBe(5);
    expect(calculateHealthActivityLevel(95, 5)).toBe(5);
    expect(calculateHealthActivityLevel(990, 10)).toBe(5);
  });

  it('renders idle plus five Health legend colors', () => {
    const html = renderToStaticMarkup(createElement(ServiceHealthCard, {
      activity: buildUsageActivityFixture(),
      loading: false,
      requestIdentity: 'admin::24h:::',
    }));

    expect(html.match(/data-health-level=/g)).toHaveLength(6);
  });
});
