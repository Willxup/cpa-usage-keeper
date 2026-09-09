import { describe, expect, it } from 'vitest';
import type { StoredUsageRangeState } from '@/utils/usage/customRange';
import {
  KEY_VIEWER_TIME_RANGE_STORAGE_KEY,
  loadKeyViewerTimeRange,
  persistKeyViewerTimeRange,
} from '../timeRange';

type MemoryStorage = {
  values: Map<string, string>;
  getItem: (key: string) => string | null;
  setItem: (key: string, value: string) => void;
};

const createStorage = (): MemoryStorage => {
  const values = new Map<string, string>();
  return {
    values,
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
  };
};

describe('API Key viewer time range persistence', () => {
  it('uses one storage entry for Overview and Analysis', () => {
    const storage = createStorage();
    const selected: StoredUsageRangeState = { range: '7d' };

    persistKeyViewerTimeRange(selected, storage);

    expect(storage.values.has(KEY_VIEWER_TIME_RANGE_STORAGE_KEY)).toBe(true);
    expect(loadKeyViewerTimeRange(storage)).toEqual(selected);
  });

  it('ignores page-specific storage entries when the shared entry is absent', () => {
    const storage = createStorage();
    storage.setItem('cli-proxy-key-overview-range-v1', JSON.stringify({ range: '7d' }));

    expect(loadKeyViewerTimeRange(storage)).toEqual({ range: 'today' });
    expect(storage.values.has(KEY_VIEWER_TIME_RANGE_STORAGE_KEY)).toBe(false);
  });

  it('falls back safely when the browser blocks localStorage access', () => {
    const originalDescriptor = Object.getOwnPropertyDescriptor(globalThis, 'localStorage');
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      get: () => {
        throw new Error('storage blocked');
      },
    });

    try {
      expect(loadKeyViewerTimeRange()).toEqual({ range: 'today' });
      expect(() => persistKeyViewerTimeRange({ range: '7d' })).not.toThrow();
    } finally {
      if (originalDescriptor) {
        Object.defineProperty(globalThis, 'localStorage', originalDescriptor);
      } else {
        delete (globalThis as { localStorage?: unknown }).localStorage;
      }
    }
  });
});
