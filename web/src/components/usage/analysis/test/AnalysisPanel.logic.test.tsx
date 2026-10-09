// @vitest-environment happy-dom

import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Interaction, Tooltip } from 'chart.js';
import type { ChartData, ChartOptions, Plugin } from 'chart.js';
import type { AnalysisCompositionItem, AnalysisLatencyDiagnostics, AnalysisModelEfficiencyItem, AnalysisResponse, AnalysisTokenUsageBucket } from '@/lib/types';

type TokenAverageLinePluginOptions = {
  value: number;
  color: string;
};

const chartCapture = vi.hoisted(() => ({
  barData: null as ChartData<'bar', Array<number | null>, string> | null,
  barOptions: null as ChartOptions<'bar'> | null,
  barPlugins: undefined as Plugin<'bar'>[] | undefined,
  doughnutData: null as ChartData<'doughnut', number[], string> | null,
  doughnutOptions: null as ChartOptions<'doughnut'> | null,
  doughnutPlugins: undefined as Plugin<'doughnut'>[] | undefined,
  doughnutCount: 0,
  scatterData: [] as ChartData<'scatter'>[],
  scatterOptions: [] as ChartOptions<'scatter'>[],
  scatterPlugins: [] as Array<Plugin<'scatter'>[] | undefined>,
}));

vi.mock('react-chartjs-2', () => ({
  Bar: (props: { data: ChartData<'bar', Array<number | null>, string>; options: ChartOptions<'bar'>; plugins?: Plugin<'bar'>[] }) => {
    // 既有断言只观察首个 Token Usage 图，后续 Analysis 柱图不能覆盖该捕获值。
    if (chartCapture.barData === null) {
      chartCapture.barData = props.data;
      chartCapture.barOptions = props.options;
      chartCapture.barPlugins = props.plugins;
    }
    return React.createElement('div');
  },
  Doughnut: (props: { data: ChartData<'doughnut', number[], string>; options: ChartOptions<'doughnut'>; plugins?: Plugin<'doughnut'>[] }) => {
    chartCapture.doughnutData = props.data;
    chartCapture.doughnutOptions = props.options;
    chartCapture.doughnutPlugins = props.plugins;
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

import { AnalysisTestPanel, emptyAnalysis, renderAnalysisPanel } from './analysisFixtures';

const composition = (overrides: Partial<AnalysisCompositionItem> = {}): AnalysisCompositionItem => ({
  key: '1',
  label: 'Primary Key',
  total_tokens: 1000,
  requests: 4,
  percent: 100,
  input_tokens: 700,
  output_tokens: 200,
  cache_read_tokens: 50,
  cache_creation_tokens: 0,
  reasoning_tokens: 50,
  cost_usd: 0.42,
  cost_available: true,
  ...overrides,
});

const efficiency = (overrides: Partial<AnalysisModelEfficiencyItem> = {}): AnalysisModelEfficiencyItem => ({
  model: 'gpt-4o',
  requests: 4,
  input_tokens: 1000,
  output_tokens: 300,
  cache_read_tokens: 100,
  cache_creation_tokens: 0,
  reasoning_tokens: 20,
  total_tokens: 2_000_000,
  cost_usd: 2,
  cost_available: true,
  cost_per_request_usd: 0.5,
  output_tokens_per_request: 80,
  cache_read_rate: 0.1,
  ...overrides,
});

const tokenBucket = (overrides: Partial<AnalysisTokenUsageBucket> = {}): AnalysisTokenUsageBucket => ({
  bucket: '2026-05-28T01:00:00Z',
  input_tokens: 1000,
  output_tokens: 100,
  cache_read_tokens: 0,
  cache_creation_tokens: 0,
  reasoning_tokens: 0,
  total_tokens: 1100,
  requests: 3,
  cost_usd: 0,
  cost_available: true,
  ...overrides,
});

describe('AnalysisPanel token chart data', () => {
  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(260);
    vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(160);
    chartCapture.barData = null;
    chartCapture.barOptions = null;
    chartCapture.barPlugins = undefined;
    chartCapture.doughnutData = null;
    chartCapture.doughnutOptions = null;
    chartCapture.doughnutPlugins = undefined;
    chartCapture.doughnutCount = 0;
    chartCapture.scatterData = [];
    chartCapture.scatterOptions = [];
    chartCapture.scatterPlugins = [];
  });

  afterEach(() => {
    document.body.replaceChildren();
    vi.restoreAllMocks();
  });

  it('splits cache read and write from input while keeping total tooltip values', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      timezone: 'Asia/Shanghai',
      token_usage: [tokenBucket({
        cache_read_tokens: 600,
        cache_creation_tokens: 100,
        reasoning_tokens: 50,
        total_tokens: 1150,
        cost_usd: 0.0123,
      })],
    };

    renderToStaticMarkup(<AnalysisTestPanel analysis={analysis} />);

    expect(chartCapture.barData?.labels).toEqual(['09:00']);
    const datasets = chartCapture.barData!.datasets;
    expect(datasets.find((dataset) => dataset.label === 'usage_stats.input_tokens')?.data).toEqual([300]);
    expect(datasets.find((dataset) => dataset.label === 'usage_stats.cache_read_tokens')?.data).toEqual([600]);
    expect(datasets.find((dataset) => dataset.label === 'usage_stats.cache_creation_tokens')?.data).toEqual([100]);
    expect(datasets.find((dataset) => dataset.label === 'usage_stats.output_tokens')?.data).toEqual([50]);
    expect(datasets.find((dataset) => dataset.label === 'usage_stats.reasoning_tokens')?.data).toEqual([50]);
    expect(datasets.find((dataset) => dataset.label === 'usage_stats.total_cost')?.data).toEqual([0.0123]);
    expect(datasets.find((dataset) => dataset.label === 'usage_stats.total_cost')?.yAxisID).toBe('cost');
    expect(chartCapture.barOptions?.scales).toHaveProperty('cost');
    const tooltipLabel = chartCapture.barOptions?.plugins?.tooltip?.callbacks?.label;
    expect(tooltipLabel!({
      dataset: { label: 'usage_stats.input_tokens', tooltipData: [1000] },
      dataIndex: 0,
      parsed: { y: 300 },
    } as never)).toBe('usage_stats.input_tokens: 1.00K');
    expect(tooltipLabel!({
      dataset: { label: 'usage_stats.output_tokens', tooltipData: [100] },
      dataIndex: 0,
      parsed: { y: 50 },
    } as never)).toBe('usage_stats.output_tokens: 100');
    expect(tooltipLabel!({
      dataset: null,
      dataIndex: 0,
      parsed: { y: 125 },
    } as never)).toBe('125');
    const tooltipFooter = chartCapture.barOptions?.plugins?.tooltip?.callbacks?.footer;
    expect(tooltipFooter!([{ dataIndex: 0 }] as never)).toBe('usage_stats.total_tokens: 1.15K');
  });

  it('shows the average total token value as a legend chip while keeping the chart reference line label-free', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      token_usage: [
        tokenBucket({
          input_tokens: 100,
          output_tokens: 0,
          total_tokens: 100,
          requests: 1,
        }),
        tokenBucket({
          bucket: '2026-05-28T02:00:00Z',
          input_tokens: 0,
          output_tokens: 0,
          total_tokens: 0,
          requests: 0,
        }),
        tokenBucket({
          bucket: '2026-05-28T03:00:00Z',
          input_tokens: 400,
          total_tokens: 500,
          requests: 2,
        }),
      ],
    };

    const markup = renderToStaticMarkup(<AnalysisTestPanel analysis={analysis} />);
    const plugins = chartCapture.barOptions?.plugins as (ChartOptions<'bar'>['plugins'] & {
      analysisTokenAverageLine?: TokenAverageLinePluginOptions;
    }) | undefined;

    expect(chartCapture.barPlugins?.map((plugin) => plugin.id)).toContain('analysis-token-average-line');
    expect(plugins?.analysisTokenAverageLine).toMatchObject({
      value: 200,
    });
    expect(markup).toContain('usage_stats.analysis_token_average: 200');
  });

  it('renders a clean circular usage distribution donut with token-share style rows', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      range_start: '2026-05-28T00:00:00Z',
      range_end: '2026-05-28T02:00:00Z',
      api_key_composition: [composition()],
      model_composition: [composition({
        key: 'gpt-4o',
        label: 'gpt-4o',
      })],
    };

    chartCapture.doughnutCount = 0;
    const markup = renderToStaticMarkup(<AnalysisTestPanel analysis={analysis} />);

    expect(chartCapture.doughnutCount).toBe(1);
    expect(chartCapture.doughnutData?.labels).toEqual(['Primary Key']);
    expect(chartCapture.doughnutData?.datasets[0]?.data).toEqual([1000]);
    expect(chartCapture.doughnutOptions).toMatchObject({
      interaction: { mode: 'analysisCompositionArc', intersect: false, axis: 'r' },
      hover: { mode: 'analysisCompositionArc', intersect: false, axis: 'r' },
    });
    expect(chartCapture.doughnutOptions?.maintainAspectRatio).toBe(false);
    expect(chartCapture.doughnutOptions?.plugins?.tooltip?.enabled).toBe(true);
    expect(chartCapture.doughnutOptions?.plugins?.tooltip?.position).toBe('analysisCompositionCursor');
    expect(chartCapture.doughnutOptions?.plugins?.tooltip?.external).toBeUndefined();
    expect(chartCapture.doughnutPlugins?.map((plugin) => plugin.id)).toContain('analysis-composition-labels');
    expect(markup).toContain('usage_stats.analysis_composition_title');
    expect(markup).toContain('usage_stats.analysis_composition_api_key_tab');
    expect(markup).toContain('usage_stats.analysis_composition_token_percent');
    expect(markup).toContain('Primary Key');
    expect(markup).toContain('usage_stats.rpm');
    expect(markup).toContain('0.03');
    expect(markup).toContain('usage_stats.tpm');
    expect(markup).toContain('8.33');
    expect(markup).not.toContain('<table');
    expect(markup).not.toContain('gpt-4o');
    expect(markup).not.toContain('usage_stats.analysis_model_composition_title');
    expect(markup).not.toContain('usage_stats.analysis_auth_files_composition_title');
    expect(markup).not.toContain('usage_stats.analysis_ai_provider_composition_title');
  });

  it('uses native usage distribution tooltip callbacks with wrapped long titles', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      api_key_composition: [composition()],
    };

    renderToStaticMarkup(<AnalysisTestPanel analysis={analysis} />);

    const tooltipLabel = chartCapture.doughnutOptions?.plugins?.tooltip?.callbacks?.label;
    const tooltipTitle = chartCapture.doughnutOptions?.plugins?.tooltip?.callbacks?.title;
    expect(tooltipTitle!([{ label: 'Primary Key' }] as never)).toEqual(['Primary Key']);
    const longTitle = tooltipTitle!([{
      label: 'averyveryverylongapikeylabelwithoutnaturalbreaks-000000000000000000000000000000000000',
    }] as never);
    expect(Array.isArray(longTitle)).toBe(true);
    expect(longTitle).toHaveLength(3);
    expect((longTitle as string[]).every((line) => line.length <= 28)).toBe(true);
    expect((longTitle as string[])[2]?.endsWith('...')).toBe(true);
    expect(tooltipLabel!({
      label: 'Primary Key',
      parsed: 1000,
    } as never)).toBe('usage_stats.total_tokens: 1.00K');
  });

  it('coerces non-string usage distribution tooltip titles before wrapping', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      api_key_composition: [composition()],
    };

    renderToStaticMarkup(<AnalysisTestPanel analysis={analysis} />);

    const tooltipTitle = chartCapture.doughnutOptions?.plugins?.tooltip?.callbacks?.title;
    expect(tooltipTitle!([{ label: 12345 }] as never)).toEqual(['12345']);
  });

  it('uses usage distribution interaction options for small arcs', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      api_key_composition: [
        composition({
          total_tokens: 999,
          percent: 99.9,
          reasoning_tokens: 49,
        }),
        composition({
          key: '2',
          label: 'Tiny Key',
          total_tokens: 1,
          requests: 1,
          percent: 0.1,
          input_tokens: 1,
          output_tokens: 0,
          cache_read_tokens: 0,
          reasoning_tokens: 0,
          cost_usd: 0,
        }),
      ],
    };

    renderToStaticMarkup(<AnalysisTestPanel analysis={analysis} />);

    expect(chartCapture.doughnutData?.labels).toEqual(['Primary Key', 'Tiny Key']);
    expect(chartCapture.doughnutOptions).toMatchObject({
      interaction: { mode: 'analysisCompositionArc', intersect: false, axis: 'r' },
      hover: { mode: 'analysisCompositionArc', intersect: false, axis: 'r' },
    });
    expect(chartCapture.doughnutOptions?.plugins?.tooltip).toMatchObject({
      enabled: true,
      mode: 'analysisCompositionArc',
      intersect: false,
      axis: 'r',
      position: 'analysisCompositionCursor',
    });
    expect(chartCapture.doughnutOptions?.plugins?.tooltip?.external).toBeUndefined();
    expect(chartCapture.doughnutPlugins?.map((plugin) => plugin.id)).toContain('analysis-composition-labels');
  });

  it('limits usage distribution hover to the doughnut ring while allowing arc edges', () => {
    renderToStaticMarkup(<AnalysisTestPanel analysis={{
      ...emptyAnalysis,
      api_key_composition: [composition()],
    }} />);

    const mode = (Interaction.modes as typeof Interaction.modes & {
      analysisCompositionArc?: (chart: unknown, event: { x: number; y: number }, options: unknown, useFinalPosition?: boolean) => unknown[];
    }).analysisCompositionArc;
    const arcElement = {
      options: { spacing: 4, borderWidth: 0 },
      getProps: () => ({
        x: 150,
        y: 150,
        innerRadius: 70,
        outerRadius: 140,
        startAngle: 0,
        endAngle: Math.PI / 2,
        circumference: Math.PI / 2,
      }),
    };
    const activeItem = { element: arcElement, datasetIndex: 0, index: 0 };
    vi.spyOn(Interaction.modes, 'nearest').mockReturnValue([activeItem] as never);

    expect(mode!({} as never, { x: 225, y: 225 }, {}, false)).toEqual([activeItem]);
    expect(mode!({} as never, { x: 150, y: 150 }, {}, false)).toEqual([]);
    expect(mode!({} as never, { x: 300, y: 150 }, {}, false)).toEqual([]);
    expect(mode!({} as never, { x: 255, y: 150 }, {}, false)).toEqual([activeItem]);
  });

  it('falls back to painted full-circle doughnut arcs when Chart.js radial nearest returns no candidates', () => {
    renderToStaticMarkup(<AnalysisTestPanel analysis={{
      ...emptyAnalysis,
      api_key_composition: [composition()],
    }} />);

    const mode = (Interaction.modes as typeof Interaction.modes & {
      analysisCompositionArc?: (chart: unknown, event: { x: number; y: number }, options: unknown, useFinalPosition?: boolean) => unknown[];
    }).analysisCompositionArc;
    const fullCircleArcElement = {
      options: { spacing: 4, borderWidth: 0 },
      getProps: () => ({
        x: 150,
        y: 150,
        innerRadius: 70,
        outerRadius: 140,
        startAngle: -Math.PI / 2,
        endAngle: (Math.PI * 3) / 2,
        circumference: Math.PI * 2,
      }),
    };
    const fakeChart = {
      getSortedVisibleDatasetMetas: () => [{
        type: 'doughnut',
        index: 0,
        data: [fullCircleArcElement],
      }],
    };

    vi.spyOn(Interaction.modes, 'nearest').mockReturnValue([]);

    expect(mode!(fakeChart as never, { x: 255, y: 150 }, {}, false)).toEqual([{
      element: fullCircleArcElement,
      datasetIndex: 0,
      index: 0,
    }]);
    expect(mode!(fakeChart as never, { x: 150, y: 150 }, {}, false)).toEqual([]);
    expect(mode!(fakeChart as never, { x: 300, y: 150 }, {}, false)).toEqual([]);
  });

  it('positions the usage distribution tooltip away from the hovered arc', () => {
    renderToStaticMarkup(<AnalysisTestPanel analysis={{
      ...emptyAnalysis,
      api_key_composition: [composition()],
    }} />);

    const positioner = (Tooltip.positioners as typeof Tooltip.positioners & {
      analysisCompositionCursor?: (items: unknown[], eventPosition: { x: number; y: number }) => unknown;
    }).analysisCompositionCursor;
    expect(positioner!.call({ chart: { chartArea: { top: 0, bottom: 300 }, height: 300 } }, [], { x: 150, y: 40 })).toEqual({
      x: 150,
      y: 40,
      xAlign: 'center',
      yAlign: 'bottom',
    });
    expect(positioner!.call({ chart: { chartArea: { top: 0, bottom: 300 }, height: 300 } }, [], { x: 150, y: 260 })).toEqual({
      x: 150,
      y: 260,
      xAlign: 'center',
      yAlign: 'top',
    });
  });

  it('shows raw composition percentages while bounding progress bar width', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      api_key_composition: [composition({
        total_tokens: 1200,
        requests: 3,
        percent: 120,
        input_tokens: 900,
        output_tokens: 300,
        cache_read_tokens: 0,
        reasoning_tokens: 0,
        cost_usd: 0.3,
      })],
    };

    const markup = renderToStaticMarkup(<AnalysisTestPanel analysis={analysis} />);

    expect(markup).toContain('120.00%');
    expect(markup).toContain('width:100%');
  });

  it('uses distinct colors for every composition entry', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      api_key_composition: Array.from({ length: 7 }, (_, index) => (composition({
        key: `key-${index + 1}`,
        label: `Key ${index + 1}`,
        total_tokens: 700 - (index * 100),
        requests: 7 - index,
        percent: 0,
        input_tokens: 0,
        output_tokens: 0,
        cache_read_tokens: 0,
        reasoning_tokens: 0,
        cost_usd: 0,
      }))),
    };

    const markup = renderToStaticMarkup(<AnalysisTestPanel analysis={analysis} />);
    const backgroundColor = chartCapture.doughnutData?.datasets[0]?.backgroundColor;
    const compositionColors = Array.from({ length: 7 }, (_, dataIndex) => (
      backgroundColor as (context: { dataIndex: number; chart: { chartArea?: unknown } }) => string
    )({ dataIndex, chart: {} }));

    expect(markup).not.toContain('usage_stats.analysis_others');
    expect(chartCapture.doughnutData?.datasets[0]?.data).toHaveLength(7);
    expect(new Set(compositionColors).size).toBe(7);
  });

  it('renders latency diagnostics scatter before usage distribution', () => {
    const latencyDiagnostics: AnalysisLatencyDiagnostics = {
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
    };

    const markup = renderToStaticMarkup(<AnalysisTestPanel analysis={emptyAnalysis} latencyDiagnostics={latencyDiagnostics} />);

    expect(markup).toContain('usage_stats.analysis_latency_title');
    expect(markup.indexOf('usage_stats.analysis_composition_title')).toBeLessThan(markup.indexOf('usage_stats.analysis_latency_title'));
    const latencyScatterIndex = chartCapture.scatterData.findIndex((data) => data.datasets[0]?.label === 'usage_stats.analysis_latency_samples');
    expect(latencyScatterIndex).toBeGreaterThanOrEqual(0);
    const latencyScatterData = chartCapture.scatterData[latencyScatterIndex];
    const latencyScatterOptions = chartCapture.scatterOptions[latencyScatterIndex];
    expect(latencyScatterData.datasets[0]?.data[0]).toMatchObject({ x: 120, y: 800 });
    expect(latencyScatterOptions.scales?.x?.type).toBe('logarithmic');
    expect(latencyScatterOptions.scales?.y?.type).toBe('logarithmic');
    expect((latencyScatterOptions.scales?.x as { min?: number }).min).toBeGreaterThan(0);
    expect((latencyScatterOptions.scales?.y as { min?: number }).min).toBeGreaterThan(0);
    expect(latencyScatterOptions.scales?.x?.title?.text).toBe('usage_stats.ttft');
    expect(latencyScatterOptions.scales?.y?.title?.text).toBe('usage_stats.latency');
    expect(latencyScatterOptions.plugins?.tooltip?.callbacks?.label!({
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
    const fakeCanvas = { style: {} as Record<string, string>, title: '' };
    const lineStrokes: Array<{ lineWidth: number; strokeStyle: string; dash: number[] }> = [];
    const fakeCtx = {
      save: vi.fn(),
      restore: vi.fn(),
      lineWidth: 1,
      strokeStyle: '',
      dash: [] as number[],
      setLineDash(dash: number[]) { this.dash = dash; },
      beginPath: vi.fn(),
      moveTo: vi.fn(),
      lineTo: vi.fn(),
      stroke() {
        lineStrokes.push({ lineWidth: this.lineWidth, strokeStyle: this.strokeStyle, dash: [...this.dash] });
      },
      fillText: vi.fn(),
      measureText: vi.fn((text: string) => ({ width: text.length * 6 })),
      fillRect: vi.fn(),
      strokeRect: vi.fn(),
      fillStyle: '',
      font: '',
      textAlign: '',
      textBaseline: '',
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
    latencyPlugin!.afterEvent!(fakeChart as never, ttftHoverArgs as never, {} as never);
    expect(ttftHoverArgs.changed).toBe(true);
    expect(fakeCanvas.style.cursor).toBe('');
    expect(fakeCanvas.title).toBe('');
    latencyPlugin!.afterDatasetsDraw!(fakeChart as never, {} as never, {} as never);
    expect(lineStrokes.some((stroke) => stroke.strokeStyle === latencyPluginOptions!.colors!.p95TTFT && stroke.lineWidth > 1.4)).toBe(true);

    const latencyHoverArgs = {
      event: { type: 'mousemove', x: 260, y: 84, native: null },
      replay: false,
      cancelable: false,
      inChartArea: true,
      changed: false,
    };
    lineStrokes.length = 0;
    latencyPlugin!.afterEvent!(fakeChart as never, latencyHoverArgs as never, {} as never);
    expect(latencyHoverArgs.changed).toBe(true);
    expect(fakeCanvas.style.cursor).toBe('');
    expect(fakeCanvas.title).toBe('');
    latencyPlugin!.afterDatasetsDraw!(fakeChart as never, {} as never, {} as never);
    expect(lineStrokes.some((stroke) => stroke.strokeStyle === latencyPluginOptions!.colors!.p95Latency && stroke.lineWidth > 1.4)).toBe(true);

    const chartWithoutArea = {
      ...fakeChart,
      chartArea: undefined,
    };
    expect(() => latencyPlugin!.afterDatasetsDraw!(chartWithoutArea as never, {} as never, {} as never)).not.toThrow();

    const outArgs = {
      event: { type: 'mouseout', x: null, y: null, native: null },
      replay: false,
      cancelable: false,
      inChartArea: false,
      changed: false,
    };
    latencyPlugin!.afterEvent!(fakeChart as never, outArgs as never, {} as never);
    expect(outArgs.changed).toBe(true);
    expect(fakeCanvas.style.cursor).toBe('');
    expect(fakeCanvas.title).toBe('');
  });

  it('builds latency diagnostics log bounds without spreading large point arrays', () => {
    const points = Array.from({ length: 150_000 }, (_, index) => ({
      ttft_ms: index + 1,
      latency_ms: (index + 1) * 2,
    }));
    const latencyDiagnostics: AnalysisLatencyDiagnostics = {
      total_points: points.length,
      sampled: true,
      p95_ttft_ms: 142_500,
      p95_latency_ms: 285_000,
      max_ttft_ms: 150_000,
      max_latency_ms: 300_000,
      points,
      density: [],
    };

    renderToStaticMarkup(<AnalysisTestPanel analysis={emptyAnalysis} latencyDiagnostics={latencyDiagnostics} />);
    const latencyScatterIndex = chartCapture.scatterData.findIndex((data) => data.datasets[0]?.label === 'usage_stats.analysis_latency_samples');
    expect(latencyScatterIndex).toBeGreaterThanOrEqual(0);
    const latencyScatterOptions = chartCapture.scatterOptions[latencyScatterIndex];
    expect((latencyScatterOptions.scales?.x as { max?: number }).max).toBeGreaterThan(150_000);
    expect((latencyScatterOptions.scales?.y as { max?: number }).max).toBeGreaterThan(300_000);
  });

  it('renders model efficiency as cost per million total tokens against total tokens', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      model_efficiency: [
        efficiency(),
        efficiency({
          model: 'claude-sonnet',
          requests: 100,
          input_tokens: 1200,
          output_tokens: 500,
          cache_read_tokens: 200,
          reasoning_tokens: 50,
          total_tokens: 3_000_000,
          cost_usd: 4.5,
          output_tokens_per_request: 55,
        }),
        efficiency({
          model: 'gemini-pro',
          requests: 10000,
          input_tokens: 1500,
          output_tokens: 650,
          cache_read_tokens: 300,
          reasoning_tokens: 60,
          total_tokens: 4_000_000,
          cost_usd: 8,
          output_tokens_per_request: 40,
        }),
      ],
    };

    const markup = renderToStaticMarkup(<AnalysisTestPanel analysis={analysis} />);

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
    expect(pointRadii[0]).toBeGreaterThan(10);
    expect(pointRadii[1]).toBeGreaterThan(pointRadii[0]);
    expect(pointRadii[2]).toBe(24);
    expect(pointRadii[2] - pointRadii[1]).toBeGreaterThan(2);
    expect(modelScatterData.datasets[0]?.clip).toBe(false);
    expect((modelScatterOptions.scales?.x as { min?: number }).min).toBeLessThan(2_000_000);
    expect((modelScatterOptions.scales?.x as { max?: number }).max).toBeGreaterThan(9_000_000);
    expect((modelScatterOptions.scales?.y as { min?: number }).min).toBeLessThan(1);
    expect((modelScatterOptions.scales?.y as { max?: number }).max).toBeGreaterThan(4);
    expect(markup).not.toContain('gpt-4o');
    expect(markup).not.toContain('claude-sonnet');
    expect(markup).not.toContain('gemini-pro');
    const modelColors = modelScatterData.datasets[0]?.borderColor as string[];
    expect(new Set(modelColors)).toHaveProperty('size', 3);
    expect(modelScatterOptions.plugins?.tooltip?.enabled).toBe(false);
  });

  it('keeps each overlapped model name grouped with its own model efficiency values', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      model_efficiency: [
        efficiency(),
        efficiency({
          model: 'claude-sonnet',
          requests: 6,
          input_tokens: 1100,
          output_tokens: 400,
          cache_read_tokens: 120,
          reasoning_tokens: 30,
          cost_per_request_usd: 0.333,
          output_tokens_per_request: 72,
          cache_read_rate: 0.12,
        }),
      ],
    };

    renderToStaticMarkup(<AnalysisTestPanel analysis={analysis} />);

    const modelScatterIndex = chartCapture.scatterData.findIndex((data) => data.datasets[0]?.label === 'usage_stats.analysis_model_efficiency_title');
    expect(modelScatterIndex).toBeGreaterThanOrEqual(0);
    chartCapture.scatterOptions[modelScatterIndex]?.plugins?.tooltip?.external!({
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

    const tooltipElement = document.getElementById('analysis-model-efficiency-tooltip')!;
    const groups = tooltipElement.children;
    expect(groups).toHaveLength(2);
    expect([...groups[0].children].map((metric) => metric.textContent)).toEqual([
      'gpt-4o',
      'usage_stats.total_tokens: 2.00M',
      'usage_stats.analysis_cost_per_million_tokens: $1.00',
      'usage_stats.requests_count: 4',
    ]);
    expect([...groups[1].children].map((metric) => metric.textContent)).toEqual([
      'claude-sonnet',
      'usage_stats.total_tokens: 2.00M',
      'usage_stats.analysis_cost_per_million_tokens: $1.00',
      'usage_stats.requests_count: 6',
    ]);
  });

  it.each([
    { label: 'mouse', native: { clientX: 420, clientY: 300 }, left: '434px', top: '220px' },
    { label: 'touch', native: { touches: [{ clientX: 520, clientY: 360 }] }, left: '534px', top: '280px' },
  ])('positions the model efficiency tooltip from the native $label point', ({ native, left, top }) => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      model_efficiency: [
        efficiency(),
      ],
    };

    renderToStaticMarkup(<AnalysisTestPanel analysis={analysis} />);

    const modelScatterIndex = chartCapture.scatterData.findIndex((data) => data.datasets[0]?.label === 'usage_stats.analysis_model_efficiency_title');
    expect(modelScatterIndex).toBeGreaterThanOrEqual(0);
    const pointerPlugin = chartCapture.scatterPlugins[modelScatterIndex]?.find((plugin) => plugin.id === 'analysis-model-efficiency-tooltip-pointer');
    expect(pointerPlugin).toBeTruthy();

    const fakeChart = {
      canvas: {
        getBoundingClientRect: () => ({ left: 10, top: 20, right: 310, bottom: 320, width: 300, height: 300 }),
      },
    };
    pointerPlugin!.beforeEvent!(fakeChart as never, {
      event: { type: 'mousemove', x: 100, y: 60, native },
      replay: false,
      changed: false,
      cancelable: false,
      inChartArea: true,
    } as never, undefined as never);
    chartCapture.scatterOptions[modelScatterIndex]?.plugins?.tooltip?.external!({
      chart: fakeChart,
      tooltip: {
        opacity: 1,
        caretX: 100,
        caretY: 60,
        dataPoints: [{ dataIndex: 0 }],
      },
    } as never);

    const tooltipElement = document.getElementById('analysis-model-efficiency-tooltip')!;
    expect(tooltipElement?.style.opacity).toBe('1');
    expect(tooltipElement.style.left).toBe(left);
    expect(tooltipElement.style.top).toBe(top);
  });

  it('keeps partial cost values visible and shows pricing hints near analysis charts', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      token_usage: [tokenBucket({
        cost_available: false,
      })],
      api_key_composition: [composition({
        key: 'unpriced-key',
        label: 'Unpriced Key',
        requests: 3,
        input_tokens: 1000,
        output_tokens: 100,
        cache_read_tokens: 0,
        reasoning_tokens: 0,
        total_tokens: 1100,
        cost_usd: 0,
        cost_available: false,
      })],
      model_efficiency: [efficiency({
        model: 'unpriced-model',
        requests: 3,
        output_tokens: 100,
        cache_read_tokens: 0,
        reasoning_tokens: 0,
        total_tokens: 1_000_000,
        cost_usd: 0,
        cost_available: false,
        cost_per_request_usd: 0,
        output_tokens_per_request: 33.33,
        cache_read_rate: 0,
      })],
      cost_breakdown: {
        uncached_input_cost_usd: 0,
        output_cost_usd: 0,
        cache_read_cost_usd: 0,
        cache_write_cost_usd: 0,
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
          cache_read_tokens: 0,
          cache_creation_tokens: 0,
          reasoning_tokens: 0,
          total_tokens: 1100,
          requests: 3,
          cost_usd: 0,
          cost_available: false,
          intensity: 1,
        }],
      },
    };

    const container = renderAnalysisPanel({ analysis });
    const markup = container.innerHTML;

    const costDataset = chartCapture.barData?.datasets.find((dataset) => dataset.label === 'usage_stats.total_cost');
    expect(costDataset?.data).toEqual([0]);
    expect(chartCapture.scatterData).toHaveLength(0);
    expect(markup).toMatch(/Unpriced Key[\s\S]*\$0\.0000/);
    expect(markup).toContain('usage_stats.cost_need_price');
    expect(markup).not.toContain('usage_stats.analysis_token_usage_subtitle (usage_stats.cost_need_price)');
    expect([...container.querySelectorAll('h2')].filter((heading) => heading.parentElement?.textContent?.includes('usage_stats.cost_need_price'))).toHaveLength(4);
    expect(container.querySelector('[class*="analysisSummary"]')?.textContent).toContain('usage_stats.analysis_cost_per_million_tokens$0.0000');
    expect(markup).toContain('usage_stats.total_cost: $0.0000');
  });

  it('keeps partially priced summary rates visible under the token chart pricing hint', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      token_usage: [tokenBucket({
        cost_usd: 9,
        cost_available: false,
      })],
      cost_breakdown: {
        uncached_input_cost_usd: 9,
        output_cost_usd: 0,
        cache_read_cost_usd: 0,
        cache_write_cost_usd: 0,
        total_cost_usd: 9,
        cost_available: false,
      },
    };

    const container = renderAnalysisPanel({ analysis });
    const markup = container.innerHTML;

    const costDataset = chartCapture.barData?.datasets.find((dataset) => dataset.label === 'usage_stats.total_cost');
    expect(costDataset?.data).toEqual([9]);
    expect(markup).toContain('usage_stats.cost_need_price');
    expect([...container.querySelectorAll('[class*="analysisSummary"] dd')].map((value) => value.textContent)).toEqual(['1.10K', '$9.00', '$8,181.82']);
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
          cache_read_tokens: 100,
          cache_creation_tokens: 0,
          total_tokens: 1330,
          requests: 3,
          cost_usd: 0.1234,
          cost_available: true,
          intensity: 1,
        }],
      },
    };

    const markup = renderToStaticMarkup(<AnalysisTestPanel analysis={analysis} />);

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
    expect(markup).not.toContain('usage_stats.analysis_heatmap_tokens_prefix');
    expect(markup).not.toContain('usage_stats.analysis_heatmap_requests_prefix');
  });

  it('keeps rendering when an older analysis response omits heatmap', () => {
    const analysis = { ...emptyAnalysis, heatmap: undefined } as unknown as AnalysisResponse;

    expect(() => renderToStaticMarkup(<AnalysisTestPanel analysis={analysis} />)).not.toThrow();
  });
});
