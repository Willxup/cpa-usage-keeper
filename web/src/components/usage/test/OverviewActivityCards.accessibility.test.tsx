import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import '@/i18n';
import { OverviewActivityCards } from '../OverviewActivityCards';
import { buildUsageActivityFixture } from './activityFixtures';

describe('OverviewActivityCards accessibility', () => {
  it('keeps both dense heatmaps keyboard focusable with complete grid semantics', () => {
    const html = renderToStaticMarkup(createElement(OverviewActivityCards, {
      activity: buildUsageActivityFixture([10]),
      loading: false,
      requestIdentity: 'key::24h:::',
    }));

    expect(html.match(/role="grid"/g)).toHaveLength(2);
    expect(html.match(/aria-rowcount="7"/g)).toHaveLength(2);
    expect(html.match(/aria-colcount="52"/g)).toHaveLength(2);
    expect(html.match(/tabindex="0"/g)).toHaveLength(2 * 7 * 52);
  });
});
