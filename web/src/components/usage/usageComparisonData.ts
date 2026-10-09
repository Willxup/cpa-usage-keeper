import type { UsageComparisonItem } from '@/lib/types';

export interface ComparisonRow extends UsageComparisonItem {
  share: number | null;
  other?: boolean;
}
export const COMPARISON_TOP_LIMIT = 5;
const SUM_FIELDS = ['requests', 'failures', 'input_tokens', 'output_tokens', 'cache_read_tokens', 'cache_creation_tokens', 'reasoning_tokens', 'total_tokens'] as const;

export function buildComparisonView(items: UsageComparisonItem[], othersLabel: string) {
  const ranked = [...items].sort((a,b) => b.total_tokens - a.total_tokens || a.key.localeCompare(b.key));
  const total = ranked.reduce((sum,item) => sum + item.total_tokens,0);
  const rows: ComparisonRow[] = ranked.slice(0,COMPARISON_TOP_LIMIT).map(item => ({
    ...item, share:total > 0 ? item.total_tokens / total * 100 : null,
  }));
  // 排名与堆叠系列固定按区间 Token；“其他”同时合并 Tooltip/列表指标，任何缺价都保留为未知。
  const tail = ranked.slice(COMPARISON_TOP_LIMIT);
  if (tail.length > 0) {
    const other: ComparisonRow = {
      key:'__comparison_others__', label:othersLabel, other:true, share:null,
      requests:0, failures:0, input_tokens:0, output_tokens:0, cache_read_tokens:0,
      cache_creation_tokens:0, reasoning_tokens:0, total_tokens:0, cost:0, token_series:[],
    };
    for (const item of tail) {
      for (const field of SUM_FIELDS) other[field] += item[field];
      item.token_series?.forEach((value, index) => { other.token_series![index] = (other.token_series![index] ?? 0) + value; });
      other.cost = other.cost === null || item.cost === null ? null : other.cost + item.cost;
    }
    other.share = total > 0 ? other.total_tokens / total * 100 : null;
    rows.push(other);
  }
  return {rows,total};
}

// 日桶是项目本地日期，不进行 UTC 转换；小时桶使用响应时区，Local 则保留 API 的本地钟面。
export function formatComparisonBucket(bucket: string, granularity: 'hourly' | 'daily', timezone: string | undefined, locale: string, full = false): string {
  const literal = bucket.match(/^(\d{4}-\d{2}-\d{2})(?:T(\d{2}:\d{2}))?/);
  if (!literal) return bucket;
  const fallback = granularity === 'hourly' && !full ? literal[2] ?? bucket : `${literal[1]} ${granularity === 'hourly' ? literal[2] ?? '' : ''}`.trim();
  const date = new Date(granularity === 'daily' ? `${literal[1]}T12:00:00Z` : bucket);
  try {
    if (granularity === 'hourly' && (!timezone || timezone === 'Local')) return fallback;
    return new Intl.DateTimeFormat(locale, {
      timeZone: granularity === 'daily' ? 'UTC' : timezone,
      ...(full ? {year: 'numeric' as const} : {}),
      ...(granularity === 'daily' || full ? {month: 'short' as const, day: 'numeric' as const} : {}),
      ...(granularity === 'hourly' ? {hour: '2-digit' as const, minute: '2-digit' as const, hourCycle: 'h23' as const} : {}),
    }).format(date);
  } catch {
    return fallback;
  }
}
