import { describe, expect, it } from 'vitest';
import { buildDailyAverageMetrics, formatDailyAverageCount, formatDailyAverageRangeDays } from './dailyAverage';
import type { UsageOverviewPayload } from './hooks/useUsageData';

const usageWithDailyAverages: UsageOverviewPayload = {
  usage: {
    total_requests: 461,
    success_count: 459,
    failure_count: 2,
    total_tokens: 52590000,
  },
  summary: {
    request_count: 461,
    token_count: 52590000,
    window_minutes: 10080,
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
    requests: {},
    tokens: {},
    rpm: {},
    tpm: {},
    cost: {},
    cache_read_rate: {},
  },
};

describe('daily average metrics', () => {
  it('uses backend daily average fields without deriving them in the frontend', () => {
    expect(buildDailyAverageMetrics(usageWithDailyAverages)).toEqual({
      requests: 65.9,
      tokens: 7512857,
      cost: 8.067,
      rangeDays: 7,
      costAvailable: false,
    });
  });

  it('returns null when any backend daily average field is absent', () => {
    expect(buildDailyAverageMetrics({
      ...usageWithDailyAverages,
      summary: {
        ...usageWithDailyAverages.summary!,
        daily_average_cost: undefined,
      },
    })).toBeNull();
  });

  it('formats compact card context without adding a separate panel suffix', () => {
    expect(formatDailyAverageCount(65.94)).toBe('65.9');
    expect(formatDailyAverageRangeDays(7)).toBe('7');
  });
});
