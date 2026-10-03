// @vitest-environment happy-dom
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it, vi } from 'vitest';
import i18n from '@/i18n';
const api = vi.hoisted(() => ({
  fetchReadOnlyKeys: vi.fn(),
  fetchKeyOverview: vi.fn(),
  fetchKeyActivity: vi.fn(),
  fetchUsageOverviewComparisons: vi.fn(),
  fetchReadOnlyQuota: vi.fn(),
}));
vi.mock('@/lib/api', async (original) => ({
  ...(await original<typeof import('@/lib/api')>()),
  ...api,
}));
vi.mock('react-chartjs-2', () => ({
  Line: () => React.createElement('canvas'),
  Bar: () => React.createElement('canvas'),
  Doughnut: () => React.createElement('canvas'),
  Scatter: () => React.createElement('canvas'),
}));
import { ReadOnlyPage } from '../ReadOnlyPage';
import { getRoleTargetPath } from '@/App';
it('uses the native key dashboard with all-key requests and account usage dimensions', async () => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  await i18n.changeLanguage('en');
  window.history.replaceState(null, '', '/read-only');
  localStorage.setItem('keeper-dashboard-toolbar-collapsed', 'false');
  api.fetchReadOnlyKeys.mockResolvedValue({
    keys: [
      { id: '1', label: 'Client key 1' },
      { id: '2', label: 'Client key 2' },
    ],
  });
  api.fetchKeyOverview.mockResolvedValue({
    usage: {
      total_requests: 7,
      total_tokens: 700,
      success_count: 6,
      failure_count: 1,
    },
    summary: { cost_available: true, total_cost: 0 },
    series: {
      buckets: [],
      requests: [],
      tokens: [],
      rpm: [],
      tpm: [],
      cost: [],
      cache_read_rate: [],
    },
    timezone: 'UTC',
  });
  api.fetchKeyActivity.mockResolvedValue({
    blocks: [],
    timezone: 'UTC',
    rows: 0,
    columns: 0,
  });
  const row = {
    key: 'account:opaque',
    label: 'Codex account 1',
    requests: 7,
    failures: 1,
    total_tokens: 700,
    input_tokens: 700,
    output_tokens: 0,
    cache_read_tokens: 0,
    cache_creation_tokens: 0,
    reasoning_tokens: 0,
    cost: 0,
    token_series: [],
  };
  api.fetchUsageOverviewComparisons.mockResolvedValue({
    buckets: [],
    granularity: 'hourly',
    timezone: 'UTC',
    models: [],
    api_keys: [{ ...row, key: '1', label: 'Client key 1' }],
    auth_files: [row],
    ai_providers: [],
  });
  api.fetchReadOnlyQuota.mockResolvedValue({
    accounts: [
      {
        label: 'Codex account 1',
        status: 'available',
        rows: [{ label: 'Weekly', remaining_percent: 96 }],
      },
    ],
  });
  const element = document.createElement('div');
  document.body.append(element);
  const root = createRoot(element);
  try {
    await act(async () =>
      root.render(<ReadOnlyPage onAuthRequired={() => undefined} />),
    );
    expect(
      element.querySelector('[data-keeper-page="key-viewer"]'),
    ).not.toBeNull();
    expect(api.fetchKeyOverview).toHaveBeenCalledWith(
      expect.anything(),
      expect.any(AbortSignal),
      true,
      '',
    );
    expect(api.fetchKeyActivity).toHaveBeenCalledWith(
      expect.objectContaining({ readOnly: true }),
    );
    expect(api.fetchUsageOverviewComparisons).toHaveBeenCalledWith(
      expect.anything(),
      expect.objectContaining({ readOnly: true }),
    );
    const selector = element.querySelector<HTMLButtonElement>(
      '[aria-label^="API Key:"]',
    );
    expect(selector).not.toBeNull();
    await act(async () => selector!.click());
    const option = Array.from(
      document.querySelectorAll<HTMLElement>('[role="option"]'),
    ).find((item) => item.textContent === 'Client key 1');
    expect(option).toBeDefined();
    await act(async () => option!.click());
    expect(api.fetchKeyOverview).toHaveBeenLastCalledWith(
      expect.anything(),
      expect.any(AbortSignal),
      true,
      '1',
    );
    expect(api.fetchKeyActivity).toHaveBeenLastCalledWith(
      expect.objectContaining({ readOnly: true, apiKeyId: '1' }),
    );
    expect(api.fetchUsageOverviewComparisons).toHaveBeenLastCalledWith(
      expect.anything(),
      expect.objectContaining({ readOnly: true, apiKeyId: '1' }),
    );
    const account = element.querySelector<HTMLButtonElement>(
      '[data-dimension="auth_files"]',
    );
    expect(account).not.toBeNull();
    await act(async () => account!.click());
    expect(element.textContent).toContain('Codex account 1');
    const quota = element.querySelector<HTMLAnchorElement>(
      'a[href="/read-only/quota"]',
    );
    expect(quota).not.toBeNull();
    await act(async () => quota!.click());
    expect(element.textContent).toContain('96%');
    expect(element.textContent).not.toMatch(
      /Reset quota|Edit credential|Delete/,
    );
    expect(getRoleTargetPath('read_only', '/auth-files')).toBe('/read-only');
    expect(getRoleTargetPath('read_only', '/read-only/analysis')).toBe(
      '/read-only/analysis',
    );
    expect(getRoleTargetPath('api_key_viewer', '/read-only/analysis')).toBe(
      '/key-overview',
    );
  } finally {
    await act(async () => root.unmount());
    element.remove();
    localStorage.clear();
  }
});
