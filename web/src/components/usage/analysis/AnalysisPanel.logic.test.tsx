import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { AnalysisCompositionItem, AnalysisModelEfficiencyItem, AnalysisResponse } from '@/lib/types';
import { getChartTheme } from '@/lib/chartTheme';

type ChartConfig = Record<string, unknown>;
type ChartChild = Record<string, unknown> & {
  type?: string;
  data?: Array<Record<string, unknown>>;
  encode?: Record<string, unknown>;
  style?: Record<string, unknown>;
  state?: Record<string, unknown>;
  transform?: Array<Record<string, unknown>>;
  tooltip?: Record<string, unknown> | false;
};

const chartCapture = vi.hoisted(() => ({
  baseCalls: [] as ChartConfig[],
  lineCalls: [] as ChartConfig[],
  pieCalls: [] as ChartConfig[],
}));

vi.mock('@ant-design/charts', () => ({
  Base: (props: ChartConfig) => {
    chartCapture.baseCalls.push({ ...props });
    return React.createElement('div', { 'data-chart-kind': 'base' });
  },
  Line: (props: ChartConfig) => {
    chartCapture.lineCalls.push({ ...props });
    return React.createElement('div', { 'data-chart-kind': 'line' });
  },
  Pie: (props: ChartConfig) => {
    chartCapture.pieCalls.push({ ...props });
    return React.createElement('div', { 'data-chart-kind': 'pie' });
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

const emptyAnalysis: AnalysisResponse = {
  granularity: 'hourly',
  timezone: 'UTC',
  token_usage: [],
  api_key_composition: [],
  model_composition: [],
  auth_files_composition: [],
  ai_provider_composition: [],
  cost_breakdown: {
    uncached_input_cost_usd: 0,
    output_cost_usd: 0,
    cache_read_cost_usd: 0,
    cache_write_cost_usd: 0,
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

const renderPanel = (analysis: AnalysisResponse, isDark = false, isMobile = false) => renderToStaticMarkup(
  <AnalysisPanel analysis={analysis} loading={false} isDark={isDark} isMobile={isMobile} />,
);

const getChildren = (config: ChartConfig): ChartChild[] => (config.children ?? []) as ChartChild[];

const findBaseConfig = (predicate: (children: ChartChild[]) => boolean): ChartConfig => {
  const config = chartCapture.baseCalls.find((candidate) => predicate(getChildren(candidate)));
  if (!config) throw new Error('Expected composed chart config was not captured');
  return config;
};

const findChild = (config: ChartConfig, type: string): ChartChild => {
  const child = getChildren(config).find((candidate) => candidate.type === type);
  if (!child) throw new Error(`Expected ${type} child was not captured`);
  return child;
};

const getTooltipItems = (tooltip: ChartChild['tooltip']) => {
  if (!tooltip || tooltip === false) return [];
  return (tooltip.items ?? []) as Array<(datum: Record<string, unknown>) => Record<string, unknown>>;
};

const compositionItem = (key: string, totalTokens: number, percent: number, overrides: Partial<AnalysisCompositionItem> = {}): AnalysisCompositionItem => ({
  key,
  label: `Label ${key}`,
  total_tokens: totalTokens,
  requests: Math.max(1, Math.round(totalTokens / 100)),
  percent,
  input_tokens: totalTokens * 0.6,
  output_tokens: totalTokens * 0.2,
  cache_read_tokens: totalTokens * 0.1,
  cache_creation_tokens: totalTokens * 0.05,
  reasoning_tokens: totalTokens * 0.05,
  cost_usd: totalTokens / 10_000,
  cost_available: true,
  ...overrides,
});

const modelEfficiencyItem = (
  model: string,
  totalTokens: number,
  requests: number,
  costUsd: number,
  overrides: Partial<AnalysisModelEfficiencyItem> = {},
): AnalysisModelEfficiencyItem => ({
  model,
  total_tokens: totalTokens,
  requests,
  cost_usd: costUsd,
  cache_read_rate: 0.25,
  cost_available: true,
  ...overrides,
});

describe('AnalysisPanel page chrome', () => {
  it('does not render a top-level Analysis heading or refresh action', () => {
    const markup = renderPanel(emptyAnalysis);

    expect(markup).not.toContain('usage_stats.tab_analysis');
    expect(markup).not.toContain('usage_stats.refresh');
    expect(markup).not.toContain('ant-btn');
  });

  it('keeps inner card headings via SectionHeader', () => {
    const markup = renderPanel(emptyAnalysis);

    expect(markup).toContain('usage_stats.analysis_token_usage_title');
    expect(markup.match(/<h2/g)?.length ?? 0).toBeGreaterThan(0);
    expect(markup).not.toContain('<h1');
  });
});

describe('AnalysisPanel Ant Design Charts configs', () => {
  beforeEach(() => {
    chartCapture.baseCalls = [];
    chartCapture.lineCalls = [];
    chartCapture.pieCalls = [];
  });

  it('preserves token decomposition and raw tooltip values in single-axis small multiples', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      token_usage: [{
        bucket: '2026-05-28T01:00:00Z',
        input_tokens: 1000,
        output_tokens: 100,
        cache_read_tokens: 600,
        cache_creation_tokens: 100,
        reasoning_tokens: 50,
        total_tokens: 1150,
        requests: 3,
        cost_usd: 0.0123,
        cost_available: true,
      }],
    };

    const markup = renderPanel(analysis);
    const tokenConfig = findBaseConfig((children) => children.some((child) => child.type === 'interval'));
    const interval = findChild(tokenConfig, 'interval');
    const tokenData = interval.data ?? [];
    const bySeries = new Map(tokenData.map((datum) => [datum.series, datum]));

    expect(bySeries.get('usage_stats.input_tokens')).toMatchObject({ value: 300, rawValue: 1000, total: 1150 });
    expect(bySeries.get('usage_stats.cache_read_tokens')).toMatchObject({ value: 600, rawValue: 600 });
    expect(bySeries.get('usage_stats.cache_creation_tokens')).toMatchObject({ value: 100, rawValue: 100 });
    expect(bySeries.get('usage_stats.output_tokens')).toMatchObject({ value: 50, rawValue: 100 });
    expect(bySeries.get('usage_stats.reasoning_tokens')).toMatchObject({ value: 50, rawValue: 50 });
    expect(interval.transform).toEqual([{ type: 'stackY' }]);

    const inputTooltip = getTooltipItems(interval.tooltip)[0](bySeries.get('usage_stats.input_tokens') ?? {});
    const outputTooltip = getTooltipItems(interval.tooltip)[0](bySeries.get('usage_stats.output_tokens') ?? {});
    expect(inputTooltip).toMatchObject({
      name: 'usage_stats.input_tokens',
      value: '1.00K · usage_stats.total_tokens: 1.15K',
    });
    expect(outputTooltip).toMatchObject({
      name: 'usage_stats.output_tokens',
      value: '100 · usage_stats.total_tokens: 1.15K',
    });

    const tokenScale = tokenConfig.scale as Record<string, Record<string, unknown>>;
    expect(tokenScale.y).toMatchObject({ type: 'linear', domainMin: 0 });
    expect(tokenScale).not.toHaveProperty('requests');
    expect(tokenScale).not.toHaveProperty('cost');
    expect(chartCapture.lineCalls).toHaveLength(2);
    expect(chartCapture.lineCalls.map((config) => (config.tooltip as { items: Array<{ name: string }> }).items[0].name)).toEqual([
      'usage_stats.requests_count',
      'usage_stats.total_cost',
    ]);
    for (const lineConfig of chartCapture.lineCalls) {
      expect(lineConfig.scale).toEqual({ y: { type: 'linear', domainMin: 0, nice: true } });
    }
    expect(markup).toContain('usage_stats.requests_count');
    expect(markup).toContain('usage_stats.total_cost');
    expect(markup).toContain('<table');
  });

  it('keeps the average token reference label-free while exposing its value in the legend', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      token_usage: [
        { bucket: '2026-05-28T01:00:00Z', input_tokens: 100, output_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, reasoning_tokens: 0, total_tokens: 100, requests: 1, cost_usd: 0, cost_available: true },
        { bucket: '2026-05-28T02:00:00Z', input_tokens: 0, output_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, reasoning_tokens: 0, total_tokens: 0, requests: 0, cost_usd: 0, cost_available: true },
        { bucket: '2026-05-28T03:00:00Z', input_tokens: 400, output_tokens: 100, cache_read_tokens: 0, cache_creation_tokens: 0, reasoning_tokens: 0, total_tokens: 500, requests: 2, cost_usd: 0, cost_available: true },
      ],
    };

    const markup = renderPanel(analysis);
    const tokenConfig = findBaseConfig((children) => children.some((child) => child.type === 'interval'));
    const averageLine = findChild(tokenConfig, 'lineY');

    expect(averageLine.data).toEqual([{ value: 200 }]);
    expect(averageLine.style).toMatchObject({
      stroke: getChartTheme(false).averageLine,
      lineDash: [5, 5],
    });
    expect(averageLine.tooltip).toBe(false);
    expect(markup).toContain('usage_stats.analysis_token_average: 200');
  });

  it('formats token buckets in the analysis response timezone', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      timezone: 'America/New_York',
      token_usage: [{
        bucket: '2026-05-28T01:00:00Z',
        input_tokens: 10,
        output_tokens: 5,
        cache_read_tokens: 0,
        cache_creation_tokens: 0,
        reasoning_tokens: 0,
        total_tokens: 15,
        requests: 1,
        cost_usd: 0.01,
        cost_available: true,
      }],
    };

    renderPanel(analysis);
    const tokenConfig = findBaseConfig((children) => children.some((child) => child.type === 'interval'));
    expect(findChild(tokenConfig, 'interval').data?.[0]).toMatchObject({ label: '21:00' });
  });

  it('configures an Ant Design Charts donut and retains the accessible composition side list', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      range_start: '2026-05-28T00:00:00Z',
      range_end: '2026-05-28T02:00:00Z',
      api_key_composition: [compositionItem('primary', 1000, 100, { label: 'Primary Key', requests: 4, cost_usd: 0.42 })],
      model_composition: [compositionItem('gpt-4o', 1000, 100, { label: 'gpt-4o' })],
    };

    const markup = renderPanel(analysis);
    expect(chartCapture.pieCalls).toHaveLength(1);
    const pieConfig = chartCapture.pieCalls[0];

    expect(pieConfig).toMatchObject({
      angleField: 'totalTokens',
      colorField: 'key',
      radius: 0.88,
      innerRadius: 0.58,
      padding: 28,
      legend: false,
      animate: false,
    });
    expect(pieConfig.data).toEqual([expect.objectContaining({ key: 'primary', label: 'Primary Key', totalTokens: 1000 })]);
    expect(pieConfig.style).toMatchObject({ radius: 10, inset: 2, lineWidth: 2 });
    expect(pieConfig.state).toEqual({ active: { inset: -2, lineWidth: 3 } });
    expect(pieConfig.interaction).toMatchObject({ elementHighlight: true });
    expect(markup).toContain('Primary Key');
    expect(markup).toContain('compositionUsageList');
    expect(markup).toContain('compositionUsageTrack');
    expect(markup).toContain('compositionUsageMetaPill');
    expect(markup).toContain('usage_stats.rpm');
    expect(markup).toContain('0.03');
    expect(markup).toContain('usage_stats.tpm');
    expect(markup).toContain('8.33');
    expect(markup).not.toContain('gpt-4o');
  });

  it('wraps long composition tooltip titles and coerces non-string labels', () => {
    const longLabel = 'A very long usage distribution label that needs several lines before it can fit in the tooltip';
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      api_key_composition: [compositionItem('long', 1000, 100, { label: longLabel })],
    };

    renderPanel(analysis);
    const tooltip = chartCapture.pieCalls[0].tooltip as {
      title: (datum: Record<string, unknown>) => string;
      items: Array<(datum: Record<string, unknown>) => Record<string, unknown>>;
    };
    const titleLines = tooltip.title({ label: longLabel }).split('\n');

    expect(titleLines.length).toBeGreaterThan(1);
    expect(titleLines.length).toBeLessThanOrEqual(3);
    expect(titleLines.at(-1)).toMatch(/\.\.\.$/);
    expect(tooltip.title({ label: 123456789 })).toBe('123456789');
    expect(tooltip.items[0]({ label: longLabel, totalTokens: 1000, color: '#123456' })).toEqual({
      name: 'usage_stats.total_tokens',
      color: '#123456',
      value: '1.00K',
    });
  });

  it('collapses composition overflow into Others without rank-dependent recoloring', () => {
    const items = [
      compositionItem('one', 600, 40),
      compositionItem('two', 300, 20),
      compositionItem('three', 200, 13.33),
      compositionItem('four', 150, 10),
      compositionItem('five', 100, 6.67),
      compositionItem('six', 90, 6),
      compositionItem('seven', 60, 4),
    ];
    const analysis: AnalysisResponse = { ...emptyAnalysis, api_key_composition: items };

    renderPanel(analysis);
    const firstData = chartCapture.pieCalls[0].data as Array<Record<string, unknown>>;
    expect(firstData).toHaveLength(6);
    expect(firstData.at(-1)).toMatchObject({ key: '__others__', label: 'usage_stats.analysis_others', totalTokens: 150 });
    const retainedColor = firstData.find((datum) => datum.key === 'three')?.color;

    chartCapture.pieCalls = [];
    renderPanel({ ...emptyAnalysis, api_key_composition: [items[2], items[0], items[1]] });
    const secondData = chartCapture.pieCalls[0].data as Array<Record<string, unknown>>;
    expect(secondData.find((datum) => datum.key === 'three')?.color).toBe(retainedColor);
  });

  it('shows raw composition percentages while bounding visual bar width', () => {
    const markup = renderPanel({
      ...emptyAnalysis,
      api_key_composition: [compositionItem('overflow', 100, 120, { label: 'Overflow share' })],
    });

    expect(markup).toContain('120.00%');
    expect(markup).toContain('width:100%');
  });

  it('uses log latency axes, p95 references, raw tooltips and preserved point radii', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      latency_diagnostics: {
        points: [
          { ttft_ms: 12, latency_ms: 250 },
          { ttft_ms: 40, latency_ms: 800 },
        ],
        density: [],
        total_points: 2,
        sampled: true,
        p95_ttft_ms: 40,
        p95_latency_ms: 800,
        max_ttft_ms: 50,
        max_latency_ms: 900,
      },
    };

    const markup = renderPanel(analysis);
    const config = findBaseConfig((children) => children.some((child) => child.data?.some((datum) => 'ttft' in datum)));
    const scale = config.scale as Record<string, Record<string, unknown>>;
    const point = findChild(config, 'point');
    const lineX = findChild(config, 'lineX');
    const lineY = findChild(config, 'lineY');

    expect(scale.x).toMatchObject({ type: 'log', nice: false });
    expect(scale.y).toMatchObject({ type: 'log', nice: false });
    expect(point.style).toMatchObject({ r: 3, stroke: 'transparent' });
    expect(point.state).toMatchObject({ active: { r: 5 } });
    expect(lineX).toMatchObject({ data: [{ value: 40 }], encode: { x: 'value' } });
    expect(lineY).toMatchObject({ data: [{ value: 800 }], encode: { y: 'value' } });
    const tooltipItems = getTooltipItems(point.tooltip);
    expect(tooltipItems[0]({ ttft: 12, latency: 250 })).toMatchObject({ name: 'usage_stats.ttft', value: '12ms' });
    expect(tooltipItems[1]({ ttft: 12, latency: 250 })).toMatchObject({ name: 'usage_stats.latency', value: '250ms' });
    expect(markup).toContain('usage_stats.analysis_latency_samples');
    expect(markup).toContain('usage_stats.analysis_latency_p95_ttft');
    expect(markup).toContain('usage_stats.analysis_latency_sampled');
  });

  it('builds latency log bounds iteratively for very large point arrays', () => {
    const points = Array.from({ length: 150_000 }, (_, index) => ({
      ttft_ms: index + 1,
      latency_ms: index + 10,
    }));
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      latency_diagnostics: {
        points,
        density: [],
        total_points: points.length,
        sampled: true,
        p95_ttft_ms: 142_500,
        p95_latency_ms: 142_510,
        max_ttft_ms: 150_000,
        max_latency_ms: 150_009,
      },
    };

    expect(() => renderPanel(analysis)).not.toThrow();
    const config = findBaseConfig((children) => children.some((child) => child.data?.some((datum) => 'ttft' in datum)));
    const scale = config.scale as Record<string, Record<string, number>>;
    expect(scale.x.domainMin).toBe(1);
    expect(scale.x.domainMax).toBeGreaterThan(150_000);
    expect(scale.y.domainMax).toBeGreaterThan(150_009);
  });

  it('uses theme-aware series colors for latency marks and p95 references', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      latency_diagnostics: {
        points: [{ ttft_ms: 10, latency_ms: 200 }],
        density: [],
        total_points: 1,
        sampled: false,
        p95_ttft_ms: 10,
        p95_latency_ms: 200,
        max_ttft_ms: 10,
        max_latency_ms: 200,
      },
    };

    renderPanel(analysis, true);
    const config = findBaseConfig((children) => children.some((child) => child.data?.some((datum) => 'ttft' in datum)));
    const theme = getChartTheme(true);
    expect(findChild(config, 'point').style).toMatchObject({ fill: theme.series.indigo.fill });
    expect(findChild(config, 'lineX').style).toMatchObject({ stroke: theme.series.cyan.stroke });
    expect(findChild(config, 'lineY').style).toMatchObject({ stroke: theme.series.violet.stroke });
  });

  it('retains the native cost breakdown summaries and segment metadata', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      token_usage: [{
        bucket: '2026-05-28T01:00:00Z',
        input_tokens: 1000,
        output_tokens: 200,
        cache_read_tokens: 300,
        cache_creation_tokens: 100,
        reasoning_tokens: 50,
        total_tokens: 1250,
        requests: 4,
        cost_usd: 0.5,
        cost_available: true,
      }],
      cost_breakdown: {
        uncached_input_cost_usd: 0.1,
        cache_read_cost_usd: 0.05,
        cache_write_cost_usd: 0.15,
        output_cost_usd: 0.2,
        total_cost_usd: 0.5,
        cost_available: true,
      },
    };

    const markup = renderPanel(analysis);
    expect(markup).toContain('usage_stats.analysis_cost_breakdown_title');
    expect(markup).toContain('usage_stats.total_tokens</span><strong>1.25K');
    expect(markup).toContain('usage_stats.total_cost</span><strong>$0.50');
    expect(markup).toContain('usage_stats.analysis_cost_per_million_tokens</span><strong>$400.00');
    expect(markup).toContain('aria-label="usage_stats.input_tokens · usage_stats.analysis_cost_share');
    expect(markup).toContain('usage_stats.analysis_cost_share: 20.00%');
    expect(markup).toContain('usage_stats.total_tokens: 600');
    expect(markup).toContain('usage_stats.analysis_cost_per_million_tokens: $166.67');
  });

  it('uses log model-efficiency axes, request-derived radii, legends, tooltips and a table alternative', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      model_efficiency: [
        modelEfficiencyItem('model-small', 1_000, 1, 0.01),
        modelEfficiencyItem('model-mid', 10_000, 100, 0.2),
        modelEfficiencyItem('model-large', 100_000, 10_000, 4),
      ],
    };

    const markup = renderPanel(analysis);
    const config = findBaseConfig((children) => children.some((child) => child.data?.some((datum) => 'model' in datum)));
    const scale = config.scale as Record<string, Record<string, unknown>>;
    const point = findChild(config, 'point');
    const data = point.data as Array<Record<string, number | string>>;

    expect(scale.x).toMatchObject({ type: 'log', tickCount: 5, nice: false });
    expect(scale.y).toMatchObject({ type: 'log', tickCount: 5, nice: false });
    expect(scale.size).toEqual({ type: 'identity' });
    expect(point.encode).toMatchObject({ size: 'radius' });
    expect(data.map((datum) => datum.costPerMillion)).toEqual([10, 20, 40]);
    expect(Number(data[0].radius)).toBe(5);
    expect(Number(data[2].radius)).toBe(24);
    expect(Number(data[1].radius)).toBeGreaterThan(5);
    expect(Number(data[1].radius)).toBeLessThan(24);
    const radius = point.style?.r as (datum: Record<string, unknown>) => number;
    const hoverRadius = (point.state as { active: { r: (datum: Record<string, unknown>) => number } }).active.r;
    expect(radius(data[1])).toBe(data[1].radius);
    expect(hoverRadius(data[1])).toBe(data[1].hoverRadius);

    const tooltipItems = getTooltipItems(point.tooltip);
    expect(tooltipItems[0](data[1])).toMatchObject({ name: 'usage_stats.total_tokens', value: '10.00K' });
    expect(tooltipItems[1](data[1])).toMatchObject({ name: 'usage_stats.analysis_cost_per_million_tokens', value: '$20.00' });
    expect(tooltipItems[2](data[1])).toMatchObject({ name: 'usage_stats.requests_count', value: '100' });
    expect(markup).toContain('model-small');
    expect(markup).toContain('model-mid');
    expect(markup).toContain('model-large');
    expect(markup).toContain('<table');
  });

  it('compresses model-efficiency outliers and keeps model colors stable across filtering', () => {
    const rows = [
      modelEfficiencyItem('steady-model', 10_000, 10, 0.2),
      modelEfficiencyItem('middle-model', 20_000, 100, 0.5),
      modelEfficiencyItem('outlier-model', 30_000, 1_000_000, 0.9),
    ];
    renderPanel({ ...emptyAnalysis, model_efficiency: rows });
    const firstConfig = findBaseConfig((children) => children.some((child) => child.data?.some((datum) => 'model' in datum)));
    const firstData = findChild(firstConfig, 'point').data as Array<Record<string, number | string>>;
    const steadyColor = firstData.find((datum) => datum.model === 'steady-model')?.color;
    const middleRadius = Number(firstData.find((datum) => datum.model === 'middle-model')?.radius);
    expect(middleRadius).toBeGreaterThan(5);
    expect(middleRadius).toBeLessThan(24);

    chartCapture.baseCalls = [];
    renderPanel({ ...emptyAnalysis, model_efficiency: [rows[2], rows[0]] });
    const secondConfig = findBaseConfig((children) => children.some((child) => child.data?.some((datum) => 'model' in datum)));
    const secondData = findChild(secondConfig, 'point').data as Array<Record<string, number | string>>;
    expect(secondData.find((datum) => datum.model === 'steady-model')?.color).toBe(steadyColor);
  });

  it('keeps partially priced values visible with one model-specific pricing notice', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      token_usage: [{
        bucket: '2026-05-28T01:00:00Z',
        input_tokens: 100,
        output_tokens: 50,
        cache_read_tokens: 0,
        cache_creation_tokens: 0,
        reasoning_tokens: 0,
        total_tokens: 150,
        requests: 1,
        cost_usd: 0.25,
        cost_available: false,
      }],
      api_key_composition: [compositionItem('partial', 150, 100, { cost_usd: 0.25, cost_available: false })],
      model_composition: [{
        ...compositionItem('*********', 150, 100, { cost_usd: 0.25, cost_available: false }),
        label: 'unpriced',
      }],
      cost_breakdown: {
        uncached_input_cost_usd: 0.1,
        cache_read_cost_usd: 0,
        cache_write_cost_usd: 0,
        output_cost_usd: 0.15,
        total_cost_usd: 0.25,
        cost_available: false,
      },
      model_efficiency: [modelEfficiencyItem('unpriced', 150, 1, 0.25, { cost_available: false })],
    };

    const markup = renderPanel(analysis);
    expect(markup).not.toContain('usage_stats.cost_need_price');
    expect(markup).toContain('usage_stats.pricing_coverage_title');
    expect(markup).toContain('<code>unpriced</code>');
    expect(markup).not.toContain('<code>*********</code>');
    expect(markup).toContain('usage_stats.total_cost</span><strong>$0.25');
    expect(markup).toContain('usage_stats.analysis_cost_per_million_tokens</span><strong>$1,666.67');
    expect(markup).toContain('usage_stats.no_data');
  });

  it('derives heatmap intensity from total tokens while keeping id keys and display labels', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      heatmap: {
        api_keys: ['key-id'],
        api_key_labels: { 'key-id': 'Friendly Key' },
        models: ['model-a', 'model-b'],
        cells: [
          { api_key: 'key-id', model: 'model-a', requests: 2, input_tokens: 20, output_tokens: 10, reasoning_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, total_tokens: 30, cost_usd: 0.1, cost_available: true, intensity: 1 },
          { api_key: 'key-id', model: 'model-b', requests: 1, input_tokens: 100, output_tokens: 50, reasoning_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, total_tokens: 150, cost_usd: 0.2, cost_available: true, intensity: 0 },
        ],
      },
    };

    const markup = renderPanel(analysis);
    expect(markup).toContain('Friendly Key');
    expect(markup).toContain('model-a');
    expect(markup).toContain('model-b');
    expect(markup).toContain('usage_stats.total_tokens: 30');
    expect(markup).toContain('usage_stats.total_tokens: 150');
    expect(markup).toContain('tabindex="0"');
    expect(markup).toContain('rgb(254, 206, 155)');
    expect(markup).toContain('rgb(239, 68, 68)');
  });

  it('keeps dark heatmap low cells visible and preserves the high red stop', () => {
    const analysis: AnalysisResponse = {
      ...emptyAnalysis,
      heatmap: {
        api_keys: ['key-id'],
        api_key_labels: { 'key-id': 'Friendly Key' },
        models: ['low', 'high'],
        cells: [
          { api_key: 'key-id', model: 'low', requests: 0, input_tokens: 0, output_tokens: 0, reasoning_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, total_tokens: 0, cost_usd: 0, cost_available: true, intensity: 1 },
          { api_key: 'key-id', model: 'high', requests: 1, input_tokens: 100, output_tokens: 0, reasoning_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, total_tokens: 100, cost_usd: 0.1, cost_available: true, intensity: 0 },
        ],
      },
    };

    const markup = renderPanel(analysis, true);
    expect(markup).toContain('rgb(58, 36, 48)');
    expect(markup).toContain('rgb(239, 68, 68)');
  });

  it('keeps loading, empty, and older heatmap responses renderable', () => {
    const olderAnalysis = { ...emptyAnalysis } as AnalysisResponse & { heatmap?: AnalysisResponse['heatmap'] };
    delete olderAnalysis.heatmap;

    expect(() => renderPanel(olderAnalysis)).not.toThrow();
    const emptyMarkup = renderPanel(olderAnalysis);
    expect(emptyMarkup).toContain('usage_stats.no_data');
    const loadingMarkup = renderToStaticMarkup(
      <AnalysisPanel analysis={olderAnalysis} loading isDark={false} isMobile={false} />,
    );
    expect(loadingMarkup).toContain('common.loading');
  });
});
