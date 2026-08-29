// @vitest-environment happy-dom

import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { ActivityHeatmapGrid } from '../ActivityHeatmapGrid';
import { buildUsageActivityFixture } from './activityFixtures';

describe('ActivityHeatmapGrid keyboard navigation', () => {
  let container: HTMLDivElement;
  let root: ReturnType<typeof createRoot>;

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  it('uses one Tab stop and follows the column-first visual grid with arrow keys', () => {
    const activity = buildUsageActivityFixture();
    act(() => root.render(
      <ActivityHeatmapGrid
        blocks={activity.blocks}
        timeZone={activity.timezone}
        requestIdentity="admin::day:::"
        ariaLabel="Activity grid"
        isIdle={(block) => block.total_tokens === 0}
        getColor={() => undefined}
        getSummary={(block) => `Total ${block.total_tokens}`}
        renderTooltipStats={(block) => <span>{block.total_tokens}</span>}
      />,
    ));

    const cells = Array.from(container.querySelectorAll<HTMLElement>('[role="gridcell"]'));
    expect(cells).toHaveLength(7 * 52);
    expect(cells.filter((cell) => cell.tabIndex === 0)).toHaveLength(1);

    act(() => cells[0].focus());
    act(() => cells[0].dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true })));
    expect(document.activeElement).toBe(cells[7]);

    act(() => cells[7].dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true })));
    expect(document.activeElement).toBe(cells[8]);

    act(() => cells[8].dispatchEvent(new KeyboardEvent('keydown', { key: 'Home', bubbles: true })));
    expect(document.activeElement).toBe(cells[0]);

    act(() => cells[0].dispatchEvent(new KeyboardEvent('keydown', { key: 'End', bubbles: true })));
    expect(document.activeElement).toBe(cells[cells.length - 1]);
  });
});
