// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { resolve } from 'node:path';
import { compile } from 'sass';
import { expect, it, vi } from 'vitest';
import { Modal } from '../Modal';
import { Select } from '../Select';
import styles from '../Select.module.scss';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
const selectCSS = compile(resolve(process.cwd(), 'src/components/ui/Select.module.scss')).css;

it('renders disabled selects with the shared inert control state', async () => {
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);
  const stylesheet = document.createElement('style');
  stylesheet.textContent = selectCSS.replace(/\.trigger(?=[:\s[])/g, `.${styles.trigger}`);
  document.head.appendChild(stylesheet);
  try {
    await act(async () => root.render(
      <Select value="a" options={[{ value: 'a', label: 'Alpha' }]} onChange={vi.fn()} disabled />
    ));
    const trigger = container.querySelector<HTMLButtonElement>('button')!;
    expect(trigger.disabled).toBe(true);
    expect(getComputedStyle(trigger).cursor).toBe('not-allowed');
    expect(getComputedStyle(trigger).opacity).toBe('0.6');
  } finally {
    await act(async () => root.unmount());
    container.remove();
    stylesheet.remove();
  }
});

it.each([false, true])('does not interrupt pointer scrolling with automatic option alignment (searchable: %s)', async (searchable) => {
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);
  try {
    await act(async () => root.render(<Select
      value="0" options={Array.from({ length: 12 }, (_, index) => ({ value: String(index), label: `Model ${index}` }))}
      onChange={vi.fn()} search={searchable ? { placeholder: 'Model', noResultsText: 'No models' } : undefined}
    />));
    const trigger = container.querySelector<HTMLInputElement | HTMLButtonElement>(searchable ? 'input' : 'button')!;
    await act(async () => trigger.click());
    const options = document.querySelectorAll<HTMLButtonElement>('[role="option"]');
    const viewport = document.querySelector<HTMLElement>('[data-menu-scroll-viewport]')!;
    vi.spyOn(viewport, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 100, 200, 100));
    vi.spyOn(options[1], 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 250, 200, 36));
    vi.spyOn(options[2], 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 300, 200, 36));
    const pageScroll = vi.spyOn(options[2], 'scrollIntoView');
    await act(async () => options[1].dispatchEvent(new MouseEvent('mouseover', { bubbles: true })));
    expect(viewport.scrollTop).toBe(0);
    await act(async () => trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true })));
    expect(viewport.scrollTop).toBe(136);
    expect(pageScroll).not.toHaveBeenCalled();
  } finally {
    await act(async () => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  }
});

it('opens touch search as a choice list and only focuses text input when requested', async () => {
  vi.stubGlobal('innerHeight', 640);
  const matchMedia = window.matchMedia.bind(window);
  vi.spyOn(window, 'matchMedia').mockImplementation((query) => {
    const media = matchMedia(query);
    Object.defineProperty(media, 'matches', { value: query === '(pointer: coarse)' });
    return media;
  });
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);
  const onChange = vi.fn();
  try {
    await act(async () => root.render(<Select value="a" ariaLabel="Model"
      options={[{ value: 'a', label: 'Alpha' }, { value: 'b', label: 'Beta' }]}
      search={{ placeholder: 'Search models', noResultsText: 'No models' }} onChange={onChange} />));
    const trigger = container.querySelector<HTMLButtonElement>('button[aria-label="Model"]')!;
    expect(trigger).not.toBeNull();
    vi.spyOn(trigger.parentElement!, 'getBoundingClientRect').mockReturnValue(new DOMRect(20, 400, 170, 40));
    await act(async () => trigger.click());
    const input = document.querySelector<HTMLInputElement>('input[role="combobox"]')!;
    expect(input).not.toBeNull();
    expect(document.activeElement).not.toBe(input);
    expect(input.closest<HTMLElement>('[style]')!.style.top).toBe('448px');
    expect(document.querySelectorAll('[role="option"]')).toHaveLength(2);
    await act(async () => document.querySelectorAll<HTMLButtonElement>('[role="option"]')[1].click());
    expect(onChange).toHaveBeenCalledExactlyOnceWith('b');
    expect(document.querySelector('[role="listbox"]')).toBeNull();
    expect(document.activeElement).toBe(trigger);

    await act(async () => trigger.click());
    const searchInput = document.querySelector<HTMLInputElement>('input[role="combobox"]')!;
    await act(async () => {
      searchInput.focus();
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(searchInput, 'bet');
      searchInput.dispatchEvent(new Event('input', { bubbles: true }));
    });
    expect(document.activeElement).toBe(searchInput);
    expect(Array.from(document.querySelectorAll('[role="option"]'), (option) => option.textContent)).toEqual(['Beta']);
    expect(onChange).toHaveBeenCalledTimes(1);
    await act(async () => searchInput.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    expect(document.querySelector('[role="listbox"]')).toBeNull();
    expect(document.activeElement).toBe(trigger);
    await act(async () => trigger.click());
    expect(document.querySelectorAll('[role="option"]')).toHaveLength(2);
    await act(async () => document.body.dispatchEvent(new PointerEvent('pointerdown', { pointerType: 'touch', bubbles: true })));
    expect(document.querySelector('[role="listbox"]')).toBeNull();

    await act(async () => trigger.click());
    await act(async () => trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })));
    const keyboardInput = document.querySelector<HTMLInputElement>('input[role="combobox"]')!;
    expect(document.activeElement).toBe(keyboardInput);
    await act(async () => keyboardInput.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true })));
    expect(document.activeElement).toBe(trigger);
    expect(document.querySelector('[role="listbox"]')).toBeNull();

    await act(async () => trigger.click());
    const dismissInput = document.querySelector<HTMLInputElement>('input[role="combobox"]')!;
    await act(async () => dismissInput.focus());
    await act(async () => dismissInput.blur());
    expect(document.querySelector('[role="listbox"]')).not.toBeNull();
    await act(async () => trigger.click());
    expect(document.querySelector('[role="listbox"]')).toBeNull();
  } finally {
    await act(async () => root.unmount());
    container.remove();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  }
});

it('tracks the visible viewport after keyboard resize and pan without moving over the trigger', async () => {
  const viewport = Object.assign(new EventTarget(), { width: 390, height: 700, offsetLeft: 0, offsetTop: 0 });
  vi.stubGlobal('visualViewport', viewport);
  vi.stubGlobal('innerWidth', 390);
  vi.stubGlobal('innerHeight', 700);
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);
  try {
    await act(async () => root.render(<Select value="a" options={[{ value: 'a', label: 'Alpha' }]} onChange={vi.fn()} />));
    const trigger = container.querySelector('button')!;
    vi.spyOn(trigger.parentElement!, 'getBoundingClientRect').mockReturnValue(new DOMRect(220, 200, 160, 40));
    await act(async () => trigger.click());
    const menu = document.querySelector<HTMLElement>('[role="listbox"]')!;
    expect(menu.style.top).toBe('248px');
    viewport.height = 280;
    await act(async () => viewport.dispatchEvent(new Event('resize')));
    expect(menu.style.top).toBe('192px');
    expect(menu.style.transform).toBe('translateY(-100%)');
    expect(menu.style.maxHeight).toBe('184px');

    viewport.width = 300;
    viewport.height = 500;
    viewport.offsetTop = 100;
    viewport.offsetLeft = 20;
    await act(async () => viewport.dispatchEvent(new Event('scroll')));
    expect(menu.style.top).toBe('248px');
    expect(menu.style.transform).toBe('');
    expect(menu.style.left).toBe('152px');

    // 固定工具条中的锚点不动时，文档滚动也应更新菜单的文档坐标。
    vi.stubGlobal('scrollY', 300);
    vi.stubGlobal('scrollX', 30);
    await act(async () => window.dispatchEvent(new Event('scroll')));
    expect(menu.style.position).toBe('absolute');
    expect(menu.style.top).toBe('548px');
    expect(menu.style.left).toBe('182px');
  } finally {
    await act(async () => root.unmount());
    container.remove();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  }
});

it('closes the nested list before the settings dialog on Escape', async () => {
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);
  const close = vi.fn();
  try {
    await act(async () => root.render(
      <Modal open title="Schedule" onClose={close}>
        <Select value="mon" options={[{ value: 'mon', label: 'Monday' }]} onChange={vi.fn()} ariaLabel="Weekday" />
      </Modal>
    ));
    const trigger = document.querySelector<HTMLButtonElement>('[aria-label="Weekday"]')!;
    await act(async () => trigger.click());
    expect(document.querySelector('[role="listbox"]')).not.toBeNull();
    await act(async () => trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    expect(document.querySelector('[role="listbox"]')).toBeNull();
    expect(close).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(trigger);
    await act(async () => trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    expect(close).toHaveBeenCalledOnce();
  } finally {
    await act(async () => root.unmount());
    container.remove();
  }
});

it.each([{ x: 120, expected: 120 }, { x: 980, expected: 836 }])(
  'aligns a wider menu with its trigger and keeps it inside the viewport at $x',
  async ({ x, expected }) => {
    const container = document.createElement('div');
    document.body.append(container);
    const root = createRoot(container);
    vi.stubGlobal('innerWidth', 1024);
    try {
      await act(async () => root.render(<Select value="a" options={[{ value: 'a', label: 'API Key' }]} onChange={vi.fn()} dropdownMinWidth={180} />));
      const trigger = container.querySelector('button')!;
      vi.spyOn(trigger.parentElement!, 'getBoundingClientRect').mockReturnValue(new DOMRect(x, 50, 44, 36));
      await act(async () => trigger.click());
      const menu = document.querySelector<HTMLElement>('[role="listbox"]')!;
      expect(menu.style.width).toBe('180px');
      expect(menu.style.left).toBe(`${expected}px`);
    } finally {
      await act(async () => root.unmount());
      container.remove();
      vi.unstubAllGlobals();
      vi.restoreAllMocks();
    }
  }
);

it('follows its anchor when layout moves it without resizing the trigger or viewport', async () => {
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);
  try {
    await act(async () => root.render(<Select value="a" options={[{ value: 'a', label: 'API Key' }]} onChange={vi.fn()} />));
    const trigger = container.querySelector('button')!;
    const bounds = vi.spyOn(trigger.parentElement!, 'getBoundingClientRect').mockReturnValue(new DOMRect(600, 80, 180, 44));
    await act(async () => trigger.click());
    const menu = document.querySelector<HTMLElement>('[role="listbox"]')!;
    expect(menu.style.left).toBe('600px');
    bounds.mockReturnValue(new DOMRect(38, 140, 180, 44));
    await act(async () => { await new Promise(requestAnimationFrame); });
    expect(menu.style.left).toBe('38px');
    expect(menu.style.top).toBe('192px');
  } finally {
    await act(async () => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  }
});
