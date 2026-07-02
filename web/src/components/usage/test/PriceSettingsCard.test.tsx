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
  pricingDraftToModelPrice,
  syncDraftToModelPrice,
  syncMatchToDraft,
  type PricingSyncDraft,
} from '../PriceSettingsCard';

const countOccurrences = (text: string, value: string) => text.split(value).length - 1;
const source = readFileSync(new URL('../PriceSettingsCard.tsx', import.meta.url), 'utf8');

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
  multiplier: '1',
});

describe('PriceSettingsCard', () => {
  it('uses the model pricing settings title', () => {
    const html = renderToStaticMarkup(
      <PriceSettingsCard
        modelNames={[]}
        modelPrices={{}}
        onPricesChange={() => undefined}
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
            multiplier: 1,
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
    expect(html).toContain('Multiplier');
    expect(html).toContain('1');
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

  it('opens edit without showing a top notice before the user saves', () => {
    const editHandlerStart = source.indexOf('const handleOpenEdit = (model: string) => {');
    const editHandlerEnd = source.indexOf('\n  const handleSaveEdit = () => {', editHandlerStart);
    const editHandler = source.slice(editHandlerStart, editHandlerEnd);

    expect(editHandlerStart).toBeGreaterThanOrEqual(0);
    expect(editHandler).toContain('setEditModel(model)');
    expect(editHandler).not.toContain('onNotice');
    expect(source).toContain("onNotice?.('success', t('usage_stats.model_price_edit_success'))");
  });

  it('requires confirmation before deleting a saved model price', () => {
    expect(source).toContain('const [deleteModel, setDeleteModel] = useState<string | null>(null);');
    expect(source).toContain('const confirmDeleteModel = () => {');
    expect(source).toContain("onClick={() => setDeleteModel(model)}");
    expect(source).toContain("title={t('usage_stats.model_price_delete_confirm_title')}");
    expect(source).toContain("t('usage_stats.model_price_delete_confirm_action')");
  });

  it('keeps explicit zero multipliers when converting sync drafts', () => {
    expect(syncDraftToModelPrice({ ...syncDraft('free-model'), multiplier: '0' })?.multiplier).toBe(0);
    expect(syncDraftToModelPrice({ ...syncDraft('bad-model'), multiplier: '-1' })).toBeNull();
  });

  it('keeps create and edit draft multipliers when converting to saved prices', () => {
    expect(pricingDraftToModelPrice({ ...syncDraft('free-model'), multiplier: '0' })?.multiplier).toBe(0);
    expect(pricingDraftToModelPrice({ ...syncDraft('scaled-model'), multiplier: '2.5' })?.multiplier).toBe(2.5);
    expect(pricingDraftToModelPrice({ ...syncDraft('bad-model'), multiplier: '-1' })).toBeNull();
  });

  it('defaults new sync matches to multiplier 1 and preserves existing model multipliers', () => {
    const match = {
      model: 'free-model',
      matched_model: 'free-model',
      match_type: 'exact',
      source_provider_id: 'openai',
      source_provider_name: 'OpenAI',
      pricing_style: 'openai' as const,
      prompt_price_per_1m: 2.5,
      completion_price_per_1m: 10,
      cache_price_per_1m: 1.25,
      cache_creation_price_per_1m: 0,
    };

    expect(syncMatchToDraft(match).multiplier).toBe('1');
    expect(syncMatchToDraft(match, {
      style: 'openai',
      prompt: 1,
      completion: 2,
      cache: 0.1,
      cacheCreation: 0,
      multiplier: 0,
    }).multiplier).toBe('0');
  });
});

describe('buildPricingModelOptions', () => {
  it('keeps configured models visible but disabled', () => {
    const options = buildPricingModelOptions(
      ['priced-zeta', 'unpriced-beta', 'priced-alpha', 'unpriced-alpha'],
      {
        'priced-zeta': { style: 'openai', prompt: 3, completion: 15, cache: 0.3, cacheCreation: 0, multiplier: 1 },
        'priced-alpha': { style: 'openai', prompt: 2, completion: 8, cache: 0.2, cacheCreation: 0, multiplier: 1 },
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
