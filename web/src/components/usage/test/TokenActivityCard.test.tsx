import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import '@/i18n';
import { TokenActivityCard, calculateTokenActivityLevels } from '../TokenActivityCard';
import { buildUsageActivityFixture } from './activityFixtures';

describe('calculateTokenActivityLevels', () => {
  it('keeps zero values idle and maps a single positive block to level five', () => {
    expect(calculateTokenActivityLevels([0, 0, 0])).toEqual([0, 0, 0]);
    expect(calculateTokenActivityLevels([0, 12, 0])).toEqual([0, 5, 0]);
  });

  it('caps the visible distribution at nearest-rank P95 before log scaling', () => {
    const values = [...Array.from({ length: 19 }, (_, index) => index + 1), 1_000_000];
    const levels = calculateTokenActivityLevels(values);

    expect(levels).toHaveLength(values.length);
    expect(levels[18]).toBe(5);
    expect(levels[19]).toBe(5);
    expect(levels.every((level) => level >= 1 && level <= 5)).toBe(true);
  });

  it('spreads positive values across five log-scaled levels between P5 and P95', () => {
    expect(calculateTokenActivityLevels([100_000, 200_000, 400_000, 800_000, 1_600_000]))
      .toEqual([1, 2, 3, 4, 5]);
  });
});

describe('TokenActivityCard', () => {
  it('renders canonical Token totals and the shared 364-cell grid', () => {
    const activity = buildUsageActivityFixture([1_234]);
    const html = renderToStaticMarkup(createElement(TokenActivityCard, {
      activity,
      loading: false,
      requestIdentity: 'admin::day:::',
    }));

    expect(html).toContain('Token Activity');
    expect(html).toContain('1.23K');
    expect(html.match(/role="gridcell"/g)).toHaveLength(7 * 52);
    expect(html).toContain('Total 1,234');
    expect(html).toContain('Input 617');
    expect(html).toContain('Cache Read 185');
  });
});
