import { createElement } from 'react';
import { readFileSync } from 'node:fs';
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
      requestIdentity: 'admin::24h:::',
    }));

    expect(html.indexOf('Token Activity')).toBeLessThan(html.indexOf('Request Health Timeline'));
    expect(html.match(/role="grid"/g)).toHaveLength(2);
    expect(html.match(/role="gridcell"/g)).toHaveLength(2 * 7 * 52);
    expect(html.match(new RegExp(`data-activity-start="${activity.blocks[0].start_time}"`, 'g'))).toHaveLength(2);
  });

  it('does not own a second Activity request or range state', () => {
    const source = readFileSync(new URL('../OverviewActivityCards.tsx', import.meta.url), 'utf8');

    expect(source).not.toContain('fetchUsageActivity');
    expect(source).not.toContain('fetchKeyActivity');
    expect(source).not.toContain('useUsageActivityData');
    expect(source).not.toContain('useRecentActivityWindow');
  });

  it('keeps the current summaries visible during a background refresh', () => {
    const html = renderToStaticMarkup(createElement(OverviewActivityCards, {
      activity: buildUsageActivityFixture([1_234]),
      loading: true,
      requestIdentity: 'admin::24h:::',
    }));

    expect(html).toContain('>1.23K</strong>');
    expect(html).toContain('>66.7%</strong>');
  });
});
