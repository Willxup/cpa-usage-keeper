import { describe, expect, it } from 'vitest';
import { getCurrentOverviewUsage, getDailyAveragePanelUsage, getOverviewDisplayLoading, isDailyAverageRange } from './overview';

describe('shared usage overview helpers', () => {
  it('keeps loading visible only while the overview has no usage payload', () => {
    expect(getOverviewDisplayLoading({ loading: true, hasUsage: false })).toBe(true);
    expect(getOverviewDisplayLoading({ loading: true, hasUsage: true })).toBe(false);
  });

  it('keeps stale overview responses away from range-scoped panels', () => {
    const usage = { summary: { daily_average_requests: 12 } };

    expect(getCurrentOverviewUsage(usage, '24h::', '7d::')).toBeNull();
    expect(getCurrentOverviewUsage(usage, '7d::', '7d::')).toBe(usage);
  });

  it('returns null before any overview response has been loaded for the current query', () => {
    expect(getCurrentOverviewUsage({ summary: {} }, '7d::', null)).toBeNull();
    expect(getCurrentOverviewUsage(null, '7d::', '7d::')).toBeNull();
  });

  it('identifies ranges that can show daily averages', () => {
    expect(isDailyAverageRange({ range: '7d' })).toBe(true);
    expect(isDailyAverageRange({ range: '30d' })).toBe(true);
    expect(isDailyAverageRange({ range: 'custom', customStart: '2026-06-01', customEnd: '2026-06-02' })).toBe(true);
    expect(isDailyAverageRange({ range: 'custom', customStart: '2026-06-01', customEnd: '2026-06-01' })).toBe(false);
    expect(isDailyAverageRange({ range: 'custom', customStart: '2026-02-31', customEnd: '2026-03-02' })).toBe(false);
    expect(isDailyAverageRange({ range: '24h' })).toBe(false);
    expect(isDailyAverageRange({ range: 'today' })).toBe(false);
    expect(isDailyAverageRange({ range: 'yesterday' })).toBe(false);
  });

  it('keeps the previous usage for the daily average panel while another daily-average range loads', () => {
    const currentUsage = { summary: { daily_average_requests: 30 } };
    const fallbackUsage = { summary: { daily_average_requests: 7 } };

    expect(getDailyAveragePanelUsage(currentUsage, fallbackUsage, true)).toBe(currentUsage);
    expect(getDailyAveragePanelUsage(null, fallbackUsage, true, true)).toBe(fallbackUsage);
    expect(getDailyAveragePanelUsage(null, fallbackUsage, true, false)).toBeNull();
    expect(getDailyAveragePanelUsage(null, fallbackUsage, false, true)).toBeNull();
  });
});
