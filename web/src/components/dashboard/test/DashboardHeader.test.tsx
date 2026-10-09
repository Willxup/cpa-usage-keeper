// @vitest-environment happy-dom
import { act, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DashboardHeader } from '../DashboardHeader';
import { Modal } from '@/components/ui/Modal';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const setTheme = vi.fn();
const themeState = { theme: 'white' };
const changeLanguage = vi.fn().mockResolvedValue(undefined);
vi.mock('@/stores', () => ({ useThemeStore: (select: (state: object) => unknown) => select({ theme: themeState.theme, setTheme }) }));
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key, i18n: { language: 'en', changeLanguage } }) }));
vi.mock('@/i18n', () => ({ isSupportedLanguage: (language: string) => ['en', 'zh', 'zh-TW'].includes(language), persistLanguage: vi.fn() }));

describe('DashboardHeader actions', () => {
  let container: HTMLDivElement;
  let root: Root;
  beforeEach(() => { themeState.theme = 'white'; vi.clearAllMocks(); container = document.createElement('div'); document.body.append(container); root = createRoot(container); });
  afterEach(async () => { await act(async () => root.unmount()); vi.useRealTimers(); document.body.replaceChildren(); });

  it('keeps CPA in the header only and preserves update and logout callbacks', async () => {
    const logout = vi.fn(); const check = vi.fn();
    await act(async () => root.render(<DashboardHeader backToCPA="https://cpa.example.test" onLogout={logout} onCheckUpdates={check} />));
    expect(container.querySelectorAll('a[href="https://cpa.example.test"]')).toHaveLength(1);
    const more = container.querySelector<HTMLButtonElement>('[aria-haspopup="menu"]')!;
    await act(async () => more.click());
    const menu = container.querySelector('[role="menu"]')!;
    expect(menu.querySelector('a')).toBeNull();
    const buttons = menu.querySelectorAll<HTMLButtonElement>('button');
    await act(async () => buttons[0].click()); expect(check).toHaveBeenCalledOnce();
    await act(async () => buttons[1].click()); expect(logout).toHaveBeenCalledOnce();
    expect(container.querySelector('[role="menu"]')).toBeNull();
  });

  it('keeps the viewer identity readable in the menu without exposing administrator actions', async () => {
    const identity = 'Research workspace · masked key 1234';
    await act(async () => root.render(<DashboardHeader identity={identity} onLogout={vi.fn()} />));
    const more = container.querySelector<HTMLButtonElement>('[aria-haspopup="menu"]')!;
    await act(async () => more.click());
    expect(container.querySelector('[role="menu"]')?.textContent).toContain(identity);
    expect(container.querySelectorAll('[role="menuitem"]')).toHaveLength(1);
    await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    expect(more.getAttribute('aria-expanded')).toBe('false'); expect(document.activeElement).toBe(more);
  });

  it('keeps header actions clickable when Safari blurs with no related target', async () => {
    const logout = vi.fn(); const check = vi.fn();
    await act(async () => root.render(<DashboardHeader onLogout={logout} onCheckUpdates={check} />));
    const more = container.querySelector<HTMLButtonElement>('[aria-haspopup="menu"]')!;
    await act(async () => more.click());
    const checkButton = container.querySelectorAll<HTMLButtonElement>('[role="menuitem"]')[0];
    await act(async () => checkButton.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, pointerType: 'touch' })));
    await act(async () => (document.activeElement as HTMLElement).blur());
    expect(checkButton.isConnected).toBe(true);
    await act(async () => checkButton.click());
    expect(check).toHaveBeenCalledOnce();

    await act(async () => root.render(<DashboardHeader key="logout" onLogout={logout} />));
    const logoutMenu = container.querySelector<HTMLButtonElement>('[aria-haspopup="menu"]')!;
    await act(async () => logoutMenu.click());
    const logoutButton = container.querySelector<HTMLButtonElement>('[role="menuitem"]')!;
    await act(async () => logoutButton.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, pointerType: 'touch' })));
    await act(async () => (document.activeElement as HTMLElement).blur());
    expect(logoutButton.isConnected).toBe(true);
    await act(async () => logoutButton.click());
    expect(logout).toHaveBeenCalledOnce();
    expect(container.querySelector('[role="menu"]')).toBeNull();
  });

  it('closes the menu for an actual outside pointerdown', async () => {
    await act(async () => root.render(<DashboardHeader onLogout={vi.fn()} />));
    const more = container.querySelector<HTMLButtonElement>('[aria-haspopup="menu"]')!;
    await act(async () => more.click());
    const outside = document.createElement('button');
    document.body.append(outside);
    await act(async () => outside.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, pointerType: 'touch' })));
    expect(container.querySelector('[role="menu"]')).toBeNull();
  });

  it('returns focus to More after cancelling the logout confirmation', async () => {
    vi.useFakeTimers();
    function LogoutConfirmation() {
      const [open, setOpen] = useState(false);
      return <>
        <DashboardHeader onLogout={() => setOpen(true)} />
        <Modal open={open} title="Confirm sign out" onClose={() => setOpen(false)} />
      </>;
    }
    await act(async () => root.render(<LogoutConfirmation />));
    const more = container.querySelector<HTMLButtonElement>('[aria-haspopup="menu"]')!;
    await act(async () => { more.focus(); more.click(); });
    const logout = container.querySelector<HTMLButtonElement>('[role="menuitem"]')!;
    expect(document.activeElement).toBe(logout);
    await act(async () => logout.click());
    await act(async () => vi.advanceTimersByTime(1));
    expect(document.querySelector('[role="dialog"]')?.contains(document.activeElement)).toBe(true);
    await act(async () => document.activeElement?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    await act(async () => vi.advanceTimersByTime(350));
    expect(document.querySelector('[role="dialog"]')).toBeNull();
    expect(document.activeElement).toBe(more);
  });

  it.each([['white', 'dark'], ['dark', 'auto'], ['auto', 'white']])('cycles theme directly from %s to %s', async (current, next) => {
    themeState.theme = current;
    await act(async () => root.render(<DashboardHeader onLogout={vi.fn()} />));
    const button = container.querySelector<HTMLButtonElement>('button:has([data-dashboard-theme])')!;
    await act(async () => button.click());
    expect(setTheme).toHaveBeenCalledWith(next);
    expect(button.hasAttribute('aria-haspopup')).toBe(false);
    expect(document.querySelector('[role="listbox"]')).toBeNull();
  });

  it('keeps the shared language listbox and restores focus after selection', async () => {
    await act(async () => root.render(<DashboardHeader onLogout={vi.fn()} />));
    const language = container.querySelector<HTMLButtonElement>('[aria-label="usage_stats.language_switch: English"]')!;
    await act(async () => language.click());
    const chinese = [...document.querySelectorAll<HTMLButtonElement>('[role="option"]')].find(option => option.textContent === '简体中文')!;
    await act(async () => chinese.click());
    expect(changeLanguage).toHaveBeenCalledWith('zh');
    expect(document.activeElement).toBe(language);
    expect(document.querySelector('[role="listbox"]')).toBeNull();
  });

});
