// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import i18n, { applyDocumentLanguage } from '@/i18n';
import { useThemeStore } from '@/stores';
import { PreferencesDropdown } from './PreferencesDropdown';

const LANGUAGE_STORAGE_KEY = 'cpa-usage-keeper-language';
const THEME_STORAGE_KEY = 'cpa-usage-theme';
const flushPortal = () => new Promise((resolve) => window.setTimeout(resolve, 0));

const mountDropdown = async () => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);

  await act(async () => {
    root.render(<PreferencesDropdown />);
  });

  return {
    container,
    unmount: async () => {
      await act(async () => root.unmount());
      container.remove();
    },
  };
};

const openDropdown = async (container: HTMLElement) => {
  const trigger = container.querySelector<HTMLButtonElement>('button');
  expect(trigger).toBeInstanceOf(HTMLButtonElement);
  await act(async () => {
    trigger?.click();
    await flushPortal();
  });
};

const findMenuItem = (label: string) => Array.from(document.body.querySelectorAll<HTMLElement>('[role="menuitem"]'))
  .find((item) => item.textContent?.trim() === label);

const expectActiveItem = (label: string) => {
  const item = findMenuItem(label);
  expect(item).toBeInstanceOf(HTMLElement);
  expect(item?.querySelector('.anticon-check')).toBeInstanceOf(HTMLElement);
};

beforeEach(async () => {
  localStorage.clear();
  document.documentElement.removeAttribute('data-theme');
  useThemeStore.setState({ theme: 'auto', resolvedTheme: 'light' });
  await i18n.changeLanguage('en');
  applyDocumentLanguage('en');
});

afterEach(() => {
  document.body.innerHTML = '';
});

describe('PreferencesDropdown', () => {
  it('groups every language and appearance option and marks both active preferences', async () => {
    const mounted = await mountDropdown();

    try {
      expect(mounted.container.querySelector('button')?.getAttribute('aria-label')).toBe('Preferences');
      await openDropdown(mounted.container);

      expect(document.body.textContent).toContain('Language');
      expect(document.body.textContent).toContain('Appearance');
      for (const label of ['English', '简体中文', '繁體中文', 'Light', 'White', 'Dark', 'Auto']) {
        expect(findMenuItem(label)).toBeInstanceOf(HTMLElement);
      }
      expectActiveItem('English');
      expectActiveItem('Auto');
      expect(document.body.querySelectorAll('.anticon-check')).toHaveLength(2);
    } finally {
      await mounted.unmount();
    }
  });

  it('changes, applies, and persists the selected language', async () => {
    const mounted = await mountDropdown();

    try {
      await openDropdown(mounted.container);
      const simplifiedChinese = findMenuItem('简体中文');
      expect(simplifiedChinese).toBeInstanceOf(HTMLElement);

      await act(async () => {
        simplifiedChinese?.click();
        await flushPortal();
      });

      expect(i18n.language).toBe('zh');
      expect(document.documentElement.lang).toBe('zh-CN');
      expect(localStorage.getItem(LANGUAGE_STORAGE_KEY)).toBe('zh');
      expect(mounted.container.querySelector('button')?.getAttribute('aria-label')).toBe('偏好设置');
    } finally {
      await mounted.unmount();
    }
  });

  it('applies and persists all four independent theme choices', async () => {
    const mounted = await mountDropdown();
    const choices = [
      { label: 'Light', theme: 'light', attribute: null },
      { label: 'White', theme: 'white', attribute: 'white' },
      { label: 'Dark', theme: 'dark', attribute: 'dark' },
      { label: 'Auto', theme: 'auto', attribute: 'white' },
    ] as const;

    try {
      for (const choice of choices) {
        await openDropdown(mounted.container);
        const item = findMenuItem(choice.label);
        expect(item).toBeInstanceOf(HTMLElement);

        await act(async () => {
          item?.click();
          await flushPortal();
        });

        expect(useThemeStore.getState().theme).toBe(choice.theme);
        expect(document.documentElement.getAttribute('data-theme')).toBe(choice.attribute);
        const persisted = JSON.parse(localStorage.getItem(THEME_STORAGE_KEY) ?? '{}') as { state?: { theme?: string } };
        expect(persisted.state?.theme).toBe(choice.theme);
      }
    } finally {
      await mounted.unmount();
    }
  });
});
