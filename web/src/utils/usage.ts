import {
  LATENCY_SOURCE_FIELD,
  LATENCY_SOURCE_UNIT,
  extractLatencyMs,
  calculateLatencyStatsFromDetails,
  formatDurationMs
} from '@/utils/usage/latency';

export * from '@/lib/usage';
export {
  LATENCY_SOURCE_FIELD,
  LATENCY_SOURCE_UNIT,
  extractLatencyMs,
  calculateLatencyStatsFromDetails,
  formatDurationMs
};
export type { UsageSnapshot as UsagePayload, UsageTimeRange, UsageFilterWindow } from '@/lib/types';

export interface ModelPrice {
  prompt: number;
  completion: number;
  cache: number;
}

export interface ChartDataset {
  label: string;
  data: number[];
  borderColor: string;
  backgroundColor: string;
  pointBackgroundColor?: string;
  pointBorderColor?: string;
  fill?: boolean;
  tension?: number;
}

export interface ChartData {
  labels: string[];
  datasets: ChartDataset[];
}

export interface ModelStatsSummary {
  model: string;
  requests: number;
  successCount: number;
  failureCount: number;
  tokens: number;
  averageLatencyMs: number | null;
  totalLatencyMs: number | null;
  latencySampleCount: number;
  cost: number;
}

export interface ApiStatsModelSummary {
  requests: number;
  successCount: number;
  failureCount: number;
  tokens: number;
}

export interface ApiStats {
  endpoint: string;
  displayName: string;
  totalRequests: number;
  successCount: number;
  failureCount: number;
  totalTokens: number;
  totalCost: number;
  models: Record<string, ApiStatsModelSummary>;
}

export type TokenCategory = 'input' | 'output' | 'cached' | 'reasoning';

export interface UsageDetailRecord {
  timestamp: string;
  source: string;
  source_raw?: string;
  source_display?: string;
  source_type?: string;
  source_key?: string;
  auth_index: string;
  failed: boolean;
  latency_ms: number;
  tokens: {
    input_tokens: number;
    output_tokens: number;
    reasoning_tokens: number;
    cached_tokens: number;
    cache_tokens?: number;
    total_tokens: number;
  };
  __apiName?: string;
  __apiDisplayName?: string;
  __modelName?: string;
  __timestampMs?: number;
  [key: string]: unknown;
}

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
  blockDetails: StatusBlockDetail[];
}

const CHART_COLORS = ['#8b8680', '#8b5cf6', '#22c55e', '#f97316', '#f59e0b', '#06b6d4', '#ef4444', '#6366f1', '#ec4899'];
const SOURCE_PREFIXES = ['sk-', 'gsk_', 'rk-', 'pk-', 'AIza', 'claude-', 'vertex-', 'gemini-'];

const toNumber = (value: unknown): number => {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
};

const startOfDayKey = (timestamp: string): string => {
  const date = new Date(timestamp);
  return Number.isNaN(date.getTime()) ? '' : date.toISOString().slice(0, 10);
};

const startOfHourKey = (timestamp: string): string => {
  const date = new Date(timestamp);
  return Number.isNaN(date.getTime()) ? '' : `${date.toISOString().slice(0, 13)}:00:00Z`;
};

const formatHourLabel = (key: string): string => {
  const date = new Date(key);
  if (Number.isNaN(date.getTime())) return key;
  const md = `${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;
  const time = `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`;
  return `${md} ${time}`;
};

const formatDayLabel = (key: string): string => key;

const normalizeHourWindow = (hourWindowHours?: number): number => {
  if (!Number.isFinite(hourWindowHours) || !hourWindowHours || hourWindowHours <= 0) {
    return 24;
  }
  return Math.min(Math.max(Math.floor(hourWindowHours), 1), 24 * 31);
};

const resolveHourlyChartWindowHours = (hourWindowHours?: number): number =>
  normalizeHourWindow(hourWindowHours);

const buildHourlyWindow = (hourWindowHours?: number, endMs?: number) => {
  const resolvedHourWindow = resolveHourlyChartWindowHours(hourWindowHours);
  const hourMs = 60 * 60 * 1000;
  const currentHour = new Date(Number.isFinite(endMs) && endMs && endMs > 0 ? endMs : Date.now());
  currentHour.setMinutes(0, 0, 0);
  const earliestBucket = new Date(currentHour);
  earliestBucket.setHours(earliestBucket.getHours() - resolvedHourWindow);
  const earliestTime = earliestBucket.getTime();
  const labels = Array.from({ length: resolvedHourWindow }, (_, index) =>
    formatHourLabel(new Date(earliestTime + index * hourMs).toISOString())
  );
  return {
    hourMs,
    earliestTime,
    lastBucketTime: earliestTime + (labels.length - 1) * hourMs,
    labels
  };
};

const resolveHourlyChartEndMs = (details: UsageDetailRecord[], _hourWindowHours?: number, endMs?: number): number | undefined => {
  const requestedEndMs = Number.isFinite(endMs) && endMs && endMs > 0 ? endMs : undefined;
  if (requestedEndMs !== undefined) return requestedEndMs;
  if (!details.length) return undefined;
  return getDetailTimestampBounds(details)?.latestMs;
};

const getHourBucketIndex = (timestamp: number, hourWindowHours?: number): { index: number; labels: string[] } | null => {
  if (!Number.isFinite(timestamp) || timestamp <= 0) return null;
  const { earliestTime, lastBucketTime, hourMs, labels } = buildHourlyWindow(hourWindowHours);
  const normalized = new Date(timestamp);
  normalized.setMinutes(0, 0, 0);
  const bucketStart = normalized.getTime();
  if (bucketStart < earliestTime || bucketStart > lastBucketTime) return null;
  const index = Math.floor((bucketStart - earliestTime) / hourMs);
  if (index < 0 || index >= labels.length) return null;
  return { index, labels };
};

const sum = (values: number[]) => values.reduce((total, value) => total + value, 0);

const PRESET_WINDOW_HOURS: Record<Exclude<UsageTimeRange, 'all' | 'custom'>, number> = {
  '4h': 4,
  '8h': 8,
  '12h': 12,
  '24h': 24,
  '7d': 24 * 7
};

const toValidTimestamp = (value: unknown): number | null => {
  const timestamp = typeof value === 'number' ? value : Date.parse(String(value ?? ''));
  return Number.isFinite(timestamp) && timestamp > 0 ? timestamp : null;
};

const getDetailTimestampBounds = (details: UsageDetailRecord[]): { earliestMs: number; latestMs: number } | null => {
  let earliestMs = Number.POSITIVE_INFINITY;
  let latestMs = Number.NEGATIVE_INFINITY;
  details.forEach((detail) => {
    const timestamp = detail.__timestampMs ?? 0;
    if (!Number.isFinite(timestamp) || timestamp <= 0) return;
    earliestMs = Math.min(earliestMs, timestamp);
    latestMs = Math.max(latestMs, timestamp);
  });
  if (!Number.isFinite(earliestMs) || !Number.isFinite(latestMs)) return null;
  return { earliestMs, latestMs };
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
  return new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: value < 1 ? 4 : 2,
    maximumFractionDigits: value < 1 ? 4 : 2
  }).format(value || 0);
}

export function normalizeAuthIndex(value: unknown): string {
  if (value === null || value === undefined) return '';
  return String(value).trim();
}

export function extractTotalTokens(detail: Partial<UsageDetailRecord>): number {
  const tokens = detail.tokens ?? {
    input_tokens: 0,
    output_tokens: 0,
    reasoning_tokens: 0,
    cached_tokens: 0,
    total_tokens: 0
  };
  const explicit = toNumber(tokens.total_tokens);
  if (explicit > 0) return explicit;
  return toNumber(tokens.input_tokens) + toNumber(tokens.output_tokens) + toNumber(tokens.reasoning_tokens) + Math.max(toNumber(tokens.cached_tokens), toNumber(tokens.cache_tokens));
}

export function collectUsageDetails(usage: UsagePayload | null | undefined): UsageDetailRecord[] {
  if (!usage?.apis) return [];
  const rows: UsageDetailRecord[] = [];
  Object.entries(usage.apis).forEach(([apiName, api]) => {
    const apiDisplayName = String(api.display_name ?? apiName).trim() || apiName;
    Object.entries(api.models ?? {}).forEach(([modelName, model]) => {
      (model.details ?? []).forEach((detail) => {
        const timestampMs = Date.parse(detail.timestamp);
        rows.push({
          ...detail,
          latency_ms: toNumber(detail.latency_ms),
          __apiName: apiName,
          __apiDisplayName: apiDisplayName,
          __modelName: modelName,
          __timestampMs: Number.isFinite(timestampMs) ? timestampMs : 0
        });
      });
    });
  });
  return rows.sort((a, b) => (b.__timestampMs ?? 0) - (a.__timestampMs ?? 0));
}

export function getModelNamesFromUsage(usage: UsagePayload | null | undefined): string[] {
  const names = new Set<string>();
  Object.values(usage?.apis ?? {}).forEach((api) => {
    Object.keys(api.models ?? {}).forEach((modelName) => {
      const normalized = modelName.trim();
      if (normalized) {
        names.add(normalized);
      }
    });
  });
  return Array.from(names).sort((a, b) => a.localeCompare(b));
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
  const details = collectUsageDetails(usage);
  const bounds = getDetailTimestampBounds(details);
  const fallbackNow = toValidTimestamp(options.nowMs) ?? Date.now();

  if (range === 'all') {
    if (!bounds) return {};
    const spanMinutes = Math.max((bounds.latestMs - bounds.earliestMs) / 60000, 1);
    return {
      startMs: bounds.earliestMs,
      endMs: bounds.latestMs,
      windowMinutes: spanMinutes
    };
  }

  if (range === 'custom') {
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

  const windowHours = PRESET_WINDOW_HOURS[range];
  const endMs = fallbackNow;
  const startMs = endMs - windowHours * 60 * 60 * 1000;
  return {
    startMs,
    endMs,
    windowMinutes: windowHours * 60
  };
}

export function filterUsageByWindow(usage: UsagePayload, window: UsageFilterWindow): UsagePayload {
  const details = collectUsageDetails(usage);
  if (!details.length) return usage;
  const { startMs, endMs } = window;
  if (startMs === undefined && endMs === undefined) {
    return usage;
  }
  const filteredDetails = details.filter((detail) => {
    const timestamp = detail.__timestampMs ?? 0;
    if (!Number.isFinite(timestamp) || timestamp <= 0) return false;
    if (startMs !== undefined && timestamp < startMs) return false;
    if (endMs !== undefined && timestamp > endMs) return false;
    return true;
  });
  return buildUsageFromDetails(filteredDetails);
}

export function filterUsageByTimeRange(
  usage: UsagePayload,
  range: UsageTimeRange,
  options: {
    nowMs?: number;
    customStart?: string | number;
    customEnd?: string | number;
  } = {}
): UsagePayload {
  const window = resolveUsageFilterWindow(usage, range, options);
  return filterUsageByWindow(usage, window);
}

export function loadModelPrices(): Record<string, ModelPrice> {
  try {
    const raw = window.localStorage.getItem('cpa-model-prices');
    if (!raw) return {};
    const parsed = JSON.parse(raw) as Record<string, ModelPrice>;
    return parsed && typeof parsed === 'object' ? parsed : {};
  } catch {
    return {};
  }
}

export function saveModelPrices(prices: Record<string, ModelPrice>): void {
  window.localStorage.setItem('cpa-model-prices', JSON.stringify(prices));
}

export function calculateCost(detail: UsageDetailRecord, modelPrices: Record<string, ModelPrice>): number {
  const modelName = detail.__modelName ?? '';
  const pricing = modelPrices[modelName];
  if (!pricing) return 0;

  const inputTokens = Math.max(toNumber(detail.tokens.input_tokens), 0);
  const completionTokens = Math.max(toNumber(detail.tokens.output_tokens), 0);
  const cachedTokens = Math.max(
    toNumber(detail.tokens.cached_tokens),
    toNumber(detail.tokens.cache_tokens)
  );
  const promptTokens = Math.max(inputTokens - cachedTokens, 0);

  return (
    (promptTokens / 1_000_000) * pricing.prompt +
    (completionTokens / 1_000_000) * pricing.completion +
    (cachedTokens / 1_000_000) * pricing.cache
  );
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

export function getApiStats(usage: UsagePayload | null, modelPrices: Record<string, ModelPrice>): ApiStats[] {
  if (!usage?.apis) return [];
  return Object.entries(usage.apis)
    .map(([endpoint, api]) => {
      const models: Record<string, ApiStatsModelSummary> = {};
      let totalCost = 0;
      Object.entries(api.models ?? {}).forEach(([modelName, model]) => {
        models[modelName] = {
          requests: toNumber(model.total_requests),
          successCount: toNumber(model.success_count),
          failureCount: toNumber(model.failure_count),
          tokens: toNumber(model.total_tokens)
        };
        (model.details ?? []).forEach((detail) => {
          totalCost += calculateCost({ ...detail, __modelName: modelName }, modelPrices);
        });
      });
      return {
        endpoint,
        displayName: String(api.display_name ?? endpoint).trim() || endpoint,
        totalRequests: toNumber(api.total_requests),
        successCount: toNumber(api.success_count),
        failureCount: toNumber(api.failure_count),
        totalTokens: toNumber(api.total_tokens),
        totalCost,
        models
      };
    })
    .sort((a, b) => b.totalRequests - a.totalRequests);
}

export function getModelStats(usage: UsagePayload | null, modelPrices: Record<string, ModelPrice>): ModelStatsSummary[] {
  const grouped = new Map<string, ModelStatsSummary>();
  collectUsageDetails(usage).forEach((detail) => {
    const model = detail.__modelName || '-';
    const current = grouped.get(model) ?? {
      model,
      requests: 0,
      successCount: 0,
      failureCount: 0,
      tokens: 0,
      averageLatencyMs: null,
      totalLatencyMs: 0,
      latencySampleCount: 0,
      cost: 0
    };
    current.requests += 1;
    current.tokens += extractTotalTokens(detail);
    current.cost += calculateCost(detail, modelPrices);
    if (detail.failed) current.failureCount += 1;
    else current.successCount += 1;
    const latency = extractLatencyMs(detail);
    if (latency !== null) {
      current.totalLatencyMs = (current.totalLatencyMs ?? 0) + latency;
      current.latencySampleCount += 1;
      current.averageLatencyMs = (current.totalLatencyMs ?? 0) / current.latencySampleCount;
    }
    grouped.set(model, current);
  });
  return Array.from(grouped.values()).sort((a, b) => b.requests - a.requests);
}

export function buildChartData(
  usage: UsagePayload,
  period: 'hour' | 'day',
  metric: 'requests' | 'tokens',
  chartLines: string[],
  options: { hourWindowHours?: number; endMs?: number } = {}
): ChartData {
  const details = collectUsageDetails(usage);
  if (!details.length) {
    const lines = chartLines.length ? chartLines : ['all'];
    if (!(lines.length === 1 && lines[0] === 'all')) {
      return { labels: [], datasets: [] };
    }
    const bucketMap = period === 'hour'
      ? (metric === 'requests' ? usage.requests_by_hour : usage.tokens_by_hour)
      : (metric === 'requests' ? usage.requests_by_day : usage.tokens_by_day);
    const bucketKeys = Object.keys(bucketMap ?? {}).sort((a, b) => a.localeCompare(b));
    if (!bucketKeys.length) {
      return { labels: [], datasets: [] };
    }
    return {
      labels: bucketKeys.map((key) => (period === 'hour' ? formatHourLabel(key) : formatDayLabel(key))),
      datasets: [
        {
          label: 'All',
          data: bucketKeys.map((key) => toNumber(bucketMap?.[key])),
          borderColor: CHART_COLORS[0],
          backgroundColor: `${CHART_COLORS[0]}22`,
          pointBackgroundColor: CHART_COLORS[0],
          pointBorderColor: CHART_COLORS[0],
          fill: false,
          tension: 0.35
        }
      ]
    };
  }

  const lines = chartLines.length ? chartLines : ['all'];
  const bucketsByLine = new Map<string, Map<string, number>>();
  const orderedKeys = new Set<string>();

  if (period === 'hour') {
    const hourEndMs = resolveHourlyChartEndMs(details, options.hourWindowHours, options.endMs);
    const { labels, earliestTime, lastBucketTime, hourMs } = buildHourlyWindow(options.hourWindowHours, hourEndMs);
    const bucketKeys = labels.map((_, index) => new Date(earliestTime + index * hourMs).toISOString());
    bucketKeys.forEach((key) => orderedKeys.add(key));

    details.forEach((detail) => {
      const timestamp = detail.__timestampMs ?? 0;
      if (!Number.isFinite(timestamp) || timestamp <= 0) return;
      const normalized = new Date(timestamp);
      normalized.setMinutes(0, 0, 0);
      const bucketStart = normalized.getTime();
      if (bucketStart < earliestTime || bucketStart > lastBucketTime) return;
      const key = new Date(bucketStart).toISOString();
      const lineKey = lines.includes(detail.__modelName ?? '')
        ? detail.__modelName ?? 'all'
        : lines.includes(detail.__apiName ?? '')
          ? detail.__apiName ?? 'all'
          : 'all';
      if (!lines.includes('all') && lineKey === 'all') return;
      const line = bucketsByLine.get(lineKey) ?? new Map<string, number>();
      const value = metric === 'requests' ? 1 : extractTotalTokens(detail);
      line.set(key, (line.get(key) ?? 0) + value);
      bucketsByLine.set(lineKey, line);
      if (lines.includes('all')) {
        const allLine = bucketsByLine.get('all') ?? new Map<string, number>();
        allLine.set(key, (allLine.get(key) ?? 0) + value);
        bucketsByLine.set('all', allLine);
      }
    });

    return {
      labels,
      datasets: Array.from(bucketsByLine.entries()).map(([label, values], index) => ({
        label: label === 'all' ? 'All' : label,
        data: bucketKeys.map((key) => values.get(key) ?? 0),
        borderColor: CHART_COLORS[index % CHART_COLORS.length],
        backgroundColor: `${CHART_COLORS[index % CHART_COLORS.length]}22`,
        pointBackgroundColor: CHART_COLORS[index % CHART_COLORS.length],
        pointBorderColor: CHART_COLORS[index % CHART_COLORS.length],
        fill: false,
        tension: 0.35
      }))
    };
  }

  details.forEach((detail) => {
    const key = startOfDayKey(detail.timestamp);
    if (!key) return;
    orderedKeys.add(key);
    const lineKey = lines.includes(detail.__modelName ?? '') ? detail.__modelName ?? 'all' : lines.includes(detail.__apiName ?? '') ? detail.__apiName ?? 'all' : 'all';
    if (!lines.includes('all') && lineKey === 'all') return;
    const line = bucketsByLine.get(lineKey) ?? new Map<string, number>();
    const value = metric === 'requests' ? 1 : extractTotalTokens(detail);
    line.set(key, (line.get(key) ?? 0) + value);
    bucketsByLine.set(lineKey, line);
    if (lines.includes('all')) {
      const allLine = bucketsByLine.get('all') ?? new Map<string, number>();
      allLine.set(key, (allLine.get(key) ?? 0) + value);
      bucketsByLine.set('all', allLine);
    }
  });

  const bucketKeys = Array.from(orderedKeys).sort((a, b) => a.localeCompare(b));
  return {
    labels: bucketKeys.map((key) => formatDayLabel(key)),
    datasets: Array.from(bucketsByLine.entries()).map(([label, values], index) => ({
      label: label === 'all' ? 'All' : label,
      data: bucketKeys.map((key) => values.get(key) ?? 0),
      borderColor: CHART_COLORS[index % CHART_COLORS.length],
      backgroundColor: `${CHART_COLORS[index % CHART_COLORS.length]}22`,
      pointBackgroundColor: CHART_COLORS[index % CHART_COLORS.length],
      pointBorderColor: CHART_COLORS[index % CHART_COLORS.length],
      fill: false,
      tension: 0.35
    }))
  };
}

export function buildHourlyTokenBreakdown(usage: UsagePayload | null, hourWindowHours = 24, endMs?: number) {
  return buildTokenBreakdownSeries(usage, 'hour', hourWindowHours, endMs);
}

export function buildDailyTokenBreakdown(usage: UsagePayload | null) {
  return buildTokenBreakdownSeries(usage, 'day');
}

function buildTokenBreakdownSeries(usage: UsagePayload | null, period: 'hour' | 'day', hourWindowHours?: number, endMs?: number) {
  const details = collectUsageDetails(usage);
  if (!details.length) {
    return { labels: [], dataByCategory: { input: [], output: [], cached: [], reasoning: [] } as Record<TokenCategory, number[]> };
  }

  if (period === 'hour') {
    const hourEndMs = resolveHourlyChartEndMs(details, hourWindowHours, endMs);
    const { labels, earliestTime, lastBucketTime, hourMs } = buildHourlyWindow(hourWindowHours, hourEndMs);
    const dataByCategory = {
      input: new Array(labels.length).fill(0),
      output: new Array(labels.length).fill(0),
      cached: new Array(labels.length).fill(0),
      reasoning: new Array(labels.length).fill(0)
    } as Record<TokenCategory, number[]>;

    details.forEach((detail) => {
      const timestamp = detail.__timestampMs ?? 0;
      if (!Number.isFinite(timestamp) || timestamp <= 0) return;
      const normalized = new Date(timestamp);
      normalized.setMinutes(0, 0, 0);
      const bucketStart = normalized.getTime();
      if (bucketStart < earliestTime || bucketStart > lastBucketTime) return;
      const bucketIndex = Math.floor((bucketStart - earliestTime) / hourMs);
      if (bucketIndex < 0 || bucketIndex >= labels.length) return;
      dataByCategory.input[bucketIndex] += toNumber(detail.tokens.input_tokens);
      dataByCategory.output[bucketIndex] += toNumber(detail.tokens.output_tokens);
      dataByCategory.cached[bucketIndex] += Math.max(toNumber(detail.tokens.cached_tokens), toNumber(detail.tokens.cache_tokens));
      dataByCategory.reasoning[bucketIndex] += toNumber(detail.tokens.reasoning_tokens);
    });

    return { labels, dataByCategory };
  }

  const keys = Array.from(new Set(details.map((detail) => startOfDayKey(detail.timestamp)))).filter(Boolean).sort((a, b) => a.localeCompare(b));
  const dataByCategory = { input: [], output: [], cached: [], reasoning: [] } as Record<TokenCategory, number[]>;
  keys.forEach((key) => {
    const matching = details.filter((detail) => startOfDayKey(detail.timestamp) === key);
    dataByCategory.input.push(sum(matching.map((detail) => toNumber(detail.tokens.input_tokens))));
    dataByCategory.output.push(sum(matching.map((detail) => toNumber(detail.tokens.output_tokens))));
    dataByCategory.cached.push(sum(matching.map((detail) => Math.max(toNumber(detail.tokens.cached_tokens), toNumber(detail.tokens.cache_tokens)))));
    dataByCategory.reasoning.push(sum(matching.map((detail) => toNumber(detail.tokens.reasoning_tokens))));
  });
  return { labels: keys.map((key) => formatDayLabel(key)), dataByCategory };
}

export function buildHourlyCostSeries(usage: UsagePayload | null, modelPrices: Record<string, ModelPrice>, hourWindowHours = 24, endMs?: number) {
  return buildCostSeries(usage, modelPrices, 'hour', hourWindowHours, endMs);
}

export function buildDailyCostSeries(usage: UsagePayload | null, modelPrices: Record<string, ModelPrice>) {
  return buildCostSeries(usage, modelPrices, 'day');
}

function buildCostSeries(usage: UsagePayload | null, modelPrices: Record<string, ModelPrice>, period: 'hour' | 'day', hourWindowHours?: number, endMs?: number) {
  const details = collectUsageDetails(usage);
  if (!details.length) return { labels: [], data: [], hasData: false };

  if (period === 'hour') {
    const hourEndMs = resolveHourlyChartEndMs(details, hourWindowHours, endMs);
    const { labels, earliestTime, lastBucketTime, hourMs } = buildHourlyWindow(hourWindowHours, hourEndMs);
    const data = new Array(labels.length).fill(0);
    let hasData = false;

    details.forEach((detail) => {
      const timestamp = detail.__timestampMs ?? 0;
      if (!Number.isFinite(timestamp) || timestamp <= 0) return;
      const normalized = new Date(timestamp);
      normalized.setMinutes(0, 0, 0);
      const bucketStart = normalized.getTime();
      if (bucketStart < earliestTime || bucketStart > lastBucketTime) return;
      const bucketIndex = Math.floor((bucketStart - earliestTime) / hourMs);
      if (bucketIndex < 0 || bucketIndex >= labels.length) return;
      const cost = calculateCost(detail, modelPrices);
      if (cost > 0) {
        data[bucketIndex] += cost;
        hasData = true;
      }
    });

    return { labels, data, hasData };
  }

  const grouped = new Map<string, number>();
  details.forEach((detail) => {
    const key = startOfDayKey(detail.timestamp);
    if (!key) return;
    grouped.set(key, (grouped.get(key) ?? 0) + calculateCost(detail, modelPrices));
  });
  const keys = Array.from(grouped.keys()).sort((a, b) => a.localeCompare(b));
  const data = keys.map((key) => grouped.get(key) ?? 0);
  return { labels: keys.map((key) => formatDayLabel(key)), data, hasData: data.some((value) => value > 0) };
}

export function calculateServiceHealthData(details: UsageDetailRecord[]): ServiceHealthData {
  const rowCount = 7;
  const blockCount = 96;
  const windowMs = 15 * 60 * 1000;
  const totalBlocks = rowCount * blockCount;
  const timelineAnchor = Date.now();
  const currentBucketStart = Math.floor(timelineAnchor / windowMs) * windowMs;
  const newestWindowEnd = currentBucketStart + windowMs;
  const oldestWindowStart = newestWindowEnd - totalBlocks * windowMs;

  const blockDetails = Array.from({ length: totalBlocks }, (_, index) => {
    const startTime = oldestWindowStart + index * windowMs;
    const endTime = startTime + windowMs;
    const matching = details.filter((detail) => {
      const timestamp = detail.__timestampMs ?? 0;
      return timestamp >= startTime && timestamp < endTime;
    });
    const success = matching.filter((detail) => !detail.failed).length;
    const failure = matching.filter((detail) => detail.failed).length;
    const total = success + failure;

    return {
      startTime,
      endTime,
      success,
      failure,
      rate: total > 0 ? success / total : -1
    };
  });

  const totalSuccess = details.filter((detail) => !detail.failed).length;
  const totalFailure = details.filter((detail) => detail.failed).length;
  const total = totalSuccess + totalFailure;

  return {
    totalSuccess,
    totalFailure,
    successRate: total > 0 ? (totalSuccess / total) * 100 : 0,
    blockDetails
  };
}

export function buildUsageFromDetails(details: UsageDetailRecord[]): UsagePayload {
  const usage: UsagePayload = {
    total_requests: 0,
    success_count: 0,
    failure_count: 0,
    total_tokens: 0,
    requests_by_day: {},
    requests_by_hour: {},
    tokens_by_day: {},
    tokens_by_hour: {},
    apis: {}
  };

  details.forEach((detail) => {
    const apiName = detail.__apiName || 'unknown';
    const modelName = detail.__modelName || 'unknown';
    const tokens = extractTotalTokens(detail);
    const dayKey = startOfDayKey(detail.timestamp);
    const hourKey = startOfHourKey(detail.timestamp);

    const api = usage.apis[apiName] ?? {
      display_name: detail.__apiDisplayName || apiName,
      total_requests: 0,
      success_count: 0,
      failure_count: 0,
      total_tokens: 0,
      models: {}
    };
    const model = api.models[modelName] ?? {
      total_requests: 0,
      success_count: 0,
      failure_count: 0,
      total_tokens: 0,
      details: []
    };

    usage.total_requests += 1;
    usage.total_tokens += tokens;
    api.total_requests += 1;
    api.total_tokens += tokens;
    model.total_requests += 1;
    model.total_tokens += tokens;

    if (detail.failed) {
      usage.failure_count += 1;
      api.failure_count += 1;
      model.failure_count += 1;
    } else {
      usage.success_count += 1;
      api.success_count += 1;
      model.success_count += 1;
    }

    model.details.push({
      timestamp: detail.timestamp,
      latency_ms: toNumber(detail.latency_ms),
      source: detail.source ?? '',
      source_raw: detail.source_raw ?? '',
      source_display: detail.source_display ?? '',
      source_type: detail.source_type ?? '',
      source_key: detail.source_key ?? '',
      auth_index: detail.auth_index ?? '',
      failed: detail.failed === true,
      tokens: {
        input_tokens: toNumber(detail.tokens.input_tokens),
        output_tokens: toNumber(detail.tokens.output_tokens),
        reasoning_tokens: toNumber(detail.tokens.reasoning_tokens),
        cached_tokens: Math.max(toNumber(detail.tokens.cached_tokens), toNumber(detail.tokens.cache_tokens)),
        total_tokens: tokens
      }
    });

    usage.requests_by_day[dayKey] = (usage.requests_by_day[dayKey] ?? 0) + 1;
    usage.requests_by_hour[hourKey] = (usage.requests_by_hour[hourKey] ?? 0) + 1;
    usage.tokens_by_day[dayKey] = (usage.tokens_by_day[dayKey] ?? 0) + tokens;
    usage.tokens_by_hour[hourKey] = (usage.tokens_by_hour[hourKey] ?? 0) + tokens;

    api.models[modelName] = model;
    usage.apis[apiName] = api;
  });

  return usage;
}

export function inferSourceType(source: string): string {
  const value = source.trim();
  if (!value) return '';
  if (value.startsWith('t:')) return 'token';
  if (SOURCE_PREFIXES.some((prefix) => value.startsWith(prefix))) return 'api-key';
  return '';
}
