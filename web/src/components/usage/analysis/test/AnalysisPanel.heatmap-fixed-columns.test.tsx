import { readFileSync } from 'node:fs';
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

const analysisPanelStyles = readFileSync(new URL('../AnalysisPanel.module.scss', import.meta.url), 'utf8').replace(/\r\n/g, '\n');

const styleRuleBlock = (selector: string) => {
  const start = analysisPanelStyles.indexOf(selector);
  expect(start).toBeGreaterThanOrEqual(0);
  const open = analysisPanelStyles.indexOf('{', start);
  const close = analysisPanelStyles.indexOf('\n}', open);
  expect(open).toBeGreaterThanOrEqual(0);
  expect(close).toBeGreaterThan(open);
  return analysisPanelStyles.slice(open + 1, close);
};

const analysis: AnalysisResponse = {
  granularity: 'hourly',
  timezone: 'UTC',
  token_usage: [],
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
  heatmap: {
    api_keys: ['key-1'],
    api_key_labels: { 'key-1': 'Primary Production Key' },
    models: ['model-a', 'model-b'],
    cells: [
      {
        api_key: 'key-1',
        model: 'model-a',
        input_tokens: 800,
        output_tokens: 200,
        cache_read_tokens: 0,
        cache_creation_tokens: 0,
        reasoning_tokens: 0,
        total_tokens: 1_000,
        requests: 2,
        cost_usd: 0.25,
        cost_available: true,
        intensity: 0.4,
      },
      {
        api_key: 'key-1',
        model: 'model-b',
        input_tokens: 2_000,
        output_tokens: 500,
        cache_read_tokens: 0,
        cache_creation_tokens: 0,
        reasoning_tokens: 0,
        total_tokens: 2_500,
        requests: 3,
        cost_usd: 0.5,
        cost_available: true,
        intensity: 1,
      },
    ],
  },
};

describe('AnalysisPanel fixed heatmap columns demo', () => {
  it('keeps the API key and two compact totals outside the scrolling model matrix', () => {
    const markup = renderToStaticMarkup(
      <AnalysisPanel analysis={analysis} loading={false} isDark isMobile={false} />,
    );

    expect(markup).toContain('heatmapKeyColumn');
    expect(markup).toContain('heatmapTotalTokensColumn');
    expect(markup).toContain('heatmapTotalCostColumn');
    expect(markup).not.toContain('heatmapSummaryColumn');
    expect(markup).toContain('heatmapKeyMarker');
    expect(markup).not.toContain('heatmapCostSummaryCell');
    expect(markup).not.toContain('usage_stats.analysis_heatmap_total');
    expect(markup).toContain('Primary Production Key');
    expect(markup).toContain('3.50K');
    expect(markup).toContain('$0.7500');
    const rowMarkup = markup.slice(markup.indexOf('heatmapRowContents'));
    expect(rowMarkup).toContain('aria-label="usage_stats.analysis_heatmap_api_key: Primary Production Key, usage_stats.total_tokens: 3.50K, usage_stats.total_cost: $0.7500"');
    expect(rowMarkup).toContain('aria-label="usage_stats.total_tokens: 3.50K, usage_stats.analysis_heatmap_api_key: Primary Production Key"');
    expect(rowMarkup).toContain('aria-label="usage_stats.total_cost: $0.7500, usage_stats.analysis_heatmap_api_key: Primary Production Key"');
    expect(rowMarkup).not.toContain('aria-label="Primary Production Key, usage_stats.total_tokens: 3.50K, usage_stats.total_cost: $0.7500"');
    expect(rowMarkup.indexOf('Primary Production Key')).toBeLessThan(rowMarkup.indexOf('1.00K'));
    expect(rowMarkup.lastIndexOf('3.50K')).toBeGreaterThan(rowMarkup.lastIndexOf('2.50K'));
    expect(rowMarkup.lastIndexOf('$0.7500')).toBeGreaterThan(rowMarkup.lastIndexOf('3.50K'));
  });

  it('masks only the rounded key column footprint without pinning the grid gap', () => {
    const heatmapScroller = styleRuleBlock('.heatmapScroller');
    expect(heatmapScroller).toContain('padding-bottom: 2px;');
    expect(heatmapScroller).toContain('scrollbar-gutter: stable;');
    expect(heatmapScroller).toMatch(/@include mobile\s*\{[\s\S]*?padding-bottom:\s*10px;/);
    expect(analysisPanelStyles).toContain('\n.heatmapGrid {\n  --heatmap-key-column-width: 160px;');
    expect(analysisPanelStyles).toContain('--heatmap-summary-column-width: 88px;');
    const keyColumn = styleRuleBlock('.heatmapKeyColumn');
    expect(keyColumn).toContain('position: sticky;');
    expect(keyColumn).toContain('left: 0;');
    expect(keyColumn).not.toContain('var(--heatmap-grid-gap) 0 0');
    expect(analysisPanelStyles).not.toContain('--heatmap-pinned-gap-color');
    const keyColumnMask = styleRuleBlock('.heatmapKeyColumn::before');
    expect(keyColumnMask).toContain('position: absolute;');
    expect(keyColumnMask).toContain('inset: 0;');
    expect(keyColumnMask).toContain('z-index: -1;');
    expect(keyColumnMask).toContain('background: inherit;');
    expect(keyColumnMask).not.toContain('var(--heatmap-grid-gap)');
    expect(analysisPanelStyles).toContain(`@include mobile {
  .heatmapKeyColumn {
    position: static;
    box-shadow: none;
  }

  .heatmapKeyColumn::before {
    content: none;
  }
}`);
    const keyMarker = styleRuleBlock('.heatmapKeyMarker');
    expect(keyMarker).toContain('width: 3px;');
    expect(keyMarker).toContain('height: 16px;');
    expect(keyMarker).not.toContain('box-shadow:');
    expect(analysisPanelStyles).toMatch(/\.heatmapRowLabel\s*\{[\s\S]*?height:\s*34px;[\s\S]*?border:\s*1px solid var\(--border-color\);[\s\S]*?background:\s*var\(--bg-primary\);/);
    expect(analysisPanelStyles).toContain('.heatmapRowLabel:focus-visible,\n.heatmapSummaryCell:focus-visible {');
    const summaryCell = styleRuleBlock('.heatmapSummaryCell');
    expect(summaryCell).toContain('display: flex;');
    expect(summaryCell).toContain('justify-content: center;');
    expect(summaryCell).toContain('font-variant-numeric: tabular-nums;');
    expect(analysisPanelStyles).not.toContain('.heatmapSummaryMetric');
    expect(analysisPanelStyles).not.toContain('.heatmapCostSummaryCell');
    expect(analysisPanelStyles).not.toContain('.heatmapTooltipTarget');
  });
});
