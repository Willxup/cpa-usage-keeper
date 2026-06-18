import { readFileSync } from 'node:fs';
import React from 'react';
import '@/i18n';
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { buildPricingEntryKey } from '@/lib/types';
import {
  buildPricingModelOptions,
  markPricingSyncFailures,
  notifyPricingSyncUnexpectedError,
  PriceSettingsCard,
  type PricingSyncDraft,
} from './PriceSettingsCard';

const countOccurrences = (text: string, value: string) => text.split(value).length - 1;
const source = readFileSync(new URL('./PriceSettingsCard.tsx', import.meta.url), 'utf8');

const syncDraft = (model: string, serviceTier: '' | 'default' | 'priority' = 'default'): PricingSyncDraft => ({
  model,
  serviceTier,
  pricingKey: buildPricingEntryKey({ model, service_tier: serviceTier }),
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
        pricingEntries={[]}
        onPricingEntriesChange={() => undefined}
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
        pricingEntries={[{
          model: 'claude-sonnet',
          service_tier: 'priority',
          pricing_style: 'claude',
          prompt_price_per_1m: 3,
          completion_price_per_1m: 15,
          cache_price_per_1m: 0.3,
          cache_creation_price_per_1m: 3.75,
        }]}
        onPricingEntriesChange={() => undefined}
        loading={false}
      />,
    );

    expect(html).toContain('Claude');
    expect(html).toContain('Priority');
    expect(html).toContain('Cache Read');
    expect(html).toContain('$0.3000/1M');
    expect(html).toContain('Cache Write');
    expect(html).toContain('$3.7500/1M');
  });

  it('shows the sync prices action when sync preview is available', () => {
    const html = renderToStaticMarkup(
      <PriceSettingsCard
        modelNames={['gpt-4o']}
        pricingEntries={[]}
        onPricingEntriesChange={() => undefined}
        onSyncPreview={async () => ({
          source_id: 'openai_official',
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
    expect(html).toContain('OpenAI Official');
  });

  it('marks failed sync drafts by pricing key and keeps only failed tiers selected for retry', () => {
    const marked = markPricingSyncFailures([
      syncDraft('gpt-4o', 'default'),
      syncDraft('gpt-4o', 'priority'),
      syncDraft('claude-sonnet'),
    ], {
      success_keys: ['gpt-4o::default', 'claude-sonnet::'],
      failures: [{
        model: 'gpt-4o',
        service_tier: 'priority',
        pricing_key: 'gpt-4o::priority',
        message: 'network unavailable',
      }],
    });

    expect(marked.find((draft) => draft.pricingKey === 'gpt-4o::default')).toMatchObject({
      selected: false,
      saveStatus: undefined,
      saveError: undefined,
    });
    expect(marked.find((draft) => draft.pricingKey === 'gpt-4o::priority')).toMatchObject({
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
  it('marks only the configured tier for a model while keeping other tiers available', () => {
    const options = buildPricingModelOptions(
      ['priced-zeta', 'unpriced-beta', 'priced-alpha', 'unpriced-alpha'],
      [
        {
          model: 'priced-zeta',
          service_tier: 'priority',
          pricing_style: 'openai',
          prompt_price_per_1m: 3,
          completion_price_per_1m: 15,
          cache_price_per_1m: 0.3,
          cache_creation_price_per_1m: 0,
        },
        {
          model: 'priced-alpha',
          service_tier: 'default',
          pricing_style: 'openai',
          prompt_price_per_1m: 2,
          completion_price_per_1m: 8,
          cache_price_per_1m: 0.2,
          cache_creation_price_per_1m: 0,
        },
      ],
      'default',
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
      suffixAriaLabel: 'Configured',
    });
    expect(options.find((option) => option.value === 'priced-alpha')?.suffix).toBeTruthy();
    expect(options.find((option) => option.value === 'priced-zeta')?.suffix).toBeUndefined();
    expect(options.find((option) => option.value === 'unpriced-alpha')?.suffix).toBeUndefined();
  });

  it('treats fallback pricing as configured for default, priority, and fallback selections', () => {
    const pricingEntries = [{
      model: 'gpt-image-2',
      service_tier: '',
      pricing_style: 'openai' as const,
      prompt_price_per_1m: 5,
      completion_price_per_1m: 30,
      cache_price_per_1m: 1.25,
      cache_creation_price_per_1m: 0,
    }];

    const defaultOptions = buildPricingModelOptions(
      ['gpt-image-2'],
      pricingEntries,
      'default',
      'Select model',
      'Configured',
    );
    const priorityOptions = buildPricingModelOptions(
      ['gpt-image-2'],
      pricingEntries,
      'priority',
      'Select model',
      'Configured',
    );
    const fallbackOptions = buildPricingModelOptions(
      ['gpt-image-2'],
      pricingEntries,
      '',
      'Select model',
      'Configured',
    );

    expect(defaultOptions.find((option) => option.value === 'gpt-image-2')).toMatchObject({
      suffixAriaLabel: 'Configured',
    });
    expect(priorityOptions.find((option) => option.value === 'gpt-image-2')).toMatchObject({
      suffixAriaLabel: 'Configured',
    });
    expect(fallbackOptions.find((option) => option.value === 'gpt-image-2')).toMatchObject({
      suffixAriaLabel: 'Configured',
    });
  });
});
