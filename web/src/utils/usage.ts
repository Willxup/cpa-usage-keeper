import type { UsageFilterWindow, UsageTimeRange } from '@/lib/types';
import type { UsagePayload } from '@/components/usage/hooks/useUsageData';
import { parseSelectableUsageRange, resolveUsageRequestRange } from '@/utils/usage/rangeQuery';
import {
  LATENCY_SOURCE_FIELD,
  LATENCY_SOURCE_UNIT,
  extractLatencyMs,
  calculateLatencyStatsFromDetails,
  formatDurationMs
} from '@/utils/usage/latency';

export {
  LATENCY_SOURCE_FIELD,
  LATENCY_SOURCE_UNIT,
  extractLatencyMs,
  calculateLatencyStatsFromDetails,
  formatDurationMs
};
export type { UsageTimeRange, UsageFilterWindow } from '@/lib/types';
export type { UsagePayload } from '@/components/usage/hooks/useUsageData';

export interface StatusBlockDetail {
  startTime: number;
  endTime: number;
  success: number;
  failure: number;
  rate: number;
}

export interface ServiceHealthData {
  totalSuccess: number;
  totalFailure: number;
  successRate: number;
  rows: number;
  columns: number;
  bucketSeconds: number;
  windowStart: number;
  windowEnd: number;
  blockDetails: StatusBlockDetail[];
}

const toNumber = (value: unknown): number => {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
};

export function calculateDisplayInputTokens({
  inputTokens,
  cacheReadTokens,
  cacheCreationTokens,
}: {
  inputTokens: unknown;
  cacheReadTokens: unknown;
  cacheCreationTokens: unknown;
}): number {
  return Math.max(
    Math.max(toNumber(inputTokens), 0)
      - Math.max(toNumber(cacheReadTokens), 0)
      - Math.max(toNumber(cacheCreationTokens), 0),
    0,
  );
}

export function calculateDisplayOutputTokens({ outputTokens, reasoningTokens }: { outputTokens: unknown; reasoningTokens: unknown }): number {
  return Math.max(Math.max(toNumber(outputTokens), 0) - Math.max(toNumber(reasoningTokens), 0), 0);
}

const toValidTimestamp = (value: unknown): number | null => {
  const timestamp = typeof value === 'number' ? value : Date.parse(String(value ?? ''));
  return Number.isFinite(timestamp) && timestamp > 0 ? timestamp : null;
};

export function formatCompactNumber(value: number): string {
  const abs = Math.abs(value);
  const formatScaled = (scaled: number, suffix: string) => `${scaled.toFixed(2)}${suffix}`;

  if (abs < 1_000) {
    return new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(value);
  }
  if (abs < 1_000_000) {
    return formatScaled(value / 1_000, 'K');
  }
  if (abs < 1_000_000_000) {
    return formatScaled(value / 1_000_000, 'M');
  }
  return formatScaled(value / 1_000_000_000, 'B');
}

export function formatCompactTokenValue(value: number, withUnit = false): string {
  const formatted = formatCompactNumber(value);
  return withUnit ? `${formatted} tokens` : formatted;
}

export function formatFixedTwoDecimals(value: number): string {
  return new Intl.NumberFormat(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2
  }).format(value || 0);
}

export function formatPerMinuteValue(value: number): string {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: value >= 100 ? 0 : value >= 10 ? 1 : 2 }).format(value);
}

export function formatUsd(value: number): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: value < 1 ? 4 : 2,
    maximumFractionDigits: value < 1 ? 4 : 2
  }).format(value || 0).replace(/^US\$/, '$');
}

export function normalizeAuthIndex(value: unknown): string {
  if (value === null || value === undefined) return '';
  return String(value).trim();
}

export function resolveUsageFilterWindow(
  usage: UsagePayload | null | undefined,
  range: UsageTimeRange,
  options: {
    nowMs?: number;
    customStart?: string | number;
    customEnd?: string | number;
  } = {}
): UsageFilterWindow {
  const fallbackNow = toValidTimestamp(options.nowMs) ?? Date.now();
  const requestRange = resolveUsageRequestRange(range);

  if (requestRange === 'custom') {
    const startMs = toValidTimestamp(options.customStart);
    const endMs = toValidTimestamp(options.customEnd);
    if (startMs === null || endMs === null || startMs > endMs) {
      return {};
    }
    return {
      startMs,
      endMs,
      windowMinutes: Math.max((endMs - startMs) / 60000, 1)
    };
  }

  if (requestRange === 'today' || requestRange === 'yesterday') {
    const start = new Date(fallbackNow);
    start.setHours(0, 0, 0, 0);
    if (requestRange === 'yesterday') {
      start.setDate(start.getDate() - 1);
    }
    const startMs = start.getTime();
    const endMs = requestRange === 'today' ? fallbackNow : startMs + (24 * 60 * 60 * 1000) - 1;
    return {
      startMs,
      endMs,
      windowMinutes: requestRange === 'today' ? Math.max((endMs - startMs) / 60000, 1) : 24 * 60
    };
  }

  const rollingRange = parseSelectableUsageRange(requestRange);
  const windowHours = (rollingRange.value ?? 8) * (rollingRange.mode === 'day' ? 24 : 1);
  const endMs = fallbackNow;
  const startMs = endMs - windowHours * 60 * 60 * 1000;
  return {
    startMs,
    endMs,
    windowMinutes: windowHours * 60
  };
}

export function calculateCacheReadRate({
  inputTokens,
  cacheReadTokens,
}: {
  inputTokens: unknown;
  cacheReadTokens: unknown;
}): number | null {
  const input = Math.max(toNumber(inputTokens), 0);
  const cacheRead = Math.max(toNumber(cacheReadTokens), 0);
  // token 已在后端按 provider type 归一化，前端只按统一字段展示缓存占比。
  const denominator = input;
  if (denominator <= 0) {
    return null;
  }
  return (cacheRead / denominator) * 100;
}

export function buildCandidateUsageSourceIds({ apiKey, prefix }: { apiKey?: string; prefix?: string }): string[] {
  const set = new Set<string>();
  if (apiKey?.trim()) {
    set.add(apiKey.trim());
    set.add(`t:${apiKey.trim()}`);
  }
  if (prefix?.trim()) {
    set.add(prefix.trim());
    set.add(`t:${prefix.trim()}`);
  }
  return Array.from(set);
}
