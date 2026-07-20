import type { UsageCustomRangeUnit, UsageTimeRange } from '@/lib/types';
import { buildUsageRangeQuery, parseSelectableUsageRange } from '@/utils/usage/rangeQuery';

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

export const getDailyAverageUsage = <T>(
  currentUsage: T | null,
  fallbackUsage: T | null,
  reserveVisible: boolean,
  loading = false,
): T | null => currentUsage ?? (reserveVisible && loading ? fallbackUsage : null);

export const isDailyAverageRange = ({
  range,
  customStart,
  customEnd,
  customUnit,
}: {
  range: UsageTimeRange;
  customStart?: string;
  customEnd?: string;
  customUnit?: UsageCustomRangeUnit;
}): boolean => {
  const rangeQuery = buildUsageRangeQuery({ range, customUnit, customStart, customEnd });
  if (!rangeQuery.valid) {
    return false;
  }
  if (rangeQuery.range !== 'custom') {
    return parseSelectableUsageRange(rangeQuery.range).mode === 'day';
  }
  return rangeQuery.unit === 'day' && Boolean(rangeQuery.start && rangeQuery.end && rangeQuery.start < rangeQuery.end);
};
