// @vitest-environment happy-dom

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/lib/api';
import type { PricingSyncPreviewResponse } from '@/lib/types';
import { PriceSettingsCard } from '../../PriceSettingsCard';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string, params?: Record<string, string | number>) => {
    const labels: Record<string, string> = {
      'usage_stats.model_price_sync': 'Sync Prices',
      'usage_stats.model_price_sync_source': 'Source',
      'usage_stats.model_price_sync_title': 'Sync preview',
      'usage_stats.model_price_sync_update_selected': 'Apply {{count}}',
      'usage_stats.model_price_sync_timeout': '{{source}} connection timed out.',
      'common.cancel': 'Cancel',
    };
    return (labels[key] ?? key).replace(/\{\{(\w+)\}\}/g, (_, name: string) => String(params?.[name] ?? ''));
  } }),
}));

const preview = (source: string): PricingSyncPreviewResponse => ({
  source_id: source === 'litellm' ? 'litellm' : 'models-dev',
  source: source === 'litellm' ? 'LiteLLM' : 'Models.dev',
  source_url: source === 'litellm' ? 'https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json' : 'https://models.dev/api.json',
  metadata_models: 1,
  matches: [{ model: `${source}-model`, matched_model: `${source}-model`, match_type: 'index_exact',
    source_provider_id: 'openai', source_provider_name: 'OpenAI', pricing_style: 'openai',
    prompt_price_per_1m: 2.5, completion_price_per_1m: 10, cache_read_price_per_1m: 0, cache_write_price_per_1m: 0 }],
  unmatched_models: [],
});

const deferred = <T,>() => {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => { resolve = r; });
  return { promise, resolve };
};

const button = (text: string) => {
  const found = [...document.querySelectorAll<HTMLButtonElement>('button')].find((node) => node.textContent?.trim() === text);
  expect(found, `button ${text}`).toBeDefined();
  return found!;
};

const selectLiteLLM = async () => {
  const select = document.querySelector<HTMLButtonElement>('[aria-label^="Source:"]');
  expect(select).not.toBeNull();
  await act(async () => select!.click());
  const option = [...document.querySelectorAll<HTMLElement>('[role="option"]')].find((node) => node.textContent === 'LiteLLM');
  expect(option).toBeDefined();
  await act(async () => option!.click());
};

describe('pricing sync source selection', () => {
  let root: Root;
  let container: HTMLDivElement;
  beforeEach(() => {
    localStorage.clear();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });
  afterEach(async () => {
    await act(async () => root.unmount());
    document.body.innerHTML = '';
    vi.restoreAllMocks();
  });

  const render = async (onPreview: (source: string, signal?: AbortSignal) => Promise<PricingSyncPreviewResponse>, onNotice = vi.fn()) => {
    const save = vi.fn(async (prices) => ({ successModels: Object.keys(prices), failures: [] }));
    await act(async () => root.render(<PriceSettingsCard modelNames={[]} modelPrices={{
      'litellm-model': { style: 'openai', prompt: 1, completion: 2, cacheRead: 3, cacheWrite: 4, multiplier: 0.4 },
    }} onPriceSave={vi.fn()} onPriceDelete={vi.fn()} onSyncPreview={onPreview} onSyncPricesChange={save} onNotice={onNotice} />));
    return save;
  };

  it('remembers the chosen source and saves zero cache prices only after confirmation', async () => {
    const onPreview = vi.fn(async (source: string) => preview(source));
    const save = await render(onPreview);
    expect(document.querySelector('[aria-label^="Source:"]')?.textContent).toContain('Models.dev');
    await selectLiteLLM();
    expect(localStorage.getItem('cpa-pricing-sync-source-v1')).toBe('litellm');
    await act(async () => button('Sync Prices').click());
    expect(onPreview.mock.calls[0]?.[0]).toBe('litellm');
    expect(save).not.toHaveBeenCalled();
    expect(document.querySelector('a[href*="BerriAI/litellm"]')).not.toBeNull();
    await act(async () => button('Apply 1').click());
    expect(save).toHaveBeenCalledTimes(1);
    expect(save).toHaveBeenCalledWith({ 'litellm-model': { style: 'openai', prompt: 2.5, completion: 10, cacheRead: 0, cacheWrite: 0, multiplier: 0.4 } });
  });

  it('aborts the previous source request and ignores its late preview', async () => {
    const old = deferred<PricingSyncPreviewResponse>();
    let oldSignal: AbortSignal | undefined;
    await render((source, signal) => {
      if (source === 'models-dev') { oldSignal = signal; return old.promise; }
      return Promise.resolve(preview(source));
    });
    await act(async () => button('Sync Prices').click());
    await selectLiteLLM();
    expect(oldSignal?.aborted).toBe(true);
    await act(async () => button('Sync Prices').click());
    await act(async () => old.resolve(preview('models-dev')));
    const dialog = document.querySelector('[role="dialog"]');
    expect(dialog?.textContent).toContain('litellm-model');
    expect(dialog?.textContent).not.toContain('models-dev-model');
  });

  it('restores the last source and names it in timeout feedback', async () => {
    localStorage.setItem('cpa-pricing-sync-source-v1', 'litellm');
    const notice = vi.fn();
    await render(async () => { throw new ApiError('timeout', 504); }, notice);
    expect(document.querySelector('[aria-label^="Source:"]')?.textContent).toContain('LiteLLM');
    await act(async () => button('Sync Prices').click());
    expect(notice).toHaveBeenCalledWith('error', 'LiteLLM connection timed out.');
  });

  it('falls back for an unknown saved source and cancels requests when leaving pricing', async () => {
    localStorage.setItem('cpa-pricing-sync-source-v1', 'removed-source');
    const pending = deferred<PricingSyncPreviewResponse>();
    let signal: AbortSignal | undefined;
    await render((_, requestSignal) => { signal = requestSignal; return pending.promise; });
    expect(document.querySelector('[aria-label^="Source:"]')?.textContent).toContain('Models.dev');
    await act(async () => button('Sync Prices').click());
    await act(async () => root.render(null));
    expect(signal?.aborted).toBe(true);
    await act(async () => pending.resolve(preview('models-dev')));
    expect(document.querySelector('[role="dialog"]')).toBeNull();
  });
});
