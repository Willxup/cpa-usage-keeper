// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import { triggerHeaderRefresh } from '@/hooks/useHeaderRefresh';
import type { UsageEventsResponse } from '@/lib/types';

const api = vi.hoisted(() => ({
  fetchCpaApiKeyOptions: vi.fn(),
  fetchUsageEvents: vi.fn(),
  exportUsageEvents: vi.fn(),
}));

vi.mock('@/lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/lib/api')>(),
  ...api,
  fetchStatus: async () => ({ timezone: 'UTC' }),
  fetchVersion: async () => ({ version: 'test' }),
  fetchUsageEventModelFilterOptions: async () => ({ models: ['gpt-5'] }),
  fetchUsageEventSourceFilterOptions: async () => ({ sources: [{ value: 'source-1', label: 'Team source' }] }),
}));

import { UsagePage, REQUEST_EVENTS_PREFERENCES_STORAGE_KEY } from '../UsagePage';

const TOP_KEY_STORAGE = 'cli-proxy-usage-api-key-filter-v1';
const keyOptions = { options: [{ id: '11', label: 'Overview key' }, { id: '22', label: 'Events key' }, { id: '33', label: 'Other key' }] };
const firstPage: UsageEventsResponse = {
  events: [{
    id: '101', timestamp: '2026-09-07T01:00:00Z', model: 'gpt-5', source: 'Team source',
    failed: false, latency_ms: 100,
    tokens: { input_tokens: 1, output_tokens: 1, reasoning_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, total_tokens: 2 },
  }],
  total_count: 2, page: 1, page_size: 50, total_pages: 1, has_more: true, next_cursor: 'cursor-101',
};

describe('UsagePage independent request event API Key filter', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    await i18n.changeLanguage('en');
    localStorage.clear();
    window.history.replaceState(null, '', '/request-events');
    localStorage.setItem(TOP_KEY_STORAGE, '11');
    localStorage.setItem(REQUEST_EVENTS_PREFERENCES_STORAGE_KEY, JSON.stringify({
      version: 9, filters: { model: 'gpt-5', apiKeyId: '22', source: 'source-1', result: 'failed' },
    }));
    api.fetchCpaApiKeyOptions.mockReset().mockResolvedValue(keyOptions);
    api.fetchUsageEvents.mockReset().mockImplementation(async (_range, _signal, options) => options.cursor
      ? { ...firstPage, events: [{ ...firstPage.events[0], id: '100' }], has_more: false, next_cursor: null }
      : firstPage);
    api.exportUsageEvents.mockReset().mockResolvedValue({ blob: new Blob(['events']), filename: 'events.csv' });
    vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockReturnValue(2000);
    vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(400);
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined);
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    vi.restoreAllMocks();
    localStorage.clear();
  });

  const render = async () => { await act(async () => root.render(<UsagePage />)); };
  const button = (text: string) => Array.from(container.querySelectorAll<HTMLButtonElement>('button')).find((node) => node.textContent?.trim() === text)!;
  const eventKey = () => container.querySelector<HTMLInputElement>('input[aria-label="API Key"]')!;
  const topKey = () => container.querySelector<HTMLButtonElement>('button[aria-label="API Key"]')!;
  const storedFilters = () => JSON.parse(localStorage.getItem(REQUEST_EVENTS_PREFERENCES_STORAGE_KEY)!).filters;
  const choose = async (control: HTMLElement, label: string) => {
    await act(async () => control.click());
    const option = Array.from(document.querySelectorAll<HTMLButtonElement>('[role="option"]')).find((node) => node.textContent === label)!;
    expect(option).toBeDefined();
    await act(async () => option.click());
  };

  it.each(['csv', 'json'] as const)('uses the event key for initial load, pagination and %s export while top changes leave the list intact', async (format) => {
    await render();
    const filters = { model: 'gpt-5', apiKeyId: '22', source: 'source-1', result: 'failed' };
    expect(api.fetchUsageEvents).toHaveBeenLastCalledWith(expect.anything(), expect.any(AbortSignal), expect.objectContaining(filters));
    expect(eventKey().value).toBe('Events key');
    await act(async () => button('Load more').click());
    expect(api.fetchUsageEvents).toHaveBeenLastCalledWith(expect.anything(), expect.any(AbortSignal), expect.objectContaining({ ...filters, cursor: 'cursor-101' }));
    const calls = api.fetchUsageEvents.mock.calls.length;
    await choose(topKey(), 'Other key');
    expect(api.fetchUsageEvents).toHaveBeenCalledTimes(calls);
    expect(eventKey().value).toBe('Events key');
    expect(storedFilters().apiKeyId).toBe('22');
    expect(localStorage.getItem(TOP_KEY_STORAGE)).toBe('33');
    await act(async () => button('Export').click());
    await act(async () => button(`Export ${format.toUpperCase()}`).click());
    expect(api.exportUsageEvents).toHaveBeenLastCalledWith(expect.anything(), format, filters);
  });

  it('persists only the event selection and clears all four list filters without changing the top key', async () => {
    await render();
    await act(async () => button('Load more').click());
    await choose(eventKey(), 'Other key');
    expect(api.fetchUsageEvents).toHaveBeenLastCalledWith(expect.anything(), expect.any(AbortSignal), expect.objectContaining({ apiKeyId: '33' }));
    expect(api.fetchUsageEvents.mock.lastCall![2].cursor).toBeUndefined();
    expect(storedFilters().apiKeyId).toBe('33');
    expect(localStorage.getItem(TOP_KEY_STORAGE)).toBe('11');
    await act(async () => root.unmount());
    root = createRoot(container);
    await render();
    expect(eventKey().value).toBe('Other key');
    expect(topKey().textContent).toContain('Overview key');
    await act(async () => button('Clear Filters').click());
    expect(storedFilters()).toEqual({ model: '__all__', apiKeyId: '', source: '__all__', result: '__all__' });
    expect(localStorage.getItem(TOP_KEY_STORAGE)).toBe('11');
    expect(api.fetchUsageEvents.mock.lastCall![2]).toMatchObject({ apiKeyId: '', model: undefined, source: undefined, result: undefined });
  });

  it('defaults old event preferences to all keys and can refresh while the top key is still resolving', async () => {
    localStorage.setItem(REQUEST_EVENTS_PREFERENCES_STORAGE_KEY, JSON.stringify({ version: 9, filters: { model: 'gpt-5' } }));
    api.fetchCpaApiKeyOptions.mockReturnValue(new Promise(() => undefined));
    await render();
    expect(api.fetchUsageEvents.mock.lastCall?.[2].apiKeyId).toBe('');
    const calls = api.fetchUsageEvents.mock.calls.length;
    await act(async () => triggerHeaderRefresh());
    expect(api.fetchUsageEvents).toHaveBeenCalledTimes(calls + 1);
    expect(storedFilters().apiKeyId).toBe('');
    expect(localStorage.getItem(TOP_KEY_STORAGE)).toBe('11');
  });

  it('preserves a saved event key when loading options fails', async () => {
    api.fetchCpaApiKeyOptions.mockRejectedValue(new Error('offline'));
    await render();
    expect(api.fetchUsageEvents.mock.lastCall?.[2].apiKeyId).toBe('22');
    expect(storedFilters().apiKeyId).toBe('22');
    expect(localStorage.getItem(TOP_KEY_STORAGE)).toBe('11');
  });

  it('aborts loading more from the previous key and ignores its late response', async () => {
    await render();
    let resolveMore!: (value: UsageEventsResponse) => void;
    api.fetchUsageEvents.mockReturnValueOnce(new Promise<UsageEventsResponse>((resolve) => { resolveMore = resolve; }));
    await act(async () => button('Load more').click());
    const oldSignal = api.fetchUsageEvents.mock.lastCall![1] as AbortSignal;
    await choose(eventKey(), 'Other key');
    expect(oldSignal.aborted).toBe(true);
    expect(api.fetchUsageEvents.mock.lastCall![2]).toMatchObject({ apiKeyId: '33' });
    expect(api.fetchUsageEvents.mock.lastCall![2].cursor).toBeUndefined();
    await act(async () => resolveMore({ ...firstPage, events: [{ ...firstPage.events[0], id: 'old-key-event', model: 'stale-model' }] }));
    expect(container.textContent).not.toContain('stale-model');
    expect(storedFilters().apiKeyId).toBe('33');
  });

  it('clears a missing event key only after options load successfully', async () => {
    api.fetchCpaApiKeyOptions.mockResolvedValue({ options: [keyOptions.options[0]] });
    await render();
    expect(api.fetchUsageEvents.mock.lastCall?.[2].apiKeyId).toBe('');
    expect(storedFilters().apiKeyId).toBe('');
    expect(localStorage.getItem(TOP_KEY_STORAGE)).toBe('11');
  });
});
