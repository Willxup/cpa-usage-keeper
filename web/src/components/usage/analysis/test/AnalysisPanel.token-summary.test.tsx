import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it, vi } from 'vitest';
import type { AnalysisResponse } from '@/lib/types';

vi.mock('react-chartjs-2', () => ({
  Bar: () => React.createElement('div'),
  Doughnut: () => React.createElement('div'),
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

import { AnalysisPanel } from '../AnalysisPanel';

const analysis: AnalysisResponse = {
  granularity: 'hourly',
  timezone: 'UTC',
  token_usage: [
    {
      bucket: '2026-07-14T08:00:00Z',
      input_tokens: 1_200_000,
      output_tokens: 300_000,
      cache_read_tokens: 400_000,
      cache_creation_tokens: 100_000,
      reasoning_tokens: 0,
      total_tokens: 2_000_000,
      requests: 6,
      cost_usd: 4,
      cost_available: true,
    },
    {
      bucket: '2026-07-14T09:00:00Z',
      input_tokens: 800_000,
      output_tokens: 200_000,
      cache_read_tokens: 0,
      cache_creation_tokens: 0,
      reasoning_tokens: 0,
      total_tokens: 1_000_000,
      requests: 4,
      cost_usd: 2,
      cost_available: true,
    },
  ],
  api_key_composition: [],
  model_composition: [],
  auth_files_composition: [],
  ai_provider_composition: [],
  cost_breakdown: {
    uncached_input_cost_usd: 1,
    cache_read_cost_usd: 1.5,
    cache_write_cost_usd: 0.5,
    output_cost_usd: 3,
    total_cost_usd: 6,
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

describe('AnalysisPanel token chart summary', () => {
  it.each([undefined, ['model'] as const])('keeps range totals in the token chart for composition dimensions %s', (compositionDimensions) => {
    const markup = renderToStaticMarkup(
      <AnalysisPanel analysis={analysis} loading={false} isDark={false} isMobile={false} compositionDimensions={compositionDimensions} />,
    );
    const tokenCard = markup.slice(markup.indexOf('<section'), markup.indexOf('</section>'));
    const summaryStart = tokenCard.indexOf('analysisSummary');
    const chartStart = tokenCard.indexOf('analysisChartSurface');
    const summaryMarkup = tokenCard.slice(summaryStart, chartStart);

    expect(summaryStart).toBeGreaterThan(-1);
    expect(chartStart).toBeGreaterThan(summaryStart);
    expect(summaryMarkup).toContain('usage_stats.total_tokens');
    expect(summaryMarkup).toContain('usage_stats.total_cost');
    expect(summaryMarkup).toContain('usage_stats.analysis_cost_per_million_tokens');
    expect(summaryMarkup.indexOf('usage_stats.total_tokens')).toBeLessThan(
      summaryMarkup.indexOf('usage_stats.total_cost'),
    );
    expect(summaryMarkup.indexOf('usage_stats.total_cost')).toBeLessThan(
      summaryMarkup.indexOf('usage_stats.analysis_cost_per_million_tokens'),
    );
    expect(summaryMarkup).toContain('3.00M');
    expect(summaryMarkup).toContain('$6.00');
    expect(summaryMarkup).toContain('$2.00');
    expect(markup).not.toContain('usage_stats.analysis_cost_breakdown_title');
    const titles = [...markup.matchAll(/<h2[^>]*>(.*?)<\/h2>/g)].map((match) => match[1]);
    expect(titles).toEqual([
      'usage_stats.analysis_token_usage_title',
      'usage_stats.analysis_composition_title',
      'usage_stats.analysis_top_models_title',
      'usage_stats.analysis_latency_title',
      'usage_stats.analysis_model_efficiency_title',
      'usage_stats.analysis_heatmap_title',
    ]);
  });

  it('retains the pricing hint and avoids invalid rates for zero tokens', () => {
    const markup = renderToStaticMarkup(
      <AnalysisPanel analysis={{ ...analysis, token_usage: [], cost_breakdown: { ...analysis.cost_breakdown, cost_available: false } }} loading={false} isDark={false} isMobile={false} />,
    );
    const tokenCard = markup.slice(markup.indexOf('<section'), markup.indexOf('</section>'));
    expect(tokenCard).toContain('usage_stats.cost_need_price');
    expect(tokenCard).toContain('analysisSummary');
    expect(tokenCard).toContain('$6.00');
    expect(tokenCard).toContain('$0.00');
    expect(tokenCard).not.toMatch(/NaN|Infinity/);
  });
});
