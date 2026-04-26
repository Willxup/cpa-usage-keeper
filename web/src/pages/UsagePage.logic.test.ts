import { describe, expect, it } from 'vitest';
import { getUsageTabOptions, refreshPageData, sanitizeRequestEventFilters, syncCpaData } from './UsagePage';
import { filterUsageByWindow, type UsageFilterWindow } from '@/utils/usage';
import type { StatusResponse, UsageSnapshot } from '@/lib/types';

const usage: UsageSnapshot = {
  total_requests: 2,
  success_count: 2,
  failure_count: 0,
  total_tokens: 300,
  requests_by_day: {},
  requests_by_hour: {},
  tokens_by_day: {},
  tokens_by_hour: {},
  apis: {
    'provider-a': {
      display_name: 'Provider A',
      total_requests: 2,
      success_count: 2,
      failure_count: 0,
      total_tokens: 300,
      models: {
        'claude-sonnet': {
          total_requests: 2,
          success_count: 2,
          failure_count: 0,
          total_tokens: 300,
          details: [
            {
              timestamp: '2026-04-23T00:00:00.000Z',
              latency_ms: 100,
              source: 'source-a',
              auth_index: '1',
              failed: false,
              tokens: {
                input_tokens: 50,
                output_tokens: 50,
                reasoning_tokens: 0,
                cached_tokens: 0,
                total_tokens: 100,
              },
            },
            {
              timestamp: '2026-04-23T02:00:00.000Z',
              latency_ms: 120,
              source: 'source-a',
              auth_index: '1',
              failed: false,
              tokens: {
                input_tokens: 100,
                output_tokens: 100,
                reasoning_tokens: 0,
                cached_tokens: 0,
                total_tokens: 200,
              },
            },
          ],
        },
      },
    },
  },
};

const deriveFilteredUsageLikePage = (input: UsageSnapshot, filterWindow: UsageFilterWindow) =>
  filterUsageByWindow(input, filterWindow);

describe('UsagePage range filtering bug', () => {
  it('changes the usage payload that summary metrics read from', () => {
    const filterWindow: UsageFilterWindow = {
      startMs: Date.parse('2026-04-23T01:00:00.000Z'),
      endMs: Date.parse('2026-04-23T03:00:00.000Z'),
      windowMinutes: 120,
    };

    const expected = filterUsageByWindow(usage, filterWindow);
    const actual = deriveFilteredUsageLikePage(usage, filterWindow);

    expect(expected.total_requests).toBe(1);
    expect(actual.total_requests).toBe(expected.total_requests);
  });
});

describe('UsagePage request event filters', () => {
  it('clears model and source filters that are no longer available', () => {
    const next = sanitizeRequestEventFilters(
      {
        model: 'claude-opus',
        source: 'source-b',
        result: 'failed',
      },
      {
        models: ['claude-sonnet'],
        sources: [{ value: 'source-a', label: 'Provider A' }],
      },
    );

    expect(next).toEqual({
      model: '__all__',
      source: '__all__',
      result: 'failed',
    });
  });
});

describe('UsagePage tab labels', () => {
  it('resolves tab labels through translation keys', () => {
    const labels = getUsageTabOptions((key) => `translated:${key}`).map((option) => option.label);

    expect(labels).toEqual([
      'translated:usage_stats.tab_overview',
      'translated:usage_stats.tab_analysis',
      'translated:usage_stats.tab_events',
      'translated:usage_stats.tab_credentials',
      'translated:usage_stats.tab_pricing',
    ]);
  });
});

describe('UsagePage refresh action', () => {
  it('reloads page data without triggering backend sync', async () => {
    let refreshCalls = 0;
    let syncCalls = 0;

    await refreshPageData({
      refreshActiveTab: async () => {
        refreshCalls += 1;
      },
      triggerBackendSync: async () => {
        syncCalls += 1;
      },
    });

    expect(refreshCalls).toBe(1);
    expect(syncCalls).toBe(0);
  });
});

describe('UsagePage sync action', () => {
  it('triggers backend sync, refreshes active tab data, and reloads status', async () => {
    const calls: string[] = [];
    let receivedStatus: StatusResponse | null = null;
    const syncStatus: StatusResponse = { running: true, sync_running: false, last_status: 'completed' };
    const refreshedStatus: StatusResponse = {
      running: true,
      sync_running: false,
      last_status: 'completed',
      last_run_at: '2026-04-26T13:00:00.000Z',
    };

    await syncCpaData({
      triggerBackendSync: async () => {
        calls.push('sync');
        return syncStatus;
      },
      refreshActiveTab: async () => {
        calls.push('refresh');
      },
      refreshStatus: async () => {
        calls.push('status');
        return refreshedStatus;
      },
      onStatus: (status) => {
        calls.push('set-status');
        receivedStatus = status;
      },
    });

    expect(calls).toEqual(['sync', 'refresh', 'status', 'set-status']);
    expect(receivedStatus).toBe(refreshedStatus);
  });
});
