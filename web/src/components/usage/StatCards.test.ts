import { describe, expect, it } from 'vitest';
import { buildStatCardMetrics } from './StatCards';
import type { UsagePayload } from './hooks/useUsageData';

const usageWithBackendSummary: UsagePayload = {
  total_requests: 9,
  success_count: 8,
  failure_count: 1,
  total_tokens: 900,
  requests_by_day: {},
  requests_by_hour: {},
  tokens_by_day: {},
  tokens_by_hour: {},
  apis: {
    'provider-a': {
      total_requests: 1,
      success_count: 1,
      failure_count: 0,
      total_tokens: 100,
      models: {
        'claude-sonnet': {
          total_requests: 1,
          success_count: 1,
          failure_count: 0,
          total_tokens: 100,
          details: [
            {
              timestamp: '2026-04-23T00:00:00.000Z',
              latency_ms: 100,
              source: 'source-a',
              auth_index: '1',
              failed: false,
              tokens: {
                input_tokens: 20,
                output_tokens: 30,
                reasoning_tokens: 4,
                cached_tokens: 5,
                total_tokens: 50,
              },
            },
          ],
        },
      },
    },
  },
  summary: {
    request_count: 3,
    token_count: 777,
    window_minutes: 120,
    rpm: 0.025,
    tpm: 6.475,
    total_cost: 1.234,
    cost_available: true,
    cached_tokens: 25,
    reasoning_tokens: 33,
  },
  series: {
    requests: {},
    tokens: {},
    rpm: {},
    tpm: {},
    cost: {},
    input_tokens: {
      '2026-04-23T00:00:00Z': 40,
      '2026-04-23T01:00:00Z': 60,
    },
    output_tokens: {},
    cached_tokens: {},
    reasoning_tokens: {},
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
    expect(metrics.tokenBreakdown.cachedTokens).toBe(25);
    expect(metrics.tokenBreakdown.reasoningTokens).toBe(33);
    expect(metrics.inputTokens).toBe(100);
    expect(metrics.cacheHitRate).toBe(25);
    expect(metrics.totalCost).toBe(1.234);
  });

  it('returns a zero cache hit rate when input tokens are zero', () => {
    const metrics = buildStatCardMetrics({
      usage: {
        ...usageWithBackendSummary,
        summary: {
          ...usageWithBackendSummary.summary!,
          cached_tokens: 10,
        },
        series: {
          ...usageWithBackendSummary.series!,
          input_tokens: {
            '2026-04-23T00:00:00Z': 0,
          },
        },
      },
    });

    expect(metrics.inputTokens).toBe(0);
    expect(metrics.cacheHitRate).toBe(0);
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
