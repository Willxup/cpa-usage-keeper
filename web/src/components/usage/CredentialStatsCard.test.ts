import { describe, expect, it } from 'vitest';
import type { UsageIdentity } from '@/lib/types';
import { buildCredentialRows, getTopCredentialRows } from './CredentialStatsCard';

const usageIdentity = (overrides: Partial<UsageIdentity>): UsageIdentity => ({
  id: 1,
  name: '',
  auth_type: 1,
  auth_type_name: 'oauth',
  identity: '',
  type: '',
  provider: '',
  total_requests: 0,
  success_count: 0,
  failure_count: 0,
  input_tokens: 0,
  output_tokens: 0,
  reasoning_tokens: 0,
  cached_tokens: 0,
  total_tokens: 0,
  last_aggregated_usage_event_id: 0,
  is_deleted: false,
  created_at: '2026-05-04T00:00:00Z',
  updated_at: '2026-05-04T00:00:00Z',
  ...overrides,
});

describe('CredentialStatsCard helpers', () => {
  it('sorts credentials by total request count descending', () => {
    const credentials = [
      usageIdentity({
        id: 1,
        identity: 'low',
        success_count: 1,
        total_requests: 1,
      }),
      usageIdentity({
        id: 2,
        name: 'High Provider',
        auth_type: 2,
        auth_type_name: 'apikey',
        identity: 'sk-a***1234',
        type: 'claude',
        success_count: 8,
        failure_count: 2,
        total_requests: 10,
      }),
    ] satisfies UsageIdentity[];

    const rows = buildCredentialRows(credentials);

    expect(rows.map((row) => row.displayName)).toEqual(['High Provider', 'low']);
    expect(rows[0]).toMatchObject({
      success: 8,
      failure: 2,
      total: 10,
      successRate: 80,
    });
  });

  it('prefers identity type over auth type name for the credential tag', () => {
    const credentials = [
      usageIdentity({
        auth_type_name: 'apikey',
        identity: 'sk-a***1234',
        type: 'openai',
      }),
    ] satisfies UsageIdentity[];

    const rows = buildCredentialRows(credentials);

    expect(rows[0].type).toBe('openai');
  });

  it('falls back to success plus failure when total count is empty', () => {
    const credentials = [
      usageIdentity({
        identity: 'fallback-total',
        success_count: 3,
        failure_count: 2,
        total_requests: 0,
      }),
    ] satisfies UsageIdentity[];

    const rows = buildCredentialRows(credentials);

    expect(rows[0].total).toBe(5);
    expect(rows[0].successRate).toBe(60);
  });

  it('returns only the top 10 non-empty credential rows', () => {
    const credentials = [
      usageIdentity({
        id: 1,
        identity: 'empty',
      }),
      ...Array.from({ length: 12 }, (_, index) => usageIdentity({
        id: index + 2,
        identity: `credential-${index + 1}`,
        success_count: index + 1,
        total_requests: index + 1,
      })),
    ] satisfies UsageIdentity[];

    const rows = buildCredentialRows(credentials);
    const topRows = getTopCredentialRows(rows);

    expect(topRows).toHaveLength(10);
    expect(topRows[0].displayName).toBe('credential-12');
    expect(topRows[9].displayName).toBe('credential-3');
    expect(topRows.some((row) => row.displayName === 'empty')).toBe(false);
  });
});
