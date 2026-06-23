import type { UsageTimeRange } from '@/lib/types';
import { buildUsageRangeQuery } from '@/utils/usage/rangeQuery';

export const getOverviewDisplayLoading = ({ loading, hasUsage }: { loading: boolean; hasUsage: boolean }) => loading && !hasUsage;

export const getCurrentOverviewUsage = <T>(
  usage: T | null,
  currentQueryKey: string | null,
  loadedQueryKey: string | null,
): T | null => {
  if (!usage || !currentQueryKey || loadedQueryKey !== currentQueryKey) {
    return null;
  }
  return usage;
};

export const getDailyAveragePanelUsage = <T>(
  currentUsage: T | null,
  fallbackUsage: T | null,
  reserveVisible: boolean,
  loading = false,
): T | null => currentUsage ?? (reserveVisible && loading ? fallbackUsage : null);

export const isDailyAverageRange = ({
  range,
  customStart,
  customEnd,
}: {
  range: UsageTimeRange;
  customStart?: string;
  customEnd?: string;
}): boolean => {
  const rangeQuery = buildUsageRangeQuery({ range, customStart, customEnd });
  if (!rangeQuery.valid) {
    return false;
  }
  if (rangeQuery.range === '7d' || rangeQuery.range === '30d') {
    return true;
  }
  if (rangeQuery.range !== 'custom') {
    return false;
  }
  return Boolean(rangeQuery.start && rangeQuery.end && rangeQuery.start < rangeQuery.end);
};
