// @vitest-environment happy-dom
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it, vi } from 'vitest';
import i18n from '@/i18n';
const api = vi.hoisted(() => ({ fetchKeyQuota: vi.fn() }));
vi.mock('@/lib/api', async (original) => ({
  ...(await original<typeof import('@/lib/api')>()),
  ...api,
}));
import { KeyQuotaPage, quotaRemaining } from '../KeyQuotaPage';

it('preserves unknown and zero quota and renders stale and unavailable accounts', async () => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  await i18n.changeLanguage('en');
  expect(quotaRemaining({ label: 'Weekly' })).toBe('—');
  expect(quotaRemaining({ label: 'Weekly', remaining_percent: 0 })).toBe('0%');
  api.fetchKeyQuota.mockResolvedValue({
    accounts: [
      {
        label: 'Codex account 1',
        status: 'stale',
        rows: [
          {
            label: 'Weekly',
            remaining_percent: 67.5,
            reset_at: '2030-01-01T00:00:00Z',
          },
        ],
      },
      { label: 'Other provider account 1', status: 'unavailable', rows: [] },
    ],
  });
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);
  try {
    await act(async () =>
      root.render(
        <KeyQuotaPage
          apiKey={{ display_key: 'masked', quota_enabled: true }}
          onNavigate={() => undefined}
        />,
      ),
    );
    expect(container.textContent).toContain(
      quotaRemaining({ label: 'Weekly', remaining_percent: 67.5 }),
    );
    expect(container.textContent).toContain('Stale observation');
    expect(container.textContent).toContain('Quota unavailable');
    expect(container.textContent).toContain('Shared provider quota');
    expect(container.textContent).toContain('Resets');
  } finally {
    await act(async () => root.unmount());
    container.remove();
  }
});
