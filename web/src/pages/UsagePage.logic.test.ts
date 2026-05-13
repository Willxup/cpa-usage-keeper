import { afterEach, describe, expect, it, vi } from 'vitest';
import { buildCustomDateRangeQuery, getCustomDateRangeBounds, getOverviewChartEndMs, getOverviewDisplayLoading, getOverviewHourWindowHours, getPreferredOverviewChartPeriod, getTimeRangeOptions, getUsageTabOptions, isCustomDateWithinBounds, openDateInputPicker, refreshPageData, sanitizeRequestEventFilters, scheduleOverviewAutoRefresh, shouldAutoRefreshUsageTab, shouldShowRangeControls, shouldShowUpdateCheckButton, getUpdateCheckToastDuration, syncCpaData } from './UsagePage';
import { ApiError } from '@/lib/api';
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

const createAutoRefreshTestDocument = (visibilityState: DocumentVisibilityState = 'visible') => {
  const target = new EventTarget();
  return {
    get visibilityState() {
      return visibilityState;
    },
    setVisibilityState(nextVisibilityState: DocumentVisibilityState) {
      visibilityState = nextVisibilityState;
    },
    addEventListener: target.addEventListener.bind(target),
    removeEventListener: target.removeEventListener.bind(target),
    dispatchEvent: target.dispatchEvent.bind(target),
  };
};

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe('UsagePage Overview loading display', () => {
  it('keeps existing Overview data visible during background refresh', () => {
    expect(getOverviewDisplayLoading({ loading: true, hasUsage: true })).toBe(false);
  });

  it('shows loading before Overview data has loaded', () => {
    expect(getOverviewDisplayLoading({ loading: true, hasUsage: false })).toBe(true);
  });
});

describe('UsagePage update check controls', () => {
  it('hides the update button before status loads', () => {
    expect(shouldShowUpdateCheckButton(null)).toBe(false);
  });

  it('hides the update button for dev builds', () => {
    expect(shouldShowUpdateCheckButton({ updateCheckEnabled: false })).toBe(false);
  });

  it('shows the update button for release builds', () => {
    expect(shouldShowUpdateCheckButton({ updateCheckEnabled: true })).toBe(true);
  });

  it('keeps failure toasts visible longer than success toasts', () => {
    expect(getUpdateCheckToastDuration('success')).toBe(4_000);
    expect(getUpdateCheckToastDuration('info')).toBe(4_000);
    expect(getUpdateCheckToastDuration('error')).toBe(6_000);
  });
});

describe('UsagePage Overview auto-refresh', () => {
  it('refreshes the Overview tab every 10 seconds', () => {
    vi.useFakeTimers();
    const testDocument = createAutoRefreshTestDocument();
    const refreshOverview = vi.fn();

    const cleanup = scheduleOverviewAutoRefresh({ enabled: true, refreshOverview, documentRef: testDocument });

    vi.advanceTimersByTime(9_999);
    expect(refreshOverview).not.toHaveBeenCalled();

    vi.advanceTimersByTime(1);
    expect(refreshOverview).toHaveBeenCalledTimes(1);

    cleanup();
  });

  it('does not schedule refreshes outside the Overview tab', () => {
    vi.useFakeTimers();
    const refreshOverview = vi.fn();

    const cleanup = scheduleOverviewAutoRefresh({ enabled: false, refreshOverview });

    vi.advanceTimersByTime(10_000);
    expect(refreshOverview).not.toHaveBeenCalled();

    cleanup();
  });

  it('pauses while the browser tab is hidden', () => {
    vi.useFakeTimers();
    const testDocument = createAutoRefreshTestDocument('hidden');
    const refreshOverview = vi.fn();

    const cleanup = scheduleOverviewAutoRefresh({ enabled: true, refreshOverview, documentRef: testDocument });

    vi.advanceTimersByTime(10_000);
    expect(refreshOverview).not.toHaveBeenCalled();

    cleanup();
  });

  it('refreshes once when the browser tab becomes visible again', () => {
    vi.useFakeTimers();
    const testDocument = createAutoRefreshTestDocument('hidden');
    const refreshOverview = vi.fn();

    const cleanup = scheduleOverviewAutoRefresh({ enabled: true, refreshOverview, documentRef: testDocument });
    testDocument.setVisibilityState('visible');
    testDocument.dispatchEvent(new Event('visibilitychange'));

    expect(refreshOverview).toHaveBeenCalledTimes(1);

    cleanup();
  });

  it('restarts the interval cadence after refreshing on visibility restore', () => {
    vi.useFakeTimers();
    const testDocument = createAutoRefreshTestDocument('hidden');
    const refreshOverview = vi.fn();

    const cleanup = scheduleOverviewAutoRefresh({ enabled: true, refreshOverview, documentRef: testDocument });
    vi.advanceTimersByTime(9_999);
    testDocument.setVisibilityState('visible');
    testDocument.dispatchEvent(new Event('visibilitychange'));

    expect(refreshOverview).toHaveBeenCalledTimes(1);

    vi.advanceTimersByTime(1);
    expect(refreshOverview).toHaveBeenCalledTimes(1);

    vi.advanceTimersByTime(9_999);
    expect(refreshOverview).toHaveBeenCalledTimes(2);

    cleanup();
  });

  it('cleans up the interval and visibility listener', () => {
    vi.useFakeTimers();
    const testDocument = createAutoRefreshTestDocument();
    const refreshOverview = vi.fn();
    const cleanup = scheduleOverviewAutoRefresh({ enabled: true, refreshOverview, documentRef: testDocument });

    cleanup();
    vi.advanceTimersByTime(10_000);
    testDocument.dispatchEvent(new Event('visibilitychange'));

    expect(refreshOverview).not.toHaveBeenCalled();
  });
});

describe('UsagePage active tab auto-refresh guard', () => {
  it('allows Request Events auto-refresh only on the first page', () => {
    expect(shouldAutoRefreshUsageTab({ activeTab: 'events', eventsPage: 1, authFilePage: 1, aiProviderPage: 1 })).toBe(true);
    expect(shouldAutoRefreshUsageTab({ activeTab: 'events', eventsPage: 2, authFilePage: 1, aiProviderPage: 1 })).toBe(false);
  });

  it('allows Credentials auto-refresh only when both lists are on the first page', () => {
    expect(shouldAutoRefreshUsageTab({ activeTab: 'credentials', eventsPage: 1, authFilePage: 1, aiProviderPage: 1 })).toBe(true);
    expect(shouldAutoRefreshUsageTab({ activeTab: 'credentials', eventsPage: 1, authFilePage: 2, aiProviderPage: 1 })).toBe(false);
    expect(shouldAutoRefreshUsageTab({ activeTab: 'credentials', eventsPage: 1, authFilePage: 1, aiProviderPage: 2 })).toBe(false);
  });

  it('keeps Overview auto-refresh enabled and does not auto-refresh other tabs', () => {
    expect(shouldAutoRefreshUsageTab({ activeTab: 'overview', eventsPage: 2, authFilePage: 2, aiProviderPage: 2 })).toBe(true);
    expect(shouldAutoRefreshUsageTab({ activeTab: 'analysis', eventsPage: 1, authFilePage: 1, aiProviderPage: 1 })).toBe(false);
    expect(shouldAutoRefreshUsageTab({ activeTab: 'pricing', eventsPage: 1, authFilePage: 1, aiProviderPage: 1 })).toBe(false);
  });
});

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
        source: 'authidx-source-b',
        result: 'failed',
      },
      {
        models: ['claude-sonnet'],
        sources: [{ value: 'authidx-source-a', label: 'authidx-source-a' }],
      },
    );

    expect(next).toEqual({
      model: '__all__',
      source: '__all__',
      result: 'failed',
    });
  });

  it('keeps source filters that are still available after refreshing options', () => {
    const next = sanitizeRequestEventFilters(
      {
        model: 'claude-sonnet',
        source: 'authidx-source-a',
        result: 'success',
      },
      {
        models: ['claude-sonnet'],
        sources: [{ value: 'authidx-source-a', label: 'authidx-source-a' }],
      },
    );

    expect(next).toEqual({
      model: 'claude-sonnet',
      source: 'authidx-source-a',
      result: 'success',
    });
  });
});

for (const [tab, expected] of [
  ['overview', true],
  ['analysis', true],
  ['events', true],
  ['credentials', false],
  ['pricing', false],
] as const) {
  it(`returns ${expected} for ${tab} range controls visibility`, () => {
    expect(shouldShowRangeControls(tab)).toBe(expected);
  });
}

describe('UsagePage time range options', () => {
  it('includes rolling 24h, local Today, and 30d ranges', () => {
    const options = getTimeRangeOptions((key) => `translated:${key}`);

    expect(options.map((option) => option.value)).toEqual(['4h', '8h', '12h', '24h', 'today', '7d', '30d', 'custom']);
    expect(options.map((option) => option.label)).toContain('translated:usage_stats.range_24h');
    expect(options.map((option) => option.label)).toContain('translated:usage_stats.range_today');
    expect(options.map((option) => option.label)).toContain('translated:usage_stats.range_30d');
  });
});

describe('UsagePage Overview chart period preference', () => {
  it('keeps sub-day windows on By Hour', () => {
    expect(getPreferredOverviewChartPeriod({ windowMinutes: 12 * 60 })).toBe('hour');
  });

  it('uses By Day only for windows longer than one day without inspecting chart data', () => {
    expect(getPreferredOverviewChartPeriod({ windowMinutes: 24 * 60 })).toBe('hour');
    expect(getPreferredOverviewChartPeriod({ windowMinutes: (24 * 60) + 1 })).toBe('day');
    expect(getPreferredOverviewChartPeriod({ windowMinutes: 30 * 24 * 60 })).toBe('day');
  });
});

describe('UsagePage custom date input bounds', () => {
  it('limits selectable Custom dates to today through the first day of the previous month', () => {
    expect(getCustomDateRangeBounds(Date.parse('2026-05-13T12:00:00.000Z'), 'UTC')).toEqual({
      min: '2026-04-01',
      max: '2026-05-13',
    });
  });

  it('uses the project timezone when deriving Custom date bounds', () => {
    expect(getCustomDateRangeBounds(Date.parse('2026-05-13T06:30:00.000Z'), 'America/Los_Angeles')).toEqual({
      min: '2026-04-01',
      max: '2026-05-12',
    });
  });

  it('rejects tomorrow and dates before the first day of the previous month', () => {
    const bounds = { min: '2026-04-01', max: '2026-05-13' };

    expect(isCustomDateWithinBounds('2026-05-13', bounds)).toBe(true);
    expect(isCustomDateWithinBounds('2026-04-01', bounds)).toBe(true);
    expect(isCustomDateWithinBounds('2026-05-14', bounds)).toBe(false);
    expect(isCustomDateWithinBounds('2026-03-31', bounds)).toBe(false);
  });

  it('opens the native date picker when the date field is activated', () => {
    const showPicker = vi.fn();

    openDateInputPicker({ showPicker } as unknown as HTMLInputElement);

    expect(showPicker).toHaveBeenCalledTimes(1);
  });

  it('ignores browsers that reject programmatic date picker opening', () => {
    const input = { showPicker: vi.fn(() => { throw new Error('not allowed') }) } as unknown as HTMLInputElement;

    expect(() => openDateInputPicker(input)).not.toThrow();
  });
});

describe('UsagePage custom date query', () => {
  it('keeps custom date query bounds as project-local dates for the backend', () => {
    expect(buildCustomDateRangeQuery({ start: '2026-04-20', end: '2026-04-21' })).toEqual({
      valid: true,
      start: '2026-04-20',
      end: '2026-04-21',
    });
  });

  it('rejects rollover calendar dates before sending them to the backend', () => {
    expect(buildCustomDateRangeQuery({ start: '2026-02-31', end: '2026-03-31' })).toEqual({
      valid: false,
      start: undefined,
      end: undefined,
    });
  });
});

describe('UsagePage Overview chart window', () => {
  it('uses the backend-resolved range end for Today hourly chart buckets', () => {
    const filterWindow: UsageFilterWindow = {
      startMs: Date.parse('2026-04-23T00:00:00.000Z'),
      endMs: Date.parse('2026-04-23T12:34:56.000Z'),
      windowMinutes: (12 * 60) + 34 + (56 / 60),
    };

    expect(getOverviewHourWindowHours({ timeRange: 'today', filterWindow })).toBe(24);
    expect(getOverviewChartEndMs({
      timeRange: 'today',
      filterWindow,
      fallbackEndMs: filterWindow.endMs ?? 0,
      resolvedRangeEndMs: Date.parse('2026-04-23T15:59:59.999Z'),
    })).toBe(Date.parse('2026-04-24T00:00:00.000Z'));
  });
});

describe('UsagePage tab labels', () => {
  it('resolves tab labels through translation keys', () => {
    const labels = getUsageTabOptions((key) => `translated:${key}`).map((option) => option.label);

    expect(labels).toEqual([
      'translated:usage_stats.tab_overview',
      'translated:usage_stats.tab_credentials',
      'translated:usage_stats.tab_events',
      'translated:usage_stats.tab_analysis',
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

  it('reloads status and preserves the sync error when backend sync fails', async () => {
    const calls: string[] = [];
    let receivedStatus: StatusResponse | null = null;
    const refreshedStatus: StatusResponse = {
      running: true,
      sync_running: false,
      last_status: 'completed',
      last_run_at: '2026-04-26T13:00:00.000Z',
    };
    const syncError = new ApiError('metadata sync failed', 500);

    await expect(syncCpaData({
      triggerBackendSync: async () => {
        calls.push('sync');
        throw syncError;
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
    })).rejects.toBe(syncError);

    expect(calls).toEqual(['sync', 'status', 'set-status']);
    expect(receivedStatus).toBe(refreshedStatus);
  });
});
