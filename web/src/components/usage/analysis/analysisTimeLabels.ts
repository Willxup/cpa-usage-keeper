import type { AnalysisResponse } from '@/lib/types';

import { disambiguateTimeLabels, getIntlTimeZone } from '@/utils/usage/timeLabels';

const formatBucketLabelFromLiteral = (bucket: string, granularity: AnalysisResponse['granularity']) => {
  const match = bucket.match(/^(\d{4})-(\d{2})-(\d{2})(?:[T\s](\d{2}))?/);
  if (!match) return null;
  const month = Number(match[2]);
  const day = Number(match[3]);
  const hour = match[4] ? Number(match[4]) : NaN;
  if (month < 1 || month > 12 || day < 1 || day > 31) return null;
  if (granularity === 'daily') {
    return `${month}/${day}`;
  }
  if (!Number.isFinite(hour) || hour < 0 || hour > 23) return null;
  return `${String(hour).padStart(2, '0')}:00`;
};

export const formatBucketLabel = (bucket: string, granularity: AnalysisResponse['granularity'], timezone?: string) => {
  const date = new Date(bucket);
  if (Number.isNaN(date.getTime())) return bucket;
  const timeZone = getIntlTimeZone(timezone);
  // Analysis bucket 已按项目 TZ 聚合，前端必须显式使用响应 TZ，避免被 CI 或浏览器本地时区二次换算。
  try {
    if (granularity === 'daily') {
      return new Intl.DateTimeFormat('en-US', { month: 'numeric', day: 'numeric', timeZone }).format(date);
    }
    const hour = new Intl.DateTimeFormat('en-GB', { hour: '2-digit', hourCycle: 'h23', timeZone }).format(date);
    return `${hour}:00`;
  } catch {
    const literalLabel = formatBucketLabelFromLiteral(bucket, granularity);
    if (literalLabel) return literalLabel;
  }
  if (granularity === 'daily') {
    return `${date.getMonth() + 1}/${date.getDate()}`;
  }
  return `${String(date.getHours()).padStart(2, '0')}:00`;
};

export const formatBucketLabels = (buckets: readonly string[], granularity: AnalysisResponse['granularity'], timezone?: string) => (
  granularity === 'daily'
    ? buckets.map((bucket) => formatBucketLabel(bucket, granularity, timezone))
    : disambiguateTimeLabels(buckets, timezone, (bucket) => formatBucketLabel(bucket, granularity, timezone), 'hours')
);
