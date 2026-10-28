import { describe, expect, it } from 'vitest';
import { formatBucketLabel, formatBucketLabels } from '../analysisTimeLabels';

describe('Analysis shared time labels', () => {
  it('uses only the hour in the response timezone for hourly buckets', () => {
    expect(formatBucketLabel('2026-10-08T00:00:00Z', 'hourly', 'Asia/Shanghai')).toBe('08:00');
    expect(formatBucketLabel('2026-10-08T00:00:00Z', 'hourly', 'America/New_York')).toBe('20:00');
    expect(formatBucketLabel('2026-10-08T16:00:00Z', 'hourly', 'Asia/Shanghai')).toBe('00:00');
  });

  it('uses short month/day in the response timezone for daily buckets', () => {
    expect(formatBucketLabel('2026-10-08T16:00:00Z', 'daily', 'Asia/Shanghai')).toBe('10/9');
    expect(formatBucketLabel('2026-10-08T00:00:00Z', 'daily', 'America/New_York')).toBe('10/7');
  });

  it('keeps Shanghai batches identical and tags repeated DST hours', () => {
    const values = ['2026-10-25T02:00:00+02:00', '2026-10-25T02:00:00+01:00'];
    expect(formatBucketLabels(values, 'hourly', 'Europe/Berlin')).toEqual(['02:00 UTC+02:00', '02:00 UTC+01:00']);
    expect(formatBucketLabels(values, 'hourly', 'Asia/Shanghai')).toEqual(values.map(value => formatBucketLabel(value, 'hourly', 'Asia/Shanghai')));
  });

});
