import { describe, expect, it, vi } from 'vitest';
import {
  ANALYSIS_COMPOSITION_ITEM_LIMIT_STORAGE_KEY,
  DEFAULT_ANALYSIS_COMPOSITION_ITEM_LIMIT,
  loadAnalysisCompositionItemLimit,
  parseAnalysisCompositionItemLimit,
  persistAnalysisCompositionItemLimit,
} from '../analysisPreferences';

describe('analysis composition item limit preferences', () => {
  it('parses zero and non-negative safe integers', () => {
    expect(parseAnalysisCompositionItemLimit('0')).toBe(0);
    expect(parseAnalysisCompositionItemLimit(' 12 ')).toBe(12);
    expect(parseAnalysisCompositionItemLimit('005')).toBe(5);
    expect(parseAnalysisCompositionItemLimit(8)).toBe(8);
  });

  it.each([
    null,
    undefined,
    '',
    ' ',
    '-1',
    '1.5',
    '1e3',
    Number.NaN,
    Number.POSITIVE_INFINITY,
    Number.MAX_SAFE_INTEGER + 1,
  ])('rejects invalid limit %s', (value) => {
    expect(parseAnalysisCompositionItemLimit(value)).toBeNull();
  });

  it('loads a valid stored value and falls back for missing or damaged values', () => {
    expect(loadAnalysisCompositionItemLimit({ getItem: () => '0' })).toBe(0);
    expect(loadAnalysisCompositionItemLimit({ getItem: () => '17' })).toBe(17);
    expect(loadAnalysisCompositionItemLimit({ getItem: () => null })).toBe(DEFAULT_ANALYSIS_COMPOSITION_ITEM_LIMIT);
    expect(loadAnalysisCompositionItemLimit({ getItem: () => '-2' })).toBe(DEFAULT_ANALYSIS_COMPOSITION_ITEM_LIMIT);
  });

  it('falls back when storage cannot be read', () => {
    expect(loadAnalysisCompositionItemLimit({
      getItem: () => {
        throw new Error('blocked');
      },
    })).toBe(DEFAULT_ANALYSIS_COMPOSITION_ITEM_LIMIT);
  });

  it('persists valid values and ignores invalid values or storage failures', () => {
    const setItem = vi.fn();
    persistAnalysisCompositionItemLimit(0, { setItem });
    persistAnalysisCompositionItemLimit(23, { setItem });
    persistAnalysisCompositionItemLimit(-1, { setItem });
    persistAnalysisCompositionItemLimit(Number.NaN, { setItem });

    expect(setItem).toHaveBeenNthCalledWith(1, ANALYSIS_COMPOSITION_ITEM_LIMIT_STORAGE_KEY, '0');
    expect(setItem).toHaveBeenNthCalledWith(2, ANALYSIS_COMPOSITION_ITEM_LIMIT_STORAGE_KEY, '23');
    expect(setItem).toHaveBeenCalledTimes(2);
    expect(() => persistAnalysisCompositionItemLimit(5, {
      setItem: () => {
        throw new Error('blocked');
      },
    })).not.toThrow();
  });
});
