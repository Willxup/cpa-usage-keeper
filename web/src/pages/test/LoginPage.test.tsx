// @vitest-environment happy-dom

import { act, type ComponentProps } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { getLoginErrorForMode, LoginPage } from '../LoginPage';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

vi.mock('react-i18next', async (importOriginal) => ({
  ...await importOriginal<typeof import('react-i18next')>(),
  useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock('@/components/ui/LanguageSwitcher', () => ({ LanguageSwitcher: () => null }));

describe('LoginPage mode-specific errors', () => {
  it('shows only the active login mode error', () => {
    expect(getLoginErrorForMode('admin', { adminError: 'bad password', apiKeyError: 'bad api key' })).toBe('bad password');
    expect(getLoginErrorForMode('api_key', { adminError: 'bad password', apiKeyError: 'bad api key' })).toBe('bad api key');
  });

  it('does not leak API Key failures onto the admin tab or admin failures onto the API Key tab', () => {
    expect(getLoginErrorForMode('admin', { adminError: '', apiKeyError: 'bad api key' })).toBe('');
    expect(getLoginErrorForMode('api_key', { adminError: 'bad password', apiKeyError: '' })).toBe('');
  });

});

it('exposes all theme options in a labelled control', () => {
  const html = renderToStaticMarkup(<LoginPage onPasswordSubmit={vi.fn()} onAPIKeySubmit={vi.fn()} />);
  expect(html).toContain('role="tablist" aria-label="usage_stats.theme_switch"');
  for (const theme of ['light', 'dark', 'auto']) {
    expect(html).toContain(`usage_stats.theme_${theme}`);
  }
});

describe('LoginPage SSO entry', () => {
  let container: HTMLDivElement;
  let root: Root;

  const renderPage = async (props: Partial<ComponentProps<typeof LoginPage>> = {}) => {
    await act(async () => root.render(
      <LoginPage onPasswordSubmit={vi.fn()} onAPIKeySubmit={vi.fn()} {...props} />,
    ));
  };

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
  });

  it('hides the SSO button while OIDC is disabled', async () => {
    await renderPage();
    expect(container.textContent).not.toContain('auth.sso_login_submit');
  });

  it('does not render the SSO button when enabled without a handler', async () => {
    await renderPage({ oidcEnabled: true });
    expect(container.textContent).not.toContain('auth.sso_login_submit');
  });

  it('renders the SSO button and starts the flow on click when OIDC is enabled', async () => {
    const onOIDCLogin = vi.fn();
    await renderPage({ oidcEnabled: true, onOIDCLogin });
    const ssoButton = [...container.querySelectorAll('button')]
      .find((button) => button.textContent === 'auth.sso_login_submit');
    expect(ssoButton).toBeTruthy();
    await act(async () => ssoButton!.click());
    expect(onOIDCLogin).toHaveBeenCalledTimes(1);
  });
});
