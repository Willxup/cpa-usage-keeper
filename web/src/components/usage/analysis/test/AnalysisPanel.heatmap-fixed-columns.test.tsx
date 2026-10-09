// @vitest-environment happy-dom

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import React from 'react';
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

import { emptyAnalysis, renderAnalysisPanel } from './analysisFixtures';

const analysisPanelStyles = readFileSync(resolve(process.cwd(), 'src/components/usage/analysis/AnalysisPanel.module.scss'), 'utf8').replace(/\r\n/g, '\n');

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
  ...emptyAnalysis,
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

describe('AnalysisPanel heatmap columns', () => {
  it('keeps the key, model values, and accessible totals in column order', () => {
    const container = renderAnalysisPanel({ analysis, isDark: true });
    const row = container.querySelector('[class*="heatmapRowContents"]')!;
    expect([...row.children].map((cell) => cell.textContent)).toEqual([
      'Primary Production Key', '1.00K', '2.50K', '3.50K', '$0.7500',
    ]);
    expect(row.querySelector('[class*="heatmapRowLabel"]')?.getAttribute('aria-label')).toBe(
      'usage_stats.analysis_heatmap_api_key: Primary Production Key, usage_stats.total_tokens: 3.50K, usage_stats.total_cost: $0.7500',
    );
    expect([...row.querySelectorAll('[class*="heatmapSummaryCell"]')].map((cell) => cell.getAttribute('aria-label'))).toEqual([
      'usage_stats.total_tokens: 3.50K, usage_stats.analysis_heatmap_api_key: Primary Production Key',
      'usage_stats.total_cost: $0.7500, usage_stats.analysis_heatmap_api_key: Primary Production Key',
    ]);
  });

  it('pins only the desktop key column footprint and preserves mobile scrolling and focus', () => {
    const scroller = styleRuleBlock('.heatmapScroller');
    expect(scroller).toContain('overflow-x: auto;');
    expect(scroller).toContain('scrollbar-gutter: stable;');
    expect(scroller).toMatch(/@include mobile\s*\{[\s\S]*?padding-bottom:\s*10px;/);
    const keyColumn = styleRuleBlock('.heatmapKeyColumn');
    expect(keyColumn).toContain('position: sticky;');
    expect(keyColumn).toContain('left: 0;');
    const mask = styleRuleBlock('.heatmapKeyColumn::before');
    expect(mask).toContain('inset: 0;');
    expect(mask).toContain('border-radius: inherit;');
    expect(mask).toContain('background: inherit;');
    expect(mask).not.toContain('var(--heatmap-grid-gap)');
    expect(analysisPanelStyles).toMatch(/@include mobile\s*\{\s*\.heatmapKeyColumn\s*\{[^}]*position: static;/);
    expect(analysisPanelStyles).toMatch(/@include mobile\s*\{\s*\.heatmapKeyColumn\s*\{[^}]*\}\s*\.heatmapKeyColumn::before\s*\{\s*content: none;/);
    expect(styleRuleBlock('.heatmapRowLabel:focus-visible,')).toMatch(/box-shadow:/);
  });
});
