import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import '@/i18n';
import { OverviewActivityCards } from '../OverviewActivityCards';
import { buildUsageActivityFixture } from './activityFixtures';

describe('OverviewActivityCards', () => {
  it('renders Token Activity before Request Health from one shared payload', () => {
    const activity = buildUsageActivityFixture([100]);
    const html = renderToStaticMarkup(createElement(OverviewActivityCards, {
      activity,
      loading: false,
      requestIdentity: 'admin::day:::',
    }));

    const tokenTitle = html.indexOf('Token Activity');
    expect(tokenTitle).toBeGreaterThanOrEqual(0);
    expect(html.indexOf('Request Health Timeline')).toBeGreaterThan(tokenTitle);
    expect(html.match(/role="grid"/g)).toHaveLength(2);
    expect(html.match(/aria-rowcount="7"/g)).toHaveLength(2);
    expect(html.match(/aria-colcount="52"/g)).toHaveLength(2);
    expect(html.match(/tabindex="0"/g)).toHaveLength(2);
    expect(html.match(/tabindex="-1"/g)).toHaveLength((2 * 7 * 52) - 2);
    expect(html.match(/role="gridcell"/g)).toHaveLength(2 * 7 * 52);
    expect(html.match(new RegExp(`data-activity-start="${activity.blocks[0].start_time}"`, 'g'))).toHaveLength(2);
  });


  it('keeps the current summaries visible during a background refresh', () => {
    const html = renderToStaticMarkup(createElement(OverviewActivityCards, {
      activity: buildUsageActivityFixture([1_234]),
      loading: true,
      requestIdentity: 'admin::day:::',
    }));

    expect(html).toContain('>1.23K</strong>');
    expect(html).toContain('>66.7%</strong>');
  });
});
