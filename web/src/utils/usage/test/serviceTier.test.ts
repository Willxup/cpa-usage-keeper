import { describe, expect, it } from 'vitest';
import i18n from '@/i18n';
import { formatServiceTier, formatServiceTierDetail } from '../serviceTier';

const labels = [
  ['auto', ['Auto', '自动', '自動']],
  ['default', ['Standard', '标准', '標準']],
  ['standard', ['Standard', '标准', '標準']],
  ['priority', ['Fast', '快速', '快速']],
  ['fast', ['Fast', '快速', '快速']],
  ['flex', ['Flex', '弹性', '彈性']],
  ['ultrafast', ['Ultrafast', '超快', '超高速']],
] as const;

describe.each([['en', 0], ['zh', 1], ['zh-TW', 2]] as const)('service tier display in %s', (language, index) => {
  const t = i18n.getFixedT(language);

  it.each(labels)('maps %s and retains the raw value in detail parentheses', (raw, localized) => {
    expect(formatServiceTier(raw, t)).toBe(localized[index]);
    expect(formatServiceTierDetail(raw, t)).toBe(`${localized[index]} (${raw})`);
  });

  it('matches aliases case-insensitively but preserves raw casing in parentheses', () => {
    const fast = ['Fast', '快速', '快速'][index];
    expect(formatServiceTier(' Priority ', t)).toBe(fast);
    expect(formatServiceTierDetail(' Priority ', t)).toBe(`${fast} (Priority)`);
  });
});

describe('language-independent service tier values', () => {
  const t = i18n.getFixedT('en');

  it.each([undefined, null, '', '  '])('shows missing value %j as a dash without parentheses', (raw) => {
    expect(formatServiceTier(raw, t)).toBe('-');
    expect(formatServiceTierDetail(raw, t)).toBe('-');
  });

  it.each(['CustomTier', '__proto__', '-'])('preserves unknown tier %s and the original detail format', (raw) => {
    expect(formatServiceTier(raw, t)).toBe(raw);
    expect(formatServiceTierDetail(raw, t)).toBe(`${raw} (${raw})`);
  });
});
