import { describe, expect, it } from 'vitest';
import { SPARKLINE_COLORS, buildUsageSparklineSeries } from './useSparklines';
import type { UsagePayload } from './useUsageData';

const usageWithBackendSeries: UsagePayload = {
  total_requests: 9,
  success_count: 8,
  failure_count: 1,
  total_tokens: 900,
  requests_by_day: {},
  requests_by_hour: {},
  tokens_by_day: {},
  tokens_by_hour: {},
  series: {
    requests: {
      '2026-04-23T10:00:00Z': 2,
      '2026-04-23T11:00:00Z': 4,
    },
    tokens: {
      '2026-04-23T10:00:00Z': 200,
      '2026-04-23T11:00:00Z': 800,
    },
    rpm: {
      '2026-04-23T10:00:00Z': 2 / 60,
      '2026-04-23T11:00:00Z': 4 / 60,
    },
    tpm: {
      '2026-04-23T10:00:00Z': 200 / 60,
      '2026-04-23T11:00:00Z': 800 / 60,
    },
    cost: {
      '2026-04-23T10:00:00Z': 0.2,
      '2026-04-23T11:00:00Z': 0.8,
    },
    input_tokens: {
      '2026-04-23T10:00:00Z': 100,
      '2026-04-23T11:00:00Z': 400,
    },
    output_tokens: {},
    cached_tokens: {
      '2026-04-23T10:00:00Z': 25,
      '2026-04-23T11:00:00Z': 0,
    },
    reasoning_tokens: {},
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
    expect(series.cachedRate).toEqual([25, 0]);
  });

  it('keeps cache rate at zero when a bucket has no input tokens', () => {
    const series = buildUsageSparklineSeries({
      usage: {
        ...usageWithBackendSeries,
        series: {
          ...usageWithBackendSeries.series!,
          requests: {
            '2026-04-23T10:00:00Z': 1,
          },
          input_tokens: {
            '2026-04-23T10:00:00Z': 0,
          },
          cached_tokens: {
            '2026-04-23T10:00:00Z': 25,
          },
        },
      },
    });

    expect(series.cachedRate).toEqual([0]);
  });

  it('normalizes invalid sparkline series values to zero', () => {
    const invalidNumber = 'not-a-number' as unknown as number;
    const series = buildUsageSparklineSeries({
      usage: {
        ...usageWithBackendSeries,
        series: {
          ...usageWithBackendSeries.series!,
          requests: {
            '2026-04-23T10:00:00Z': invalidNumber,
          },
          tokens: {
            '2026-04-23T10:00:00Z': -4,
          },
          rpm: {
            '2026-04-23T10:00:00Z': Number.POSITIVE_INFINITY,
          },
          tpm: {
            '2026-04-23T10:00:00Z': Number.NaN,
          },
          cost: {
            '2026-04-23T10:00:00Z': invalidNumber,
          },
          input_tokens: {
            '2026-04-23T10:00:00Z': invalidNumber,
          },
          cached_tokens: {
            '2026-04-23T10:00:00Z': 25,
          },
        },
      },
    });

    expect(series.requests).toEqual([0]);
    expect(series.tokens).toEqual([0]);
    expect(series.rpm).toEqual([0]);
    expect(series.tpm).toEqual([0]);
    expect(series.cost).toEqual([0]);
    expect(series.cachedRate).toEqual([0]);
  });
});

describe('SPARKLINE_COLORS', () => {
  it('keeps the requests sparkline aligned with the Total Requests card accent', () => {
    expect(SPARKLINE_COLORS.requests.border).toBe('#3b82f6');
    expect(SPARKLINE_COLORS.requests.background).toBe('rgba(59, 130, 246, 0.18)');
  });
});
