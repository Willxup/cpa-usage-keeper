import type { KeyOverviewTimeRange, UsageCustomRangeUnit, UsageRangeRequest, UsageTimeRange } from '@/lib/types';

export type UsageTimeRangeMode = 'hour' | 'day' | 'today' | 'yesterday' | 'custom';

export interface ParsedSelectableUsageRange {
  mode: UsageTimeRangeMode;
  value?: number;
}

export interface UsageRangeQuery extends UsageRangeRequest {
  valid: boolean;
}

const parseRollingUsageRange = (value: unknown): { unit: 'hour' | 'day'; value: number } | null => {
  if (typeof value !== 'string') return null;
  const match = /^(\d+)([hd])$/.exec(value);
  if (!match) return null;
  const amount = Number(match[1]);
  if (!Number.isInteger(amount) || String(amount) !== match[1]) return null;
  const unit = match[2] === 'h' ? 'hour' : 'day';
  const minimum = unit === 'hour' ? 5 : 1;
  const maximum = unit === 'hour' ? 24 : 30;
  if (amount < minimum || amount > maximum) return null;
  return { unit, value: amount };
};

export const isSelectableUsageRange = (value: unknown): value is KeyOverviewTimeRange => (
  value === 'today' || value === 'yesterday' || parseRollingUsageRange(value) !== null
);

export const normalizeSelectableUsageRange = (value: unknown): KeyOverviewTimeRange => (
  isSelectableUsageRange(value) ? value : '8h'
);

export const normalizeUsageRange = (value: string): UsageTimeRange => (
  value === 'custom' ? value : normalizeSelectableUsageRange(value)
);

// 1d 只保留为选择器显示值；所有查询语义复用已经优化过的 today 路径。
export const resolveUsageRequestRange = <T extends string>(value: T): T | 'today' => (
  value === '1d' ? 'today' : value
);

export const parseSelectableUsageRange = (value: string): ParsedSelectableUsageRange => {
  const normalized = normalizeSelectableUsageRange(value);
  if (normalized === 'today' || normalized === 'yesterday') {
    return { mode: normalized };
  }
  const rolling = parseRollingUsageRange(normalized);
  return rolling ? { mode: rolling.unit, value: rolling.value } : { mode: 'hour', value: 8 };
};

export const buildRollingUsageRange = (unit: 'hour' | 'day', value: number): KeyOverviewTimeRange => {
  const minimum = unit === 'hour' ? 5 : 1;
  const maximum = unit === 'hour' ? 24 : 30;
  const amount = Math.min(maximum, Math.max(minimum, Math.round(Number.isFinite(value) ? value : minimum)));
  return `${amount}${unit === 'hour' ? 'h' : 'd'}`;
};

const parseCustomDateParam = (value: string | undefined): { value: string; timestampMs: number } | undefined => {
  const trimmed = value?.trim();
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(trimmed ?? '');
  if (!match || !trimmed) return undefined;
  const [, year, month, day] = match;
  const yearNumber = Number(year);
  const monthNumber = Number(month);
  const dayNumber = Number(day);
  const date = new Date(yearNumber, monthNumber - 1, dayNumber, 0, 0, 0, 0);
  if (Number.isNaN(date.getTime())) return undefined;
  if (date.getFullYear() !== yearNumber || date.getMonth() !== monthNumber - 1 || date.getDate() !== dayNumber) return undefined;
  return { value: trimmed, timestampMs: date.getTime() };
};

const parseCustomHourParam = (value: string | undefined): { value: string; timestampMs: number } | undefined => {
  const trimmed = value?.trim();
  if (!trimmed || !/^\d{4}-\d{2}-\d{2}T\d{2}:00:00(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(trimmed)) return undefined;
  const timestampMs = Date.parse(trimmed);
  return Number.isFinite(timestampMs) ? { value: trimmed, timestampMs } : undefined;
};

export function buildUsageRangeQuery({
  range,
  customUnit,
  customStart,
  customEnd,
}: {
  range: string;
  customUnit?: UsageCustomRangeUnit;
  customStart?: string;
  customEnd?: string;
}): UsageRangeQuery {
  const normalizedRange = normalizeUsageRange(range);
  const requestRange = resolveUsageRequestRange(normalizedRange);
  if (requestRange !== 'custom') {
    return { valid: true, range: requestRange };
  }

  const unit = customUnit ?? 'day';
  const parseParam = unit === 'hour' ? parseCustomHourParam : parseCustomDateParam;
  const start = parseParam(customStart);
  const end = parseParam(customEnd);
  if (!start || !end || start.timestampMs > end.timestampMs) {
    return { valid: false, range: normalizedRange };
  }
  if (unit === 'hour') {
    const selectedHours = (end.timestampMs - start.timestampMs) / (60 * 60 * 1000) + 1;
    if (!Number.isInteger(selectedHours) || selectedHours < 5 || selectedHours > 24) {
      return { valid: false, range: normalizedRange };
    }
  }
  return { valid: true, range: normalizedRange, unit, start: start.value, end: end.value };
}
