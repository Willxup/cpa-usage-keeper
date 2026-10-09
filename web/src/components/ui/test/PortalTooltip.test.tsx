// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { describe, expect, it, vi } from 'vitest';
import { PortalTooltip, usePortalTooltip } from '../PortalTooltip';

function TooltipHarness({ lines }: { lines: string[] }) {
  const { tooltip, showOnMouseEnter, hideOnMouseLeave } = usePortalTooltip();
  return (
    <>
      <button
        type="button"
        data-tooltip-anchor="true"
        onMouseEnter={(event) => showOnMouseEnter(lines, event.currentTarget)}
        onMouseLeave={(event) => hideOnMouseLeave(event.currentTarget)}
      >
        anchor
      </button>
      <PortalTooltip tooltip={tooltip} />
    </>
  );
}

describe('PortalTooltip positioning', () => {
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
