// @vitest-environment happy-dom

import { act, type ComponentProps } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import App from '../App';
import { ApiError } from '../lib/api';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const api = vi.hoisted(() => ({ getSession: vi.fn(), startOIDCLogin: vi.fn() }));
vi.mock('../lib/api', async (importOriginal) => ({ ...await importOriginal<typeof import('../lib/api')>(), ...api }));
vi.mock('react-i18next', async (importOriginal) => ({ ...await importOriginal<typeof import('react-i18next')>(), useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock('../components/AppFooter', () => ({ AppFooter: () => null }));
vi.mock('../pages/UsagePage', () => ({ UsagePage: ({ onAuthRequired }: { onAuthRequired: () => void }) => <button data-expire onClick={onAuthRequired}>Expire</button> }));
vi.mock('../pages/LoginPage', () => ({ LoginPage: ({ oidcEnabled, onOIDCLogin, adminError }: ComponentProps<typeof import('../pages/LoginPage').LoginPage>) => (
  <><div data-error>{adminError}</div>{oidcEnabled && <button data-oidc onClick={() => void onOIDCLogin?.()}>OIDC</button>}</>
) }));

let container: HTMLDivElement;
let root: Root;
beforeEach(() => {
  vi.resetAllMocks();
  window.__APP_BASE_PATH__ = '/keeper';
  window.history.replaceState(null, '', '/keeper/auth-files?credential=abc#detail');
  container = document.createElement('div'); document.body.appendChild(container); root = createRoot(container);
});
afterEach(async () => { await act(async () => root.unmount()); container.remove(); delete window.__APP_BASE_PATH__; vi.restoreAllMocks(); });

it('preserves OIDC capability after a logged-in session expires', async () => {
  api.getSession.mockResolvedValue({ authenticated: true, role: 'admin', oidc_login_enabled: true });
  await act(async () => root.render(<App />));
  await act(async () => container.querySelector<HTMLButtonElement>('[data-expire]')!.click());
  expect(container.querySelector('[data-oidc]')).not.toBeNull();
});

it('keeps start failures and rate limiting on the page without displaying upstream text', async () => {
  api.getSession.mockResolvedValue({ authenticated: false, oidc_login_enabled: true });
  api.startOIDCLogin.mockRejectedValue(new ApiError('raw upstream secret', 429));
  await act(async () => root.render(<App />));
  await act(async () => container.querySelector<HTMLButtonElement>('[data-oidc]')!.click());
  expect(api.startOIDCLogin).toHaveBeenCalledWith('/keeper/auth-files?credential=abc#detail');
  expect(container.textContent).toContain('auth.login_rate_limited');
  expect(container.textContent).not.toContain('raw upstream');
});

it('hides OIDC in CPAMC even if a response incorrectly advertises the capability', async () => {
  window.history.replaceState(null, '', '/keeper/?embed=cpamc');
  api.getSession.mockResolvedValue({ authenticated: false, oidc_login_enabled: true });
  await act(async () => root.render(<App />));
  expect(container.querySelector('[data-oidc]')).toBeNull();
});
