// @vitest-environment happy-dom

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import { triggerHeaderRefresh } from '@/hooks/useHeaderRefresh';
import type { UsageCredentialHealth, UsageIdentity } from '@/lib/types';

const api = vi.hoisted(() => ({
  fetchUsageIdentitiesPage: vi.fn(),
  fetchUsageIdentity: vi.fn(),
  resetUsageIdentityStats: vi.fn(),
}));

vi.mock('@/lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/lib/api')>(),
  ...api,
  fetchStatus: async () => ({ timezone: 'UTC' }),
  fetchVersion: async () => ({ version: 'test' }),
  fetchCpaApiKeyOptions: async () => ({ options: [] }),
  fetchUsageQuotaCache: async () => ({ results: [] }),
  fetchUsageQuotaInspectionStatus: async () => ({}),
  fetchQuotaAutoRefreshSettings: async () => ({}),
  fetchUsageQuotaResetCredits: async () => ({}),
}));

import { UsagePage } from '../UsagePage';

function health(success = 10, failure = 0, input = 100, cached = 20, end = '2026-09-11T16:00:00+08:00'): UsageCredentialHealth {
  const endMs = Date.parse(end);
  return {
    window_seconds: 18000, bucket_seconds: 600, window_start: new Date(endMs - 18000_000).toISOString(), window_end: end,
    total_success: success, total_failure: failure, success_rate: success + failure ? success / (success + failure) * 100 : 0,
    input_tokens: input, cache_read_tokens: cached,
    buckets: [{ start_time: new Date(endMs - 600_000).toISOString(), end_time: end, success, failure, rate: success + failure ? success / (success + failure) : 0 }],
  };
}

function identity(authType: 1 | 2, count: number, id = '1'): UsageIdentity {
  return {
    id, auth_type: authType, identity: `fixture-${id}`, name: `Fixture ${id}`, displayName: `Fixture ${id}`, type: 'openai', provider: 'OpenAI',
    total_requests: 100 + count, success_count: 100 + count, total_tokens: 1000 + count * 10,
    period_stats: { total_requests: count, success_count: count, failure_count: 0, total_tokens: count * 10, input_tokens: count * 8, cache_read_tokens: count * 2 },
    credential_health: health(),
  } as UsageIdentity;
}

describe('UsagePage credential detail statistics', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    await i18n.changeLanguage('en');
    localStorage.clear();
    Object.values(api).forEach((mock) => mock.mockReset());
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  const button = (label: string) => {
    const found = Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find((node) => node.textContent?.trim() === label || node.getAttribute('aria-label') === label);
    if (!found) throw new Error(`Missing button: ${label}`);
    return found;
  };
  const metric = () => document.querySelector('[role="tabpanel"] strong')?.textContent;
  const healthPanel = () => Array.from(document.querySelectorAll('h3')).find((node) => node.textContent === 'Recent health')!.parentElement!;
  const expectHealth = (score: string, cacheRate: string) => {
    expect(healthPanel().querySelector('strong')?.textContent).toBe(score);
    const cacheLabel = Array.from(healthPanel().querySelectorAll('span')).find((node) => node.textContent === 'Cache rate 5h')!;
    expect(cacheLabel.nextElementSibling?.textContent).toBe(cacheRate);
  };
  const tick = async () => { await act(async () => vi.advanceTimersByTimeAsync(60_000)); };
  const open = async (label = 'Fixture 1') => {
    const trigger = Array.from(container.querySelectorAll<HTMLButtonElement>('[data-credential-detail-trigger]')).find((node) => node.querySelector('span')?.textContent === label);
    expect(trigger).toBeDefined();
    await act(async () => trigger!.click());
  };

  const render = async (authType: 1 | 2) => {
    window.history.replaceState(null, '', authType === 1 ? '/auth-files' : '/ai-provider');
    api.fetchUsageIdentitiesPage.mockResolvedValue({ identities: [identity(authType, 10), identity(authType, 20, '2')], total_count: 12, total_pages: 2 });
    api.fetchUsageIdentity.mockImplementation(async (id: string) => identity(authType, id === '1' ? 10 : 20, id));
    await act(async () => root.render(<UsagePage />));
  };

  it.each([1, 2] as const)('keeps observing type %s after reset moves it off the list page and retains data on failure', async (authType) => {
    await render(authType);
    expect(api.fetchUsageIdentity).not.toHaveBeenCalled();
    api.fetchUsageIdentity.mockResolvedValue({ ...identity(authType, 10), credential_health: health(9, 1, 200, 50) });
    await open();
    expect(metric()).toBe('10');
    expectHealth('90.0%', '25.00%');
    const initialBuckets = healthPanel().querySelector('[role="list"]')?.textContent;
    const reset = { ...identity(authType, 0), credential_health: undefined, stats_reset_at: '2026-09-10T10:00:00Z' };
    api.resetUsageIdentityStats.mockResolvedValue(reset);
    api.fetchUsageIdentity.mockResolvedValue(reset);
    api.fetchUsageIdentitiesPage.mockResolvedValue({ identities: [identity(authType, 20, '2')], total_count: 12, total_pages: 2 });
    await act(async () => button('Reset statistics').click());
    await act(async () => button('Reset').click());
    expect(metric()).toBe('0');
    expectHealth('90.0%', '25.00%');
    expect(container.textContent).not.toContain('Fixture 1');
    api.fetchUsageIdentity.mockResolvedValue({ ...identity(authType, 5), credential_health: health(4, 1, 200, 100, '2026-09-11T16:10:00+08:00') });
    await tick();
    expect(metric()).toBe('5');
    expectHealth('80.0%', '50.00%');
    expect(healthPanel().querySelector('[role="list"]')?.textContent).not.toBe(initialBuckets);
    expect(api.fetchUsageIdentity).toHaveBeenLastCalledWith('1', expect.any(AbortSignal));
    api.fetchUsageIdentity.mockRejectedValue(new Error('offline'));
    api.fetchUsageIdentitiesPage.mockRejectedValue(new Error('offline'));
    await tick();
    expect(metric()).toBe('5');
    expectHealth('80.0%', '50.00%');
    api.fetchUsageIdentity.mockResolvedValue({ ...identity(authType, 7), credential_health: health(5, 2, 400, 300) });
    await act(async () => triggerHeaderRefresh());
    expect(metric()).toBe('7');
    expectHealth('71.4%', '75.00%');
    // 没有新增用量也要推进健康窗口，过期数据不能一直留在图上。
    api.fetchUsageIdentity.mockResolvedValue({ ...identity(authType, 7), credential_health: health(0, 0, 0, 0, '2026-09-11T22:00:00+08:00') });
    await tick();
    expect(metric()).toBe('7');
    expectHealth('0.0%', '—');
    await act(async () => button('Close').click());
    const calls = api.fetchUsageIdentity.mock.calls.length;
    await tick();
    expect(api.fetchUsageIdentity).toHaveBeenCalledTimes(calls);
  });

  it('ignores a detail response started before reset', async () => {
    await render(2);
    const pending = Promise.withResolvers<UsageIdentity>();
    api.fetchUsageIdentity.mockReturnValueOnce(pending.promise);
    await open();
    const signal = api.fetchUsageIdentity.mock.lastCall?.[1] as AbortSignal;
    api.resetUsageIdentityStats.mockResolvedValue(identity(2, 0));
    await act(async () => button('Reset statistics').click());
    await act(async () => button('Reset').click());
    expect(metric()).toBe('0');
    expect(signal?.aborted).toBe(true);
    await act(async () => pending.resolve(identity(2, 10)));
    expect(metric()).toBe('0');
  });

  it('cancels the previous identity request when opening another credential', async () => {
    await render(2);
    const pending = Promise.withResolvers<UsageIdentity>();
    api.fetchUsageIdentity.mockReturnValueOnce(pending.promise);
    await open();
    const signal = api.fetchUsageIdentity.mock.lastCall?.[1] as AbortSignal;
    await act(async () => button('Close').click());
    await open('Fixture 2');
    expect(signal?.aborted).toBe(true);
    await act(async () => pending.resolve(identity(2, 99)));
    expect(metric()).toBe('20');
  });
});
