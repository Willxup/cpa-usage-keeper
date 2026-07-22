import { describe, expect, it } from 'vitest';
import { SPARKLINE_COLORS, buildUsageSparklineSeries } from './useSparklines';
import type { UsageOverviewPayload } from './useUsageData';

const usageWithBackendSeries: UsageOverviewPayload = {
  usage: {
    total_requests: 9,
    success_count: 8,
    failure_count: 1,
    total_tokens: 900,
  },
  summary: {
    rpm: 0.075,
    tpm: 7.5,
    total_cost: 1,
    cost_available: true,
    input_tokens: 500,
    cache_read_tokens: 25,
    cache_creation_tokens: 5,
    reasoning_tokens: 0,
  },
  series: {
    buckets: ['2026-04-23T10:00:00Z', '2026-04-23T11:00:00Z'],
    requests: [2, 4],
    tokens: [200, 800],
    rpm: [2 / 60, 4 / 60],
    tpm: [200 / 60, 800 / 60],
    cost: [0.2, 0.8],
    cache_read_rate: [25, null],
  },
};

describe('buildUsageSparklineSeries', () => {
  it('prefers backend series over detail-derived fallback values', () => {
    const series = buildUsageSparklineSeries({
      usage: usageWithBackendSeries,
    });

    expect(series.labels).toEqual(['2026-04-23T10:00:00Z', '2026-04-23T11:00:00Z']);
    expect(series.requests).toEqual([2, 4]);
    expect(series.tokens).toEqual([200, 800]);
    expect(series.rpm).toEqual([2 / 60, 4 / 60]);
    expect(series.tpm).toEqual([200 / 60, 800 / 60]);
    expect(series.cost).toEqual([0.2, 0.8]);
    expect(series.cacheReadRate).toEqual([25, null]);
  });

  it('keeps cache rate empty when the backend omits a bucket cache rate', () => {
    const series = buildUsageSparklineSeries({
      usage: {
        ...usageWithBackendSeries,
        series: {
          ...usageWithBackendSeries.series!,
          buckets: ['2026-04-23T10:00:00Z'],
          requests: [1],
          cache_read_rate: [],
        },
      },
    });

    expect(series.cacheReadRate).toEqual([null]);
  });

  it('normalizes invalid sparkline series values to zero', () => {
    const invalidNumber = 'not-a-number' as unknown as number;
    const series = buildUsageSparklineSeries({
      usage: {
        ...usageWithBackendSeries,
        series: {
          ...usageWithBackendSeries.series!,
          buckets: ['2026-04-23T10:00:00Z'],
          requests: [invalidNumber],
          tokens: [-4],
          rpm: [Number.POSITIVE_INFINITY],
          tpm: [Number.NaN],
          cost: [invalidNumber],
          cache_read_rate: [invalidNumber],
        },
      },
    });

    expect(series.requests).toEqual([0]);
    expect(series.tokens).toEqual([0]);
    expect(series.rpm).toEqual([0]);
    expect(series.tpm).toEqual([0]);
    expect(series.cost).toEqual([0]);
    expect(series.cacheReadRate).toEqual([0]);
  });
});

describe('SPARKLINE_COLORS', () => {
  it('keeps the requests sparkline aligned with the Total Requests card accent', () => {
    expect(SPARKLINE_COLORS.requests.border).toBe('#3b82f6');
    expect(SPARKLINE_COLORS.requests.background).toBe('rgba(59, 130, 246, 0.18)');
  });
});
