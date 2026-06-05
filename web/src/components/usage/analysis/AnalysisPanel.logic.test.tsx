import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ChartData, ChartOptions } from 'chart.js';
import type { AnalysisResponse } from '@/lib/types';

const chartCapture = vi.hoisted(() => ({
  barData: null as ChartData<'bar', Array<number | null>, string> | null,
  barOptions: null as ChartOptions<'bar'> | null,
  doughnutData: null as ChartData<'doughnut', number[], string> | null,
  doughnutCount: 0,
  scatterData: null as ChartData<'scatter'> | null,
  scatterOptions: null as ChartOptions<'scatter'> | null,
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
  Scatter: (props: { data: ChartData<'scatter'>; options: ChartOptions<'scatter'> }) => {
    chartCapture.scatterData = props.data;
    chartCapture.scatterOptions = props.options;
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
};

describe('AnalysisPanel token chart data', () => {
  beforeEach(() => {
    chartCapture.barData = null;
    chartCapture.barOptions = null;
    chartCapture.doughnutData = null;
    chartCapture.doughnutCount = 0;
    chartCapture.scatterData = null;
    chartCapture.scatterOptions = null;
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

  it('renders cost breakdown with total beside blended rate, segment percentages and sparkline', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
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
    expect(markup).toContain('usage_stats.analysis_cost_per_million_tokens: $4.00');
    expect(markup).toContain('usage_stats.total_tokens: 500.00K');
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

    expect(chartCapture.scatterData?.datasets[0]?.label).toBe('usage_stats.analysis_model_efficiency_title');
    expect(chartCapture.scatterData?.datasets[0]?.data[0]).toMatchObject({ x: 2_000_000, y: 1 });
    expect(chartCapture.scatterOptions?.scales?.x?.type).toBe('logarithmic');
    expect(chartCapture.scatterOptions?.scales?.y?.type).toBe('logarithmic');
    expect(chartCapture.scatterOptions?.scales?.x).not.toHaveProperty('beginAtZero');
    expect(chartCapture.scatterOptions?.scales?.y).not.toHaveProperty('beginAtZero');
    const pointRadii = chartCapture.scatterData?.datasets[0]?.pointRadius as number[];
    expect(pointRadii[0]).toBe(5);
    expect(pointRadii[1]).toBeGreaterThan(10);
    expect(pointRadii[2]).toBe(24);
    expect(pointRadii[2] - pointRadii[1]).toBeGreaterThan(4);
    expect(chartCapture.scatterData?.datasets[0]?.clip).toBe(false);
    expect(chartCapture.scatterOptions?.layout?.padding).toEqual({ top: 16, right: 24, bottom: 22, left: 18 });
    expect((chartCapture.scatterOptions?.scales?.x as { min?: number }).min).toBeLessThan(2_000_000);
    expect((chartCapture.scatterOptions?.scales?.x as { max?: number }).max).toBeGreaterThan(9_000_000);
    expect((chartCapture.scatterOptions?.scales?.y as { min?: number }).min).toBeLessThan(1);
    expect((chartCapture.scatterOptions?.scales?.y as { max?: number }).max).toBeGreaterThan(4);
    expect(markup).not.toContain('gpt-4o');
    expect(markup).not.toContain('claude-sonnet');
    expect(markup).not.toContain('gemini-pro');
    const modelColors = chartCapture.scatterData?.datasets[0]?.borderColor as string[];
    expect(new Set(modelColors)).toHaveProperty('size', 3);
    expect(modelColors).not.toContain('#dc2626');
    expect(modelColors).not.toContain('#2563eb');
    expect(typeof chartCapture.scatterData?.datasets[0]?.backgroundColor).toBe('function');
    const gradient = {
      addColorStop: vi.fn(),
    };
    const createLinearGradient = vi.fn(() => gradient);
    const createRadialGradient = vi.fn();
    const fill = (chartCapture.scatterData?.datasets[0]?.backgroundColor as (context: unknown) => unknown)({
      dataIndex: 0,
      chart: { ctx: { createLinearGradient, createRadialGradient } },
      element: { x: 40, y: 50, options: { radius: 12 } },
    });
    expect(fill).toBe(gradient);
    expect(createRadialGradient).not.toHaveBeenCalled();
    expect(createLinearGradient).toHaveBeenCalledWith(28, 50, 52, 50);
    expect(gradient.addColorStop).toHaveBeenCalledWith(0, '#7898c8');
    expect(gradient.addColorStop).toHaveBeenCalledWith(1, '#5b7fb9');
    expect(chartCapture.scatterOptions?.plugins?.tooltip?.enabled).toBe(false);
    expect(typeof chartCapture.scatterOptions?.plugins?.tooltip?.external).toBe('function');
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

    chartCapture.scatterOptions?.plugins?.tooltip?.external?.({
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

  it('does not plot unavailable cost as zero in token and model efficiency charts', () => {
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
      model_efficiency: [{
        model: 'unpriced-model',
        requests: 3,
        input_tokens: 1000,
        output_tokens: 100,
        cached_tokens: 0,
        reasoning_tokens: 0,
        total_tokens: 1100,
        cost_usd: 0,
        cost_available: false,
        cost_per_request_usd: 0,
        output_tokens_per_request: 33.33,
        cache_rate: 0,
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
    expect(costDataset?.data).toEqual([null]);
    expect(chartCapture.scatterData).toBeNull();
    expect(markup).toContain('usage_stats.cost_need_price');
    expect(markup).toContain('usage_stats.analysis_cost_per_million_tokens</span><strong>usage_stats.cost_need_price</strong>');
    expect(markup).not.toContain('$8,181.82');
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
    expect(markup).toContain('title="claude-3-7-sonnet-20250219-long-context"');
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
