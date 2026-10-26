// @vitest-environment happy-dom
import { act, useEffect } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/lib/api';
import type { CpaApiKeyOptionsResponse } from '@/lib/types';
import { useRequestEventAPIKeyOptions, type RequestEventAPIKeyOptionsParams } from '../useRequestEventAPIKeyOptions';

const fetchOptions = vi.hoisted(() => vi.fn());
vi.mock('@/lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/lib/api')>(),
  fetchUsageEventAPIKeyFilterOptions: fetchOptions,
}));

let latest: ReturnType<typeof useRequestEventAPIKeyOptions>;
function Harness({ params }: { params: RequestEventAPIKeyOptionsParams }) {
  const state = useRequestEventAPIKeyOptions(params);
  useEffect(() => { latest = state; }, [state]);
  return null;
}
const initial: RequestEventAPIKeyOptionsParams = {
  request: { range: '24h' }, filters: { model: 'gpt-5', source: 'source-a', result: 'failed' }, enabled: true,
};
const oldKeys = { options: [{ id: '11', label: 'Old key' }] };
const newKeys = { options: [{ id: '22', label: 'New key' }] };
const deferred = () => {
  let resolve!: (value: CpaApiKeyOptionsResponse) => void;
  const promise = new Promise<CpaApiKeyOptionsResponse>((done) => { resolve = done; });
  return { promise, resolve };
};

describe('useRequestEventAPIKeyOptions', () => {
  let root: Root;
  let container: HTMLDivElement;
  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    fetchOptions.mockReset().mockResolvedValue(oldKeys);
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });
  afterEach(() => { act(() => root.unmount()); container.remove(); });
  const render = async (params = initial) => { await act(async () => root.render(<Harness params={params} />)); };

  it('loads only on open and refreshes on every subsequent opening', async () => {
    await render();
    expect(fetchOptions).not.toHaveBeenCalled();
    await act(async () => latest.refresh());
    expect(fetchOptions).toHaveBeenCalledExactlyOnceWith(initial.request, expect.any(AbortSignal), initial.filters);
    expect(latest.options).toEqual(oldKeys.options);
    fetchOptions.mockResolvedValue(newKeys);
    await act(async () => latest.refresh());
    expect(fetchOptions).toHaveBeenCalledTimes(2);
    expect(latest.options).toEqual(newKeys.options);
  });

  it('removes prior candidates while refreshing and ignores superseded late responses', async () => {
    await render();
    await act(async () => latest.refresh());
    const pending = deferred();
    fetchOptions.mockReturnValueOnce(pending.promise).mockResolvedValueOnce(newKeys);
    await act(async () => { void latest.refresh(); });
    const previousSignal = fetchOptions.mock.lastCall![1] as AbortSignal;
    expect(latest.status).toBe('loading');
    expect(latest.options).toEqual([]);
    await act(async () => latest.refresh());
    expect(previousSignal.aborted).toBe(true);
    await act(async () => pending.resolve(oldKeys));
    expect(latest.options).toEqual(newKeys.options);
  });

  it('aborts and replaces candidates when the event range or other filters change', async () => {
    const pending = deferred();
    fetchOptions.mockReturnValueOnce(pending.promise).mockResolvedValueOnce(newKeys);
    await render();
    await act(async () => { void latest.refresh(); });
    const previousSignal = fetchOptions.mock.lastCall![1] as AbortSignal;
    const next = { ...initial, request: { range: '7d' as const }, filters: { model: 'claude-sonnet', result: 'success' } };
    await render(next);
    expect(previousSignal.aborted).toBe(true);
    expect(fetchOptions).toHaveBeenLastCalledWith(next.request, expect.any(AbortSignal), next.filters);
    await act(async () => pending.resolve(oldKeys));
    expect(latest.options).toEqual(newKeys.options);
  });

  it('clears selectable candidates on failure and retries on the next opening', async () => {
    await render();
    await act(async () => latest.refresh());
    fetchOptions.mockRejectedValueOnce(new Error('offline'));
    await act(async () => latest.refresh());
    expect(latest.status).toBe('error');
    expect(latest.options).toEqual([]);
    await act(async () => latest.refresh());
    expect(latest.status).toBe('ready');
  });

  it('aborts when leaving Request Events and ignores completion', async () => {
    const pending = deferred();
    fetchOptions.mockReturnValueOnce(pending.promise);
    await render();
    await act(async () => { void latest.refresh(); });
    const signal = fetchOptions.mock.lastCall![1] as AbortSignal;
    await render({ ...initial, enabled: false });
    expect(signal.aborted).toBe(true);
    await act(async () => pending.resolve(oldKeys));
    expect(latest.options).toEqual([]);
    await act(async () => latest.refresh());
    expect(fetchOptions).toHaveBeenCalledTimes(1);
  });

  it('forwards authorization and range recovery failures', async () => {
    const onAuthRequired = vi.fn();
    const onRangeBoundsConflict = vi.fn().mockReturnValue(false);
    fetchOptions.mockRejectedValue(new ApiError('expired', 401));
    await render({ ...initial, onAuthRequired, onRangeBoundsConflict });
    await act(async () => latest.refresh());
    expect(onAuthRequired).toHaveBeenCalledOnce();
    expect(onRangeBoundsConflict).toHaveBeenCalledOnce();
  });
});
