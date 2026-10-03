import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { login, loginReadOnly, loginWithCPAAPIKey } from '../api';
beforeEach(() => vi.stubGlobal('window', { __APP_BASE_PATH__: undefined }));
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
it.each([
  ['admin', login],
  ['read-only', loginReadOnly],
  ['key', loginWithCPAAPIKey],
] as const)(
  'sends the explicit cookie choice for %s login',
  async (_name, submit) => {
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response(null, { status: 204 }));
    await submit('test-credential', false);
    await submit('test-credential', true);
    expect(
      JSON.parse(String(fetchMock.mock.calls[0][1]?.body)).rememberMe,
    ).toBe(false);
    expect(
      JSON.parse(String(fetchMock.mock.calls[1][1]?.body)).rememberMe,
    ).toBe(true);
    expect(fetchMock.mock.calls[0][1]?.credentials).toBe('include');
  },
);
