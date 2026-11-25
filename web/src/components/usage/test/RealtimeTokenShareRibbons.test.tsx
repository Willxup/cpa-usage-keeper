// @vitest-environment happy-dom

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import { RealtimeTokenShareRibbons } from '../RealtimeTokenShareRibbons';

const label = 'very-long-credential-name-without-any-spaces.json';
const items = [
  { key: '__realtime_others__', label, tokens: 987, requests: 7, share: 98.7, cost: 1, input_tokens: 300, cache_read_tokens: 100 },
  { key: 'tiny', label: 'tiny', tokens: 3, requests: 1, share: 0.3, cost: null, input_tokens: 0, cache_read_tokens: 0 },
  { key: 'b', label: 'b', tokens: 3, requests: 1, share: 0.3, input_tokens: 2 },
  { key: 'c', label: 'c', tokens: 3, requests: 1, share: 0.3, cache_read_tokens: 1 },
  { key: 'd', label: 'd', tokens: 2, requests: 1, share: 0.2, input_tokens: 2, cache_read_tokens: 0 },
  { key: '__realtime_others__', label: 'Other', tokens: 2, requests: 1, share: 0.2 },
];

describe('RealtimeTokenShareRibbons', () => {
  let container: HTMLDivElement;
  let root: Root;
  beforeEach(async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    await i18n.changeLanguage('en');
    container = document.createElement('div');
    root = createRoot(container);
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, 120, 100));
  });
  afterEach(async () => {
    await act(async () => root.unmount());
    vi.restoreAllMocks();
  });

  it('shows the label and share above the token count, cache rate and cost', async () => {
    await act(async () => root.render(<RealtimeTokenShareRibbons loading={false} items={items} />));
    const rows = container.querySelectorAll('[data-ribbon-row]');
    const topline = rows[0].firstElementChild!;
    expect(Array.from(topline.children, (el) => el.textContent)).toEqual(['', label, '98.70%']);
    expect(rows[5].textContent).toContain('Others');
    expect(rows[1].textContent).toContain('0.30%');
    const metrics = rows[0].querySelector('[data-metric="cache"]')!.parentElement!;
    expect(Array.from(metrics.children, (el) => el.textContent)).toEqual(['Tokens 987', 'Cache Rate 33.33%', 'Cost $1.00']);
    expect(rows[1].textContent).toContain('Cost —');
    expect(Array.from(rows, (row) => row.querySelector('[data-metric="cache"] strong')!.textContent)).toEqual(['33.33%', '—', '—', '—', '0.00%', '—']);
    expect(container.querySelector('[class*="overviewRealtimeUsageMetaPill"]')).toBeNull();
  });

  it('reuses the first composition color for the seventh row', async () => {
    const sevenItems = [...items, { key: 'extra', label: 'extra', tokens: 0, requests: 0, share: 0 }];
    await act(async () => root.render(<RealtimeTokenShareRibbons loading={false} items={sevenItems} />));
    expect(container.querySelector<HTMLElement>('[data-ribbon-row="6"] [aria-hidden]')!.style.backgroundColor).toBe('#3b82f6');
  });

  it.each([false, true])('uses the composition gradient and neutral synthetic Other (dark=%s)', async (isDark) => {
    await act(async () => root.render(<RealtimeTokenShareRibbons loading={false} items={items} isDark={isDark} />));
    const base = ['#3b82f6', '#0d9488', '#8b5cf6', '#f59e0b', '#06b6d4', '#94a3b8'];
    const light = ['#60a5fa', '#2dd4bf', '#a78bfa', '#fbbf24', '#22d3ee', '#cbd5e1'];
    const lightShareText = ['#235ee7', '#0f766e', '#7c3aed', '#a84d08', '#0e7490', '#5b697c'];
    const groups = container.querySelectorAll('[data-ribbon-path]');
    const gradients = container.querySelectorAll('svg > defs > *');
    expect(groups).toHaveLength(items.length);
    expect(gradients).toHaveLength(groups.length);
    groups.forEach((group, index) => {
      const accent = isDark ? light[index] : base[index];
      const shareColor = isDark ? light[index] : lightShareText[index];
      const gradient = gradients[index];
      const stops = gradient.querySelectorAll('stop');
      expect(gradient.getAttribute('gradientUnits')).toBe('userSpaceOnUse');
      expect(group.querySelector('path')!.getAttribute('fill')).toBe(`url(#${gradient.id})`);
      expect(Array.from(stops, (stop) => stop.getAttribute('stop-color'))).toEqual([base[index], light[index]]);
      expect(Array.from(stops, (stop) => stop.getAttribute('stop-opacity'))).toEqual(isDark ? ['0.44', '0.26'] : ['0.38', '0.22']);
      expect(Array.from(group.querySelectorAll('rect'), (rect) => rect.getAttribute('fill'))).toEqual([accent, accent]);
      const topline = container.querySelector(`[data-ribbon-row="${index}"]`)!.firstElementChild!;
      expect(topline.querySelector<HTMLElement>('[aria-hidden]')!.style.backgroundColor).toBe(accent);
      expect(topline.querySelector('strong')?.style.color).toBe(shareColor);
    });
  });
});
