// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import type { ModelPrice, PricingSaveResult, PricingSyncPreviewResponse } from '@/lib/types';
import { PriceSettingsCard, type PriceSettingsCardProps } from '../PriceSettingsCard';

const price: ModelPrice = { style: 'openai', prompt: 3, completion: 15, cacheRead: 0.3, cacheWrite: 3.75, multiplier: 1.5 };
const preview: PricingSyncPreviewResponse = {
  source: 'Models.dev', source_url: 'https://models.dev/api.json', metadata_models: 1,
  unmatched_models: [],
  matches: [{
    model: 'model-a', matched_model: 'model-a', match_type: 'exact',
    source_provider_id: 'openai', source_provider_name: 'OpenAI', pricing_style: 'openai',
    prompt_price_per_1m: 3, completion_price_per_1m: 15,
    cache_read_price_per_1m: 0.3, cache_write_price_per_1m: 3.75,
  }],
};

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

function button(text: string, scope: ParentNode = document.body) {
  const found = Array.from(scope.querySelectorAll<HTMLButtonElement>('button'))
    .find((node) => node.textContent?.trim() === text);
  expect(found, `button ${text}`).toBeDefined();
  return found!;
}

const click = async (target: HTMLElement) => { await act(async () => target.click()); };
const dialog = () => document.querySelector<HTMLElement>('[role="dialog"]')!;

function expectInputsDisabled(scope: ParentNode, expectedCount: number) {
  const controls = scope.querySelectorAll<HTMLInputElement | HTMLButtonElement>('input, button[aria-haspopup="listbox"]');
  expect(controls).toHaveLength(expectedCount);
  for (const control of controls) expect(control.disabled).toBe(true);
}

describe('PriceSettingsCard persistence', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    await i18n.changeLanguage('en');
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    vi.useRealTimers();
  });

  async function renderCard(props: Partial<PriceSettingsCardProps>) {
    await act(async () => root.render(<PriceSettingsCard
      modelNames={['model-a']} modelPrices={{}}
      onPriceSave={() => undefined} onPriceDelete={() => undefined}
      {...props}
    />));
  }

  it.each(['create', 'edit', 'delete'])('waits for %s persistence before success and locks the active form', async (action) => {
    vi.useFakeTimers();
    const pending = deferred<void>();
    const onPriceSave = vi.fn(() => pending.promise);
    const onPriceDelete = vi.fn(() => pending.promise);
    const onNotice = vi.fn();
    await renderCard({ modelPrices: action === 'create' ? {} : { 'model-a': price }, onPriceSave, onPriceDelete, onNotice });

    if (action === 'create') {
      await click(container.querySelector<HTMLButtonElement>('button[aria-haspopup="listbox"]')!);
      await click(button('model-a'));
    } else {
      await click(button(action === 'edit' ? 'Edit' : 'Delete'));
    }
    expect(onPriceSave).not.toHaveBeenCalled();
    expect(onPriceDelete).not.toHaveBeenCalled();
    expect(onNotice).not.toHaveBeenCalled();

    const scope = action === 'create' ? container : dialog();
    if (action !== 'delete') {
      expect(scope.textContent).toContain('Cache Read');
      expect(scope.textContent).toContain('Cache Write');
      const inputs = scope.querySelectorAll<HTMLInputElement>('input[type="number"]');
      expect(inputs).toHaveLength(5);
      await act(async () => {
        [3, 15, 0.3, 3.75, 1.5].forEach((value, index) => {
          Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(inputs[index], String(value));
          inputs[index].dispatchEvent(new Event('input', { bubbles: true }));
        });
      });
    }
    await click(button(action === 'delete' ? 'Delete price' : 'Save', scope));
    if (action === 'delete') {
      expect(onPriceDelete).toHaveBeenCalledExactlyOnceWith('model-a');
    } else {
      expect(onPriceSave).toHaveBeenCalledExactlyOnceWith('model-a', price);
      expectInputsDisabled(scope, action === 'create' ? 7 : 6);
    }
    expect(onNotice).not.toHaveBeenCalled();
    if (action !== 'create') {
      expect(button('Cancel', scope).disabled).toBe(true);
      expect(scope.querySelector<HTMLButtonElement>('.modal-close-floating')!.disabled).toBe(true);
      await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
      expect(dialog()).toBe(scope);
    }

    await act(async () => { pending.resolve(); await pending.promise; });
    expect(onNotice).toHaveBeenCalledExactlyOnceWith('success', expect.any(String));
    if (action === 'create') {
      expect(container.querySelector<HTMLInputElement>('input[type="number"]')!.disabled).toBe(false);
    } else {
      await act(async () => vi.runOnlyPendingTimersAsync());
      expect(dialog()).toBeNull();
    }
  });

  it('locks sync drafts while applying and exposes failed models for retry', async () => {
    const pending = deferred<PricingSaveResult>();
    const onSyncPricesChange = vi.fn(() => pending.promise);
    const onNotice = vi.fn();
    await renderCard({ onSyncPreview: async () => preview, onSyncPricesChange, onNotice });
    await click(button('Sync Prices'));
    const scope = dialog();
    expect(scope.textContent).toContain('Cache Read');
    expect(scope.textContent).toContain('Cache Write');
    await click(button('Update Selected (1)', scope));
    expect(onSyncPricesChange).toHaveBeenCalledExactlyOnceWith({ 'model-a': { ...price, multiplier: 1 } });
    expectInputsDisabled(scope, 7);
    expect(button('Cancel', scope).disabled).toBe(true);
    expect(onNotice).not.toHaveBeenCalled();

    await act(async () => {
      pending.resolve({ successModels: [], failures: [{ model: 'model-a', message: 'network unavailable' }] });
      await pending.promise;
    });
    expect(scope.querySelector('[role="img"][aria-label="Price sync failed for model-a."]')?.getAttribute('title')).toBe('network unavailable');
    expect(scope.querySelector<HTMLInputElement>('input[type="checkbox"]')!.checked).toBe(true);
    expect(button('Update Selected (1)', scope).disabled).toBe(false);
    expect(onNotice).toHaveBeenCalledWith('error', expect.stringContaining('network unavailable'));
  });

  it.each(['preview', 'apply'])('reports unexpected %s failures through the notice callback', async (phase) => {
    const onNotice = vi.fn();
    const fail = async () => { throw new Error('connection reset'); };
    await renderCard({ onSyncPreview: phase === 'preview' ? fail : async () => preview, onSyncPricesChange: fail, onNotice });
    await click(button('Sync Prices'));
    if (phase === 'apply') await click(button('Update Selected (1)', dialog()));
    expect(onNotice).toHaveBeenCalledExactlyOnceWith('error', expect.stringContaining('connection reset'));
  });
});
