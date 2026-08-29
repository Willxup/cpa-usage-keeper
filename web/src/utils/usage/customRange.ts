import type { UsageCustomRange, UsageCustomRangeUnit, UsageTimeRange } from '@/lib/types';
import { isSelectableUsageRange } from './rangeQuery';

const HOUR_MS = 60 * 60 * 1000;
const DAY_MS = 24 * HOUR_MS;
export const DEFAULT_CUSTOM_DAY_RANGE_MAX_DAYS = 365;

interface CustomRangeClockOptions {
  nowMs: number;
  timeZone: string;
  locale?: string;
  maxDayRangeDays?: number;
}

interface BuildDefaultCustomRangeOptions extends CustomRangeClockOptions {
  unit: UsageCustomRangeUnit;
}

export interface StoredUsageRangeState {
  range: UsageTimeRange;
  customRange?: UsageCustomRange;
  timeZone?: string;
}

export const resolveUsageRangeRecoveryTimeZone = (
  state: StoredUsageRangeState,
  projectTimeZone?: string,
): string | undefined => {
  if (state.range !== 'custom') return undefined;
  return projectTimeZone?.trim() || state.timeZone?.trim() || undefined;
};

export interface UsageCustomRangeSlot {
  value: string;
  label: string;
  dateLabel: string;
  current: boolean;
}

interface ZonedParts {
  year: number;
  month: number;
  day: number;
  hour: number;
  minute: number;
  second: number;
}

const getZonedParts = (timestampMs: number, timeZone: string): ZonedParts => {
  const formatter = new Intl.DateTimeFormat('en-CA', {
    timeZone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23',
  });
  const parts = Object.fromEntries(
    formatter.formatToParts(new Date(timestampMs))
      .filter((part) => part.type !== 'literal')
      .map((part) => [part.type, Number(part.value)]),
  );
  return {
    year: parts.year,
    month: parts.month,
    day: parts.day,
    hour: parts.hour,
    minute: parts.minute,
    second: parts.second,
  };
};

const pad2 = (value: number): string => String(value).padStart(2, '0');

const formatDateKey = ({ year, month, day }: Pick<ZonedParts, 'year' | 'month' | 'day'>): string => (
  `${year}-${pad2(month)}-${pad2(day)}`
);

const isValidDateKey = (value: unknown): value is string => {
  if (typeof value !== 'string') return false;
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  if (!match) return false;
  const [, year, month, day] = match.map(Number);
  const date = new Date(Date.UTC(year, month - 1, day));
  return date.getUTCFullYear() === year
    && date.getUTCMonth() === month - 1
    && date.getUTCDate() === day;
};

const formatZonedRFC3339Hour = (timestampMs: number, timeZone: string): string => {
  const parts = getZonedParts(timestampMs, timeZone);
  const representedUTC = Date.UTC(parts.year, parts.month - 1, parts.day, parts.hour, 0, 0, 0);
  const offsetMinutes = Math.round((representedUTC - timestampMs) / 60_000);
  const sign = offsetMinutes >= 0 ? '+' : '-';
  const absoluteOffset = Math.abs(offsetMinutes);
  return `${formatDateKey(parts)}T${pad2(parts.hour)}:00:00${sign}${pad2(Math.floor(absoluteOffset / 60))}:${pad2(absoluteOffset % 60)}`;
};

const formatDayLabel = (dateKey: string, locale?: string): string => {
  const [year, month, day] = dateKey.split('-').map(Number);
  return new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric', timeZone: 'UTC' })
    .format(new Date(Date.UTC(year, month - 1, day)));
};

interface CustomDayBounds {
  firstCalendarMs: number;
  firstDay: string;
  slotCount: number;
  todayCalendarMs: number;
  today: string;
}

const resolveCustomDayRangeMaxDays = (value?: number): number => (
  Number.isInteger(value) && Number(value) > 0 ? Number(value) : DEFAULT_CUSTOM_DAY_RANGE_MAX_DAYS
);

const getCustomDayBounds = ({ nowMs, timeZone, maxDayRangeDays }: CustomRangeClockOptions): CustomDayBounds => {
  const slotCount = resolveCustomDayRangeMaxDays(maxDayRangeDays);
  const todayParts = getZonedParts(nowMs, timeZone);
  const todayCalendarMs = Date.UTC(todayParts.year, todayParts.month - 1, todayParts.day);
  const firstCalendarMs = todayCalendarMs - (slotCount - 1) * DAY_MS;
  const firstDate = new Date(firstCalendarMs);
  return {
    firstCalendarMs,
    firstDay: formatDateKey({
      year: firstDate.getUTCFullYear(),
      month: firstDate.getUTCMonth() + 1,
      day: firstDate.getUTCDate(),
    }),
    slotCount,
    todayCalendarMs,
    today: formatDateKey(todayParts),
  };
};

export const buildCustomWeekdayLabels = (locale?: string): string[] => {
  const formatter = new Intl.DateTimeFormat(locale, { weekday: 'short', timeZone: 'UTC' });
  const sunday = Date.UTC(2024, 0, 7);
  return Array.from({ length: 7 }, (_, index) => formatter.format(new Date(sunday + index * DAY_MS)));
};

export const buildCustomDaySlots = (options: CustomRangeClockOptions): UsageCustomRangeSlot[] => {
  const { locale } = options;
  const { firstCalendarMs, slotCount } = getCustomDayBounds(options);
  const labelFormatter = new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric', timeZone: 'UTC' });
  return Array.from({ length: slotCount }, (_, index) => {
    const date = new Date(firstCalendarMs + index * DAY_MS);
    const value = formatDateKey({ year: date.getUTCFullYear(), month: date.getUTCMonth() + 1, day: date.getUTCDate() });
    return { value, label: labelFormatter.format(date), dateLabel: value, current: index === slotCount - 1 };
  });
};

export const buildCustomHourSlots = ({ nowMs, timeZone, locale }: CustomRangeClockOptions): UsageCustomRangeSlot[] => {
  const current = getZonedParts(nowMs, timeZone);
  const currentHourStartMs = nowMs
    - current.minute * 60_000
    - current.second * 1_000
    - ((nowMs % 1_000) + 1_000) % 1_000;
  return Array.from({ length: 24 }, (_, index) => {
    const timestampMs = currentHourStartMs - (23 - index) * HOUR_MS;
    const parts = getZonedParts(timestampMs, timeZone);
    return {
      value: formatZonedRFC3339Hour(timestampMs, timeZone),
      label: `${pad2(parts.hour)}:00`,
      dateLabel: formatDayLabel(formatDateKey(parts), locale),
      current: index === 23,
    };
  });
};

export const buildDefaultCustomRange = ({ unit, nowMs, timeZone, maxDayRangeDays }: BuildDefaultCustomRangeOptions): UsageCustomRange => {
  if (unit === 'day') {
    const { todayCalendarMs, today } = getCustomDayBounds({ nowMs, timeZone, maxDayRangeDays });
    const startDate = new Date(todayCalendarMs - 6 * DAY_MS);
    return {
      unit,
      start: formatDateKey({
        year: startDate.getUTCFullYear(),
        month: startDate.getUTCMonth() + 1,
        day: startDate.getUTCDate(),
      }),
      end: today,
    };
  }
  const slots = buildCustomHourSlots({ nowMs, timeZone });
  const startIndex = slots.length - 8;
  return { unit, start: slots[startIndex].value, end: slots[slots.length - 1].value };
};

const buildTodayCustomDayRange = (options: CustomRangeClockOptions): UsageCustomRange => {
  const { today } = getCustomDayBounds(options);
  return { unit: 'day', start: today, end: today };
};

export const normalizeCustomRange = (
  range: UsageCustomRange | null | undefined,
  options: CustomRangeClockOptions,
): UsageCustomRange => {
  const unit = range?.unit ?? 'day';
  if (unit === 'day') {
    if (!range) return buildDefaultCustomRange({ unit, ...options });
    return clampCustomRangeToCurrentBounds(range, options);
  }
  const slots = buildCustomHourSlots(options);
  const startIndex = slots.findIndex((slot) => slot.value === range?.start);
  const endIndex = slots.findIndex((slot) => slot.value === range?.end);
  const selectedSlots = endIndex - startIndex + 1;
  const validLength = unit === 'hour' ? selectedSlots >= 5 && selectedSlots <= 24 : selectedSlots >= 1 && selectedSlots <= 30;
  if (startIndex < 0 || endIndex < startIndex || !validLength) {
    return buildDefaultCustomRange({ unit, ...options });
  }
  return { unit, start: slots[startIndex].value, end: slots[endIndex].value };
};

const customRangeSlotTimestamp = (value: string, unit: UsageCustomRangeUnit): number => {
  if (unit === 'hour') return Date.parse(value);
  const [year, month, day] = value.split('-').map(Number);
  return Date.UTC(year, month - 1, day);
};

export const clampCustomRangeToCurrentBounds = (
  range: UsageCustomRange,
  options: CustomRangeClockOptions,
): UsageCustomRange => {
  if (range.unit === 'day') {
    const { firstDay, today } = getCustomDayBounds(options);
    if (!isValidDateKey(range.start) || !isValidDateKey(range.end) || range.start > range.end || range.start > today) {
      return buildTodayCustomDayRange(options);
    }
    if (range.end < firstDay) return buildTodayCustomDayRange(options);
    return {
      unit: range.unit,
      start: range.start < firstDay ? firstDay : range.start,
      end: range.end > today ? today : range.end,
    };
  }
  const slots = buildCustomHourSlots(options);
  const firstSlot = slots[0];
  const lastSlot = slots[slots.length - 1];
  const firstTimestamp = customRangeSlotTimestamp(firstSlot.value, range.unit);
  const lastTimestamp = customRangeSlotTimestamp(lastSlot.value, range.unit);
  const startTimestamp = customRangeSlotTimestamp(range.start, range.unit);
  const endTimestamp = customRangeSlotTimestamp(range.end, range.unit);
  if (![firstTimestamp, lastTimestamp, startTimestamp, endTimestamp].every(Number.isFinite)) {
    return buildDefaultCustomRange({ unit: range.unit, ...options });
  }

  let startIndex = slots.findIndex((slot) => slot.value === range.start);
  if (startIndex < 0) {
    if (startTimestamp < firstTimestamp) startIndex = 0;
    else return buildDefaultCustomRange({ unit: range.unit, ...options });
  }
  let endIndex = slots.findIndex((slot) => slot.value === range.end);
  if (endIndex < 0) {
    if (endTimestamp > lastTimestamp) endIndex = slots.length - 1;
    else return buildDefaultCustomRange({ unit: range.unit, ...options });
  }

  const selectedSlots = endIndex - startIndex + 1;
  const validLength = range.unit === 'hour' ? selectedSlots >= 5 : selectedSlots >= 1;
  if (endIndex < startIndex || !validLength) {
    return buildDefaultCustomRange({ unit: range.unit, ...options });
  }
  return { unit: range.unit, start: slots[startIndex].value, end: slots[endIndex].value };
};

export const clampStoredUsageRangeStateToCurrentBounds = (
  state: StoredUsageRangeState,
  options: CustomRangeClockOptions,
): StoredUsageRangeState => {
  if (state.range !== 'custom' || !state.customRange) return state;
  const customRange = clampCustomRangeToCurrentBounds(state.customRange, options);
  if (customRange.start === state.customRange.start
    && customRange.end === state.customRange.end
    && state.timeZone === options.timeZone) {
    return state;
  }
  return { range: 'custom', customRange, timeZone: options.timeZone };
};

export const formatCustomRangeLabel = (
  range: UsageCustomRange,
  { locale, timeZone }: { locale?: string; timeZone: string },
): string => {
  if (range.unit === 'day') {
    const parseDateKey = (value: string) => {
      const [year, month, day] = value.split('-').map(Number);
      return new Date(Date.UTC(year, month - 1, day));
    };
    const formatter = new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric', timeZone: 'UTC' });
    return `${formatter.format(parseDateKey(range.start))} – ${formatter.format(parseDateKey(range.end))}`;
  }

  const start = new Date(range.start);
  const end = new Date(range.end);
  const formatter = new Intl.DateTimeFormat(locale, {
    timeZone,
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  });
  return `${formatter.format(start)} – ${formatter.format(end)}`;
};

const isCustomRange = (value: unknown): value is UsageCustomRange => {
  if (!value || typeof value !== 'object') return false;
  const candidate = value as Partial<UsageCustomRange>;
  return (candidate.unit === 'hour' || candidate.unit === 'day')
    && typeof candidate.start === 'string'
    && typeof candidate.end === 'string';
};

export const parseLegacyCustomRange = (raw: string | null | undefined): UsageCustomRange | null => {
  const trimmed = raw?.trim();
  if (!trimmed) return null;
  try {
    const parsed = JSON.parse(trimmed) as { start?: unknown; end?: unknown };
    if (!isValidDateKey(parsed.start) || !isValidDateKey(parsed.end) || parsed.start > parsed.end) {
      return null;
    }
    return { unit: 'day', start: parsed.start, end: parsed.end };
  } catch {
    return null;
  }
};

export const parseStoredUsageRangeState = (
  raw: string | null | undefined,
  { nowMs }: { nowMs: number },
): StoredUsageRangeState => {
  const trimmed = raw?.trim();
  if (!trimmed) return { range: 'today' };
  if (!trimmed.startsWith('{')) {
    return { range: isSelectableUsageRange(trimmed) ? trimmed : 'today' };
  }
  try {
    const parsed = JSON.parse(trimmed) as Partial<StoredUsageRangeState>;
    const range = String(parsed.range ?? '');
    if (range !== 'custom') return { range: isSelectableUsageRange(range) ? range : 'today' };
    const timeZone = parsed.timeZone?.trim();
    if (!timeZone || !isCustomRange(parsed.customRange)) return { range: 'today' };
    return {
      range: 'custom',
      customRange: clampCustomRangeToCurrentBounds(parsed.customRange, { nowMs, timeZone }),
      timeZone,
    };
  } catch {
    return { range: 'today' };
  }
};

export const serializeUsageRangeState = (state: StoredUsageRangeState): string => JSON.stringify(state);
