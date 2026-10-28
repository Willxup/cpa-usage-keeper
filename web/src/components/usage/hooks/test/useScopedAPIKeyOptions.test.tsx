// @vitest-environment happy-dom
import React, { act, useEffect } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fetchCpaApiKeyOptions, type CpaApiKeyOptionsRequest } from '@/lib/api';
import { useScopedAPIKeyOptions } from '../useScopedAPIKeyOptions';
vi.mock('@/lib/api', async (original) => ({ ...await original<typeof import('@/lib/api')>(), fetchCpaApiKeyOptions: vi.fn() }));
const fetchOptions = vi.mocked(fetchCpaApiKeyOptions);

function deferNextOptions() {
  let resolve!: (value: Awaited<ReturnType<typeof fetchCpaApiKeyOptions>>) => void;
  fetchOptions.mockReturnValueOnce(new Promise((done) => { resolve = done; }));
  return resolve;
}

describe('scoped API key candidates', () => {
  let root: Root;
  let host: HTMLDivElement;
  let result: ReturnType<typeof useScopedAPIKeyOptions>;
  const onOptions = vi.fn();
  const request: CpaApiKeyOptionsRequest = { scope: 'overview', range: { range: '24h' } };
  function Harness({ query }: { query: CpaApiKeyOptionsRequest | null }) {
    const value = useScopedAPIKeyOptions(query, onOptions);
    useEffect(() => { result = value; }, [value]);
    return null;
  }
  const render = async (query: CpaApiKeyOptionsRequest | null, open = true) => {
    await act(async () => root.render(<Harness query={query} />));
    if (open) await act(async () => { void result.ensureFresh(); });
  };
  beforeEach(() => { globalThis.IS_REACT_ACT_ENVIRONMENT = true; host = document.createElement('div'); root = createRoot(host); onOptions.mockClear(); fetchOptions.mockReset().mockResolvedValue({ options: [] }); });
  afterEach(async () => { await act(async () => root.unmount()); });
  it('forces manual refresh and preserves empty success instead of falling back to the roster', async () => {
    fetchOptions.mockResolvedValueOnce({ options: [{ id: '1', label: 'Used' }] });
    await render(request);
    expect(result.options).toHaveLength(1);
    await act(async () => { await result.refresh(); });
    expect(fetchOptions).toHaveBeenCalledTimes(2);
    expect(fetchOptions).toHaveBeenLastCalledWith(expect.any(AbortSignal), request);
    expect(result.status).toBe('ready');
    expect(result.options).toEqual([]);
  });
  it('loads only on opening, reuses empty results and coalesces invalidations', async () => {
    await render(request, false);
    expect(fetchOptions).not.toHaveBeenCalled();
    const finish = deferNextOptions();
    await act(async () => { void result.ensureFresh(); });
    expect(result.status).toBe('loading');
    await act(async () => { void result.ensureFresh(); });
    expect(fetchOptions).toHaveBeenCalledTimes(1);
    await act(async () => finish({ options: [] }));
    await act(async () => { await result.ensureFresh(); });
    expect(fetchOptions).toHaveBeenCalledTimes(1);
    expect(result.status).toBe('ready');
    await act(async () => { result.invalidate(); result.invalidate(); });
    expect(fetchOptions).toHaveBeenCalledTimes(1);
    const finishRefresh = deferNextOptions();
    await act(async () => { void result.ensureFresh(); });
    expect(result.status).toBe('ready');
    expect(fetchOptions).toHaveBeenCalledTimes(2);
    await act(async () => finishRefresh({ options: [] }));
  });
  it('does not clear an invalidation that happens during a request', async () => {
    const finish = deferNextOptions();
    await render(request);
    await act(async () => { result.invalidate(); });
    await act(async () => finish({ options: [{ id: 'old', label: 'Earlier refresh' }] }));
    await act(async () => { await result.ensureFresh(); });
    expect(fetchOptions).toHaveBeenCalledTimes(2);
    expect(result.options).toEqual([]);
  });
  it('manual refresh replaces an in-flight request and ignores its late response', async () => {
    const finishOld = deferNextOptions();
    await render(request);
    const signal = fetchOptions.mock.calls[0][0]!;
    await act(async () => { await result.refresh(); });
    expect(signal.aborted).toBe(true);
    await act(async () => finishOld({ options: [{ id: 'old', label: 'Old' }] }));
    expect(result.options).toEqual([]);
    expect(onOptions).toHaveBeenCalledTimes(1);
  });
  it('aborts old scopes and ignores late successful responses', async () => {
    const finish = deferNextOptions();
    await render(request);
    const signal = fetchOptions.mock.calls[0][0]!;
    await render({ scope: 'realtime', window: '30m' });
    expect(signal.aborted).toBe(true);
    await act(async () => finish({ options: [{ id: 'old', label: 'Old scope' }] }));
    expect(result.options).toEqual([]);
    expect(onOptions).toHaveBeenCalledTimes(1);
    await render(null);
    expect(fetchOptions).toHaveBeenCalledTimes(2);
  });
  it('keeps candidates without a loading row during both same-scope and changed-scope refreshes', async () => {
    const existing = [{ id: '1', label: 'Used' }];
    fetchOptions.mockResolvedValueOnce({ options: existing });
    await render(request);
    const finishRefresh = deferNextOptions();
    await act(async () => { void result.refresh(); });
    expect(result.status).toBe('ready');
    expect(result.options).toEqual(existing);
    await act(async () => finishRefresh({ options: [] }));
    expect(result.status).toBe('ready');
    expect(result.options).toEqual([]);

    fetchOptions.mockResolvedValueOnce({ options: existing });
    await act(async () => { await result.refresh(); });
    const finishScopeChange = deferNextOptions();
    await render({ scope: 'overview', range: { range: '8h' } }, false);
    expect(fetchOptions).toHaveBeenCalledTimes(3);
    expect(result.options).toEqual(existing);
    await act(async () => { void result.ensureFresh(); });
    expect(result.status).toBe('ready');
    expect(result.options).toEqual(existing);
    await act(async () => finishScopeChange({ options: [] }));
  });
  it('returns an empty result without loading when no query is valid and ignores a canceled response', async () => {
    await render(null);
    expect(result.status).toBe('ready');
    expect(result.options).toEqual([]);
    await act(async () => { await result.refresh(); });
    expect(fetchOptions).not.toHaveBeenCalled();

    const finish = deferNextOptions();
    await render(request);
    const signal = fetchOptions.mock.calls[0][0]!;
    await render(null);
    expect(signal.aborted).toBe(true);
    await act(async () => finish({ options: [{ id: 'old', label: 'Old' }] }));
    expect(result.status).toBe('ready');
    expect(result.options).toEqual([]);
    expect(onOptions).not.toHaveBeenCalled();

    fetchOptions.mockResolvedValueOnce({ options: [{ id: 'new', label: 'New' }] });
    await render(request);
    expect(result.status).toBe('ready');
    expect(result.options).toEqual([{ id: 'new', label: 'New' }]);
  });
  it('preserves candidates on errors and retries on the next opening', async () => {
    fetchOptions.mockResolvedValueOnce({ options: [{ id: '1', label: 'Used' }] });
    await render(request);
    fetchOptions.mockRejectedValueOnce(new Error('offline'));
    await act(async () => { await result.refresh(); });
    expect(result.status).toBe('error');
    expect(result.options).toEqual([{ id: '1', label: 'Used' }]);
    await act(async () => { await result.ensureFresh(); });
    expect(result.status).toBe('ready');
  });
});
