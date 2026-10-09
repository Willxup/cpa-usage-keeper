import { afterEach, describe, expect, it, vi } from 'vitest';
import { apiPath, createUsageEventRequestLogDownloadURL, getSession, login, loginWithCPAAPIKey, logout } from '../api';

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('apiPath CPAMC embed behavior', () => {
  it('keeps CPAMC embed query out of API paths', () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper/', location: { search: '?embed=cpamc' } });

    expect(apiPath('/auth/session')).toBe('/keeper/api/v1/auth/session');
  });

  it('marks embed API requests with a header instead of query params', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper/', location: { search: '?embed=cpamc' } });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(Response.json({}));

    await login('secret');

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(String(url), 'http://localhost');
    expect(parsed.pathname).toBe('/keeper/api/v1/auth/login');
    expect(parsed.search).toBe('');
    expect(headerValue(init, 'X-CPA-Usage-Keeper-Embed')).toBe('cpamc');
    expect(headerValue(init, 'X-CPA-Usage-Keeper-Request')).toBe('fetch');
    expect(headerValue(init, 'Content-Type')).toBe('application/json');
  });

  it('sends embed headers on reads without request intent', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper/', location: { search: '?mode=cpamc' } });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(Response.json({ authenticated: false }));

    await getSession();

    const [url, init] = fetchMock.mock.calls[0];
    expect(new URL(String(url), 'http://localhost').pathname).toBe('/keeper/api/v1/auth/session');
    expect(headerValue(init, 'X-CPA-Usage-Keeper-Embed')).toBe('cpamc');
    expect(headerValue(init, 'X-CPA-Usage-Keeper-Request')).toBeNull();
  });

  it('keeps embed session token out of storage when the embed cookie authenticates', async () => {
    const sessionStorage = createSessionStorage();
    vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper/', location: { search: '?embed=cpamc' }, sessionStorage });
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(Response.json({ session_token: 'embed-token' }))
      .mockResolvedValueOnce(Response.json({ authenticated: true, role: 'admin' }))
      .mockResolvedValueOnce(Response.json({ authenticated: true, role: 'admin' }));

    await login('secret');
    await getSession();

    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(headerValue(fetchMock.mock.calls[1][1], 'X-CPA-Usage-Keeper-Embed-Session')).toBeNull();
    expect(headerValue(fetchMock.mock.calls[2][1], 'X-CPA-Usage-Keeper-Embed-Session')).toBeNull();
    expect(sessionStorage.values()).toEqual([]);
  });

  it('stores and sends the embed session token when the embed cookie cannot authenticate', async () => {
    const sessionStorage = createSessionStorage();
    vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper/', location: { search: '?mode=cpamc' }, sessionStorage });
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(Response.json({ session_token: 'embed-token' }))
      .mockResolvedValueOnce(Response.json({ authenticated: false }))
      .mockResolvedValueOnce(Response.json({ authenticated: true, role: 'admin' }));

    await login('secret');
    await getSession();

    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(headerValue(fetchMock.mock.calls[1][1], 'X-CPA-Usage-Keeper-Embed-Session')).toBeNull();
    expect(headerValue(fetchMock.mock.calls[2][1], 'X-CPA-Usage-Keeper-Embed-Session')).toBe('embed-token');
  });

  it('stores and sends the embed session token after API key login when the embed cookie cannot authenticate', async () => {
    const sessionStorage = createSessionStorage();
    vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper/', location: { search: '?embed=cpamc' }, sessionStorage });
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(Response.json({ session_token: 'api-key-embed-token' }))
      .mockResolvedValueOnce(Response.json({ authenticated: false }))
      .mockResolvedValueOnce(Response.json({ authenticated: true, role: 'admin' }));

    await loginWithCPAAPIKey('cpa-key');
    await getSession();

    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(new URL(String(fetchMock.mock.calls[0][0]), 'http://localhost').pathname).toBe('/keeper/api/v1/auth/api-key-login');
    expect(headerValue(fetchMock.mock.calls[1][1], 'X-CPA-Usage-Keeper-Embed-Session')).toBeNull();
    expect(headerValue(fetchMock.mock.calls[2][1], 'X-CPA-Usage-Keeper-Embed-Session')).toBe('api-key-embed-token');
  });

  it('uses embed headers when creating request log download URLs', async () => {
    const sessionStorage = createSessionStorage();
    sessionStorage.setItem('cpa_usage_keeper_embed_session', 'embed-token');
    vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper/', location: { search: '?embed=cpamc' }, sessionStorage });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(Response.json({
      download_url: '/keeper/api/v1/usage/events/42/request-log/download-file?token=abc',
    }));

    const url = await createUsageEventRequestLogDownloadURL('42');

    const [requestURL, init] = fetchMock.mock.calls[0];
    expect(new URL(String(requestURL), 'http://localhost').pathname).toBe('/keeper/api/v1/usage/events/42/request-log/download-token');
    expect(headerValue(init, 'X-CPA-Usage-Keeper-Embed')).toBe('cpamc');
    expect(headerValue(init, 'X-CPA-Usage-Keeper-Embed-Session')).toBe('embed-token');
    expect(headerValue(init, 'X-CPA-Usage-Keeper-Request')).toBe('fetch');
    expect(url).toBe('/keeper/api/v1/usage/events/42/request-log/download-file?token=abc');
  });

  it('does not send an existing fallback token while creating a new embed login', async () => {
    const sessionStorage = createSessionStorage();
    vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper/', location: { search: '?embed=cpamc' }, sessionStorage });
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(Response.json({ session_token: 'old-token' }))
      .mockResolvedValueOnce(Response.json({ authenticated: false }))
      .mockResolvedValueOnce(Response.json({ session_token: 'new-token' }))
      .mockResolvedValueOnce(Response.json({ authenticated: false }));

    await login('secret');
    await login('secret');

    expect(fetchMock).toHaveBeenCalledTimes(4);
    expect(headerValue(fetchMock.mock.calls[2][1], 'X-CPA-Usage-Keeper-Embed-Session')).toBeNull();
    expect(headerValue(fetchMock.mock.calls[3][1], 'X-CPA-Usage-Keeper-Embed-Session')).toBeNull();
  });

  it('clears the embed session token after logout', async () => {
    const sessionStorage = createSessionStorage();
    vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper/', location: { search: '?embed=cpamc' }, sessionStorage });
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(Response.json({ session_token: 'embed-token' }))
      .mockResolvedValueOnce(Response.json({ authenticated: false }))
      .mockResolvedValueOnce(new Response())
      .mockResolvedValueOnce(Response.json({ authenticated: false }));

    await login('secret');
    await logout();
    await getSession();

    expect(fetchMock).toHaveBeenCalledTimes(4);
    expect(headerValue(fetchMock.mock.calls[2][1], 'X-CPA-Usage-Keeper-Embed-Session')).toBe('embed-token');
    expect(headerValue(fetchMock.mock.calls[3][1], 'X-CPA-Usage-Keeper-Embed-Session')).toBeNull();
  });

  it('clears an existing embed session token when getSession returns unauthenticated', async () => {
    const sessionStorage = createSessionStorage();
    sessionStorage.setItem('cpa_usage_keeper_embed_session', 'stale-token');
    vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper/', location: { search: '?embed=cpamc' }, sessionStorage });
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(Response.json({ authenticated: false }))
      .mockResolvedValueOnce(Response.json({ authenticated: false }));

    await expect(getSession()).resolves.toMatchObject({ authenticated: false });
    await getSession();

    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(headerValue(fetchMock.mock.calls[0][1], 'X-CPA-Usage-Keeper-Embed-Session')).toBe('stale-token');
    expect(headerValue(fetchMock.mock.calls[1][1], 'X-CPA-Usage-Keeper-Embed-Session')).toBeNull();
    expect(sessionStorage.values()).toEqual([]);
  });

  it('ignores embed session storage method failures', async () => {
    const sessionStorage = createThrowingSessionStorage();
    vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper/', location: { search: '?embed=cpamc' }, sessionStorage });
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(Response.json({ session_token: 'embed-token' }))
      .mockResolvedValueOnce(Response.json({ authenticated: false }))
      .mockResolvedValueOnce(Response.json({ authenticated: false }));

    await expect(login('secret')).resolves.toBeUndefined();
    await expect(getSession()).resolves.toMatchObject({ authenticated: false });

    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(headerValue(fetchMock.mock.calls[1][1], 'X-CPA-Usage-Keeper-Embed-Session')).toBeNull();
    expect(headerValue(fetchMock.mock.calls[2][1], 'X-CPA-Usage-Keeper-Embed-Session')).toBeNull();
  });

  it('adds request intent to normal mutating requests', async () => {
    vi.stubGlobal('window', { __APP_BASE_PATH__: undefined, location: { search: '' } });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response());

    await logout();

    const [, init] = fetchMock.mock.calls[0];
    expect(headerValue(init, 'X-CPA-Usage-Keeper-Request')).toBe('fetch');
    expect(headerValue(init, 'X-CPA-Usage-Keeper-Embed')).toBeNull();
  });
});

function headerValue(init: RequestInit | undefined, name: string): string | null {
  return new Headers(init?.headers).get(name);
}

function createSessionStorage() {
  const store = new Map<string, string>();
  return {
    getItem: (key: string) => store.get(key) ?? null,
    removeItem: (key: string) => {
      store.delete(key);
    },
    setItem: (key: string, value: string) => {
      store.set(key, value);
    },
    values: () => Array.from(store.values()),
  };
}

function createThrowingSessionStorage() {
  const blocked = () => { throw new Error('session storage blocked'); };
  return { getItem: blocked, removeItem: blocked, setItem: blocked };
}
