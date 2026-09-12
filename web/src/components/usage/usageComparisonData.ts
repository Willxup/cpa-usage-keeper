import type { UsageComparisonItem } from '@/lib/types';

export interface ComparisonRow extends UsageComparisonItem {
  value: number;
  share: number | null;
  other?: boolean;
}
export interface ComparisonRect {
  row: ComparisonRow;
  x: number;
  y: number;
  width: number;
  height: number;
}

const LIMIT = 5;
const SUM_FIELDS = ['requests', 'failures', 'input_tokens', 'output_tokens', 'cache_read_tokens', 'cache_creation_tokens', 'reasoning_tokens', 'total_tokens'] as const;

export function buildComparisonView(items: UsageComparisonItem[], othersLabel: string) {
  const ranked = [...items].sort((a,b) => b.total_tokens - a.total_tokens || a.key.localeCompare(b.key));
  const total = ranked.reduce((sum,item) => sum + item.total_tokens,0);
  const rows: ComparisonRow[] = ranked.slice(0,LIMIT).map(item => ({
    ...item, value:item.total_tokens, share:total > 0 ? item.total_tokens / total * 100 : null,
  }));
  // 排名与面积固定按 Token；“其他”同时合并 Tooltip/列表指标，任何缺价都保留为未知。
  const tail = ranked.slice(LIMIT);
  if (tail.length > 0) {
    const other: ComparisonRow = {
      key:'__comparison_others__', label:othersLabel, other:true, value:0, share:null,
      requests:0, failures:0, input_tokens:0, output_tokens:0, cache_read_tokens:0,
      cache_creation_tokens:0, reasoning_tokens:0, total_tokens:0, cost:0,
    };
    for (const item of tail) {
      for (const field of SUM_FIELDS) other[field] += item[field];
      other.cost = other.cost === null || item.cost === null ? null : other.cost + item.cost;
    }
    other.value = other.total_tokens;
    other.share = total > 0 ? other.total_tokens / total * 100 : null;
    rows.push(other);
  }
  return {rows,total};
}

// 六个可见分类以内采用平衡二分，不引入图表依赖；矩形真实面积保持与用量成比例。
export function layoutComparisonTreemap(rows: ComparisonRow[], aspect = 1): ComparisonRect[] {
  const result: ComparisonRect[] = [];
  const positive = rows.filter(row => row.value > 0);
  const split = (items: ComparisonRow[], x: number, y: number, width: number, height: number) => {
    if (items.length === 0) return;
    if (items.length === 1) { result.push({row:items[0], x, y, width, height}); return; }
    const total = items.reduce((sum, row) => sum + row.value, 0);
    let cut = 1, sum = items[0].value;
    while (cut < items.length - 1 && Math.abs(sum + items[cut].value - total / 2) < Math.abs(sum - total / 2)) {
      sum += items[cut++].value;
    }
    const ratio = sum / total;
    if (width * aspect >= height) {
      split(items.slice(0, cut), x, y, width * ratio, height);
      split(items.slice(cut), x + width * ratio, y, width * (1 - ratio), height);
    } else {
      split(items.slice(0, cut), x, y, width, height * ratio);
      split(items.slice(cut), x, y + height * ratio, width, height * (1 - ratio));
    }
  };
  split(positive, 0, 0, 100, 100);
  return result;
}
