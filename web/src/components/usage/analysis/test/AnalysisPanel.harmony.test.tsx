// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { AnalysisCompositionItem, AnalysisResponse } from '@/lib/types';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;
vi.mock('react-chartjs-2', () => ({ Bar: () => <div />, Doughnut: () => <div />, Scatter: () => <div /> }));
vi.mock('react-i18next', () => ({ initReactI18next: { type: '3rdParty', init: () => {} }, useTranslation: () => ({ t: (key: string) => key }) }));

import { AnalysisPanel } from '../AnalysisPanel';

const item = (index: number): AnalysisCompositionItem => ({
  key: `model-${index}`, label: `model-${index}`, total_tokens: 100 - index, percent: 10,
  requests: 10, input_tokens: index === 1 ? 0 : 100, cache_read_tokens: index === 0 ? 0 : 25,
  output_tokens: 0, cache_creation_tokens: 0, reasoning_tokens: 0, cost_usd: 1, cost_available: true,
});
const fixture = (): AnalysisResponse => {
  const items = Array.from({ length: 12 }, (_, index) => item(index));
  return {
    granularity: 'hourly', timezone: 'UTC', range_start: '2026-09-12T00:00:00Z', range_end: '2026-09-12T01:00:00Z',
    token_usage: [], model_usage: { buckets: ['2026-09-12T00:00:00Z'], series: items.map((row) => ({ model: row.label, total_tokens: [row.total_tokens], requests: [row.requests] })) },
    api_key_composition: items, model_composition: items, auth_files_composition: [], ai_provider_composition: [],
    model_efficiency: [], heatmap: { api_keys: [], models: [], api_key_labels: {}, cells: [] },
    cost_breakdown: { uncached_input_cost_usd: 0, cache_read_cost_usd: 0, cache_write_cost_usd: 0, output_cost_usd: 0, total_cost_usd: 0, cost_available: true },
  };
};

describe('paired analysis chart details', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: true } as MediaQueryList);
  });
  afterEach(() => vi.restoreAllMocks());

  const mount = (analysis = fixture()) => {
    const container = document.createElement('div');
    document.body.append(container);
    const root = createRoot(container);
    const render = (data: AnalysisResponse) => act(() => root.render(<AnalysisPanel analysis={data} loading={false} isDark={false} isMobile={false} compositionDimensions={['model']} />));
    render(analysis);
    return { container, render, unmount: () => act(() => root.unmount()) };
  };

  it('lists all entries on both sides and exposes matching cache metrics, including zero and unavailable values', () => {
    const { container, unmount } = mount();
    try {
      const lists = container.querySelectorAll('ol');
      expect(lists).toHaveLength(2);
      for (const list of lists) {
        expect(list.querySelectorAll('button')).toHaveLength(12);
        expect(list.textContent).not.toContain('usage_stats.analysis_others');
        const cache = (index: number) => list.querySelectorAll('button')[index].querySelector('[data-metric="cache"]')?.textContent;
        expect(cache(0)).toContain('0.00%');
        expect(cache(1)).toContain('--');
        expect(cache(2)).toContain('25.00%');
      }
    } finally { unmount(); }
  });

  it('supports hover, keyboard focus and toggle selection in either list', () => {
    const { container, unmount } = mount();
    try {
      for (const list of container.querySelectorAll('ol')) {
        const [first, second] = list.querySelectorAll('button');
        act(() => first.dispatchEvent(new PointerEvent('pointerover', { bubbles: true, pointerType: 'mouse' })));
        expect(first.dataset.active).toBe('true');
        expect(second.dataset.muted).toBe('true');
        act(() => second.focus());
        expect(second.dataset.active).toBe('true');
        act(() => second.blur());
        act(() => first.dispatchEvent(new PointerEvent('pointerout', { bubbles: true, pointerType: 'mouse', relatedTarget: document.body })));
        act(() => second.click());
        expect(second.getAttribute('aria-pressed')).toBe('true');
        act(() => second.click());
        expect(second.getAttribute('aria-pressed')).toBe('false');
        expect(second.dataset.active).toBe('false');
      }
    } finally { unmount(); }
  });

  it('preserves each chart fixed palette before appending colors for additional entries', () => {
    const { container, unmount } = mount();
    const compositionPalette = ['#1d4ed8', '#ca8a04', '#15803d', '#7e22ce', '#b91c1c', '#0891b2'];
    const modelPalette = ['#db2777', '#d97706', '#059669', '#2563eb', '#dc2626'];
    try {
      const lists = Array.from(container.querySelectorAll('ol'));
      for (const [index, palette] of [compositionPalette, modelPalette].entries()) {
        const colors = Array.from(lists[index].querySelectorAll('button')).map((row) => row.style.getPropertyValue('--ranking-color'));
        expect(colors.slice(0, palette.length)).toEqual(palette);
        expect(colors).toHaveLength(12);
        expect(colors[palette.length]).toBeTruthy();
        expect(palette).not.toContain(colors[palette.length]);
      }
    } finally { unmount(); }
  });
});
