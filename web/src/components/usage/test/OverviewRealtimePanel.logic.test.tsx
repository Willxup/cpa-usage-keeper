import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ChartData, ChartOptions, Plugin } from 'chart.js';
import type { OverviewRealtimeBlock } from '@/lib/types';
import i18n from '@/i18n';

const chartCapture = vi.hoisted(() => ({
  lineCalls: [] as Array<{ data: ChartData<'line', Array<number | null>, string>; options: ChartOptions<'line'>; plugins?: Plugin<'line'>[] }>,
  chartCalls: [] as Array<{ type?: string; data: ChartData; options: ChartOptions }>,
  scatterCalls: [] as Array<{ data: ChartData<'scatter'>; options: ChartOptions<'scatter'>; plugins?: Plugin<'scatter'>[] }>,
}));

vi.mock('react-chartjs-2', () => ({
  Line: (props: { data: ChartData<'line', Array<number | null>, string>; options: ChartOptions<'line'>; plugins?: Plugin<'line'>[] }) => {
    chartCapture.lineCalls.push(props);
    return React.createElement('div');
  },
  Scatter: (props: { data: ChartData<'scatter'>; options: ChartOptions<'scatter'>; plugins?: Plugin<'scatter'>[] }) => {
    chartCapture.scatterCalls.push(props);
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

import { OverviewRealtimePanel } from '../OverviewRealtimePanel';

const realtime: OverviewRealtimeBlock = {
  window: '15m',
  bucket_seconds: 30,
  window_start: '2026-06-09T11:55:00Z',
  window_end: '2026-06-09T12:10:00Z',
  token_velocity: [
    { bucket: '2026-06-09T11:55:00Z', tokens_per_minute: 120, tokens: 60, cost: 0.01 },
    { bucket: '2026-06-09T11:55:30Z', tokens_per_minute: 240, tokens: 120, cost: 0.02 },
  ],
  latency_scatter: {
    points: [{ ttft_ms: 120, latency_ms: 520 }, { ttft_ms: 230, latency_ms: 940 }],
    total_points: 2, p95_ttft_ms: 230, p95_latency_ms: 940, max_ttft_ms: 230, max_latency_ms: 940,
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
    { bucket: '2026-06-09T11:55:00Z', cache_read_rate: 25, cache_read_tokens: 10, cache_creation_tokens: 2, input_tokens: 40 },
    { bucket: '2026-06-09T11:55:30Z', cache_read_rate: 50, cache_read_tokens: 30, cache_creation_tokens: 4, input_tokens: 60 },
  ],
} as OverviewRealtimeBlock;

const realtimeWithProjectOffset: OverviewRealtimeBlock = {
  ...realtime,
  token_velocity: [
    { bucket: '2026-06-09T11:55:00+08:00', tokens_per_minute: 120, tokens: 60 },
    { bucket: '2026-06-09T11:55:30+08:00', tokens_per_minute: 240, tokens: 120 },
  ],
  request_level: [
    { bucket: '2026-06-09T11:55:00+08:00', requests_per_minute: 2, requests: 1 },
    { bucket: '2026-06-09T11:55:30+08:00', requests_per_minute: 4, requests: 2 },
  ],
};

describe('OverviewRealtimePanel', () => {
  afterEach(async () => {
    chartCapture.lineCalls = [];
    chartCapture.chartCalls = [];
    chartCapture.scatterCalls = [];
    await i18n.changeLanguage('en');
  });

  it('renders a dual-axis throughput chart without duplicating the request chart', () => {
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

    expect(html).toContain('usage_stats.overview_realtime_throughput');
    expect(html).toContain('usage_stats.overview_realtime_section_title');
    expect(html).toContain('usage_stats.analysis_latency_title');
    expect(html).toContain('usage_stats.analysis_latency_p95_ttft');
    expect(html).toContain('usage_stats.analysis_latency_p95_latency');
    expect(html).toContain('usage_stats.overview_realtime_current_usage');
    expect(html).toContain('usage_stats.overview_realtime_cache_level');
    expect(html).toContain('30m');
    expect(html).not.toMatch(/>5m<\/button>/);
    expect(html).toContain('usage_stats.overview_realtime_dimension_api_keys');
    expect(html).toContain('usage_stats.overview_realtime_dimension_auth_files');
    expect(html).toContain('gpt-5');
    expect(chartCapture.lineCalls).toHaveLength(1);
    expect(chartCapture.chartCalls).toHaveLength(1);
    expect(chartCapture.scatterCalls).toHaveLength(1);
    expect(chartCapture.lineCalls[0].data.datasets).toMatchObject([
      {
        label: 'usage_stats.overview_realtime_tpm',
        data: [120, 240],
        yAxisID: 'tokens',
        fill: true,
      },
      {
        label: 'usage_stats.overview_realtime_rpm',
        data: [2, 4],
        yAxisID: 'requests',
        fill: false,
        borderDash: [6, 4],
      },
    ]);
    expect(chartCapture.lineCalls[0].options).toMatchObject({
      plugins: {
        legend: {
          display: true,
          labels: {
            usePointStyle: true,
            pointStyle: 'line',
            pointStyleWidth: 32,
            generateLabels: expect.any(Function),
          },
        },
      },
      scales: {
        tokens: {
          position: 'left',
          beginAtZero: true,
          border: { color: 'rgba(17, 24, 39, 0.07)' },
          ticks: { count: 6 },
        },
        requests: {
          position: 'right',
          beginAtZero: true,
          max: 5,
          grid: { drawOnChartArea: false },
          border: { color: 'rgba(17, 24, 39, 0.07)' },
          ticks: { count: 6, precision: 0 },
        },
      },
    });
    const generateLabels = chartCapture.lineCalls[0].options.plugins?.legend?.labels?.generateLabels;
    const legendItems = generateLabels?.({
      data: chartCapture.lineCalls[0].data,
      isDatasetVisible: () => true,
    } as never);
    expect(legendItems).toMatchObject([
      { text: 'usage_stats.overview_realtime_tpm', lineDash: [], pointStyle: 'line' },
      { text: 'usage_stats.overview_realtime_rpm', lineDash: [6, 4], pointStyle: 'line' },
    ]);
    expect(chartCapture.lineCalls[0].plugins?.map((plugin) => plugin.id)).toContain('throughputLegendSpacing');
    expect(html).toContain('usage_stats.tpm');
    expect(html).toContain('usage_stats.rpm');
    expect(html).toContain('>usage_stats.overview_realtime_latest usage_stats.overview_realtime_tpm 240 usage_stats.overview_realtime_rpm 4 usage_stats.overview_realtime_throughput_hint</span>');
    expect(html).toContain('aria-hidden="true">usage_stats.overview_realtime_latest</span>');
    expect(chartCapture.scatterCalls[0].data.datasets[0].data).toEqual([{ x: 120, y: 520 }, { x: 230, y: 940 }]);
    expect(chartCapture.chartCalls[0].data.datasets[3].data).toEqual([25, 50]);
  });

  it('aligns throughput series by bucket when one response series has a missing point', () => {
    renderToStaticMarkup(
      <OverviewRealtimePanel
        realtime={{
          ...realtime,
          request_level: [
            { bucket: '2026-06-09T11:55:30Z', requests_per_minute: 4, requests: 2 },
          ],
        }}
        loading={false}
        window="15m"
        onWindowChange={() => {}}
        isDark={false}
        isMobile={false}
        timezone="UTC"
      />
    );

    expect(chartCapture.lineCalls[0].data.labels).toEqual(['11:55', '11:55:30']);
    expect(chartCapture.lineCalls[0].data.datasets[0].data).toEqual([120, 240]);
    expect(chartCapture.lineCalls[0].data.datasets[1].data).toEqual([null, 4]);
  });

  it('renders a continuous cache rate line across empty token buckets', () => {
    renderToStaticMarkup(
      <OverviewRealtimePanel
        realtime={{
          ...realtime,
          cache_level: [
            { bucket: '2026-06-09T11:55:00Z', cache_read_rate: 40, cache_read_tokens: 20, cache_creation_tokens: 5, input_tokens: 50 },
            { bucket: '2026-06-09T11:55:30Z', cache_read_rate: null, cache_read_tokens: 0, cache_creation_tokens: 0, input_tokens: 0 },
          ],
        }}
        loading={false}
        window="15m"
        onWindowChange={() => {}}
        isDark={false}
        isMobile={false}
      />
    );

    expect(chartCapture.chartCalls[0].data.datasets[3].data).toEqual([40, 0]);
  });

  it('shows an empty scatter with zero valid requests while keeping valid zero throughput', () => {
    const html = renderToStaticMarkup(<OverviewRealtimePanel
      realtime={{
        ...realtime,
        latency_scatter: { points: [], total_points: 0, p95_ttft_ms: 0, p95_latency_ms: 0, max_ttft_ms: 0, max_latency_ms: 0 },
        token_velocity: realtime.token_velocity.map((point) => ({ ...point, tokens_per_minute: 0, tokens: 0 })),
        request_level: realtime.request_level.map((point) => ({ ...point, requests_per_minute: 0, requests: 0 })),
        cache_level: realtime.cache_level.map((point) => ({ ...point, cache_read_rate: null, input_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0 })),
        current_usage: { models: [], api_keys: [], auth_files: [], ai_providers: [] },
      }}
      loading={false} window="15m" onWindowChange={() => {}} isDark={false} isMobile={false}
    />);
    expect(html).not.toContain('usage_stats.overview_realtime_throughput_empty');
    expect(html).toContain('usage_stats.no_data');
    expect(html).toContain('usage_stats.overview_realtime_cache_empty');
    expect(html).toContain('usage_stats.overview_realtime_usage_empty');
    expect(html).toContain('usage_stats.analysis_latency_samples_count');
    expect(chartCapture.lineCalls[0].data.datasets.map((dataset) => dataset.data)).toEqual([[0, 0], [0, 0]]);
    expect(chartCapture.chartCalls[0].data.datasets[3].data).toEqual([0, 0]);
    expect(chartCapture.scatterCalls[0].data.datasets[0].data).toEqual([]);
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

  it('renders request-paired scatter points with logarithmic axes, p95 lines and hover labels', () => {
    const html = renderToStaticMarkup(<OverviewRealtimePanel
      realtime={realtime} loading={false} window="15m" onWindowChange={() => {}}
      isDark={false} isMobile={false} timezone="UTC"
    />);
    expect(html).toContain('usage_stats.analysis_latency_title');
    expect(html).toContain('usage_stats.analysis_latency_samples_count');
    expect(html).toContain('230ms');
    expect(html).toContain('940ms');
    expect(html).not.toContain('usage_stats.analysis_latency_sampled');
    expect(chartCapture.scatterCalls).toHaveLength(1);
    const scatter = chartCapture.scatterCalls[0];
    expect(scatter.data.datasets[0].data).toEqual([{ x: 120, y: 520 }, { x: 230, y: 940 }]);
    expect(scatter.options.scales?.x?.type).toBe('logarithmic');
    expect(scatter.options.scales?.y?.type).toBe('logarithmic');
    expect((scatter.options.scales?.x as { max?: number }).max).toBeGreaterThan(230);
    expect((scatter.options.scales?.y as { max?: number }).max).toBeGreaterThan(940);
    expect(scatter.options.plugins?.tooltip?.callbacks?.label!({ parsed: { x: 120, y: 520 } } as never)).toEqual([
      'usage_stats.ttft: 120ms', 'usage_stats.latency: 520ms',
    ]);
    expect(scatter.plugins?.map((plugin) => plugin.id)).toContain('analysis-latency-diagnostics');
  });

  it('uses full-window maxima for scatter bounds even when plotted points are sampled', () => {
    renderToStaticMarkup(<OverviewRealtimePanel
      realtime={{ ...realtime, latency_scatter: {
        points: [{ ttft_ms: 120, latency_ms: 520 }], total_points: 1205,
        p95_ttft_ms: 2000, p95_latency_ms: 8000, max_ttft_ms: 5000, max_latency_ms: 30000,
      } }} loading={false} window="60m" onWindowChange={() => {}} isDark={true} isMobile={true}
    />);
    const scatter = chartCapture.scatterCalls[0];
    expect((scatter.options.scales?.x as { max?: number }).max).toBeGreaterThan(5000);
    expect((scatter.options.scales?.y as { max?: number }).max).toBeGreaterThan(30000);
    expect((scatter.options.plugins as { analysisLatencyDiagnostics?: { p95TTFTMS: number; p95LatencyMS: number } }).analysisLatencyDiagnostics).toMatchObject({
      p95TTFTMS: 2000, p95LatencyMS: 8000,
    });
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
    expect(chartCapture.lineCalls).toHaveLength(1);
    expect(chartCapture.chartCalls).toHaveLength(1);
    expect(chartCapture.scatterCalls).toHaveLength(1);
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

  it('formats realtime rate and latency summary values', () => {
    const html = renderToStaticMarkup(<OverviewRealtimePanel
      realtime={{ ...realtime,
        token_velocity: [
          { bucket: '2026-06-09T11:55:00Z', tokens_per_minute: 1000, tokens: 500 },
          { bucket: '2026-06-09T11:55:30Z', tokens_per_minute: 2000, tokens: 1000 },
        ],
        latency_scatter: {
        points: [{ ttft_ms: 900, latency_ms: 2500 }], total_points: 1205,
        p95_ttft_ms: 900, p95_latency_ms: 2500, max_ttft_ms: 900, max_latency_ms: 2500,
      } }} loading={false} window="15m" onWindowChange={() => {}} isDark={false} isMobile={false}
    />);
    expect(html).toContain('2.00K');
    expect(html).toContain('1.50K');
    expect(html).not.toContain('2.00K/min');
    expect(html).toContain('900ms');
    expect(html).toContain('2.5s');
    expect(html).toContain('1.21K');
  });

  it('keeps realtime duration units as ms and s in Chinese locales', async () => {
    await i18n.changeLanguage('zh-CN');
    const html = renderToStaticMarkup(<OverviewRealtimePanel realtime={realtime} loading={false} window="15m" onWindowChange={() => {}} isDark={false} isMobile={false} />);
    const scatter = chartCapture.scatterCalls[0];
    const yTicks = scatter.options.scales?.y?.ticks as { callback?: (value: number) => string };
    expect(html).toContain('940ms');
    expect(html).not.toContain('940毫秒');
    expect(yTicks.callback?.(900)).toBe('900ms');
    expect(yTicks.callback?.(2500)).toBe('2.5s');
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

  it('keeps cache chart independent of latency scatter settings', () => {
    renderToStaticMarkup(<OverviewRealtimePanel realtime={realtime} loading={false} window="15m" onWindowChange={() => {}} isDark={false} isMobile={false} />);
    expect(chartCapture.lineCalls[0].options.spanGaps).toBeUndefined();
    expect(chartCapture.chartCalls[0].options.spanGaps).toBeUndefined();
    expect((chartCapture.scatterCalls[0].options.scales?.x as { type?: string }).type).toBe('logarithmic');
    expect((chartCapture.chartCalls[0].options.scales?.y as { type?: string }).type).not.toBe('logarithmic');
  });

});
