import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, appPath, isUsageRangeBoundsConflict, createUsageEventRequestLogDownloadURL, deleteAuthFiles, exportUsageEvents, fetchAnalysis, fetchAnalysisLatency, fetchAuthSessions, fetchCodexQuotaHistory, fetchCpaApiKeyOptions, fetchCpaApiKeys, fetchCpaApiKeySettings, fetchKeyActivity, fetchKeyAnalysis, fetchKeyAnalysisLatency, fetchKeyOverview, fetchKeyOverviewRealtime, fetchQuotaAutoRefreshSettings, fetchUsageActivity, fetchUsageOverview, fetchUsageOverviewRealtime, fetchUsageQuotaCache, fetchUsageQuotaInspectionStatus, fetchUsageQuotaResetCredits, fetchUpdateCheck, fetchUsageEventModelFilterOptions, fetchUsageEventRequestLog, fetchUsageEventSourceFilterOptions, fetchUsageEvents, fetchUsageIdentities, fetchUsageIdentitiesPage, fetchUsageQuotaRefreshTask, fetchVersion, loginWithCPAAPIKey, logout, refreshUsageQuotas, resetUsageQuota, revokeAuthSession, setAuthFilesDisabled, setCredentialDisabled, setCredentialPriority, startUsageQuotaInspection, updateAuthSessionAlias, updateCpaApiKeyAlias, updateQuotaAutoRefreshSettings } from '../api';

const headerValue = (init: RequestInit | undefined, name: string): string | null => new Headers(init?.headers).get(name);

function mockJSON(payload: unknown) {
  return vi.spyOn(globalThis, 'fetch').mockImplementation(async () => Response.json(payload));
}

describe('fetchUsageEvents', () => {
  beforeEach(() => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('builds app paths from the configured base path', () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper/' });

    expect(appPath('/key-overview')).toBe('/keeper/key-overview');
    expect(appPath('key-overview')).toBe('/keeper/key-overview');
  });

  it('identifies only HTTP 409 as a usage range bounds conflict', () => {
    expect(isUsageRangeBoundsConflict(new ApiError('expired range', 409))).toBe(true);
    expect(isUsageRangeBoundsConflict(new ApiError('invalid range', 400))).toBe(false);
    expect(isUsageRangeBoundsConflict(new Error('network error'))).toBe(false);
  });

  it('posts CPA API key logins to the dedicated auth endpoint', async () => {
    const fetchMock = mockJSON({});

    await loginWithCPAAPIKey('sk-cpa-viewer');

    const [url, init] = fetchMock.mock.calls[0];
    expect(new URL(String(url), 'http://localhost').pathname).toBe('/api/v1/auth/api-key-login');
    expect(init).toMatchObject({ credentials: 'include', method: 'POST' });
    expect(headerValue(init, 'Content-Type')).toBe('application/json');
    expect(init?.body).toBe(JSON.stringify({ apiKey: 'sk-cpa-viewer' }));
  });

  it('loads key overview with only the viewer range query', async () => {
    const fetchMock = mockJSON({ usage: { total_requests: 0, success_count: 0, failure_count: 0, total_tokens: 0 } });
    const signal = new AbortController().signal;

    await fetchKeyOverview({ range: '8h' }, signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');
    expect(parsed.pathname).toBe('/api/v1/key-overview');
    expect(parsed.searchParams.get('range')).toBe('8h');
    expect(parsed.searchParams.get('api_key_id')).toBeNull();
    expect(parsed.searchParams.get('start')).toBeNull();
    expect(parsed.searchParams.get('end')).toBeNull();
    expect(init).toMatchObject({ credentials: 'include', signal });
  });

  it('loads key analysis sections without accepting a client API key scope', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper' });
    const fetchMock = mockJSON({});
    const signal = new AbortController().signal;
    const request = { range: 'custom' as const, unit: 'day' as const, start: '2026-08-01', end: '2026-08-07' };

    await fetchKeyAnalysis(request, signal);
    await fetchKeyAnalysisLatency(request, signal);

    expect(fetchMock.mock.calls).toHaveLength(2);
    const [analysisURL, latencyURL] = fetchMock.mock.calls.map(([rawURL]) => new URL(String(rawURL), 'http://localhost'));
    expect(analysisURL.pathname).toBe('/keeper/api/v1/key-analysis');
    expect(latencyURL.pathname).toBe('/keeper/api/v1/key-analysis/latency');
    for (const url of [analysisURL, latencyURL]) {
      expect(url.searchParams.get('range')).toBe('custom');
      expect(url.searchParams.get('unit')).toBe('day');
      expect(url.searchParams.get('start')).toBe('2026-08-01');
      expect(url.searchParams.get('end')).toBe('2026-08-07');
      expect(url.searchParams.get('api_key_id')).toBeNull();
    }
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ credentials: 'include', signal });
    expect(fetchMock.mock.calls[1][1]).toMatchObject({ credentials: 'include', signal });
  });

  it('sends the displayed 1d range as today on every usage request surface', async () => {
    const fetchMock = mockJSON({});

    await fetchKeyOverview({ range: '1d' });
    await fetchUsageOverview({ range: '1d' });
    await fetchUsageEvents({ range: '1d' });
    await fetchAnalysis({ range: '1d' });

    expect(fetchMock.mock.calls).toHaveLength(4);
    for (const [url] of fetchMock.mock.calls) {
      expect(new URL(String(url), 'http://localhost').searchParams.get('range')).toBe('today');
    }
  });

  it('sends one custom range request shape to every usage surface', async () => {
    const fetchMock = mockJSON({});
    const request = {
      range: 'custom' as const,
      unit: 'day' as const,
      start: '2026-06-18',
      end: '2026-07-17',
    };

    await fetchKeyOverview(request);
    await fetchUsageOverview(request);
    await fetchUsageEvents(request);
    await exportUsageEvents(request, 'csv');
    await fetchAnalysis(request);

    expect(fetchMock.mock.calls).toHaveLength(5);
    for (const [rawURL] of fetchMock.mock.calls) {
      const params = new URL(String(rawURL), 'http://localhost').searchParams;
      expect(params.get('range')).toBe('custom');
      expect(params.get('unit')).toBe('day');
      expect(params.get('start')).toBe('2026-06-18');
      expect(params.get('end')).toBe('2026-07-17');
    }
  });

  it('preserves the realtime insight block for both admin and Key Viewer responses', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined });
    const insights = { summary: { requests: 12, failures: 2, cost: null }, outcomes: [{ bucket: '2026-09-12T12:00:00+08:00', requests: 12, failures: 2 }] };
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, json: async () => ({ insights }) } as Response);
    expect((await fetchUsageOverviewRealtime()).insights).toEqual(insights);
    expect((await fetchKeyOverviewRealtime()).insights).toEqual(insights);
  });

  it('loads realtime overview from dedicated endpoints', async () => {
    const fetchMock = mockJSON({ usage: { total_requests: 0, success_count: 0, failure_count: 0, total_tokens: 0 } });
    const signal = new AbortController().signal;

    await fetchUsageOverview({ range: '24h' }, signal, '9007199254740993');
    await fetchUsageOverviewRealtime({ signal, apiKeyId: '9007199254740993', window: '60m' });
    await fetchKeyOverview({ range: '8h' }, signal);
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

  it('loads Recent Activity with the same time query contract as Overview', async () => {
    const fetchMock = mockJSON({ window: 'week', grain: 'medium', rows: 7, columns: 52, blocks: [] });
    const signal = new AbortController().signal;

    await fetchUsageActivity({
      request: { range: 'custom', unit: 'day', start: '2026-07-15', end: '2026-07-21' },
      apiKeyId: '9007199254740993',
      signal,
    });
    await fetchKeyActivity({ request: { range: '30d' }, signal });

    const usageUrl = new URL(String(fetchMock.mock.calls[0][0]), 'http://localhost');
    const keyUrl = new URL(String(fetchMock.mock.calls[1][0]), 'http://localhost');
    expect(usageUrl.pathname).toBe('/api/v1/usage/activity');
    expect(usageUrl.searchParams.get('window')).toBeNull();
    expect(usageUrl.searchParams.get('api_key_id')).toBe('9007199254740993');
    expect(usageUrl.searchParams.get('range')).toBe('custom');
    expect(usageUrl.searchParams.get('unit')).toBe('day');
    expect(usageUrl.searchParams.get('start')).toBe('2026-07-15');
    expect(usageUrl.searchParams.get('end')).toBe('2026-07-21');
    expect(keyUrl.pathname).toBe('/api/v1/key-activity');
    expect(keyUrl.searchParams.get('window')).toBeNull();
    expect(keyUrl.searchParams.get('range')).toBe('30d');
    expect(keyUrl.searchParams.get('api_key_id')).toBeNull();
  });

  it('loads one-year Recent Activity through its dedicated window parameter', async () => {
    const fetchMock = mockJSON({ window: 'year', grain: 'daily', rows: 7, columns: 52, blocks: [] });
    const signal = new AbortController().signal;

    await fetchUsageActivity({ request: { window: 'year' }, apiKeyId: '42', signal });
    await fetchKeyActivity({ request: { window: 'year' }, signal });

    const usageUrl = new URL(String(fetchMock.mock.calls[0][0]), 'http://localhost');
    const keyUrl = new URL(String(fetchMock.mock.calls[1][0]), 'http://localhost');
    for (const url of [usageUrl, keyUrl]) {
      expect(url.searchParams.get('window')).toBe('year');
      expect(url.searchParams.get('range')).toBeNull();
    }
    expect(usageUrl.searchParams.get('api_key_id')).toBe('42');
    expect(keyUrl.searchParams.get('api_key_id')).toBeNull();
  });

  it('loads calendar-day Recent Activity through dedicated window parameters', async () => {
    const fetchMock = mockJSON({ window: 'day', grain: 'short', rows: 7, columns: 52, blocks: [] });
    const signal = new AbortController().signal;

    await fetchUsageActivity({ request: { window: 'today' }, apiKeyId: '42', signal });
    await fetchKeyActivity({ request: { window: 'yesterday' }, signal });

    const usageUrl = new URL(String(fetchMock.mock.calls[0][0]), 'http://localhost');
    const keyUrl = new URL(String(fetchMock.mock.calls[1][0]), 'http://localhost');
    expect(usageUrl.searchParams.get('window')).toBe('today');
    expect(keyUrl.searchParams.get('window')).toBe('yesterday');
    for (const url of [usageUrl, keyUrl]) {
      expect(url.searchParams.get('range')).toBeNull();
    }
    expect(usageUrl.searchParams.get('api_key_id')).toBe('42');
    expect(keyUrl.searchParams.get('api_key_id')).toBeNull();
  });

  it('normalizes key overview realtime responses that omit internal usage dimensions', async () => {
    const fetchMock = mockJSON({
      window: '30m',
      bucket_seconds: 60,
      token_velocity: [],
      latency_scatter: {
        points: [{ ttft_ms: 120, latency_ms: 800 }],
        total_points: 1, p95_ttft_ms: 120, p95_latency_ms: 800,
        max_ttft_ms: 120, max_latency_ms: 800,
      },
      current_usage: { models: [{ key: 'gpt-5', label: 'gpt-5', tokens: 20, requests: 1, share: 100 }] },
      request_level: [],
      cache_level: [],
    });
    const signal = new AbortController().signal;

    const response = await fetchKeyOverviewRealtime({ window: '30m', signal });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(response.current_usage.models).toEqual([{ key: 'gpt-5', label: 'gpt-5', tokens: 20, requests: 1, share: 100 }]);
    expect(response.current_usage.api_keys).toEqual([]);
    expect(response.current_usage.auth_files).toEqual([]);
    expect(response.current_usage.ai_providers).toEqual([]);
    expect(response.latency_scatter).toEqual({
      points: [{ ttft_ms: 120, latency_ms: 800 }],
      total_points: 1, p95_ttft_ms: 120, p95_latency_ms: 800,
      max_ttft_ms: 120, max_latency_ms: 800,
    });
  });

  it('derives realtime bucket seconds from the response window when omitted', async () => {
    mockJSON({
      window: '60m',
      token_velocity: [],
      current_usage: { models: [] },
      request_level: [],
      cache_level: [],
    });

    const response = await fetchUsageOverviewRealtime();

    expect(response.window).toBe('60m');
    expect(response.bucket_seconds).toBe(120);
  });

  it('posts logout to the auth endpoint', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response());

    await logout();

    const [url, init] = fetchMock.mock.calls[0];
    expect(new URL(String(url), 'http://localhost').pathname).toBe('/api/v1/auth/logout');
    expect(init).toMatchObject({ credentials: 'include', method: 'POST' });
  });

  it('manages auth sessions through the dedicated admin endpoints', async () => {
    const fetchMock = mockJSON({ items: [] });
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

  it('updates an admin session alias through the dedicated session endpoint', async () => {
    const fetchMock = mockJSON({ id: 'session/hash', kind: 'admin', role: 'admin', source: 'standard', alias: 'Office Mac' });

    const updated = await updateAuthSessionAlias('session/hash', 'Office Mac');

    const [url, init] = fetchMock.mock.calls[0];
    expect(updated.alias).toBe('Office Mac');
    expect(new URL(String(url), 'http://localhost').pathname).toBe('/api/v1/auth/sessions/session%2Fhash');
    expect(init).toMatchObject({ credentials: 'include', method: 'PATCH' });
    expect(headerValue(init, 'Content-Type')).toBe('application/json');
    expect(init?.body).toBe(JSON.stringify({ alias: 'Office Mac' }));
  });

  it('loads quota auto refresh settings from the typed quota endpoint', async () => {
    const fetchMock = mockJSON({ enabled: true, schedule: { unit: 'hour', value: 6 } });
    const signal = new AbortController().signal;

    const response = await fetchQuotaAutoRefreshSettings(signal);

    const [url, init] = fetchMock.mock.calls[0];
    expect(response.schedule).toEqual({ unit: 'hour', value: 6 });
    expect(new URL(String(url), 'http://localhost').pathname).toBe('/api/v1/quota/auto-refresh/settings');
    expect(init).toMatchObject({ credentials: 'include', signal, cache: 'no-store' });
  });

  it('updates quota auto refresh settings through the typed quota endpoint', async () => {
    const fetchMock = mockJSON({ enabled: true, schedule: { unit: 'week', value: 2 } });

    const response = await updateQuotaAutoRefreshSettings({ enabled: true, schedule: { unit: 'week', value: 2 } });

    const [url, init] = fetchMock.mock.calls[0];
    expect(response.schedule).toEqual({ unit: 'week', value: 2 });
    expect(new URL(String(url), 'http://localhost').pathname).toBe('/api/v1/quota/auto-refresh/settings');
    expect(init).toMatchObject({ credentials: 'include', method: 'PUT' });
    expect(headerValue(init, 'Content-Type')).toBe('application/json');
    expect(init?.body).toBe(JSON.stringify({ enabled: true, schedule: { unit: 'week', value: 2 } }));
  });

  it('loads app version from the dedicated version endpoint', async () => {
    const fetchMock = mockJSON({ version: 'v1.2.3', updateCheckEnabled: true });
    const signal = new AbortController().signal;

    const response = await fetchVersion(signal);

    const [url, init] = fetchMock.mock.calls[0];
    expect(response.version).toBe('v1.2.3');
    expect(response.updateCheckEnabled).toBe(true);
    expect(new URL(String(url), 'http://localhost').pathname).toBe('/api/v1/version');
    expect(init).toMatchObject({ credentials: 'include', signal, cache: 'no-store' });
  });

  it('loads model filter options without query params', async () => {
    const fetchMock = mockJSON({ models: ['claude-sonnet'] });
    const signal = new AbortController().signal;

    const response = await fetchUsageEventModelFilterOptions(signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.models).toEqual(['claude-sonnet']);
    expect(parsed.pathname).toBe('/api/v1/usage/events/filters/models');
    expect(parsed.search).toBe('');
    expect(init).toMatchObject({ credentials: 'include', signal, cache: 'no-store' });
  });

  it('loads source filter options without query params', async () => {
    const fetchMock = mockJSON({ sources: [{ value: 'source-a', label: 'Provider A' }] });
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
    const fetchMock = mockJSON({ events: [], models: [], sources: [], total_count: 0, page: 3, page_size: 100, total_pages: 0 });
    const signal = new AbortController().signal;

    await fetchUsageEvents({
      range: 'custom',
      unit: 'hour',
      start: '2026-04-20T00:00:00Z',
      end: '2026-04-21T00:00:00Z',
    }, signal, {
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

  it('passes cursor pagination metadata for incremental event loading', async () => {
    const fetchMock = mockJSON({ events: [], total_count: -1, page: 1, page_size: 100, total_pages: 0, has_more: false });

    await fetchUsageEvents({ range: '24h' }, undefined, {
      pageSize: 100,
      cursorMode: true,
      cursor: 'opaque-cursor',
      apiKeyId: '42',
    });

    const parsed = new URL(String(fetchMock.mock.calls[0][0]), 'http://localhost');
    expect(parsed.searchParams.get('page_size')).toBe('100');
    expect(parsed.searchParams.get('cursor_mode')).toBe('true');
    expect(parsed.searchParams.get('cursor')).toBe('opaque-cursor');
    expect(parsed.searchParams.get('api_key_id')).toBe('42');
    expect(parsed.searchParams.get('page')).toBeNull();
  });

  it('loads latest credential events without a fixed range and includes the identity type', async () => {
    const fetchMock = mockJSON({ events: [], total_count: 0, page: 1, page_size: 50, total_pages: 0, has_more: false });

    await fetchUsageEvents(undefined, undefined, {
      pageSize: 50,
      cursorMode: true,
      source: 'shared-auth',
      authType: 2,
    });

    const parsed = new URL(String(fetchMock.mock.calls[0][0]), 'http://localhost');
    expect(parsed.searchParams.get('range')).toBeNull();
    expect(parsed.searchParams.get('source')).toBe('shared-auth');
    expect(parsed.searchParams.get('auth_type')).toBe('2');
    expect(parsed.searchParams.get('cursor_mode')).toBe('true');
  });

  it.each(['csv', 'json'] as const)('exports usage events as %s with filters but without pagination params', async (format) => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('id,timestamp\n', {
      headers: new Headers({ 'Content-Disposition': 'attachment; filename="usage-events-20260627-013245.csv"' }),
    }));

    const file = await exportUsageEvents({
      range: 'custom',
      unit: 'hour',
      start: '2026-04-20T00:00:00Z',
      end: '2026-04-21T00:00:00Z',
    }, format, {
      page: 3,
      pageSize: 100,
      model: 'claude-sonnet',
      source: 'authidx-source-a',
      result: 'failed',
      apiKeyId: '42',
    });

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(await file.blob.text()).toBe('id,timestamp\n');
    expect(file.filename).toBe('usage-events-20260627-013245.csv');
    expect(parsed.pathname).toBe('/api/v1/usage/events/export');
    expect(parsed.searchParams.get('range')).toBe('custom');
    expect(parsed.searchParams.get('start')).toBe('2026-04-20T00:00:00Z');
    expect(parsed.searchParams.get('end')).toBe('2026-04-21T00:00:00Z');
    expect(parsed.searchParams.get('format')).toBe(format);
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
    const fetchMock = mockJSON({ usage: { total_requests: 0, success_count: 0, failure_count: 0, total_tokens: 0 }, events: [], total_count: 0, page: 1, page_size: 100, total_pages: 0 });
    const signal = new AbortController().signal;

    await fetchUsageOverview({ range: '24h' }, signal, '9007199254740993');
    await fetchUsageEvents({ range: '24h' }, signal, { apiKeyId: '9007199254740993' });

    const overviewUrl = new URL(String(fetchMock.mock.calls[0][0]), 'http://localhost');
    const eventsUrl = new URL(String(fetchMock.mock.calls[1][0]), 'http://localhost');

    expect(overviewUrl.pathname).toBe('/api/v1/usage/overview');
    expect(eventsUrl.pathname).toBe('/api/v1/usage/events');
    expect(overviewUrl.searchParams.get('api_key_id')).toBe('9007199254740993');
    expect(eventsUrl.searchParams.get('api_key_id')).toBe('9007199254740993');
  });

  it('omits empty API key id from usage requests', async () => {
    const fetchMock = mockJSON({ usage: { total_requests: 0, success_count: 0, failure_count: 0, total_tokens: 0 }, events: [], total_count: 0, page: 1, page_size: 100, total_pages: 0 });
    const signal = new AbortController().signal;

    await fetchUsageOverview({ range: '24h' }, signal, '  ');
    await fetchUsageEvents({ range: '24h' }, signal, { apiKeyId: '' });

    for (const call of fetchMock.mock.calls) {
      expect(new URL(String(call[0]), 'http://localhost').searchParams.get('api_key_id')).toBeNull();
    }
  });

  it('loads Analysis from the dedicated endpoint with API key filtering', async () => {
    const fetchMock = mockJSON({ granularity: 'hourly', timezone: 'UTC', token_usage: [], api_key_composition: [], model_composition: [], heatmap: { api_keys: [], models: [], cells: [] } });
    const signal = new AbortController().signal;

    await fetchAnalysis({ range: 'custom', unit: 'day', start: '2026-04-20', end: '2026-04-21' }, signal, '9007199254740993');

    const analysisUrl = new URL(String(fetchMock.mock.calls[0][0]), 'http://localhost');

    expect(analysisUrl.pathname).toBe('/api/v1/usage/analysis');
    expect(analysisUrl.searchParams.get('range')).toBe('custom');
    expect(analysisUrl.searchParams.get('start')).toBe('2026-04-20');
    expect(analysisUrl.searchParams.get('end')).toBe('2026-04-21');
    expect(analysisUrl.searchParams.get('api_key_id')).toBe('9007199254740993');
    expect(Array.from(analysisUrl.searchParams.keys())).toEqual(['range', 'unit', 'start', 'end', 'api_key_id']);
  });

  it('loads Analysis latency from its independent endpoint with the same filters', async () => {
    const fetchMock = mockJSON({ points: [], density: [], total_points: 0 });
    const signal = new AbortController().signal;

    await fetchAnalysisLatency({ range: 'custom', unit: 'day', start: '2026-04-20', end: '2026-04-21' }, signal, '9007199254740993');

    const latencyUrl = new URL(String(fetchMock.mock.calls[0][0]), 'http://localhost');
    expect(latencyUrl.pathname).toBe('/api/v1/usage/analysis/latency');
    expect(latencyUrl.searchParams.get('range')).toBe('custom');
    expect(latencyUrl.searchParams.get('start')).toBe('2026-04-20');
    expect(latencyUrl.searchParams.get('end')).toBe('2026-04-21');
    expect(latencyUrl.searchParams.get('api_key_id')).toBe('9007199254740993');
    expect(Array.from(latencyUrl.searchParams.keys())).toEqual(['range', 'unit', 'start', 'end', 'api_key_id']);
  });

  it('loads a usage event request log by event id', async () => {
    const fetchMock = mockJSON({ event_id: '42', request_id: 'req-log-42', available: true, sections: [] });
    const signal = new AbortController().signal;

    const response = await fetchUsageEventRequestLog('42', signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(parsed.pathname).toBe('/api/v1/usage/events/42/request-log');
    expect(init).toMatchObject({ credentials: 'include', signal, cache: 'no-store' });
    expect(response.request_id).toBe('req-log-42');
  });

  it('creates a usage event request log download URL without fetching the file into memory', async () => {
    const fetchMock = mockJSON({ download_url: '/api/v1/usage/events/42/request-log/download-file?token=abc' });

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
    const fetchMock = mockJSON({ identities: [], total_count: 0, page: 1, page_size: 10, total_pages: 0 });
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
    const fetchMock = mockJSON({
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
          cache_read_tokens: 0,
          total_tokens: 30,
          last_aggregated_usage_event_id: '9',
          is_deleted: false,
          created_at: '2026-05-04T00:00:00Z',
          updated_at: '2026-05-04T00:00:00Z',
        },
      ],
    });
    const signal = new AbortController().signal;

    const response = await fetchUsageIdentities(signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.identities[0].identity).toBe('sk-a***1234');
    expect(response.identities[0].auth_type).toBe(2);
    expect(parsed.pathname).toBe('/api/v1/usage/identities');
    expect(parsed.search).toBe('');
    expect(init).toMatchObject({ credentials: 'include', signal });
  });

  it('loads CPA API key settings without exposing numeric ids', async () => {
    const fetchMock = mockJSON({ items: [{ id: '9007199254740993', keyAlias: '', displayKey: 'sk-*********123456', label: 'sk-*********123456', lastSyncedAt: null }] });
    const signal = new AbortController().signal;

    const response = await fetchCpaApiKeys(signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.items[0].id).toBe('9007199254740993');
    expect(parsed.pathname).toBe('/api/v1/usage/api-keys');
    expect(init).toMatchObject({ credentials: 'include', signal, cache: 'no-store' });
  });

  it('loads CPA API key settings from the admin-only raw key endpoint', async () => {
    const fetchMock = mockJSON({ items: [{ id: '9007199254740993', apiKey: 'sk-alpha123456', keyAlias: '', displayKey: 'sk-*********123456', label: 'sk-*********123456', lastSyncedAt: null }] });
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
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(Response.json({ options: [{ id: '123', keyAlias: 'Main', displayKey: 'sk-*********123456', label: 'Main', lastSyncedAt: '2026-05-13T00:00:00Z' }] }))
      .mockResolvedValueOnce(Response.json({ id: '123', keyAlias: '', displayKey: 'sk-*********123456', label: 'sk-*********123456', lastSyncedAt: '2026-05-13T00:00:00Z' }));
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
    const fetchMock = mockJSON({ identities: [], total_count: 25, page: 3, page_size: 10, total_pages: 3 });
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
    const fetchMock = mockJSON({
      items: [{ auth_index: 'auth-1', file_name: 'claude-user.json', status: 'completed', quota: { id: 'auth-1', quota: [{ key: 'rate_limit.secondary_window', label: 'Weekly', remaining: 12 }] }, refreshed_at: '2026-05-25T00:00:00Z' }],
    });
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

  it('loads one encoded Codex quota history role', async () => {
    const fetchMock = mockJSON({ generated_at: '2026-08-21T12:00:00Z', range_start: '2026-07-22T12:00:00Z', windows: [], selected_window: null, cycles: [] });
    const signal = new AbortController().signal;

    await fetchCodexQuotaHistory('codex/auth + user', { windowRole: 'primary' }, signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');
    expect(parsed.pathname).toBe('/api/v1/quota/history/codex%2Fauth%20%2B%20user');
    expect(parsed.searchParams.get('window_role')).toBe('primary');
    expect(parsed.searchParams.has('window_seconds')).toBe(false);
    expect(init).toMatchObject({ credentials: 'include', signal });
  });

  it('creates quota refresh tasks for current page auth indexes', async () => {
    const fetchMock = mockJSON({
      tasks: [{ authIndex: 'auth-1' }],
      rejected: [],
      accepted: 1,
      skipped: 0,
      limit: 1,
    });
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
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(Response.json({
      error: 'quota_reset_failed',
      detail: 'HTTP 401: invalid codex token',
    }, { status: 502 }));

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

  it('loads reset credit expiries for one auth index on demand', async () => {
    const fetchMock = mockJSON({
      authIndex: 'codex-auth',
      availableCount: 1,
      credits: [{ id: 'credit-1', status: 'available', expiresAt: '2026-07-20T00:00:00Z' }],
    });
    const signal = new AbortController().signal;

    const response = await fetchUsageQuotaResetCredits('codex-auth', signal);

    const [url, init] = fetchMock.mock.calls[0];
    expect(new URL(String(url), 'http://localhost').pathname).toBe('/api/v1/quota/reset-credits/codex-auth');
    expect(init).toMatchObject({ credentials: 'include', signal });
    expect(response.availableCount).toBe(1);
    expect(response.credits[0].expiresAt).toBe('2026-07-20T00:00:00Z');
  });

  it('loads quota inspection status', async () => {
    const fetchMock = mockJSON({
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
    });
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
    const fetchMock = mockJSON({
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
    });
    const signal = new AbortController().signal;

    const response = await startUsageQuotaInspection(signal);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.running).toBe(true);
    expect(parsed.pathname).toBe('/api/v1/quota/inspection');
    expect(init).toMatchObject({ credentials: 'include', method: 'POST', signal });
  });

  it('disables selected auth files through the protected management endpoint', async () => {
    const fetchMock = mockJSON({ names: ['a.json'], affected: 1 });

    const response = await setAuthFilesDisabled(['a.json'], true);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');

    expect(response.affected).toBe(1);
    expect(parsed.pathname).toBe('/api/v1/auth-files/status');
    expect(init).toMatchObject({ credentials: 'include', method: 'PATCH' });
    expect(headerValue(init, 'Content-Type')).toBe('application/json');
    expect(init?.body).toBe(JSON.stringify({ names: ['a.json'], disabled: true }));
  });

  it('updates a single credential through the auth-index status endpoint', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper' });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ auth_index: 'provider/idx', disabled: false }),
    } as Response);

    const response = await setCredentialDisabled('ai-provider', 'provider/idx', false);

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');
    expect(response).toEqual({ auth_index: 'provider/idx', disabled: false });
    expect(parsed.pathname).toBe('/keeper/api/v1/ai-providers/provider%2Fidx/status');
    expect(init).toMatchObject({ credentials: 'include', method: 'PATCH' });
    expect(headerValue(init, 'Content-Type')).toBe('application/json');
    expect(init?.body).toBe(JSON.stringify({ disabled: false }));
  });

  it('sends only an integer priority and encoded auth index for each credential kind', async () => {
    const fetchMock = mockJSON({ auth_index: 'idx/one', priority: -3 });
    await setCredentialPriority('auth-file', 'idx/one', -3);
    await setCredentialPriority('ai-provider', 'idx/one', 0);
    const paths = fetchMock.mock.calls.map(([url]) => new URL(String(url), 'http://localhost').pathname);
    expect(paths).toEqual(['/api/v1/auth-files/idx%2Fone/priority', '/api/v1/ai-providers/idx%2Fone/priority']);
    for (const [, init] of fetchMock.mock.calls) {
      expect(init).toMatchObject({ credentials: 'include', method: 'PATCH' });
      expect(Object.keys(JSON.parse(String(init?.body)))).toEqual(['priority']);
    }
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({ priority: -3 });
    expect(JSON.parse(String(fetchMock.mock.calls[1][1]?.body))).toEqual({ priority: 0 });
  });

  it('deletes selected auth files through the protected management endpoint', async () => {
    const fetchMock = mockJSON({ names: ['a.json', 'b.json'], affected: 2 });

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
    const fetchMock = mockJSON({
      authIndex: 'auth-1',
      file_name: 'claude-user.json',
      status: 'completed',
      http_status_code: 401,
      refreshed_at: '2026-05-25T00:00:00Z',
      quota: { id: 'auth-1', quota: [{ key: 'rate_limit.primary_window', label: '5h' }] },
    });
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
    const fetchMock = mockJSON({
      currentVersion: 'v1.2.3',
      latestVersion: 'v1.2.4',
      updateAvailable: true,
      canCompare: true,
      message: 'new version available: v1.2.4',
    });
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
