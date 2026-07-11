import { describe, expect, it } from 'vitest';
import { buildStatCardMetrics } from './StatCards';
import type { UsageOverviewPayload } from './hooks/useUsageData';

const usageWithBackendSummary: UsageOverviewPayload = {
  usage: {
    total_requests: 9,
    success_count: 8,
    failure_count: 1,
    total_tokens: 900,
  },
  summary: {
    request_count: 3,
    token_count: 777,
    window_minutes: 120,
    rpm: 0.025,
    tpm: 6.475,
    total_cost: 1.234,
    cost_available: true,
    input_tokens: 220,
    cache_read_tokens: 22,
    cache_creation_tokens: 11,
    reasoning_tokens: 33,
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

describe('buildStatCardMetrics', () => {
  it('prefers backend summary values over detail-derived metrics', () => {
    const metrics = buildStatCardMetrics({
      usage: usageWithBackendSummary,
    });

    expect(metrics.rateStats.requestCount).toBe(3);
    expect(metrics.rateStats.tokenCount).toBe(777);
    expect(metrics.rateStats.windowMinutes).toBe(120);
    expect(metrics.rateStats.rpm).toBe(0.025);
    expect(metrics.rateStats.tpm).toBe(6.475);
    expect(metrics.requestStats.successRate).toBeCloseTo(88.8888888889);
    expect(metrics.tokenBreakdown.cacheReadTokens).toBe(22);
    expect(metrics.tokenBreakdown.cacheCreationTokens).toBe(11);
    expect(metrics.tokenBreakdown.reasoningTokens).toBe(33);
    expect(metrics.cacheReadRateStats.cacheReadRate).toBe(10);
    expect(metrics.cacheReadRateStats.inputTokens).toBe(220);
    expect(metrics.totalCost).toBe(1.234);
  });

  it('keeps cache rate empty when overview summary input tokens are missing', () => {
    const metrics = buildStatCardMetrics({
      usage: {
        ...usageWithBackendSummary,
        summary: {
          ...usageWithBackendSummary.summary!,
          input_tokens: 0,
        },
      },
    });

    expect(metrics.cacheReadRateStats.cacheReadRate).toBeNull();
    expect(metrics.cacheReadRateStats.inputTokens).toBe(0);
  });

  it('keeps success rate empty when total requests are missing', () => {
    const metrics = buildStatCardMetrics({
      usage: {
        ...usageWithBackendSummary,
        usage: {
          ...usageWithBackendSummary.usage,
          total_requests: 0,
          success_count: 3,
        },
      },
    });

    expect(metrics.requestStats.successRate).toBeNull();
  });

  it('keeps priced total cost visible when availability is partial', () => {
    const metrics = buildStatCardMetrics({
      usage: {
        ...usageWithBackendSummary,
        summary: {
          ...usageWithBackendSummary.summary!,
          total_cost: 4.56,
          cost_available: false,
        },
      },
    });

    expect(metrics.totalCost).toBe(4.56);
    expect(metrics.costAvailable).toBe(false);
  });
});
