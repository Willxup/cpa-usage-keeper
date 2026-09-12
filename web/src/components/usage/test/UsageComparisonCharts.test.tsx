// @vitest-environment happy-dom
import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it } from 'vitest';
import i18n from '@/i18n';
import type { UsageComparisonItem } from '@/lib/types';
import { UsageComparisonCharts } from '../UsageComparisonCharts';

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
const click = async (text: string) => act(async () => Array.from(firstChart().querySelectorAll('button')).find((button) => button.textContent === text)!.click());

it('ranks all metrics locally and exposes failures without token usage', async () => {
  await render([item('large', { total_tokens: 900 }), item('failing', { failures: 10, total_tokens: 0, cost: 0, input_tokens: 0 }), item('unknown', { cost: null })]);
  expect(firstChart().querySelector('summary')?.textContent).toContain('large');
  await click('Failed');
  expect(firstChart().querySelector('summary')?.textContent).toContain('failing');
  expect(firstChart().querySelector('details')?.textContent).toContain('0.0%');
  await click('Cost');
  expect(firstChart().textContent).toContain('Known cost');
  const unknown = Array.from(firstChart().querySelectorAll('details')).find((node) => node.textContent?.includes('unknown'))!;
  expect(unknown.querySelector('summary')?.textContent).toContain('—');
});

it('keeps shares relative to the complete selection while paging and searching', async () => {
  await render(Array.from({ length: 13 }, (_, index) => item(`model-${index.toString().padStart(2, '0')}`)));
  expect(firstChart().querySelectorAll('details')).toHaveLength(6);
  expect(firstChart().querySelector('summary')?.textContent).toContain('7.7%');
  await click('Next');
  expect(firstChart().querySelector('summary')?.textContent).toContain('model-06');
  const search = firstChart().querySelector('input')!;
  await act(async () => { search.value = 'model-12'; search.dispatchEvent(new Event('input', { bubbles: true })); });
  expect(firstChart().querySelectorAll('details')).toHaveLength(1);
  expect(firstChart().querySelector('summary')?.textContent).toContain('7.7%');
});

it('retains expanded details while polling and distinguishes empty loading', async () => {
  await render([item('model-a')]);
  const details = firstChart().querySelector('details')!;
  details.open = true;
  await render([item('model-a', { requests: 20 })], true);
  expect(firstChart().querySelector('details')?.open).toBe(true);
  expect(firstChart().textContent).toContain('Input');
  await act(async () => root.render(<UsageComparisonCharts loading />));
  expect(container.querySelector('[aria-busy="true"]')).not.toBeNull();
  expect(container.textContent).not.toContain('model-a');
});

it('shows model efficiency for Key Viewer using per-request metrics and unknown cache values', async () => {
  await act(async () => root.render(<UsageComparisonCharts keyViewer loading={false} comparisons={{models:[item('small', {requests: 100}), item('large', {requests: 2}), item('no-input', {input_tokens:0})]}} />));
  expect(container.querySelector('[data-comparison="api_keys"]')).toBeNull();
  const chart = container.querySelector('[data-comparison="efficiency"]')!;
  expect(chart.querySelector('summary')?.textContent).toContain('large');
  const cacheButton = Array.from(chart.querySelectorAll('button')).find((button) => button.textContent === 'Cache rate')!;
  await act(async () => cacheButton.click());
  const missing = Array.from(chart.querySelectorAll('details')).find((row) => row.textContent?.includes('no-input'))!;
  expect(missing.querySelector('summary')?.textContent).toContain('—');
});
