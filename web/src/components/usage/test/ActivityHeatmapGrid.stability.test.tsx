// @vitest-environment happy-dom

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import type { UsageActivityBlock } from '@/lib/types';
import { ActivityHeatmapGrid } from '../ActivityHeatmapGrid';
import { buildUsageActivityFixture } from './activityFixtures';

const gridProps = {
  timeZone: 'UTC',
  ariaLabel: 'Activity',
  isIdle: () => false,
  getColor: () => '#3b82f6',
  getSummary: (block: UsageActivityBlock) => String(block.total_tokens),
  renderTooltipStats: (block: UsageActivityBlock) => <span>{block.total_tokens}</span>,
};

describe('ActivityHeatmapGrid stability', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    document.body.replaceChildren();
  });

  it('reuses the fixed grid cells when a new window replaces their timestamps and values', () => {
    const firstBlocks = buildUsageActivityFixture([100]).blocks;
    const nextBlocks = firstBlocks.map((block, index) => ({
      ...block,
      start_time: new Date(Date.parse(block.start_time) + 86_400_000).toISOString(),
      end_time: new Date(Date.parse(block.end_time) + 86_400_000).toISOString(),
      total_tokens: index + 1,
    }));

    act(() => root.render(
      <ActivityHeatmapGrid {...gridProps} blocks={firstBlocks} requestIdentity="admin::day:::" />,
    ));
    const firstCell = container.querySelector<HTMLElement>('[role="gridcell"]');

    act(() => root.render(
      <ActivityHeatmapGrid {...gridProps} blocks={nextBlocks} requestIdentity="admin::week:::" />,
    ));

    expect(container.querySelector<HTMLElement>('[role="gridcell"]')).toBe(firstCell);
    expect(firstCell?.getAttribute('data-activity-start')).toBe(nextBlocks[0].start_time);
    expect(firstCell?.getAttribute('aria-label')).toContain(': 1');
  });
});
