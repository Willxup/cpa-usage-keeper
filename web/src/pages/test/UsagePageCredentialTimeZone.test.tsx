// @vitest-environment happy-dom

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import type { UsageIdentity } from '@/lib/types';

const api = vi.hoisted(() => ({
  fetchUsageIdentitiesPage: vi.fn(),
  fetchUsageIdentity: vi.fn(),
  fetchUsageQuotaCache: vi.fn(),
}));

vi.mock('@/lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/lib/api')>(),
  ...api,
  fetchStatus: async () => ({ timezone: 'Asia/Shanghai' }),
  fetchVersion: async () => ({ version: 'test' }),
  fetchCpaApiKeyOptions: async () => ({ options: [] }),
  fetchUsageQuotaInspectionStatus: async () => ({}),
}));

import { UsagePage } from '../UsagePage';

const RESET_AT = '2026-05-12T03:15:00-07:00';
const PROJECT_RESET_LABEL = '05/12 18:15';
const COMPACT_RESET_LABEL = '05-12 18:15';

const credential = {
  id: '1',
  name: 'codex-account.json',
  displayName: 'Codex Account',
  auth_type: 1,
  identity: 'auth-1',
  type: 'codex',
  provider: 'OpenAI',
  disabled: false,
  total_requests: 10,
  success_count: 10,
  failure_count: 0,
  input_tokens: 80,
  cache_read_tokens: 40,
  total_tokens: 100,
  is_deleted: false,
} as UsageIdentity;

describe('UsagePage credential project timezone', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    await i18n.changeLanguage('en');
    localStorage.clear();
    window.history.replaceState(null, '', '/credentials');
    Object.values(api).forEach((mock) => mock.mockReset());

    api.fetchUsageIdentitiesPage.mockResolvedValue({
      identities: [credential], total_count: 1, total_pages: 1,
    });
    api.fetchUsageIdentity.mockResolvedValue(credential);
    api.fetchUsageQuotaCache.mockResolvedValue({
      items: [{
        auth_index: credential.identity,
        status: 'completed',
        quota: {
          id: credential.identity,
          quota: [{
            key: 'rate_limit.primary_window',
            label: '5h',
            remainingFraction: 0.75,
            resetAt: RESET_AT,
          }],
        },
      }],
    });

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

  it('uses the project timezone for the compact reset tooltip and the full detail reset label', async () => {
    await act(async () => root.render(<UsagePage />));

    const countdown = container.querySelector<HTMLButtonElement>('[data-quota-reset-countdown="true"]')!;
    expect(countdown).not.toBeNull();
    expect(countdown.getAttribute('aria-label')).toBe(`Resets at ${COMPACT_RESET_LABEL}`);
    expect(countdown.getAttribute('aria-label')).not.toContain('Asia/Shanghai');
    expect(container.textContent).not.toContain(COMPACT_RESET_LABEL);
    expect(container.textContent).not.toContain('05-12 10:15');

    await act(async () => countdown.focus());
    const tooltipId = countdown.getAttribute('aria-describedby')!;
    expect(document.getElementById(tooltipId)?.textContent).toBe(`Resets at ${COMPACT_RESET_LABEL}`);
    expect(document.getElementById(tooltipId)?.textContent).not.toContain('Asia/Shanghai');
    await act(async () => countdown.blur());
    expect(document.getElementById(tooltipId)).toBeNull();
    await act(async () => countdown.click());
    expect(document.getElementById(countdown.getAttribute('aria-describedby')!)?.textContent).toBe(`Resets at ${COMPACT_RESET_LABEL}`);

    const detailTrigger = container.querySelector<HTMLButtonElement>('[data-credential-detail-entry="title"]');
    expect(detailTrigger).not.toBeNull();
    await act(async () => detailTrigger!.click());

    const drawer = document.querySelector<HTMLElement>('[role="dialog"]');
    expect(drawer).not.toBeNull();
    expect(drawer!.textContent).toContain(PROJECT_RESET_LABEL);
  });
});
