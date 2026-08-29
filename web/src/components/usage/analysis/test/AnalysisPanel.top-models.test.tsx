// @vitest-environment happy-dom

import React, { act } from 'react';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { BasicPlatform, Chart } from 'chart.js/auto';
import { createRoot } from 'react-dom/client';
import { renderToStaticMarkup } from 'react-dom/server';
import type { ChartData, ChartOptions, Plugin } from 'chart.js';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { AnalysisResponse, AnalysisTokenUsageBucket } from '@/lib/types';

type CapturedBar = {
  data: ChartData<'bar', Array<number | null>, string>;
  options: ChartOptions<'bar'>;
  plugins?: Plugin<'bar'>[];
};

type RecordedGradient = {
  readonly [Symbol.toStringTag]: 'CanvasGradient';
  stops: Array<[number, string]>;
  addColorStop: (offset: number, color: string) => void;
};

const chartCapture = vi.hoisted(() => ({
  bars: [] as CapturedBar[],
}));

vi.mock('react-chartjs-2', () => ({
  Bar: (props: CapturedBar) => {
    chartCapture.bars.push(props);
    return React.createElement('div');
  },
  Doughnut: () => React.createElement('div'),
  Scatter: () => React.createElement('div'),
}));

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => {} },
  useTranslation: () => ({ t: (key: string) => key }),
}));

import { AnalysisPanel } from '../AnalysisPanel';

const tokenBucket = (bucket: string, totalTokens: number): AnalysisTokenUsageBucket => ({
  bucket,
  input_tokens: totalTokens,
  output_tokens: 0,
  cache_read_tokens: 0,
  cache_creation_tokens: 0,
  reasoning_tokens: 0,
  total_tokens: totalTokens,
  requests: 1,
  cost_usd: 0,
  cost_available: true,
});

const baseAnalysis = (granularity: AnalysisResponse['granularity'], buckets: string[]): AnalysisResponse => ({
  granularity,
  timezone: 'Asia/Shanghai',
  token_usage: buckets.map((bucket) => tokenBucket(bucket, 1)),
  model_usage: {
    buckets,
    series: [],
  },
  api_key_composition: [],
  model_composition: [],
  auth_files_composition: [],
  ai_provider_composition: [],
  cost_breakdown: {
    uncached_input_cost_usd: 0,
    cache_read_cost_usd: 0,
    cache_write_cost_usd: 0,
    output_cost_usd: 0,
    total_cost_usd: 0,
    cost_available: true,
  },
  model_efficiency: [],
  heatmap: { api_keys: [], api_key_labels: {}, models: [], cells: [] },
});

const findTopModelsBar = () => chartCapture.bars.find((bar) =>
  bar.data.datasets.some((dataset) => dataset.label === 'model-alpha'),
);

const findLatestTopModelsBar = () => [...chartCapture.bars].reverse().find((bar) =>
  bar.data.datasets.some((dataset) => dataset.label === 'model-alpha'),
);

const createFakeChartCanvas = (): HTMLCanvasElement => {
  const canvas = {
    width: 500,
    height: 310,
    style: {},
    clientWidth: 500,
    clientHeight: 310,
    getAttribute: () => null,
    setAttribute: () => {},
    removeAttribute: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    getContext: () => context,
  };
  const contextTarget: Record<PropertyKey, unknown> = {
    canvas,
    measureText: (text: unknown) => ({
      width: String(text).length * 6,
      actualBoundingBoxLeft: 0,
      actualBoundingBoxRight: String(text).length * 6,
      actualBoundingBoxAscent: 8,
      actualBoundingBoxDescent: 2,
    }),
    createLinearGradient: (): RecordedGradient => {
      const stops: Array<[number, string]> = [];
      return {
        [Symbol.toStringTag]: 'CanvasGradient',
        stops,
        addColorStop: (offset, color) => stops.push([offset, color]),
      };
    },
    getLineDash: () => [],
  };
  const context = new Proxy(contextTarget, {
    get: (target, property) => Reflect.has(target, property) ? Reflect.get(target, property) : () => {},
    set: (target, property, value) => {
      Reflect.set(target, property, value);
      return true;
    },
  }) as unknown as CanvasRenderingContext2D;
  return canvas as unknown as HTMLCanvasElement;
};

const getRecordedGradientStops = (fill: unknown): Array<[number, string]> => {
  if (!fill || typeof fill !== 'object') return [];
  const stops = (fill as Partial<RecordedGradient>).stops;
  return Array.isArray(stops) ? stops : [];
};

const readVerticalGradientStops = (backgroundColor: unknown) => {
  expect(typeof backgroundColor).toBe('function');
  const stops: Array<[number, string]> = [];
  const gradient = {
    addColorStop: (offset: number, color: string) => stops.push([offset, color]),
  };
  const createLinearGradient = vi.fn(() => gradient);
  const fill = (backgroundColor as (context: unknown) => unknown)({
    chart: { ctx: { createLinearGradient }, chartArea: { top: 0, bottom: 100 } },
  });
  expect(fill).toBe(gradient);
  expect(createLinearGradient).toHaveBeenCalledWith(0, 0, 0, 100);
  return stops;
};

describe('AnalysisPanel Top Models card', () => {
  beforeEach(() => {
    chartCapture.bars = [];
  });

  it('builds a stable whole-range Top 5 and merges every remaining model into Others', () => {
    const buckets = ['2026-08-01T01:00:00Z', '2026-08-01T02:00:00Z'];
    const analysis = baseAnalysis('hourly', buckets);
    analysis.model_usage.series = [
      { model: 'model-gamma', total_tokens: [40, 40], requests: [1, 1] },
      { model: 'model-eta', total_tokens: [0, 40], requests: [0, 1] },
      { model: 'model-alpha', total_tokens: [100, 0], requests: [1, 0] },
      { model: 'model-zeta', total_tokens: [50, 0], requests: [1, 0] },
      { model: 'model-beta', total_tokens: [0, 90], requests: [0, 1] },
      { model: 'model-delta', total_tokens: [70, 0], requests: [1, 0] },
      { model: 'model-epsilon', total_tokens: [0, 60], requests: [0, 1] },
    ];

    const markup = renderToStaticMarkup(
      <AnalysisPanel analysis={analysis} loading={false} isDark={false} isMobile={false} />,
    );

    const topModelsStart = markup.indexOf('usage_stats.analysis_top_models_title');
    const latencyStart = markup.indexOf('usage_stats.analysis_latency_title');
    expect(topModelsStart).toBeGreaterThan(markup.indexOf('usage_stats.analysis_model_efficiency_title'));
    expect(topModelsStart).toBeLessThan(latencyStart);

    const topModelsBar = findTopModelsBar();
    expect(topModelsBar?.data.labels).toEqual(['09:00', '10:00']);
    expect(topModelsBar?.data.datasets.map((dataset) => dataset.label)).toEqual([
      'model-alpha',
      'model-beta',
      'model-gamma',
      'model-delta',
      'model-epsilon',
      'usage_stats.analysis_others',
    ]);
    expect(topModelsBar?.data.datasets[0]?.data).toEqual([100, null]);
    expect(topModelsBar?.data.datasets.at(-1)?.data).toEqual([50, 40]);
    expect(topModelsBar?.data.datasets.map((dataset) => readVerticalGradientStops(dataset.backgroundColor))).toEqual([
      [[0, '#f9a8d4'], [1, '#db2777']],
      [[0, '#fcd34d'], [1, '#d97706']],
      [[0, '#6ee7b7'], [1, '#059669']],
      [[0, '#93c5fd'], [1, '#2563eb']],
      [[0, '#fca5a5'], [1, '#dc2626']],
      [[0, '#cbd5e1'], [1, '#64748b']],
    ]);
    expect(topModelsBar?.data.datasets.map((dataset) => dataset.minBarLength)).toEqual([4, 4, 4, 4, 4, 4]);
    expect(topModelsBar?.options.scales?.x?.stacked).toBe(true);
    expect(topModelsBar?.options.scales?.tokens?.stacked).toBe(true);

    const cardMarkup = markup.slice(topModelsStart, latencyStart);
    expect(cardMarkup).toContain('<button');
    expect(cardMarkup).toContain('aria-label="1. model-alpha');
    expect(cardMarkup).toContain('aria-label="6. usage_stats.analysis_others');
    expect(cardMarkup).not.toContain('model-zeta');
    expect(cardMarkup).not.toContain('model-eta');
  });

  it('uses response timezone for daily labels and reports model share plus bucket total in tooltip', () => {
    const buckets = ['2026-07-31T16:00:00Z', '2026-08-01T16:00:00Z'];
    const analysis = baseAnalysis('daily', buckets);
    analysis.model_usage.series = [
      { model: 'model-alpha', total_tokens: [75, 25], requests: [2, 1] },
      { model: 'model-beta', total_tokens: [25, 75], requests: [1, 2] },
    ];
    renderToStaticMarkup(
      <AnalysisPanel analysis={analysis} loading={false} isDark isMobile={false} />,
    );

    const topModelsBar = findTopModelsBar();
    expect(topModelsBar?.data.labels).toEqual(['8/1', '8/2']);
    const tooltip = topModelsBar?.options.plugins?.tooltip;
    const label = tooltip?.callbacks?.label as ((context: unknown) => string) | undefined;
    const footer = tooltip?.callbacks?.footer as ((items: unknown[]) => string) | undefined;
    const filter = tooltip?.filter as ((context: unknown) => boolean) | undefined;
    expect(label?.({ dataset: { label: 'model-alpha' }, dataIndex: 0, parsed: { y: 75 } })).toContain('75.00%');
    expect(footer?.([{ dataIndex: 0 }])).toContain('100');
    expect(filter?.({ parsed: { y: 0 } })).toBe(false);
  });

  it('sorts each tooltip by that bucket token usage and shares Token Usage tooltip spacing', () => {
    const buckets = ['2026-08-01T01:00:00Z'];
    const analysis = baseAnalysis('hourly', buckets);
    analysis.model_usage.series = [
      { model: 'model-alpha', total_tokens: [843.43], requests: [1] },
      { model: 'model-beta', total_tokens: [26.19], requests: [1] },
      { model: 'model-gamma', total_tokens: [171.04], requests: [1] },
    ];
    renderToStaticMarkup(
      <AnalysisPanel analysis={analysis} loading={false} isDark={false} isMobile={false} />,
    );

    const tokenTooltip = chartCapture.bars[0]?.options.plugins?.tooltip;
    const topModelsTooltip = findTopModelsBar()?.options.plugins?.tooltip;
    const itemSort = topModelsTooltip?.itemSort as ((left: unknown, right: unknown, data: unknown) => number) | undefined;
    expect(typeof itemSort).toBe('function');
    const items = [
      { label: 'model-beta', datasetIndex: 1, parsed: { y: 26.19 } },
      { label: 'model-gamma', datasetIndex: 2, parsed: { y: 171.04 } },
      { label: 'model-alpha', datasetIndex: 0, parsed: { y: 843.43 } },
    ];
    expect(items.sort((left, right) => itemSort?.(left, right, {}) ?? 0).map((item) => item.label)).toEqual([
      'model-alpha',
      'model-gamma',
      'model-beta',
    ]);
    expect(topModelsTooltip?.bodySpacing).toBe(2);
    expect(topModelsTooltip?.bodySpacing).toBe(tokenTooltip?.bodySpacing);
    expect(topModelsTooltip?.footerMarginTop).toBe(tokenTooltip?.footerMarginTop);
    expect(topModelsTooltip?.padding).toEqual(tokenTooltip?.padding);
  });

  it('keeps every non-zero stacked segment visible after Chart.js clipping', () => {
    const buckets = ['2026-08-01T01:00:00Z'];
    const analysis = baseAnalysis('hourly', buckets);
    analysis.token_usage = [tokenBucket(buckets[0], 1_000)];
    analysis.model_usage.series = [
      { model: 'model-alpha', total_tokens: [995], requests: [1] },
      { model: 'model-beta', total_tokens: [1], requests: [1] },
      { model: 'model-gamma', total_tokens: [1], requests: [1] },
      { model: 'model-delta', total_tokens: [1], requests: [1] },
      { model: 'model-epsilon', total_tokens: [1], requests: [1] },
      { model: 'model-zeta', total_tokens: [1], requests: [1] },
    ];
    renderToStaticMarkup(
      <AnalysisPanel analysis={analysis} loading={false} isDark={false} isMobile={false} />,
    );

    const topModelsBar = findTopModelsBar();
    expect(topModelsBar).toBeDefined();
    const chart = new Chart(createFakeChartCanvas(), {
      type: 'bar',
      data: topModelsBar?.data ?? { labels: [], datasets: [] },
      platform: BasicPlatform,
      options: {
        ...topModelsBar?.options,
        responsive: false,
        animation: false,
      },
    });
    try {
      const visibleHeights = chart.data.datasets.map((_, index) => {
        const element = chart.getDatasetMeta(index).data[0] as unknown as { y: number; base: number };
        const visibleTop = Math.max(element.y, chart.chartArea.top);
        const visibleBottom = Math.min(element.base, chart.chartArea.bottom);
        return Math.max(0, visibleBottom - visibleTop);
      });
      expect(visibleHeights).toHaveLength(6);
      expect(visibleHeights.every((height) => height >= 3.99)).toBe(true);
    } finally {
      chart.destroy();
    }
  });

  it('shows card-local loading and empty states', () => {
    const analysis = baseAnalysis('hourly', []);
    const loadingMarkup = renderToStaticMarkup(
      <AnalysisPanel analysis={analysis} loading isDark={false} isMobile />,
    );
    const loadingStart = loadingMarkup.indexOf('usage_stats.analysis_top_models_title');
    const loadingEnd = loadingMarkup.indexOf('usage_stats.analysis_latency_title');
    expect(loadingMarkup.slice(loadingStart, loadingEnd)).toContain('common.loading');

    const emptyMarkup = renderToStaticMarkup(
      <AnalysisPanel analysis={analysis} loading={false} isDark={false} isMobile />,
    );
    const emptyStart = emptyMarkup.indexOf('usage_stats.analysis_top_models_title');
    const emptyEnd = emptyMarkup.indexOf('usage_stats.analysis_latency_title');
    expect(emptyMarkup.slice(emptyStart, emptyEnd)).toContain('usage_stats.no_data');
  });

  it('prioritizes keyboard focus over another hovered ranking item', () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    const buckets = ['2026-08-01T01:00:00Z'];
    const analysis = baseAnalysis('hourly', buckets);
    analysis.model_usage.series = [
      { model: 'model-alpha', total_tokens: [100], requests: [1] },
      { model: 'model-beta', total_tokens: [50], requests: [1] },
    ];
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);
    try {
      act(() => root.render(
        <AnalysisPanel analysis={analysis} loading={false} isDark={false} isMobile={false} />,
      ));
      const buttons = Array.from(container.querySelectorAll('button'));
      const alphaButton = buttons.find((item) => item.getAttribute('aria-label')?.startsWith('1. model-alpha'));
      const betaButton = buttons.find((item) => item.getAttribute('aria-label')?.startsWith('2. model-beta'));
      expect(alphaButton).toBeDefined();
      expect(betaButton).toBeDefined();

      act(() => alphaButton?.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })));
      act(() => betaButton?.focus());
      expect(document.activeElement).toBe(betaButton);
      expect(findLatestTopModelsBar()?.data.datasets[0]?.borderWidth).toBe(0);
      expect(findLatestTopModelsBar()?.data.datasets[1]?.borderWidth).toBe(1.5);

      act(() => alphaButton?.dispatchEvent(new MouseEvent('mouseout', { bubbles: true, relatedTarget: document.body })));
      expect(document.activeElement).toBe(betaButton);
      expect(findLatestTopModelsBar()?.data.datasets[1]?.borderWidth).toBe(1.5);
    } finally {
      act(() => root.unmount());
      container.remove();
    }

    const panelSource = readFileSync(resolve(process.cwd(), 'src/components/usage/analysis/AnalysisPanel.tsx'), 'utf8');
    const panelStyles = readFileSync(resolve(process.cwd(), 'src/components/usage/analysis/AnalysisPanel.module.scss'), 'utf8');
    expect(panelSource).toContain('setHoveredModel');
    expect(panelSource).toContain('setFocusedModel');
    expect(panelSource).toContain('onMouseEnter');
    expect(panelSource).toContain('onFocus');
    expect(panelStyles).toMatch(/\.topModelsRankItem:focus-visible/);
    expect(panelStyles).toContain('outline: 2px solid var(--text-primary);');
    expect(panelStyles).not.toMatch(/\[data-muted='true'\]\s*\{\s*opacity:/);
    expect(panelStyles).toMatch(/\.topModelsRankItem\[data-muted='true'\] \.topModelsColor/);
    expect(panelStyles).toContain('@media (prefers-reduced-motion: reduce)');
  });

  it('keeps non-focused datasets muted while the chart bucket is active', () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    const buckets = ['2026-08-01T01:00:00Z'];
    const analysis = baseAnalysis('hourly', buckets);
    analysis.model_usage.series = [
      { model: 'model-alpha', total_tokens: [100], requests: [1] },
      { model: 'model-beta', total_tokens: [50], requests: [1] },
    ];
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);
    let chart: Chart<'bar', Array<number | null>, string> | undefined;
    try {
      act(() => root.render(
        <AnalysisPanel analysis={analysis} loading={false} isDark={false} isMobile={false} />,
      ));
      const betaButton = Array.from(container.querySelectorAll('button'))
        .find((item) => item.getAttribute('aria-label')?.startsWith('2. model-beta'));
      act(() => betaButton?.focus());

      const topModelsBar = findLatestTopModelsBar();
      expect(topModelsBar).toBeDefined();
      chart = new Chart(createFakeChartCanvas(), {
        type: 'bar',
        data: topModelsBar?.data ?? { labels: [], datasets: [] },
        platform: BasicPlatform,
        options: {
          ...topModelsBar?.options,
          responsive: false,
          animation: false,
        },
      });
      chart.setActiveElements([
        { datasetIndex: 0, index: 0 },
        { datasetIndex: 1, index: 0 },
      ]);

      const alphaElement = chart.getDatasetMeta(0).data[0] as unknown as { options: { backgroundColor?: unknown } };
      expect(getRecordedGradientStops(alphaElement.options.backgroundColor)).toEqual([
        [0, '#f9a8d433'],
        [1, '#db277733'],
      ]);
    } finally {
      chart?.destroy();
      act(() => root.unmount());
      container.remove();
    }
  });
});
