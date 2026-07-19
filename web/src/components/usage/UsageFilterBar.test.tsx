// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import { UsageFilterBar, type UsageFilterBarProps } from './UsageFilterBar';

const defaultProps: UsageFilterBarProps = {
  apiKeyOptions: [{ id: 'key-1', label: 'Production' }],
  selectedApiKeyId: '',
  onApiKeyChange: () => undefined,
  timeRange: '24h',
  timeRangeOptions: [
    { value: '4h', label: '4h' },
    { value: '24h', label: '24h' },
    { value: '7d', label: '7d' },
    { value: 'custom', label: 'Custom' },
  ],
  onTimeRangeChange: () => undefined,
  customTimeRange: { start: '2026-07-10', end: '2026-07-18' },
  onCustomTimeRangeChange: () => undefined,
  customDateRangeBounds: { min: '2025-07-18', max: '2026-07-18' },
  showAutoRefresh: true,
  autoRefreshInterval: 10_000,
  autoRefreshOptions: [
    { value: 0, label: 'Off' },
    { value: 10_000, label: 'Every 10 seconds' },
  ],
  onAutoRefreshChange: () => undefined,
  compact: true,
};

const mountFilterBar = async (props: Partial<UsageFilterBarProps> = {}) => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);

  await act(async () => {
    root.render(<UsageFilterBar {...defaultProps} {...props} />);
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
});

afterEach(() => {
  document.body.innerHTML = '';
  vi.restoreAllMocks();
});

describe('UsageFilterBar control order and time range picker', () => {
  it('keeps Overview controls ordered as interval, API Key, then range', async () => {
    const mounted = await mountFilterBar();

    try {
      const autoRefresh = mounted.container.querySelector('[aria-label="Auto refresh"]');
      const apiKey = mounted.container.querySelector('[aria-label="API Key"]');
      const range = mounted.container.querySelector('[aria-label="Range"]');

      expect(autoRefresh).toBeInstanceOf(HTMLElement);
      expect(apiKey).toBeInstanceOf(HTMLElement);
      expect(range).toBeInstanceOf(HTMLElement);
      expect(autoRefresh?.compareDocumentPosition(apiKey as Node) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
      expect(apiKey?.compareDocumentPosition(range as Node) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    } finally {
      await mounted.unmount();
    }
  });

  it('keeps API Key before range when the interval control is absent', async () => {
    const mounted = await mountFilterBar({ showAutoRefresh: false });

    try {
      const apiKey = mounted.container.querySelector('[aria-label="API Key"]');
      const range = mounted.container.querySelector('[aria-label="Range"]');

      expect(apiKey?.compareDocumentPosition(range as Node) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    } finally {
      await mounted.unmount();
    }
  });

  it('keeps custom dates inside the range popover and applies presets without growing the toolbar', async () => {
    const onTimeRangeChange = vi.fn();
    const mounted = await mountFilterBar({ onTimeRangeChange });

    try {
      expect(mounted.container.querySelector('[aria-label="Custom dates"]')).toBeNull();

      const range = mounted.container.querySelector<HTMLButtonElement>('button[aria-label="Range"]');
      await act(async () => range?.click());

      expect(document.body.textContent).toContain('Quick ranges');
      expect(document.body.textContent).toContain('Absolute time range');
      expect(document.body.querySelector('[aria-label="Custom dates"]')).toBeInstanceOf(HTMLElement);

      const fourHours = Array.from(document.body.querySelectorAll<HTMLButtonElement>('button'))
        .find((button) => button.textContent?.trim() === '4h');
      await act(async () => fourHours?.click());

      expect(onTimeRangeChange).toHaveBeenCalledWith('4h');
      expect(mounted.container.querySelector('[aria-label="Custom dates"]')).toBeNull();
    } finally {
      await mounted.unmount();
    }
  });
});
