import { afterEach, describe, expect, it, vi } from 'vitest';
import { getBackToCPALinkURL, getCredentialSectionVisibility, getOverviewDisplayLoading, getUsageTabOptions, isUsagePageVisible, loadAnalysisSections, loadRequestEventsPreferences, loadUsagePageVersionInfo, normalizeRequestEventsPreferences, normalizeUsageTabValue, refreshPageData, REQUEST_EVENTS_PREFERENCES_STORAGE_KEY, runUsageEventRequestLogDownload, sanitizeRequestEventFilters, saveRequestEventsPreferences, scheduleOverviewAutoRefresh, shouldAutoRefreshUsageTab, shouldShowApiKeyFilter, shouldShowRangeControls, shouldShowUpdateCheckButton, getUpdateCheckToastDuration } from '../UsagePage';
import { REQUEST_EVENT_COLUMN_IDS } from '@/components/usage/RequestEventsDetailsCard';
import { ApiError } from '@/lib/api';
import type { UsageFilterWindow, VersionResponse } from '@/lib/types';

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

const flushPromises = async () => {
  await Promise.resolve();
  await Promise.resolve();
};

const createMemoryStorage = (seed: Record<string, string> = {}) => {
  const values = new Map(Object.entries(seed));
  return {
    getItem: vi.fn((key: string) => values.get(key) ?? null),
    setItem: vi.fn((key: string, value: string) => {
      values.set(key, value);
    }),
    value: (key: string) => values.get(key),
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

describe('UsagePage Analysis section loading', () => {
  it('starts core and latency requests together and publishes each result independently', async () => {
    let resolveCore: (value: 'core') => void = () => undefined;
    let rejectLatency: (reason: Error) => void = () => undefined;
    const loadCore = vi.fn(() => new Promise<'core'>((resolve) => {
      resolveCore = resolve;
    }));
    const loadLatency = vi.fn(() => new Promise<'latency'>((_resolve, reject) => {
      rejectLatency = reject;
    }));
    const onCoreLoaded = vi.fn();
    const onCoreError = vi.fn();
    const onLatencyLoaded = vi.fn();
    const onLatencyError = vi.fn();

    const loading = loadAnalysisSections({
      loadCore,
      loadLatency,
      onCoreLoaded,
      onCoreError,
      onLatencyLoaded,
      onLatencyError,
    });

    expect(loadCore).toHaveBeenCalledOnce();
    expect(loadLatency).toHaveBeenCalledOnce();

    resolveCore('core');
    await flushPromises();
    expect(onCoreLoaded).toHaveBeenCalledWith('core');
    expect(onLatencyLoaded).not.toHaveBeenCalled();

    const latencyError = new Error('latency failed');
    rejectLatency(latencyError);
    await loading;
    expect(onCoreError).not.toHaveBeenCalled();
    expect(onLatencyError).toHaveBeenCalledWith(latencyError);
  });
});

describe('UsagePage legacy Custom range migration', () => {
  it('keeps a valid legacy Custom range pending until the project timezone is available', async () => {
    const usagePageModule = await import('../UsagePage') as Record<string, unknown>;
    const loadUsageRangeState = usagePageModule.loadUsageRangeState as ((storage: ReturnType<typeof createMemoryStorage>, nowMs: number) => unknown) | undefined;
    const storage = createMemoryStorage({
      'cli-proxy-usage-time-range-v1': 'custom',
      'cli-proxy-usage-custom-range-v1': '{"start":"2026-07-01","end":"2026-07-17"}',
    });

    expect(loadUsageRangeState).toBeTypeOf('function');
    expect(loadUsageRangeState?.(storage, Date.parse('2026-07-17T07:36:42.000Z'))).toEqual({
      state: { range: '8h' },
      pendingLegacyCustomRange: {
        unit: 'day',
        start: '2026-07-01',
        end: '2026-07-17',
      },
    });
  });

  it('ignores invalid legacy Custom state instead of scheduling a migration', async () => {
    const usagePageModule = await import('../UsagePage') as Record<string, unknown>;
    const loadUsageRangeState = usagePageModule.loadUsageRangeState as ((storage: ReturnType<typeof createMemoryStorage>, nowMs: number) => unknown) | undefined;
    const storage = createMemoryStorage({
      'cli-proxy-usage-time-range-v1': 'custom',
      'cli-proxy-usage-custom-range-v1': '{"start":"bad","end":"2026-07-17"}',
    });

    expect(loadUsageRangeState?.(storage, Date.parse('2026-07-17T07:36:42.000Z'))).toEqual({
      state: { range: '8h' },
      pendingLegacyCustomRange: null,
    });
  });

  it('normalizes the pending legacy dates after the project timezone arrives', async () => {
    const usagePageModule = await import('../UsagePage') as Record<string, unknown>;
    const migrateLegacyUsageRangeState = usagePageModule.migrateLegacyUsageRangeState as ((
      range: { unit: 'day'; start: string; end: string },
      options: { nowMs: number; timeZone: string },
    ) => unknown) | undefined;

    expect(migrateLegacyUsageRangeState).toBeTypeOf('function');
    expect(migrateLegacyUsageRangeState?.({
      unit: 'day',
      start: '2026-07-01',
      end: '2026-07-17',
    }, {
      nowMs: Date.parse('2026-07-17T07:36:42.000Z'),
      timeZone: 'Asia/Shanghai',
    })).toEqual({
      range: 'custom',
      customRange: {
        unit: 'day',
        start: '2026-07-01',
        end: '2026-07-17',
      },
      timeZone: 'Asia/Shanghai',
    });
  });

  it('preserves historical legacy dates and their selected end', async () => {
    const usagePageModule = await import('../UsagePage') as Record<string, unknown>;
    const migrateLegacyUsageRangeState = usagePageModule.migrateLegacyUsageRangeState as ((
      range: { unit: 'day'; start: string; end: string },
      options: { nowMs: number; timeZone: string },
    ) => unknown) | undefined;

    expect(migrateLegacyUsageRangeState?.({
      unit: 'day',
      start: '2026-06-17',
      end: '2026-07-16',
    }, {
      nowMs: Date.parse('2026-07-17T07:36:42.000Z'),
      timeZone: 'Asia/Shanghai',
    })).toEqual({
      range: 'custom',
      customRange: {
        unit: 'day',
        start: '2026-06-17',
        end: '2026-07-16',
      },
      timeZone: 'Asia/Shanghai',
    });
  });

  it('writes the migrated state before deleting the only legacy copy', async () => {
    const usagePageModule = await import('../UsagePage') as Record<string, unknown>;
    const persistMigratedUsageRangeState = usagePageModule.persistMigratedUsageRangeState as ((
      storage: { setItem: (key: string, value: string) => void; removeItem: (key: string) => void },
      state: { range: 'custom'; customRange: { unit: 'day'; start: string; end: string }; timeZone: string },
    ) => boolean) | undefined;
    const calls: string[] = [];
    const state = {
      range: 'custom' as const,
      customRange: { unit: 'day' as const, start: '2026-07-01', end: '2026-07-17' },
      timeZone: 'Asia/Shanghai',
    };

    expect(persistMigratedUsageRangeState).toBeTypeOf('function');
    expect(persistMigratedUsageRangeState?.({
      setItem: (key) => calls.push(`set:${key}`),
      removeItem: (key) => calls.push(`remove:${key}`),
    }, state)).toBe(true);
    expect(calls).toEqual([
      'set:cli-proxy-usage-time-range-v1',
      'remove:cli-proxy-usage-custom-range-v1',
    ]);
  });

  it('keeps the legacy copy when writing the migrated state fails', async () => {
    const usagePageModule = await import('../UsagePage') as Record<string, unknown>;
    const persistMigratedUsageRangeState = usagePageModule.persistMigratedUsageRangeState as ((
      storage: { setItem: () => void; removeItem: () => void },
      state: { range: 'custom'; customRange: { unit: 'day'; start: string; end: string }; timeZone: string },
    ) => boolean) | undefined;
    const removeItem = vi.fn();

    expect(persistMigratedUsageRangeState?.({
      setItem: () => { throw new Error('quota exceeded'); },
      removeItem,
    }, {
      range: 'custom',
      customRange: { unit: 'day', start: '2026-07-01', end: '2026-07-17' },
      timeZone: 'Asia/Shanghai',
    })).toBe(false);
    expect(removeItem).not.toHaveBeenCalled();
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
  it('loads version info through the dedicated version loader', async () => {
    const signal = new AbortController().signal;
    const versionInfo = { version: 'v1.2.3', updateCheckEnabled: true };
    const loadVersion = vi.fn(async () => versionInfo);
    const setVersionInfo = vi.fn();

    await loadUsagePageVersionInfo({ loadVersion, signal, setVersionInfo });

    expect(loadVersion).toHaveBeenCalledWith(signal);
    expect(setVersionInfo).toHaveBeenCalledWith(versionInfo);
  });

  it('clears version info when version loading fails', async () => {
    const signal = new AbortController().signal;
    const loadVersion = vi.fn(async () => {
      throw new Error('network failed');
    });
    const setVersionInfo = vi.fn();

    await loadUsagePageVersionInfo({ loadVersion, signal, setVersionInfo });

    expect(setVersionInfo).toHaveBeenCalledWith(null);
  });

  it('requests reauthentication when version loading returns 401', async () => {
    const signal = new AbortController().signal;
    const loadVersion = vi.fn(async () => {
      throw new ApiError('expired', 401);
    });
    const setVersionInfo = vi.fn();
    const onAuthRequired = vi.fn();

    await loadUsagePageVersionInfo({ loadVersion, signal, setVersionInfo, onAuthRequired });

    expect(onAuthRequired).toHaveBeenCalledTimes(1);
    expect(setVersionInfo).not.toHaveBeenCalled();
  });

  it('ignores version results after the request is aborted', async () => {
    const requestController = new AbortController();
    const versionInfo = { version: 'v1.2.3', updateCheckEnabled: true };
    const loadVersion = vi.fn(async () => {
      requestController.abort();
      return versionInfo;
    });
    const setVersionInfo = vi.fn();

    await loadUsagePageVersionInfo({ loadVersion, signal: requestController.signal, setVersionInfo });

    expect(setVersionInfo).not.toHaveBeenCalled();
  });

  it('hides the update button before version loads', () => {
    expect(shouldShowUpdateCheckButton(null)).toBe(false);
  });

  it('hides the update button for dev builds', () => {
    expect(shouldShowUpdateCheckButton({ version: 'dev', updateCheckEnabled: false } satisfies VersionResponse)).toBe(false);
  });

  it('shows the update button for release builds', () => {
    expect(shouldShowUpdateCheckButton({ version: 'v1.2.3', updateCheckEnabled: true })).toBe(true);
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

  it('routes auto-refresh failures to the refresh error handler', async () => {
    vi.useFakeTimers();
    const testDocument = createAutoRefreshTestDocument();
    const failure = new Error('refresh failed');
    const refreshOverview = vi.fn(async () => {
      throw failure;
    });
    const onRefreshError = vi.fn();

    const cleanup = scheduleOverviewAutoRefresh({ enabled: true, refreshOverview, onRefreshError, documentRef: testDocument });

    vi.advanceTimersByTime(10_000);
    await flushPromises();

    expect(onRefreshError).toHaveBeenCalledWith(failure);

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
  it('keeps restored model and source filters until backend filter options load', () => {
    const next = sanitizeRequestEventFilters(
      {
        model: 'claude-opus',
        source: 'authidx-source-b',
        result: 'failed',
      },
      {
        models: [],
        sources: [],
      },
      false,
    );

    expect(next).toEqual({
      model: 'claude-opus',
      source: 'authidx-source-b',
      result: 'failed',
    });
  });

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

describe('UsagePage request event preferences', () => {
  it('normalizes persisted filters, page size, and visible columns', () => {
    const preferences = normalizeRequestEventsPreferences({
      version: 1,
      pageSize: 500,
      filters: {
        model: 'claude-opus',
        source: 'authidx-source-b',
        result: 'failed',
      },
      visibleColumnIds: ['model', 'timestamp', 'model', 'not-a-column', 'total_cost'],
    });

    expect(preferences).toEqual({
      version: 5,
      pageSize: 500,
      filters: {
        model: 'claude-opus',
        source: 'authidx-source-b',
        result: 'failed',
      },
      visibleColumnIds: ['model', 'timestamp', 'total_cost'],
    });
  });

  it('falls back safely for damaged persisted request event preferences', () => {
    const preferences = normalizeRequestEventsPreferences({
      version: 1,
      pageSize: 999,
      filters: {
        model: 42,
        source: '',
        result: 'maybe',
      },
      visibleColumnIds: ['not-a-column'],
    });

    expect(preferences.pageSize).toBe(100);
    expect(preferences.filters).toEqual({
      model: '__all__',
      source: '__all__',
      result: '__all__',
    });
    expect(preferences.visibleColumnIds[0]).toBe('timestamp');
    expect(preferences.visibleColumnIds.length).toBeGreaterThan(1);
  });

  it('keeps persisted request event columns unchanged when Speed is absent', () => {
    const columnIdsWithoutSpeed = REQUEST_EVENT_COLUMN_IDS.filter((columnId) => columnId !== 'speed');
    const preferences = normalizeRequestEventsPreferences({
      version: 1,
      pageSize: 100,
      visibleColumnIds: columnIdsWithoutSpeed,
    });

    expect(preferences.visibleColumnIds).toEqual(columnIdsWithoutSpeed);
    expect(preferences.visibleColumnIds).not.toContain('speed');
  });

  it('adds Speed Mode to legacy full-column request event preferences', () => {
    const legacyFullColumnIds = [
      'timestamp',
      'api_key',
      'source',
      'model',
      'reasoning_effort',
      'result',
      'request_type',
      'endpoint',
      'ttft',
      'latency',
      'speed',
      'input_tokens',
      'output_tokens',
      'reasoning_tokens',
      'cached_tokens',
      'cache_rate',
      'total_tokens',
      'total_cost',
    ];
    const preferences = normalizeRequestEventsPreferences({
      version: 1,
      pageSize: 100,
      visibleColumnIds: legacyFullColumnIds,
    });

    expect(preferences.visibleColumnIds).toEqual(REQUEST_EVENT_COLUMN_IDS);
  });

  it('preserves a saved preference that intentionally hides Speed', () => {
    const storage = createMemoryStorage();
    const hiddenSpeedColumnIds = REQUEST_EVENT_COLUMN_IDS.filter((columnId) => columnId !== 'speed');

    saveRequestEventsPreferences({
      version: 5,
      pageSize: 100,
      filters: {
        model: '__all__',
        source: '__all__',
        result: '__all__',
      },
      visibleColumnIds: hiddenSpeedColumnIds,
    }, storage);

    const stored = JSON.parse(storage.value(REQUEST_EVENTS_PREFERENCES_STORAGE_KEY) ?? '');
    expect(stored).toEqual({
      version: 5,
      pageSize: 100,
      filters: {
        model: '__all__',
        source: '__all__',
        result: '__all__',
      },
      visibleColumnIds: hiddenSpeedColumnIds,
    });
    expect(loadRequestEventsPreferences(storage).visibleColumnIds).toEqual(hiddenSpeedColumnIds);
  });

  it('preserves a saved preference that intentionally hides Speed Mode', () => {
    const storage = createMemoryStorage();
    const hiddenSpeedModeColumnIds = REQUEST_EVENT_COLUMN_IDS.filter((columnId) => columnId !== 'service_tier');

    saveRequestEventsPreferences({
      version: 5,
      pageSize: 100,
      filters: {
        model: '__all__',
        source: '__all__',
        result: '__all__',
      },
      visibleColumnIds: hiddenSpeedModeColumnIds,
    }, storage);

    expect(loadRequestEventsPreferences(storage).visibleColumnIds).toEqual(hiddenSpeedModeColumnIds);
  });

  it('loads defaults from invalid JSON and persists normalized request event preferences', () => {
    const storage = createMemoryStorage({
      [REQUEST_EVENTS_PREFERENCES_STORAGE_KEY]: '{bad json',
    });

    expect(loadRequestEventsPreferences(storage).pageSize).toBe(100);

    saveRequestEventsPreferences({
      version: 4,
      pageSize: 50,
      filters: {
        model: 'gpt-4.1',
        source: 'source-a',
        result: 'success',
      },
      visibleColumnIds: ['timestamp', 'timestamp', 'model'],
    }, storage);

    expect(storage.setItem).toHaveBeenCalledTimes(1);
    expect(JSON.parse(storage.value(REQUEST_EVENTS_PREFERENCES_STORAGE_KEY) ?? '')).toEqual({
      version: 5,
      pageSize: 50,
      filters: {
        model: 'gpt-4.1',
        source: 'source-a',
        result: 'success',
      },
      visibleColumnIds: ['timestamp', 'model'],
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

describe('UsagePage request log download guard', () => {
  it('does not trigger a stale native download after the modal is closed', async () => {
    const generationRef = { current: 0 };
    let resolveDownloadURL: (url: string) => void = () => undefined;
    const createDownloadURL = vi.fn(() => new Promise<string>((resolve) => {
      resolveDownloadURL = resolve;
    }));
    const triggerDownload = vi.fn();
    const setDownloading = vi.fn();
    const showDownloadError = vi.fn();

    const pendingDownload = runUsageEventRequestLogDownload({
      eventId: ' 42 ',
      generationRef,
      createDownloadURL,
      triggerDownload,
      setDownloading,
      showDownloadError,
    });

    expect(createDownloadURL).toHaveBeenCalledWith('42');
    expect(setDownloading).toHaveBeenCalledWith(true);

    generationRef.current += 1;
    resolveDownloadURL('/api/v1/usage/events/42/request-log/download-file?token=abc');
    await pendingDownload;

    expect(triggerDownload).not.toHaveBeenCalled();
    expect(showDownloadError).not.toHaveBeenCalled();
    expect(setDownloading).toHaveBeenCalledTimes(1);
  });
});
