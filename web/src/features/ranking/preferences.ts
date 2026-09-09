import { RANKING_METRICS, RANKING_PERIODS, type RankingMetric, type RankingPeriod } from './types';

export const RANKING_PREFERENCES_STORAGE_KEY = 'cli-proxy-usage-ranking-preferences-v1';
export const RANKING_PREFERENCES_VERSION = 1;

export const DEFAULT_RANKING_PERIOD: RankingPeriod = 'today';
export const DEFAULT_RANKING_METRIC: RankingMetric = 'overall';

export interface RankingPreferences {
  period: RankingPeriod;
  metric: RankingMetric;
}

interface RankingPreferencesStorage {
  getItem: (key: string) => string | null;
  setItem: (key: string, value: string) => void;
}

const defaultStorage = (): RankingPreferencesStorage | undefined => (
  typeof localStorage === 'undefined' ? undefined : localStorage
);

export const defaultRankingPreferences = (): RankingPreferences => ({
  period: DEFAULT_RANKING_PERIOD,
  metric: DEFAULT_RANKING_METRIC,
});

export const normalizeRankingPeriod = (value: unknown): RankingPeriod | null => (
  typeof value === 'string' && RANKING_PERIODS.includes(value as RankingPeriod)
    ? value as RankingPeriod
    : null
);

export const normalizeRankingMetric = (value: unknown): RankingMetric | null => (
  typeof value === 'string' && RANKING_METRICS.includes(value as RankingMetric)
    ? value as RankingMetric
    : null
);

export const normalizeRankingPreferences = (value: unknown): RankingPreferences => {
  // period/metric 会直接拼进排行榜请求，读回来必须按枚举白名单校验。
  const stored = typeof value === 'object' && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {};
  if (stored.version !== RANKING_PREFERENCES_VERSION) {
    return defaultRankingPreferences();
  }
  return {
    period: normalizeRankingPeriod(stored.period) ?? DEFAULT_RANKING_PERIOD,
    metric: normalizeRankingMetric(stored.metric) ?? DEFAULT_RANKING_METRIC,
  };
};

export const loadRankingPreferences = (
  storage: RankingPreferencesStorage | undefined = defaultStorage(),
): RankingPreferences => {
  try {
    const raw = storage?.getItem(RANKING_PREFERENCES_STORAGE_KEY);
    if (!raw) {
      return defaultRankingPreferences();
    }
    return normalizeRankingPreferences(JSON.parse(raw));
  } catch {
    return defaultRankingPreferences();
  }
};

export const persistRankingPreferences = (
  patch: Partial<RankingPreferences>,
  storage: RankingPreferencesStorage | undefined = defaultStorage(),
): boolean => {
  try {
    if (!storage) {
      return false;
    }
    // period 与 metric 分开切换，先读回当前值再合并，避免互相覆盖。
    const next = { ...loadRankingPreferences(storage), ...patch };
    storage.setItem(RANKING_PREFERENCES_STORAGE_KEY, JSON.stringify({
      version: RANKING_PREFERENCES_VERSION,
      ...normalizeRankingPreferences({ version: RANKING_PREFERENCES_VERSION, ...next }),
    }));
    return true;
  } catch {
    return false;
  }
};
