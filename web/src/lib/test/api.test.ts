import { afterEach, describe, expect, it, vi } from 'vitest';
import { appPath, createUsageEventRequestLogDownloadURL, deleteAuthFiles, exportUsageEvents, fetchAnalysis, fetchAuthSessions, fetchCpaApiKeyOptions, fetchCpaApiKeys, fetchCpaApiKeySettings, fetchKeyOverview, fetchKeyOverviewRealtime, fetchQuotaAutoRefreshSettings, fetchUsageOverview, fetchUsageOverviewRealtime, fetchUsageQuotaCache, fetchUsageQuotaInspectionStatus, fetchUpdateCheck, fetchUsageEventModelFilterOptions, fetchUsageEventRequestLog, fetchUsageEventSourceFilterOptions, fetchUsageEvents, fetchUsageIdentities, fetchUsageIdentitiesPage, fetchUsageQuotaRefreshTask, fetchVersion, loginWithCPAAPIKey, logout, refreshUsageQuotas, resetUsageQuota, revokeAuthSession, setAuthFilesDisabled, startUsageQuotaInspection, updateCpaApiKeyAlias, updateQuotaAutoRefreshSettings } from '../api';

const headerValue = (init: RequestInit | undefined, name: string): string | null => new Headers(init?.headers).get(name);

describe('fetchUsageEvents', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('builds app paths from the configured base path', () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper/' });

    expect(appPath('/key-overview')).toBe('/keeper/key-overview');
    expect(appPath('key-overview')).toBe('/keeper/key-overview');
  });

  it('posts CPA API key logins to the dedicated auth endpoint', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({}),
    } as Response);

    await loginWithCPAAPIKey('sk-cpa-viewer');

    const [url, init] = fetchMock.mock.calls[0];
    expect(new URL(String(url), 'http://localhost').pathname).toBe('/api/v1/auth/api-key-login');
    expect(init).toMatchObject({ credentials: 'include', method: 'POST' });
    expect(headerValue(init, 'Content-Type')).toBe('application/json');
    expect(init?.body).toBe(JSON.stringify({ apiKey: 'sk-cpa-viewer' }));
  });

  it('loads key overview with only the viewer range query', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ usage: { total_requests: 0, success_count: 0, failure_count: 0, total_tokens: 0 } }),
    } as Response);
    const signal = new AbortController().signal;

    await fetchKeyOverview('8h', signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');
    expect(parsed.pathname).toBe('/api/v1/key-overview');
    expect(parsed.searchParams.get('range')).toBe('8h');
    expect(parsed.searchParams.get('api_key_id')).toBeNull();
    expect(parsed.searchParams.get('start')).toBeNull();
    expect(parsed.searchParams.get('end')).toBeNull();
    expect(init).toMatchObject({ credentials: 'include', signal });
  });

  it('loads realtime overview from dedicated endpoints', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ usage: { total_requests: 0, success_count: 0, failure_count: 0, total_tokens: 0 } }),
    } as Response);
    const signal = new AbortController().signal;

    await fetchUsageOverview('24h', undefined, undefined, signal, '9007199254740993');
    await fetchUsageOverviewRealtime({ signal, apiKeyId: '9007199254740993', window: '60m' });
    await fetchKeyOverview('8h', signal);
    await fetchKeyOverviewRealtime({ window: '30m', signal });

    const overviewUrl = new URL(String(fetchMock.mock.calls[0][0]), 'http://localhost');
    const overviewRealtimeUrl = new URL(String(fetchMock.mock.calls[1][0]), 'http://localhost');
    const keyOverviewUrl = new URL(String(fetchMock.mock.calls[2][0]), 'http://localhost');
    const keyOverviewRealtimeUrl = new URL(String(fetchMock.mock.calls[3][0]), 'http://localhost');
    expect(overviewUrl.pathname).toBe('/api/v1/usage/overview');
    expect(overviewUrl.searchParams.get('realtime_window')).toBeNull();
    expect(overviewRealtimeUrl.pathname).toBe('/api/v1/usage/overview/realtime');
    expect(overviewRealtimeUrl.searchParams.get('window')).toBe('60m');
    expect(overviewRealtimeUrl.searchParams.get('api_key_id')).toBe('9007199254740993');
    expect(keyOverviewUrl.pathname).toBe('/api/v1/key-overview');
    expect(keyOverviewUrl.searchParams.get('realtime_window')).toBeNull();
    expect(keyOverviewRealtimeUrl.pathname).toBe('/api/v1/key-overview/realtime');
    expect(keyOverviewRealtimeUrl.searchParams.get('window')).toBe('30m');
    expect(keyOverviewRealtimeUrl.searchParams.get('api_key_id')).toBeNull();
  });

  it('normalizes key overview realtime responses that omit internal usage dimensions', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        window: '30m',
        bucket_seconds: 60,
        token_velocity: [],
        response_level: [],
        current_usage: { models: [{ key: 'gpt-5', label: 'gpt-5', tokens: 20, requests: 1, share: 100 }] },
        request_level: [],
        cache_level: [],
      }),
    } as Response);
    const signal = new AbortController().signal;

    const response = await fetchKeyOverviewRealtime({ window: '30m', signal });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(response.current_usage.models).toEqual([{ key: 'gpt-5', label: 'gpt-5', tokens: 20, requests: 1, share: 100 }]);
    expect(response.current_usage.api_keys).toEqual([]);
    expect(response.current_usage.auth_files).toEqual([]);
    expect(response.current_usage.ai_providers).toEqual([]);
  });

  it('derives realtime bucket seconds from the response window when omitted', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        window: '60m',
        token_velocity: [],
        response_level: [],
        current_usage: { models: [] },
        request_level: [],
        cache_level: [],
      }),
    } as Response);

    const response = await fetchUsageOverviewRealtime();

    expect(response.window).toBe('60m');
    expect(response.bucket_seconds).toBe(120);
  });

  it('posts logout to the auth endpoint', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
    } as Response);

    await logout();

    const [url, init] = fetchMock.mock.calls[0];
    expect(new URL(String(url), 'http://localhost').pathname).toBe('/api/v1/auth/logout');
    expect(init).toMatchObject({ credentials: 'include', method: 'POST' });
  });

  it('manages auth sessions through the dedicated admin endpoints', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ items: [] }),
    } as Response);
    const signal = new AbortController().signal;

    await fetchAuthSessions(signal);
    await revokeAuthSession('hash/with special');

    const listUrl = new URL(String(fetchMock.mock.calls[0][0]), 'http://localhost');
    expect(listUrl.pathname).toBe('/api/v1/auth/sessions');
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ credentials: 'include', signal, cache: 'no-store' });

    const revokeUrl = new URL(String(fetchMock.mock.calls[1][0]), 'http://localhost');
    expect(revokeUrl.pathname).toBe('/api/v1/auth/sessions/hash%2Fwith%20special');
    expect(fetchMock.mock.calls[1][1]).toMatchObject({ credentials: 'include', method: 'DELETE' });
  });

  it('loads quota auto refresh settings from the typed quota endpoint', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ enabled: true, schedule: { unit: 'hour', value: 6 } }),
    } as Response);
    const signal = new AbortController().signal;

    const response = await fetchQuotaAutoRefreshSettings(signal);

    const [url, init] = fetchMock.mock.calls[0];
    expect(response.schedule).toEqual({ unit: 'hour', value: 6 });
    expect(new URL(String(url), 'http://localhost').pathname).toBe('/api/v1/quota/auto-refresh/settings');
    expect(init).toMatchObject({ credentials: 'include', signal, cache: 'no-store' });
  });

  it('updates quota auto refresh settings through the typed quota endpoint', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ enabled: true, schedule: { unit: 'week', value: 2 } }),
    } as Response);

    const response = await updateQuotaAutoRefreshSettings({ enabled: true, schedule: { unit: 'week', value: 2 } });

    const [url, init] = fetchMock.mock.calls[0];
    expect(response.schedule).toEqual({ unit: 'week', value: 2 });
    expect(new URL(String(url), 'http://localhost').pathname).toBe('/api/v1/quota/auto-refresh/settings');
    expect(init).toMatchObject({ credentials: 'include', method: 'PUT' });
    expect(headerValue(init, 'Content-Type')).toBe('application/json');
    expect(init?.body).toBe(JSON.stringify({ enabled: true, schedule: { unit: 'week', value: 2 } }));
  });

  it('loads app version from the dedicated version endpoint', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ version: 'v1.2.3', updateCheckEnabled: true }),
    } as Response);
    const signal = new AbortController().signal;

    const response = await fetchVersion(signal);

    const [url, init] = fetchMock.mock.calls[0];
    expect(response.version).toBe('v1.2.3');
    expect(response.updateCheckEnabled).toBe(true);
    expect(new URL(String(url), 'http://localhost').pathname).toBe('/api/v1/version');
    expect(init).toMatchObject({ credentials: 'include', signal, cache: 'no-store' });
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

  it('exports usage events with filters but without pagination params', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const blob = new Blob(['id,timestamp\n']);
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      headers: new Headers({ 'Content-Disposition': 'attachment; filename="usage-events-20260627-013245.csv"' }),
      blob: async () => blob,
    } as Response);

    const file = await exportUsageEvents('custom', '2026-04-20T00:00:00Z', '2026-04-21T00:00:00Z', 'csv', {
      page: 3,
      pageSize: 100,
      model: 'claude-sonnet',
      source: 'authidx-source-a',
      result: 'failed',
      apiKeyId: '42',
    });

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(file.blob).toBe(blob);
    expect(file.filename).toBe('usage-events-20260627-013245.csv');
    expect(parsed.pathname).toBe('/api/v1/usage/events/export');
    expect(parsed.searchParams.get('range')).toBe('custom');
    expect(parsed.searchParams.get('start')).toBe('2026-04-20T00:00:00Z');
    expect(parsed.searchParams.get('end')).toBe('2026-04-21T00:00:00Z');
    expect(parsed.searchParams.get('format')).toBe('csv');
    expect(parsed.searchParams.get('model')).toBe('claude-sonnet');
    expect(parsed.searchParams.get('source')).toBe('authidx-source-a');
    expect(parsed.searchParams.get('result')).toBe('failed');
    expect(parsed.searchParams.get('api_key_id')).toBe('42');
    expect(parsed.searchParams.get('page')).toBeNull();
    expect(parsed.searchParams.get('page_size')).toBeNull();
    expect(parsed.searchParams.get('auth_index')).toBeNull();
    expect(parsed.searchParams.get('visibleColumnIds')).toBeNull();
    expect(init).toMatchObject({ credentials: 'include' });
  });

  it('passes API key id to overview and events requests', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ usage: { total_requests: 0, success_count: 0, failure_count: 0, total_tokens: 0 }, events: [], total_count: 0, page: 1, page_size: 100, total_pages: 0 }),
    } as Response);
    const signal = new AbortController().signal;

    await fetchUsageOverview('24h', undefined, undefined, signal, '9007199254740993');
    await fetchUsageEvents('24h', undefined, undefined, signal, { apiKeyId: '9007199254740993' });

    const overviewUrl = new URL(String(fetchMock.mock.calls[0][0]), 'http://localhost');
    const eventsUrl = new URL(String(fetchMock.mock.calls[1][0]), 'http://localhost');

    expect(overviewUrl.pathname).toBe('/api/v1/usage/overview');
    expect(eventsUrl.pathname).toBe('/api/v1/usage/events');
    expect(overviewUrl.searchParams.get('api_key_id')).toBe('9007199254740993');
    expect(eventsUrl.searchParams.get('api_key_id')).toBe('9007199254740993');
  });

  it('omits empty API key id from usage requests', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ usage: { total_requests: 0, success_count: 0, failure_count: 0, total_tokens: 0 }, events: [], total_count: 0, page: 1, page_size: 100, total_pages: 0 }),
    } as Response);
    const signal = new AbortController().signal;

    await fetchUsageOverview('24h', undefined, undefined, signal, '  ');
    await fetchUsageEvents('24h', undefined, undefined, signal, { apiKeyId: '' });

    for (const call of fetchMock.mock.calls) {
      expect(new URL(String(call[0]), 'http://localhost').searchParams.get('api_key_id')).toBeNull();
    }
  });

  it('loads Analysis from the dedicated endpoint with API key filtering', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ granularity: 'hourly', timezone: 'UTC', token_usage: [], api_key_composition: [], model_composition: [], heatmap: { api_keys: [], models: [], cells: [] } }),
    } as Response);
    const signal = new AbortController().signal;

    await fetchAnalysis('custom', '2026-04-20', '2026-04-21', signal, '9007199254740993');

    const analysisUrl = new URL(String(fetchMock.mock.calls[0][0]), 'http://localhost');

    expect(analysisUrl.pathname).toBe('/api/v1/usage/analysis');
    expect(analysisUrl.searchParams.get('range')).toBe('custom');
    expect(analysisUrl.searchParams.get('start')).toBe('2026-04-20');
    expect(analysisUrl.searchParams.get('end')).toBe('2026-04-21');
    expect(analysisUrl.searchParams.get('api_key_id')).toBe('9007199254740993');
  });

  it('loads a usage event request log by event id', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ event_id: '42', request_id: 'req-log-42', available: true, sections: [] }),
    } as Response);
    const signal = new AbortController().signal;

    const response = await fetchUsageEventRequestLog('42', signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(parsed.pathname).toBe('/api/v1/usage/events/42/request-log');
    expect(init).toMatchObject({ credentials: 'include', signal, cache: 'no-store' });
    expect(response.request_id).toBe('req-log-42');
  });

  it('creates a usage event request log download URL without fetching the file into memory', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ download_url: '/api/v1/usage/events/42/request-log/download-file?token=abc' }),
    } as Response);

    const url = await createUsageEventRequestLogDownloadURL('42');
    const parsed = new URL(url, 'http://localhost');
    const [requestURL, init] = fetchMock.mock.calls[0];

    expect(new URL(String(requestURL), 'http://localhost').pathname).toBe('/api/v1/usage/events/42/request-log/download-token');
    expect(init).toMatchObject({ method: 'POST', credentials: 'include', cache: 'no-store' });
    expect(parsed.pathname).toBe('/api/v1/usage/events/42/request-log/download-file');
    expect(parsed.searchParams.get('token')).toBe('abc');
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('passes credential page filters and sorting as query params', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ identities: [], total_count: 0, page: 1, page_size: 10, total_pages: 0 }),
    } as Response);
    const signal = new AbortController().signal;

    await fetchUsageIdentitiesPage(signal, {
      authType: 1,
      page: 2,
      pageSize: 20,
      activeOnly: true,
      sort: 'last_used_at',
      types: ['claude', ' openai '],
    });

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(parsed.pathname).toBe('/api/v1/usage/identities/page');
    expect(parsed.searchParams.get('auth_type')).toBe('1');
    expect(parsed.searchParams.get('page')).toBe('2');
    expect(parsed.searchParams.get('page_size')).toBe('20');
    expect(parsed.searchParams.get('active_only')).toBe('true');
    expect(parsed.searchParams.get('sort')).toBe('last_used_at');
    expect(parsed.searchParams.getAll('type')).toEqual(['claude', ' openai ']);
    expect(init).toMatchObject({ credentials: 'include', signal });
  });

  it('loads unified usage identities without query params', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        identities: [
          {
            id: '1',
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
            last_aggregated_usage_event_id: '9',
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

  it('loads CPA API key settings without exposing numeric ids', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ items: [{ id: '9007199254740993', keyAlias: '', displayKey: 'sk-*********123456', label: 'sk-*********123456', lastSyncedAt: null }] }),
    } as Response);
    const signal = new AbortController().signal;

    const response = await fetchCpaApiKeys(signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.items[0].id).toBe('9007199254740993');
    expect(typeof response.items[0].id).toBe('string');
    expect(parsed.pathname).toBe('/api/v1/usage/api-keys');
    expect(init).toMatchObject({ credentials: 'include', signal, cache: 'no-store' });
  });

  it('loads CPA API key settings from the admin-only raw key endpoint', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ items: [{ id: '9007199254740993', apiKey: 'sk-alpha123456', keyAlias: '', displayKey: 'sk-*********123456', label: 'sk-*********123456', lastSyncedAt: null }] }),
    } as Response);
    const signal = new AbortController().signal;

    const response = await fetchCpaApiKeySettings(signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.items[0].apiKey).toBe('sk-alpha123456');
    expect(response.items[0].id).toBe('9007199254740993');
    expect(parsed.pathname).toBe('/api/v1/usage/api-keys/settings');
    expect(init).toMatchObject({ credentials: 'include', signal, cache: 'no-store' });
  });

  it('loads CPA API key options and updates aliases', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ options: [{ id: '123', keyAlias: 'Main', displayKey: 'sk-*********123456', label: 'Main', lastSyncedAt: '2026-05-13T00:00:00Z' }] }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ id: '123', keyAlias: '', displayKey: 'sk-*********123456', label: 'sk-*********123456', lastSyncedAt: '2026-05-13T00:00:00Z' }),
      } as Response);
    const signal = new AbortController().signal;

    const options = await fetchCpaApiKeyOptions(signal);
    const updated = await updateCpaApiKeyAlias('123', '');

    const [optionsUrl, optionsInit] = fetchMock.mock.calls[0];
    const [updateUrl, updateInit] = fetchMock.mock.calls[1];

    expect(options.options[0].id).toBe('123');
    expect(new URL(String(optionsUrl), 'http://localhost').pathname).toBe('/api/v1/usage/api-keys/options');
    expect(optionsInit).toMatchObject({ credentials: 'include', signal, cache: 'no-store' });
    expect(updated.label).toBe('sk-*********123456');
    expect(new URL(String(updateUrl), 'http://localhost').pathname).toBe('/api/v1/usage/api-keys/123');
    expect(updateInit).toMatchObject({ credentials: 'include', method: 'PATCH' });
    expect(updateInit?.body).toBe(JSON.stringify({ keyAlias: '' }));
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

  it('loads cached quota for current page auth indexes without refreshing', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        items: [{ auth_index: 'auth-1', file_name: 'claude-user.json', status: 'completed', quota: { id: 'auth-1', quota: [{ key: 'rate_limit.secondary_window', label: 'Weekly', remaining: 12 }] }, refreshed_at: '2026-05-25T00:00:00Z' }],
      }),
    } as Response);
    const signal = new AbortController().signal;

    const response = await fetchUsageQuotaCache(['auth-1'], signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.items[0].auth_index).toBe('auth-1');
    expect(response.items[0].file_name).toBe('claude-user.json');
    expect(response.items[0].refreshed_at).toBe('2026-05-25T00:00:00Z');
    expect(response.items[0].quota?.quota[0].remaining).toBe(12);
    expect(parsed.pathname).toBe('/api/v1/quota/cache');
    expect(init).toMatchObject({ credentials: 'include', method: 'POST', signal });
    expect(headerValue(init, 'Content-Type')).toBe('application/json');
    expect(init?.body).toBe(JSON.stringify({ auth_indexes: ['auth-1'] }));
  });

  it('creates quota refresh tasks for current page auth indexes', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        tasks: [{ authIndex: 'auth-1' }],
        rejected: [],
        accepted: 1,
        skipped: 0,
        limit: 1,
      }),
    } as Response);
    const signal = new AbortController().signal;

    const response = await refreshUsageQuotas(['auth-1'], signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.tasks[0]).toEqual({ authIndex: 'auth-1' });
    expect(response.limit).toBe(1);
    expect(parsed.pathname).toBe('/api/v1/quota/refresh');
    expect(init).toMatchObject({ credentials: 'include', method: 'POST', signal });
    expect(headerValue(init, 'Content-Type')).toBe('application/json');
    expect(init?.body).toBe(JSON.stringify({ auth_indexes: ['auth-1'] }));
  });

  it('uses the reset error code returned by the backend', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: false,
      status: 502,
      json: async () => ({
        error: 'quota_reset_failed',
        detail: 'HTTP 401: invalid codex token',
      }),
    } as Response);

    await expect(resetUsageQuota('auth-1')).rejects.toMatchObject({
      name: 'ApiError',
      status: 502,
      message: 'quota_reset_failed',
    });

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');
    expect(parsed.pathname).toBe('/api/v1/quota/reset');
    expect(init).toMatchObject({ credentials: 'include', method: 'POST' });
    expect(headerValue(init, 'Content-Type')).toBe('application/json');
    expect(init?.body).toBe(JSON.stringify({ auth_index: 'auth-1' }));
  });

  it('loads quota inspection status', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        total: 2,
        cached: 1,
        running: true,
        completed: false,
        normal: 1,
        limit_reached: 0,
        unauthorized_401: 0,
        payment_required_402: 0,
        unauthorized_401_402: 0,
        other_failed: 0,
        unknown: 1,
        results: [{ auth_index: 'auth-1', name: 'Claude Main', type: 'claude', file_name: 'claude-user.json', provider: 'claude', status: 'normal', refreshed_at: '2026-06-03T10:30:00Z' }],
      }),
    } as Response);
    const signal = new AbortController().signal;

    const response = await fetchUsageQuotaInspectionStatus(signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.total).toBe(2);
    expect(response.cached).toBe(1);
    expect(response.results[0].auth_index).toBe('auth-1');
    expect(response.results[0].file_name).toBe('claude-user.json');
    expect(parsed.pathname).toBe('/api/v1/quota/inspection');
    expect(init).toMatchObject({ credentials: 'include', signal });
  });

  it('starts quota inspection from the protected endpoint', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        total: 2,
        cached: 0,
        running: true,
        completed: false,
        normal: 0,
        limit_reached: 0,
        unauthorized_401: 0,
        payment_required_402: 0,
        unauthorized_401_402: 0,
        other_failed: 0,
        unknown: 2,
        results: [],
      }),
    } as Response);
    const signal = new AbortController().signal;

    const response = await startUsageQuotaInspection(signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.running).toBe(true);
    expect(parsed.pathname).toBe('/api/v1/quota/inspection');
    expect(init).toMatchObject({ credentials: 'include', method: 'POST', signal });
  });

  it('disables selected auth files through the protected management endpoint', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ names: ['a.json'], affected: 1 }),
    } as Response);

    const response = await setAuthFilesDisabled(['a.json'], true);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.affected).toBe(1);
    expect(parsed.pathname).toBe('/api/v1/auth-files/status');
    expect(init).toMatchObject({ credentials: 'include', method: 'PATCH' });
    expect(headerValue(init, 'Content-Type')).toBe('application/json');
    expect(init?.body).toBe(JSON.stringify({ names: ['a.json'], disabled: true }));
  });

  it('deletes selected auth files through the protected management endpoint', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ names: ['a.json', 'b.json'], affected: 2 }),
    } as Response);

    const response = await deleteAuthFiles(['a.json', 'b.json']);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.names).toEqual(['a.json', 'b.json']);
    expect(parsed.pathname).toBe('/api/v1/auth-files');
    expect(init).toMatchObject({ credentials: 'include', method: 'DELETE' });
    expect(headerValue(init, 'Content-Type')).toBe('application/json');
    expect(init?.body).toBe(JSON.stringify({ names: ['a.json', 'b.json'] }));
  });

  it('loads quota refresh task status', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({
        authIndex: 'auth-1',
        file_name: 'claude-user.json',
        status: 'completed',
        http_status_code: 401,
        refreshed_at: '2026-05-25T00:00:00Z',
        quota: { id: 'auth-1', quota: [{ key: 'rate_limit.primary_window', label: '5h' }] },
      }),
    } as Response);
    const signal = new AbortController().signal;

    const response = await fetchUsageQuotaRefreshTask('auth-1', signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.status).toBe('completed');
    expect(response.file_name).toBe('claude-user.json');
    expect(response.http_status_code).toBe(401);
    expect(response.refreshed_at).toBe('2026-05-25T00:00:00Z');
    expect(response.quota?.id).toBe('auth-1');
    expect(parsed.pathname).toBe('/api/v1/quota/refresh/auth-1');
    expect(init).toMatchObject({ credentials: 'include', signal });
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
