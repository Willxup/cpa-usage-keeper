// @vitest-environment happy-dom

import { act, useEffect } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import * as api from '@/lib/api';
import type { ModelPricingConfig, PricingModelsResponse } from '@/lib/types';
import { useModelPricingData } from '../useModelPricingData';

vi.mock('@/lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/lib/api')>(),
  fetchPricingModels: vi.fn(),
  fetchPricingModelOptions: vi.fn(),
  savePricingModel: vi.fn(),
  deletePricingModel: vi.fn(),
}));

const config: ModelPricingConfig = {
  model: 'provider/model',
  pricing_style: 'claude',
  base_prices: { input: 2, output: 5, cache_read: 0.2, cache_write: 3 },
  model_multiplier: 0,
  conditional_multipliers: [{ key: 'service_tier', value: 'Priority', multiplier: 2 }],
  branches: [{
    id: 'large',
    name: 'Large context',
    context: { type: 'gt', threshold: 200000 },
    period: { type: 'window', start: '20:00', end: '08:00' },
    prices: { input: 4, output: 6, cache_read: 1, cache_write: 3 },
  }],
};

let latest: ReturnType<typeof useModelPricingData> | null = null;
function Harness({ enabled = true, onAuthRequired }: { enabled?: boolean; onAuthRequired?: () => void }) {
  const result = useModelPricingData({ enabled, onAuthRequired });
  useEffect(() => { latest = result; }, [result]);
  return null;
}

describe('useModelPricingData', () => {
  let root: Root;
  let container: HTMLDivElement;

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    vi.resetAllMocks();
    vi.mocked(api.fetchPricingModels).mockResolvedValue({ models: [config], config_revision: 7 });
    vi.mocked(api.fetchPricingModelOptions).mockResolvedValue({ models: ['provider/model', 'other'] });
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    latest = null;
  });

  it('loads complete models, revision and options on entry and manual refresh', async () => {
    await act(async () => root.render(<Harness />));
    expect(latest!.models).toEqual([config]);
    expect(latest!.configRevision).toBe(7);
    expect(latest!.modelOptions).toEqual(['provider/model', 'other']);

    vi.mocked(api.fetchPricingModels).mockResolvedValueOnce({ models: [], config_revision: 8 });
    await act(async () => expect(latest!.loadPricing()).resolves.toBe(true));
    expect(latest!.models).toEqual([]);
    expect(latest!.configRevision).toBe(8);
    expect(api.fetchPricingModels).toHaveBeenCalledTimes(2);
    expect(api.fetchPricingModelOptions).toHaveBeenCalledTimes(2);
  });

  it('ignores late responses after a newer refresh and cancels the old read', async () => {
    await act(async () => root.render(<Harness enabled={false} />));
    const stale = Promise.withResolvers<PricingModelsResponse>();
    vi.mocked(api.fetchPricingModels)
      .mockReturnValueOnce(stale.promise)
      .mockResolvedValueOnce({ models: [], config_revision: 9 });
    let first!: Promise<boolean>;
    act(() => { first = latest!.loadPricing(); });
    await act(async () => expect(latest!.loadPricing()).resolves.toBe(true));
    expect(vi.mocked(api.fetchPricingModels).mock.calls[0][0]?.aborted).toBe(true);
    await act(async () => {
      stale.resolve({ models: [config], config_revision: 1 });
      await expect(first).resolves.toBe(false);
    });
    expect(latest!.models).toEqual([]);
    expect(latest!.configRevision).toBe(9);
  });

  it('cancels a pending read when the editor becomes disabled', async () => {
    const pending = Promise.withResolvers<PricingModelsResponse>();
    vi.mocked(api.fetchPricingModels).mockReturnValueOnce(pending.promise);
    await act(async () => root.render(<Harness />));
    expect(latest!.loading).toBe(true);
    const signal = vi.mocked(api.fetchPricingModels).mock.calls[0][0]!;

    await act(async () => root.render(<Harness enabled={false} />));
    expect(signal.aborted).toBe(true);
    expect(latest!.loading).toBe(false);
    await act(async () => { pending.resolve({ models: [config], config_revision: 1 }); });
    expect(latest!.configRevision).toBeNull();
    expect(latest!.models).toEqual([]);
  });

  it('saves the complete configuration once and reports a later refresh failure separately', async () => {
    await act(async () => root.render(<Harness />));
    vi.mocked(api.savePricingModel).mockResolvedValueOnce({ model: config.model, config_revision: 8 });
    vi.mocked(api.fetchPricingModels).mockRejectedValueOnce(new Error('list unavailable'));

    let outcome: Awaited<ReturnType<NonNullable<typeof latest>['saveModel']>> | undefined;
    await act(async () => { outcome = await latest!.saveModel(config); });
    expect(api.savePricingModel).toHaveBeenCalledOnce();
    expect(api.savePricingModel).toHaveBeenCalledWith(config);
    expect(outcome).toEqual({ result: { model: config.model, config_revision: 8 }, refreshed: false });
    expect(latest!.error).toBe('list unavailable');
    expect(latest!.models).toEqual([config]);
    expect(latest!.configRevision).toBe(7);
  });

  it('deletes once, refreshes, and preserves a mutation error without changing the list', async () => {
    await act(async () => root.render(<Harness />));
    vi.mocked(api.deletePricingModel).mockResolvedValueOnce({ config_revision: 8 });
    vi.mocked(api.fetchPricingModels).mockResolvedValueOnce({ models: [], config_revision: 8 });
    await act(async () => expect(latest!.deleteModel(config.model)).resolves.toEqual({
      result: { config_revision: 8 }, refreshed: true,
    }));
    expect(api.deletePricingModel).toHaveBeenCalledWith(config.model);
    expect(latest!.models).toEqual([]);

    const writeError = new Error('delete failed');
    vi.mocked(api.deletePricingModel).mockRejectedValueOnce(writeError);
    await act(async () => expect(latest!.deleteModel(config.model)).rejects.toBe(writeError));
    expect(api.fetchPricingModels).toHaveBeenCalledTimes(2);
    expect(latest!.models).toEqual([]);
  });

  it('uses the latest admin auth callback for read and write failures', async () => {
    const oldAuth = vi.fn();
    const newAuth = vi.fn();
    await act(async () => root.render(<Harness onAuthRequired={oldAuth} />));
    await act(async () => root.render(<Harness onAuthRequired={newAuth} />));
    vi.mocked(api.fetchPricingModels).mockRejectedValueOnce(new api.ApiError('auth required', 401));
    await act(async () => expect(latest!.loadPricing()).resolves.toBe(false));
    vi.mocked(api.savePricingModel).mockRejectedValueOnce(new api.ApiError('auth required', 401));
    await act(async () => expect(latest!.saveModel(config)).rejects.toBeInstanceOf(api.ApiError));
    expect(oldAuth).not.toHaveBeenCalled();
    expect(newAuth).toHaveBeenCalledTimes(2);
  });
});
