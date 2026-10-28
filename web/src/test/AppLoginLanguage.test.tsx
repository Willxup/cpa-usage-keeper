// @vitest-environment happy-dom

import { act, type ComponentProps } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import App from '../App';
import i18n from '../i18n';
import { ApiError } from '../lib/api';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const api = vi.hoisted(() => ({ getSession: vi.fn(), startOIDCLogin: vi.fn(), login: vi.fn() }));
vi.mock('../lib/api', async (original) => ({ ...await original<typeof import('../lib/api')>(), ...api }));
vi.mock('../components/AppFooter', () => ({ AppFooter: () => null }));
vi.mock('../pages/UsagePage', () => ({ UsagePage: () => null }));
vi.mock('../pages/LoginPage', () => ({ LoginPage: ({ onOIDCLogin, onPasswordSubmit, adminError }: ComponentProps<typeof import('../pages/LoginPage').LoginPage>) => (
  <><div role="alert">{adminError}</div><button data-oidc onClick={() => void onOIDCLogin?.()}>OIDC</button><button data-password onClick={() => void onPasswordSubmit('wrong')}>Password</button></>
) }));

let container: HTMLDivElement;
let root: Root;
beforeEach(async () => {
  vi.resetAllMocks();
  await i18n.changeLanguage('en');
  window.__APP_BASE_PATH__ = '/keeper';
  window.history.replaceState(null, '', '/keeper/auth-files?credential=abc#detail');
  api.getSession.mockResolvedValue({ authenticated: false, oidc_login_enabled: true });
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});
afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  delete window.__APP_BASE_PATH__;
  await i18n.changeLanguage('en');
});

it.each(['callback', 'start', 'password'])('updates a %s error with the selected language', async (source) => {
  const key = source === 'password' ? 'auth.invalid_password' : 'auth.oidc_denied';
  if (source === 'callback') window.history.replaceState(null, '', '/keeper/auth-files?credential=abc&oidc_error=oidc_denied#detail');
  api.startOIDCLogin.mockRejectedValue(new ApiError('upstream text', 403, 'oidc_denied'));
  api.login.mockRejectedValue(new ApiError('upstream text', 401));
  await act(async () => root.render(<App />));
  if (source !== 'callback') await act(async () => container.querySelector<HTMLButtonElement>(source === 'start' ? '[data-oidc]' : '[data-password]')!.click());
  const english = container.querySelector('[role="alert"]')!.textContent;
  expect(english).toBe(i18n.t(key));
  for (const language of ['zh', 'zh-TW', 'en']) {
    await act(async () => { await i18n.changeLanguage(language); });
    const error = container.querySelector('[role="alert"]')!.textContent;
    expect(error).toBe(i18n.t(key));
    if (language !== 'en') expect(error).not.toBe(english);
  }
  expect(window.location.pathname + window.location.search + window.location.hash).toBe('/keeper/auth-files?credential=abc#detail');
  expect(api.startOIDCLogin).toHaveBeenCalledTimes(source === 'start' ? 1 : 0);
});
