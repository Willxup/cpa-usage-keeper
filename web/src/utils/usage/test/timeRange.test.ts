import { describe, expect, it } from 'vitest';
import * as rangeQuery from '../rangeQuery';
import { resolveUsageFilterWindow } from '../../usage';
import { isDailyAverageRange } from '../overview';

type RangeMode = 'hour' | 'day' | 'today' | 'yesterday';
type ParsedRange = { mode: RangeMode; value?: number };

describe('usage rolling time ranges', () => {
  it('normalizes every bounded hour and day range', () => {
    expect(rangeQuery.normalizeUsageRange('5h')).toBe('5h');
    expect(rangeQuery.normalizeUsageRange('13h')).toBe('13h');
    expect(rangeQuery.normalizeUsageRange('24h')).toBe('24h');
    expect(rangeQuery.normalizeUsageRange('1d')).toBe('1d');
    expect(rangeQuery.normalizeUsageRange('17d')).toBe('17d');
    expect(rangeQuery.normalizeUsageRange('30d')).toBe('30d');
  });

  it('rejects out-of-bounds and non-canonical rolling ranges', () => {
    for (const value of ['0h', '1h', '4h', '25h', '0d', '31d', '01h', '1w']) {
      expect(rangeQuery.normalizeUsageRange(value)).toBe('8h');
    }
  });

  it('migrates persisted custom and invalid UI ranges to the default', () => {
    const normalizeSelectable = (rangeQuery as unknown as Record<string, unknown>).normalizeSelectableUsageRange as ((value: unknown) => string) | undefined;
    expect(typeof normalizeSelectable).toBe('function');
    if (!normalizeSelectable) return;

    expect(normalizeSelectable('custom')).toBe('8h');
    expect(normalizeSelectable('17d')).toBe('17d');
    expect(normalizeSelectable('today')).toBe('today');
    expect(normalizeSelectable('invalid')).toBe('8h');
  });

  it('parses the four selector modes and rolling values', () => {
    const parseSelectable = (rangeQuery as unknown as Record<string, unknown>).parseSelectableUsageRange as ((value: string) => ParsedRange) | undefined;
    expect(typeof parseSelectable).toBe('function');
    if (!parseSelectable) return;

    expect(parseSelectable('13h')).toEqual({ mode: 'hour', value: 13 });
    expect(parseSelectable('17d')).toEqual({ mode: 'day', value: 17 });
    expect(parseSelectable('today')).toEqual({ mode: 'today' });
    expect(parseSelectable('yesterday')).toEqual({ mode: 'yesterday' });
  });

  it('builds clamped rolling ranges for slider values', () => {
    const buildRolling = (rangeQuery as unknown as Record<string, unknown>).buildRollingUsageRange as ((unit: 'hour' | 'day', value: number) => string) | undefined;
    expect(typeof buildRolling).toBe('function');
    if (!buildRolling) return;

    expect(buildRolling('hour', 0)).toBe('5h');
    expect(buildRolling('hour', 13)).toBe('13h');
    expect(buildRolling('hour', 25)).toBe('24h');
    expect(buildRolling('day', 0)).toBe('1d');
    expect(buildRolling('day', 17)).toBe('17d');
    expect(buildRolling('day', 31)).toBe('30d');
  });

  it('resolves arbitrary rolling ranges to concrete windows', () => {
    const nowMs = Date.parse('2026-07-16T12:00:00.000Z');

    expect(resolveUsageFilterWindow(null, '13h', { nowMs })).toEqual({
      startMs: nowMs - 13 * 60 * 60 * 1000,
      endMs: nowMs,
      windowMinutes: 13 * 60,
    });
    expect(resolveUsageFilterWindow(null, '17d', { nowMs })).toEqual({
      startMs: nowMs - 17 * 24 * 60 * 60 * 1000,
      endMs: nowMs,
      windowMinutes: 17 * 24 * 60,
    });
  });

  it('keeps 1d visible while using today request and local-window semantics', () => {
    const nowMs = Date.parse('2026-07-16T12:00:00.000Z');
    const todayStart = new Date(nowMs);
    todayStart.setHours(0, 0, 0, 0);

    expect(rangeQuery.parseSelectableUsageRange('1d')).toEqual({ mode: 'day', value: 1 });
    expect(rangeQuery.buildUsageRangeQuery({ range: '1d' })).toEqual({ valid: true, range: 'today' });
    expect(resolveUsageFilterWindow(null, '1d', { nowMs })).toEqual({
      startMs: todayStart.getTime(),
      endMs: nowMs,
      windowMinutes: Math.max((nowMs - todayStart.getTime()) / 60_000, 1),
    });
  });

  it('builds validated custom day and hour requests', () => {
    expect(rangeQuery.buildUsageRangeQuery({
      range: 'custom',
      customUnit: 'day',
      customStart: '2026-06-18',
      customEnd: '2026-07-17',
    })).toEqual({
      valid: true,
      range: 'custom',
      unit: 'day',
      start: '2026-06-18',
      end: '2026-07-17',
    });
    expect(rangeQuery.buildUsageRangeQuery({
      range: 'custom',
      customUnit: 'hour',
      customStart: '2026-07-17T10:00:00+08:00',
      customEnd: '2026-07-17T14:00:00+08:00',
    })).toEqual({
      valid: true,
      range: 'custom',
      unit: 'hour',
      start: '2026-07-17T10:00:00+08:00',
      end: '2026-07-17T14:00:00+08:00',
    });
  });

  it('rejects custom hour requests outside the 5-24 slot length', () => {
    expect(rangeQuery.buildUsageRangeQuery({
      range: 'custom',
      customUnit: 'hour',
      customStart: '2026-07-17T10:00:00+08:00',
      customEnd: '2026-07-17T13:00:00+08:00',
    }).valid).toBe(false);
    expect(rangeQuery.buildUsageRangeQuery({
      range: 'custom',
      customUnit: 'hour',
      customStart: '2026-07-16T14:00:00+08:00',
      customEnd: '2026-07-17T14:00:00+08:00',
    }).valid).toBe(false);
  });

  it('shows daily averages only for day-unit ranges longer than 24 hours', () => {
    expect(isDailyAverageRange({ range: '1d' })).toBe(false);
    expect(isDailyAverageRange({ range: '2d' })).toBe(true);
    expect(isDailyAverageRange({ range: '17d' })).toBe(true);
    expect(isDailyAverageRange({ range: '13h' })).toBe(false);
    expect(isDailyAverageRange({ range: 'custom', customUnit: 'day', customStart: '2026-07-16', customEnd: '2026-07-17' })).toBe(true);
    expect(isDailyAverageRange({ range: 'custom', customUnit: 'day', customStart: '2026-07-17', customEnd: '2026-07-17' })).toBe(false);
    expect(isDailyAverageRange({
      range: 'custom',
      customUnit: 'hour',
      customStart: '2026-07-17T10:00:00+08:00',
      customEnd: '2026-07-17T15:00:00+08:00',
    })).toBe(false);
  });
});
