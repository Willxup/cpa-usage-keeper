import '@/i18n';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { DailyAverageCard, buildDailyAverageMetrics } from '../DailyAverageCard';
import type { UsageOverviewPayload } from '../hooks/useUsageData';

const usageWithDailyAverages: UsageOverviewPayload = {
  usage: {
    total_requests: 461,
    success_count: 459,
    failure_count: 2,
    total_tokens: 52590000,
  },
  summary: {
    rpm: 0.0457,
    tpm: 5217.26,
    total_cost: 56.47,
    cost_available: false,
    input_tokens: 52340000,
    cache_read_tokens: 46850000,
    cache_creation_tokens: 0,
    reasoning_tokens: 97390,
    daily_average_requests: 65.9,
    daily_average_tokens: 7512857,
    daily_average_cost: 8.067,
    daily_average_range_days: 7,
  },
  series: {
    buckets: [],
    requests: [],
    tokens: [],
    rpm: [],
    tpm: [],
    cost: [],
    cache_read_rate: [],
  },
};

describe('buildDailyAverageMetrics', () => {
  it('uses backend daily average fields without deriving averages in the frontend', () => {
    expect(buildDailyAverageMetrics(usageWithDailyAverages)).toEqual({
      requests: 65.9,
      tokens: 7512857,
      cost: 8.067,
      rangeDays: 7,
      costAvailable: false,
    });
  });

  it('returns null when backend daily average fields are absent', () => {
    expect(buildDailyAverageMetrics({
      ...usageWithDailyAverages,
      summary: {
        ...usageWithDailyAverages.summary!,
        daily_average_requests: undefined,
        daily_average_tokens: undefined,
        daily_average_cost: undefined,
        daily_average_range_days: undefined,
      },
    })).toBeNull();
  });
});

describe('DailyAverageCard', () => {
  it('renders the three backend averages in one compact summary card', () => {
    const html = renderToStaticMarkup(<DailyAverageCard usage={usageWithDailyAverages} loading={false} />);

    expect(html).toContain('Daily Average');
    expect(html).toContain('Range 7 days');
    expect(html).toContain('Avg Requests');
    expect(html).toContain('65.9');
    expect(html).toContain('Avg Tokens');
    expect(html).toContain('7.51M');
    expect(html).toContain('Avg Cost');
    expect(html).toContain('$8.07');
    expect(html).toContain('Set pricing to calculate cost');
    expect(html).not.toContain('/day');
    expect(html.match(/<svg/g)).toHaveLength(3);
    expect(html.indexOf('Set pricing to calculate cost')).toBeGreaterThan(html.indexOf('Avg Cost'));
    expect(html.indexOf('Set pricing to calculate cost')).toBeLessThan(html.indexOf('$8.07'));
  });

  it('renders a loading shell before an eligible range returns its averages', () => {
    const html = renderToStaticMarkup(<DailyAverageCard usage={null} loading />);

    expect(html).toContain('Daily Average');
    expect(html).toContain('Avg Requests');
    expect(html).toContain('Avg Tokens');
    expect(html).toContain('Avg Cost');
    expect(html).not.toContain('Range ');
    expect(html).not.toContain('65.9');
  });
});
