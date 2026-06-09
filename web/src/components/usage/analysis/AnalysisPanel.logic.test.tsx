import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ChartData, ChartOptions, Plugin } from 'chart.js';
import type { AnalysisResponse } from '@/lib/types';

const chartCapture = vi.hoisted(() => ({
  barData: null as ChartData<'bar', Array<number | null>, string> | null,
  barOptions: null as ChartOptions<'bar'> | null,
  doughnutData: null as ChartData<'doughnut', number[], string> | null,
  doughnutCount: 0,
  scatterData: [] as ChartData<'scatter'>[],
  scatterOptions: [] as ChartOptions<'scatter'>[],
  scatterPlugins: [] as Array<Plugin<'scatter'>[] | undefined>,
}));

vi.mock('react-chartjs-2', () => ({
  Bar: (props: { data: ChartData<'bar', Array<number | null>, string>; options: ChartOptions<'bar'> }) => {
    chartCapture.barData = props.data;
    chartCapture.barOptions = props.options;
    return React.createElement('div');
  },
  Doughnut: (props: { data: ChartData<'doughnut', number[], string> }) => {
    chartCapture.doughnutData = props.data;
    chartCapture.doughnutCount += 1;
    return React.createElement('div');
  },
  Scatter: (props: { data: ChartData<'scatter'>; options: ChartOptions<'scatter'>; plugins?: Plugin<'scatter'>[] }) => {
    chartCapture.scatterData.push(props.data);
    chartCapture.scatterOptions.push(props.options);
    chartCapture.scatterPlugins.push(props.plugins);
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

import { AnalysisPanel } from './AnalysisPanel';

type FakeElement = {
  tagName: string;
  id: string;
  className: string;
  textContent: string;
  style: Record<string, string>;
  children: FakeElement[];
  offsetWidth: number;
  offsetHeight: number;
  appendChild: (child: FakeElement) => FakeElement;
  replaceChildren: (...children: FakeElement[]) => void;
  remove: () => void;
};

function createFakeElement(tagName: string, elements: Map<string, FakeElement>): FakeElement {
  const element: FakeElement = {
    tagName,
    id: '',
    className: '',
    textContent: '',
    style: {},
    children: [],
    offsetWidth: 260,
    offsetHeight: 160,
    appendChild(child) {
      this.children.push(child);
      if (child.id) {
        elements.set(child.id, child);
      }
      return child;
    },
    replaceChildren(...children) {
      this.children = children;
    },
    remove() {
      if (this.id) {
        elements.delete(this.id);
      }
    },
  };
  return element;
}

function createFakeDocument(elements: Map<string, FakeElement>) {
  return {
    body: createFakeElement('body', elements),
    createElement: (tagName: string) => createFakeElement(tagName, elements),
    getElementById: (id: string) => elements.get(id) ?? null,
  };
}

function collectFakeText(element: FakeElement | undefined): string[] {
  if (!element) return [];
  return [
    ...(element.textContent ? [element.textContent] : []),
    ...element.children.flatMap((child) => collectFakeText(child)),
  ];
}

const emptyAnalysis: AnalysisResponse = {
  granularity: 'hourly',
  timezone: 'UTC',
  token_usage: [],
  api_key_composition: [],
  model_composition: [],
  auth_files_composition: [],
  ai_provider_composition: [],
  cost_breakdown: {
    input_cost_usd: 0,
    output_cost_usd: 0,
    cached_cost_usd: 0,
    total_cost_usd: 0,
    cost_available: true,
  },
  model_efficiency: [],
  heatmap: {
    api_keys: [],
    api_key_labels: {},
    models: [],
    cells: [],
  },
  latency_diagnostics: {
    points: [],
    density: [],
    total_points: 0,
    sampled: false,
    p95_ttft_ms: 0,
    p95_latency_ms: 0,
    max_ttft_ms: 0,
    max_latency_ms: 0,
  },
};

describe('AnalysisPanel token chart data', () => {
  beforeEach(() => {
    chartCapture.barData = null;
    chartCapture.barOptions = null;
    chartCapture.doughnutData = null;
    chartCapture.doughnutCount = 0;
    chartCapture.scatterData = [];
    chartCapture.scatterOptions = [];
    chartCapture.scatterPlugins = [];
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('subtracts cached and reasoning tokens from displayed token series while keeping total tooltip values', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      token_usage: [{
        bucket: '2026-05-28T01:00:00Z',
        input_tokens: 1000,
        output_tokens: 100,
        cached_tokens: 600,
        reasoning_tokens: 50,
        total_tokens: 1150,
        requests: 3,
        cost_usd: 0.0123,
        cost_available: true,
      }],
    };

    renderToStaticMarkup(<AnalysisPanel analysis={analysis} loading={false} isDark={false} isMobile={false} />);

    const datasets = chartCapture.barData?.datasets ?? [];
    expect(datasets.find((dataset) => dataset.label === 'usage_stats.input_tokens')?.data).toEqual([400]);
    expect(datasets.find((dataset) => dataset.label === 'usage_stats.cached_tokens')?.data).toEqual([600]);
    expect(datasets.find((dataset) => dataset.label === 'usage_stats.output_tokens')?.data).toEqual([50]);
    expect(datasets.find((dataset) => dataset.label === 'usage_stats.reasoning_tokens')?.data).toEqual([50]);
    expect(datasets.find((dataset) => dataset.label === 'usage_stats.total_cost')?.data).toEqual([0.0123]);
    expect(datasets.find((dataset) => dataset.label === 'usage_stats.total_cost')?.yAxisID).toBe('cost');
    expect(datasets.find((dataset) => dataset.label === 'usage_stats.total_cost')?.borderColor).toBe('#14b8a6');
    expect(chartCapture.barOptions?.scales).toHaveProperty('cost');
    expect(chartCapture.barOptions?.scales?.cost?.ticks?.color).not.toBe('#14b8a6');
    const tooltipLabel = chartCapture.barOptions?.plugins?.tooltip?.callbacks?.label;
    expect(typeof tooltipLabel).toBe('function');
    expect(tooltipLabel?.({
      dataset: { label: 'usage_stats.input_tokens', tooltipData: [1000] },
      dataIndex: 0,
      parsed: { y: 400 },
    } as never)).toBe('usage_stats.input_tokens: 1.00K');
    expect(tooltipLabel?.({
      dataset: { label: 'usage_stats.output_tokens', tooltipData: [100] },
      dataIndex: 0,
      parsed: { y: 50 },
    } as never)).toBe('usage_stats.output_tokens: 100');
    expect(tooltipLabel?.({
      dataset: null,
      dataIndex: 0,
      parsed: { y: 125 },
    } as never)).toBe('125');
    const tooltipFooter = chartCapture.barOptions?.plugins?.tooltip?.callbacks?.footer;
    expect(typeof tooltipFooter).toBe('function');
    expect(tooltipFooter?.([{ dataIndex: 0 }] as never)).toBe('usage_stats.total_tokens: 1.15K');
  });

  it('replaces the four composition cards with one tabbed composition table', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      api_key_composition: [{
        key: '1',
        label: 'Primary Key',
        total_tokens: 1000,
        requests: 4,
        percent: 100,
        input_tokens: 700,
        output_tokens: 200,
        cached_tokens: 50,
        reasoning_tokens: 50,
        cost_usd: 0.42,
        cost_available: true,
      }],
      model_composition: [{
        key: 'gpt-4o',
        label: 'gpt-4o',
        total_tokens: 1000,
        requests: 4,
        percent: 100,
        input_tokens: 700,
        output_tokens: 200,
        cached_tokens: 50,
        reasoning_tokens: 50,
        cost_usd: 0.42,
        cost_available: true,
      }],
    };

    chartCapture.doughnutCount = 0;
    const markup = renderToStaticMarkup(<AnalysisPanel analysis={analysis} loading={false} isDark={false} isMobile={false} />);

    expect(chartCapture.doughnutCount).toBe(1);
    expect(chartCapture.doughnutData?.labels).toEqual(['Primary Key']);
    expect(chartCapture.doughnutData?.datasets[0]?.data).toEqual([1000]);
    expect(markup).toContain('usage_stats.analysis_composition_title');
    expect(markup).toContain('usage_stats.analysis_composition_api_key_tab');
    expect(markup).toContain('usage_stats.analysis_composition_token_percent');
    expect(markup).toContain('Primary Key');
    expect(markup).not.toContain('gpt-4o');
    expect(markup).not.toContain('usage_stats.analysis_model_composition_title');
    expect(markup).not.toContain('usage_stats.analysis_auth_files_composition_title');
    expect(markup).not.toContain('usage_stats.analysis_ai_provider_composition_title');
  });

  it('renders latency diagnostics scatter before usage distribution', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      latency_diagnostics: {
        total_points: 3,
        sampled: false,
        p95_ttft_ms: 300,
        p95_latency_ms: 1400,
        max_ttft_ms: 900,
        max_latency_ms: 3600,
        points: [
          { ttft_ms: 120, latency_ms: 800 },
          { ttft_ms: 300, latency_ms: 1400 },
          { ttft_ms: 900, latency_ms: 3600 },
        ],
        density: [{
          ttft_min_ms: 0,
          ttft_max_ms: 400,
          latency_min_ms: 0,
          latency_max_ms: 1800,
          count: 2,
          intensity: 1,
        }],
      },
    };

    const markup = renderToStaticMarkup(<AnalysisPanel analysis={analysis} loading={false} isDark={false} isMobile={false} />);

    expect(markup).toContain('usage_stats.analysis_latency_title');
    expect(markup.indexOf('usage_stats.analysis_latency_title')).toBeLessThan(markup.indexOf('usage_stats.analysis_composition_title'));
    const latencyScatterIndex = chartCapture.scatterData.findIndex((data) => data.datasets[0]?.label === 'usage_stats.analysis_latency_samples');
    expect(latencyScatterIndex).toBeGreaterThanOrEqual(0);
    const latencyScatterData = chartCapture.scatterData[latencyScatterIndex];
    const latencyScatterOptions = chartCapture.scatterOptions[latencyScatterIndex];
    expect(latencyScatterData.datasets[0]?.data[0]).toMatchObject({ x: 120, y: 800 });
    expect(latencyScatterData.datasets[0]?.pointRadius).toBe(3);
    expect(latencyScatterData.datasets[0]?.pointBackgroundColor).toBe('rgba(45, 212, 191, 0.62)');
    expect(latencyScatterData.datasets[0]?.pointBorderColor).toBe('transparent');
    expect(latencyScatterData.datasets[0]?.pointBorderWidth).toBe(0);
    expect(latencyScatterData.datasets[0]?.borderWidth).toBe(0);
    expect(latencyScatterOptions.scales?.x?.type).toBe('logarithmic');
    expect(latencyScatterOptions.scales?.y?.type).toBe('logarithmic');
    expect((latencyScatterOptions.scales?.x as { min?: number }).min).toBeGreaterThan(0);
    expect((latencyScatterOptions.scales?.y as { min?: number }).min).toBeGreaterThan(0);
    expect(latencyScatterOptions.scales?.x?.title?.text).toBe('usage_stats.ttft');
    expect(latencyScatterOptions.scales?.y?.title?.text).toBe('usage_stats.latency');
    expect(latencyScatterOptions.plugins?.tooltip?.callbacks?.label?.({
      parsed: { x: 120, y: 800 },
    } as never)).toEqual([
      'usage_stats.ttft: 120ms',
      'usage_stats.latency: 800ms',
    ]);
    expect(chartCapture.scatterPlugins[latencyScatterIndex]?.map((plugin) => plugin.id)).toContain('analysis-latency-diagnostics');
    const latencyPlugin = chartCapture.scatterPlugins[latencyScatterIndex]?.find((plugin) => plugin.id === 'analysis-latency-diagnostics');
    expect(markup).toContain('usage_stats.analysis_latency_p95_ttft');
    expect(markup).toContain('300ms');
    expect(markup).toContain('usage_stats.analysis_latency_p95_latency');
    expect(markup).toContain('1.4s');
    expect(markup).toContain('usage_stats.analysis_latency_samples_count');
    const latencyPluginOptions = (latencyScatterOptions.plugins as {
      analysisLatencyDiagnostics?: {
        labels?: {
          p95TTFT?: string;
          p95Latency?: string;
        };
        colors?: {
          point?: string;
          pointFill?: string;
          p95TTFT?: string;
          p95Latency?: string;
        };
      };
    }).analysisLatencyDiagnostics;
    expect(markup).not.toContain('usage_stats.analysis_latency_density');
    expect(markup).not.toContain('usage_stats.analysis_latency_density_low');
    expect(markup).not.toContain('usage_stats.analysis_latency_density_high');
    expect(markup).not.toContain('usage_stats.analysis_latency_dots_hint');
    expect(latencyPluginOptions?.labels).toMatchObject({
      p95TTFT: 'usage_stats.analysis_latency_p95_ttft',
      p95Latency: 'usage_stats.analysis_latency_p95_latency',
    });
    expect(latencyPluginOptions).not.toHaveProperty('visualStyle');
    expect(latencyPluginOptions).not.toHaveProperty('density');
    expect(latencyPluginOptions).not.toHaveProperty('isDark');
    expect(latencyPluginOptions?.labels).not.toHaveProperty('equalLine');
    expect(latencyPluginOptions?.labels).not.toHaveProperty('fastArea');
    expect(latencyPluginOptions?.labels).not.toHaveProperty('longCompletionArea');
    expect(latencyPluginOptions?.labels).not.toHaveProperty('slowFirstTokenArea');
    expect(latencyPluginOptions?.colors).toMatchObject({
      point: '#14b8a6',
      pointFill: 'rgba(45, 212, 191, 0.62)',
      p95TTFT: '#38bdf8',
      p95Latency: '#fb7185',
    });
    expect(latencyPluginOptions?.colors).not.toHaveProperty('fastZone');
    expect(latencyPluginOptions?.colors).not.toHaveProperty('longCompletionZone');
    expect(latencyPluginOptions?.colors).not.toHaveProperty('slowFirstTokenZone');
    expect(latencyPluginOptions?.colors).not.toHaveProperty('densityCloud');

    const fakeCanvas = { style: {} as Record<string, string>, title: '' };
    const lineStrokes: Array<{ lineWidth: number; strokeStyle: string; dash: number[] }> = [];
    let currentLineWidth = 1;
    let currentStrokeStyle = '';
    let currentDash: number[] = [];
    const fakeCtx = {
      save: vi.fn(),
      restore: vi.fn(),
      setLineDash: vi.fn((dash: number[]) => {
        currentDash = dash;
      }),
      beginPath: vi.fn(),
      moveTo: vi.fn(),
      lineTo: vi.fn(),
      stroke: vi.fn(() => {
        lineStrokes.push({
          lineWidth: currentLineWidth,
          strokeStyle: currentStrokeStyle,
          dash: [...currentDash],
        });
      }),
      fillText: vi.fn(),
      measureText: vi.fn((text: string) => ({ width: text.length * 6 })),
      fillRect: vi.fn(),
      strokeRect: vi.fn(),
      fillStyle: '',
      font: '',
      textAlign: '',
      textBaseline: '',
      set lineWidth(value: number) {
        currentLineWidth = value;
      },
      get lineWidth() {
        return currentLineWidth;
      },
      set strokeStyle(value: string) {
        currentStrokeStyle = value;
      },
      get strokeStyle() {
        return currentStrokeStyle;
      },
    };
    const fakeChart = {
      options: latencyScatterOptions,
      chartArea: { left: 10, right: 500, top: 20, bottom: 300 },
      ctx: fakeCtx,
      canvas: fakeCanvas,
      scales: {
        x: { getPixelForValue: (value: number) => (value === 300 ? 120 : 20) },
        y: { getPixelForValue: (value: number) => (value === 1400 ? 80 : 280) },
      },
    };
    const ttftHoverArgs = {
      event: { type: 'mousemove', x: 124, y: 100, native: null },
      replay: false,
      cancelable: false,
      inChartArea: true,
      changed: false,
    };
    latencyPlugin?.afterEvent?.(fakeChart as never, ttftHoverArgs as never, {} as never);
    expect(ttftHoverArgs.changed).toBe(true);
    expect(fakeCanvas.style.cursor).toBe('');
    expect(fakeCanvas.title).toBe('');
    latencyPlugin?.afterDatasetsDraw?.(fakeChart as never, {} as never, {} as never);
    expect(lineStrokes.some((stroke) => stroke.strokeStyle === '#38bdf8' && stroke.lineWidth > 1.4)).toBe(true);

    const latencyHoverArgs = {
      event: { type: 'mousemove', x: 260, y: 84, native: null },
      replay: false,
      cancelable: false,
      inChartArea: true,
      changed: false,
    };
    lineStrokes.length = 0;
    latencyPlugin?.afterEvent?.(fakeChart as never, latencyHoverArgs as never, {} as never);
    expect(latencyHoverArgs.changed).toBe(true);
    expect(fakeCanvas.style.cursor).toBe('');
    expect(fakeCanvas.title).toBe('');
    latencyPlugin?.afterDatasetsDraw?.(fakeChart as never, {} as never, {} as never);
    expect(lineStrokes.some((stroke) => stroke.strokeStyle === '#fb7185' && stroke.lineWidth > 1.4)).toBe(true);

    const chartWithoutArea = {
      ...fakeChart,
      chartArea: undefined,
    };
    expect(() => latencyPlugin?.afterDatasetsDraw?.(chartWithoutArea as never, {} as never, {} as never)).not.toThrow();

    const outArgs = {
      event: { type: 'mouseout', x: null, y: null, native: null },
      replay: false,
      cancelable: false,
      inChartArea: false,
      changed: false,
    };
    latencyPlugin?.afterEvent?.(fakeChart as never, outArgs as never, {} as never);
    expect(outArgs.changed).toBe(true);
    expect(fakeCanvas.style.cursor).toBe('');
    expect(fakeCanvas.title).toBe('');
  });

  it('builds latency diagnostics log bounds without spreading large point arrays', () => {
    const points = Array.from({ length: 150_000 }, (_, index) => ({
      ttft_ms: index + 1,
      latency_ms: (index + 1) * 2,
    }));
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      latency_diagnostics: {
        total_points: points.length,
        sampled: true,
        p95_ttft_ms: 142_500,
        p95_latency_ms: 285_000,
        max_ttft_ms: 150_000,
        max_latency_ms: 300_000,
        points,
        density: [],
      },
    };

    expect(() => renderToStaticMarkup(<AnalysisPanel analysis={analysis} loading={false} isDark={false} isMobile={false} />)).not.toThrow();
    const latencyScatterIndex = chartCapture.scatterData.findIndex((data) => data.datasets[0]?.label === 'usage_stats.analysis_latency_samples');
    expect(latencyScatterIndex).toBeGreaterThanOrEqual(0);
    const latencyScatterOptions = chartCapture.scatterOptions[latencyScatterIndex];
    expect((latencyScatterOptions.scales?.x as { max?: number }).max).toBeGreaterThan(150_000);
    expect((latencyScatterOptions.scales?.y as { max?: number }).max).toBeGreaterThan(300_000);
  });

  it('uses theme-aware lighter colors for latency diagnostics', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      latency_diagnostics: {
        total_points: 1,
        sampled: false,
        p95_ttft_ms: 240,
        p95_latency_ms: 1200,
        max_ttft_ms: 240,
        max_latency_ms: 1200,
        points: [{ ttft_ms: 240, latency_ms: 1200 }],
        density: [{
          ttft_min_ms: 100,
          ttft_max_ms: 300,
          latency_min_ms: 800,
          latency_max_ms: 1400,
          count: 1,
          intensity: 1,
        }],
      },
    };

    renderToStaticMarkup(<AnalysisPanel analysis={analysis} loading={false} isDark={false} isMobile={false} />);
    const lightScatterIndex = chartCapture.scatterData.findIndex((data) => data.datasets[0]?.label === 'usage_stats.analysis_latency_samples');
    const lightData = chartCapture.scatterData[lightScatterIndex];
    const lightOptions = chartCapture.scatterOptions[lightScatterIndex];

    chartCapture.scatterData = [];
    chartCapture.scatterOptions = [];
    chartCapture.scatterPlugins = [];
    renderToStaticMarkup(<AnalysisPanel analysis={analysis} loading={false} isDark isMobile={false} />);
    const darkScatterIndex = chartCapture.scatterData.findIndex((data) => data.datasets[0]?.label === 'usage_stats.analysis_latency_samples');
    const darkData = chartCapture.scatterData[darkScatterIndex];
    const darkOptions = chartCapture.scatterOptions[darkScatterIndex];

    expect(lightData.datasets[0]?.pointBackgroundColor).toBe('rgba(45, 212, 191, 0.62)');
    expect(darkData.datasets[0]?.pointBackgroundColor).toBe('rgba(94, 234, 212, 0.72)');
    expect(lightData.datasets[0]?.pointBorderColor).toBe('transparent');
    expect(darkData.datasets[0]?.pointBorderColor).toBe('transparent');
    const lightPluginColors = (lightOptions.plugins as { analysisLatencyDiagnostics?: { colors?: Record<string, unknown> } }).analysisLatencyDiagnostics?.colors;
    const darkPluginColors = (darkOptions.plugins as { analysisLatencyDiagnostics?: { colors?: Record<string, unknown> } }).analysisLatencyDiagnostics?.colors;
    expect(lightPluginColors).toMatchObject({
      point: '#14b8a6',
      pointFill: 'rgba(45, 212, 191, 0.62)',
      p95TTFT: '#38bdf8',
      p95Latency: '#fb7185',
    });
    expect(darkPluginColors).toMatchObject({
      point: '#5eead4',
      pointFill: 'rgba(94, 234, 212, 0.72)',
      p95TTFT: '#7dd3fc',
      p95Latency: '#fda4af',
    });
    expect(lightPluginColors).not.toHaveProperty('densityRamp');
    expect(darkPluginColors).not.toHaveProperty('densityRamp');
    expect(lightPluginColors).not.toHaveProperty('equalLine');
    expect(darkPluginColors).not.toHaveProperty('equalLine');
    expect(lightPluginColors).not.toHaveProperty('guideText');
    expect(darkPluginColors).not.toHaveProperty('guideText');
  });

  it('renders cost breakdown with total beside blended rate, segment percentages and sparkline', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      timezone: 'Asia/Shanghai',
      token_usage: [{
        bucket: '2026-05-28T01:00:00Z',
        input_tokens: 1_000_000,
        output_tokens: 1_000_000,
        cached_tokens: 500_000,
        reasoning_tokens: 100_000,
        total_tokens: 3_000_000,
        requests: 10,
        cost_usd: 6,
        cost_available: true,
      }],
      cost_breakdown: {
        input_cost_usd: 1,
        output_cost_usd: 3,
        cached_cost_usd: 2,
        total_cost_usd: 6,
        cost_available: true,
      },
    };

    const markup = renderToStaticMarkup(<AnalysisPanel analysis={analysis} loading={false} isDark={false} isMobile={false} />);

    expect(markup).not.toContain('costHeaderTotal');
    expect(markup).toContain('costRateMetric');
    expect(markup).toContain('usage_stats.analysis_cost_per_million_tokens');
    expect(markup).toContain('usage_stats.analysis_blended_rate');
    expect(markup.indexOf('usage_stats.total_cost')).toBeLessThan(markup.indexOf('usage_stats.analysis_cost_per_million_tokens'));
    expect(markup).toContain('--cost-segment-color:#2563eb');
    expect(markup).toContain('--cost-segment-color:#16a34a');
    expect(markup).toContain('--cost-segment-color:#d97706');
    expect(markup).toContain('background-color:#2563eb');
    expect(markup).toContain('background-color:#16a34a');
    expect(markup).toContain('background-color:#d97706');
    expect(markup).not.toContain('filter:saturate');
    expect(markup).toContain('usage_stats.analysis_cost_share: 16.67%');
    expect(markup).toContain('usage_stats.input_tokens · usage_stats.analysis_cost_share');
    expect(markup).not.toContain('title="usage_stats.input_tokens · usage_stats.analysis_cost_share');
    expect(markup).toContain('usage_stats.analysis_cost_per_million_tokens: $4.00');
    expect(markup).toContain('usage_stats.total_tokens: 500.00K');
    expect(markup).toContain('usage_stats.analysis_cost_rate_sparkline_hint');
    expect(markup).toContain('usage_stats.analysis_cost_per_million_tokens: $2.00');
    expect(markup).toContain('usage_stats.total_cost: $6.00');
    expect(markup).toContain('usage_stats.total_tokens: 3.00M');
    expect(chartCapture.barData?.labels).toEqual(['09:00']);
    expect(markup).toContain('aria-label="09:00, usage_stats.analysis_cost_per_million_tokens: $2.00, usage_stats.total_cost: $6.00, usage_stats.total_tokens: 3.00M"');
    expect(markup).toContain('class="_costRateSparkBar_');
    expect(markup).toContain('tabindex="0"');
    expect(markup).toContain('$6.00');
    expect(markup).toContain('$2.00');
    expect(markup).toContain('16.67%');
    expect(markup).toContain('50.00%');
    expect(markup).toContain('33.33%');
  });

  it('renders model efficiency as cost per million total tokens against total tokens', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      model_efficiency: [
        {
          model: 'gpt-4o',
          requests: 4,
          input_tokens: 1000,
          output_tokens: 300,
          cached_tokens: 100,
          reasoning_tokens: 20,
          total_tokens: 2_000_000,
          cost_usd: 2,
          cost_available: true,
          cost_per_request_usd: 0.5,
          output_tokens_per_request: 80,
          cache_rate: 0.1,
        },
        {
          model: 'claude-sonnet',
          requests: 100,
          input_tokens: 1200,
          output_tokens: 500,
          cached_tokens: 200,
          reasoning_tokens: 50,
          total_tokens: 3_000_000,
          cost_usd: 4.5,
          cost_available: true,
          cost_per_request_usd: 0.5,
          output_tokens_per_request: 55,
          cache_rate: 0.1,
        },
        {
          model: 'gemini-pro',
          requests: 10000,
          input_tokens: 1500,
          output_tokens: 650,
          cached_tokens: 300,
          reasoning_tokens: 60,
          total_tokens: 4_000_000,
          cost_usd: 8,
          cost_available: true,
          cost_per_request_usd: 0.5,
          output_tokens_per_request: 40,
          cache_rate: 0.1,
        },
      ],
    };

    const markup = renderToStaticMarkup(<AnalysisPanel analysis={analysis} loading={false} isDark={false} isMobile={false} />);

    const modelScatterIndex = chartCapture.scatterData.findIndex((data) => data.datasets[0]?.label === 'usage_stats.analysis_model_efficiency_title');
    expect(modelScatterIndex).toBeGreaterThanOrEqual(0);
    const modelScatterData = chartCapture.scatterData[modelScatterIndex];
    const modelScatterOptions = chartCapture.scatterOptions[modelScatterIndex];
    expect(modelScatterData.datasets[0]?.label).toBe('usage_stats.analysis_model_efficiency_title');
    expect(modelScatterData.datasets[0]?.data[0]).toMatchObject({ x: 2_000_000, y: 1 });
    expect(modelScatterOptions.scales?.x?.type).toBe('logarithmic');
    expect(modelScatterOptions.scales?.y?.type).toBe('logarithmic');
    expect(modelScatterOptions.scales?.x).not.toHaveProperty('beginAtZero');
    expect(modelScatterOptions.scales?.y).not.toHaveProperty('beginAtZero');
    const pointRadii = modelScatterData.datasets[0]?.pointRadius as number[];
    expect(pointRadii[0]).toBe(5);
    expect(pointRadii[1]).toBeGreaterThan(10);
    expect(pointRadii[2]).toBe(24);
    expect(pointRadii[2] - pointRadii[1]).toBeGreaterThan(4);
    expect(modelScatterData.datasets[0]?.clip).toBe(false);
    expect(modelScatterOptions.layout?.padding).toEqual({ top: 16, right: 24, bottom: 22, left: 18 });
    expect((modelScatterOptions.scales?.x as { min?: number }).min).toBeLessThan(2_000_000);
    expect((modelScatterOptions.scales?.x as { max?: number }).max).toBeGreaterThan(9_000_000);
    expect((modelScatterOptions.scales?.y as { min?: number }).min).toBeLessThan(1);
    expect((modelScatterOptions.scales?.y as { max?: number }).max).toBeGreaterThan(4);
    expect(markup).not.toContain('gpt-4o');
    expect(markup).not.toContain('claude-sonnet');
    expect(markup).not.toContain('gemini-pro');
    const modelColors = modelScatterData.datasets[0]?.borderColor as string[];
    expect(new Set(modelColors)).toHaveProperty('size', 3);
    expect(modelColors).not.toContain('#dc2626');
    expect(modelColors).not.toContain('#2563eb');
    expect(typeof modelScatterData.datasets[0]?.backgroundColor).toBe('function');
    const gradient = {
      addColorStop: vi.fn(),
    };
    const createLinearGradient = vi.fn(() => gradient);
    const createRadialGradient = vi.fn();
    const fill = (modelScatterData.datasets[0]?.backgroundColor as (context: unknown) => unknown)({
      dataIndex: 0,
      chart: { ctx: { createLinearGradient, createRadialGradient } },
      element: { x: 40, y: 50, options: { radius: 12 } },
    });
    expect(fill).toBe(gradient);
    expect(createRadialGradient).not.toHaveBeenCalled();
    expect(createLinearGradient).toHaveBeenCalledWith(28, 50, 52, 50);
    expect(gradient.addColorStop).toHaveBeenCalledWith(0, '#7898c8');
    expect(gradient.addColorStop).toHaveBeenCalledWith(1, '#5b7fb9');
    expect(modelScatterOptions.plugins?.tooltip?.enabled).toBe(false);
    expect(typeof modelScatterOptions.plugins?.tooltip?.external).toBe('function');
  });

  it('keeps each overlapped model name grouped with its own model efficiency values', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      model_efficiency: [
        {
          model: 'gpt-4o',
          requests: 4,
          input_tokens: 1000,
          output_tokens: 300,
          cached_tokens: 100,
          reasoning_tokens: 20,
          total_tokens: 2_000_000,
          cost_usd: 2,
          cost_available: true,
          cost_per_request_usd: 0.5,
          output_tokens_per_request: 80,
          cache_rate: 0.1,
        },
        {
          model: 'claude-sonnet',
          requests: 6,
          input_tokens: 1100,
          output_tokens: 400,
          cached_tokens: 120,
          reasoning_tokens: 30,
          total_tokens: 2_000_000,
          cost_usd: 2,
          cost_available: true,
          cost_per_request_usd: 0.333,
          output_tokens_per_request: 72,
          cache_rate: 0.12,
        },
      ],
    };

    renderToStaticMarkup(<AnalysisPanel analysis={analysis} loading={false} isDark={false} isMobile={false} />);

    const elements = new Map<string, FakeElement>();
    const fakeDocument = createFakeDocument(elements);
    vi.stubGlobal('document', fakeDocument);
    vi.stubGlobal('window', { innerWidth: 1024 });

    const modelScatterIndex = chartCapture.scatterData.findIndex((data) => data.datasets[0]?.label === 'usage_stats.analysis_model_efficiency_title');
    expect(modelScatterIndex).toBeGreaterThanOrEqual(0);
    chartCapture.scatterOptions[modelScatterIndex]?.plugins?.tooltip?.external?.({
      chart: {
        canvas: {
          getBoundingClientRect: () => ({ left: 10, top: 20 }),
        },
      },
      tooltip: {
        opacity: 1,
        caretX: 100,
        caretY: 60,
        dataPoints: [{ dataIndex: 0 }, { dataIndex: 1 }],
      },
    } as never);

    const tooltipElement = elements.get('analysis-model-efficiency-tooltip');
    expect(tooltipElement).toBeTruthy();
    const groups = tooltipElement?.children ?? [];
    expect(groups).toHaveLength(2);
    expect(groups[0]?.children[0]?.children[0]?.className).toContain('modelEfficiencyTooltipDot');
    expect(groups[0]?.children[0]?.children[1]?.tagName).toBe('strong');
    expect(groups[0]?.children[0]?.children[1]?.textContent).toBe('gpt-4o');
    expect(collectFakeText(groups[0])).toEqual([
      'gpt-4o',
      'usage_stats.total_tokens: 2.00M',
      'usage_stats.analysis_cost_per_million_tokens: $1.00',
      'usage_stats.requests_count: 4',
    ]);
    expect(groups[1]?.children[0]?.children[0]?.className).toContain('modelEfficiencyTooltipDot');
    expect(groups[1]?.children[0]?.children[1]?.tagName).toBe('strong');
    expect(groups[1]?.children[0]?.children[1]?.textContent).toBe('claude-sonnet');
    expect(collectFakeText(groups[1])).toEqual([
      'claude-sonnet',
      'usage_stats.total_tokens: 2.00M',
      'usage_stats.analysis_cost_per_million_tokens: $1.00',
      'usage_stats.requests_count: 6',
    ]);
  });

  it('keeps partial cost values visible and shows pricing hints near analysis charts', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      token_usage: [{
        bucket: '2026-05-28T01:00:00Z',
        input_tokens: 1000,
        output_tokens: 100,
        cached_tokens: 0,
        reasoning_tokens: 0,
        total_tokens: 1100,
        requests: 3,
        cost_usd: 0,
        cost_available: false,
      }],
      api_key_composition: [{
        key: 'unpriced-key',
        label: 'Unpriced Key',
        requests: 3,
        input_tokens: 1000,
        output_tokens: 100,
        cached_tokens: 0,
        reasoning_tokens: 0,
        total_tokens: 1100,
        percent: 100,
        cost_usd: 0,
        cost_available: false,
      }],
      model_efficiency: [{
        model: 'unpriced-model',
        requests: 3,
        input_tokens: 1000,
        output_tokens: 100,
        cached_tokens: 0,
        reasoning_tokens: 0,
        total_tokens: 1_000_000,
        cost_usd: 0,
        cost_available: false,
        cost_per_request_usd: 0,
        output_tokens_per_request: 33.33,
        cache_rate: 0,
      }],
      cost_breakdown: {
        input_cost_usd: 0,
        output_cost_usd: 0,
        cached_cost_usd: 0,
        total_cost_usd: 0,
        cost_available: false,
      },
      heatmap: {
        api_keys: ['unpriced-key'],
        api_key_labels: { 'unpriced-key': 'Unpriced Key' },
        models: ['unpriced-model'],
        cells: [{
          api_key: 'unpriced-key',
          model: 'unpriced-model',
          input_tokens: 1000,
          output_tokens: 100,
          cached_tokens: 0,
          reasoning_tokens: 0,
          total_tokens: 1100,
          requests: 3,
          cost_usd: 0,
          cost_available: false,
          intensity: 1,
        }],
      },
    };

    const markup = renderToStaticMarkup(<AnalysisPanel analysis={analysis} loading={false} isDark={false} isMobile={false} />);

    const costDataset = chartCapture.barData?.datasets.find((dataset) => dataset.label === 'usage_stats.total_cost');
    expect(costDataset?.data).toEqual([0]);
    expect(chartCapture.scatterData).toHaveLength(0);
    expect(markup).toMatch(/Unpriced Key[\s\S]*\$0\.0000/);
    expect(markup).toContain('usage_stats.cost_need_price');
    expect(markup).toContain('<div class="_cardTitleLine_');
    expect(markup).toContain('<h2>usage_stats.analysis_token_usage_title</h2><small class="_costHeaderHint_');
    expect(markup).toContain('</small></div><p>usage_stats.analysis_token_usage_subtitle</p>');
    expect(markup).not.toContain('usage_stats.analysis_token_usage_subtitle (usage_stats.cost_need_price)');
    expect(markup.match(/costHeaderHint/g)?.length).toBe(5);
    expect(markup).not.toContain('costWarning');
    expect(markup).toContain('usage_stats.analysis_cost_per_million_tokens</span><strong>$0.0000</strong>');
    expect(markup).toContain('usage_stats.total_cost: $0.0000');
  });

  it('keeps partially priced cost breakdown rates visible under the card-level pricing hint', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      token_usage: [{
        bucket: '2026-05-28T01:00:00Z',
        input_tokens: 1000,
        output_tokens: 100,
        cached_tokens: 0,
        reasoning_tokens: 0,
        total_tokens: 1100,
        requests: 3,
        cost_usd: 9,
        cost_available: false,
      }],
      cost_breakdown: {
        input_cost_usd: 9,
        output_cost_usd: 0,
        cached_cost_usd: 0,
        total_cost_usd: 9,
        cost_available: false,
      },
    };

    const markup = renderToStaticMarkup(<AnalysisPanel analysis={analysis} loading={false} isDark={false} isMobile={false} />);

    const costDataset = chartCapture.barData?.datasets.find((dataset) => dataset.label === 'usage_stats.total_cost');
    expect(costDataset?.data).toEqual([9]);
    expect(markup).toContain('<h2>usage_stats.analysis_cost_breakdown_title</h2><small class="_costHeaderHint_');
    expect(markup).toContain('usage_stats.cost_need_price');
    expect(markup).toContain('usage_stats.total_cost</span><strong>$9.00</strong>');
    expect(markup).toContain('usage_stats.analysis_cost_per_million_tokens</span><strong>$8,181.82</strong>');
    expect(markup).not.toContain('usage_stats.analysis_cost_per_million_tokens</span><strong>usage_stats.cost_need_price</strong>');
    expect(markup).not.toContain('costWarning');
  });

  it('shows compact heatmap cells with id keys and display labels', () => {
    const responseKey = '9007199254740993';
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      heatmap: {
        api_keys: [responseKey],
        api_key_labels: {
          [responseKey]: 'Primary Key',
        },
        models: ['claude-3-7-sonnet-20250219-long-context'],
        cells: [{
          api_key: responseKey,
          model: 'claude-3-7-sonnet-20250219-long-context',
          input_tokens: 1000,
          output_tokens: 200,
          reasoning_tokens: 30,
          cached_tokens: 100,
          total_tokens: 1330,
          requests: 3,
          cost_usd: 0.1234,
          cost_available: true,
          intensity: 1,
        }],
      },
    };

    const markup = renderToStaticMarkup(<AnalysisPanel analysis={analysis} loading={false} isDark={false} isMobile={false} />);

    expect(markup).toContain('1.33K');
    expect(markup).toContain('Primary Key');
    expect(markup).not.toContain(responseKey);
    expect(markup).toContain('data-full-name="claude-3-7-sonnet-20250219-long-context"');
    expect(markup).toContain('aria-label="claude-3-7-sonnet-20250219-long-context"');
    expect(markup).not.toContain('title="claude-3-7-sonnet-20250219-long-context"');
    expect(markup).toContain('usage_stats.requests_count');
    expect(markup).toContain('usage_stats.input_tokens');
    expect(markup).toContain('usage_stats.reasoning_tokens');
    expect(markup).toContain('usage_stats.total_cost');
    expect(markup).toContain('heatmapCardLight');
    expect(markup).not.toContain('usage_stats.analysis_heatmap_tokens_prefix');
    expect(markup).not.toContain('usage_stats.analysis_heatmap_requests_prefix');
  });

  it('keeps rendering when an older analysis response omits heatmap', () => {
    const analysis = { ...emptyAnalysis, heatmap: undefined } as unknown as AnalysisResponse;

    expect(() => renderToStaticMarkup(<AnalysisPanel analysis={analysis} loading={false} isDark={false} isMobile={false} />)).not.toThrow();
  });
});
