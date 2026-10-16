// @vitest-environment happy-dom

import { act, type ComponentProps } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import App from '../App';
import { useUsageStatsStore } from '../stores/useUsageStatsStore';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const api = vi.hoisted(() => ({ getSession: vi.fn(), login: vi.fn(), loginWithCPAAPIKey: vi.fn() }));
vi.mock('../lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('../lib/api')>(), ...api,
}));
vi.mock('react-i18next', async (importOriginal) => ({
  ...await importOriginal<typeof import('react-i18next')>(),
  useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock('../components/AppFooter', () => ({
  AppFooter: ({ loadVersion }: { loadVersion: boolean }) => <footer data-load-version={loadVersion}>Footer</footer>,
}));
vi.mock('../pages/UsagePage', () => ({
  UsagePage: ({ onAuthRequired }: ComponentProps<typeof import('../pages/UsagePage').UsagePage>) => (
    <button data-expire onClick={onAuthRequired}>Admin</button>
  ),
}));
vi.mock('../pages/LoginPage', () => ({
  LoginPage: ({ onPasswordSubmit, onAPIKeySubmit }: ComponentProps<typeof import('../pages/LoginPage').LoginPage>) => (
    <><button data-password-login onClick={() => onPasswordSubmit('password')}>Password</button>
      <button data-key-login onClick={() => onAPIKeySubmit('test-key')}>API Key</button></>
  ),
}));
vi.mock('../pages/KeyOverviewPage', () => ({
  KeyOverviewPage: ({ onNavigate }: ComponentProps<typeof import('../pages/KeyOverviewPage').KeyOverviewPage>) => (
    <button data-navigate onClick={() => onNavigate('/key-ranking')}>Overview</button>
  ),
}));
vi.mock('../pages/KeyAnalysisPage', () => ({ KeyAnalysisPage: () => <div>Analysis</div> }));
vi.mock('../pages/KeyRankingPage', () => ({ KeyRankingPage: () => <div>Ranking</div> }));

describe('App session and navigation', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    vi.resetAllMocks();
    window.__APP_BASE_PATH__ = '/cpa';
    window.history.replaceState(null, '', '/cpa/');
    useUsageStatsStore.getState().clearUsageStats();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    useUsageStatsStore.getState().clearUsageStats();
    delete window.__APP_BASE_PATH__;
    vi.restoreAllMocks();
  });

  it('clears stale usage errors and disables footer loading when a session expires', async () => {
    api.getSession.mockResolvedValue({ authenticated: true, role: 'admin' });
    useUsageStatsStore.setState({ error: 'AUTH_REQUIRED', realtimeError: 'AUTH_REQUIRED' });
    await act(async () => root.render(<App />));
    expect(container.querySelector('footer')!.dataset.loadVersion).toBe('true');
    await act(async () => container.querySelector<HTMLButtonElement>('[data-expire]')!.click());
    expect(useUsageStatsStore.getState()).toMatchObject({ error: '', realtimeError: '' });
    expect(container.querySelector('[data-key-login]')).not.toBeNull();
    expect(container.querySelector('footer')!.dataset.loadVersion).toBe('false');
  });

  it('preserves the embed query through session normalization and viewer navigation', async () => {
    window.history.replaceState(null, '', '/cpa/auth-files?embed=cpamc');
    api.getSession.mockResolvedValue({ authenticated: true, role: 'api_key_viewer' });
    const historyLength = window.history.length;
    await act(async () => root.render(<App />));
    expect(window.location.pathname + window.location.search).toBe('/cpa/key-overview?embed=cpamc');
    expect(container.querySelector('.app-frame')!.getAttribute('data-embed')).toBe('cpamc');
    await act(async () => container.querySelector<HTMLButtonElement>('[data-navigate]')!.click());
    expect(container.textContent).toContain('Ranking');
    expect(window.location.pathname + window.location.search).toBe('/cpa/key-ranking?embed=cpamc');
    expect(window.history.length).toBe(historyLength);
  });

  it.each([
    { role: 'admin', path: '/auth-files', button: '[data-password-login]', page: 'Admin' },
    { role: 'admin', path: '/analysis', button: '[data-password-login]', page: 'Admin' },
    { role: 'api_key_viewer', path: '/key-analysis', button: '[data-key-login]', page: 'Analysis' },
  ])('preserves the allowed path after $role login', async ({ role, path, button, page }) => {
    window.history.replaceState(null, '', `/cpa${path}?embed=cpamc`);
    api.getSession.mockResolvedValueOnce({ authenticated: false }).mockResolvedValueOnce({ authenticated: true, role });
    await act(async () => root.render(<App />));
    await act(async () => container.querySelector<HTMLButtonElement>(button)!.click());
    expect(container.textContent).toContain(page);
    expect(window.location.pathname + window.location.search).toBe(`/cpa${path}?embed=cpamc`);
  });

  it.each(['/auth-files', '/ai-provider'])('preserves provider links through password login at %s', async (path) => {
    window.history.replaceState(null, '', `/cpa${path}?embed=cpamc&provider=codex&other=keep#details`);
    api.getSession.mockResolvedValueOnce({ authenticated: false }).mockResolvedValueOnce({ authenticated: true, role: 'admin' });
    await act(async () => root.render(<App />));
    await act(async () => container.querySelector<HTMLButtonElement>('[data-password-login]')!.click());
    expect(window.location.pathname + window.location.search + window.location.hash).toBe(`/cpa${path}?embed=cpamc&provider=codex&other=keep#details`);
  });

  it('drops administrator provider state when logging in as an API key viewer', async () => {
    window.history.replaceState(null, '', '/cpa/auth-files?embed=cpamc&provider=codex');
    api.getSession.mockResolvedValueOnce({ authenticated: false }).mockResolvedValueOnce({ authenticated: true, role: 'api_key_viewer' });
    await act(async () => root.render(<App />));
    await act(async () => container.querySelector<HTMLButtonElement>('[data-key-login]')!.click());
    expect(window.location.pathname + window.location.search).toBe('/cpa/key-overview?embed=cpamc');
  });
});
