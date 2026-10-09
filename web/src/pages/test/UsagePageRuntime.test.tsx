// @vitest-environment happy-dom

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import type { UsageEventRequestLogResponse, UsageEventsResponse } from '@/lib/types';

const api = vi.hoisted(() => ({
  fetchUsageEventRequestLog: vi.fn(),
}));

vi.mock('react-chartjs-2', () => ({
  Bar: () => null,
  Chart: () => null,
  Doughnut: () => null,
  Line: () => null,
  Scatter: () => null,
}));

vi.mock('@/lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/lib/api')>(),
  ...api,
  fetchStatus: async () => ({
    timezone: 'UTC',
    cpa_public_url: 'https://cpa.example.test',
    cpa_request_log_access_enabled: true,
  }),
  fetchVersion: async () => ({ version: 'test' }),
  fetchCpaApiKeyOptions: async () => ({ options: [] }),
  fetchUsageEventModelFilterOptions: async () => ({ models: [] }),
  fetchUsageEventSourceFilterOptions: async () => ({ sources: [] }),
  fetchUsageEvents: async (): Promise<UsageEventsResponse> => ({
    events: [{
      id: '42',
      request_id: 'req-42',
      timestamp: '2026-09-20T00:00:00Z',
      model: 'gpt-5',
      source: 'openai',
      failed: false,
      latency_ms: 100,
      tokens: {
        input_tokens: 1,
        output_tokens: 1,
        reasoning_tokens: 0,
        cache_read_tokens: 0,
        cache_creation_tokens: 0,
        total_tokens: 2,
      },
    }],
    total_count: 1,
    page: 1,
    page_size: 50,
    total_pages: 1,
  }),
}));

import { UsagePage } from '../UsagePage';

const lateResponse: UsageEventRequestLogResponse = {
  event_id: '42',
  request_id: 'req-42',
  available: true,
  sections: [{ title: 'RAW LOG', content: 'late request log content' }],
};

describe('UsagePage runtime behavior', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    await i18n.changeLanguage('en');
    localStorage.clear();
    window.history.replaceState(null, '', '/request-events');
    api.fetchUsageEventRequestLog.mockReset();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    localStorage.clear();
    vi.restoreAllMocks();
  });

  const render = async () => {
    await act(async () => root.render(<UsagePage />));
  };

  it('preserves Safari menu clicks when native mouse focus falls back to an ancestor', async () => {
    await render();
    const trigger = container.querySelector<HTMLButtonElement>('[data-dashboard-page-trigger]')!;
    await act(async () => trigger.click());
    const next = container.querySelector<HTMLAnchorElement>('[data-dashboard-page-menu] a[href$="/settings"]')!;
    await act(async () => {
      next.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, pointerType: 'touch' }));
      next.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
      // 按真机日志重放：Safari 未聚焦链接时，焦点会落到可聚焦的祖先；没有祖先则为空。
      const ancestor = next.parentElement?.closest<HTMLElement>('[tabindex]');
      if (ancestor) ancestor.focus();
      else (document.activeElement as HTMLElement).blur();
    });
    expect(next.isConnected).toBe(true);
    await act(async () => next.click());
    expect(window.location.pathname).toBe('/settings');
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
  });

  const openPendingRequestLog = async () => {
    const pending = Promise.withResolvers<UsageEventRequestLogResponse>();
    api.fetchUsageEventRequestLog.mockReturnValue(pending.promise);
    await render();

    const openButton = container.querySelector<HTMLButtonElement>('[aria-label="Success. View request log"]');
    expect(openButton).not.toBeNull();
    await act(async () => openButton!.click());

    const signal = api.fetchUsageEventRequestLog.mock.lastCall?.[1] as AbortSignal;
    expect(signal.aborted).toBe(false);
    return { pending, signal };
  };

  it.each([
    { mode: 'CPAMC embed', path: '/request-events?embed=cpamc', standalone: false },
    { mode: 'standalone', path: '/request-events', standalone: true },
  ])('keeps the $mode header boundary', async ({ path, standalone }) => {
    window.history.replaceState(null, '', path);
    await render();

    expect(container.querySelector('[data-keeper-page="usage"] header')).not.toBeNull();
    expect(Boolean(container.querySelector('[data-dashboard-header]'))).toBe(standalone);
    expect(Boolean(container.querySelector('a[href="https://cpa.example.test/management.html"]'))).toBe(standalone);
  });

  it('aborts the active request when the log modal closes and ignores its late response', async () => {
    const { pending, signal } = await openPendingRequestLog();
    const closeButton = document.querySelector<HTMLButtonElement>('[role="dialog"] [aria-label="Close"]');
    expect(closeButton).not.toBeNull();

    await act(async () => closeButton!.click());
    expect(signal.aborted).toBe(true);

    await act(async () => {
      pending.resolve(lateResponse);
      await pending.promise;
    });
    expect(document.body.textContent).not.toContain('late request log content');
  });

  it('aborts the active request when UsagePage unmounts', async () => {
    const { pending, signal } = await openPendingRequestLog();

    await act(async () => root.unmount());
    expect(signal.aborted).toBe(true);
    root = createRoot(container);

    await act(async () => {
      pending.resolve(lateResponse);
      await pending.promise;
    });
    expect(document.querySelector('[role="dialog"]')).toBeNull();
  });
});
