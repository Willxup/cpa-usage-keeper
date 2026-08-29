import { describe, expect, it } from 'vitest';
import { normalizeRankingDisplayName } from '../profile';

describe('ranking profile names', () => {
  it('normalizes compatible Unicode and accepts letters, numbers, hyphens, and underscores', () => {
    expect(normalizeRankingDisplayName('  Ｋeeper_中-1  ')).toEqual({
      value: 'Keeper_中-1',
      error: null,
    });
  });

  it('accepts 16 Unicode characters and rejects the 17th character', () => {
    expect(normalizeRankingDisplayName('abcdefghijklmnop')).toMatchObject({ error: null });
    expect(normalizeRankingDisplayName('abcdefghijklmnopq')).toMatchObject({ error: 'too_long' });
  });

  it.each([
    ['', 'required'],
    ['Keep Name', 'characters'],
    ['Keep.Name', 'characters'],
    ['Keeper😀', 'characters'],
    ['___', 'characters'],
    ['1234567890', 'phone'],
    ['١٢٣٤٥٦٧٨٩٠', 'phone'],
    ['a'.repeat(17), 'too_long'],
  ])('rejects %j with %s', (name, error) => {
    expect(normalizeRankingDisplayName(name)).toMatchObject({ error });
  });
});
