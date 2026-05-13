import { afterEach, describe, expect, it, vi } from 'vitest';
import { buildChartData, buildUsageFromDetails, calculateCost, filterUsageByWindow, filterUsageSnapshot, resolveUsageFilterWindow, sanitizeChartLines } from '@/utils/usage';
import type { UsageSnapshot } from '@/lib/types';

afterEach(() => {
  vi.useRealTimers();
});

const formatTestLocalDayKey = (date: Date): string => {
  const pad = (value: number) => String(value).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
};

const localDayKeyForBoundarySample = formatTestLocalDayKey(new Date('2026-04-22T16:30:00.000Z'));

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

describe('local day usage buckets', () => {
  it('rebuilds day aggregate keys from the browser local day', () => {
    const rebuilt = buildUsageFromDetails([
      {
        timestamp: '2026-04-22T16:30:00.000Z',
        latency_ms: 100,
        source: 'source-a',
        auth_index: '1',
        failed: false,
        tokens: {
          input_tokens: 1,
          output_tokens: 2,
          reasoning_tokens: 0,
          cached_tokens: 0,
          total_tokens: 3,
        },
        __apiName: 'provider-a',
        __apiDisplayName: 'Provider A',
        __modelName: 'claude-sonnet',
        __timestampMs: Date.parse('2026-04-22T16:30:00.000Z'),
      },
    ]);

    expect(rebuilt.requests_by_day).toEqual({ [localDayKeyForBoundarySample]: 1 });
    expect(rebuilt.tokens_by_day).toEqual({ [localDayKeyForBoundarySample]: 3 });
  });

  it('groups daily chart buckets by local day keys', () => {
    const chartData = buildChartData({
      total_requests: 1,
      success_count: 1,
      failure_count: 0,
      total_tokens: 3,
      requests_by_day: {},
      requests_by_hour: {},
      tokens_by_day: {},
      tokens_by_hour: {},
      apis: {
        'provider-a': {
          display_name: 'Provider A',
          total_requests: 1,
          success_count: 1,
          failure_count: 0,
          total_tokens: 3,
          models: {
            'claude-sonnet': {
              total_requests: 1,
              success_count: 1,
              failure_count: 0,
              total_tokens: 3,
              details: [{
                timestamp: '2026-04-22T16:30:00.000Z',
                latency_ms: 100,
                source: 'source-a',
                auth_index: '1',
                failed: false,
                tokens: {
                  input_tokens: 1,
                  output_tokens: 2,
                  reasoning_tokens: 0,
                  cached_tokens: 0,
                  total_tokens: 3,
                },
              }],
            },
          },
        },
      },
    }, 'day', 'requests', ['all']);

    expect(chartData.labels).toEqual([localDayKeyForBoundarySample]);
  });
});

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
  it('filters today against the current local day instead of the latest event day', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-04-24T00:30:00.000Z'));

    const filtered = filterUsageSnapshot(usage, 'today');

    expect(filtered.total_requests).toBe(0);
    expect(filtered.total_tokens).toBe(0);
  });
});

describe('resolveUsageFilterWindow', () => {
  it('resolves today from local day start through the refresh anchor', () => {
    const nowMs = Date.parse('2026-04-23T12:34:56.000Z');
    const expectedStart = new Date(nowMs);
    expectedStart.setHours(0, 0, 0, 0);

    const window = resolveUsageFilterWindow(usage, 'today', { nowMs });

    expect(window).toEqual({
      startMs: expectedStart.getTime(),
      endMs: nowMs,
      windowMinutes: Math.max((nowMs - expectedStart.getTime()) / 60000, 1),
    });
  });

  it('resolves 30d as a rolling thirty-day window', () => {
    const nowMs = Date.parse('2026-04-23T12:34:56.000Z');

    const window = resolveUsageFilterWindow(usage, '30d', { nowMs });

    expect(window).toEqual({
      startMs: nowMs - 30 * 24 * 60 * 60 * 1000,
      endMs: nowMs,
      windowMinutes: 30 * 24 * 60,
    });
  });
});

describe('sanitizeChartLines', () => {
  it('falls back to all when persisted lines no longer exist in the current overview payload', () => {
    expect(sanitizeChartLines(['stale-model'], ['gpt-5.4', 'gpt-5.4-mini'])).toEqual(['all']);
  });
});

describe('calculateCost', () => {
  const prices = { 'm': { prompt: 15, completion: 75, cache: 1.5 } };
  const baseDetail = {
    timestamp: '',
    source: '',
    auth_index: '',
    failed: false,
    latency_ms: 0,
    __modelName: 'm',
  };

  it('treats input as containing cached for OpenAI-style providers', () => {
    const cost = calculateCost(
      {
        ...baseDetail,
        source_type: 'openai',
        tokens: { input_tokens: 1000, output_tokens: 100, reasoning_tokens: 0, cached_tokens: 600, total_tokens: 1100 },
      },
      prices,
    );
    // promptTokens = 1000 - 600 = 400 → 400*15 + 100*75 + 600*1.5 = 6000+7500+900 = 14400 micro-units
    expect(cost).toBeCloseTo((400 * 15 + 100 * 75 + 600 * 1.5) / 1_000_000, 9);
  });

  it('treats input as excluding cached for Anthropic-style providers', () => {
    const cost = calculateCost(
      {
        ...baseDetail,
        source_type: 'claude',
        tokens: { input_tokens: 400, output_tokens: 100, reasoning_tokens: 0, cached_tokens: 600, total_tokens: 500 },
      },
      prices,
    );
    // promptTokens stays 400 (no subtraction) → same total as the OpenAI case for the same physical request
    expect(cost).toBeCloseTo((400 * 15 + 100 * 75 + 600 * 1.5) / 1_000_000, 9);
  });

  it('matches anthropic provider name case-insensitively', () => {
    const cost = calculateCost(
      {
        ...baseDetail,
        source_type: 'Anthropic',
        tokens: { input_tokens: 1, output_tokens: 0, reasoning_tokens: 0, cached_tokens: 100, total_tokens: 1 },
      },
      prices,
    );
    // 用 anthropic 公式：promptTokens=1，不会被减成 0
    expect(cost).toBeGreaterThan(0);
  });
});
