import { useCallback, useState } from 'react';
import type { UsageActivityRequest, UsageActivityWindow, UsageRangeRequest } from '@/lib/types';
import type { UsageRangeQuery } from '@/utils/usage/rangeQuery';

interface ManualActivityWindowSelection {
  queryIdentity: string;
  window: UsageActivityWindow | null;
}

export interface UseRecentActivityWindowReturn {
  request: UsageActivityRequest;
  manualWindow: UsageActivityWindow | null;
  setWindow: (window: UsageActivityWindow) => void;
}

const buildTimeQueryIdentity = (query: UsageRangeRequest): string => (
  `${query.range}:${query.unit ?? ''}:${query.start ?? ''}:${query.end ?? ''}`
);

export function useRecentActivityWindow(query: UsageRangeQuery): UseRecentActivityWindowReturn {
  const queryIdentity = buildTimeQueryIdentity(query);
  const [selection, setSelection] = useState<ManualActivityWindowSelection>(() => ({
    queryIdentity,
    window: null,
  }));

  // 在当前 render 内失效旧选择，避免先用旧手动窗口发起一次请求。
  if (selection.queryIdentity !== queryIdentity) {
    setSelection({ queryIdentity, window: null });
  }
  const manualWindow = selection.queryIdentity === queryIdentity ? selection.window : null;

  const request: UsageActivityRequest = (
    manualWindow
      ? { window: manualWindow }
      : query.range === 'today' || query.range === 'yesterday'
        ? { window: query.range }
      : query.range === 'custom'
        ? { range: query.range, unit: query.unit, start: query.start, end: query.end }
        : { range: query.range }
  );

  const setWindow = useCallback((window: UsageActivityWindow) => {
    setSelection({ queryIdentity, window });
  }, [queryIdentity]);

  return { request, manualWindow, setWindow };
}
