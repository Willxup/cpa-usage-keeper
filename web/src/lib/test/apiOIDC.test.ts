import { afterEach, expect, it, vi } from 'vitest';
import { ApiError, startOIDCLogin } from '../api';

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

it('starts OIDC through request-intent POST and preserves the in-app target', async () => {
  vi.stubGlobal('window', { __APP_BASE_PATH__: '/keeper', location: { search: '' } });
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(Response.json({ authorization_url: 'https://idp.example/authorize?state=random' }));
  const returnTo = '/keeper/auth-files?credential=abc&provider=codex#detail';
  expect(await startOIDCLogin(returnTo)).toEqual({ authorization_url: 'https://idp.example/authorize?state=random' });
  const [target, init] = fetchMock.mock.calls[0];
  expect(target).toBe('/keeper/api/v1/auth/oidc/login');
  expect(init).toMatchObject({ method: 'POST', credentials: 'include', body: JSON.stringify({ return_to: returnTo }) });
  expect(new Headers(init?.headers).get('X-CPA-Usage-Keeper-Request')).toBe('fetch');
});

it('keeps rate-limit and unavailable responses as typed page errors', async () => {
  vi.stubGlobal('window', { __APP_BASE_PATH__: '', location: { search: '' } });
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(Response.json({ code: 'oidc_unavailable', error: 'Identity service unavailable' }, { status: 503 }));
  await expect(startOIDCLogin('/')).rejects.toMatchObject({ status: 503, code: 'oidc_unavailable' } satisfies Partial<ApiError>);
});
