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
  fetchUsageOverview: vi.fn(),
  fetchUsageOverviewRealtime: vi.fn(),
  fetchUsageActivity: vi.fn(),
  fetchAnalysis: vi.fn(),
  fetchAnalysisLatency: vi.fn(),
}));

// 此处验证跨页查询范围，图表绘制由组件测试覆盖。
vi.mock('react-chartjs-2', () => ({
  Bar: () => null,
  Chart: () => null,
  Doughnut: () => null,
  Line: () => null,
  Scatter: () => null,
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

describe('UsagePage top API Key request event filter', () => {
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
    localStorage.setItem('cli-proxy-usage-time-range-v1', '24h');
    for (const mock of [api.fetchUsageOverview, api.fetchUsageOverviewRealtime, api.fetchUsageActivity, api.fetchAnalysis, api.fetchAnalysisLatency]) {
      mock.mockReset().mockReturnValue(new Promise(() => undefined));
    }
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
    vi.useRealTimers();
    vi.restoreAllMocks();
    localStorage.clear();
  });

  const render = async () => { await act(async () => root.render(<UsagePage />)); };
  const button = (text: string) => Array.from(container.querySelectorAll<HTMLButtonElement>('button')).find((node) => node.textContent?.trim() === text)!;
  const topKey = () => container.querySelector<HTMLButtonElement>('[data-dashboard-toolbar] button[aria-label^="API Key: "]')!;
  const storedFilters = () => JSON.parse(localStorage.getItem(REQUEST_EVENTS_PREFERENCES_STORAGE_KEY)!).filters;
  const choose = async (control: HTMLElement, label: string) => {
    await act(async () => control.click());
    const option = Array.from(document.querySelectorAll<HTMLButtonElement>('[role="option"]')).find((node) => node.textContent === label)!;
    expect(option).toBeDefined();
    await act(async () => option.click());
  };

  it.each(['csv', 'json'] as const)('uses the top key and range for initial load, pagination, refresh and %s export', async (format) => {
    await render();
    const filters = { model: 'gpt-5', apiKeyId: '11', source: 'source-1', result: 'failed' };
    const range = expect.objectContaining({ range: '24h' });
    expect(api.fetchUsageEvents).toHaveBeenCalledExactlyOnceWith(range, expect.any(AbortSignal), expect.objectContaining(filters));
    expect(container.querySelector('input[aria-label="API Key"]')).toBeNull();
    await act(async () => button('Load more').click());
    expect(api.fetchUsageEvents).toHaveBeenLastCalledWith(range, expect.any(AbortSignal), expect.objectContaining({ ...filters, cursor: 'cursor-101' }));

    const calls = api.fetchUsageEvents.mock.calls.length;
    await choose(topKey(), 'Other key');
    expect(api.fetchUsageEvents).toHaveBeenCalledTimes(calls + 1);
    expect(api.fetchUsageEvents).toHaveBeenLastCalledWith(range, expect.any(AbortSignal), expect.objectContaining({ ...filters, apiKeyId: '33' }));
    expect(api.fetchUsageEvents.mock.lastCall![2].cursor).toBeUndefined();
    expect(localStorage.getItem(TOP_KEY_STORAGE)).toBe('33');
    expect(storedFilters()).toEqual({ model: 'gpt-5', source: 'source-1', result: 'failed' });

    await act(async () => triggerHeaderRefresh());
    expect(api.fetchUsageEvents).toHaveBeenCalledTimes(calls + 2);
    expect(api.fetchUsageEvents).toHaveBeenLastCalledWith(range, expect.any(AbortSignal), expect.objectContaining({ ...filters, apiKeyId: '33' }));
    expect(api.fetchUsageEvents.mock.lastCall![2].cursor).toBeUndefined();
    await act(async () => button('Load more').click());
    expect(api.fetchUsageEvents).toHaveBeenLastCalledWith(range, expect.any(AbortSignal), expect.objectContaining({ ...filters, apiKeyId: '33', cursor: 'cursor-101' }));
    await act(async () => button('Export').click());
    await act(async () => button(`Export ${format.toUpperCase()}`).click());
    expect(api.exportUsageEvents).toHaveBeenLastCalledWith(range, format, { ...filters, apiKeyId: '33' });
  });

  it('restores the top selection and clears only Model, Source and Status', async () => {
    await render();
    await choose(topKey(), 'Other key');
    await act(async () => root.unmount());
    root = createRoot(container);
    await render();
    expect(topKey().textContent).toContain('Other key');
    expect(api.fetchUsageEvents.mock.lastCall![2].apiKeyId).toBe('33');
    await act(async () => button('Clear Filters').click());
    expect(storedFilters()).toEqual({ model: '__all__', source: '__all__', result: '__all__' });
    expect(localStorage.getItem(TOP_KEY_STORAGE)).toBe('33');
    expect(api.fetchUsageEvents.mock.lastCall![2]).toMatchObject({ apiKeyId: '33', model: undefined, source: undefined, result: undefined });
    expect(button('Clear Filters').disabled).toBe(true);
  });

  it('ignores a legacy list key when the top selection is All', async () => {
    localStorage.removeItem(TOP_KEY_STORAGE);
    await render();
    expect(api.fetchUsageEvents.mock.lastCall![2].apiKeyId).toBe('');
    expect(localStorage.getItem(TOP_KEY_STORAGE)).toBe('');
    expect(storedFilters()).not.toHaveProperty('apiKeyId');
  });

  it.each([undefined, '22'])('waits for saved top key options before querying, refreshing or exporting with legacy list key %s', async (apiKeyId) => {
    localStorage.setItem(REQUEST_EVENTS_PREFERENCES_STORAGE_KEY, JSON.stringify({ version: 9, filters: { model: 'gpt-5', apiKeyId } }));
    let resolveOptions!: (value: typeof keyOptions) => void;
    api.fetchCpaApiKeyOptions.mockReturnValue(new Promise((resolve) => { resolveOptions = resolve; }));
    await render();
    await act(async () => triggerHeaderRefresh());
    await act(async () => button('Export').click());
    await act(async () => button('Export CSV').click());
    expect(api.fetchUsageEvents).not.toHaveBeenCalled();
    expect(api.exportUsageEvents).not.toHaveBeenCalled();
    await act(async () => resolveOptions(keyOptions));
    expect(api.fetchUsageEvents).toHaveBeenCalledExactlyOnceWith(expect.anything(), expect.any(AbortSignal), expect.objectContaining({ apiKeyId: '11' }));
    expect(storedFilters()).not.toHaveProperty('apiKeyId');
    expect(localStorage.getItem(TOP_KEY_STORAGE)).toBe('11');
  });

  it('preserves the saved top key when loading options fails', async () => {
    api.fetchCpaApiKeyOptions.mockRejectedValue(new Error('offline'));
    await render();
    expect(api.fetchUsageEvents.mock.lastCall![2].apiKeyId).toBe('11');
    expect(storedFilters()).not.toHaveProperty('apiKeyId');
    expect(localStorage.getItem(TOP_KEY_STORAGE)).toBe('11');
  });

  it('clears a missing top key only after options load successfully', async () => {
    api.fetchCpaApiKeyOptions.mockResolvedValue({ options: [keyOptions.options[1]] });
    await render();
    expect(api.fetchUsageEvents.mock.calls.every((call) => call[2].apiKeyId === '')).toBe(true);
    expect(api.fetchUsageEvents).toHaveBeenCalled();
    expect(localStorage.getItem(TOP_KEY_STORAGE)).toBe('');
    expect(storedFilters()).not.toHaveProperty('apiKeyId');
  });

  it.each(['initial load', 'load more'] as const)('aborts the previous key %s and ignores its late response and cursor', async (phase) => {
    let resolveOld!: (value: UsageEventsResponse) => void;
    const oldResponse = new Promise<UsageEventsResponse>((resolve) => { resolveOld = resolve; });
    if (phase === 'initial load') {
      api.fetchUsageEvents.mockReturnValueOnce(oldResponse);
      await render();
    } else {
      await render();
      api.fetchUsageEvents.mockReturnValueOnce(oldResponse);
      await act(async () => button('Load more').click());
    }
    const oldSignal = api.fetchUsageEvents.mock.lastCall![1] as AbortSignal;
    await choose(topKey(), 'Other key');
    expect(oldSignal.aborted).toBe(true);
    expect(api.fetchUsageEvents.mock.lastCall![2]).toMatchObject({ apiKeyId: '33' });
    expect(api.fetchUsageEvents.mock.lastCall![2].cursor).toBeUndefined();
    await act(async () => resolveOld({
      ...firstPage, events: [{ ...firstPage.events[0], id: 'old-key-event', model: 'stale-model' }],
      next_cursor: 'stale-cursor',
    }));
    expect(container.textContent).not.toContain('stale-model');
    await act(async () => button('Load more').click());
    expect(api.fetchUsageEvents.mock.lastCall![2]).toMatchObject({ apiKeyId: '33', cursor: 'cursor-101' });
  });

  it('resets pagination immediately while a new top key query is pending and resumes auto refresh on page one', async () => {
    vi.useFakeTimers();
    await render();
    // 旧 Key 的第二批仍有下一页，才能验证切换时确实丢弃了旧游标。
    api.fetchUsageEvents.mockResolvedValueOnce({
      ...firstPage,
      events: [{ ...firstPage.events[0], id: '100' }],
      next_cursor: 'cursor-old-page-2',
    });
    await act(async () => button('Load more').click());
    expect(button('Load more').disabled).toBe(false);
    let resolveNew!: (value: UsageEventsResponse) => void;
    api.fetchUsageEvents.mockReturnValueOnce(new Promise<UsageEventsResponse>((resolve) => { resolveNew = resolve; }));
    await choose(topKey(), 'Other key');
    expect(api.fetchUsageEvents.mock.lastCall![2].cursor).toBeUndefined();
    expect(button('Load more')).toBeUndefined();
    await act(async () => resolveNew({ ...firstPage, next_cursor: 'cursor-new-key' }));
    const calls = api.fetchUsageEvents.mock.calls.length;
    await act(async () => vi.advanceTimersByTimeAsync(15_000));
    expect(api.fetchUsageEvents.mock.calls.length).toBeGreaterThan(calls);
    expect(api.fetchUsageEvents.mock.lastCall![2]).toMatchObject({ apiKeyId: '33' });
    expect(api.fetchUsageEvents.mock.lastCall![2].cursor).toBeUndefined();
  });

  it('applies a changed top range together with the top key to queries and export', async () => {
    await render();
    await act(async () => button('Load more').click());
    await act(async () => container.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]')!.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-time-range-mode="yesterday"]')!.click());
    expect(api.fetchUsageEvents).toHaveBeenLastCalledWith(expect.objectContaining({ range: 'yesterday' }), expect.any(AbortSignal), expect.objectContaining({ apiKeyId: '11' }));
    expect(api.fetchUsageEvents.mock.lastCall![2].cursor).toBeUndefined();
    await act(async () => button('Export').click());
    await act(async () => button('Export JSON').click());
    expect(api.exportUsageEvents).toHaveBeenLastCalledWith(expect.objectContaining({ range: 'yesterday' }), 'json', expect.objectContaining({ apiKeyId: '11' }));
  });

  it('isolates Overview and Realtime requests while retaining the shared API Key filter', async () => {
    await render();
    await choose(topKey(), 'Other key');
    const navigate = async (path: string) => {
      await act(async () => container.querySelector<HTMLAnchorElement>(`[data-dashboard-toolbar] a[href="${path}"]`)!.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, button: 0 })));
    };
    await navigate('/overview');
    expect(api.fetchUsageOverview).toHaveBeenLastCalledWith(expect.anything(), expect.any(AbortSignal), '33');
    expect(api.fetchUsageOverviewRealtime).not.toHaveBeenCalled();
    expect(api.fetchUsageActivity).toHaveBeenLastCalledWith(expect.objectContaining({ apiKeyId: '33' }));
    const overviewCalls = api.fetchUsageOverview.mock.calls.length;
    const activityCalls = api.fetchUsageActivity.mock.calls.length;
    await navigate('/realtime');
    expect(api.fetchUsageOverviewRealtime).toHaveBeenLastCalledWith(expect.objectContaining({ apiKeyId: '33' }));
    expect(container.querySelector('[data-time-range-trigger]')).toBeNull();
    await act(async () => { void triggerHeaderRefresh(); });
    expect(api.fetchUsageOverview.mock.calls.length).toBe(overviewCalls);
    expect(api.fetchUsageActivity.mock.calls.length).toBe(activityCalls);
    await navigate('/analysis');
    expect(api.fetchAnalysis).toHaveBeenLastCalledWith(expect.anything(), expect.any(AbortSignal), '33');
    expect(api.fetchAnalysisLatency).toHaveBeenLastCalledWith(expect.anything(), expect.any(AbortSignal), '33');
    await navigate('/request-events');
    expect(api.fetchUsageEvents.mock.lastCall![2].apiKeyId).toBe('33');
    await choose(topKey(), 'All');
    expect(api.fetchUsageEvents.mock.lastCall![2].apiKeyId).toBe('');
    expect(localStorage.getItem(TOP_KEY_STORAGE)).toBe('');
  });
});
