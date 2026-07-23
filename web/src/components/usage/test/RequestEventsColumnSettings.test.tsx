// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { UsageEvent } from '@/lib/types';
import {
  REQUEST_EVENT_COLUMN_IDS,
  RequestEventsDetailsCard,
  type RequestEventColumnId,
} from '../RequestEventsDetailsCard';
import { moveRequestEventColumnId } from '../requestEventColumns';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const baseProps: React.ComponentProps<typeof RequestEventsDetailsCard> = {
  events: [],
  loading: false,
  page: 1,
  pageSize: 20,
  pageSizeOptions: [20, 50, 100],
  totalCount: 0,
  totalPages: 0,
  modelOptions: [],
  sourceOptions: [],
  modelFilter: '__all__',
  sourceFilter: '__all__',
  resultFilter: '__all__',
  onPageChange: () => undefined,
  onPageSizeChange: () => undefined,
  onModelFilterChange: () => undefined,
  onSourceFilterChange: () => undefined,
  onResultFilterChange: () => undefined,
};

const customOrder: RequestEventColumnId[] = [
  'model',
  'timestamp',
  ...REQUEST_EVENT_COLUMN_IDS.filter((columnId) => columnId !== 'model' && columnId !== 'timestamp'),
];

const event: UsageEvent = {
  id: '101',
  timestamp: '2026-04-23T02:00:00.000Z',
  model: 'claude-sonnet',
  source: 'Provider A',
  source_raw: 'source-a',
  auth_index: '1',
  failed: false,
  tokens: { total_tokens: 0 },
};

describe('RequestEvents column settings', () => {
  let container: HTMLDivElement;
  let root: Root;
  let restoreElementAnimate = () => undefined;

  beforeEach(() => {
    vi.spyOn(window, 'scrollTo').mockImplementation(() => undefined);
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    restoreElementAnimate();
    restoreElementAnimate = () => undefined;
    document.body.replaceChildren();
    vi.restoreAllMocks();
  });

  const mockElementAnimate = () => {
    const originalDescriptor = Object.getOwnPropertyDescriptor(Element.prototype, 'animate');
    const animateSpy = vi.fn(() => ({ cancel: vi.fn() } as unknown as Animation));
    Object.defineProperty(Element.prototype, 'animate', {
      configurable: true,
      writable: true,
      value: animateSpy,
    });
    restoreElementAnimate = () => {
      if (originalDescriptor) {
        Object.defineProperty(Element.prototype, 'animate', originalDescriptor);
      } else {
        delete (Element.prototype as { animate?: Element['animate'] }).animate;
      }
    };
    return animateSpy;
  };

  const renderCard = async ({
    onVisibleColumnIdsChange = vi.fn(),
    onColumnOrderChange = vi.fn(),
    visibleColumnIds = ['model', 'timestamp'],
    columnOrder = customOrder,
    events = [],
  }: {
    onVisibleColumnIdsChange?: ReturnType<typeof vi.fn>;
    onColumnOrderChange?: ReturnType<typeof vi.fn>;
    visibleColumnIds?: RequestEventColumnId[];
    columnOrder?: RequestEventColumnId[];
    events?: UsageEvent[];
  } = {}) => {
    await act(async () => {
      root.render(
        <RequestEventsDetailsCard
          {...baseProps}
          events={events}
          visibleColumnIds={visibleColumnIds}
          columnOrder={columnOrder}
          onVisibleColumnIdsChange={onVisibleColumnIdsChange}
          onColumnOrderChange={onColumnOrderChange}
        />,
      );
    });
    return { onVisibleColumnIdsChange, onColumnOrderChange };
  };

  const openSettings = async () => {
    const trigger = document.querySelector<HTMLButtonElement>('[data-request-events-column-settings-trigger]');
    expect(trigger).not.toBeNull();
    await act(async () => {
      trigger?.click();
      await Promise.resolve();
    });
  };

  const dispatchPointer = (
    element: HTMLElement | null,
    type: string,
    {
      pointerId = 1,
      clientX = 12,
      clientY = 12,
      button = 0,
      isPrimary = true,
    }: {
      pointerId?: number;
      clientX?: number;
      clientY?: number;
      button?: number;
      isPrimary?: boolean;
    } = {},
  ) => {
    const pointerEvent = new Event(type, { bubbles: true, cancelable: true });
    Object.defineProperties(pointerEvent, {
      pointerId: { value: pointerId },
      clientX: { value: clientX },
      clientY: { value: clientY },
      button: { value: button },
      isPrimary: { value: isPrimary },
    });
    element?.dispatchEvent(pointerEvent);
  };

  const mockColumnRowRect = (row: HTMLElement, top: number) => {
    row.getBoundingClientRect = () => ({
      top,
      right: 560,
      bottom: top + 46,
      left: 0,
      width: 560,
      height: 46,
      x: 0,
      y: top,
      toJSON: () => ({}),
    });
  };

  const mockColumnRowsInCurrentOrder = () => {
    const rows = [...document.querySelectorAll<HTMLElement>('[data-request-events-column-row]')];
    for (const row of rows) {
      row.getBoundingClientRect = () => {
        const currentRows = [...document.querySelectorAll('[data-request-events-column-row]')];
        const top = currentRows.indexOf(row) * 52;
        return {
          top,
          right: 560,
          bottom: top + 46,
          left: 0,
          width: 560,
          height: 46,
          x: 0,
          y: top,
          toJSON: () => ({}),
        };
      };
    }
    return rows;
  };

  it('reuses the current order when the drag target has not changed', () => {
    expect(moveRequestEventColumnId(customOrder, 'model', 0)).toBe(customOrder);
  });

  it('applies visibility and the full order together after moving an unchecked column', async () => {
    const { onVisibleColumnIdsChange, onColumnOrderChange } = await renderCard();
    await openSettings();

    const modelVisibility = document.querySelector<HTMLInputElement>('[data-request-events-column-visibility="model"]');
    expect(modelVisibility?.checked).toBe(true);
    await act(async () => modelVisibility?.click());

    const modelHandle = document.querySelector<HTMLButtonElement>('[data-request-events-column-drag-handle="model"]');
    expect(modelHandle?.querySelector('svg')).not.toBeNull();
    await act(async () => modelHandle?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true })));

    expect(onVisibleColumnIdsChange).not.toHaveBeenCalled();
    expect(onColumnOrderChange).not.toHaveBeenCalled();
    expect(document.querySelector('[data-request-events-column-row]')?.getAttribute('data-request-events-column-row')).toBe('timestamp');

    await act(async () => document.querySelector<HTMLButtonElement>('[data-request-events-column-settings-apply]')?.click());

    expect(onVisibleColumnIdsChange).toHaveBeenCalledWith(['timestamp']);
    expect(onColumnOrderChange).toHaveBeenCalledWith([
      'timestamp',
      'model',
      ...REQUEST_EVENT_COLUMN_IDS.filter((columnId) => columnId !== 'model' && columnId !== 'timestamp'),
    ]);
  });

  it('renders visible table columns in the saved full-list order', async () => {
    await renderCard({ events: [event] });

    expect([...document.querySelectorAll('th')].map((header) => header.textContent)).toEqual(['Model', 'Timestamp']);
  });

  it('discards draft visibility and order changes when cancelled', async () => {
    const { onVisibleColumnIdsChange, onColumnOrderChange } = await renderCard();
    await openSettings();

    await act(async () => document.querySelector<HTMLInputElement>('[data-request-events-column-visibility="model"]')?.click());
    await act(async () => document.querySelector<HTMLButtonElement>('[data-request-events-column-drag-handle="model"]')?.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }),
    ));
    await act(async () => document.querySelector<HTMLButtonElement>('[data-request-events-column-settings-cancel]')?.click());

    expect(onVisibleColumnIdsChange).not.toHaveBeenCalled();
    expect(onColumnOrderChange).not.toHaveBeenCalled();

    await openSettings();
    expect(document.querySelector<HTMLInputElement>('[data-request-events-column-visibility="model"]')?.checked).toBe(true);
    expect(document.querySelector('[data-request-events-column-row]')?.getAttribute('data-request-events-column-row')).toBe('model');
  });

  it('keeps an unchecked column in the order changed by pointer dragging', async () => {
    const { onColumnOrderChange } = await renderCard();
    await openSettings();

    await act(async () => document.querySelector<HTMLInputElement>('[data-request-events-column-visibility="model"]')?.click());
    const modelHandle = document.querySelector<HTMLButtonElement>('[data-request-events-column-drag-handle="model"]');
    const timestampRow = document.querySelector<HTMLElement>('[data-request-events-column-row="timestamp"]');
    expect(modelHandle).not.toBeNull();
    expect(timestampRow).not.toBeNull();
    vi.spyOn(document, 'elementFromPoint').mockReturnValue(timestampRow);

    await act(async () => {
      dispatchPointer(modelHandle, 'pointerdown');
      dispatchPointer(modelHandle, 'pointermove');
      dispatchPointer(modelHandle, 'pointerup');
    });
    await act(async () => document.querySelector<HTMLButtonElement>('[data-request-events-column-settings-apply]')?.click());

    expect(onColumnOrderChange).toHaveBeenCalledWith([
      'timestamp',
      'model',
      ...REQUEST_EVENT_COLUMN_IDS.filter((columnId) => columnId !== 'model' && columnId !== 'timestamp'),
    ]);
  });

  it('keeps one pointer drag active across consecutive row moves', async () => {
    await renderCard();
    await openSettings();

    const modelHandle = document.querySelector<HTMLButtonElement>('[data-request-events-column-drag-handle="model"]');
    const timestampRow = document.querySelector<HTMLElement>('[data-request-events-column-row="timestamp"]');
    const apiKeyRow = document.querySelector<HTMLElement>('[data-request-events-column-row="api_key"]');
    const settingsList = document.querySelector<HTMLElement>('[data-request-events-column-row]')?.parentElement ?? null;
    expect(modelHandle).not.toBeNull();
    expect(settingsList).not.toBeNull();

    let captureOwner: HTMLElement | null = null;
    vi.spyOn(modelHandle!, 'setPointerCapture').mockImplementation(() => {
      captureOwner = modelHandle;
    });
    vi.spyOn(settingsList!, 'setPointerCapture').mockImplementation(() => {
      captureOwner = settingsList;
    });
    vi.spyOn(document, 'elementFromPoint')
      .mockReturnValueOnce(timestampRow)
      .mockReturnValueOnce(apiKeyRow);

    await act(async () => {
      dispatchPointer(modelHandle, 'pointerdown', { clientY: 100 });
      dispatchPointer(modelHandle, 'pointermove', { clientY: 100 });
    });

    await act(async () => {
      // 浏览器在被 capture 的拖柄随父行重排时会丢失 capture；稳定容器则不会。
      if (captureOwner === modelHandle) {
        dispatchPointer(modelHandle, 'lostpointercapture', { clientY: 100 });
      }
      dispatchPointer(captureOwner, 'pointermove', { clientY: 100 });
    });

    expect([...document.querySelectorAll('[data-request-events-column-row]')].slice(0, 3).map((row) => (
      row.getAttribute('data-request-events-column-row')
    ))).toEqual(['timestamp', 'api_key', 'model']);
  });

  it('ignores pointer events that do not belong to the active drag', async () => {
    await renderCard();
    await openSettings();

    const modelHandle = document.querySelector<HTMLButtonElement>('[data-request-events-column-drag-handle="model"]');
    const timestampRow = document.querySelector<HTMLElement>('[data-request-events-column-row="timestamp"]');
    const settingsList = document.querySelector<HTMLElement>('[data-request-events-column-settings-list]');
    expect(modelHandle).not.toBeNull();
    expect(timestampRow).not.toBeNull();
    expect(settingsList).not.toBeNull();
    vi.spyOn(document, 'elementFromPoint').mockReturnValue(timestampRow);

    await act(async () => {
      dispatchPointer(modelHandle, 'pointerdown', { pointerId: 1, clientY: 10 });
      dispatchPointer(settingsList, 'pointerup', {
        pointerId: 2,
        clientY: 90,
        isPrimary: false,
      });
    });

    expect(document.querySelector('[data-request-events-column-row]')?.getAttribute('data-request-events-column-row')).toBe('model');
    expect(document.querySelector('[data-request-events-column-row="model"]')?.className).toContain('Dragging');

    await act(async () => dispatchPointer(settingsList, 'pointercancel', { pointerId: 1, clientY: 10 }));
  });

  it('uses the nearest row when a fast drop lands in a row gap', async () => {
    await renderCard();
    await openSettings();

    const modelHandle = document.querySelector<HTMLButtonElement>('[data-request-events-column-drag-handle="model"]');
    mockColumnRowsInCurrentOrder();
    expect(modelHandle).not.toBeNull();
    vi.spyOn(document, 'elementFromPoint').mockReturnValue(null);

    await act(async () => {
      dispatchPointer(modelHandle, 'pointerdown', { clientY: 10 });
      dispatchPointer(modelHandle, 'pointerup', { clientY: 101 });
    });

    expect([...document.querySelectorAll('[data-request-events-column-row]')].slice(0, 2).map((row) => (
      row.getAttribute('data-request-events-column-row')
    ))).toEqual(['timestamp', 'model']);
  });

  it('appends the dragged row when a fast drop lands below the final row', async () => {
    await renderCard();
    await openSettings();

    const modelHandle = document.querySelector<HTMLButtonElement>('[data-request-events-column-drag-handle="model"]');
    const rows = mockColumnRowsInCurrentOrder();
    expect(modelHandle).not.toBeNull();
    vi.spyOn(document, 'elementFromPoint').mockReturnValue(null);

    await act(async () => {
      dispatchPointer(modelHandle, 'pointerdown', { clientY: 10 });
      dispatchPointer(modelHandle, 'pointerup', { clientY: rows.length * 52 + 20 });
    });

    const reorderedRows = [...document.querySelectorAll('[data-request-events-column-row]')];
    expect(reorderedRows.at(-1)?.getAttribute('data-request-events-column-row')).toBe('model');
  });

  it('moves past skipped rows when dropped on the near half of a lower row', async () => {
    await renderCard();
    await openSettings();

    const modelHandle = document.querySelector<HTMLButtonElement>('[data-request-events-column-drag-handle="model"]');
    const apiKeyRow = document.querySelector<HTMLElement>('[data-request-events-column-row="api_key"]');
    expect(modelHandle).not.toBeNull();
    expect(apiKeyRow).not.toBeNull();
    mockColumnRowRect(apiKeyRow!, 104);
    vi.spyOn(document, 'elementFromPoint').mockReturnValue(apiKeyRow);

    await act(async () => {
      dispatchPointer(modelHandle, 'pointerdown', { clientY: 10 });
      dispatchPointer(modelHandle, 'pointerup', { clientY: 110 });
    });

    expect([...document.querySelectorAll('[data-request-events-column-row]')].slice(0, 3).map((row) => (
      row.getAttribute('data-request-events-column-row')
    ))).toEqual(['timestamp', 'model', 'api_key']);
  });

  it('moves past skipped rows when dropped on the near half of a higher row', async () => {
    await renderCard();
    await openSettings();

    const apiKeyHandle = document.querySelector<HTMLButtonElement>('[data-request-events-column-drag-handle="api_key"]');
    const apiKeyRow = document.querySelector<HTMLElement>('[data-request-events-column-row="api_key"]');
    const modelRow = document.querySelector<HTMLElement>('[data-request-events-column-row="model"]');
    expect(apiKeyHandle).not.toBeNull();
    expect(apiKeyRow).not.toBeNull();
    expect(modelRow).not.toBeNull();
    mockColumnRowRect(apiKeyRow!, 104);
    mockColumnRowRect(modelRow!, 0);
    vi.spyOn(document, 'elementFromPoint').mockReturnValue(modelRow);

    await act(async () => {
      dispatchPointer(apiKeyHandle, 'pointerdown', { clientY: 110 });
      dispatchPointer(apiKeyHandle, 'pointerup', { clientY: 40 });
    });

    expect([...document.querySelectorAll('[data-request-events-column-row]')].slice(0, 3).map((row) => (
      row.getAttribute('data-request-events-column-row')
    ))).toEqual(['model', 'api_key', 'timestamp']);
  });

  it('moves the dragged row with the latest pointer position in one animation frame', async () => {
    await renderCard();
    await openSettings();

    const modelHandle = document.querySelector<HTMLButtonElement>('[data-request-events-column-drag-handle="model"]');
    const modelRow = document.querySelector<HTMLElement>('[data-request-events-column-row="model"]');
    const modalBody = document.querySelector<HTMLElement>('.modal-body');
    expect(modelHandle).not.toBeNull();
    expect(modelRow).not.toBeNull();
    expect(modalBody).not.toBeNull();

    modelRow!.getBoundingClientRect = () => ({
      top: 100,
      right: 560,
      bottom: 146,
      left: 0,
      width: 560,
      height: 46,
      x: 0,
      y: 100,
      toJSON: () => ({}),
    });
    modalBody!.getBoundingClientRect = () => ({
      top: 0,
      right: 560,
      bottom: 1000,
      left: 0,
      width: 560,
      height: 1000,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    });
    vi.spyOn(document, 'elementFromPoint').mockReturnValue(null);
    let animationFrame: FrameRequestCallback | null = null;
    const requestAnimationFrameSpy = vi.spyOn(window, 'requestAnimationFrame').mockImplementation((callback) => {
      animationFrame = callback;
      return 23;
    });

    await act(async () => {
      dispatchPointer(modelHandle, 'pointerdown', { clientY: 110 });
      dispatchPointer(modelHandle, 'pointermove', { clientY: 150 });
      dispatchPointer(modelHandle, 'pointermove', { clientY: 175 });
      dispatchPointer(modelHandle, 'pointermove', { clientY: 190 });
    });

    expect(requestAnimationFrameSpy).toHaveBeenCalledTimes(1);
    await act(async () => animationFrame?.(0));
    expect(modelRow?.style.transform).toBe('translate3d(0, 80px, 0)');

    await act(async () => dispatchPointer(modelHandle, 'pointerup', { clientY: 190 }));
  });

  it('keeps the dragged row under the pointer when the modal body scrolls', async () => {
    await renderCard();
    await openSettings();

    const modelHandle = document.querySelector<HTMLButtonElement>('[data-request-events-column-drag-handle="model"]');
    const modelRow = document.querySelector<HTMLElement>('[data-request-events-column-row="model"]');
    const modalBody = document.querySelector<HTMLElement>('.modal-body');
    expect(modelHandle).not.toBeNull();
    expect(modelRow).not.toBeNull();
    expect(modalBody).not.toBeNull();

    modelRow!.getBoundingClientRect = () => {
      const translateY = Number(/translate3d\(0, (-?[\d.]+)px, 0\)/.exec(modelRow!.style.transform)?.[1] ?? 0);
      const top = 100 - modalBody!.scrollTop + translateY;
      return {
        top,
        right: 560,
        bottom: top + 46,
        left: 0,
        width: 560,
        height: 46,
        x: 0,
        y: top,
        toJSON: () => ({}),
      };
    };
    Object.defineProperties(modalBody, {
      clientHeight: { configurable: true, value: 400 },
      scrollHeight: { configurable: true, value: 1200 },
    });
    modalBody!.getBoundingClientRect = () => ({
      top: 0,
      right: 560,
      bottom: 1000,
      left: 0,
      width: 560,
      height: 1000,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    });
    vi.spyOn(document, 'elementFromPoint').mockReturnValue(null);
    let animationFrame: FrameRequestCallback | null = null;
    const requestAnimationFrameSpy = vi.spyOn(window, 'requestAnimationFrame').mockImplementation((callback) => {
      animationFrame = callback;
      return requestAnimationFrameSpy.mock.calls.length;
    });

    await act(async () => {
      dispatchPointer(modelHandle, 'pointerdown', { clientY: 110 });
      dispatchPointer(modelHandle, 'pointermove', { clientY: 190 });
    });
    await act(async () => animationFrame?.(0));
    expect(modelRow?.style.transform).toBe('translate3d(0, 80px, 0)');

    animationFrame = null;
    await act(async () => {
      modalBody!.scrollTop = 40;
      modalBody!.dispatchEvent(new Event('scroll'));
    });
    expect(requestAnimationFrameSpy).toHaveBeenCalledTimes(2);
    await act(async () => animationFrame?.(16));
    expect(modelRow?.style.transform).toBe('translate3d(0, 120px, 0)');

    animationFrame = null;
    await act(async () => {
      modalBody!.scrollTop = 10;
      modalBody!.dispatchEvent(new Event('scroll'));
    });
    expect(requestAnimationFrameSpy).toHaveBeenCalledTimes(3);
    await act(async () => animationFrame?.(32));
    expect(modelRow?.style.transform).toBe('translate3d(0, 90px, 0)');

    await act(async () => dispatchPointer(modelHandle, 'pointerup', { clientY: 190 }));
  });

  it('keeps the current order while the pointer stays past the same row midpoint', async () => {
    await renderCard();
    await openSettings();

    const modelHandle = document.querySelector<HTMLButtonElement>('[data-request-events-column-drag-handle="model"]');
    const timestampRow = document.querySelector<HTMLElement>('[data-request-events-column-row="timestamp"]');
    expect(modelHandle).not.toBeNull();
    expect(timestampRow).not.toBeNull();

    timestampRow!.getBoundingClientRect = () => {
      const rows = [...document.querySelectorAll('[data-request-events-column-row]')];
      const top = rows.indexOf(timestampRow!) * 52;
      return {
        top,
        right: 560,
        bottom: top + 46,
        left: 0,
        width: 560,
        height: 46,
        x: 0,
        y: top,
        toJSON: () => ({}),
      };
    };
    vi.spyOn(document, 'elementFromPoint').mockReturnValue(timestampRow);
    const animationFrames: FrameRequestCallback[] = [];
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((callback) => {
      animationFrames.push(callback);
      return animationFrames.length;
    });

    await act(async () => {
      dispatchPointer(modelHandle, 'pointerdown', { clientY: 10 });
      dispatchPointer(modelHandle, 'pointermove', { clientY: 90 });
      animationFrames.shift()?.(0);
    });
    expect([...document.querySelectorAll('[data-request-events-column-row]')].slice(0, 2).map((row) => (
      row.getAttribute('data-request-events-column-row')
    ))).toEqual(['timestamp', 'model']);

    await act(async () => {
      dispatchPointer(modelHandle, 'pointermove', { clientY: 90 });
      animationFrames.shift()?.(16);
    });
    expect([...document.querySelectorAll('[data-request-events-column-row]')].slice(0, 2).map((row) => (
      row.getAttribute('data-request-events-column-row')
    ))).toEqual(['timestamp', 'model']);

    await act(async () => dispatchPointer(modelHandle, 'pointerup', { clientY: 90 }));
  });

  it('animates rows from their previous positions after reordering', async () => {
    const animateSpy = mockElementAnimate();
    const originalGetBoundingClientRect = Element.prototype.getBoundingClientRect;
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function getBoundingClientRect() {
      if (this.hasAttribute('data-request-events-column-row')) {
        const rows = [...document.querySelectorAll('[data-request-events-column-row]')];
        const index = rows.indexOf(this);
        const top = index * 52;
        return {
          top,
          right: 560,
          bottom: top + 46,
          left: 0,
          width: 560,
          height: 46,
          x: 0,
          y: top,
          toJSON: () => ({}),
        };
      }
      return originalGetBoundingClientRect.call(this);
    });

    await renderCard();
    await openSettings();
    expect(animateSpy).not.toHaveBeenCalled();

    const modelHandle = document.querySelector<HTMLButtonElement>('[data-request-events-column-drag-handle="model"]');
    await act(async () => modelHandle?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true })));

    expect(animateSpy).toHaveBeenCalledTimes(2);
    expect(animateSpy.mock.calls.map(([keyframes]) => JSON.stringify(keyframes)).join('\n')).toContain('translate3d');

    for (const animationResult of animateSpy.mock.results.slice(0, 2)) {
      const animation = animationResult.value as Animation;
      animation.onfinish?.call(animation, new Event('finish') as unknown as AnimationPlaybackEvent);
    }
    await act(async () => modelHandle?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true })));
    expect(animateSpy).toHaveBeenCalledTimes(4);
  });

  it('skips row movement animations when reduced motion is requested', async () => {
    const animateSpy = mockElementAnimate();
    vi.spyOn(window, 'matchMedia').mockImplementation((query) => ({
      matches: query === '(prefers-reduced-motion: reduce)',
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }));
    const originalGetBoundingClientRect = Element.prototype.getBoundingClientRect;
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function getBoundingClientRect() {
      if (this.hasAttribute('data-request-events-column-row')) {
        const rows = [...document.querySelectorAll('[data-request-events-column-row]')];
        const index = rows.indexOf(this);
        const top = index * 52;
        return {
          top,
          right: 560,
          bottom: top + 46,
          left: 0,
          width: 560,
          height: 46,
          x: 0,
          y: top,
          toJSON: () => ({}),
        };
      }
      return originalGetBoundingClientRect.call(this);
    });

    await renderCard();
    await openSettings();
    const modelHandle = document.querySelector<HTMLButtonElement>('[data-request-events-column-drag-handle="model"]');
    await act(async () => modelHandle?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true })));

    expect(animateSpy).not.toHaveBeenCalled();
  });

  it('announces keyboard moves with the current column position', async () => {
    await renderCard();
    await openSettings();

    const modelHandle = document.querySelector<HTMLButtonElement>('[data-request-events-column-drag-handle="model"]');
    expect(modelHandle?.getAttribute('aria-label')).toContain(`1 of ${REQUEST_EVENT_COLUMN_IDS.length}`);

    await act(async () => modelHandle?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true })));

    expect(modelHandle?.getAttribute('aria-label')).toContain(`2 of ${REQUEST_EVENT_COLUMN_IDS.length}`);
    expect(document.querySelector('[data-request-events-column-move-announcement]')?.textContent).toContain(
      `Moved Model to position 2 of ${REQUEST_EVENT_COLUMN_IDS.length}`,
    );
  });

  it('keeps the keyboard-moved row visible', async () => {
    await renderCard();
    await openSettings();

    const modelHandle = document.querySelector<HTMLButtonElement>('[data-request-events-column-drag-handle="model"]');
    const modelRow = document.querySelector<HTMLElement>('[data-request-events-column-row="model"]');
    const scrollIntoView = vi.fn();
    expect(modelHandle).not.toBeNull();
    expect(modelRow).not.toBeNull();
    modelRow!.scrollIntoView = scrollIntoView;

    await act(async () => modelHandle?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true })));

    expect(scrollIntoView).toHaveBeenCalledWith({ block: 'nearest' });
  });

  it('auto-scrolls the modal body while dragging near its lower edge', async () => {
    await renderCard();
    await openSettings();

    const modelHandle = document.querySelector<HTMLButtonElement>('[data-request-events-column-drag-handle="model"]');
    const timestampRow = document.querySelector<HTMLElement>('[data-request-events-column-row="timestamp"]');
    const modalBody = document.querySelector<HTMLElement>('.modal-body');
    expect(modelHandle).not.toBeNull();
    expect(timestampRow).not.toBeNull();
    expect(modalBody).not.toBeNull();

    Object.defineProperties(modalBody, {
      clientHeight: { configurable: true, value: 200 },
      scrollHeight: { configurable: true, value: 1200 },
    });
    modalBody!.getBoundingClientRect = () => ({
      top: 0,
      right: 560,
      bottom: 200,
      left: 0,
      width: 560,
      height: 200,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    });
    vi.spyOn(document, 'elementFromPoint').mockReturnValue(timestampRow);
    let animationFrame: FrameRequestCallback | null = null;
    const requestAnimationFrameSpy = vi.spyOn(window, 'requestAnimationFrame').mockImplementation((callback) => {
      animationFrame = callback;
      return 17;
    });

    await act(async () => {
      dispatchPointer(modelHandle, 'pointerdown', { clientY: 190 });
      dispatchPointer(modelHandle, 'pointermove', { clientY: 190 });
    });

    expect(requestAnimationFrameSpy).toHaveBeenCalledTimes(1);
    await act(async () => animationFrame?.(0));
    expect(modalBody?.scrollTop).toBeGreaterThan(0);

    await act(async () => dispatchPointer(modelHandle, 'pointerup', { clientY: 190 }));
  });

  it('pauses edge auto-scroll when the user wheels in the opposite direction', async () => {
    await renderCard();
    await openSettings();

    const modelHandle = document.querySelector<HTMLButtonElement>('[data-request-events-column-drag-handle="model"]');
    const modalBody = document.querySelector<HTMLElement>('.modal-body');
    expect(modelHandle).not.toBeNull();
    expect(modalBody).not.toBeNull();

    Object.defineProperties(modalBody, {
      clientHeight: { configurable: true, value: 200 },
      scrollHeight: { configurable: true, value: 1200 },
    });
    modalBody!.getBoundingClientRect = () => ({
      top: 0,
      right: 560,
      bottom: 200,
      left: 0,
      width: 560,
      height: 200,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    });
    vi.spyOn(document, 'elementFromPoint').mockReturnValue(null);
    const animationFrames: FrameRequestCallback[] = [];
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((callback) => {
      animationFrames.push(callback);
      return animationFrames.length;
    });

    await act(async () => {
      dispatchPointer(modelHandle, 'pointerdown', { clientY: 190 });
      dispatchPointer(modelHandle, 'pointermove', { clientY: 190 });
      animationFrames.shift()?.(0);
    });
    expect(modalBody!.scrollTop).toBeGreaterThan(0);

    const manuallyScrolledTop = modalBody!.scrollTop - 5;
    await act(async () => {
      modalBody!.dispatchEvent(new WheelEvent('wheel', { deltaY: -40, bubbles: true }));
      modalBody!.scrollTop = manuallyScrolledTop;
      modalBody!.dispatchEvent(new Event('scroll'));
      animationFrames.shift()?.(16);
    });
    expect(modalBody!.scrollTop).toBe(manuallyScrolledTop);

    await act(async () => dispatchPointer(modelHandle, 'pointerup', { clientY: 190 }));
  });

  it('starts pointer dragging only for the primary left button', async () => {
    await renderCard();
    await openSettings();

    const modelHandle = document.querySelector<HTMLButtonElement>('[data-request-events-column-drag-handle="model"]');
    const timestampRow = document.querySelector<HTMLElement>('[data-request-events-column-row="timestamp"]');
    vi.spyOn(document, 'elementFromPoint').mockReturnValue(timestampRow);

    await act(async () => {
      dispatchPointer(modelHandle, 'pointerdown', { button: 2 });
      dispatchPointer(modelHandle, 'pointermove', { button: 2 });
      dispatchPointer(modelHandle, 'pointerup', { button: 2 });
    });

    expect(document.querySelector('[data-request-events-column-row]')?.getAttribute('data-request-events-column-row')).toBe('model');
  });

  it('clears pointer dragging when capture is lost', async () => {
    await renderCard();
    await openSettings();

    const modelHandle = document.querySelector<HTMLButtonElement>('[data-request-events-column-drag-handle="model"]');
    const timestampRow = document.querySelector<HTMLElement>('[data-request-events-column-row="timestamp"]');
    const settingsList = document.querySelector<HTMLElement>('[data-request-events-column-row]')?.parentElement ?? null;
    const elementFromPointSpy = vi.spyOn(document, 'elementFromPoint').mockReturnValue(timestampRow);

    await act(async () => {
      dispatchPointer(modelHandle, 'pointerdown');
      dispatchPointer(settingsList, 'lostpointercapture');
      dispatchPointer(modelHandle, 'pointermove');
    });

    expect(elementFromPointSpy).not.toHaveBeenCalled();
    expect(document.querySelector('[data-request-events-column-row]')?.getAttribute('data-request-events-column-row')).toBe('model');
  });

  it('prevents hiding the final visible column', async () => {
    await renderCard({ visibleColumnIds: ['timestamp'] });
    await openSettings();

    expect(document.querySelector<HTMLInputElement>('[data-request-events-column-visibility="timestamp"]')?.disabled).toBe(true);
  });
});
