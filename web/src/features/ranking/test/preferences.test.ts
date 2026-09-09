import { describe, expect, it, vi } from 'vitest';
import {
  DEFAULT_RANKING_METRIC,
  DEFAULT_RANKING_PERIOD,
  defaultRankingPreferences,
  loadRankingPreferences,
  normalizeRankingMetric,
  normalizeRankingPeriod,
  normalizeRankingPreferences,
  persistRankingPreferences,
  RANKING_PREFERENCES_STORAGE_KEY,
} from '../preferences';

const createStorage = (value: string | null = null) => ({
  getItem: vi.fn(() => value),
  setItem: vi.fn(),
});

const storedValue = (storage: ReturnType<typeof createStorage>) => (
  JSON.parse(String(storage.setItem.mock.calls.at(-1)?.[1]))
);

describe('ranking period and metric persistence', () => {
  it('keeps only values the leaderboard request accepts', () => {
    expect(normalizeRankingPeriod('previous_month')).toBe('previous_month');
    expect(normalizeRankingPeriod('last_week')).toBeNull();
    expect(normalizeRankingMetric('peak_tpm')).toBe('peak_tpm');
    expect(normalizeRankingMetric('total_cost')).toBeNull();
  });

  it('restores a saved selection', () => {
    const storage = createStorage(JSON.stringify({
      version: 1,
      period: 'current_month',
      metric: 'cache_read_rate',
    }));

    expect(loadRankingPreferences(storage)).toEqual({
      period: 'current_month',
      metric: 'cache_read_rate',
    });
    expect(storage.getItem).toHaveBeenCalledWith(RANKING_PREFERENCES_STORAGE_KEY);
  });

  it('falls back per field for damaged records', () => {
    expect(normalizeRankingPreferences({
      version: 1,
      period: 'yesterday',
      metric: 'total_cost',
    })).toEqual({ period: 'yesterday', metric: DEFAULT_RANKING_METRIC });
    expect(normalizeRankingPreferences({
      version: 1,
      period: 42,
      metric: 'peak_rpm',
    })).toEqual({ period: DEFAULT_RANKING_PERIOD, metric: 'peak_rpm' });
  });

  it('resets when the stored version is missing or stale', () => {
    expect(normalizeRankingPreferences({ period: 'yesterday' })).toEqual(defaultRankingPreferences());
    expect(normalizeRankingPreferences({ version: 0, period: 'yesterday' })).toEqual(defaultRankingPreferences());
    expect(normalizeRankingPreferences('not an object')).toEqual(defaultRankingPreferences());
  });

  it('defaults to today/overall for unusable storage', () => {
    expect(loadRankingPreferences(createStorage('not json'))).toEqual(defaultRankingPreferences());
    expect(loadRankingPreferences({
      getItem: () => { throw new Error('blocked'); },
      setItem: vi.fn(),
    })).toEqual(defaultRankingPreferences());
  });

  it('keeps the untouched field when only one selector changes', () => {
    const storage = createStorage(JSON.stringify({
      version: 1,
      period: 'previous_month',
      metric: 'ttft_average',
    }));

    expect(persistRankingPreferences({ metric: 'request_count' }, storage)).toBe(true);
    expect(storedValue(storage)).toEqual({
      version: 1,
      period: 'previous_month',
      metric: 'request_count',
    });
  });

  it('reports a failed write without throwing', () => {
    expect(persistRankingPreferences({ period: 'today' }, {
      getItem: vi.fn(() => null),
      setItem: () => { throw new Error('blocked'); },
    })).toBe(false);
  });
});
