// @vitest-environment happy-dom

import { act, Profiler, type ComponentType } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { UsageCustomRange, UsageTimeRange } from '@/lib/types';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({
    t: (key: string, params?: Record<string, string | number>) => params?.count === undefined ? key : `${key}:${params.count}`,
    i18n: { language: 'en-US' },
  }),
}));

interface TimeRangeControlProps {
  value: UsageTimeRange;
  customRange?: UsageCustomRange;
  onChange: (value: UsageTimeRange, customRange?: UsageCustomRange) => void;
  ariaLabel: string;
  timeZone?: string;
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
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  const renderControl = async (value: UsageTimeRange, onChange = vi.fn(), customRange?: UsageCustomRange, timeZone: string | null = 'Asia/Shanghai') => {
    const TimeRangeControl = await loadTimeRangeControl();
    expect(TimeRangeControl).not.toBeNull();
    if (!TimeRangeControl) return { onChange };
    await act(async () => {
      root.render(<TimeRangeControl value={value} customRange={customRange} onChange={onChange} ariaLabel="Range" timeZone={timeZone ?? undefined} />);
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

  it('discards a pending mobile Custom draft when crossing to desktop', async () => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 600 });
    await renderControl('8h');
    const mobileTrigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="mobile"]');
    const desktopTrigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    await act(async () => mobileTrigger?.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-time-range-mode="custom"]')?.click());
    expect(document.querySelector('[data-custom-range-summary]')).not.toBeNull();

    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 1024 });
    await act(async () => window.dispatchEvent(new Event('resize')));
    await act(async () => desktopTrigger?.click());

    const desktopSlider = document.querySelector('[data-time-range-slider]');
    const desktopDialog = desktopSlider?.closest('[role="dialog"]');
    expect(desktopSlider).not.toBeNull();
    expect(desktopDialog?.querySelector('[data-custom-range-summary]')).toBeNull();
  });

  it('does not rerender a closed mobile control during desktop-only resizes', async () => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 1024 });
    const TimeRangeControl = await loadTimeRangeControl();
    expect(TimeRangeControl).not.toBeNull();
    if (!TimeRangeControl) return;
    let commitCount = 0;

    await act(async () => {
      root.render(
        <Profiler id="time-range-control" onRender={() => { commitCount += 1; }}>
          <TimeRangeControl
            value="custom"
            customRange={{ unit: 'day', start: '2026-07-11', end: '2026-07-17' }}
            onChange={vi.fn()}
            ariaLabel="Range"
            timeZone="Asia/Shanghai"
          />
        </Profiler>,
      );
    });
    const commitsAfterRender = commitCount;

    await act(async () => window.dispatchEvent(new Event('resize')));

    expect(commitCount).toBe(commitsAfterRender);
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

  it('restores custom summary actions and applies the current draft directly', async () => {
    const { onChange } = await renderControl('8h');
    const trigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    await act(async () => trigger?.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-time-range-mode="custom"]')?.click());

    expect(onChange).not.toHaveBeenCalled();
    expect(document.querySelector('[data-custom-range-summary]')).not.toBeNull();
    expect(document.querySelector('[data-custom-summary-cancel]')).not.toBeNull();
    expect(document.querySelector('[data-custom-summary-apply]')).not.toBeNull();
    expect(document.querySelector('input[type="date"], input[type="time"], input[type="datetime-local"]')).toBeNull();

    await act(async () => document.querySelector<HTMLButtonElement>('[data-custom-summary-apply]')?.click());

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith('custom', expect.objectContaining({ unit: 'day' }));
  });

  it('cancels the custom summary without querying and closes the range popover', async () => {
    const { onChange } = await renderControl('8h');
    const trigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    await act(async () => trigger?.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-time-range-mode="custom"]')?.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-custom-summary-cancel]')?.click());

    expect(onChange).not.toHaveBeenCalled();
    expect(trigger?.getAttribute('aria-expanded')).toBe('false');
    expect(document.querySelector('[role="dialog"][aria-label="Range"]')).toBeNull();
  });

  it('syncs the visible calendar month when switching between start and end', async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-07-17T07:36:42.000Z'));
    await renderControl('custom', vi.fn(), {
      unit: 'day',
      start: '2026-06-18',
      end: '2026-07-17',
    });
    const trigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    await act(async () => trigger?.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-custom-endpoint="start"]')?.click());

    expect(document.querySelector('[data-custom-calendar-month]')?.getAttribute('data-custom-calendar-month')).toBe('2026-06');

    await act(async () => document.querySelector<HTMLButtonElement>('[data-custom-picker-endpoint="end"]')?.click());

    expect(document.querySelector('[data-custom-calendar-month]')?.getAttribute('data-custom-calendar-month')).toBe('2026-07');
  });

  it('keeps crossed-month ranges continuous inside a fixed six-week calendar', async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-07-17T07:36:42.000Z'));
    await renderControl('custom', vi.fn(), {
      unit: 'day',
      start: '2026-06-30',
      end: '2026-07-10',
    });
    const trigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    await act(async () => trigger?.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-custom-endpoint="start"]')?.click());

    expect(document.querySelector('[data-custom-calendar-month]')?.getAttribute('data-custom-calendar-month')).toBe('2026-06');
    expect(document.querySelectorAll('[data-custom-calendar-cell]')).toHaveLength(42);
    expect(document.querySelector('[data-custom-calendar-cell="2026-05-31"]')).not.toBeNull();
    expect(document.querySelector('[data-custom-calendar-cell="2026-07-11"]')).not.toBeNull();
    expect(document.querySelector('[data-custom-calendar-cell="2026-07-01"]')?.hasAttribute('data-custom-outside-month')).toBe(true);
    expect(document.querySelector('[data-custom-calendar-cell="2026-07-01"]')?.hasAttribute('data-custom-in-range')).toBe(true);
    expect(document.querySelector('[data-custom-calendar-cell="2026-07-01"]')?.hasAttribute('data-custom-range-row-start')).toBe(false);
    expect(document.querySelector('[data-custom-calendar-cell="2026-07-01"]')?.hasAttribute('data-custom-range-row-end')).toBe(false);
    expect(document.querySelector('[data-custom-calendar-cell="2026-06-30"]')?.hasAttribute('data-custom-range-row-start')).toBe(true);
    expect(document.querySelector('[data-custom-calendar-cell="2026-07-04"]')?.hasAttribute('data-custom-range-row-end')).toBe(true);
    expect(document.querySelector('[data-custom-calendar-cell="2026-07-05"]')?.hasAttribute('data-custom-range-row-start')).toBe(true);
    expect(document.querySelector('[data-custom-calendar-cell="2026-07-10"]')?.hasAttribute('data-custom-range-row-end')).toBe(true);

    await act(async () => document.querySelector<HTMLButtonElement>('[data-custom-picker-endpoint="end"]')?.click());

    expect(document.querySelector('[data-custom-calendar-month]')?.getAttribute('data-custom-calendar-month')).toBe('2026-07');
    expect(document.querySelectorAll('[data-custom-calendar-cell]')).toHaveLength(42);
    expect(document.querySelector('[data-custom-calendar-cell="2026-06-30"]')?.hasAttribute('data-custom-outside-month')).toBe(true);
    expect(document.querySelector('[data-custom-calendar-cell="2026-06-30"]')?.hasAttribute('data-custom-in-range')).toBe(true);
  });

  it('announces full dates and selected range states for the custom calendar', async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-07-17T07:36:42.000Z'));
    await renderControl('custom', vi.fn(), {
      unit: 'day',
      start: '2026-06-30',
      end: '2026-07-10',
    });
    const trigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    await act(async () => trigger?.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-custom-endpoint="start"]')?.click());

    const start = document.querySelector<HTMLButtonElement>('[data-custom-calendar-cell="2026-06-30"]');
    const middle = document.querySelector<HTMLButtonElement>('[data-custom-calendar-cell="2026-07-01"]');
    const end = document.querySelector<HTMLButtonElement>('[data-custom-calendar-cell="2026-07-10"]');

    expect(start?.getAttribute('aria-label')).toBe('Tuesday, June 30, 2026, usage_stats.range_custom_day_start');
    expect(start?.getAttribute('aria-pressed')).toBe('true');
    expect(middle?.getAttribute('aria-label')).toBe('Wednesday, July 1, 2026, usage_stats.range_custom_day_in_range');
    expect(middle?.getAttribute('aria-pressed')).toBe('false');
    expect(end?.getAttribute('aria-label')).toBe('Friday, July 10, 2026, usage_stats.range_custom_day_end');
    expect(end?.getAttribute('aria-pressed')).toBe('true');
  });

  it('syncs a migrated Custom range into an already open popover', async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-07-17T07:36:42.000Z'));
    const onChange = vi.fn();
    await renderControl('8h', onChange, undefined, null);
    const trigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    await act(async () => trigger?.click());

    await renderControl('custom', onChange, {
      unit: 'day',
      start: '2026-06-30',
      end: '2026-07-10',
    }, 'Asia/Shanghai');

    expect(document.querySelector('[data-custom-endpoint="start"] strong')?.textContent).toBe('Jun 30, 2026');
    expect(document.querySelector('[data-custom-endpoint="end"] strong')?.textContent).toBe('Jul 10, 2026');
  });

  it('cancels picker edits back to the summary snapshot without querying', async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-07-17T07:36:42.000Z'));
    const { onChange } = await renderControl('8h');
    const trigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    await act(async () => trigger?.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-time-range-mode="custom"]')?.click());

    const initialStartLabel = document.querySelector('[data-custom-endpoint="start"] strong')?.textContent;
    await act(async () => document.querySelector<HTMLButtonElement>('[data-custom-endpoint="start"]')?.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-custom-day="2026-06-20"]')?.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-custom-picker-cancel]')?.click());

    expect(document.querySelector('[data-custom-range-summary]')).not.toBeNull();
    expect(document.querySelector('[data-custom-endpoint="start"] strong')?.textContent).toBe(initialStartLabel);
    expect(onChange).not.toHaveBeenCalled();
  });

  it('disables Custom until the project timezone is available', async () => {
    await renderControl('8h', vi.fn(), undefined, null);
    const trigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    await act(async () => trigger?.click());

    expect(document.querySelector<HTMLButtonElement>('[data-time-range-mode="custom"]')?.disabled).toBe(true);
  });

  it('uses a Keeper-rendered calendar instead of a native date input', async () => {
    await renderControl('8h');
    const trigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    await act(async () => trigger?.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-time-range-mode="custom"]')?.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-custom-endpoint="start"]')?.click());

    expect(document.querySelector('[data-custom-day-picker]')).not.toBeNull();
    expect(document.querySelectorAll('[data-custom-day]').length).toBeGreaterThan(0);
    expect(document.querySelector('input[type="date"], input[type="time"], input[type="datetime-local"]')).toBeNull();
  });

  it('uses two custom 24-slot hour lists and disables too-short end choices', async () => {
    await renderControl('8h');
    const trigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    await act(async () => trigger?.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-time-range-mode="custom"]')?.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-custom-unit="hour"]')?.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-custom-endpoint="start"]')?.click());

    const startSlots = [...document.querySelectorAll<HTMLButtonElement>('[data-custom-hour-start]')];
    const endSlots = [...document.querySelectorAll<HTMLButtonElement>('[data-custom-hour-end]')];
    expect(startSlots).toHaveLength(24);
    expect(endSlots).toHaveLength(24);
    expect(endSlots.some((slot) => slot.disabled)).toBe(true);
    expect(document.querySelector('input[type="date"], input[type="time"], input[type="datetime-local"]')).toBeNull();
  });

  it('exposes the active Custom endpoint and selected hour slots to assistive technology', async () => {
    await renderControl('8h');
    const trigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    await act(async () => trigger?.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-time-range-mode="custom"]')?.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-custom-unit="hour"]')?.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-custom-endpoint="start"]')?.click());

    expect(document.querySelector('[data-custom-picker-endpoint="start"]')?.getAttribute('aria-pressed')).toBe('true');
    expect(document.querySelector('[data-custom-picker-endpoint="end"]')?.getAttribute('aria-pressed')).toBe('false');
    expect(document.querySelector('[data-custom-hour-start][data-custom-hour-selected]')?.getAttribute('aria-pressed')).toBe('true');
    expect(document.querySelector('[data-custom-hour-end][data-custom-hour-selected]')?.getAttribute('aria-pressed')).toBe('true');
    expect(document.querySelector('[data-custom-hour-start]:not([data-custom-hour-selected])')?.getAttribute('aria-pressed')).toBe('false');
  });

  it('centers both selected hour options when the hour picker opens', async () => {
    vi.spyOn(HTMLElement.prototype, 'offsetTop', 'get').mockImplementation(function () {
      return this.hasAttribute('data-custom-hour-selected') ? 320 : 0;
    });
    vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockImplementation(function () {
      return this.hasAttribute('data-custom-hour-selected') ? 32 : 0;
    });
    vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockImplementation(function () {
      return this.hasAttribute('data-custom-hour-list') ? 160 : 0;
    });

    await renderControl('8h');
    const trigger = document.querySelector<HTMLButtonElement>('[data-time-range-trigger="desktop"]');
    await act(async () => trigger?.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-time-range-mode="custom"]')?.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-custom-unit="hour"]')?.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-custom-endpoint="start"]')?.click());

    const lists = [...document.querySelectorAll<HTMLElement>('[data-custom-hour-list]')];
    expect(lists).toHaveLength(2);
    expect(lists.every((list) => list.querySelector('[data-custom-hour-selected]'))).toBe(true);
    expect(lists.map((list) => list.scrollTop)).toEqual([256, 256]);
  });
});
