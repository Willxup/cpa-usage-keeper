import { describe, expect, it } from 'vitest';
import { buildUsageSparklineSeries } from './useSparklines';
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
  });
});
