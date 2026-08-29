import { RANKING_SCOPES, type RankingScope } from './types';

export const RANKING_SCOPE_STORAGE_KEY = 'cli-proxy-usage-ranking-scope-v1';
export const DEFAULT_RANKING_SCOPE: RankingScope = 'community';

interface RankingScopeStorage {
  getItem: (key: string) => string | null;
  setItem: (key: string, value: string) => void;
}

export const normalizeRankingScope = (value: unknown): RankingScope | null => (
  typeof value === 'string' && RANKING_SCOPES.includes(value as RankingScope)
    ? value as RankingScope
    : null
);

export const loadRankingScope = (
  storage: RankingScopeStorage | undefined = typeof localStorage === 'undefined' ? undefined : localStorage,
): RankingScope => {
  try {
    return normalizeRankingScope(storage?.getItem(RANKING_SCOPE_STORAGE_KEY)) ?? DEFAULT_RANKING_SCOPE;
  } catch {
    return DEFAULT_RANKING_SCOPE;
  }
};

export const persistRankingScope = (
  scope: RankingScope,
  storage: RankingScopeStorage | undefined = typeof localStorage === 'undefined' ? undefined : localStorage,
): boolean => {
  try {
    storage?.setItem(RANKING_SCOPE_STORAGE_KEY, scope);
    return Boolean(storage);
  } catch {
    return false;
  }
};
