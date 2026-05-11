import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchUsageQuotaCache, fetchUpdateCheck, fetchUsageEventModelFilterOptions, fetchUsageEventSourceFilterOptions, fetchUsageEvents, fetchUsageIdentities, fetchUsageIdentitiesPage, fetchUsageQuotaCheck, fetchUsageQuotaRefreshTask, refreshUsageQuotas, triggerSync } from './api';

describe('fetchUsageEvents', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('loads model filter options without query params', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ models: ['claude-sonnet'] }),
    } as Response);
    const signal = new AbortController().signal;

    const response = await fetchUsageEventModelFilterOptions(signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.models).toEqual(['claude-sonnet']);
    expect(parsed.pathname).toBe('/api/v1/usage/events/filters/models');
    expect(parsed.search).toBe('');
    expect(parsed.searchParams.get('range')).toBeNull();
    expect(parsed.searchParams.get('start')).toBeNull();
    expect(parsed.searchParams.get('end')).toBeNull();
    expect(parsed.searchParams.get('page')).toBeNull();
    expect(parsed.searchParams.get('page_size')).toBeNull();
    expect(parsed.searchParams.get('model')).toBeNull();
    expect(parsed.searchParams.get('source')).toBeNull();
    expect(parsed.searchParams.get('result')).toBeNull();
    expect(init).toMatchObject({ credentials: 'include', signal, cache: 'no-store' });
  });

  it('loads source filter options without query params', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ sources: [{ value: 'source-a', label: 'Provider A' }] }),
    } as Response);
    const signal = new AbortController().signal;

    const response = await fetchUsageEventSourceFilterOptions(signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.sources).toEqual([{ value: 'source-a', label: 'Provider A' }]);
    expect(parsed.pathname).toBe('/api/v1/usage/events/filters/sources');
    expect(parsed.search).toBe('');
    expect(init).toMatchObject({ credentials: 'include', signal, cache: 'no-store' });
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
      source: 'authidx-source-a',
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
    expect(parsed.searchParams.get('source')).toBe('authidx-source-a');
    expect(parsed.searchParams.get('result')).toBe('failed');
    expect(parsed.searchParams.get('auth_index')).toBeNull();
    expect(init).toMatchObject({ credentials: 'include', signal });
  });

  it('loads unified usage identities without query params', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        identities: [
          {
            id: 1,
            name: 'Claude primary',
            auth_type: 2,
            auth_type_name: 'apikey',
            identity: 'sk-a***1234',
            type: 'claude',
            provider: 'anthropic',
            total_requests: 3,
            success_count: 2,
            failure_count: 1,
            input_tokens: 10,
            output_tokens: 20,
            reasoning_tokens: 0,
            cached_tokens: 0,
            total_tokens: 30,
            last_aggregated_usage_event_id: 9,
            is_deleted: false,
            created_at: '2026-05-04T00:00:00Z',
            updated_at: '2026-05-04T00:00:00Z',
          },
        ],
      }),
    } as Response);
    const signal = new AbortController().signal;

    const response = await fetchUsageIdentities(signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.identities[0].identity).toBe('sk-a***1234');
    expect(response.identities[0].auth_type).toBe(2);
    expect(typeof response.identities[0].auth_type).toBe('number');
    expect(parsed.pathname).toBe('/api/v1/usage/identities');
    expect(parsed.search).toBe('');
    expect(init).toMatchObject({ credentials: 'include', signal });
  });

  it('loads paged usage identities for one credential auth type', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ identities: [], total_count: 25, page: 3, page_size: 10, total_pages: 3 }),
    } as Response);
    const signal = new AbortController().signal;

    const response = await fetchUsageIdentitiesPage(signal, { authType: 2, page: 3, pageSize: 10 });

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.total_count).toBe(25);
    expect(parsed.pathname).toBe('/api/v1/usage/identities/page');
    expect(parsed.searchParams.get('auth_type')).toBe('2');
    expect(parsed.searchParams.get('page')).toBe('3');
    expect(parsed.searchParams.get('page_size')).toBe('10');
    expect(init).toMatchObject({ credentials: 'include', signal });
  });

  it('checks quota for a single auth index', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        id: 'auth-1',
        quota: [{ key: 'rate_limit.primary_window', label: '5h', remaining: 12 }],
      }),
    } as Response);
    const signal = new AbortController().signal;

    const response = await fetchUsageQuotaCheck('auth-1', signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.id).toBe('auth-1');
    expect(response.quota[0].remaining).toBe(12);
    expect(parsed.pathname).toBe('/api/v1/quota/check');
    expect(init).toMatchObject({ credentials: 'include', method: 'POST', signal });
    expect(init?.headers).toEqual({ 'Content-Type': 'application/json' });
    expect(init?.body).toBe(JSON.stringify({ auth_index: 'auth-1' }));
  });

  it('loads cached quota for current page auth indexes without refreshing', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        items: [{ id: 'auth-1', quota: [{ key: 'rate_limit.secondary_window', label: 'Weekly', remaining: 12 }] }],
      }),
    } as Response);
    const signal = new AbortController().signal;

    const response = await fetchUsageQuotaCache(['auth-1'], signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.items[0].id).toBe('auth-1');
    expect(response.items[0].quota[0].remaining).toBe(12);
    expect(parsed.pathname).toBe('/api/v1/quota/cache');
    expect(init).toMatchObject({ credentials: 'include', method: 'POST', signal });
    expect(init?.headers).toEqual({ 'Content-Type': 'application/json' });
    expect(init?.body).toBe(JSON.stringify({ auth_indexes: ['auth-1'] }));
  });

  it('creates quota refresh tasks for current page auth indexes', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        tasks: [{ authIndex: 'auth-1', taskId: 'task-1' }],
        rejected: [],
        accepted: 1,
        skipped: 0,
        limit: 20,
      }),
    } as Response);
    const signal = new AbortController().signal;

    const response = await refreshUsageQuotas(['auth-1'], signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.tasks[0]).toEqual({ authIndex: 'auth-1', taskId: 'task-1' });
    expect(response.limit).toBe(20);
    expect(parsed.pathname).toBe('/api/v1/quota/refresh');
    expect(init).toMatchObject({ credentials: 'include', method: 'POST', signal });
    expect(init?.headers).toEqual({ 'Content-Type': 'application/json' });
    expect(init?.body).toBe(JSON.stringify({ auth_indexes: ['auth-1'], limit: 20 }));
  });

  it('loads quota refresh task status', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        taskId: 'task-1',
        authIndex: 'auth-1',
        status: 'completed',
        quota: { id: 'auth-1', quota: [{ key: 'rate_limit.primary_window', label: '5h' }] },
      }),
    } as Response);
    const signal = new AbortController().signal;

    const response = await fetchUsageQuotaRefreshTask('task-1', signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.status).toBe('completed');
    expect(response.quota?.id).toBe('auth-1');
    expect(parsed.pathname).toBe('/api/v1/quota/refresh/task-1');
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

  it('loads update check status from the protected endpoint', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        currentVersion: 'v1.2.3',
        latestVersion: 'v1.2.4',
        updateAvailable: true,
        canCompare: true,
        message: 'new version available: v1.2.4',
      }),
    } as Response);
    const signal = new AbortController().signal;

    const response = await fetchUpdateCheck(signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.latestVersion).toBe('v1.2.4');
    expect(response.updateAvailable).toBe(true);
    expect(parsed.pathname).toBe('/api/v1/update/check');
    expect(init).toMatchObject({ credentials: 'include', signal });
  });
});
