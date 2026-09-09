import {
  parseStoredUsageRangeState,
  serializeUsageRangeState,
  type StoredUsageRangeState,
} from '@/utils/usage/customRange';

export const KEY_VIEWER_TIME_RANGE_STORAGE_KEY = 'cli-proxy-key-viewer-time-range-v1';

interface StorageLike {
  getItem: (key: string) => string | null;
  setItem: (key: string, value: string) => void;
}

const resolveStorage = (storage?: StorageLike): StorageLike | undefined => {
  if (storage) return storage;
  try {
    if (typeof localStorage === 'undefined') return undefined;
    return localStorage;
  } catch {
    // sandbox iframe 或隐私模式可能在读取 localStorage 时直接抛出异常。
    return undefined;
  }
};

const defaultRangeState = (): StoredUsageRangeState => ({ range: 'today' });

/** API Key 的各个只读页面共用同一时间范围。 */
export const loadKeyViewerTimeRange = (
  storage?: StorageLike,
  nowMs = Date.now(),
): StoredUsageRangeState => {
  const target = resolveStorage(storage);
  if (!target) return defaultRangeState();

  try {
    const sharedRaw = target.getItem(KEY_VIEWER_TIME_RANGE_STORAGE_KEY);
    if (sharedRaw) return parseStoredUsageRangeState(sharedRaw, { nowMs });
  } catch {
    // 忽略浏览器存储不可用；页面仍使用默认范围。
  }
  return defaultRangeState();
};

export const persistKeyViewerTimeRange = (
  state: StoredUsageRangeState,
  storage?: StorageLike,
): void => {
  const target = resolveStorage(storage);
  if (!target) return;
  try {
    target.setItem(KEY_VIEWER_TIME_RANGE_STORAGE_KEY, serializeUsageRangeState(state));
  } catch {
    // 忽略浏览器存储不可用；页面仍使用当前内存状态。
  }
};
