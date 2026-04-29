import { afterEach, describe, expect, it, vi } from 'vitest';
import { filterUsageByWindow, filterUsageSnapshot, resolveUsageFilterWindow, sanitizeChartLines } from '@/utils/usage';
import type { UsageSnapshot } from '@/lib/types';

afterEach(() => {
  vi.useRealTimers();
});

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

describe('filterUsageByWindow', () => {
  it('rebuilds aggregate totals from only the details inside the selected time window', () => {
    const filtered = filterUsageByWindow(usage, {
      startMs: Date.parse('2026-04-23T01:00:00.000Z'),
      endMs: Date.parse('2026-04-23T03:00:00.000Z'),
      windowMinutes: 120,
    });

    expect(filtered.total_requests).toBe(1);
    expect(filtered.total_tokens).toBe(200);
    expect(filtered.apis['provider-a']?.total_requests).toBe(1);
    expect(filtered.apis['provider-a']?.total_tokens).toBe(200);
    expect(filtered.apis['provider-a']?.models['claude-sonnet']?.total_requests).toBe(1);
    expect(filtered.apis['provider-a']?.models['claude-sonnet']?.total_tokens).toBe(200);
  });
});

describe('filterUsageSnapshot', () => {
  it('filters today against the current UTC day instead of the latest event day', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-04-24T00:30:00.000Z'));

    const filtered = filterUsageSnapshot(usage, 'today');

    expect(filtered.total_requests).toBe(0);
    expect(filtered.total_tokens).toBe(0);
  });
});

describe('resolveUsageFilterWindow', () => {
  it('resolves today from UTC day start through the refresh anchor', () => {
    const window = resolveUsageFilterWindow(usage, 'today', {
      nowMs: Date.parse('2026-04-23T12:34:56.000Z'),
    });

    expect(window).toEqual({
      startMs: Date.parse('2026-04-23T00:00:00.000Z'),
      endMs: Date.parse('2026-04-23T12:34:56.000Z'),
      windowMinutes: (12 * 60) + 34 + (56 / 60),
    });
  });
});

describe('sanitizeChartLines', () => {
  it('falls back to all when persisted lines no longer exist in the current overview payload', () => {
    expect(sanitizeChartLines(['stale-model'], ['gpt-5.4', 'gpt-5.4-mini'])).toEqual(['all']);
  });
});
