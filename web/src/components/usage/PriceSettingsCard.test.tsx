import { readFileSync } from 'node:fs';
import React from 'react';
import '@/i18n';
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import {
  buildPricingModelOptions,
  markPricingSyncFailures,
  notifyPricingSyncUnexpectedError,
  PriceSettingsCard,
  type PricingSyncDraft,
} from './PriceSettingsCard';

const countOccurrences = (text: string, value: string) => text.split(value).length - 1;
const source = readFileSync(new URL('./PriceSettingsCard.tsx', import.meta.url), 'utf8');

const syncDraft = (model: string): PricingSyncDraft => ({
  model,
  matchedModel: model,
  matchType: 'exact',
  sourceProviderId: 'openai',
  sourceProviderName: 'OpenAI',
  selected: true,
  style: 'openai',
  prompt: '2.5',
  completion: '10',
  cache: '1.25',
  cacheCreation: '0',
});

describe('PriceSettingsCard', () => {
  it('uses the model pricing settings title', () => {
    const html = renderToStaticMarkup(
      <PriceSettingsCard
        modelNames={[]}
        modelPrices={{}}
        onPricesChange={() => undefined}
        onSyncPrices={async () => undefined}
        loading={false}
      />,
    );

    expect(html).toContain('Model Pricing Settings');
    expect(countOccurrences(html, 'Pricing Settings')).toBe(1);
    expect(html).not.toContain('Model Pricing Table');
  });

  it('renders Claude pricing style with cache read and write prices', () => {
    const html = renderToStaticMarkup(
      <PriceSettingsCard
        modelNames={['claude-sonnet']}
        modelPrices={{
          'claude-sonnet': {
            style: 'claude',
            prompt: 3,
            completion: 15,
            cache: 0.3,
            cacheCreation: 3.75,
          },
        }}
        onPricesChange={() => undefined}
        loading={false}
      />,
    );

    expect(html).toContain('Claude');
    expect(html).toContain('Cache Read');
    expect(html).toContain('$0.3000/1M');
    expect(html).toContain('Cache Write');
    expect(html).toContain('$3.7500/1M');
  });

  it('shows the sync prices action when sync preview is available', () => {
    const html = renderToStaticMarkup(
      <PriceSettingsCard
        modelNames={['gpt-4o']}
        modelPrices={{}}
        onPricesChange={() => undefined}
        onSyncPreview={async () => ({
          source: 'Models.dev',
          source_url: 'https://models.dev/api.json',
          metadata_models: 1,
          matches: [],
          unmatched_models: [],
        })}
        loading={false}
      />,
    );

    expect(html).toContain('Sync Prices');
    expect(html).toContain('Models.dev');
  });

  it('marks failed sync drafts and keeps them selected for retry', () => {
    const marked = markPricingSyncFailures([
      syncDraft('gpt-4o'),
      syncDraft('gpt-4o-mini'),
      syncDraft('claude-sonnet'),
    ], {
      successModels: ['gpt-4o', 'claude-sonnet'],
      failures: [{ model: 'gpt-4o-mini', message: 'network unavailable' }],
    });

    expect(marked.find((draft) => draft.model === 'gpt-4o')).toMatchObject({
      selected: false,
      saveStatus: undefined,
      saveError: undefined,
    });
    expect(marked.find((draft) => draft.model === 'gpt-4o-mini')).toMatchObject({
      selected: true,
      saveStatus: 'failed',
      saveError: 'network unavailable',
    });
  });

  it('renders a small red alert marker for failed sync drafts', () => {
    expect(source).toContain('IconCircleAlert');
    expect(source).toContain('syncDraftFailureIcon');
    expect(source).toContain('model_price_sync_apply_partial');
  });

  it('notifies when pricing sync throws an unexpected error', () => {
    const notices: Array<{ kind: string; message: string }> = [];

    notifyPricingSyncUnexpectedError(
      new Error('connection reset'),
      (key) => (key === 'usage_stats.model_price_sync_failed' ? 'Unable to sync model prices' : key),
      (kind, message) => notices.push({ kind, message }),
    );

    expect(notices).toEqual([
      { kind: 'error', message: 'Unable to sync model prices: connection reset' },
    ]);
    expect(source).toContain('notifyPricingSyncUnexpectedError(error, t, onNotice)');
  });
});

describe('buildPricingModelOptions', () => {
  it('keeps configured models visible but disabled', () => {
    const options = buildPricingModelOptions(
      ['priced-zeta', 'unpriced-beta', 'priced-alpha', 'unpriced-alpha'],
      {
        'priced-zeta': { style: 'openai', prompt: 3, completion: 15, cache: 0.3, cacheCreation: 0 },
        'priced-alpha': { style: 'openai', prompt: 2, completion: 8, cache: 0.2, cacheCreation: 0 },
      },
      'Select model',
      'Configured',
    );

    expect(options.map((option) => option.value)).toEqual([
      '',
      'priced-alpha',
      'priced-zeta',
      'unpriced-alpha',
      'unpriced-beta',
    ]);
    expect(options.find((option) => option.value === 'priced-alpha')).toMatchObject({
      disabled: true,
      suffixAriaLabel: 'Configured',
    });
    expect(options.find((option) => option.value === 'priced-alpha')?.suffix).toBeTruthy();
    expect(options.find((option) => option.value === 'unpriced-alpha')?.suffix).toBeUndefined();
    expect(options.find((option) => option.value === 'unpriced-alpha')?.disabled).toBeUndefined();
  });
});
