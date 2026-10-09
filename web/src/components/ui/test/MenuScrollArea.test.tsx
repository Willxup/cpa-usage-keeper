// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { MenuScrollArea } from '../MenuScrollArea';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

describe('MenuScrollArea', () => {
  let container: HTMLDivElement;
  let root: Root;
  let area: HTMLDivElement;
  let viewport: HTMLDivElement;
  let track: HTMLDivElement;
  let thumb: HTMLDivElement;
  let scrollHeight: number;
  let reduced: boolean;
  let select: ReturnType<typeof vi.fn>;

  beforeEach(async () => {
    vi.useFakeTimers();
    reduced = false;
    vi.spyOn(window, 'matchMedia').mockImplementation(() => ({
      get matches() { return reduced; },
      addEventListener: vi.fn(), removeEventListener: vi.fn(),
    } as unknown as MediaQueryList));
    container = document.createElement('div');
    document.body.append(container);
    root = createRoot(container);
    select = vi.fn();
    await act(async () => root.render(<MenuScrollArea role="listbox" ariaLabel="Models">
      <button role="option" aria-selected="false" onClick={select}>Model</button>
    </MenuScrollArea>));
    area = container.querySelector('[data-menu-scroll-area]')!;
    viewport = container.querySelector('[data-menu-scroll-viewport]')!;
    track = container.querySelector('[data-menu-scrollbar]')!;
    thumb = container.querySelector('[data-menu-scroll-thumb]')!;
    scrollHeight = 500;
    Object.defineProperties(viewport, {
      clientHeight: { get: () => 200 },
      scrollHeight: { get: () => scrollHeight },
    });
    vi.spyOn(viewport, 'scrollTo').mockImplementation((options) => {
      if (typeof options === 'object') viewport.scrollTop = options.top ?? 0;
      viewport.dispatchEvent(new Event('scroll'));
    });
    track.setPointerCapture = vi.fn();
    window.dispatchEvent(new Event('resize'));
  });
  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  const wheel = (deltaY: number, extra: WheelEventInit = {}) => {
    const event = new WheelEvent('wheel', { deltaY, bubbles: true, cancelable: true, ...extra });
    // happy-dom 的 WheelEvent 未继承鼠标修饰键，补齐浏览器实际提供的属性。
    Object.defineProperty(event, 'ctrlKey', { value: extra.ctrlKey ?? false });
    viewport.dispatchEvent(event);
    return event;
  };
  const pull = () => Number(viewport.style.transform.match(/translate3d\(0, ([\d.-]+)px/)?.[1] ?? 0);
  const touch = (type: string, y: number) => {
    const event = new Event(type, { bubbles: true, cancelable: true });
    Object.defineProperty(event, 'touches', { value: type === 'touchend' ? [] : [{ identifier: 1, clientX: 50, clientY: y }] });
    viewport.dispatchEvent(event);
    return event;
  };

  it('leaves in-range scrolling and pinch-to-zoom with the browser', () => {
    viewport.scrollTop = 100;
    expect(wheel(40).defaultPrevented).toBe(false);
    expect(viewport.scrollTo).not.toHaveBeenCalled();
    viewport.scrollTop = 0;
    expect(wheel(-40, { ctrlKey: true }).defaultPrevented).toBe(false);
    expect(pull()).toBe(0);
  });

  it.each(['top', 'bottom'])('compresses the thumb at the %s edge and restores both layers', async (edge) => {
    const normalHeight = Number.parseFloat(thumb.style.height);
    viewport.scrollTop = edge === 'top' ? 0 : 300;
    expect(wheel(edge === 'top' ? -120 : 120).defaultPrevented).toBe(true);
    expect(Math.sign(pull())).toBe(edge === 'top' ? 1 : -1);
    expect(Math.abs(pull())).toBeLessThanOrEqual(32);
    expect(Number.parseFloat(thumb.style.height)).toBeLessThan(normalHeight);
    const thumbTop = Number(thumb.style.transform.match(/translateY\(([\d.-]+)px/)?.[1]);
    expect(edge === 'top' ? thumbTop : thumbTop + Number.parseFloat(thumb.style.height)).toBeCloseTo(edge === 'top' ? 0 : 188);
    await act(async () => vi.advanceTimersByTime(3000));
    expect(pull()).toBe(0);
    expect(Number.parseFloat(thumb.style.height)).toBe(normalHeight);
  });

  it('starts returning on the next frames instead of waiting at the boundary', async () => {
    wheel(-120);
    const peak = pull();
    await act(async () => vi.advanceTimersByTime(48));
    expect(pull()).toBeGreaterThan(0);
    expect(pull()).toBeLessThan(peak * 0.9);
  });

  it('keeps returning while small momentum-tail wheel events continue arriving', async () => {
    wheel(-120);
    for (let index = 0; index < 30; index++) {
      await act(async () => vi.advanceTimersByTime(16));
      wheel(-0.5);
    }
    expect(Math.abs(pull())).toBeLessThan(2);
  });

  it('unwinds a touch pull gradually and prevents the release from selecting an option', async () => {
    touch('touchstart', 100);
    touch('touchmove', 180);
    const stretched = pull();
    touch('touchmove', 160);
    expect(pull()).toBeGreaterThan(0);
    expect(pull()).toBeLessThan(stretched);
    touch('touchend', 160);
    const option = container.querySelector('button')!;
    await act(async () => option.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, detail: 1 })));
    expect(select).not.toHaveBeenCalled();
    touch('touchstart', 160);
    touch('touchend', 160);
    await act(async () => option.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, detail: 1 })));
    expect(select).toHaveBeenCalledOnce();
  });

  it('does not stretch short lists or users who prefer reduced motion', () => {
    reduced = true;
    wheel(-120);
    expect(pull()).toBe(0);
    reduced = false;
    scrollHeight = 200;
    window.dispatchEvent(new Event('resize'));
    expect(track.hidden).toBe(true);
    expect(wheel(-120).defaultPrevented).toBe(false);
    expect(pull()).toBe(0);
  });

  it('adds a rebound when native touch momentum reaches the end after release', async () => {
    viewport.scrollTop = 100;
    touch('touchstart', 160);
    expect(touch('touchmove', 140).defaultPrevented).toBe(false);
    await act(async () => vi.advanceTimersByTime(16));
    viewport.scrollTop = 120;
    viewport.dispatchEvent(new Event('scroll'));
    touch('touchend', 140);
    for (const top of [230, 290, 300]) {
      await act(async () => vi.advanceTimersByTime(16));
      viewport.scrollTop = top;
      viewport.dispatchEvent(new Event('scroll'));
    }
    expect(pull()).toBeLessThan(0);
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Home', bubbles: true }));
    expect(pull()).toBe(0);
  });

  it('keeps the custom thumb draggable without adding an option or moving selection focus', () => {
    const option = container.querySelector('button')!;
    option.focus();
    thumb.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, clientY: 20, bubbles: true, cancelable: true }));
    track.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, clientY: 60, bubbles: true }));
    expect(viewport.scrollTop).toBeGreaterThan(50);
    expect(document.activeElement).toBe(option);
    expect(container.querySelectorAll('[role="option"]')).toHaveLength(1);
    track.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1 }));
    expect(area.dataset.dragging).toBe('false');
  });

  it('cancels a pending rebound when the menu closes', async () => {
    wheel(-120);
    await act(async () => vi.advanceTimersByTime(150));
    expect(vi.getTimerCount()).toBeGreaterThan(0);
    await act(async () => root.render(null));
    expect(vi.getTimerCount()).toBe(0);
  });
});
