// @vitest-environment happy-dom

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { ActivityHeatmapGrid } from '../ActivityHeatmapGrid';
import { buildUsageActivityFixture } from './activityFixtures';

describe('ActivityHeatmapGrid tooltip lifecycle', () => {
  let container: HTMLDivElement;
  let scrollContainer: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement('div');
    scrollContainer = document.createElement('div');
    scrollContainer.appendChild(container);
    document.body.appendChild(scrollContainer);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    document.body.replaceChildren();
  });

  const renderGrid = () => {
    const activity = buildUsageActivityFixture();
    act(() => root.render(
      <ActivityHeatmapGrid
        blocks={activity.blocks}
        timeZone={activity.timezone}
        requestIdentity="admin::day:::"
        ariaLabel="Activity grid"
        isIdle={(block) => block.total_tokens === 0}
        getColor={() => '#3b82f6'}
        getSummary={(block) => 'Total ' + block.total_tokens}
        renderTooltipStats={(block) => <span>{block.total_tokens}</span>}
      />,
    ));
    return Array.from(container.querySelectorAll<HTMLElement>('[role="gridcell"]'));
  };

  it('dismisses a mouse tooltip when the page scrolls', () => {
    const cells = renderGrid();

    act(() => cells[0]?.dispatchEvent(new PointerEvent('pointerover', { bubbles: true, pointerType: 'mouse' })));
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();

    act(() => window.dispatchEvent(new Event('scroll')));
    expect(document.querySelector('[role="tooltip"]')).toBeNull();
  });

  it('dismisses a mouse tooltip when an ancestor scroll container moves', () => {
    const cells = renderGrid();

    act(() => cells[0]?.dispatchEvent(new PointerEvent('pointerover', { bubbles: true, pointerType: 'mouse' })));
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();

    act(() => scrollContainer.dispatchEvent(new Event('scroll')));
    expect(document.querySelector('[role="tooltip"]')).toBeNull();
  });

  it('keeps a mouse tooltip positioned when the viewport is resized', () => {
    const cells = renderGrid();

    act(() => cells[0]?.dispatchEvent(new PointerEvent('pointerover', { bubbles: true, pointerType: 'mouse' })));
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();

    act(() => window.dispatchEvent(new Event('resize')));
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();
  });

  it('keeps a focused tooltip available while its anchor is repositioned by scrolling', () => {
    const cells = renderGrid();

    act(() => cells[0]?.focus());
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();

    act(() => window.dispatchEvent(new Event('scroll')));
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();
  });

  it('keeps focus interaction ahead of hover when the pointer enters the focused cell', () => {
    const cells = renderGrid();

    act(() => cells[0]?.focus());
    act(() => cells[0]?.dispatchEvent(new PointerEvent('pointerover', { bubbles: true, pointerType: 'mouse' })));
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();

    act(() => window.dispatchEvent(new Event('scroll')));
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();
  });

  it('restores a focused tooltip after the pointer leaves and re-enters its cell', () => {
    const cells = renderGrid();

    act(() => cells[0]?.focus());
    act(() => cells[0]?.dispatchEvent(new PointerEvent('pointerover', { bubbles: true, pointerType: 'mouse' })));
    act(() => cells[0]?.dispatchEvent(new PointerEvent('pointerout', {
      bubbles: true,
      pointerType: 'mouse',
      relatedTarget: document.body,
    })));
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();

    act(() => cells[0]?.dispatchEvent(new PointerEvent('pointerover', { bubbles: true, pointerType: 'mouse' })));
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();
  });

  it('keeps touch tooltip toggling independent from mouse scroll cleanup', () => {
    const cells = renderGrid();

    act(() => cells[0]?.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, pointerType: 'touch' })));
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();

    act(() => window.dispatchEvent(new Event('scroll')));
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();

    act(() => cells[0]?.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, pointerType: 'touch' })));
    expect(document.querySelector('[role="tooltip"]')).toBeNull();
  });

  it('does not dismiss a mouse tooltip when an unrelated container scrolls', () => {
    const cells = renderGrid();
    const unrelatedContainer = document.createElement('div');
    document.body.appendChild(unrelatedContainer);

    act(() => cells[0]?.dispatchEvent(new PointerEvent('pointerover', { bubbles: true, pointerType: 'mouse' })));
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();

    act(() => unrelatedContainer.dispatchEvent(new Event('scroll')));
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();

    unrelatedContainer.remove();
  });
});
