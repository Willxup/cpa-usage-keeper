// @vitest-environment happy-dom
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import type { ViewerQuotaAccount } from '@/lib/api';
const api = vi.hoisted(() => ({ fetchAdminProviderQuota: vi.fn(), fetchKeyQuota: vi.fn(), fetchReadOnlyQuota: vi.fn() }));
vi.mock('@/lib/api', async original => ({ ...await original<typeof import('@/lib/api')>(), ...api }));
import { OverviewQuotaPanel, OverviewQuotaTable, remainingPercent, resetCountdown, observationAge, localizedResetDate } from '../OverviewQuotaPanel';

const now = Date.parse('2026-10-03T15:00:00Z');
const accounts: ViewerQuotaAccount[] = [
  { label: 'Codex account 1', provider: 'Codex', status: 'available', rows: [
    { label: '5h', remaining_percent: 0, captured_at: '2026-10-03T14:59:00Z', source: 'websocket_event', reset_at: '2026-10-03T17:18:00Z' },
    { label: 'Weekly', remaining_percent: 92, captured_at: '2026-10-03T14:58:00Z', source: 'api_response_headers', reset_at: '2026-10-09T21:00:00Z' },
    { label: 'Reserve Weekly', remaining_percent: 100 },
  ] },
  { label: 'Codex account 2', provider: 'Codex', status: 'stale', rows: [{ label: 'Weekly', remaining_percent: 64, captured_at: '2026-10-01T15:00:00Z', source: 'scheduled_provider_query', reset_at: '2026-10-03T14:00:00Z' }] },
  { label: 'OpenAI account 1', provider: 'OpenAI', kind: 'api', status: 'unavailable', rows: [] },
];

beforeEach(async () => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  await i18n.changeLanguage('en');
  vi.clearAllMocks();
  for (const fetchQuota of Object.values(api)) fetchQuota.mockResolvedValue({ accounts });
});
afterEach(() => vi.useRealTimers());

describe('overview quota data', () => {
  it('preserves unknown and exhausted quotas, and uses reported numeric limits', () => {
    expect(remainingPercent({ label: 'Unknown' })).toBeNull();
    expect(remainingPercent({ label: 'Empty', remaining_percent: 0 })).toBe(0);
    expect(remainingPercent({ label: 'Requests', remaining: 25, limit: 50 })).toBe(50);
    expect(remainingPercent({ label: 'Invalid', remaining: 10, limit: 0 })).toBeNull();
    expect(remainingPercent({ label: 'Invalid', remaining_percent: NaN })).toBeNull();
    expect(resetCountdown('2026-10-03T17:18:00Z', now)).toBe('2h 18m');
    expect(resetCountdown('2026-10-09T21:00:00Z', now)).toBe('6d 6h 0m');
    expect(resetCountdown('2026-10-09T21:34:00Z', now)).toBe('6d 6h 34m');
    expect(resetCountdown('2026-10-04T15:00:00Z', now)).toBe('1d 0h 0m');
    expect(resetCountdown('2026-10-03T14:00:00Z', now)).toBe('0m');
    expect(resetCountdown(undefined, now)).toBeNull();
    expect(observationAge(now - 123_000, now)).toEqual({ minutes: 2, seconds: '03' });
    expect(observationAge(now + 1_000, now)).toEqual({ minutes: 0, seconds: '00' });
  });

  it('hides reserve, aligns windows with resets, and presents each window’s actual source in an unclipped tooltip', async () => {
    const container = document.createElement('div'); document.body.append(container); const root = createRoot(container);
    try {
      await act(async () => root.render(<OverviewQuotaTable accounts={accounts} now={now} />));
      expect(container.textContent).not.toContain('Reserve');
      expect([...container.querySelectorAll('th')].map(th => th.textContent)).toEqual(['Provider / account', 'WindowRemaining', 'Resets in']);
      const first = container.querySelector('tbody tr')!;
      expect(first.textContent).toContain('0%');
      expect(first.textContent).toContain('2h 18m');
      expect(first.nextElementSibling?.textContent).toContain('6d 6h 0m');
      expect(first.querySelector('td')?.rowSpan).toBe(2);
      expect(container.querySelectorAll('progress')).toHaveLength(3);
      expect(container.textContent).toContain('Awaiting update');
      expect(container.textContent).toContain('Quota unavailable');
      const buttons = container.querySelectorAll('button');
      expect(buttons[0].getAttribute('aria-label')).toContain('WebSocket event');
      expect(buttons[1].getAttribute('aria-label')).toContain('API response headers');
      expect(buttons[0].getAttribute('aria-label')).not.toBe(buttons[1].getAttribute('aria-label'));
      await act(async () => buttons[1].focus());
      const tooltip = document.querySelector('[role="tooltip"]')!;
      expect(tooltip.textContent).toContain('Codex account 1');
      expect(tooltip.textContent).toContain('API response headers');
      expect(tooltip.textContent).toContain('Window: 7d');
      expect(tooltip.textContent).toContain('2m 00s ago');
      expect(tooltip.textContent).toContain(new Date(accounts[0].rows[1].captured_at!).toLocaleString());
      expect(tooltip.textContent).toContain(localizedResetDate(Date.parse(accounts[0].rows[1].reset_at!)));
      expect(container.contains(tooltip)).toBe(false);
    } finally { await act(async () => root.unmount()); container.remove(); }
  });

  it('updates a focused tooltip every second without fetching provider quota again', async () => {
    vi.useFakeTimers(); vi.setSystemTime(now);
    const container = document.createElement('div'); document.body.append(container); const root = createRoot(container);
    try {
      await act(async () => root.render(<OverviewQuotaPanel />));
      await act(async () => container.querySelectorAll('button')[1].focus());
      expect(document.querySelector('[role="tooltip"]')?.textContent).toContain('2m 00s ago');
      await act(async () => vi.advanceTimersByTimeAsync(3_000));
      expect(document.querySelector('[role="tooltip"]')?.textContent).toContain('2m 03s ago');
      expect(document.querySelector('[role="tooltip"]')?.textContent).toContain(new Date(accounts[0].rows[1].captured_at!).toLocaleString());
      expect(api.fetchAdminProviderQuota).toHaveBeenCalledTimes(1);
    } finally { await act(async () => root.unmount()); container.remove(); }
    expect(vi.getTimerCount()).toBe(0);
  });

  it.each(['admin', 'key', 'readOnly'] as const)('only calls the %s cached endpoint and keeps last good data after an error', async scope => {
    vi.useFakeTimers(); vi.setSystemTime(now);
    const selected = scope === 'admin' ? api.fetchAdminProviderQuota : scope === 'key' ? api.fetchKeyQuota : api.fetchReadOnlyQuota;
    const container = document.createElement('div'); const root = createRoot(container);
    try {
      await act(async () => root.render(<OverviewQuotaPanel scope={scope} />));
      expect(selected).toHaveBeenCalledTimes(1);
      for (const fetchQuota of Object.values(api)) if (fetchQuota !== selected) expect(fetchQuota).not.toHaveBeenCalled();
      selected.mockRejectedValueOnce(new Error('offline'));
      await act(async () => vi.advanceTimersByTimeAsync(10_000));
      expect(container.textContent).toContain('Codex account 1');
      expect(container.querySelector('[role="status"]')?.textContent).toBe(i18n.t('key_quota.error'));
    } finally { await act(async () => root.unmount()); }
    expect(vi.getTimerCount()).toBe(0);
  });
});
