// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { describe, expect, it, vi } from 'vitest';
import { PortalTooltip, usePortalTooltip } from '../PortalTooltip';

function TooltipHarness({ lines, trackAnchor = false }: { lines: string[]; trackAnchor?: boolean }) {
  const { tooltip, showOnMouseEnter, hideOnMouseLeave, showOnFocus, hideOnBlur, dismiss } = usePortalTooltip({ trackAnchor });
  return (
    <>
      <button
        type="button"
        data-tooltip-anchor="true"
        onMouseEnter={(event) => showOnMouseEnter(lines, event.currentTarget)}
        onMouseLeave={(event) => hideOnMouseLeave(event.currentTarget)}
        onFocus={(event) => showOnFocus(lines, event.currentTarget)}
        onBlur={(event) => hideOnBlur(event.currentTarget)}
        onKeyDown={(event) => { if (event.key === 'Escape') dismiss(); }}
      >
        anchor
      </button>
      <PortalTooltip tooltip={tooltip} />
    </>
  );
}

describe('PortalTooltip positioning', () => {
  it.each([false, true])('tracks layout-only movement only when opted in (%s), and stops on dismissal/unmount', async (trackAnchor) => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);
    const frames = new Map<number, FrameRequestCallback>();
    let nextFrame = 0;
    const requestFrame = vi.spyOn(window, 'requestAnimationFrame').mockImplementation(callback => {
      frames.set(++nextFrame, callback);
      return nextFrame;
    });
    const cancelFrame = vi.spyOn(window, 'cancelAnimationFrame').mockImplementation(id => { frames.delete(id); });
    let anchorRect = new DOMRect(200, 200, 40, 20);
    const getRect = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function () {
      return this.hasAttribute('data-tooltip-anchor') ? anchorRect : new DOMRect(0, 0, 120, 30);
    });
    const tick = async () => {
      await act(async () => {
        const pending = [...frames.values()];
        frames.clear();
        pending.forEach(callback => callback(0));
      });
    };
    const tooltip = () => document.body.querySelector<HTMLDivElement>('[role="tooltip"]');
    try {
      await act(async () => root.render(<TooltipHarness lines={['Account A reset']} trackAnchor={trackAnchor} />));
      expect(frames.size).toBe(0);
      const anchor = container.querySelector<HTMLButtonElement>('button')!;
      await act(async () => anchor.focus());
      await tick();
      expect(tooltip()?.style.left).toBe('220px');
      expect(tooltip()?.style.top).toBe('230px');
      // 模拟后台重排，只有坐标改变，不派发鼠标、滚动或窗口事件。
      anchorRect = new DOMRect(500, 300, 40, 20);
      await tick();
      expect(document.activeElement).toBe(anchor);
      expect(tooltip()?.textContent).toBe('Account A reset');
      expect(tooltip()?.style.left).toBe(trackAnchor ? '520px' : '220px');
      expect(tooltip()?.style.top).toBe(trackAnchor ? '330px' : '230px');
      await act(async () => anchor.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
      expect(tooltip()).toBeNull();
      expect(frames.size).toBe(0);
      await tick();
      expect(tooltip()).toBeNull();
      await act(async () => { anchor.blur(); anchor.focus(); });
      expect(frames.size).toBe(trackAnchor ? 1 : 0);
      await act(async () => anchor.blur());
      expect(tooltip()).toBeNull();
      expect(frames.size).toBe(0);
      await act(async () => anchor.focus());
    } finally {
      await act(async () => root.unmount());
      container.remove();
      expect(frames.size).toBe(0);
      getRect.mockRestore();
      requestFrame.mockRestore();
      cancelFrame.mockRestore();
    }
  });

  it('moves a multi-line tooltip above the anchor when its measured height does not fit below', async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);
    const anchorRect = new DOMRect(300, 656, 40, 20);
    const tooltipRect = new DOMRect(0, 0, 180, 95);
    const defaultRect = new DOMRect(0, 0, 0, 0);
    const getBoundingClientRect = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect')
      .mockImplementation(function getRect() {
        if (this.getAttribute('data-tooltip-anchor') === 'true') return anchorRect;
        if (this.getAttribute('role') === 'tooltip') return tooltipRect;
        return defaultRect;
      });

    try {
      await act(async () => root.render(
        <TooltipHarness lines={['Total Tokens: 200', 'Input: 100', 'Output: 60', 'Reasoning: 20']} />
      ));

      const anchor = container.querySelector('[data-tooltip-anchor="true"]')!;
      await act(async () => {
        anchor.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
      });

      const tooltip = document.body.querySelector<HTMLDivElement>('[role="tooltip"]')!;
      expect(tooltip.style.top).toBe('646px');
      expect(tooltip.style.transform).toBe('translate(-50%, -100%)');
    } finally {
      getBoundingClientRect.mockRestore();
      await act(async () => root.unmount());
      container.remove();
    }
  });
});
