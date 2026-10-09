// @vitest-environment happy-dom

import React, { act } from 'react';
import { Chart } from 'chart.js';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { AnalysisCompositionItem, AnalysisResponse } from '@/lib/types';
import i18n from '@/i18n';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

vi.mock('react-chartjs-2', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-chartjs-2')>();
  return {
    Bar: () => <div />,
    Scatter: () => <div />,
    // 使用真实的 React 包装器和 Chart.js，只关闭依赖浏览器布局的响应式尺寸与动画。
    Doughnut: (props: React.ComponentProps<typeof actual.Doughnut>) => (
      <actual.Doughnut {...props} width={500} height={280} options={{ ...props.options, responsive: false, animation: false }} />
    ),
  };
});

import { AnalysisPanel, type AnalysisCompositionDimension } from '../AnalysisPanel';

type DrawnLabel = { text: string; color: unknown };
const frames = new WeakMap<HTMLCanvasElement, DrawnLabel[]>();
const filledSweeps = new WeakMap<HTMLCanvasElement, number[]>();

function contextFor(canvas: HTMLCanvasElement): CanvasRenderingContext2D {
  let pathSweeps: number[] = [];
  const target: Record<PropertyKey, unknown> = {
    canvas,
    measureText: (text: unknown) => ({ width: String(text).length * 6 }),
    createLinearGradient: () => ({ [Symbol.toStringTag]: 'CanvasGradient', addColorStop: () => {} }),
    getLineDash: () => [],
    clearRect: () => { frames.set(canvas, []); filledSweeps.set(canvas, []); },
    beginPath: () => { pathSweeps = []; },
    arc: (_x: number, _y: number, radius: number, start: number, end: number, anticlockwise = false) => {
      // 记录 Canvas 真正绘制的方向与角度，而不只检查 Chart.js 中仍然正确的数据占比。
      if (radius <= 20) return;
      const sweep = anticlockwise ? start - end : end - start;
      const fullCircle = Math.PI * 2;
      pathSweeps.push(Math.abs(sweep) >= fullCircle ? fullCircle : ((sweep % fullCircle) + fullCircle) % fullCircle);
    },
    fill: () => filledSweeps.get(canvas)?.push(...pathSweeps),
    fillText: (text: unknown) => frames.get(canvas)?.push({ text: String(text), color: target.fillStyle }),
  };
  return new Proxy(target, {
    get: (object, property) => Reflect.has(object, property) ? Reflect.get(object, property) : () => {},
    set: (object, property, value) => Reflect.set(object, property, value),
  }) as unknown as CanvasRenderingContext2D;
}

const item = (key: string, label: string, percent: number): AnalysisCompositionItem => ({
  key, label, percent, total_tokens: percent, requests: 1,
  input_tokens: percent, output_tokens: 0, cache_read_tokens: 0,
  cache_creation_tokens: 0, reasoning_tokens: 0, cost_usd: 0, cost_available: true,
});

const analysisWith = (items: AnalysisCompositionItem[]): AnalysisResponse => ({
  granularity: 'hourly', timezone: 'UTC', token_usage: [],
  api_key_composition: items, model_composition: items,
  auth_files_composition: [], ai_provider_composition: [], model_efficiency: [],
  cost_breakdown: {
    uncached_input_cost_usd: 0, cache_read_cost_usd: 0, cache_write_cost_usd: 0,
    output_cost_usd: 0, total_cost_usd: 0, cost_available: true,
  },
  heatmap: { api_keys: [], api_key_labels: {}, models: [], cells: [] },
});

describe('composition labels after mounted chart updates', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(async () => {
    await i18n.changeLanguage('en');
    const contexts = new WeakMap<HTMLCanvasElement, CanvasRenderingContext2D>();
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockImplementation(function (this: HTMLCanvasElement) {
      let context = contexts.get(this);
      if (!context) {
        context = contextFor(this);
        contexts.set(this, context);
      }
      return context;
    });
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    act(() => root.unmount());
    container.remove();
    vi.restoreAllMocks();
    await i18n.changeLanguage('en');
  });

  const render = async (analysis: AnalysisResponse, isDark = false, dimensions?: readonly AnalysisCompositionDimension[]) => {
    await act(async () => {
      root.render(<AnalysisPanel analysis={analysis} loading={false} isDark={isDark} isMobile={false} compositionDimensions={dimensions} />);
    });
  };

  const drawn = () => frames.get(container.querySelector('canvas')!) ?? [];

  it.each(['focus', 'selection', 'chart hover'])('keeps a 0.02 percent slice narrow during %s', async (interaction) => {
    await render(analysisWith([90.73, 4.2, 3.16, 1.89, 0.02].map((share, index) => item(String(index), `Model ${index}`, share))));
    const canvas = container.querySelector('canvas')!;
    const chart = Chart.getChart(canvas)!;
    const tinyButton = container.querySelectorAll<HTMLButtonElement>('ol button')[4];
    const assertNarrowPaths = () => {
      const sweeps = filledSweeps.get(canvas)!;
      expect(sweeps.length).toBeGreaterThan(0);
      // 圆环内外边界各分两段；即使 90.73% 的大扇区，单段也不会超过半圈。
      expect(Math.max(...sweeps)).toBeLessThan(Math.PI);
    };
    assertNarrowPaths();
    await act(async () => {
      if (interaction === 'focus') tinyButton.focus();
      else if (interaction === 'selection') tinyButton.click();
      else { chart.setActiveElements([{ datasetIndex: 0, index: 4 }]); chart.update('none'); }
    });
    assertNarrowPaths();
    expect(chart.data.datasets[0].data[4]).toBe(0.02);
  });

  it.each([
    { page: 'analysis', dimensions: undefined },
    { page: 'key-analysis', dimensions: ['model'] as const },
  ])('updates names and percentages with unchanged item IDs on $page', async ({ dimensions }) => {
    await render(analysisWith([item('a', 'Alpha', 60), item('b', 'Beta', 40)]), false, dimensions);
    expect(drawn()).toContainEqual({ text: 'Alpha', color: '#111827' });
    expect(drawn()).toContainEqual({ text: '60.00%', color: '#111827' });

    await render(analysisWith([item('a', 'Renamed', 80), item('b', 'Beta', 20)]), false, dimensions);
    const texts = drawn().map((label) => label.text);
    expect(texts).toContain('Renamed');
    expect(texts).toContain('80.00%');
    expect(texts).not.toContain('Alpha');
    expect(texts).not.toContain('60.00%');
  });

  it('updates label colors when switching themes without reloading the page', async () => {
    const analysis = analysisWith([item('a', 'Alpha', 100)]);
    await render(analysis);
    expect(drawn()).toContainEqual({ text: 'Alpha', color: '#111827' });
    await render(analysis, true);
    expect(drawn()).toContainEqual({ text: 'Alpha', color: '#f5f1e8' });
    await render(analysis, false);
    expect(drawn()).toContainEqual({ text: 'Alpha', color: '#111827' });
  });

  it('updates the total label when changing language while keeping all entries', async () => {
    const analysis = analysisWith([50, 20, 10, 8, 5, 4, 3].map((share, index) => item(String(index), `Model ${index}`, share)));
    await render(analysis);
    expect(container.querySelector('ol')?.querySelectorAll('button')).toHaveLength(7);
    expect(container.textContent).toContain('Total Tokens');

    await act(async () => { await i18n.changeLanguage('zh'); });
    expect(container.textContent).toContain('总 Token');
    expect(container.textContent).not.toContain('Total Tokens');
    expect(container.querySelector('ol')?.querySelectorAll('button')).toHaveLength(7);
  });
});
