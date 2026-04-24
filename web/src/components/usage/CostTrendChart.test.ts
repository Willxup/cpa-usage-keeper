import { describe, expect, it } from 'vitest';
import { buildOverviewCostTrendSeries, shouldShowCostPricingHint } from './CostTrendChart';
import type { UsageOverviewResponse } from '@/lib/types';

const usageWithBackendCost: UsageOverviewResponse = {
  usage: {
    total_requests: 9,
    success_count: 8,
    failure_count: 1,
    total_tokens: 900,
    requests_by_day: {},
    requests_by_hour: {},
    tokens_by_day: {},
    tokens_by_hour: {},
    apis: {},
  },
  summary: {
    request_count: 9,
    token_count: 900,
    window_minutes: 1440,
    rpm: 0.5,
    tpm: 10,
    total_cost: 1.23,
    cost_available: true,
    cached_tokens: 0,
    reasoning_tokens: 0,
  },
  series: {
    requests: {
      '2026-04-22': 3,
    },
    tokens: {
      '2026-04-22': 300,
    },
    rpm: {
      '2026-04-22': 3 / 1440,
    },
    tpm: {
      '2026-04-22': 300 / 1440,
    },
    cost: {
      '2026-04-22': 0.46,
    },
    input_tokens: {},
    output_tokens: {},
    cached_tokens: {},
    reasoning_tokens: {},
  },
  hourly_series: {
    requests: {
      '2026-04-22T09:00:00Z': 1,
      '2026-04-22T10:00:00Z': 2,
    },
    tokens: {
      '2026-04-22T09:00:00Z': 100,
      '2026-04-22T10:00:00Z': 200,
    },
    rpm: {
      '2026-04-22T09:00:00Z': 1 / 60,
      '2026-04-22T10:00:00Z': 2 / 60,
    },
    tpm: {
      '2026-04-22T09:00:00Z': 100 / 60,
      '2026-04-22T10:00:00Z': 200 / 60,
    },
    cost: {
      '2026-04-22T09:00:00Z': 0.12,
      '2026-04-22T10:00:00Z': 0.34,
    },
    input_tokens: {},
    output_tokens: {},
    cached_tokens: {},
    reasoning_tokens: {},
  },
  daily_series: {
    requests: {
      '2026-04-22': 3,
    },
    tokens: {
      '2026-04-22': 300,
    },
    rpm: {
      '2026-04-22': 3 / 1440,
    },
    tpm: {
      '2026-04-22': 300 / 1440,
    },
    cost: {
      '2026-04-22': 0.46,
    },
    input_tokens: {},
    output_tokens: {},
    cached_tokens: {},
    reasoning_tokens: {},
  },
};

const usageWithMixedCostBuckets: UsageOverviewResponse = {
  ...usageWithBackendCost,
  summary: {
    ...usageWithBackendCost.summary!,
    window_minutes: 180,
  },
  series: {
    ...usageWithBackendCost.series!,
    cost: {
      '2026-04-22T09:00:00Z': 0.12,
      '2026-04-22T10:00:00Z': 0.34,
      '2026-04-22': 0.46,
    },
  },
};

const usageWithLongRangeHourlySeries: UsageOverviewResponse = {
  ...usageWithBackendCost,
  summary: {
    ...usageWithBackendCost.summary!,
    window_minutes: 7 * 24 * 60,
  },
  hourly_series: {
    ...(usageWithBackendCost.hourly_series ?? usageWithBackendCost.series!),
    cost: Object.fromEntries(
      Array.from({ length: 48 }, (_, index) => {
        const hour = String(index % 24).padStart(2, '0');
        const day = index < 24 ? '22' : '23';
        return [`2026-04-${day}T${hour}:00:00Z`, index + 1];
      }),
    ),
  },
};

const usageWithLongRangeDailySeries: UsageOverviewResponse = {
  ...usageWithBackendCost,
  summary: {
    ...usageWithBackendCost.summary!,
    window_minutes: 7 * 24 * 60,
  },
  series: {
    ...usageWithBackendCost.series!,
    cost: {
      '2026-04-17': 1,
      '2026-04-18': 2,
      '2026-04-19': 3,
      '2026-04-20': 4,
      '2026-04-21': 5,
      '2026-04-22': 6,
      '2026-04-23': 7,
    },
  },
};

describe('buildOverviewCostTrendSeries', () => {
  it('uses a 24-hour window and token-breakdown-style raw hour labels for backend cost series', () => {
    const result = buildOverviewCostTrendSeries({
      usage: usageWithBackendCost,
      period: 'hour',
      hourWindowHours: 24,
      endMs: Date.parse('2026-04-22T10:59:59Z'),
    });

    expect(result.costAvailable).toBe(true);
    expect(result.labels).toHaveLength(24);
    expect(result.labels[0]).toMatch(/^\d{2}:\d{2}$/);
    expect(result.labels[23]).toMatch(/^\d{2}:\d{2}$/);
    expect(result.data.slice(-2)).toEqual([0.12, 0.34]);
    expect(result.hasData).toBe(true);
  });

  it('keeps short-range hour view aligned to backend partial-hour buckets', () => {
    const result = buildOverviewCostTrendSeries({
      usage: {
        ...usageWithBackendCost,
        hourly_series: {
          ...usageWithBackendCost.hourly_series!,
          cost: {
            '2026-04-24T02:00:00Z': 1.47,
            '2026-04-24T03:00:00Z': 0,
            '2026-04-24T04:00:00Z': 0,
            '2026-04-24T05:00:00Z': 0,
            '2026-04-24T06:00:00Z': 0,
          },
        },
      },
      period: 'hour',
      hourWindowHours: 4,
      endMs: Date.parse('2026-04-24T06:16:00Z'),
    });

    expect(result.labels).toHaveLength(5);
    expect(result.data).toEqual([1.47, 0, 0, 0, 0]);
    expect(result.hasData).toBe(true);
  });

  it('aggregates hourly cost into daily buckets when day view is selected for a short range', () => {
    const result = buildOverviewCostTrendSeries({
      usage: usageWithMixedCostBuckets,
      period: 'day',
      hourWindowHours: 3,
      endMs: Date.parse('2026-04-22T10:59:59Z'),
    });

    expect(result.costAvailable).toBe(true);
    expect(result.labels).toEqual(['2026-04-22']);
    expect(result.data).toEqual([0.46]);
    expect(result.hasData).toBe(true);
  });

  it('limits hour view to the latest 24 hourly buckets for long ranges', () => {
    const result = buildOverviewCostTrendSeries({
      usage: usageWithLongRangeHourlySeries,
      period: 'hour',
      hourWindowHours: 24,
      endMs: Date.parse('2026-04-23T23:59:59Z'),
    });

    expect(result.costAvailable).toBe(true);
    expect(result.labels).toHaveLength(24);
    expect(result.labels[0]).toMatch(/^\d{2}:\d{2}$/);
    expect(result.labels[23]).toMatch(/^\d{2}:\d{2}$/);
    expect(result.data[0]).toBe(25);
    expect(result.data[23]).toBe(48);
    expect(result.hasData).toBe(true);
  });

  it('still returns cost data when cost availability is partial but backend series is populated', () => {
    const result = buildOverviewCostTrendSeries({
      usage: {
        ...usageWithLongRangeDailySeries,
        summary: {
          ...usageWithLongRangeDailySeries.summary!,
          cost_available: false,
        },
        hourly_series: {
          ...(usageWithLongRangeDailySeries.hourly_series ?? usageWithLongRangeDailySeries.series!),
          cost: {
            '2026-04-23T22:00:00Z': 1,
            '2026-04-23T23:00:00Z': 2,
          },
        },
      },
      period: 'hour',
      hourWindowHours: 24,
      endMs: Date.parse('2026-04-23T23:59:59Z'),
    });

    expect(result.costAvailable).toBe(false);
    expect(result.labels).toHaveLength(24);
    expect(result.data.slice(-2)).toEqual([1, 2]);
    expect(result.hasData).toBe(true);
  });

  it('does not show pricing setup hint when cost data is already present', () => {
    expect(shouldShowCostPricingHint({ costAvailable: false, hasData: true })).toBe(false);
    expect(shouldShowCostPricingHint({ costAvailable: false, hasData: false })).toBe(true);
  });
});
