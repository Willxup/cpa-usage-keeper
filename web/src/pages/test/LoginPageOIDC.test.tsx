// @vitest-environment happy-dom

import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, expect, it, vi } from 'vitest';
import { LoginPage } from '../LoginPage';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;
vi.mock('react-i18next', async (importOriginal) => ({
  ...await importOriginal<typeof import('react-i18next')>(),
  useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock('@/components/ui/LanguageSwitcher', () => ({ LanguageSwitcher: () => null }));

const container = document.createElement('div');
document.body.appendChild(container);
let root = createRoot(container);
afterEach(async () => {
  await act(async () => root.unmount());
  container.innerHTML = '';
  root = createRoot(container);
});

it('offers OIDC only on the enabled administrator tab and disables it during submission', async () => {
  const login = vi.fn().mockResolvedValue(undefined);
  const props = { onPasswordSubmit: vi.fn(), onAPIKeySubmit: vi.fn(), onOIDCLogin: login, oidcEnabled: true };
  await act(async () => root.render(<LoginPage {...props} />));
  const oidcButton = () => [...container.querySelectorAll('button')].find((button) => button.textContent === 'auth.oidc_login_submit');
  expect(oidcButton()).toBeDefined();
  await act(async () => oidcButton()!.click());
  expect(login).toHaveBeenCalledTimes(1);
  const apiKeyTab = [...container.querySelectorAll<HTMLButtonElement>('[role="tab"]')].find((tab) => tab.textContent === 'auth.api_key_tab')!;
  await act(async () => apiKeyTab.click());
  expect(oidcButton()).toBeUndefined();
  const adminTab = [...container.querySelectorAll<HTMLButtonElement>('[role="tab"]')].find((tab) => tab.textContent === 'auth.admin_tab')!;
  await act(async () => adminTab.click());
  await act(async () => root.render(<LoginPage {...props} loading />));
  expect(oidcButton()!.disabled).toBe(true);
  await act(async () => root.render(<LoginPage {...props} oidcEnabled={false} />));
  expect(oidcButton()).toBeUndefined();
});
