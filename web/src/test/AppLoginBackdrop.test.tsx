// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import App from '../App';
import styles from '../pages/LoginPage.module.scss';

const api = vi.hoisted(() => ({ getSession: vi.fn(), fetchVersion: vi.fn() }));
vi.mock('@/lib/api', async (original) => ({ ...await original<typeof import('@/lib/api')>(), ...api }));
vi.mock('@/pages/UsagePage', () => ({ UsagePage: () => <div>Usage page</div> }));
vi.mock('@/pages/KeyOverviewPage', () => ({ KeyOverviewPage: () => <div>Key overview</div> }));
vi.mock('@/pages/KeyAnalysisPage', () => ({ KeyAnalysisPage: () => <div>Key analysis</div> }));

let container: HTMLDivElement;
let root: Root | null;

const unmount = async () => {
  await act(async () => root?.unmount());
  root = null;
};

beforeEach(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  api.getSession.mockReset();
  api.fetchVersion.mockReset().mockResolvedValue({ version: 'test' });
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(async () => {
  await unmount();
  container.remove();
  vi.restoreAllMocks();
});

it('places one login backdrop in the shared shell so it covers the content and footer together', async () => {
  api.getSession.mockResolvedValue({ authenticated: false });
  await act(async () => root!.render(<App />));

  const shell = container.querySelector('.app-frame')!;
  const backdrop = container.querySelector(`.${styles.backdrop}`)!;
  expect(backdrop.parentElement).toBe(shell);
  expect(shell.querySelectorAll(`.${styles.backdrop}`)).toHaveLength(1);
  expect(shell.querySelector('.app-footer')?.parentElement).toBe(shell);
  expect(shell.querySelector('[data-keeper-page="login"]')).not.toBeNull();
  expect(shell.getAttribute('data-page')).toBe('login');
});

it('pauses the decorative background without resetting the form and removes its listener on unmount', async () => {
  let hidden = true;
  vi.spyOn(document, 'hidden', 'get').mockImplementation(() => hidden);
  const addListener = vi.spyOn(document, 'addEventListener');
  const removeListener = vi.spyOn(document, 'removeEventListener');
  api.getSession.mockResolvedValue({ authenticated: false });
  await act(async () => root!.render(<App />));
  const backdrop = container.querySelector<HTMLDivElement>(`.${styles.backdrop}`)!;
  const input = container.querySelector('input')!;
  input.value = 'preserve-form-value';
  expect(backdrop.getAttribute('aria-hidden')).toBe('true');
  expect(backdrop.dataset.paused).toBe('true');

  hidden = false;
  await act(async () => document.dispatchEvent(new Event('visibilitychange')));
  expect(backdrop.dataset.paused).toBe('false');
  expect(container.querySelector('input')).toBe(input);
  expect(input.value).toBe('preserve-form-value');

  hidden = true;
  await act(async () => document.dispatchEvent(new Event('visibilitychange')));
  expect(backdrop.dataset.paused).toBe('true');

  const visibilityListener = addListener.mock.calls.find(([event]) => event === 'visibilitychange')?.[1];
  expect(visibilityListener).toBeTypeOf('function');
  await unmount();
  expect(removeListener).toHaveBeenCalledWith('visibilitychange', visibilityListener);
});

it('keeps the login background out of authenticated pages', async () => {
  api.getSession.mockResolvedValue({ authenticated: true, role: 'admin' });
  await act(async () => root!.render(<App />));

  expect(container.querySelector(`.${styles.backdrop}`)).toBeNull();
  expect(container.querySelector('.app-frame')?.hasAttribute('data-page')).toBe(false);
  expect(container.querySelector('.app-footer')).not.toBeNull();
});
