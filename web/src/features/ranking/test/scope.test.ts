import { describe, expect, it, vi } from 'vitest';
import {
  DEFAULT_RANKING_SCOPE,
  loadRankingScope,
  normalizeRankingScope,
  persistRankingScope,
  RANKING_SCOPE_STORAGE_KEY,
} from '../scope';

const createStorage = (value: string | null = null) => ({
  getItem: vi.fn(() => value),
  setItem: vi.fn(),
});

describe('ranking scope persistence', () => {
  it('keeps only local and community values', () => {
    expect(normalizeRankingScope('local')).toBe('local');
    expect(normalizeRankingScope('community')).toBe('community');
    expect(normalizeRankingScope('unexpected')).toBeNull();
  });

  it('defaults to community and ignores unavailable storage', () => {
    expect(loadRankingScope(undefined)).toBe(DEFAULT_RANKING_SCOPE);
    expect(loadRankingScope(createStorage('broken'))).toBe('community');
    expect(loadRankingScope({
      getItem: () => { throw new Error('blocked'); },
      setItem: vi.fn(),
    })).toBe('community');
  });

  it('stores the last explicit selection without failing the page', () => {
    const storage = createStorage();
    expect(persistRankingScope('local', storage)).toBe(true);
    expect(storage.setItem).toHaveBeenCalledWith(RANKING_SCOPE_STORAGE_KEY, 'local');
    expect(persistRankingScope('community', {
      getItem: vi.fn(),
      setItem: () => { throw new Error('blocked'); },
    })).toBe(false);
  });
});
