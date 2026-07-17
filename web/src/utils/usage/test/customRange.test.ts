import { describe, expect, it } from 'vitest';
import {
  buildCustomDaySlots,
  buildCustomHourSlots,
  buildCustomWeekdayLabels,
  buildDefaultCustomRange,
  formatCustomRangeLabel,
  parseStoredUsageRangeState,
  serializeUsageRangeState,
  normalizeCustomRange,
} from '../customRange';

const SHANGHAI_NOW = Date.parse('2026-07-17T07:36:42.000Z');

describe('custom usage range slots', () => {
  it('builds exactly 30 project-timezone calendar days including today', () => {
    const slots = buildCustomDaySlots({ nowMs: SHANGHAI_NOW, timeZone: 'Asia/Shanghai' });

    expect(slots).toHaveLength(30);
    expect(slots[0].value).toBe('2026-06-18');
    expect(slots.at(-1)?.value).toBe('2026-07-17');
  });

  it('localizes the custom calendar weekday headings', () => {
    expect(buildCustomWeekdayLabels('en-US')).toEqual(['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']);
    expect(buildCustomWeekdayLabels('zh-CN')).toEqual(['周日', '周一', '周二', '周三', '周四', '周五', '周六']);
  });

  it('builds exactly 24 project-timezone hour slots including the current hour', () => {
    const slots = buildCustomHourSlots({ nowMs: SHANGHAI_NOW, timeZone: 'Asia/Shanghai' });

    expect(slots).toHaveLength(24);
    expect(slots[0].value).toBe('2026-07-16T16:00:00+08:00');
    expect(slots.at(-1)?.value).toBe('2026-07-17T15:00:00+08:00');
    expect(slots.at(-1)?.current).toBe(true);
  });

  it('keeps repeated DST hours as distinct slots with explicit offsets', () => {
    const slots = buildCustomHourSlots({
      nowMs: Date.parse('2026-11-01T06:30:00.000Z'),
      timeZone: 'America/New_York',
    });
    const repeatedHours = slots.filter((slot) => slot.value.startsWith('2026-11-01T01:00:00'));

    expect(repeatedHours.map((slot) => slot.value)).toEqual([
      '2026-11-01T01:00:00-04:00',
      '2026-11-01T01:00:00-05:00',
    ]);
  });

  it('defaults custom days to seven inclusive days ending today and hours to eight slots', () => {
    expect(buildDefaultCustomRange({ unit: 'day', nowMs: SHANGHAI_NOW, timeZone: 'Asia/Shanghai' })).toEqual({
      unit: 'day',
      start: '2026-07-11',
      end: '2026-07-17',
    });
    expect(buildDefaultCustomRange({ unit: 'hour', nowMs: SHANGHAI_NOW, timeZone: 'Asia/Shanghai' })).toEqual({
      unit: 'hour',
      start: '2026-07-17T08:00:00+08:00',
      end: '2026-07-17T15:00:00+08:00',
    });
  });

  it('replaces stale or too-short persisted ranges with a valid default', () => {
    expect(normalizeCustomRange({
      unit: 'day',
      start: '2026-05-01',
      end: '2026-07-17',
    }, { nowMs: SHANGHAI_NOW, timeZone: 'Asia/Shanghai' })).toEqual({
      unit: 'day',
      start: '2026-07-11',
      end: '2026-07-17',
    });
    expect(normalizeCustomRange({
      unit: 'hour',
      start: '2026-07-17T12:00:00+08:00',
      end: '2026-07-17T15:00:00+08:00',
    }, { nowMs: SHANGHAI_NOW, timeZone: 'Asia/Shanghai' })).toEqual({
      unit: 'hour',
      start: '2026-07-17T08:00:00+08:00',
      end: '2026-07-17T15:00:00+08:00',
    });
  });

  it('formats applied custom ranges in the project timezone', () => {
    expect(formatCustomRangeLabel({ unit: 'day', start: '2026-06-18', end: '2026-07-17' }, {
      locale: 'en-US',
      timeZone: 'Asia/Shanghai',
    })).toBe('Jun 18 – Jul 17');
    expect(formatCustomRangeLabel({
      unit: 'hour',
      start: '2026-07-17T10:00:00+08:00',
      end: '2026-07-17T15:00:00+08:00',
    }, {
      locale: 'en-US',
      timeZone: 'Asia/Shanghai',
    })).toBe('10:00 – 15:00');
  });

  it('migrates legacy stored ranges and round-trips custom state', () => {
    expect(parseStoredUsageRangeState('17d', { nowMs: SHANGHAI_NOW })).toEqual({ range: '17d' });
    const state = {
      range: 'custom' as const,
      customRange: { unit: 'day' as const, start: '2026-06-18', end: '2026-07-17' },
      timeZone: 'Asia/Shanghai',
    };
    expect(parseStoredUsageRangeState(serializeUsageRangeState(state), { nowMs: SHANGHAI_NOW })).toEqual(state);
    expect(parseStoredUsageRangeState('{"range":"custom"}', { nowMs: SHANGHAI_NOW })).toEqual({ range: '8h' });
  });
});
