import type { RankingDetailMetric, RankingLeaderboardEntry, RankingMetric, RankingScope } from './types';

const formatCompactNumber = (value: number, maximumFractionDigits = 0): string => {
  const absolute = Math.abs(value);
  if (absolute < 1_000) {
    return new Intl.NumberFormat('en-US', { maximumFractionDigits }).format(value);
  }
  if (absolute < 1_000_000) return `${(value / 1_000).toFixed(2)}K`;
  if (absolute < 1_000_000_000) return `${(value / 1_000_000).toFixed(2)}M`;
  return `${(value / 1_000_000_000).toFixed(2)}B`;
};

const formatDuration = (milliseconds: number): string => {
  if (milliseconds < 1_000) return `${Math.round(milliseconds)}ms`;
  const seconds = milliseconds / 1_000;
  if (seconds < 60) {
    return `${seconds.toFixed(seconds < 10 ? 2 : 1).replace(/\.0+$/, '')}s`;
  }
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = Math.round(seconds % 60);
  return remainingSeconds > 0 ? `${minutes}m ${remainingSeconds}s` : `${minutes}m`;
};

export const formatLeaderboardValue = (
  metric: RankingMetric,
  entry: RankingLeaderboardEntry,
  scope: RankingScope = 'community',
): string => {
  if (metric === 'cache_read_rate') {
    const percent = entry.rate_denominator && entry.rate_denominator > 0
      ? (entry.rate_numerator ?? 0) / entry.rate_denominator * 100
      : entry.value / 10_000;
    return `${percent.toFixed(2)}%`;
  }
  if (metric === 'ttft_average' || metric === 'latency_average') {
    const milliseconds = entry.rate_denominator && entry.rate_denominator > 0
      ? (entry.rate_numerator ?? 0) / entry.rate_denominator
      : entry.value / 1_000;
    return formatDuration(milliseconds);
  }
  if (metric === 'peak_tpm' || metric === 'peak_rpm') {
    return formatCompactNumber(entry.value / 5, 2);
  }
  if (metric === 'overall') {
    return scope === 'local' ? `${entry.value} PTS` : `${(entry.value / 100).toFixed(2)} PTS`;
  }
  return formatCompactNumber(entry.value);
};

export const formatOverallMetricValue = (
  metric: RankingDetailMetric,
  entry: RankingLeaderboardEntry,
): string => {
  const value = entry.metrics?.[metric];
  if (value === undefined) return '—';
  return formatLeaderboardValue(metric, {
    ...entry,
    value,
    rate_numerator: undefined,
    rate_denominator: undefined,
  });
};
