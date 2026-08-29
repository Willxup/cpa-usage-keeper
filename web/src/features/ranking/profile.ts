export type RankingProfileError = 'required' | 'too_long' | 'characters' | 'phone';

export interface NormalizedRankingDisplayName {
  value: string;
  error: RankingProfileError | null;
}

export const RANKING_DISPLAY_NAME_MAX_LENGTH = 16;

const unicodeDecimalDigit = /\p{Nd}/u;

const containsPhonePattern = (value: string): boolean => {
  let digits = 0;
  for (const character of value) {
    if (unicodeDecimalDigit.test(character)) {
      digits += 1;
      if (digits >= 10) return true;
      continue;
    }
    if (digits > 0 && (character === '-' || character === '_')) continue;
    digits = 0;
  }
  return false;
};

export const normalizeRankingDisplayName = (rawValue: string): NormalizedRankingDisplayName => {
  const value = rawValue.normalize('NFKC').trim();
  if (!value) return { value, error: 'required' };
  if ([...value].length > RANKING_DISPLAY_NAME_MAX_LENGTH || new TextEncoder().encode(value).length > 128) {
    return { value, error: 'too_long' };
  }
  if (!/^[\p{L}\p{N}_-]+$/u.test(value) || !/[\p{L}\p{N}]/u.test(value)) {
    return { value, error: 'characters' };
  }
  if (containsPhonePattern(value)) return { value, error: 'phone' };
  return { value, error: null };
};
