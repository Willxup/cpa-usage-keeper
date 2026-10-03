// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AUTO_REFRESH_STORAGE_KEY, AutoRefreshControl } from '../AutoRefreshControl';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string, args?: { seconds: number }) => args ? `Every ${args.seconds}s` : key }) }));

describe('AutoRefreshControl', () => {
  let root: Root;
  let container: HTMLDivElement;
  let hidden: boolean;
  const refresh = vi.fn<() => Promise<unknown>>();
  const onError = vi.fn();
  beforeEach(() => {
    vi.useFakeTimers();
    localStorage.clear();
    vi.clearAllMocks();
    refresh.mockResolvedValue(undefined);
    hidden = false;
    vi.spyOn(document, 'hidden', 'get').mockImplementation(() => hidden);
    container = document.createElement('div');
    document.body.append(container);
    root = createRoot(container);
  });
  afterEach(async () => {
    await act(async () => root.unmount());
    document.body.replaceChildren();
    vi.restoreAllMocks();
    vi.useRealTimers();
  });
  const render = async (busy = false, enabled = true) => act(async () => root.render(<AutoRefreshControl busy={busy} enabled={enabled} onRefresh={refresh} onError={onError} />));
  const advance = async (ms: number) => act(async () => { await vi.advanceTimersByTimeAsync(ms); });
  const choose = async (seconds: number) => {
    await act(async () => container.querySelector<HTMLButtonElement>('button')!.click());
    const label = seconds === 0 ? 'auto_refresh.off' : `Every ${seconds}s`;
    const option = Array.from(document.querySelectorAll<HTMLElement>('[role="option"]')).find(node => node.textContent?.includes(label));
    expect(option).toBeTruthy();
    await act(async () => option!.click());
  };

  it('counts down from the existing 10s default and refreshes without remounting the control', async () => {
    await render();
    const button = container.querySelector('button');
    expect(button?.textContent).toContain('10s');
    await advance(3000);
    expect(button?.textContent).toContain('7s');
    await advance(7000);
    expect(refresh).toHaveBeenCalledTimes(1);
    expect(container.querySelector('button')).toBe(button);
    expect(button?.textContent).toContain('10s');
  });

  it('offers and saves 30s, 60s and Off, and restores the preference on remount', async () => {
    await render();
    await choose(30);
    expect(localStorage.getItem(AUTO_REFRESH_STORAGE_KEY)).toBe('30');
    await advance(29000);
    expect(refresh).not.toHaveBeenCalled();
    await advance(1000);
    expect(refresh).toHaveBeenCalledTimes(1);
    await choose(60);
    await advance(59000);
    expect(refresh).toHaveBeenCalledTimes(1);
    await advance(1000);
    expect(refresh).toHaveBeenCalledTimes(2);
    await choose(0);
    await advance(120000);
    expect(refresh).toHaveBeenCalledTimes(2);
    await act(async () => root.unmount());
    root = createRoot(container);
    await render();
    expect(container.textContent).toContain('auto_refresh.off');
    await advance(120000);
    expect(refresh).toHaveBeenCalledTimes(2);
  });

  it('pauses while hidden and refreshes immediately on return, with no requests when disabled', async () => {
    await render();
    hidden = true;
    await act(async () => document.dispatchEvent(new Event('visibilitychange')));
    await advance(60000);
    expect(refresh).not.toHaveBeenCalled();
    hidden = false;
    await act(async () => document.dispatchEvent(new Event('visibilitychange')));
    expect(refresh).toHaveBeenCalledTimes(1);
    await advance(10000);
    expect(refresh).toHaveBeenCalledTimes(2);
    await render(false, false);
    await advance(60000);
    expect(refresh).toHaveBeenCalledTimes(2);
  });

  it('does not overlap slow requests or manual reloads and retries after an error', async () => {
    let reject!: (error: unknown) => void;
    refresh.mockImplementationOnce(() => new Promise((_, fail) => { reject = fail; }));
    await render();
    await advance(10000);
    await advance(30000);
    expect(refresh).toHaveBeenCalledTimes(1);
    const error = new Error('Network failure');
    await act(async () => reject(error));
    expect(onError).toHaveBeenCalledWith(error);
    await render(true);
    await advance(20000);
    expect(refresh).toHaveBeenCalledTimes(1);
    await render(false);
    await advance(10000);
    expect(refresh).toHaveBeenCalledTimes(2);
  });

  it('does not get stuck refreshing when settings change during a request', async () => {
    let resolve!: () => void;
    refresh.mockImplementationOnce(() => new Promise<void>((done) => { resolve = done; }));
    await render();
    await advance(10000);
    await choose(30);
    await act(async () => resolve());
    await advance(1000);
    expect(container.textContent).toContain('29s');
    expect(refresh).toHaveBeenCalledTimes(1);
  });
});
