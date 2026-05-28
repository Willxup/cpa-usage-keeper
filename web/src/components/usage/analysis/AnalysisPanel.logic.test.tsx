import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it, vi } from 'vitest';
import type { ChartData, ChartOptions } from 'chart.js';
import type { AnalysisResponse } from '@/lib/types';

const chartCapture = vi.hoisted(() => ({
  barData: null as ChartData<'bar', number[], string> | null,
  barOptions: null as ChartOptions<'bar'> | null,
}));

vi.mock('react-chartjs-2', () => ({
  Bar: (props: { data: ChartData<'bar', number[], string>; options: ChartOptions<'bar'> }) => {
    chartCapture.barData = props.data;
    chartCapture.barOptions = props.options;
    return React.createElement('div');
  },
  Doughnut: () => React.createElement('div'),
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
  heatmap: {
    api_keys: [],
    models: [],
    cells: [],
  },
};

describe('AnalysisPanel token chart data', () => {
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
      }],
    };

    renderToStaticMarkup(<AnalysisPanel analysis={analysis} loading={false} isDark={false} isMobile={false} />);

    const datasets = chartCapture.barData?.datasets ?? [];
    expect(datasets.find((dataset) => dataset.label === 'usage_stats.input_tokens')?.data).toEqual([400]);
    expect(datasets.find((dataset) => dataset.label === 'usage_stats.cached_tokens')?.data).toEqual([600]);
    expect(datasets.find((dataset) => dataset.label === 'usage_stats.output_tokens')?.data).toEqual([50]);
    expect(datasets.find((dataset) => dataset.label === 'usage_stats.reasoning_tokens')?.data).toEqual([50]);
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
    const tooltipFooter = chartCapture.barOptions?.plugins?.tooltip?.callbacks?.footer;
    expect(typeof tooltipFooter).toBe('function');
    expect(tooltipFooter?.([{ dataIndex: 0 }] as never)).toBe('usage_stats.total_tokens: 1.15K');
  });
});
