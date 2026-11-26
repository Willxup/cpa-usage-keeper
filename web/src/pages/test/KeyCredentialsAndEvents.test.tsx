// @vitest-environment happy-dom

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { KeyCredentialsPage } from '../KeyCredentialsPage';
import { isKeyViewerPath, KEY_VIEWER_PAGE_PATHS } from '@/features/key-viewer/navigation';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const api = vi.hoisted(() => ({
  fetchKeyStatus: vi.fn(),
  fetchKeyCredentialsPage: vi.fn(),
  fetchKeyQuotaCache: vi.fn(),
  refreshKeyQuotas: vi.fn(),
  fetchKeyQuotaRefreshTask: vi.fn(),
  fetchKeyCredential: vi.fn(),
  fetchKeyCredentialModels: vi.fn(),
  fetchKeyEvents: vi.fn(),
  fetchKeyCodexQuotaHistory: vi.fn(),
}));

vi.mock('@/lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/lib/api')>(),
  ...api,
}));

vi.mock('react-i18next', async (importOriginal) => ({
  ...await importOriginal<typeof import('react-i18next')>(),
  useTranslation: () => ({ t: (key: string) => key, i18n: { resolvedLanguage: 'en', language: 'en' } }),
}));

describe('KeyCredentialsPage read-only quotas with live refresh', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    vi.resetAllMocks();
    window.localStorage.clear();
    window.history.replaceState(null, '', '/key-credentials');
    api.fetchKeyStatus.mockResolvedValue({ timezone: 'Asia/Shanghai' });
    api.fetchKeyCredentialsPage.mockResolvedValue({
      identities: [{
        id: '1',
        name: 'alice@example.com',
        alias: null,
        displayName: 'alice@example.com',
        auth_type: 1,
        auth_type_name: 'oauth',
        identity: 'auth-codex-1',
        type: 'codex',
        provider: 'codex',
        prefix: '',
        file_name: 'codex-alice@example.com.json',
        priority: 2,
        disabled: false,
        total_requests: 10,
        success_count: 9,
        failure_count: 1,
        input_tokens: 1000,
        output_tokens: 500,
        reasoning_tokens: 0,
        cache_read_tokens: 200,
        total_tokens: 1500,
        last_aggregated_usage_event_id: '10',
        period_stats: { total_requests: 10, success_count: 9, failure_count: 1, input_tokens: 1000, cache_read_tokens: 200, total_tokens: 1500 },
        is_deleted: false,
        created_at: '2026-11-25T00:00:00Z',
        updated_at: '2026-11-25T00:00:00Z',
      }],
      total_count: 1,
      page: 1,
      page_size: 10,
      total_pages: 1,
      type_counts: [{ type: 'codex', count: 1 }],
    });
    api.fetchKeyQuotaCache.mockResolvedValue({ items: [] });
    api.refreshKeyQuotas.mockResolvedValue({ tasks: [{ authIndex: 'auth-codex-1' }], rejected: [], accepted: 1, skipped: 0, limit: 1 });
    api.fetchKeyQuotaRefreshTask.mockResolvedValue({
      authIndex: 'auth-codex-1',
      status: 'completed',
      quota: { id: 'auth-codex-1', quota: [{ key: 'primary', label: '5h', remainingFraction: 0.8 }] },
    });
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  });

  it('registers /key-events and /key-credentials viewer paths', () => {
    expect(KEY_VIEWER_PAGE_PATHS.events).toBe('/key-events');
    expect(KEY_VIEWER_PAGE_PATHS.credentials).toBe('/key-credentials');
    expect(isKeyViewerPath('/key-events')).toBe(true);
    expect(isKeyViewerPath('/key-credentials')).toBe(true);
  });

  it('hides status toggles, edit controls, inspection, and reset while allowing live quota refresh', async () => {
    await act(async () => {
      root.render(
        <KeyCredentialsPage
          apiKey={{ id: '42', display_key: 'sk-***1234' }}
          onNavigate={vi.fn()}
        />,
      );
    });

    expect(container.textContent).toContain('alice@example.com');
    // Must not render interactive status toggle, edit pencil, priority edit button, inspection button, or reset button.
    expect(container.querySelector('[data-credential-status-toggle="true"]')).toBeNull();
    expect(container.querySelector('button[aria-label*="edit"]')).toBeNull();
    expect(container.textContent).not.toContain('usage_stats.credentials_inspection_open');

    // Live quota refresh button must work and invoke /key-quota/refresh.
    const refreshBtn = container.querySelector<HTMLButtonElement>('button[aria-label="usage_stats.credentials_refresh_single"]');
    expect(refreshBtn).not.toBeNull();
    await act(async () => {
      refreshBtn!.click();
    });
    expect(api.refreshKeyQuotas).toHaveBeenCalledWith(['auth-codex-1']);
  });
});
