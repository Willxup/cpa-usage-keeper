import { describe, expect, it } from 'vitest';
import { createZonedPartsReader, disambiguateTimeLabels } from '../timeLabels';

describe('response timezone label identity', () => {
  it('distinguishes repeated clocks without changing order or source values', () => {
    const values = ['2026-11-01T01:00:00-07:00', '2026-11-01T01:00:00-08:00'];
    expect(disambiguateTimeLabels(values, 'America/Los_Angeles', () => '01:00')).toEqual([
      '01:00 UTC-07:00', '01:00 UTC-08:00',
    ]);
    expect(values[0]).toBe('2026-11-01T01:00:00-07:00');
  });

  it('preserves Shanghai labels, including identical clocks on different dates', () => {
    const values = ['2026-11-01T01:00:00+08:00', '2026-11-02T01:00:00+08:00'];
    expect(disambiguateTimeLabels(values, 'Asia/Shanghai', () => '01:00')).toEqual(['01:00', '01:00']);
  });

  it('does not tag different dates even across a timezone transition', () => {
    const values = ['2026-10-31T01:00:00-07:00', '2026-11-01T01:00:00-08:00'];
    expect(disambiguateTimeLabels(values, 'America/Los_Angeles', () => '01:00')).toEqual(['01:00', '01:00']);
  });

  it('handles positive offsets and half-hour folds', () => {
    expect(disambiguateTimeLabels(['2026-10-25T02:00:00+02:00', '2026-10-25T02:00:00+01:00'], 'Europe/Berlin', () => '02:00'))
      .toEqual(['02:00 UTC+02:00', '02:00 UTC+01:00']);
    expect(disambiguateTimeLabels(['2026-04-05T01:30:00+11:00', '2026-04-05T01:30:00+10:30'], 'Australia/Lord_Howe', () => '01:30'))
      .toEqual(['01:30 UTC+11:00', '01:30 UTC+10:30']);
  });

  it('preserves caller fallback and duplicate instant labels', () => {
    const duplicate = ['2026-11-01T08:00:00Z', '2026-11-01T01:00:00-07:00'];
    expect(disambiguateTimeLabels(duplicate, 'America/Los_Angeles', () => '01:00')).toEqual(['01:00', '01:00']);
    expect(disambiguateTimeLabels(['bad'], 'invalid/zone', () => 'fallback')).toEqual(['fallback']);
  });

  it('reads the configured clock independently of the browser timezone', () => {
    expect(createZonedPartsReader('Asia/Kathmandu')(Date.parse('2026-10-25T00:00:00Z')))
      .toEqual({ year: 2026, month: 10, day: 25, hour: 5, minute: 45, second: 0 });
  });

  it('keeps literal fallback labels when the server timezone is unknown', () => {
    const previous = process.env.TZ;
    process.env.TZ = 'America/Los_Angeles';
    try {
      // 浏览器恰逢重复小时，但服务端原字面钟面是上海16点和17点。
      const values = ['2026-11-01T16:00:00+08:00', '2026-11-01T17:00:00+08:00'];
      for (const timezone of [undefined, 'Local', '']) {
        expect(disambiguateTimeLabels(values, timezone, (_, i) => ['16:00', '17:00'][i]))
          .toEqual(['16:00', '17:00']);
      }
    } finally {
      if (previous === undefined) delete process.env.TZ;
      else process.env.TZ = previous;
    }
  });
});
