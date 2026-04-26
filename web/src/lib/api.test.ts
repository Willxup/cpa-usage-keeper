import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchUsageEventFilterOptions, fetchUsageEvents, triggerSync } from './api';

describe('fetchUsageEvents', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('loads stable filter options without event pagination or selected filters', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ models: ['claude-sonnet'], sources: [{ value: 'source-a', label: 'Provider A' }] }),
    } as Response);
    const signal = new AbortController().signal;

    const response = await fetchUsageEventFilterOptions('custom', '2026-04-20T00:00:00Z', '2026-04-21T00:00:00Z', signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.models).toEqual(['claude-sonnet']);
    expect(parsed.pathname).toBe('/api/v1/usage/events/filters');
    expect(parsed.searchParams.get('range')).toBe('custom');
    expect(parsed.searchParams.get('start')).toBe('2026-04-20T00:00:00Z');
    expect(parsed.searchParams.get('end')).toBe('2026-04-21T00:00:00Z');
    expect(parsed.searchParams.get('page')).toBeNull();
    expect(parsed.searchParams.get('page_size')).toBeNull();
    expect(parsed.searchParams.get('model')).toBeNull();
    expect(parsed.searchParams.get('source')).toBeNull();
    expect(parsed.searchParams.get('result')).toBeNull();
    expect(init).toMatchObject({ credentials: 'include', signal });
  });

  it('passes pagination and server-side filters as query params', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ events: [], models: [], sources: [], total_count: 0, page: 3, page_size: 100, total_pages: 0 }),
    } as Response);
    const signal = new AbortController().signal;

    await fetchUsageEvents('custom', '2026-04-20T00:00:00Z', '2026-04-21T00:00:00Z', signal, {
      page: 3,
      pageSize: 100,
      model: 'claude-sonnet',
      source: 'source-a',
      result: 'failed',
    });

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(parsed.pathname).toBe('/api/v1/usage/events');
    expect(parsed.searchParams.get('range')).toBe('custom');
    expect(parsed.searchParams.get('start')).toBe('2026-04-20T00:00:00Z');
    expect(parsed.searchParams.get('end')).toBe('2026-04-21T00:00:00Z');
    expect(parsed.searchParams.get('page')).toBe('3');
    expect(parsed.searchParams.get('page_size')).toBe('100');
    expect(parsed.searchParams.get('model')).toBe('claude-sonnet');
    expect(parsed.searchParams.get('source')).toBe('source-a');
    expect(parsed.searchParams.get('result')).toBe('failed');
    expect(parsed.searchParams.get('auth_index')).toBeNull();
    expect(init).toMatchObject({ credentials: 'include', signal });
  });

  it('posts to the manual sync endpoint', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ running: true, sync_running: false, last_status: 'completed' }),
    } as Response);
    const signal = new AbortController().signal;

    const response = await triggerSync(signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.last_status).toBe('completed');
    expect(parsed.pathname).toBe('/api/v1/sync');
    expect(init).toMatchObject({ credentials: 'include', method: 'POST', signal });
  });
});
