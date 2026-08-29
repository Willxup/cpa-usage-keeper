import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { ActivityHeatmapGrid } from '../ActivityHeatmapGrid';
import { buildUsageActivityFixture } from './activityFixtures';

describe('ActivityHeatmapGrid', () => {
  it('renders the fixed column-first 7 by 52 grid with exact ARIA coordinates', () => {
    const activity = buildUsageActivityFixture();
    const html = renderToStaticMarkup(createElement(ActivityHeatmapGrid, {
      blocks: activity.blocks,
      timeZone: activity.timezone,
      requestIdentity: 'admin::day:::',
      ariaLabel: 'Activity grid',
      isIdle: (block) => block.total_tokens === 0,
      getColor: () => undefined,
      getSummary: (block) => `Total ${block.total_tokens}`,
      renderTooltipStats: (block) => createElement('span', null, block.total_tokens),
    }));

    expect(html).toContain('role="grid"');
    expect(html).toContain('aria-rowcount="7"');
    expect(html).toContain('aria-colcount="52"');
    expect(html.match(/role="gridcell"/g)).toHaveLength(7 * 52);
    expect(html).toContain('data-activity-index="0"');
    expect(html).toMatch(/aria-rowindex="1"[^>]*aria-colindex="1"[^>]*data-activity-index="0"/);
    expect(html).toMatch(/aria-rowindex="7"[^>]*aria-colindex="1"[^>]*data-activity-index="6"/);
    expect(html).toMatch(/aria-rowindex="1"[^>]*aria-colindex="2"[^>]*data-activity-index="7"/);
  });
});
