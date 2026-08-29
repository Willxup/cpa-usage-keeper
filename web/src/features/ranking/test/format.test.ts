import { describe, expect, it } from 'vitest';
import { formatLeaderboardValue, formatOverallMetricValue } from '../format';
import type { RankingLeaderboardEntry } from '../types';

const entry = (value: number, overrides: Partial<RankingLeaderboardEntry> = {}): RankingLeaderboardEntry => ({
  rank: 1,
  participant_id: 'p_hidden',
  display_name: 'Keeper_01',
  avatar_id: 7,
  value,
  ...overrides,
});

describe('ranking value formatting', () => {
  it('uses compact English units for count and token metrics', () => {
    expect(formatLeaderboardValue('total_tokens', entry(999))).toBe('999');
    expect(formatLeaderboardValue('total_tokens', entry(1_500))).toBe('1.50K');
    expect(formatLeaderboardValue('request_count', entry(2_500_000))).toBe('2.50M');
    expect(formatLeaderboardValue('total_tokens', entry(3_200_000_000))).toBe('3.20B');
  });

  it('derives rates and durations from their exact numerators when available', () => {
    expect(formatLeaderboardValue('cache_read_rate', entry(0, {
      rate_numerator: 4,
      rate_denominator: 5,
    }))).toBe('80.00%');
    expect(formatLeaderboardValue('ttft_average', entry(0, {
      rate_numerator: 250,
      rate_denominator: 2,
    }))).toBe('125ms');
    expect(formatLeaderboardValue('latency_average', entry(0, {
      rate_numerator: 5_000,
      rate_denominator: 2,
    }))).toBe('2.50s');
  });

  it('converts five-minute peaks to per-minute values and overall score to points', () => {
    expect(formatLeaderboardValue('peak_tpm', entry(5_000))).toBe('1.00K');
    expect(formatLeaderboardValue('peak_rpm', entry(45))).toBe('9');
    expect(formatLeaderboardValue('overall', entry(9_325))).toBe('93.25 PTS');
    expect(formatLeaderboardValue('overall', entry(93), 'local')).toBe('93 PTS');
  });

  it('formats overall supporting metrics without exposing a participant identifier', () => {
    const overall = entry(9_325, { metrics: { total_tokens: 1_500, cache_read_rate: 825_000 } });
    expect(formatOverallMetricValue('total_tokens', overall)).toBe('1.50K');
    expect(formatOverallMetricValue('cache_read_rate', overall)).toBe('82.50%');
    expect(formatOverallMetricValue('peak_rpm', overall)).toBe('—');
  });
});
