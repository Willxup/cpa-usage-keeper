// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import i18n, { applyDocumentLanguage } from '@/i18n';
import { SidebarUtilityActions } from './SidebarUtilityActions';

const mountActions = async (props: React.ComponentProps<typeof SidebarUtilityActions>) => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);

  await act(async () => {
    root.render(<SidebarUtilityActions {...props} />);
  });

  return {
    container,
    unmount: async () => {
      await act(async () => root.unmount());
      container.remove();
    },
  };
};

beforeEach(async () => {
  await i18n.changeLanguage('en');
  applyDocumentLanguage('en');
});

afterEach(() => {
  document.body.innerHTML = '';
});

describe('SidebarUtilityActions', () => {
  it('always renders Preferences, About, and Sign out while gating Check Updates', async () => {
    const mounted = await mountActions({
      showUpdateCheck: false,
      hasNewVersion: false,
      updateCheckLoading: false,
      onCheckUpdates: vi.fn(),
      loggingOut: false,
      onRequestLogout: vi.fn(),
    });

    try {
      expect(mounted.container.querySelector('[aria-label="Language"]')).toBeInstanceOf(HTMLElement);
      expect(mounted.container.querySelector('[aria-label="Appearance"]')).toBeInstanceOf(HTMLElement);
      expect(mounted.container.textContent).toContain('About');
      expect(mounted.container.textContent).toContain('Sign out');
      expect(mounted.container.textContent).not.toContain('Check Updates');
    } finally {
      await mounted.unmount();
    }
  });

  it('passes update state through to the Check Updates action', async () => {
    const onCheckUpdates = vi.fn();
    const mounted = await mountActions({
      showUpdateCheck: true,
      hasNewVersion: true,
      updateCheckLoading: false,
      onCheckUpdates,
      loggingOut: false,
      onRequestLogout: vi.fn(),
    });

    try {
      const updateButton = Array.from(mounted.container.querySelectorAll('button'))
        .find((button) => button.textContent?.includes('Check Updates'));
      expect(updateButton).toBeInstanceOf(HTMLButtonElement);
      expect(updateButton?.getAttribute('aria-pressed')).toBe('true');

      await act(async () => {
        updateButton?.click();
      });
      expect(onCheckUpdates).toHaveBeenCalledTimes(1);
    } finally {
      await mounted.unmount();
    }
  });

  it('passes Sign out clicks through when idle', async () => {
    const onRequestLogout = vi.fn();
    const mounted = await mountActions({
      showUpdateCheck: true,
      hasNewVersion: false,
      updateCheckLoading: false,
      onCheckUpdates: vi.fn(),
      loggingOut: false,
      onRequestLogout,
    });

    try {
      const logoutButton = Array.from(mounted.container.querySelectorAll('button'))
        .find((button) => button.textContent?.includes('Sign out'));
      expect(logoutButton).toBeInstanceOf(HTMLButtonElement);

      await act(async () => {
        logoutButton?.click();
      });
      expect(onRequestLogout).toHaveBeenCalledTimes(1);
    } finally {
      await mounted.unmount();
    }
  });

  it('shows the Sign out loading state', async () => {
    const mounted = await mountActions({
      showUpdateCheck: true,
      hasNewVersion: false,
      updateCheckLoading: false,
      onCheckUpdates: vi.fn(),
      loggingOut: true,
      onRequestLogout: vi.fn(),
    });

    try {
      const logoutButton = Array.from(mounted.container.querySelectorAll('button'))
        .find((button) => button.textContent?.includes('Sign out'));
      expect(logoutButton?.className).toContain('ant-btn-loading');
    } finally {
      await mounted.unmount();
    }
  });
});
