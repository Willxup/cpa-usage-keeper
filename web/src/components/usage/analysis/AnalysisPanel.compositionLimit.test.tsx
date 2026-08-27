// @vitest-environment happy-dom

import React from 'react';
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ChartData, ChartOptions } from 'chart.js';
import type { AnalysisCompositionItem, AnalysisResponse } from '@/lib/types';
import { ANALYSIS_COMPOSITION_ITEM_LIMIT_STORAGE_KEY } from '@/utils/usage/analysisPreferences';
const testStorage = vi.hoisted(() => {
  const values = new Map<string, string>();
  const storage: Storage = {
    get length() {
      return values.size;
    },
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    key: (index) => Array.from(values.keys())[index] ?? null,
    removeItem: (key) => values.delete(key),
    setItem: (key, value) => values.set(key, value),
  };
  Object.defineProperty(window, 'localStorage', { configurable: true, value: storage });
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: storage });
  return storage;
});


const chartCapture = vi.hoisted(() => ({
  doughnutData: null as ChartData<'doughnut', number[], string> | null,
  doughnutOptions: null as ChartOptions<'doughnut'> | null,
}));

vi.mock('react-chartjs-2', () => ({
  Bar: () => React.createElement('div'),
  Doughnut: (props: { data: ChartData<'doughnut', number[], string>; options: ChartOptions<'doughnut'> }) => {
    chartCapture.doughnutData = props.data;
    chartCapture.doughnutOptions = props.options;
    return React.createElement('div', { 'data-composition-chart': true });
  },
  Scatter: () => React.createElement('div'),
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

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const buildComposition = (prefix: string, count = 8): AnalysisCompositionItem[] => Array.from(
  { length: count },
  (_, index) => ({
    key: `${prefix}-${index + 1}`,
    label: `${prefix} ${index + 1}`,
    total_tokens: (count - index) * 100,
    requests: count - index,
    percent: ((count - index) / ((count * (count + 1)) / 2)) * 100,
    input_tokens: (count - index) * 40,
    output_tokens: (count - index) * 20,
    cache_read_tokens: (count - index) * 15,
    cache_creation_tokens: (count - index) * 10,
    reasoning_tokens: (count - index) * 5,
    cost_usd: count - index,
    cost_available: true,
  }),
);

const analysis: AnalysisResponse = {
  granularity: 'hourly',
  timezone: 'UTC',
  token_usage: [],
  api_key_composition: buildComposition('API'),
  model_composition: buildComposition('Model'),
  auth_files_composition: buildComposition('Auth'),
  ai_provider_composition: buildComposition('Provider'),
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
};

describe('AnalysisPanel usage distribution item limit', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    testStorage.clear();
    chartCapture.doughnutData = null;
    chartCapture.doughnutOptions = null;
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    testStorage.clear();
  });

  const renderPanel = async () => {
    await act(async () => {
      root.render(<AnalysisPanel analysis={analysis} loading={false} isDark={false} isMobile={false} />);
    });
  };

  const setLimitDraft = async (value: string) => {
    const input = container.querySelector<HTMLInputElement>('input[type="number"]');
    expect(input).not.toBeNull();
    await act(async () => {
      const valueSetter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set;
      valueSetter?.call(input, value);
      input?.dispatchEvent(new Event('input', { bubbles: true }));
    });
  };

  const submitLimit = async () => {
    const form = container.querySelector<HTMLFormElement>('form');
    expect(form).not.toBeNull();
    await act(async () => {
      form?.dispatchEvent(new SubmitEvent('submit', { bubbles: true, cancelable: true }));
    });
  };

  it('defaults to five, applies a shared positive limit, and persists it', async () => {
    await renderPanel();

    const input = container.querySelector<HTMLInputElement>('input[type="number"]');
    expect(input?.value).toBe('5');
    expect(input?.min).toBe('0');
    expect(input?.step).toBe('1');
    expect(chartCapture.doughnutData?.labels).toEqual([
      'API 1', 'API 2', 'API 3', 'API 4', 'API 5', 'usage_stats.analysis_others',
    ]);

    await setLimitDraft('2');
    await submitLimit();

    expect(testStorage.getItem(ANALYSIS_COMPOSITION_ITEM_LIMIT_STORAGE_KEY)).toBe('2');
    expect(chartCapture.doughnutData?.labels).toEqual(['API 1', 'API 2', 'usage_stats.analysis_others']);
    expect(chartCapture.doughnutData?.datasets[0]?.data).toEqual([800, 700, 2100]);

    const modelTab = Array.from(container.querySelectorAll<HTMLButtonElement>('[role="tab"]'))
      .find((button) => button.textContent === 'usage_stats.analysis_composition_model_tab');
    await act(async () => modelTab?.click());
    expect(chartCapture.doughnutData?.labels).toEqual(['Model 1', 'Model 2', 'usage_stats.analysis_others']);
  });

  it('loads zero as show-all and uses dense, non-repeating chart styling', async () => {
    testStorage.setItem(ANALYSIS_COMPOSITION_ITEM_LIMIT_STORAGE_KEY, '0');
    await renderPanel();

    expect(container.querySelector<HTMLInputElement>('input[type="number"]')?.value).toBe('0');
    expect(chartCapture.doughnutData?.labels).toEqual(analysis.api_key_composition.map((item) => item.label));
    expect(chartCapture.doughnutData?.labels).not.toContain('usage_stats.analysis_others');
    expect(chartCapture.doughnutData?.datasets[0]).toMatchObject({ borderRadius: 4 });
    expect(chartCapture.doughnutOptions?.spacing).toBe(2);
    expect(container.querySelector('[class*="compositionUsageListScrollable"]')).not.toBeNull();

    const backgroundColor = chartCapture.doughnutData?.datasets[0]?.backgroundColor;
    expect(typeof backgroundColor).toBe('function');
    const resolveColor = backgroundColor as (context: { dataIndex: number; chart: { chartArea?: unknown } }) => string;
    expect(resolveColor({ dataIndex: 0, chart: {} })).toBe('#1d4ed8');
    expect(resolveColor({ dataIndex: 6, chart: {} })).toMatch(/^hsl\(/);
    expect(resolveColor({ dataIndex: 6, chart: {} })).not.toBe(resolveColor({ dataIndex: 0, chart: {} }));
  });

  it('rejects invalid drafts without replacing the applied value', async () => {
    await renderPanel();
    await setLimitDraft('-1');

    const input = container.querySelector<HTMLInputElement>('input[type="number"]');
    const applyButton = container.querySelector<HTMLButtonElement>('form button[type="submit"]');
    expect(input?.getAttribute('aria-invalid')).toBe('true');
    expect(applyButton?.disabled).toBe(true);
    expect(container.textContent).toContain('usage_stats.analysis_composition_limit_invalid');

    await submitLimit();
    expect(testStorage.getItem(ANALYSIS_COMPOSITION_ITEM_LIMIT_STORAGE_KEY)).toBeNull();
    expect(chartCapture.doughnutData?.labels).toHaveLength(6);
  });
});
