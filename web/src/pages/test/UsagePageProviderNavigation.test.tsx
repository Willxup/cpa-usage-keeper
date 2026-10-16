// @vitest-environment happy-dom

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import { CREDENTIAL_LIST_PREFERENCES_STORAGE_KEYS } from '@/components/usage/credentials/credentialListPreferences';

const api = vi.hoisted(() => ({ fetchUsageIdentitiesPage: vi.fn() }));
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

describe('credential provider navigation', () => {
  let container: HTMLDivElement;
  let root: Root;
  const save = (scope: 'auth-files' | 'ai-provider', providerFilter: string) => {
    localStorage.setItem(CREDENTIAL_LIST_PREFERENCES_STORAGE_KEYS[scope], JSON.stringify({ version: 1, sort: 'last_used_at', pageSize: 10, providerFilter }));
  };
  const saved = (scope: 'auth-files' | 'ai-provider') => JSON.parse(localStorage.getItem(CREDENTIAL_LIST_PREFERENCES_STORAGE_KEYS[scope])!).providerFilter;
  const provider = () => new URLSearchParams(window.location.search).get('provider');
  const select = async (label: string) => {
    const button = Array.from(container.querySelectorAll<HTMLButtonElement>('[role="toolbar"] button')).find((node) => node.textContent?.startsWith(label));
    expect(button).toBeDefined();
    await act(async () => button!.click());
  };
  const navigate = async (path: string) => {
    const link = container.querySelector<HTMLAnchorElement>(`a[href^="/cpa/${path}"]`);
    expect(link).not.toBeNull();
    await act(async () => link!.click());
  };
  const reload = async () => {
    await act(async () => root.unmount());
    root = createRoot(container);
    await act(async () => root.render(<UsagePage />));
  };

  beforeEach(async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    await i18n.changeLanguage('en');
    localStorage.clear();
    window.__APP_BASE_PATH__ = '/cpa';
    window.history.replaceState(null, '', '/cpa/auth-files');
    api.fetchUsageIdentitiesPage.mockReset().mockResolvedValue({ identities: [], total_count: 0, total_pages: 0,
      type_counts: [{ type: 'codex', count: 2 }, { type: 'claude', count: 3 }] });
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    delete window.__APP_BASE_PATH__;
    window.history.replaceState(null, '', '/');
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it.each(['auth-files', 'ai-provider'] as const)('pins the default and manual selections for %s across reloads and other tabs', async (scope) => {
    save(scope, 'claude');
    window.history.replaceState(null, '', `/cpa/${scope}?embed=cpamc&other=keep#details`);
    const historyLength = window.history.length;
    await act(async () => root.render(<UsagePage />));
    expect(provider()).toBe('claude');
    // 模拟另一个标签页更改共享默认偏好；本页的 URL 必须保持自己的选择。
    save(scope, 'codex');
    await reload();
    expect(provider()).toBe('claude');
    expect(saved(scope)).toBe('codex');
    await select('All');
    expect(provider()).toBe('all');
    expect(saved(scope)).toBe('all');
    save(scope, 'codex');
    await reload();
    expect(provider()).toBe('all');
    expect(saved(scope)).toBe('codex');
    expect(api.fetchUsageIdentitiesPage.mock.lastCall?.[1].types).toEqual([]);
    expect(new URLSearchParams(window.location.search).get('other')).toBe('keep');
    expect(new URLSearchParams(window.location.search).get('embed')).toBe('cpamc');
    expect(window.location.hash).toBe('#details');
    expect(window.history.length).toBe(historyLength);
  });

  it('keeps independent page selections in navigation links and when returning to a page', async () => {
    save('auth-files', 'claude');
    save('ai-provider', 'claude');
    window.history.replaceState(null, '', '/cpa/auth-files?provider=codex&embed=cpamc');
    await act(async () => root.render(<UsagePage />));
    expect(provider()).toBe('codex');
    expect(container.querySelector('a[href="/cpa/ai-provider?embed=cpamc&provider=claude"]')).not.toBeNull();
    expect(container.querySelector('a[href="/cpa/request-events?embed=cpamc"]')).not.toBeNull();
    await navigate('ai-provider');
    expect(provider()).toBe('claude');
    await select('All');
    save('auth-files', 'all');
    await navigate('auth-files');
    expect(provider()).toBe('codex');
    expect(saved('auth-files')).toBe('all');
    await reload();
    expect(provider()).toBe('codex');
  });

  it('restores the remembered credential page at the root without treating its query as a provider link', async () => {
    localStorage.setItem('cli-proxy-usage-tab-v1', 'auth-files');
    save('auth-files', 'claude');
    window.history.replaceState(null, '', '/cpa/?provider=codex');
    await act(async () => root.render(<UsagePage />));
    expect(window.location.pathname).toBe('/cpa/auth-files');
    expect(provider()).toBe('claude');
  });

  it.each(['', '?provider=unknown', '?provider=openai'])('normalizes an absent or invalid Auth Files provider: %s', async (search) => {
    window.history.replaceState(null, '', `/cpa/auth-files${search}`);
    await act(async () => root.render(<UsagePage />));
    expect(provider()).toBe('all');
    expect(localStorage.getItem(CREDENTIAL_LIST_PREFERENCES_STORAGE_KEYS['auth-files'])).toBeNull();
  });

  it('keeps a valid linked provider selected even when it has no credentials', async () => {
    save('auth-files', 'claude');
    window.history.replaceState(null, '', '/cpa/auth-files?provider=codex');
    api.fetchUsageIdentitiesPage.mockResolvedValue({ identities: [], total_count: 0, total_pages: 0, type_counts: [{ type: 'claude', count: 3 }] });
    await act(async () => root.render(<UsagePage />));
    expect(provider()).toBe('codex');
    expect(saved('auth-files')).toBe('claude');
    expect(container.querySelector('[role="toolbar"] [aria-pressed="true"]')?.textContent).toBe('Codex0');
    expect(api.fetchUsageIdentitiesPage.mock.calls.every(([, options]) => options.types.join() === 'codex')).toBe(true);
  });
});
