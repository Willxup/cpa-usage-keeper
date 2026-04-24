import { describe, expect, it } from 'vitest';
import type { UsageCredential } from '@/lib/types';
import { buildCredentialRows, getTopCredentialRows } from './CredentialStatsCard';

describe('CredentialStatsCard helpers', () => {
  it('sorts credentials by total request count descending', () => {
    const credentials: UsageCredential[] = [
      {
        source: 'low',
        source_key: 'low',
        success_count: 1,
        failure_count: 0,
        total_count: 1,
      },
      {
        source: 'high',
        source_key: 'high',
        success_count: 8,
        failure_count: 2,
        total_count: 10,
      },
    ];

    const rows = buildCredentialRows(credentials);

    expect(rows.map((row) => row.displayName)).toEqual(['high', 'low']);
    expect(rows[0]).toMatchObject({
      success: 8,
      failure: 2,
      total: 10,
      successRate: 80,
    });
  });

  it('falls back to success plus failure when total count is empty', () => {
    const rows = buildCredentialRows([
      {
        source: 'fallback-total',
        source_key: 'fallback-total',
        success_count: 3,
        failure_count: 2,
        total_count: 0,
      },
    ]);

    expect(rows[0].total).toBe(5);
    expect(rows[0].successRate).toBe(60);
  });

  it('returns only the top 10 non-empty credential rows', () => {
    const credentials: UsageCredential[] = [
      {
        source: 'empty',
        source_key: 'empty',
        success_count: 0,
        failure_count: 0,
        total_count: 0,
      },
      ...Array.from({ length: 12 }, (_, index) => ({
        source: `credential-${index + 1}`,
        source_key: `credential-${index + 1}`,
        success_count: index + 1,
        failure_count: 0,
        total_count: index + 1,
      })),
    ];

    const rows = buildCredentialRows(credentials);
    const topRows = getTopCredentialRows(rows);

    expect(topRows).toHaveLength(10);
    expect(topRows[0].displayName).toBe('credential-12');
    expect(topRows[9].displayName).toBe('credential-3');
    expect(topRows.some((row) => row.displayName === 'empty')).toBe(false);
  });
});
