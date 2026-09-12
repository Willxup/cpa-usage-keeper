// @vitest-environment happy-dom
import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it } from 'vitest';
import i18n from '@/i18n';
import type { UsageComparisonItem } from '@/lib/types';
import { UsageComparisonCharts } from '../UsageComparisonCharts';
import { buildComparisonView, layoutComparisonTreemap } from '../usageComparisonData';

let container: HTMLDivElement;
let root: Root;
const item = (key: string, overrides: Partial<UsageComparisonItem> = {}): UsageComparisonItem => ({
  key, label: key, requests: 10, failures: 0, input_tokens: 80, output_tokens: 20,
  cache_read_tokens: 40, cache_creation_tokens: 0, reasoning_tokens: 5, total_tokens: 100, cost: 1, ...overrides,
});
beforeEach(async () => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  await i18n.changeLanguage('en');
  container = document.createElement('div'); document.body.appendChild(container); root = createRoot(container);
});
afterEach(async () => { await act(async () => root.unmount()); container.remove(); });
const render = async (items: UsageComparisonItem[], loading = false) => act(async () => root.render(<UsageComparisonCharts comparisons={{ models: items, api_keys: items }} loading={loading} />));
const firstChart = () => container.querySelector('[data-comparison="models"]')!;

it('keeps token ranking and full shares while aggregating all Others information and unknown prices', () => {
  const items = Array.from({ length: 13 }, (_, index) => item(`model-${index.toString().padStart(2,'0')}`));
  items[12].cost = null;
  const view = buildComparisonView(items, 'Others');
  expect(view.rows).toHaveLength(6);
  expect(view.total).toBe(1300);
  expect(view.rows[0].share).toBeCloseTo(100 / 13);
  expect(view.rows[5]).toMatchObject({ label: 'Others', value: 800, requests: 80, cost: null, cache_read_tokens: 320 });
  expect(view.rows.reduce((sum, row) => sum + row.share!, 0)).toBeCloseTo(100);
});

it('preserves proportional areas for tiny shares in wide and narrow treemaps', () => {
  const view = buildComparisonView([item('large', {total_tokens: 9999}), item('tiny', {total_tokens: 1})], 'Others');
  for (const aspect of [2, .7]) {
    const rects = layoutComparisonTreemap(view.rows, aspect);
    expect(rects).toHaveLength(2);
    for (const rect of rects) expect(rect.width * rect.height / 100).toBeCloseTo(rect.row.share!, 6);
    const [a,b] = rects;
    expect(a.x + a.width <= b.x || b.x + b.width <= a.x || a.y + a.height <= b.y || b.y + b.height <= a.y).toBe(true);
  }
});

it('puts model information in the tooltip and Key information in the list without metric switches', async () => {
  await render([item('most-tokens', {total_tokens:800,cost:null}),item('most-requests',{total_tokens:200,requests:1000,cost:50})]);
  const tile=firstChart().querySelector<HTMLButtonElement>('[data-comparison-entry]')!;
  expect(tile.textContent).toBe('most-tokens');
  expect(firstChart().querySelector('[aria-pressed]')).toBeNull();
  await act(async()=>tile.focus());
  expect(document.querySelector('[role="tooltip"]')?.textContent).toContain('80.0%');
  expect(document.querySelector('[role="tooltip"]')?.textContent).toContain('Cost: —');
  expect(document.querySelector('[role="tooltip"]')?.textContent).toContain('Requests: 10');
  const row=container.querySelector('[data-comparison="api_keys"] [data-usage-share-item]')!;
  expect(row.textContent).toContain('most-tokens');
  expect(row.textContent).toContain('800');
  expect(row.textContent).toContain('80.00%');
  expect(row.textContent).toContain('Requests');
  expect(row.textContent).toContain('—');
  expect((row.querySelector('[aria-hidden="true"] > span') as HTMLElement).style.width).toBe('80%');
});

it('updates token-ranked rows during polling and preserves zero versus unknown prices', async () => {
  await render([item('model-a')]);
  await render([item('model-b',{cost:0})],true);
  expect(firstChart().textContent).toContain('model-b');
  expect(firstChart().textContent).not.toContain('model-a');
  const row=container.querySelector('[data-comparison="api_keys"] [data-usage-share-item]')!;
  expect(row.textContent).not.toContain('—');
  await act(async()=>root.render(<UsageComparisonCharts loading />));
  expect(container.querySelector('[aria-busy="true"]')).not.toBeNull();
});

it('uses a token-ranked model list for Key Viewer without exposing any other Key', async () => {
  const items=[item('higher-average',{requests:1,total_tokens:100}),item('more-tokens',{requests:100,total_tokens:900})];
  await act(async()=>root.render(<UsageComparisonCharts keyViewer loading={false} comparisons={{models:items, api_keys: items}} />));
  expect(container.querySelector('[data-comparison="api_keys"]')).not.toBeNull();
  expect(container.querySelector('[data-comparison="api_keys"] [data-usage-share-item]')?.textContent).toContain('more-tokens');
  expect(container.querySelector('[aria-pressed]')).toBeNull();
});
