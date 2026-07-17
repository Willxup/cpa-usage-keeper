// @vitest-environment happy-dom

import { act, type ComponentType } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { KeyOverviewTimeRange } from '@/lib/types';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({
    t: (key: string, params?: Record<string, string | number>) => params?.count === undefined ? key : `${key}:${params.count}`,
  }),
}));

interface TimeRangeControlProps {
  value: KeyOverviewTimeRange;
  onChange: (value: KeyOverviewTimeRange) => void;
  ariaLabel: string;
}

const loadTimeRangeControl = async (): Promise<ComponentType<TimeRangeControlProps> | null> => {
  try {
    const modulePath = '../TimeRangeControl';
    const module = await import(/* @vite-ignore */ modulePath) as Record<string, unknown>;
    return (module.TimeRangeControl as ComponentType<TimeRangeControlProps> | undefined) ?? null;
  } catch {
    return null;
  }
};

describe('TimeRangeControl', () => {
  let container: HTMLDivElement;
  let root: Root;
  const initialInnerWidth = window.innerWidth;

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    document.body.replaceChildren();
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: initialInnerWidth });
    vi.restoreAllMocks();
  });

  const renderControl = async (value: KeyOverviewTimeRange, onChange = vi.fn()) => {
    const TimeRangeControl = await loadTimeRangeControl();
    expect(TimeRangeControl).not.toBeNull();
    if (!TimeRangeControl) return { onChange };
    await act(async () => {
      root.render(<TimeRangeControl value={value} onChange={onChange} ariaLabel="Range" />);
    });
    return { onChange };
  };

  it('renders labeled double-pill shells for desktop and mobile triggers', async () => {
    await renderControl('8h');

    const desktopShell = document.querySelector('[data-time-range-shell="desktop"]');
    const mobileShell = document.querySelector('[data-time-range-shell="mobile"]');

    expect(desktopShell?.textContent).toContain('Range');
    expect(desktopShell?.querySelector('[data-time-range-trigger="desktop"]')).not.toBeNull();
    expect(mobileShell?.textContent).toContain('Range');
    expect(mobileShell?.querySelector('[data-time-range-trigger="mobile"]')).not.toBeNull();
  });

  it('includes the applied range in both trigger accessible names', async () => {
    await renderControl('8h');

    expect(document.querySelector('[data-time-range-trigger="desktop"]')?.getAttribute('aria-label')).toBe('Range: usage_stats.range_last_hours:8');
    expect(document.querySelector('[data-time-range-trigger="mobile"]')?.getAttribute('aria-label')).toBe('Range: usage_stats.range_last_hours:8');
  });

  it('uses a fixed SVG timer icon beside the mobile range label', async () => {
    await renderControl('7d');

    const mobileTrigger = document.querySelector('[data-time-range-trigger="mobile"]');

    expect(mobileTrigger?.querySelector('svg')).not.toBeNull();
    expect(mobileTrigger?.textContent).not.toContain('◷');
  });

  it('opens the C popover and switches immediately to natural-day ranges', async () => {
    const { onChange } = await renderControl('8h');
    const trigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    expect(trigger).not.toBeNull();
    await act(async () => trigger?.click());

    const yesterday = document.querySelector<HTMLButtonElement>('[data-time-range-mode="yesterday"]');
    expect(yesterday).not.toBeNull();
    await act(async () => yesterday?.click());

    expect(onChange).toHaveBeenCalledWith('yesterday');
  });

  it('moves focus into the desktop dialog, traps Tab, and restores focus on Escape', async () => {
    await renderControl('8h');
    const trigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    expect(trigger).not.toBeNull();
    trigger?.focus();
    await act(async () => trigger?.click());

    const activeMode = document.querySelector<HTMLButtonElement>('[data-time-range-mode="hour"]');
    const slider = document.querySelector<HTMLInputElement>('[data-time-range-slider]');
    expect(document.activeElement).toBe(activeMode);

    slider?.focus();
    await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true })));
    expect(document.activeElement).toBe(activeMode);

    await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    expect(document.querySelector('[role="dialog"][aria-label="Range"]')).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  it('discards an uncommitted keyboard draft when Escape closes the desktop dialog', async () => {
    const { onChange } = await renderControl('8h');
    const trigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    await act(async () => trigger?.click());
    const slider = document.querySelector<HTMLInputElement>('[data-time-range-slider]');
    expect(slider).not.toBeNull();
    if (!slider) return;

    await act(async () => {
      slider.value = '13';
      slider.dispatchEvent(new Event('input', { bubbles: true }));
    });
    expect(trigger?.textContent).toContain('usage_stats.range_last_hours:13');

    await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));

    expect(onChange).not.toHaveBeenCalled();
    expect(trigger?.textContent).toContain('usage_stats.range_last_hours:8');
    await act(async () => trigger?.click());
    expect(document.querySelector<HTMLInputElement>('[data-time-range-slider]')?.value).toBe('8');
  });

  it('closes the incompatible overlay when crossing the mobile breakpoint', async () => {
    await renderControl('8h');
    const desktopTrigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    const mobileTrigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="mobile"]');
    await act(async () => desktopTrigger?.click());
    expect(desktopTrigger?.getAttribute('aria-expanded')).toBe('true');

    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 600 });
    await act(async () => window.dispatchEvent(new Event('resize')));

    expect(desktopTrigger?.getAttribute('aria-expanded')).toBe('false');
    expect(document.querySelector('[role="dialog"][aria-label="Range"]')).toBeNull();

    await act(async () => mobileTrigger?.click());
    expect(mobileTrigger?.getAttribute('aria-expanded')).toBe('true');

    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 1024 });
    await act(async () => window.dispatchEvent(new Event('resize')));
    expect(mobileTrigger?.getAttribute('aria-expanded')).toBe('false');
  });

  it('renders eighteen independently timed liquid particles for rolling ranges', async () => {
    await renderControl('8h');
    const trigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    await act(async () => trigger?.click());

    const particles = [...document.querySelectorAll<HTMLElement>('[data-liquid-particle]')];
    const motions = new Set(particles.map((particle) => particle.dataset.particleMotion));
    const durations = new Set(particles.map((particle) => particle.style.getPropertyValue('--liquid-particle-duration')));

    expect(particles).toHaveLength(18);
    expect(motions).toEqual(new Set(['a', 'b', 'c']));
    expect(durations.size).toBeGreaterThanOrEqual(6);
    expect(particles.every((particle) => particle.style.getPropertyValue('--liquid-particle-delay').startsWith('-'))).toBe(true);
  });

  it('updates the slider draft without querying until interaction finishes', async () => {
    const { onChange } = await renderControl('8h');
    const trigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    await act(async () => trigger?.click());
    const slider = document.querySelector<HTMLInputElement>('[data-time-range-slider]');
    expect(slider).not.toBeNull();
    if (!slider) return;

    await act(async () => {
      slider.value = '13';
      slider.dispatchEvent(new Event('input', { bubbles: true }));
    });
    expect(onChange).not.toHaveBeenCalled();

    await act(async () => slider.dispatchEvent(new Event('pointerup', { bubbles: true })));
    expect(onChange).toHaveBeenCalledWith('13h');

    await act(async () => slider.dispatchEvent(new FocusEvent('focusout', { bubbles: true })));
    expect(onChange).toHaveBeenCalledTimes(1);
  });

  it.each([
    { initialRange: '8h' as const, minimum: '5', expectedRange: '5h' as const },
    { initialRange: '7d' as const, minimum: '1', expectedRange: '1d' as const },
  ])('commits the latest $expectedRange value when input and pointerup arrive in one batch', async ({ initialRange, minimum, expectedRange }) => {
    const { onChange } = await renderControl(initialRange);
    const trigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    await act(async () => trigger?.click());
    const slider = document.querySelector<HTMLInputElement>('[data-time-range-slider]');
    expect(slider).not.toBeNull();
    if (!slider) return;

    await act(async () => {
      slider.value = minimum;
      slider.dispatchEvent(new Event('input', { bubbles: true }));
      slider.dispatchEvent(new Event('pointerup', { bubbles: true }));
    });

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith(expectedRange);
  });

  it.each([
    { initialRange: '8h' as const, expectedRange: '5h' as const },
    { initialRange: '7d' as const, expectedRange: '1d' as const },
  ])('commits $expectedRange when the pointer is released outside the range card', async ({ initialRange, expectedRange }) => {
    const { onChange } = await renderControl(initialRange);
    const trigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    await act(async () => trigger?.click());
    const slider = document.querySelector<HTMLInputElement>('[data-time-range-slider]');
    expect(slider).not.toBeNull();
    if (!slider) return;

    await act(async () => {
      slider.dispatchEvent(new Event('pointerdown', { bubbles: true }));
      slider.value = initialRange.endsWith('h') ? '5' : '1';
      slider.dispatchEvent(new Event('input', { bubbles: true }));
    });
    expect(onChange).not.toHaveBeenCalled();

    await act(async () => window.dispatchEvent(new Event('pointerup')));

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith(expectedRange);
  });

  it('keeps the first pointer as drag owner when another pointer also starts and ends', async () => {
    const createPointerEvent = (type: string, pointerId: number, bubbles = false) => {
      const event = new Event(type, { bubbles });
      Object.defineProperty(event, 'pointerId', { value: pointerId });
      return event;
    };
    const { onChange } = await renderControl('8h');
    const trigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    await act(async () => trigger?.click());
    const slider = document.querySelector<HTMLInputElement>('[data-time-range-slider]');
    expect(slider).not.toBeNull();
    if (!slider) return;

    await act(async () => {
      slider.dispatchEvent(createPointerEvent('pointerdown', 7, true));
      slider.value = '5';
      slider.dispatchEvent(new Event('input', { bubbles: true }));
      slider.dispatchEvent(createPointerEvent('pointerdown', 8, true));
    });

    await act(async () => window.dispatchEvent(createPointerEvent('pointerup', 8)));
    expect(onChange).not.toHaveBeenCalled();

    await act(async () => window.dispatchEvent(createPointerEvent('pointerup', 7)));
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith('5h');
  });

  it('renders a natural-day summary instead of a slider for today', async () => {
    await renderControl('today');
    const trigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    await act(async () => trigger?.click());

    expect(document.querySelector('[data-time-range-natural-summary="today"]')).not.toBeNull();
    expect(document.querySelector('[data-time-range-slider]')).toBeNull();
  });
});
