// @vitest-environment happy-dom
import { act, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DashboardToolbar } from '../DashboardToolbar';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key, i18n: { language: 'en' } }) }));

describe('DashboardToolbar', () => {
  let container: HTMLDivElement;
  let root: Root;
  const navigate = vi.fn();
  const refresh = vi.fn();
  const items = [{ id: 'overview', label: 'Overview', href: '/cpa/overview' }, { id: 'analysis', label: 'Analysis', href: '/cpa/analysis' }];
  beforeEach(() => { localStorage.clear(); vi.clearAllMocks(); container = document.createElement('div'); document.body.append(container); root = createRoot(container); });
  afterEach(async () => { await act(async () => root.unmount()); document.body.replaceChildren(); vi.restoreAllMocks(); });
  const render = async (loading = false) => act(async () => root.render(<DashboardToolbar items={items} activeId="overview" onNavigate={navigate} onRefresh={refresh} refreshing={loading} filters={[<button key="filter" onClick={() => {}}>Existing filter</button>]} />));

  it('starts collapsed and remembers an explicit expansion', async () => {
    await render();
    expect(container.querySelector('[data-dashboard-toolbar]')?.getAttribute('data-collapsed')).toBe('true');
    await act(async () => container.querySelector<HTMLButtonElement>('[data-dashboard-collapse]')!.click());
    expect(container.querySelector('[data-dashboard-toolbar]')?.getAttribute('data-collapsed')).toBe('false');
    await act(async () => root.unmount());
    root = createRoot(container);
    await render();
    expect(container.querySelector('[data-dashboard-toolbar]')?.getAttribute('data-collapsed')).toBe('false');
  });

  it('preserves real links, modified clicks and Space activation', async () => {
    const scroll = vi.spyOn(window, 'scrollTo').mockImplementation(() => {});
    await render();
    const link = container.querySelector<HTMLAnchorElement>('[data-dashboard-tabs] a[href="/cpa/analysis"]')!;
    await act(async () => link.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, ctrlKey: true })));
    expect(navigate).not.toHaveBeenCalled();
    expect(scroll).not.toHaveBeenCalled();
    await act(async () => link.click()); expect(navigate).toHaveBeenLastCalledWith('analysis');
    expect(scroll).toHaveBeenLastCalledWith({ top: 0, behavior: 'instant' });
    navigate.mockClear();
    await act(async () => link.dispatchEvent(new KeyboardEvent('keydown', { key: ' ', bubbles: true, cancelable: true })));
    expect(navigate).toHaveBeenCalledWith('analysis');
    expect(scroll).toHaveBeenCalledTimes(2);
  });

  it('keeps filters mounted when collapsed and opens the same chooser from name and arrow', async () => {
    const scroll = vi.spyOn(window, 'scrollTo').mockImplementation(() => {});
    await render();
    const filter = container.querySelector('[data-dashboard-filters] button');
    await act(async () => container.querySelector<HTMLButtonElement>('[data-dashboard-collapse]')!.click());
    expect(container.querySelector('[data-dashboard-filters] button')).toBe(filter);
    await act(async () => container.querySelector<HTMLButtonElement>('[data-dashboard-collapse]')!.click());
    expect(container.querySelector('[data-dashboard-filters] button')).toBe(filter);
    const trigger = container.querySelector<HTMLButtonElement>('[data-dashboard-page-trigger]')!;
    await act(async () => trigger.querySelector<HTMLElement>('[data-dashboard-page-label]')!.click());
    expect(trigger.getAttribute('aria-expanded')).toBe('true');
    await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    expect(trigger.getAttribute('aria-expanded')).toBe('false'); expect(document.activeElement).toBe(trigger);
    await act(async () => trigger.querySelector('svg')!.dispatchEvent(new MouseEvent('click', { bubbles: true })));
    const choice = document.querySelector<HTMLAnchorElement>('[data-dashboard-page-menu] a[href="/cpa/analysis"]')!;
    await act(async () => choice.click()); expect(navigate).toHaveBeenCalledWith('analysis');
    expect(scroll).toHaveBeenLastCalledWith({ top: 0, behavior: 'instant' });
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    navigate.mockClear();
    await act(async () => trigger.click());
    await act(async () => document.querySelector('[data-dashboard-page-menu] a[href="/cpa/analysis"]')!.dispatchEvent(new KeyboardEvent('keydown', { key: ' ', bubbles: true, cancelable: true })));
    expect(navigate).toHaveBeenCalledWith('analysis');
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    expect(scroll).toHaveBeenCalledTimes(2);
  });

  it.each([false, true])('restores menu selection focus, remounting toolbar: %s', async (remount) => {
    function Pages() {
      const [page, setPage] = useState('overview');
      return <DashboardToolbar key={remount ? page : 'shared'} items={items} activeId={page} onNavigate={setPage} onRefresh={refresh} />;
    }
    await act(async () => root.render(<Pages />));
    const trigger = () => container.querySelector<HTMLButtonElement>('[data-dashboard-page-trigger]')!;
    await act(async () => trigger().click());
    await act(async () => container.querySelector<HTMLAnchorElement>('[data-dashboard-page-menu] a[aria-current="page"]')!.click());
    expect(document.activeElement).toBe(trigger());
    await act(async () => trigger().click());
    const next = container.querySelector<HTMLAnchorElement>('[data-dashboard-page-menu] a[href="/cpa/analysis"]')!;
    await act(async () => { next.focus(); next.click(); });
    expect(trigger().getAttribute('aria-label')).toContain('Analysis');
    expect(document.activeElement).toBe(trigger());
  });

  it.each([false, true])('commits a touch selection after a blur with no focus target, remounting toolbar: %s', async (remount) => {
    function Pages() {
      const [page, setPage] = useState('overview');
      return <DashboardToolbar key={remount ? page : 'shared'} items={items} activeId={page} onNavigate={(id) => { navigate(id); setPage(id); }} onRefresh={refresh} />;
    }
    await act(async () => root.render(<Pages />));
    const trigger = () => container.querySelector<HTMLButtonElement>('[data-dashboard-page-trigger]')!;
    await act(async () => trigger().click());
    const next = container.querySelector<HTMLAnchorElement>('[data-dashboard-page-menu] a[href="/cpa/analysis"]')!;
    await act(async () => next.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, pointerType: 'touch' })));
    // Safari 的触摸点击可能先失焦，且不会把 relatedTarget 指向被点击的链接。
    await act(async () => (document.activeElement as HTMLElement).blur());
    expect(next.isConnected).toBe(true);
    await act(async () => next.click());
    expect(navigate).toHaveBeenCalledExactlyOnceWith('analysis');
    expect(trigger().getAttribute('aria-label')).toContain('Analysis');
    expect(trigger().getAttribute('aria-expanded')).toBe('false');
    expect(document.activeElement).toBe(trigger());
  });

  it.each([
    { part: 'name', pointerType: 'touch' },
    { part: 'arrow', pointerType: 'touch' },
    { part: 'name', pointerType: 'mouse' },
    { part: 'arrow', pointerType: 'mouse' },
  ])('toggles from the $part with $pointerType when blur has no focus target', async ({ part, pointerType }) => {
    await render();
    const trigger = container.querySelector<HTMLButtonElement>('[data-dashboard-page-trigger]')!;
    const target = trigger.querySelector(part === 'name' ? '[data-dashboard-page-label]' : 'svg')!;
    await act(async () => target.dispatchEvent(new MouseEvent('click', { bubbles: true })));
    expect(trigger.getAttribute('aria-expanded')).toBe('true');
    await act(async () => target.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, pointerType })));
    await act(async () => (document.activeElement as HTMLElement).blur());
    await act(async () => target.dispatchEvent(new MouseEvent('click', { bubbles: true })));
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    expect(container.querySelector('[data-dashboard-page-menu]')).toBeNull();
    expect(navigate).not.toHaveBeenCalled();
  });

  it.each(['toolbar', 'page'])('closes before an outside touch moves focus: %s', async (target) => {
    await render();
    const trigger = container.querySelector<HTMLButtonElement>('[data-dashboard-page-trigger]')!;
    await act(async () => trigger.click());
    const outside = target === 'toolbar' ? container.querySelector<HTMLButtonElement>('[data-dashboard-refresh]')! : document.body;
    await act(async () => outside.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, pointerType: 'touch' })));
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    if (target === 'toolbar') {
      await act(async () => outside.click());
      expect(refresh).toHaveBeenCalledOnce();
    }
  });

  it('keeps keyboard focus inside the menu and closes when focus moves to a filter', async () => {
    await render();
    const trigger = container.querySelector<HTMLButtonElement>('[data-dashboard-page-trigger]')!;
    await act(async () => trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true })));
    const current = container.querySelector<HTMLAnchorElement>('[data-dashboard-page-menu] a[aria-current="page"]')!;
    expect(document.activeElement).toBe(current);
    await act(async () => current.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true })));
    expect(document.activeElement?.getAttribute('href')).toBe('/cpa/analysis');
    expect(trigger.getAttribute('aria-expanded')).toBe('true');
    await act(async () => container.querySelector<HTMLButtonElement>('[data-dashboard-filters] button')!.focus());
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
  });

  it('keeps the current scroll position when reselecting the current page or refreshing', async () => {
    const scroll = vi.spyOn(window, 'scrollTo').mockImplementation(() => {});
    await render();
    await act(async () => container.querySelector<HTMLAnchorElement>('[data-dashboard-tabs] a[href="/cpa/overview"]')!.click());
    await act(async () => container.querySelector<HTMLButtonElement>('[data-dashboard-page-trigger]')!.click());
    await act(async () => document.querySelector<HTMLAnchorElement>('[data-dashboard-page-menu] a[href="/cpa/overview"]')!.click());
    await act(async () => container.querySelector<HTMLButtonElement>('[data-dashboard-refresh]')!.click());
    expect(scroll).not.toHaveBeenCalled();
    expect(refresh).toHaveBeenCalledOnce();
  });

  it.each([false, true])('shows a working back-to-top control only after scrolling, reduced motion: %s', async (reducedMotion) => {
    const position = vi.spyOn(window, 'scrollY', 'get').mockReturnValue(0);
    const scroll = vi.spyOn(window, 'scrollTo').mockImplementation(() => {});
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: reducedMotion } as MediaQueryList);
    await render();
    const button = document.querySelector<HTMLButtonElement>('[data-dashboard-back-to-top]')!;
    expect(button).not.toBeNull();
    expect(button.disabled).toBe(true);
    position.mockReturnValue(600);
    await act(async () => window.dispatchEvent(new Event('scroll')));
    expect(button.disabled).toBe(false);
    expect(button.getAttribute('aria-label')).toBe('usage_stats.back_to_top');
    await act(async () => button.click());
    expect(scroll).toHaveBeenCalledWith({ top: 0, behavior: reducedMotion ? 'instant' : 'smooth' });
    position.mockReturnValue(0);
    await act(async () => window.dispatchEvent(new Event('scroll')));
    expect(button.disabled).toBe(true);
    expect(button.getAttribute('aria-hidden')).toBe('true');
  });

  it('keeps manual Refresh disabled while loading', async () => {
    await render(true);
    const button = container.querySelector<HTMLButtonElement>('[data-dashboard-refresh]')!;
    expect(button.disabled).toBe(true); expect(button.getAttribute('aria-busy')).toBe('true');
    await act(async () => button.click()); expect(refresh).not.toHaveBeenCalled();
    await render(); await act(async () => button.click()); expect(refresh).toHaveBeenCalledOnce();
  });

  it('works when localStorage is unavailable', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked'); });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked'); });
    await render();
    expect(container.querySelector('[data-dashboard-toolbar]')?.getAttribute('data-collapsed')).toBe('true');
    await act(async () => container.querySelector<HTMLButtonElement>('[data-dashboard-collapse]')!.click());
    expect(container.querySelector('[data-dashboard-toolbar]')?.getAttribute('data-collapsed')).toBe('false');
  });
});
