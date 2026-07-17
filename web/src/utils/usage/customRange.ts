import type { UsageCustomRange, UsageCustomRangeUnit, UsageTimeRange } from '@/lib/types';
import { normalizeSelectableUsageRange, normalizeUsageRange } from './rangeQuery';

const HOUR_MS = 60 * 60 * 1000;
const DAY_MS = 24 * HOUR_MS;

interface CustomRangeClockOptions {
  nowMs: number;
  timeZone: string;
  locale?: string;
}

interface BuildDefaultCustomRangeOptions extends CustomRangeClockOptions {
  unit: UsageCustomRangeUnit;
}

export interface StoredUsageRangeState {
  range: UsageTimeRange;
  customRange?: UsageCustomRange;
  timeZone?: string;
}

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

export const buildCustomWeekdayLabels = (locale?: string): string[] => {
  const formatter = new Intl.DateTimeFormat(locale, { weekday: 'short', timeZone: 'UTC' });
  const sunday = Date.UTC(2024, 0, 7);
  return Array.from({ length: 7 }, (_, index) => formatter.format(new Date(sunday + index * DAY_MS)));
};

export const buildCustomDaySlots = ({ nowMs, timeZone, locale }: CustomRangeClockOptions): UsageCustomRangeSlot[] => {
  const today = getZonedParts(nowMs, timeZone);
  const todayCalendarMs = Date.UTC(today.year, today.month - 1, today.day);
  return Array.from({ length: 30 }, (_, index) => {
    const date = new Date(todayCalendarMs - (29 - index) * DAY_MS);
    const value = formatDateKey({ year: date.getUTCFullYear(), month: date.getUTCMonth() + 1, day: date.getUTCDate() });
    return { value, label: formatDayLabel(value, locale), dateLabel: value, current: index === 29 };
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

export const buildDefaultCustomRange = ({ unit, nowMs, timeZone }: BuildDefaultCustomRangeOptions): UsageCustomRange => {
  const slots = unit === 'hour'
    ? buildCustomHourSlots({ nowMs, timeZone })
    : buildCustomDaySlots({ nowMs, timeZone });
  const startIndex = slots.length - (unit === 'hour' ? 8 : 7);
  return { unit, start: slots[startIndex].value, end: slots[slots.length - 1].value };
};

export const normalizeCustomRange = (
  range: UsageCustomRange | null | undefined,
  options: CustomRangeClockOptions,
): UsageCustomRange => {
  const unit = range?.unit ?? 'day';
  const slots = unit === 'hour' ? buildCustomHourSlots(options) : buildCustomDaySlots(options);
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
  const slots = range.unit === 'hour' ? buildCustomHourSlots(options) : buildCustomDaySlots(options);
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

interface CustomRangeBoundsRefreshDocument {
  visibilityState: DocumentVisibilityState;
  addEventListener: (type: 'visibilitychange', listener: () => void) => void;
  removeEventListener: (type: 'visibilitychange', listener: () => void) => void;
}

interface CustomRangeBoundsRefreshTimerTarget {
  setInterval: (handler: () => void, timeout: number) => number;
  clearInterval: (id: number) => void;
}

export const CUSTOM_RANGE_BOUNDS_REFRESH_INTERVAL_MS = 60_000;

export const scheduleCustomRangeBoundsRefresh = ({
  enabled,
  refreshBounds,
  documentRef,
  timerTarget,
  intervalMs = CUSTOM_RANGE_BOUNDS_REFRESH_INTERVAL_MS,
}: {
  enabled: boolean;
  refreshBounds: () => void;
  documentRef?: CustomRangeBoundsRefreshDocument;
  timerTarget?: CustomRangeBoundsRefreshTimerTarget;
  intervalMs?: number;
}): (() => void) => {
  if (!enabled) return () => undefined;
  const targetDocument = documentRef ?? (typeof document === 'undefined' ? undefined : document);
  const timers = timerTarget ?? (typeof window === 'undefined' ? undefined : {
    setInterval: window.setInterval.bind(window),
    clearInterval: window.clearInterval.bind(window),
  });
  if (!timers) return () => undefined;

  const refreshIfVisible = () => {
    if (targetDocument?.visibilityState === 'hidden') return;
    refreshBounds();
  };
  refreshIfVisible();
  const interval = timers.setInterval(refreshIfVisible, intervalMs);
  targetDocument?.addEventListener('visibilitychange', refreshIfVisible);
  return () => {
    timers.clearInterval(interval);
    targetDocument?.removeEventListener('visibilitychange', refreshIfVisible);
  };
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
  if (!trimmed) return { range: '8h' };
  if (!trimmed.startsWith('{')) {
    return { range: normalizeSelectableUsageRange(trimmed) };
  }
  try {
    const parsed = JSON.parse(trimmed) as Partial<StoredUsageRangeState>;
    const range = normalizeUsageRange(String(parsed.range ?? ''));
    if (range !== 'custom') return { range };
    const timeZone = parsed.timeZone?.trim();
    if (!timeZone || !isCustomRange(parsed.customRange)) return { range: '8h' };
    return {
      range: 'custom',
      customRange: clampCustomRangeToCurrentBounds(parsed.customRange, { nowMs, timeZone }),
      timeZone,
    };
  } catch {
    return { range: '8h' };
  }
};

export const serializeUsageRangeState = (state: StoredUsageRangeState): string => JSON.stringify(state);
