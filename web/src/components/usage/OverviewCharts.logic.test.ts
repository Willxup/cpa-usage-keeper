import { describe, expect, it } from 'vitest';
import { buildOverviewCostTrendSeries } from './CostTrendChart';
import { buildTokenBreakdownChartSeries } from './TokenBreakdownChart';
import { buildHourlyTokenBreakdown } from '@/utils/usage';
import { buildChartData, filterUsageByWindow } from '@/utils/usage';
import type { UsageOverviewResponse, UsageEvent, UsageSnapshot } from '@/lib/types';

const overviewUsage: UsageOverviewResponse = {
  usage: {
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
          },
        },
      },
    },
  },
  summary: {
    request_count: 2,
    token_count: 300,
    window_minutes: 180,
    rpm: 2 / 180,
    tpm: 300 / 180,
    total_cost: 1.23,
    cost_available: true,
    cached_tokens: 30,
    reasoning_tokens: 30,
  },
  series: {
    requests: {
      '2026-04-23': 2,
    },
    tokens: {
      '2026-04-23': 300,
    },
    rpm: {
      '2026-04-23': 2 / 1440,
    },
    tpm: {
      '2026-04-23': 300 / 1440,
    },
    cost: {
      '2026-04-23': 1.23,
    },
  },
  hourly_series: {
    requests: {
      '2026-04-23T00:00:00Z': 1,
      '2026-04-23T02:00:00Z': 1,
    },
    tokens: {
      '2026-04-23T00:00:00Z': 100,
      '2026-04-23T02:00:00Z': 200,
    },
    rpm: {
      '2026-04-23T00:00:00Z': 1 / 60,
      '2026-04-23T02:00:00Z': 1 / 60,
    },
    tpm: {
      '2026-04-23T00:00:00Z': 100 / 60,
      '2026-04-23T02:00:00Z': 200 / 60,
    },
    cost: {
      '2026-04-23T00:00:00Z': 0.45,
      '2026-04-23T02:00:00Z': 0.78,
    },
    input_tokens: {
      '2026-04-23T00:00:00Z': 60,
      '2026-04-23T02:00:00Z': 140,
    },
    output_tokens: {
      '2026-04-23T00:00:00Z': 40,
      '2026-04-23T02:00:00Z': 60,
    },
    cached_tokens: {
      '2026-04-23T00:00:00Z': 10,
      '2026-04-23T02:00:00Z': 20,
    },
    reasoning_tokens: {
      '2026-04-23T00:00:00Z': 10,
      '2026-04-23T02:00:00Z': 20,
    },
  },
  daily_series: {
    requests: {
      '2026-04-23': 2,
    },
    tokens: {
      '2026-04-23': 300,
    },
    rpm: {
      '2026-04-23': 2 / 1440,
    },
    tpm: {
      '2026-04-23': 300 / 1440,
    },
    cost: {
      '2026-04-23': 1.23,
    },
    input_tokens: {
      '2026-04-23': 200,
    },
    output_tokens: {
      '2026-04-23': 100,
    },
    cached_tokens: {
      '2026-04-23': 30,
    },
    reasoning_tokens: {
      '2026-04-23': 30,
    },
  },
};

const asyncEvents: UsageEvent[] = [
  {
    timestamp: '2026-04-23T02:00:00.000Z',
    model: 'claude-sonnet',
    source: 'source-a',
    auth_index: '1',
    failed: false,
    latency_ms: 120,
    tokens: {
      input_tokens: 100,
      output_tokens: 60,
      reasoning_tokens: 20,
      cached_tokens: 20,
      total_tokens: 200,
    },
  },
];

describe('overview chart data flow', () => {
  it('requests and tokens charts need the full overview payload to read explicit hourly and daily series', () => {
    const filterWindow = {
      startMs: Date.parse('2026-04-23T01:00:00.000Z'),
      endMs: Date.parse('2026-04-23T03:00:00.000Z'),
      windowMinutes: 120,
    };

    const filteredUsage = filterUsageByWindow(overviewUsage.usage as UsageSnapshot, filterWindow);

    const wrongRequests = buildChartData(filteredUsage, 'hour', 'requests', ['all'], {
      hourWindowHours: 24,
      endMs: Date.parse('2026-04-23T03:00:00.000Z'),
    });
    const correctRequests = buildChartData({
      ...overviewUsage.usage,
      requests_by_hour: overviewUsage.hourly_series?.requests ?? {},
      requests_by_day: overviewUsage.daily_series?.requests ?? {},
      tokens_by_hour: overviewUsage.hourly_series?.tokens ?? {},
      tokens_by_day: overviewUsage.daily_series?.tokens ?? {},
    }, 'hour', 'requests', ['all'], {
      hourWindowHours: 24,
      endMs: Date.parse('2026-04-23T03:00:00.000Z'),
    });
    const correctTokens = buildChartData({
      ...overviewUsage.usage,
      requests_by_hour: overviewUsage.hourly_series?.requests ?? {},
      requests_by_day: overviewUsage.daily_series?.requests ?? {},
      tokens_by_hour: overviewUsage.hourly_series?.tokens ?? {},
      tokens_by_day: overviewUsage.daily_series?.tokens ?? {},
    }, 'day', 'tokens', ['all'], {
      hourWindowHours: 24,
      endMs: Date.parse('2026-04-23T03:00:00.000Z'),
    });

    expect(wrongRequests.labels).toEqual([]);
    expect(correctRequests.labels).toHaveLength(24);
    expect(correctRequests.datasets[0]?.data.filter((value) => value > 0)).toEqual([1, 1]);
    expect(correctTokens.labels).toEqual(['2026-04-23']);
    expect(correctTokens.datasets[0]?.data).toEqual([300]);
  });

  it('keeps aggregate overview charts visible when all traffic is selected with extra model lines', () => {
    const chartUsage = {
      ...overviewUsage.usage,
      requests_by_hour: overviewUsage.hourly_series?.requests ?? {},
      requests_by_day: overviewUsage.daily_series?.requests ?? {},
      tokens_by_hour: overviewUsage.hourly_series?.tokens ?? {},
      tokens_by_day: overviewUsage.daily_series?.tokens ?? {},
    };

    const requests = buildChartData(chartUsage, 'hour', 'requests', ['all', 'claude-sonnet'], {
      hourWindowHours: 24,
      endMs: Date.parse('2026-04-23T03:00:00.000Z'),
    });
    const tokens = buildChartData(chartUsage, 'day', 'tokens', ['all', 'claude-sonnet'], {
      hourWindowHours: 24,
      endMs: Date.parse('2026-04-23T03:00:00.000Z'),
    });

    expect(requests.labels).toHaveLength(24);
    expect(requests.datasets[0]?.label).toBe('All');
    expect(requests.datasets[0]?.data.filter((value) => value > 0)).toEqual([1, 1]);
    expect(tokens.labels).toEqual(['2026-04-23']);
    expect(tokens.datasets[0]?.label).toBe('All');
    expect(tokens.datasets[0]?.data).toEqual([300]);
  });

  it('renders selected model lines from backend overview model series', () => {
    const chartUsage = {
      ...overviewUsage.usage,
      requests_by_hour: overviewUsage.hourly_series?.requests ?? {},
      requests_by_day: overviewUsage.daily_series?.requests ?? {},
      tokens_by_hour: overviewUsage.hourly_series?.tokens ?? {},
      tokens_by_day: overviewUsage.daily_series?.tokens ?? {},
      model_series: {
        'claude-sonnet': {
          requests_by_hour: {
            '2026-04-23T00:00:00Z': 1,
            '2026-04-23T02:00:00Z': 1,
          },
          requests_by_day: {
            '2026-04-23': 2,
          },
          tokens_by_hour: {
            '2026-04-23T00:00:00Z': 100,
            '2026-04-23T02:00:00Z': 200,
          },
          tokens_by_day: {
            '2026-04-23': 300,
          },
        },
      },
    };

    const requests = buildChartData(chartUsage, 'hour', 'requests', ['all', 'claude-sonnet'], {
      hourWindowHours: 24,
      endMs: Date.parse('2026-04-23T03:00:00.000Z'),
    });
    const tokens = buildChartData(chartUsage, 'day', 'tokens', ['all', 'claude-sonnet'], {
      hourWindowHours: 24,
      endMs: Date.parse('2026-04-23T03:00:00.000Z'),
    });

    expect(requests.datasets.map((dataset) => dataset.label)).toEqual(['All', 'claude-sonnet']);
    expect(requests.datasets[1]?.data.filter((value) => value > 0)).toEqual([1, 1]);
    expect(tokens.datasets.map((dataset) => dataset.label)).toEqual(['All', 'claude-sonnet']);
    expect(tokens.datasets[1]?.data).toEqual([300]);
  });

  it('renders a single selected model line without all traffic selected', () => {
    const chartUsage = {
      ...overviewUsage.usage,
      requests_by_hour: overviewUsage.hourly_series?.requests ?? {},
      requests_by_day: overviewUsage.daily_series?.requests ?? {},
      tokens_by_hour: overviewUsage.hourly_series?.tokens ?? {},
      tokens_by_day: overviewUsage.daily_series?.tokens ?? {},
      model_series: {
        'claude-sonnet': {
          requests_by_hour: {
            '2026-04-23T00:00:00Z': 1,
            '2026-04-23T02:00:00Z': 1,
          },
          requests_by_day: {
            '2026-04-23': 2,
          },
          tokens_by_hour: {
            '2026-04-23T00:00:00Z': 100,
            '2026-04-23T02:00:00Z': 200,
          },
          tokens_by_day: {
            '2026-04-23': 300,
          },
        },
      },
    };

    const requests = buildChartData(chartUsage, 'hour', 'requests', ['claude-sonnet'], {
      hourWindowHours: 24,
      endMs: Date.parse('2026-04-23T03:00:00.000Z'),
    });
    const tokens = buildChartData(chartUsage, 'day', 'tokens', ['claude-sonnet'], {
      hourWindowHours: 24,
      endMs: Date.parse('2026-04-23T03:00:00.000Z'),
    });

    expect(requests.datasets.map((dataset) => dataset.label)).toEqual(['claude-sonnet']);
    expect(requests.datasets[0]?.data.filter((value) => value > 0)).toEqual([1, 1]);
    expect(tokens.datasets.map((dataset) => dataset.label)).toEqual(['claude-sonnet']);
    expect(tokens.datasets[0]?.data).toEqual([300]);
  });

  it('token breakdown needs the async event-derived usage shape to show data on first render', () => {
    const withoutEvents = buildHourlyTokenBreakdown(overviewUsage.usage, 24, Date.parse('2026-04-23T03:00:00.000Z'));

    const usageWithAsyncEvents = {
      ...(overviewUsage.usage ?? {}),
      apis: {
        __overview__: {
          total_requests: asyncEvents.length,
          success_count: asyncEvents.length,
          failure_count: 0,
          total_tokens: 200,
          models: {
            __overview__: {
              total_requests: asyncEvents.length,
              success_count: asyncEvents.length,
              failure_count: 0,
              total_tokens: 200,
              details: [
                {
                  timestamp: asyncEvents[0].timestamp,
                  latency_ms: asyncEvents[0].latency_ms,
                  source: asyncEvents[0].source,
                  auth_index: asyncEvents[0].auth_index ?? '',
                  failed: false,
                  tokens: asyncEvents[0].tokens,
                },
              ],
            },
          },
        },
      },
    };

    const withEvents = buildHourlyTokenBreakdown(usageWithAsyncEvents, 24, Date.parse('2026-04-23T03:00:00.000Z'));

    expect(withoutEvents.labels).toEqual([]);
    expect(withEvents.labels.length).toBeGreaterThan(0);
    expect(withEvents.dataByCategory.input.some((value) => value > 0)).toBe(true);
  });

  it('keeps short-range overview hour charts aligned to backend partial-hour buckets', () => {
    const chartUsage = {
      ...overviewUsage.usage,
      requests_by_hour: {
        '2026-04-24T02:00:00Z': 34,
        '2026-04-24T03:00:00Z': 41,
        '2026-04-24T04:00:00Z': 9,
        '2026-04-24T05:00:00Z': 16,
        '2026-04-24T06:00:00Z': 93,
      },
      tokens_by_hour: {
        '2026-04-24T02:00:00Z': 3664982,
        '2026-04-24T03:00:00Z': 5003310,
        '2026-04-24T04:00:00Z': 1362696,
        '2026-04-24T05:00:00Z': 2583370,
        '2026-04-24T06:00:00Z': 6477989,
      },
    };

    const requests = buildChartData(chartUsage, 'hour', 'requests', ['all'], {
      hourWindowHours: 4,
      endMs: Date.parse('2026-04-24T06:16:00Z'),
    });
    const tokens = buildChartData(chartUsage, 'hour', 'tokens', ['all'], {
      hourWindowHours: 4,
      endMs: Date.parse('2026-04-24T06:16:00Z'),
    });

    expect(requests.labels).toHaveLength(5);
    expect(requests.datasets[0]?.data).toEqual([34, 41, 9, 16, 93]);
    expect(tokens.labels).toHaveLength(5);
    expect(tokens.datasets[0]?.data[0]).toBe(3664982);
  });

  it('fills empty hourly buckets between backend overview points', () => {
    const chartUsage = {
      ...overviewUsage.usage,
      requests_by_hour: {
        '2026-04-24T02:00:00Z': 1,
        '2026-04-24T04:00:00Z': 2,
      },
      tokens_by_hour: {
        '2026-04-24T02:00:00Z': 100,
        '2026-04-24T04:00:00Z': 200,
      },
    };

    const requests = buildChartData(chartUsage, 'hour', 'requests', ['all'], {
      hourWindowHours: 4,
      endMs: Date.parse('2026-04-24T04:16:00Z'),
    });
    const tokens = buildChartData(chartUsage, 'hour', 'tokens', ['all'], {
      hourWindowHours: 4,
      endMs: Date.parse('2026-04-24T04:16:00Z'),
    });

    expect(requests.labels).toHaveLength(5);
    expect(requests.datasets[0]?.data).toEqual([0, 0, 1, 0, 2]);
    expect(tokens.datasets[0]?.data).toEqual([0, 0, 100, 0, 200]);
  });

  it('fills token breakdown hour buckets across the latest 24 hours when only one bucket has data', () => {
    const series = buildTokenBreakdownChartSeries({
      usage: {
        ...overviewUsage,
        hourly_series: {
          ...overviewUsage.hourly_series!,
          input_tokens: {
            '2026-04-23T23:00:00Z': 100,
          },
          output_tokens: {
            '2026-04-23T23:00:00Z': 50,
          },
          cached_tokens: {},
          reasoning_tokens: {},
        },
      },
      period: 'hour',
      hourWindowHours: 24,
      endMs: Date.parse('2026-04-23T23:16:00Z'),
    });

    expect(series.labels).toHaveLength(24);
    expect(series.dataByCategory.input.slice(0, 23)).toEqual(Array(23).fill(0));
    expect(series.dataByCategory.input[23]).toBe(100);
    expect(series.dataByCategory.output[23]).toBe(50);
  });

  it('keeps overview hour charts capped to the latest 24 hours even when the query range is 7d', () => {
    const usageWithSevenDaysOfDetails = {
      ...overviewUsage.usage,
      apis: {
        __overview__: {
          total_requests: 48,
          success_count: 48,
          failure_count: 0,
          total_tokens: 4800,
          models: {
            __overview__: {
              total_requests: 48,
              success_count: 48,
              failure_count: 0,
              total_tokens: 4800,
              details: Array.from({ length: 48 }, (_, index) => ({
                timestamp: `2026-04-${index < 24 ? '22' : '23'}T${String(index % 24).padStart(2, '0')}:00:00.000Z`,
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
              })),
            },
          },
        },
      },
    };

    const requestsByHour = buildChartData(usageWithSevenDaysOfDetails, 'hour', 'requests', ['all'], {
      hourWindowHours: 168,
      endMs: Date.parse('2026-04-23T23:59:59Z'),
    });
    const tokenBreakdownByHour = buildHourlyTokenBreakdown(
      usageWithSevenDaysOfDetails,
      168,
      Date.parse('2026-04-23T23:59:59Z'),
    );
    const costTrendByHour = buildOverviewCostTrendSeries({
      usage: {
        ...overviewUsage,
        summary: {
          ...overviewUsage.summary!,
          window_minutes: 7 * 24 * 60,
        },
        series: {
          ...overviewUsage.series!,
          cost: Object.fromEntries(
            Array.from({ length: 48 }, (_, index) => [
              `2026-04-${index < 24 ? '22' : '23'}T${String(index % 24).padStart(2, '0')}:00:00Z`,
              index + 1,
            ]),
          ),
        },
      },
      period: 'hour',
      hourWindowHours: 168,
      endMs: Date.parse('2026-04-23T23:59:59Z'),
    });

    expect(requestsByHour.labels).toHaveLength(24);
    expect(tokenBreakdownByHour.labels).toHaveLength(24);
    expect(costTrendByHour.labels).toHaveLength(24);
  });
});
