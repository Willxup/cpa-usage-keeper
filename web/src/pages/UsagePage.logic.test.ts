import { afterEach, describe, expect, it, vi } from 'vitest';
import { buildCustomDateRangeQuery, getBackToCPALinkURL, getCredentialSectionVisibility, getCustomDateRangeBounds, getOverviewChartEndMs, getOverviewDisplayLoading, getOverviewHourWindowHours, getPreferredOverviewChartPeriod, getTimeRangeOptions, getUsageTabOptions, isCustomDateWithinBounds, isUsagePageVisible, normalizeUsageTabValue, openDateInputPicker, refreshPageData, sanitizeRequestEventFilters, scheduleOverviewAutoRefresh, scheduleStatusActiveHeartbeat, shouldAutoRefreshUsageTab, shouldShowApiKeyFilter, shouldShowRangeControls, shouldShowUpdateCheckButton, STATUS_ACTIVE_HEARTBEAT_INTERVAL_MS, getUpdateCheckToastDuration } from './UsagePage';
import type { StatusResponse, UsageFilterWindow } from '@/lib/types';

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

const createStatusResponse = (lastError = '', quotaAutoRefreshEnabled = true): StatusResponse => ({
  running: true,
  sync_running: false,
  timezone: 'UTC',
  last_error: lastError,
  quotaAutoRefreshEnabled,
});

const flushPromises = async () => {
  await Promise.resolve();
  await Promise.resolve();
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

describe('UsagePage Back to CPA link', () => {
  it('uses the CPA public URL from status', () => {
    expect(getBackToCPALinkURL({ cpa_public_url: 'https://cpa.example.com' }, 'https://keeper.example.com')).toBe('https://cpa.example.com/management.html');
  });

  it('uses the current origin when status does not include a CPA public URL', () => {
    expect(getBackToCPALinkURL({}, 'https://cpa.domain.com')).toBe('https://cpa.domain.com/management.html');
    expect(getBackToCPALinkURL(null, 'https://cpa.domain.com')).toBe('https://cpa.domain.com/management.html');
  });

  it('normalizes trailing slashes and existing management pages', () => {
    expect(getBackToCPALinkURL({ cpa_public_url: 'https://cpa.example.com/' }, 'https://keeper.example.com')).toBe('https://cpa.example.com/management.html');
    expect(getBackToCPALinkURL({ cpa_public_url: 'https://cpa.example.com/cpa/' }, 'https://keeper.example.com')).toBe('https://cpa.example.com/cpa/management.html');
    expect(getBackToCPALinkURL({ cpa_public_url: 'https://cpa.example.com/management.html' }, 'https://keeper.example.com')).toBe('https://cpa.example.com/management.html');
  });

  it('supports relative public paths and bare host names', () => {
    expect(getBackToCPALinkURL({ cpa_public_url: '/cpa/' }, 'https://keeper.example.com')).toBe('https://keeper.example.com/cpa/management.html');
    expect(getBackToCPALinkURL({ cpa_public_url: 'cpa.domain.com/' }, 'https://keeper.example.com')).toBe('https://cpa.domain.com/management.html');
    expect(getBackToCPALinkURL({ cpa_public_url: 'cpa.domain.com:8317/' }, 'https://keeper.example.com')).toBe('https://cpa.domain.com:8317/management.html');
  });

  it('rejects explicit non-http public URL schemes', () => {
    expect(getBackToCPALinkURL({ cpa_public_url: 'javascript://alert(1)' }, 'https://keeper.example.com')).toBe('');
    expect(getBackToCPALinkURL({ cpa_public_url: 'data://text/html,<script>alert(1)</script>' }, 'https://keeper.example.com')).toBe('');
    expect(getBackToCPALinkURL({ cpa_public_url: 'file:///etc/passwd' }, 'https://keeper.example.com')).toBe('');
    expect(getBackToCPALinkURL({ cpa_public_url: 'ftp://cpa.example.com' }, 'https://keeper.example.com')).toBe('');
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

describe('UsagePage visibility guard', () => {
  it('treats hidden documents as inactive for credentials polling', () => {
    expect(isUsagePageVisible({ visibilityState: 'visible' })).toBe(true);
    expect(isUsagePageVisible({ visibilityState: 'hidden' })).toBe(false);
  });
});

describe('UsagePage status active heartbeat', () => {
  it('loads status and marks the page active immediately and on the 30s cadence', async () => {
    let intervalHandler: (() => void) | undefined;
    const testDocument = createAutoRefreshTestDocument();
    const timerTarget = {
      setInterval: vi.fn((handler: () => void, timeout: number) => {
        intervalHandler = handler;
        expect(timeout).toBe(STATUS_ACTIVE_HEARTBEAT_INTERVAL_MS);
        return 7;
      }),
      clearInterval: vi.fn(),
    };
    const status = createStatusResponse('last problem');
    const loadStatus = vi.fn(async () => status);
    const markActive = vi.fn(async () => undefined);
    const setStatus = vi.fn();
    const setStatusError = vi.fn();

    const cleanup = scheduleStatusActiveHeartbeat({
      loadStatus,
      markActive,
      setStatus,
      setStatusError,
      documentRef: testDocument,
      timerTarget,
    });
    await flushPromises();

    expect(loadStatus).toHaveBeenCalledTimes(1);
    expect(markActive).toHaveBeenCalledTimes(1);
    expect(setStatus).toHaveBeenCalledWith(status);
    expect(setStatusError).toHaveBeenCalledWith('last problem');

    intervalHandler?.();
    await flushPromises();

    expect(loadStatus).toHaveBeenCalledTimes(2);
    expect(markActive).toHaveBeenCalledTimes(2);

    cleanup();
  });

  it('loads status once without active heartbeat when quota auto refresh is disabled', async () => {
    const testDocument = createAutoRefreshTestDocument();
    const timerTarget = {
      setInterval: vi.fn(() => 7),
      clearInterval: vi.fn(),
    };
    const status = createStatusResponse('', false);
    const loadStatus = vi.fn(async () => status);
    const markActive = vi.fn(async () => undefined);
    const setStatus = vi.fn();
    const setStatusError = vi.fn();

    const cleanup = scheduleStatusActiveHeartbeat({
      loadStatus,
      markActive,
      setStatus,
      setStatusError,
      documentRef: testDocument,
      timerTarget,
    });
    await flushPromises();

    expect(loadStatus).toHaveBeenCalledTimes(1);
    expect(markActive).not.toHaveBeenCalled();
    expect(timerTarget.setInterval).not.toHaveBeenCalled();
    expect(setStatus).toHaveBeenCalledWith(status);

    cleanup();
  });

  it('does not start while hidden and starts immediately when visible again', async () => {
    const testDocument = createAutoRefreshTestDocument('hidden');
    const timerTarget = {
      setInterval: vi.fn(() => 8),
      clearInterval: vi.fn(),
    };
    const loadStatus = vi.fn(async () => createStatusResponse());
    const markActive = vi.fn(async () => undefined);

    const cleanup = scheduleStatusActiveHeartbeat({
      loadStatus,
      markActive,
      setStatus: vi.fn(),
      setStatusError: vi.fn(),
      documentRef: testDocument,
      timerTarget,
    });
    await flushPromises();

    expect(loadStatus).not.toHaveBeenCalled();

    testDocument.setVisibilityState('visible');
    testDocument.dispatchEvent(new Event('visibilitychange'));
    await flushPromises();

    expect(loadStatus).toHaveBeenCalledTimes(1);
    expect(markActive).toHaveBeenCalledTimes(1);

    cleanup();
  });

  it('aborts the in-flight heartbeat before creating an interval when hidden', () => {
    let capturedSignal: AbortSignal | undefined;
    const testDocument = createAutoRefreshTestDocument();
    const timerTarget = {
      setInterval: vi.fn(() => 9),
      clearInterval: vi.fn(),
    };
    const loadStatus = vi.fn((signal: AbortSignal) => {
      capturedSignal = signal;
      return new Promise<StatusResponse>(() => undefined);
    });

    const cleanup = scheduleStatusActiveHeartbeat({
      loadStatus,
      markActive: vi.fn(async () => undefined),
      setStatus: vi.fn(),
      setStatusError: vi.fn(),
      documentRef: testDocument,
      timerTarget,
    });

    expect(loadStatus).toHaveBeenCalledTimes(1);
    expect(capturedSignal?.aborted).toBe(false);

    testDocument.setVisibilityState('hidden');
    testDocument.dispatchEvent(new Event('visibilitychange'));

    expect(capturedSignal?.aborted).toBe(true);
    expect(timerTarget.setInterval).not.toHaveBeenCalled();
    expect(timerTarget.clearInterval).not.toHaveBeenCalled();

    cleanup();
  });
});

describe('UsagePage active tab auto-refresh guard', () => {
  it('allows Request Events auto-refresh only on the first page', () => {
    expect(shouldAutoRefreshUsageTab({ activeTab: 'events', eventsPage: 1 })).toBe(true);
    expect(shouldAutoRefreshUsageTab({ activeTab: 'events', eventsPage: 2 })).toBe(false);
  });

  it('does not auto-refresh credential detail tabs', () => {
    expect(shouldAutoRefreshUsageTab({ activeTab: 'auth-files', eventsPage: 1 })).toBe(false);
    expect(shouldAutoRefreshUsageTab({ activeTab: 'ai-provider', eventsPage: 1 })).toBe(false);
  });

  it('keeps Overview auto-refresh enabled and does not auto-refresh other tabs', () => {
    expect(shouldAutoRefreshUsageTab({ activeTab: 'overview', eventsPage: 2 })).toBe(true);
    expect(shouldAutoRefreshUsageTab({ activeTab: 'analysis', eventsPage: 1 })).toBe(false);
    expect(shouldAutoRefreshUsageTab({ activeTab: 'settings', eventsPage: 1 })).toBe(false);
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
  ['auth-files', false],
  ['ai-provider', false],
  ['settings', false],
] as const) {
  it(`returns ${expected} for ${tab} range controls visibility`, () => {
    expect(shouldShowRangeControls(tab)).toBe(expected);
  });
}

for (const [tab, expected] of [
  ['overview', true],
  ['analysis', true],
  ['events', true],
  ['auth-files', false],
  ['ai-provider', false],
  ['settings', false],
] as const) {
  it(`returns ${expected} for ${tab} API Key filter visibility`, () => {
    expect(shouldShowApiKeyFilter(tab)).toBe(expected);
  });
}

describe('UsagePage time range options', () => {
  it('includes rolling 24h, local Today, Yesterday, and 30d ranges', () => {
    const options = getTimeRangeOptions((key) => `translated:${key}`);

    expect(options.map((option) => option.value)).toEqual(['4h', '8h', '12h', '24h', 'today', 'yesterday', '7d', '30d', 'custom']);
    expect(options.map((option) => option.label)).toContain('translated:usage_stats.range_24h');
    expect(options.map((option) => option.label)).toContain('translated:usage_stats.range_today');
    expect(options.map((option) => option.label)).toContain('translated:usage_stats.range_yesterday');
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
  it('uses backend Today range start instead of browser-local midnight for chart buckets', () => {
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
      resolvedRangeStartMs: Date.parse('2026-04-22T16:00:00.000Z'),
      resolvedRangeEndMs: Date.parse('2026-04-23T15:59:59.999Z'),
    })).toBe(Date.parse('2026-04-23T16:00:00.000Z'));
  });

  it('uses Yesterday hourly chart buckets through the next day boundary', () => {
    const filterWindow: UsageFilterWindow = {
      startMs: Date.parse('2026-04-23T00:00:00.000Z'),
      endMs: Date.parse('2026-04-23T23:59:59.999Z'),
      windowMinutes: 24 * 60,
    };
    const resolvedRangeEndMs = Date.parse('2026-04-23T23:59:59.999Z');

    expect(getOverviewHourWindowHours({ timeRange: 'yesterday', filterWindow })).toBe(24);
    expect(getOverviewChartEndMs({
      timeRange: 'yesterday',
      filterWindow,
      fallbackEndMs: Date.parse('2026-04-24T12:34:56.000Z'),
      resolvedRangeEndMs,
    })).toBe(Date.parse('2026-04-24T00:00:00.000Z'));
  });
});

describe('UsagePage tab labels', () => {
  it('resolves tab labels through translation keys', () => {
    const labels = getUsageTabOptions((key) => `translated:${key}`).map((option) => option.label);

    expect(labels).toEqual([
      'translated:usage_stats.tab_overview',
      'translated:usage_stats.tab_analysis',
      'translated:usage_stats.tab_events',
      'translated:usage_stats.tab_auth_files',
      'translated:usage_stats.tab_ai_provider',
      'translated:usage_stats.tab_settings',
    ]);
  });
});

describe('UsagePage credentials tab migration', () => {
  it('migrates the legacy Credentials tab value to Auth Files', () => {
    expect(normalizeUsageTabValue('credentials')).toBe('auth-files');
  });

  it('keeps each credential section scoped to its own tab', () => {
    expect(getCredentialSectionVisibility('auth-files')).toEqual({
      enabled: true,
      showAuthFiles: true,
      showAiProvider: false,
    });
    expect(getCredentialSectionVisibility('ai-provider')).toEqual({
      enabled: true,
      showAuthFiles: false,
      showAiProvider: true,
    });
    expect(getCredentialSectionVisibility('overview')).toEqual({
      enabled: false,
      showAuthFiles: false,
      showAiProvider: false,
    });
  });
});

describe('UsagePage refresh action', () => {
  it('reloads page data without triggering backend sync', async () => {
    let refreshCalls = 0;
    const syncCalls = 0;

    await refreshPageData({
      refreshActiveTab: async () => {
        refreshCalls += 1;
      },
    });

    expect(refreshCalls).toBe(1);
    expect(syncCalls).toBe(0);
  });
});
