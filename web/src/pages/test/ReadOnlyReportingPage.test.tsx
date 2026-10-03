// @vitest-environment happy-dom
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it, vi } from 'vitest';
import i18n from '@/i18n';
const api = vi.hoisted(() => ({
  fetchReadOnlyReport: vi.fn(),
  fetchUsageEvents: vi.fn(),
  exportUsageEvents: vi.fn(),
}));
vi.mock('@/lib/api', async (original) => ({
  ...(await original<typeof import('@/lib/api')>()),
  ...api,
}));
import { ReadOnlyReportingPage } from '../ReadOnlyReportingPage';

it('renders cached account usage and drill-down without management controls or admin requests', async () => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  await i18n.changeLanguage('en');
  localStorage.clear();
  const identity = {
    id: '3',
    name: 'Codex account 1',
    displayName: 'Codex account 1',
    identity: 'account:opaque',
    type: 'codex',
    auth_type: 1,
    auth_type_name: 'Auth file',
    disabled: false,
    is_deleted: false,
    total_requests: 5,
    success_count: 4,
    failure_count: 1,
    total_tokens: 1200,
    input_tokens: 1000,
    cache_read_tokens: 100,
    period_stats: {
      total_requests: 5,
      success_count: 4,
      failure_count: 1,
      total_tokens: 1200,
      input_tokens: 1000,
      cache_read_tokens: 100,
    },
  };
  api.fetchReadOnlyReport.mockImplementation((path: string) =>
    Promise.resolve(
      path === 'accounts'
        ? {
            identities: [identity],
            total_count: 1,
            total_pages: 1,
            type_counts: [{ type: 'codex', count: 1 }],
          }
        : {
            items: [
              {
                auth_index: 'account:opaque',
                status: 'completed',
                quota: {
                  id: 'safe',
                  quota: [
                    { key: 'weekly', label: 'Weekly', remainingFraction: 0.94 },
                  ],
                },
              },
            ],
          },
    ),
  );
  api.fetchUsageEvents.mockResolvedValue({ events: [], has_more: false });
  const element = document.createElement('div');
  document.body.append(element);
  const root = createRoot(element);
  try {
    await act(async () =>
      root.render(
        <ReadOnlyReportingPage
          page="auth-files"
          onNavigate={() => undefined}
          onAuthRequired={() => undefined}
        />,
      ),
    );
    expect(element.textContent).toContain('Codex account 1');
    expect(element.textContent).toContain('94');
    expect(element.querySelector('[data-credential-status-toggle]')).toBeNull();
    expect(element.textContent).not.toMatch(
      /Quota inspection|Refresh quota|Reset quota|Edit credential|Delete account/,
    );
    expect(
      api.fetchReadOnlyReport.mock.calls.map((call) => call[0]).sort(),
    ).toEqual(['accounts', 'accounts/quota-cache']);
    const trigger = element.querySelector<HTMLButtonElement>(
      '[data-credential-detail-trigger]',
    );
    expect(trigger).not.toBeNull();
    await act(async () => trigger!.click());
    const requestTab = document.querySelector<HTMLButtonElement>(
      '[data-credential-detail-tab="requests"]',
    );
    expect(requestTab).not.toBeNull();
    expect(
      document.querySelector('[data-credential-detail-tab="errors"]'),
    ).toBeNull();
    expect(
      document.querySelector('[data-credential-detail-tab="quota-history"]'),
    ).toBeNull();
    await act(async () => requestTab!.click());
    expect(api.fetchUsageEvents).toHaveBeenCalledWith(
      undefined,
      expect.any(AbortSignal),
      expect.objectContaining({ readOnly: true, source: 'account:opaque' }),
    );
  } finally {
    await act(async () => root.unmount());
    element.remove();
  }
});

it('uses sanitized event APIs, key scope and both export formats', async () => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  await i18n.changeLanguage('en');
  localStorage.clear();
  api.fetchReadOnlyReport.mockResolvedValue({
    models: ['gpt-model'],
    sources: [],
  });
  api.fetchUsageEvents.mockResolvedValue({
    events: [],
    total_count: 0,
    has_more: false,
  });
  api.exportUsageEvents.mockResolvedValue({
    blob: new Blob(['[]']),
    filename: 'keeper-reporting.json',
  });
  vi.stubGlobal(
    'URL',
    Object.assign(URL, {
      createObjectURL: vi.fn(() => 'blob:sanitized-export'),
      revokeObjectURL: vi.fn(),
    }),
  );
  const click = vi
    .spyOn(HTMLAnchorElement.prototype, 'click')
    .mockImplementation(() => undefined);
  const element = document.createElement('div');
  document.body.append(element);
  const root = createRoot(element);
  try {
    await act(async () =>
      root.render(
        <ReadOnlyReportingPage
          page="events"
          selectedApiKeyId="2"
          onNavigate={() => undefined}
          onAuthRequired={() => undefined}
        />,
      ),
    );
    expect(api.fetchUsageEvents).toHaveBeenLastCalledWith(
      expect.objectContaining({ range: 'today' }),
      expect.any(AbortSignal),
      expect.objectContaining({ readOnly: true, apiKeyId: '2' }),
    );
    const exportButton = Array.from(
      element.querySelectorAll<HTMLButtonElement>('button'),
    ).find(
      (button) =>
        button.getAttribute('aria-label')?.includes('Export') ||
        button.textContent?.includes('Export'),
    );
    expect(exportButton).toBeDefined();
    await act(async () => exportButton!.click());
    const json = Array.from(
      document.querySelectorAll<HTMLButtonElement>('button'),
    ).find((button) => button.textContent?.includes('JSON'));
    expect(json).toBeDefined();
    await act(async () => json!.click());
    expect(api.exportUsageEvents).toHaveBeenCalledWith(
      expect.objectContaining({ range: 'today' }),
      'json',
      expect.objectContaining({ readOnly: true, apiKeyId: '2' }),
    );
    expect(click).toHaveBeenCalled();
    await act(async () => exportButton!.click());
    const csv = Array.from(
      document.querySelectorAll<HTMLButtonElement>('button'),
    ).find((button) => button.textContent?.includes('CSV'));
    expect(csv).toBeDefined();
    await act(async () => csv!.click());
    expect(api.exportUsageEvents).toHaveBeenLastCalledWith(
      expect.objectContaining({ range: 'today' }),
      'csv',
      expect.objectContaining({ readOnly: true, apiKeyId: '2' }),
    );
  } finally {
    await act(async () => root.unmount());
    element.remove();
    click.mockRestore();
    vi.unstubAllGlobals();
  }
});
