// @vitest-environment happy-dom
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import { LoginPage, getLoginErrorForMode } from '../LoginPage';

it('offers a distinct read-only login when enabled', async () => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  await i18n.changeLanguage('en');
  const element = document.createElement('div');
  document.body.append(element);
  const root = createRoot(element);
  try {
    await act(async () =>
      root.render(
        <LoginPage
          readOnlyEnabled
          onPasswordSubmit={vi.fn()}
          onAPIKeySubmit={vi.fn()}
          onReadOnlySubmit={vi.fn()}
        />,
      ),
    );
    const tabs = Array.from(
      element.querySelectorAll<HTMLButtonElement>('button[role="tab"]'),
    );
    expect(
      tabs.find((tab) => tab.textContent === i18n.t('auth.admin_tab')),
    ).toBeDefined();
    const readOnly = tabs.find(
      (tab) => tab.textContent === 'Read-only overview',
    );
    expect(readOnly).toBeDefined();
    await act(async () => readOnly!.click());
    expect(element.textContent).toContain('Read-only password');
    expect(element.textContent).toContain('Open overview');
    expect(element.querySelector('input')?.placeholder).toBe(
      'Enter read-only password',
    );
    const remember = element.querySelector<HTMLInputElement>(
      'input[type="checkbox"]',
    );
    expect(remember?.checked).toBe(false);
    expect(element.textContent).toContain('browser session');
    await act(async () => remember!.click());
    expect(remember?.checked).toBe(true);
    expect(element.textContent).toContain('7 days');
    expect(
      getLoginErrorForMode('read_only', {
        adminError: 'admin',
        readOnlyError: 'read-only',
      }),
    ).toBe('read-only');
    await act(async () =>
      root.render(
        <LoginPage onPasswordSubmit={vi.fn()} onAPIKeySubmit={vi.fn()} />,
      ),
    );
    expect(
      Array.from(element.querySelectorAll('button[role="tab"]')).some(
        (tab) => tab.textContent === 'Read-only overview',
      ),
    ).toBe(false);
  } finally {
    await act(async () => root.unmount());
    element.remove();
  }
});
