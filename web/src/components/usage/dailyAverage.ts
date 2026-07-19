import type { UsageOverviewPayload } from './hooks/useUsageData';
import { formatCompactNumber } from '@/utils/usage';

export interface DailyAverageMetrics {
  requests: number;
  tokens: number;
  cost: number;
  rangeDays: number;
  costAvailable: boolean;
}

const isFiniteMetric = (value: unknown): value is number => (
  typeof value === 'number' && Number.isFinite(value)
);

export function buildDailyAverageMetrics(usage: UsageOverviewPayload | null): DailyAverageMetrics | null {
  const summary = usage?.summary;
  if (!summary) return null;

  const requests = summary.daily_average_requests;
  const tokens = summary.daily_average_tokens;
  const cost = summary.daily_average_cost;
  const rangeDays = summary.daily_average_range_days;
  if (!isFiniteMetric(requests) || !isFiniteMetric(tokens) || !isFiniteMetric(cost) || !isFiniteMetric(rangeDays)) {
    return null;
  }

  return {
    requests,
    tokens,
    cost,
    rangeDays,
    costAvailable: summary.cost_available === true,
  };
}

export const formatDailyAverageCount = (value: number): string => {
  if (Math.abs(value) < 1_000) {
    return new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(value);
  }
  return formatCompactNumber(value);
};

export const formatDailyAverageRangeDays = (value: number): string => (
  new Intl.NumberFormat(undefined, { maximumFractionDigits: value >= 10 ? 0 : 1 }).format(value)
);
