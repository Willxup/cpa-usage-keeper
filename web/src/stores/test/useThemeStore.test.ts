// @vitest-environment happy-dom

import { afterEach, describe, expect, it } from 'vitest';
import { useThemeStore } from '../useThemeStore';
import { STORAGE_KEY_THEME } from '@/utils/constants';

afterEach(() => {
  useThemeStore.setState(useThemeStore.getInitialState(), true);
  localStorage.clear();
  document.documentElement.removeAttribute('data-theme');
});

describe('useThemeStore', () => {
  it('defaults to auto and persists an applied selection', () => {
    expect(useThemeStore.getState().theme).toBe('auto');
    useThemeStore.getState().setTheme('dark');
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY_THEME)!)).toMatchObject({
      state: { theme: 'dark', resolvedTheme: 'dark' },
    });
  });
});
