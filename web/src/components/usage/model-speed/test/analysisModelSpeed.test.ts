import { describe, expect, it } from 'vitest';
import type { AnalysisModelSpeedPayload } from '@/lib/types';
import { formatModelSpeedTimeRange, mergeModelSpeedBuckets, modelSpeedBucketIndex, modelSpeedMean, modelSpeedTooltipRows } from '../analysisModelSpeed';

const fixture = (): AnalysisModelSpeedPayload => ({
  buckets: Array.from({ length: 9 }, (_, index) => ({ start: `2026-10-${String(index + 1).padStart(2, '0')}T00:00:00+08:00`, end: `2026-10-${String(index + 2).padStart(2, '0')}T00:00:00+08:00` })),
  series: [{ model: 'a', total_tokens: 300, speed_sample_count: 3, samples: Array.from({ length: 9 }, (_, index) => ({ speed_tps_sum: index === 0 ? 100 : index === 1 ? 20 : 0, speed_sample_count: index === 0 ? 1 : index === 1 ? 2 : 0, mean_tps: null })) }],
});

describe('Analysis model speed groups', () => {
  it('merges sums and counts before averaging and preserves empty groups and source bounds', () => {
    const speed = fixture();
    const grouped = mergeModelSpeedBuckets(speed);
    expect(grouped.buckets).toHaveLength(8);
    expect(grouped.buckets[0]).toEqual({ start: speed.buckets[0].start, end: speed.buckets[1].end });
    expect(grouped.buckets.at(-1)?.end).toBe(speed.buckets.at(-1)?.end);
    expect(grouped.series[0].samples[0].mean_tps).toBeCloseTo(40);
    expect(grouped.series[0].samples[1].mean_tps).toBeNull();
    expect(speed.series[0].samples[0].speed_sample_count).toBe(1);
  });

  it('merges the four mobile display groups using sum/count', () => {
    const grouped = mergeModelSpeedBuckets(fixture(), 4);
    expect(grouped.buckets).toHaveLength(4);
    expect(grouped.buckets[0].end).toBe('2026-10-04T00:00:00+08:00');
    expect(grouped.series[0].samples[0].speed_sample_count).toBe(3);
    expect(grouped.series[0].samples[0].mean_tps).toBeCloseTo(40);
  });

  it('uses supplied DST boundaries without assuming a 24-hour natural day', () => {
    const speed = fixture();
    speed.buckets[0] = { start: '2026-03-07T00:00:00-05:00', end: '2026-03-08T00:00:00-05:00' };
    speed.buckets[1] = { start: '2026-03-08T00:00:00-05:00', end: '2026-03-09T00:00:00-04:00' };
    const grouped = mergeModelSpeedBuckets(speed);
    expect(grouped.buckets[0].end).toBe('2026-03-09T00:00:00-04:00');
    expect(Date.parse(grouped.buckets[0].end) - Date.parse(grouped.buckets[0].start)).toBe(47 * 3600_000);
  });

  it('returns every selected model, sorts valid speeds and preserves null for missing data', () => {
    const speed = mergeModelSpeedBuckets(fixture());
    speed.series.push({ model: 'b', total_tokens: 1000, speed_sample_count: 1, samples: [{ speed_tps_sum: 50, speed_sample_count: 1, mean_tps: 999 }] });
    const rows = modelSpeedTooltipRows(speed, ['a', 'missing', 'b'], 0);
    expect(rows.map((row) => row.model)).toEqual(['b', 'a', 'missing']);
    expect(rows[0].mean).toBeCloseTo(50);
    expect(rows[2]).toEqual({ model: 'missing', mean: null, count: 0 });
    expect(modelSpeedMean({ speed_tps_sum: 0, speed_sample_count: 0 })).toBeNull();
    expect(modelSpeedBucketIndex(-0.4, 8)).toBe(0);
    expect(modelSpeedBucketIndex(2.3, 8)).toBe(2);
    expect(modelSpeedBucketIndex(99, 8)).toBe(7);
  });

  it('keeps same-day half ranges to time only even when the UTC date changes', () => {
    expect(formatModelSpeedTimeRange('2026-10-08T23:55:00Z', '2026-10-09T00:05:00Z', 'Asia/Shanghai', 'en'))
      .toBe('07:55:00 – 08:05:00');
  });

  it('adds short month and day only when the half range crosses midnight in the response timezone', () => {
    const range = formatModelSpeedTimeRange('2026-10-08T15:55:00Z', '2026-10-08T16:05:00Z', 'Asia/Shanghai', 'en');
    expect(range).toContain('10/08');
    expect(range).toContain('10/09');
    expect(range).toContain('23:55:00');
    expect(range).toContain('00:05:00');
    expect(range).not.toContain('2026');
  });

  it('uses minute precision for hourly Analysis ranges while retaining short dates across midnight', () => {
    expect(formatModelSpeedTimeRange('2026-10-08T23:55:00Z', '2026-10-09T00:05:00Z', 'Asia/Shanghai', 'en', 'minutes'))
      .toBe('07:55 – 08:05');
    const range = formatModelSpeedTimeRange('2026-10-08T15:55:00Z', '2026-10-08T16:05:00Z', 'Asia/Shanghai', 'en', 'minutes');
    expect(range).toContain('10/08');
    expect(range).toContain('10/09');
    expect(range).toContain('23:55');
    expect(range).toContain('00:05');
    expect(range).not.toContain('2026');
    expect(range).not.toContain(':00');
  });
  it('uses the complete chart context to distinguish repeated tooltip endpoints', () => {
    const first = '2026-11-01T01:00:00-07:00';
    const second = '2026-11-01T01:00:00-08:00';
    expect(formatModelSpeedTimeRange(first, second, 'America/Los_Angeles', 'en', 'minutes'))
      .toBe('01:00 UTC-07:00 – 01:00 UTC-08:00');
    expect(formatModelSpeedTimeRange(second, '2026-11-01T02:00:00-08:00', 'America/Los_Angeles', 'en', 'minutes', [first]))
      .toBe('01:00 UTC-08:00 – 02:00');
  });

});
