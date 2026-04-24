import { describe, expect, it } from 'vitest';
import { filterUsageByWindow } from '@/utils/usage';
import type { UsageSnapshot } from '@/lib/types';

const usage: UsageSnapshot = {
  total_requests: 2,
  success_count: 2,
  failure_count: 0,
  total_tokens: 300,
  requests_by_day: {},
  requests_by_hour: {},
  tokens_by_day: {},
  tokens_by_hour: {},
  apis: {
    'provider-a': {
      display_name: 'Provider A',
      total_requests: 2,
      success_count: 2,
      failure_count: 0,
      total_tokens: 300,
      models: {
        'claude-sonnet': {
          total_requests: 2,
          success_count: 2,
          failure_count: 0,
          total_tokens: 300,
          details: [
            {
              timestamp: '2026-04-23T00:00:00.000Z',
              latency_ms: 100,
              source: 'source-a',
              auth_index: '1',
              failed: false,
              tokens: {
                input_tokens: 50,
                output_tokens: 50,
                reasoning_tokens: 0,
                cached_tokens: 0,
                total_tokens: 100,
              },
            },
            {
              timestamp: '2026-04-23T02:00:00.000Z',
              latency_ms: 120,
              source: 'source-a',
              auth_index: '1',
              failed: false,
              tokens: {
                input_tokens: 100,
                output_tokens: 100,
                reasoning_tokens: 0,
                cached_tokens: 0,
                total_tokens: 200,
              },
            },
          ],
        },
      },
    },
  },
};

describe('UsagePage filtering wiring', () => {
  it('should feed stat cards with range-filtered usage data', () => {
    const filterWindow = {
      startMs: Date.parse('2026-04-23T01:00:00.000Z'),
      endMs: Date.parse('2026-04-23T03:00:00.000Z'),
      windowMinutes: 120,
    };

    const filteredUsage = filterUsageByWindow(usage, filterWindow);

    expect(filteredUsage.total_requests).toBe(1);
    expect(filteredUsage.total_tokens).toBe(200);
    expect(filteredUsage).not.toBe(usage);
  });
});
