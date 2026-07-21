// @vitest-environment happy-dom

import { act, useEffect } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { UsageActivityResponse, UsageActivityWindow } from '@/lib/types';
import { ApiError } from '@/lib/api';
import { useUsageActivityData, type UseUsageActivityDataOptions } from '../useUsageActivityData';

const apiMocks = vi.hoisted(() => ({
  fetchUsageActivity: vi.fn(),
  fetchKeyActivity: vi.fn(),
}));

vi.mock('@/lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/lib/api')>(),
  fetchUsageActivity: apiMocks.fetchUsageActivity,
  fetchKeyActivity: apiMocks.fetchKeyActivity,
}));

const activityFor = (window: UsageActivityWindow): UsageActivityResponse => ({
  window,
  grain: window === '24h' ? 'short' : window === '7d' ? 'medium' : 'long',
  timezone: 'UTC',
  rows: 7,
  columns: 52,
  bucket_seconds: 1,
  window_start: '2026-07-01T00:00:00Z',
  window_end: '2026-07-02T00:00:00Z',
  total_success: 0,
  total_failure: 0,
  success_rate: 0,
  blocks: [],
});

let latest: ReturnType<typeof useUsageActivityData> | null = null;

function Harness({ options }: { options: UseUsageActivityDataOptions }) {
  const result = useUsageActivityData(options);
  useEffect(() => {
    latest = result;
  }, [result]);
  return null;
}

describe('useUsageActivityData', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    apiMocks.fetchUsageActivity.mockReset();
    apiMocks.fetchKeyActivity.mockReset();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    latest = null;
  });

  const renderOptions = async (options: UseUsageActivityDataOptions) => {
    await act(async () => {
      root.render(<Harness options={options} />);
      await Promise.resolve();
    });
  };

  it('uses the Admin endpoint with the Overview time query and selected API key scope', async () => {
    apiMocks.fetchUsageActivity.mockResolvedValue(activityFor('7d'));
    const request = { range: '2d' as const };

    await renderOptions({ viewer: 'admin', request, apiKeyId: '42' });

    expect(apiMocks.fetchUsageActivity).toHaveBeenCalledWith(expect.objectContaining({ request, apiKeyId: '42' }));
    expect(apiMocks.fetchKeyActivity).not.toHaveBeenCalled();
    expect(latest?.activity?.window).toBe('7d');
  });

  it('uses the Key endpoint without an external API key scope', async () => {
    apiMocks.fetchKeyActivity.mockResolvedValue(activityFor('24h'));
    const request = { range: '8h' as const };

    await renderOptions({ viewer: 'key', request, apiKeyId: '999' });

    expect(apiMocks.fetchKeyActivity).toHaveBeenCalledWith(expect.objectContaining({ request }));
    expect(apiMocks.fetchKeyActivity.mock.calls[0][0].apiKeyId).toBeUndefined();
    expect(apiMocks.fetchUsageActivity).not.toHaveBeenCalled();
  });

  it('aborts the previous time query and never displays its late response', async () => {
    let resolveFirst: ((value: UsageActivityResponse) => void) | undefined;
    apiMocks.fetchUsageActivity
      .mockImplementationOnce(() => new Promise<UsageActivityResponse>((resolve) => { resolveFirst = resolve; }))
      .mockResolvedValueOnce(activityFor('30d'));

    await renderOptions({ viewer: 'admin', request: { range: '8h' } });
    const firstSignal = apiMocks.fetchUsageActivity.mock.calls[0][0].signal as AbortSignal;
    const firstIdentity = latest?.requestIdentity;
    await renderOptions({ viewer: 'admin', request: { range: '8d' } });

    expect(firstSignal.aborted).toBe(true);
    expect(latest?.activity?.window).toBe('30d');
    expect(latest?.requestIdentity).not.toBe(firstIdentity);
    await act(async () => resolveFirst?.(activityFor('24h')));
    expect(latest?.activity?.window).toBe('30d');
  });

  it('reuses the same in-flight request when automatic refresh skips it', async () => {
    let resolveRequest: ((value: UsageActivityResponse) => void) | undefined;
    apiMocks.fetchUsageActivity.mockImplementation(() => new Promise<UsageActivityResponse>((resolve) => {
      resolveRequest = resolve;
    }));

    await renderOptions({ viewer: 'admin', request: { range: '8h' } });
    const firstSignal = apiMocks.fetchUsageActivity.mock.calls[0][0].signal as AbortSignal;
    let refreshPromise: Promise<void> | undefined;
    await act(async () => {
      refreshPromise = latest?.loadActivity({ skipIfInFlight: true });
      await Promise.resolve();
    });

    expect(apiMocks.fetchUsageActivity).toHaveBeenCalledTimes(1);
    expect(firstSignal.aborted).toBe(false);
    await act(async () => {
      resolveRequest?.(activityFor('24h'));
      await refreshPromise;
    });
    expect(latest?.activity?.window).toBe('24h');
  });

  it('lets a manual refresh replace the same in-flight request', async () => {
    let resolveReplacement: ((value: UsageActivityResponse) => void) | undefined;
    apiMocks.fetchUsageActivity
      .mockImplementationOnce(() => new Promise<UsageActivityResponse>(() => undefined))
      .mockImplementationOnce(() => new Promise<UsageActivityResponse>((resolve) => {
        resolveReplacement = resolve;
      }));

    await renderOptions({ viewer: 'admin', request: { range: '8h' } });
    const firstSignal = apiMocks.fetchUsageActivity.mock.calls[0][0].signal as AbortSignal;
    let refreshPromise: Promise<void> | undefined;
    await act(async () => {
      refreshPromise = latest?.loadActivity();
      await Promise.resolve();
    });

    expect(apiMocks.fetchUsageActivity).toHaveBeenCalledTimes(2);
    expect(firstSignal.aborted).toBe(true);
    await act(async () => {
      resolveReplacement?.(activityFor('24h'));
      await refreshPromise;
    });
    expect(latest?.activity?.window).toBe('24h');
  });

  it('keeps Activity errors local and handles viewer authentication errors', async () => {
    const onAuthRequired = vi.fn();
    apiMocks.fetchKeyActivity.mockRejectedValueOnce(new ApiError('unauthorized', 401));
    await renderOptions({ viewer: 'key', request: { range: '8h' }, onAuthRequired });
    expect(onAuthRequired).toHaveBeenCalledTimes(1);
    expect(latest?.error).toBe('AUTH_REQUIRED');

    apiMocks.fetchKeyActivity.mockRejectedValueOnce(new ApiError('limited', 429));
    await act(async () => latest?.loadActivity());
    expect(latest?.error).toBe('KEY_ACTIVITY_RATE_LIMITED');
  });

  it('normalizes backend failures to the Activity-specific error state', async () => {
    apiMocks.fetchUsageActivity.mockRejectedValue(new ApiError('internal details', 500));

    await renderOptions({ viewer: 'admin', request: { range: '8h' } });

    expect(latest?.error).toBe('ACTIVITY_LOAD_FAILED');
  });
});
