import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ChartData, ChartOptions } from 'chart.js';
import type { OverviewRealtimeBlock } from '@/lib/types';
import i18n from '@/i18n';

const chartCapture = vi.hoisted(() => ({
  lineCalls: [] as Array<{ data: ChartData<'line', Array<number | null>, string>; options: ChartOptions<'line'> }>,
  chartCalls: [] as Array<{ type?: string; data: ChartData; options: ChartOptions }>,
}));

vi.mock('react-chartjs-2', () => ({
  Line: (props: { data: ChartData<'line', Array<number | null>, string>; options: ChartOptions<'line'> }) => {
    chartCapture.lineCalls.push(props);
    return React.createElement('div');
  },
  Chart: (props: { type?: string; data: ChartData; options: ChartOptions }) => {
    chartCapture.chartCalls.push(props);
    return React.createElement('div');
  },
}));

vi.mock('react-i18next', () => ({
  initReactI18next: {
    type: '3rdParty',
    init: () => {},
  },
  useTranslation: () => ({
    t: (key: string) => key,
  }),
}));

import { OverviewRealtimePanel } from './OverviewRealtimePanel';

const realtime: OverviewRealtimeBlock = {
  window: '15m',
  bucket_seconds: 30,
  window_start: '2026-06-09T11:55:00Z',
  window_end: '2026-06-09T12:10:00Z',
  token_velocity: [
    { bucket: '2026-06-09T11:55:00Z', tokens_per_minute: 120, tokens: 60, cost: 0.01 },
    { bucket: '2026-06-09T11:55:30Z', tokens_per_minute: 240, tokens: 120, cost: 0.02 },
  ],
  response_level: [
    { bucket: '2026-06-09T11:55:00Z', ttft_p95_ms: 180, latency_p95_ms: 720 },
    { bucket: '2026-06-09T11:55:30Z', ttft_p95_ms: 210, latency_p95_ms: 860 },
  ],
  response_distribution: {
    ttft: {
      average_line: [
        { bucket: '2026-06-09T11:55:00Z', avg_ms: 150 },
        { bucket: '2026-06-09T11:55:30Z', avg_ms: 190 },
      ],
      particles: [
        { bucket: '2026-06-09T11:55:00Z', timestamp: '2026-06-09T11:55:10Z', ms: 120, count: 2 },
        { bucket: '2026-06-09T11:55:30Z', timestamp: '2026-06-09T11:55:41Z', ms: 230, count: 5 },
      ],
    },
    latency: {
      average_line: [
        { bucket: '2026-06-09T11:55:00Z', avg_ms: 640 },
        { bucket: '2026-06-09T11:55:30Z', avg_ms: 780 },
      ],
      particles: [
        { bucket: '2026-06-09T11:55:00Z', timestamp: '2026-06-09T11:55:11Z', ms: 520, count: 3 },
        { bucket: '2026-06-09T11:55:30Z', timestamp: '2026-06-09T11:55:44Z', ms: 940, count: 6 },
      ],
    },
  },
  current_usage: {
    models: [{ key: 'gpt-5', label: 'gpt-5', tokens: 180, requests: 3, share: 72, cost: 0.03 }],
    api_keys: [{ key: '1', label: 'Team Key', tokens: 180, requests: 3, share: 72 }],
    auth_files: [{ key: 'auth-1', label: 'Claude Account', tokens: 45, requests: 1, share: 18 }],
    ai_providers: [{ key: 'provider-1', label: 'OpenAI Provider', tokens: 25, requests: 1, share: 10 }],
  },
  request_level: [
    { bucket: '2026-06-09T11:55:00Z', requests_per_minute: 2, requests: 1 },
    { bucket: '2026-06-09T11:55:30Z', requests_per_minute: 4, requests: 2 },
  ],
  cache_level: [
    { bucket: '2026-06-09T11:55:00Z', cache_rate: 25, cached_tokens: 10, input_tokens: 40 },
    { bucket: '2026-06-09T11:55:30Z', cache_rate: 50, cached_tokens: 30, input_tokens: 60 },
  ],
} as OverviewRealtimeBlock;

const realtimeWithProjectOffset: OverviewRealtimeBlock = {
  ...realtime,
  token_velocity: [
    { bucket: '2026-06-09T11:55:00+08:00', tokens_per_minute: 120, tokens: 60 },
    { bucket: '2026-06-09T11:55:30+08:00', tokens_per_minute: 240, tokens: 120 },
  ],
};

describe('OverviewRealtimePanel', () => {
  afterEach(async () => {
    chartCapture.lineCalls = [];
    chartCapture.chartCalls = [];
    await i18n.changeLanguage('en');
  });

  it('renders realtime chart layout with one full-width chart and two two-column rows', () => {
    const html = renderToStaticMarkup(
      <OverviewRealtimePanel
        realtime={realtime}
        loading={false}
        window="15m"
        onWindowChange={() => {}}
        isDark={false}
        isMobile={false}
      />
    );

    expect(html).toContain('usage_stats.overview_realtime_token_velocity');
    expect(html).toContain('usage_stats.overview_realtime_section_title');
    expect(html).toContain('usage_stats.overview_realtime_ttft_distribution');
    expect(html).toContain('usage_stats.overview_realtime_latency_distribution');
    expect(html).toContain('usage_stats.overview_realtime_current_usage');
    expect(html).toContain('usage_stats.overview_realtime_request_level');
    expect(html).toContain('usage_stats.overview_realtime_cache_level');
    expect(html).toContain('overviewRealtimeCardFull');
    expect(html).toContain('30m');
    expect(html).not.toMatch(/>5m<\/button>/);
    expect(html).toContain('usage_stats.overview_realtime_dimension_api_keys');
    expect(html).toContain('usage_stats.overview_realtime_dimension_auth_files');
    expect(html).toContain('gpt-5');
    expect(chartCapture.lineCalls).toHaveLength(3);
    expect(chartCapture.chartCalls).toHaveLength(2);
    expect(chartCapture.lineCalls[0].data.datasets[0].data).toEqual([120, 240]);
    expect(chartCapture.chartCalls[0].data.datasets.map((dataset) => dataset.label)).toEqual([
      'usage_stats.overview_realtime_ttft_average',
      'usage_stats.overview_realtime_ttft_distribution',
    ]);
    expect(chartCapture.chartCalls[1].data.datasets.map((dataset) => dataset.label)).toEqual([
      'usage_stats.overview_realtime_latency_average',
      'usage_stats.overview_realtime_latency_distribution',
    ]);
    expect(chartCapture.lineCalls[1].data.datasets[0].data).toEqual([2, 4]);
    expect(chartCapture.lineCalls[2].data.datasets[0].data).toEqual([25, 50]);
  });

  it('shows metric-specific empty states while keeping valid zero lines visible', () => {
    const html = renderToStaticMarkup(
      <OverviewRealtimePanel
        realtime={{
          ...realtime,
          token_velocity: [
            { bucket: '2026-06-09T11:55:00Z', tokens_per_minute: 0, tokens: 0 },
            { bucket: '2026-06-09T11:55:30Z', tokens_per_minute: 0, tokens: 0 },
          ],
          response_level: [],
          response_distribution: {
            ttft: { average_line: [], particles: [] },
            latency: { average_line: [], particles: [] },
          },
          current_usage: {
            models: [],
            api_keys: [],
            auth_files: [],
            ai_providers: [],
          },
          request_level: [
            { bucket: '2026-06-09T11:55:00Z', requests_per_minute: 0, requests: 0 },
            { bucket: '2026-06-09T11:55:30Z', requests_per_minute: 0, requests: 0 },
          ],
          cache_level: [
            { bucket: '2026-06-09T11:55:00Z', cache_rate: null, cached_tokens: 0, input_tokens: 0 },
            { bucket: '2026-06-09T11:55:30Z', cache_rate: null, cached_tokens: 0, input_tokens: 0 },
          ],
        }}
        loading={false}
        window="15m"
        onWindowChange={() => {}}
        isDark={false}
        isMobile={false}
      />
    );

    expect(html).not.toContain('usage_stats.overview_realtime_token_empty');
    expect(html).not.toContain('usage_stats.overview_realtime_request_empty');
    expect(html).toContain('usage_stats.overview_realtime_ttft_empty');
    expect(html).toContain('usage_stats.overview_realtime_latency_empty');
    expect(html).toContain('usage_stats.overview_realtime_cache_empty');
    expect(html).toContain('usage_stats.overview_realtime_usage_empty');
    expect(chartCapture.lineCalls).toHaveLength(3);
    expect(chartCapture.chartCalls).toHaveLength(2);
  });

  it('labels realtime metric chips as rolling values with localized tooltip text', () => {
    const html = renderToStaticMarkup(
      <OverviewRealtimePanel
        realtime={realtime}
        loading={false}
        window="15m"
        onWindowChange={() => {}}
        isDark={false}
        isMobile={false}
      />
    );

    expect(html).toContain('title="usage_stats.overview_realtime_rolling_metric_hint"');
    expect(html).toContain('aria-label="usage_stats.overview_realtime_latest usage_stats.overview_realtime_rolling_metric_hint"');
  });

  it('renders token share metadata as labeled compact chips', () => {
    const html = renderToStaticMarkup(
      <OverviewRealtimePanel
        realtime={realtime}
        loading={false}
        window="15m"
        onWindowChange={() => {}}
        isDark={false}
        isMobile={false}
      />
    );

    expect(html).toContain('usage_stats.overview_realtime_tokens_label');
    expect(html).toContain('usage_stats.overview_realtime_requests_label');
    expect(html).toContain('usage_stats.overview_realtime_cost_label');
    expect(html).toContain('overviewRealtimeUsageMetaPill');
  });

  it('renders response level as separate TTFT and latency distribution cards', () => {
    const html = renderToStaticMarkup(
      <OverviewRealtimePanel
        realtime={realtime}
        loading={false}
        window="15m"
        onWindowChange={() => {}}
        isDark={false}
        isMobile={false}
        timezone="UTC"
      />
    );

    expect(html).toContain('usage_stats.overview_realtime_ttft_distribution');
    expect(html).toContain('usage_stats.overview_realtime_latency_distribution');
    expect(html).not.toContain('usage_stats.overview_realtime_response_level</h3>');
    expect(chartCapture.chartCalls).toHaveLength(2);
    expect(chartCapture.chartCalls[0].data.datasets.map((dataset) => dataset.label)).toEqual([
      'usage_stats.overview_realtime_ttft_average',
      'usage_stats.overview_realtime_ttft_distribution',
    ]);
    expect(chartCapture.chartCalls[0].data.datasets[0].data).toEqual([
      { x: Date.parse('2026-06-09T11:55:00Z'), y: 150 },
      { x: Date.parse('2026-06-09T11:55:30Z'), y: 190 },
    ]);
    expect(chartCapture.chartCalls[0].data.datasets[1].data).toEqual([
      { x: Date.parse('2026-06-09T11:55:10Z'), y: 120, count: 2 },
      { x: Date.parse('2026-06-09T11:55:41Z'), y: 230, count: 5 },
    ]);
    expect(chartCapture.chartCalls[1].data.datasets.map((dataset) => dataset.label)).toEqual([
      'usage_stats.overview_realtime_latency_average',
      'usage_stats.overview_realtime_latency_distribution',
    ]);
    const ttftXAxis = chartCapture.chartCalls[0].options.scales?.x as { type?: string; min?: number; max?: number };
    const latencyXAxis = chartCapture.chartCalls[1].options.scales?.x as { type?: string; min?: number; max?: number };
    expect(ttftXAxis.type).toBe('linear');
    expect(ttftXAxis.min).toBe(Date.parse('2026-06-09T11:55:00Z'));
    expect(ttftXAxis.max).toBe(Date.parse('2026-06-09T12:10:00Z'));
    expect(latencyXAxis.min).toBe(ttftXAxis.min);
    expect(latencyXAxis.max).toBe(ttftXAxis.max);
  });

  it('uses data-driven logarithmic response axes per distribution chart', () => {
    renderToStaticMarkup(
      <OverviewRealtimePanel
        realtime={{
          ...realtime,
          response_distribution: {
            ttft: {
              average_line: [
                { bucket: '2026-06-09T11:55:00Z', avg_ms: 1200 },
                { bucket: '2026-06-09T11:55:30Z', avg_ms: 5000 },
              ],
              particles: [
                { bucket: '2026-06-09T11:55:00Z', ms: 800, count: 1 },
                { bucket: '2026-06-09T11:55:30Z', ms: 30_000, count: 1 },
              ],
            },
            latency: {
              average_line: [
                { bucket: '2026-06-09T11:55:00Z', avg_ms: 500 },
                { bucket: '2026-06-09T11:55:30Z', avg_ms: 900 },
              ],
              particles: [
                { bucket: '2026-06-09T11:55:00Z', ms: 300, count: 1 },
                { bucket: '2026-06-09T11:55:30Z', ms: 1200, count: 1 },
              ],
            },
          },
        }}
        loading={false}
        window="15m"
        onWindowChange={() => {}}
        isDark={false}
        isMobile={false}
      />
    );

    const ttftYAxis = chartCapture.chartCalls[0].options.scales?.y as { type?: string; beginAtZero?: boolean; min?: number; max?: number };
    const latencyYAxis = chartCapture.chartCalls[1].options.scales?.y as { type?: string; beginAtZero?: boolean; min?: number; max?: number };

    expect(ttftYAxis.type).toBe('logarithmic');
    expect(ttftYAxis.beginAtZero).toBeUndefined();
    expect(ttftYAxis.min).toBeGreaterThan(0);
    expect(ttftYAxis.max).toBeGreaterThan(30_000);
    expect(latencyYAxis.type).toBe('logarithmic');
    expect(latencyYAxis.beginAtZero).toBeUndefined();
    expect(latencyYAxis.min).toBeGreaterThan(0);
    expect(latencyYAxis.max).toBeLessThan(2_000);
  });

  it('omits non-positive response values from logarithmic distribution charts', () => {
    const malformedRealtime = {
      ...realtime,
      response_distribution: {
        ttft: {
          average_line: [
            { bucket: '2026-06-09T11:55:00Z', avg_ms: 0 },
            { bucket: '2026-06-09T11:55:30Z', avg_ms: -5 },
            { bucket: '2026-06-09T11:56:00Z', avg_ms: 120 },
          ],
          particles: undefined,
        },
        latency: {
          average_line: [
            { bucket: '2026-06-09T11:55:00Z', avg_ms: 0 },
            { bucket: '2026-06-09T11:55:30Z', avg_ms: 800 },
          ],
          particles: [
            { bucket: '2026-06-09T11:55:00Z', ms: 0, count: 1 },
            { bucket: '2026-06-09T11:55:30Z', timestamp: '2026-06-09T11:55:42Z', ms: 900, count: 1 },
          ],
        },
      },
    } as unknown as OverviewRealtimeBlock;

    renderToStaticMarkup(
      <OverviewRealtimePanel
        realtime={malformedRealtime}
        loading={false}
        window="15m"
        onWindowChange={() => {}}
        isDark={false}
        isMobile={false}
        timezone="UTC"
      />
    );

    expect(chartCapture.chartCalls[0].data.datasets[0].data).toEqual([
      { x: Date.parse('2026-06-09T11:55:00Z'), y: null },
      { x: Date.parse('2026-06-09T11:55:30Z'), y: null },
      { x: Date.parse('2026-06-09T11:56:00Z'), y: 120 },
    ]);
    expect(chartCapture.chartCalls[0].data.datasets[1].data).toEqual([]);
    expect(chartCapture.chartCalls[1].data.datasets[0].data).toEqual([
      { x: Date.parse('2026-06-09T11:55:00Z'), y: null },
      { x: Date.parse('2026-06-09T11:55:30Z'), y: 800 },
    ]);
    expect(chartCapture.chartCalls[1].data.datasets[1].data).toEqual([
      { x: Date.parse('2026-06-09T11:55:42Z'), y: 900, count: 1 },
    ]);
  });

  it('keeps every response distribution particle visible', () => {
    const start = Date.parse('2026-06-09T11:40:00Z');
    const particles = Array.from({ length: 1_205 }, (_, index) => ({
      bucket: new Date(start + index * 1000).toISOString(),
      timestamp: new Date(start + index * 1000).toISOString(),
      ms: index + 1,
      count: 1,
    }));

    renderToStaticMarkup(
      <OverviewRealtimePanel
        realtime={{
          ...realtime,
          response_distribution: {
            ...realtime.response_distribution,
            ttft: {
              average_line: realtime.response_distribution.ttft.average_line,
              particles,
            },
          },
        }}
        loading={false}
        window="15m"
        onWindowChange={() => {}}
        isDark={false}
        isMobile={false}
      />
    );

    const ttftParticleData = chartCapture.chartCalls[0].data.datasets[1].data as Array<{ y: number }>;

    expect(ttftParticleData).toHaveLength(1_205);
    expect(ttftParticleData[0].y).toBe(1);
    expect(ttftParticleData[ttftParticleData.length - 1].y).toBe(1_205);
  });

  it('shows an error state before realtime data has loaded', () => {
    const html = renderToStaticMarkup(
      <OverviewRealtimePanel
        loading={false}
        error="Realtime failed"
        window="15m"
        onWindowChange={() => {}}
        isDark={false}
        isMobile={false}
      />
    );

    expect(html).toContain('Realtime failed');
    expect(chartCapture.lineCalls).toHaveLength(0);
  });

  it('keeps stale charts visible when a realtime refresh fails after data has loaded', () => {
    const html = renderToStaticMarkup(
      <OverviewRealtimePanel
        realtime={realtime}
        loading={false}
        error="Realtime failed"
        window="15m"
        onWindowChange={() => {}}
        isDark={false}
        isMobile={false}
      />
    );

    expect(html).toContain('Realtime failed');
    expect(chartCapture.lineCalls).toHaveLength(3);
    expect(chartCapture.chartCalls).toHaveLength(2);
  });

  it('shows a loading state before realtime data has loaded', () => {
    const html = renderToStaticMarkup(
      <OverviewRealtimePanel
        loading
        window="15m"
        onWindowChange={() => {}}
        isDark={false}
        isMobile={false}
      />
    );

    expect(html).toContain('common.loading');
    expect(html).not.toContain('usage_stats.overview_realtime_empty');
    expect(chartCapture.lineCalls).toHaveLength(0);
  });

  it('formats realtime metric chips with compact token rates and readable durations', () => {
    const formattedRealtime: OverviewRealtimeBlock = {
      ...realtime,
      token_velocity: [
        { bucket: '2026-06-09T11:55:00Z', tokens_per_minute: 1000, tokens: 500 },
        { bucket: '2026-06-09T11:55:30Z', tokens_per_minute: 2000, tokens: 1000 },
      ],
      response_level: [
        { bucket: '2026-06-09T11:55:00Z', ttft_p95_ms: 700, latency_p95_ms: 1500 },
        { bucket: '2026-06-09T11:55:30Z', ttft_p95_ms: 900, latency_p95_ms: 2500 },
      ],
      response_distribution: {
        ttft: {
          average_line: [
            { bucket: '2026-06-09T11:55:00Z', avg_ms: 700 },
            { bucket: '2026-06-09T11:55:30Z', avg_ms: 900 },
          ],
          particles: [],
        },
        latency: {
          average_line: [
            { bucket: '2026-06-09T11:55:00Z', avg_ms: 1500 },
            { bucket: '2026-06-09T11:55:30Z', avg_ms: 2500 },
          ],
          particles: [],
        },
      },
    };

    const html = renderToStaticMarkup(
      <OverviewRealtimePanel
        realtime={formattedRealtime}
        loading={false}
        window="15m"
        onWindowChange={() => {}}
        isDark={false}
        isMobile={false}
      />
    );

    expect(html).toContain('2.00K/min');
    expect(html).toContain('1.50K/min');
    expect(html).toContain('2.5s');
    expect(html).toContain('2s');
  });

  it('keeps realtime response duration units fixed in non-English locales', async () => {
    await i18n.changeLanguage('zh-CN');

    const formattedRealtime: OverviewRealtimeBlock = {
      ...realtime,
      response_level: [
        { bucket: '2026-06-09T11:55:00Z', ttft_p95_ms: 700, latency_p95_ms: 900 },
        { bucket: '2026-06-09T11:55:30Z', ttft_p95_ms: 900, latency_p95_ms: 2500 },
      ],
      response_distribution: {
        ttft: {
          average_line: [
            { bucket: '2026-06-09T11:55:00Z', avg_ms: 700 },
            { bucket: '2026-06-09T11:55:30Z', avg_ms: 900 },
          ],
          particles: [],
        },
        latency: {
          average_line: [
            { bucket: '2026-06-09T11:55:00Z', avg_ms: 900 },
            { bucket: '2026-06-09T11:55:30Z', avg_ms: 2500 },
          ],
          particles: [],
        },
      },
    };

    const html = renderToStaticMarkup(
      <OverviewRealtimePanel
        realtime={formattedRealtime}
        loading={false}
        window="15m"
        onWindowChange={() => {}}
        isDark={false}
        isMobile={false}
      />
    );
    const responseYAxis = chartCapture.chartCalls[1].options.scales?.y as { ticks?: { callback?: (value: string | number) => string } };

    expect(html).toContain('2.5s');
    expect(html).not.toContain('2.5秒');
    expect(responseYAxis.ticks?.callback?.(900)).toBe('900ms');
    expect(responseYAxis.ticks?.callback?.(2500)).toBe('2.5s');
  });

  it('shows only the Models current-usage dimension for key overview', () => {
    const html = renderToStaticMarkup(
      <OverviewRealtimePanel
        realtime={realtime}
        loading={false}
        window="15m"
        onWindowChange={() => {}}
        isDark={false}
        isMobile={false}
        visibleDimensions={['models'] as const}
      />
    );

    expect(html).not.toContain('usage_stats.overview_realtime_dimension_api_keys');
    expect(html).not.toContain('usage_stats.overview_realtime_dimension_auth_files');
    expect(html).not.toContain('usage_stats.overview_realtime_dimension_ai_providers');
    expect(html).not.toContain('Team Key');
    expect(html).toContain('usage_stats.overview_realtime_dimension_models');
  });

  it('does not render a nonzero usage bar for zero-share rows', () => {
    const html = renderToStaticMarkup(
      <OverviewRealtimePanel
        realtime={{
          ...realtime,
          current_usage: {
            ...realtime.current_usage,
            models: [{ key: 'zero', label: 'zero', tokens: 0, requests: 1, share: 0 }],
          },
        }}
        loading={false}
        window="15m"
        onWindowChange={() => {}}
        isDark={false}
        isMobile={false}
      />
    );

    expect(html).toContain('zero');
    expect(html).not.toContain('width:0%');
  });

  it('formats realtime bucket labels with the realtime response timezone', () => {
    renderToStaticMarkup(
      <OverviewRealtimePanel
        realtime={{ ...realtimeWithProjectOffset, timezone: 'Asia/Shanghai' }}
        loading={false}
        window="15m"
        onWindowChange={() => {}}
        isDark={false}
        isMobile={false}
      />
    );

    expect(chartCapture.lineCalls[0].data.labels).toEqual(['11:55', '11:55:30']);
  });

  it('keeps gap spanning disabled for realtime charts', () => {
    renderToStaticMarkup(
      <OverviewRealtimePanel
        realtime={realtime}
        loading={false}
        window="15m"
        onWindowChange={() => {}}
        isDark={false}
        isMobile={false}
      />
    );

    expect(chartCapture.lineCalls[0].options.spanGaps).toBeUndefined();
    expect(chartCapture.lineCalls[1].options.spanGaps).toBeUndefined();
    expect(chartCapture.lineCalls[2].options.spanGaps).toBeUndefined();
    expect(chartCapture.chartCalls[0].options.spanGaps).toBeUndefined();
    expect(chartCapture.chartCalls[1].options.spanGaps).toBeUndefined();
  });

  it('keeps response axis logarithmic and cache axis light', () => {
    renderToStaticMarkup(
      <OverviewRealtimePanel
        realtime={realtime}
        loading={false}
        window="15m"
        onWindowChange={() => {}}
        isDark={false}
        isMobile={false}
      />
    );

    const responseYAxis = chartCapture.chartCalls[0].options.scales?.y as { type?: string; beginAtZero?: boolean; min?: number; ticks?: { maxTicksLimit?: number } };
    const cacheYAxis = chartCapture.lineCalls[2].options.scales?.y as { ticks?: { maxTicksLimit?: number } };

    expect(responseYAxis.type).toBe('logarithmic');
    expect(responseYAxis.beginAtZero).toBeUndefined();
    expect(responseYAxis.min).toBeGreaterThan(0);
    expect(responseYAxis.ticks?.maxTicksLimit).toBe(5);
    expect(cacheYAxis.ticks?.maxTicksLimit).toBe(5);
  });
});
