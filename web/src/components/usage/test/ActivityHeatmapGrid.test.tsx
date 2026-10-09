// @vitest-environment happy-dom

import { act, type ComponentProps } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { ActivityHeatmapGrid } from '../ActivityHeatmapGrid';
import { buildUsageActivityFixture } from './activityFixtures';

describe('ActivityHeatmapGrid', () => {
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

  const renderGrid = (props: Partial<ComponentProps<typeof ActivityHeatmapGrid>> = {}) => {
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
        {...props}
      />,
    ));
  };

  it('renders exact column-first ARIA coordinates and follows them with one Tab stop', () => {
    renderGrid();
    const grid = container.querySelector('[role="grid"]')!;
    expect(grid.getAttribute('aria-rowcount')).toBe('7');
    expect(grid.getAttribute('aria-colcount')).toBe('52');
    const cells = Array.from(container.querySelectorAll<HTMLElement>('[role="gridcell"]'));
    expect(cells).toHaveLength(7 * 52);
    expect(cells.filter((cell) => cell.tabIndex === 0)).toHaveLength(1);
    for (const [index, row, column] of [[0, 1, 1], [6, 7, 1], [7, 1, 2]]) {
      expect(cells[index].getAttribute('aria-rowindex')).toBe(String(row));
      expect(cells[index].getAttribute('aria-colindex')).toBe(String(column));
      expect(cells[index].getAttribute('data-activity-index')).toBe(String(index));
    }

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

  it('reuses the fixed grid cells when a new window replaces their timestamps and values', () => {
    const firstBlocks = buildUsageActivityFixture([100]).blocks;
    const nextBlocks = firstBlocks.map((block, index) => ({
      ...block,
      start_time: new Date(Date.parse(block.start_time) + 86_400_000).toISOString(),
      end_time: new Date(Date.parse(block.end_time) + 86_400_000).toISOString(),
      total_tokens: index + 1,
    }));

    renderGrid({ blocks: firstBlocks, requestIdentity: 'admin::day:::' });
    const firstCell = container.querySelector<HTMLElement>('[role="gridcell"]')!;

    renderGrid({ blocks: nextBlocks, requestIdentity: 'admin::week:::' });

    expect(container.querySelector<HTMLElement>('[role="gridcell"]')).toBe(firstCell);
    expect(firstCell.getAttribute('data-activity-start')).toBe(nextBlocks[0].start_time);
    expect(firstCell.getAttribute('aria-label')).toContain(': Total 1');
  });
});
