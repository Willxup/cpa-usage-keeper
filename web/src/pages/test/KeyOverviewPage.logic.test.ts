import { afterEach, describe, expect, it, vi } from 'vitest';
import { scheduleKeyOverviewAutoRefresh, startKeyOverviewRequest } from '../KeyOverviewPage';

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

describe('KeyOverviewPage auto refresh', () => {
  it('routes auto-refresh failures to the refresh error handler', async () => {
    vi.useFakeTimers();
    const testDocument = createAutoRefreshTestDocument();
    const failure = new Error('refresh failed');
    const refreshOverview = vi.fn(async () => {
      throw failure;
    });
    const onRefreshError = vi.fn();

    const cleanup = scheduleKeyOverviewAutoRefresh({
      refreshOverview,
      onRefreshError,
      documentRef: testDocument,
    });

    await vi.advanceTimersByTimeAsync(10_000);

    expect(onRefreshError).toHaveBeenCalledWith(failure);

    cleanup();
  });

  it('restarts the interval cadence after refreshing on visibility restore', () => {
    vi.useFakeTimers();
    const testDocument = createAutoRefreshTestDocument('hidden');
    const refreshOverview = vi.fn();

    const cleanup = scheduleKeyOverviewAutoRefresh({ refreshOverview, documentRef: testDocument });
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
});

describe('KeyOverviewPage request controller', () => {
  it('skips auto-refresh requests while the same loader already has a request in flight', () => {
    const inFlightController = new AbortController();

    const result = startKeyOverviewRequest({
      currentController: inFlightController,
      skipIfInFlight: true,
    });

    expect(result.skipped).toBe(true);
    expect(result.controller).toBeNull();
    expect(inFlightController.signal.aborted).toBe(false);
  });

  it('restarts manual requests by aborting the current controller', () => {
    const inFlightController = new AbortController();

    const result = startKeyOverviewRequest({
      currentController: inFlightController,
      skipIfInFlight: false,
    });

    expect(result.skipped).toBe(false);
    expect(result.controller).toBeInstanceOf(AbortController);
    expect(result.controller).not.toBe(inFlightController);
    expect(inFlightController.signal.aborted).toBe(true);
  });
});
