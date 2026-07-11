import { readFileSync } from 'node:fs';
import React from 'react';
import '@/i18n';
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import {
  buildPricingModelOptions,
  buildSelectedSyncPrices,
  markPricingSyncFailures,
  notifyPricingSyncUnexpectedError,
  PriceSettingsCard,
  pricingDraftToModelPrice,
  syncDraftToModelPrice,
  syncMatchToDraft,
  saveSyncDraftsWithSingleModelCallback,
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
  cacheRead: '1.25',
  cacheWrite: '0',
  multiplier: '1',
});

describe('PriceSettingsCard', () => {
  it('uses the model pricing settings title', () => {
    const html = renderToStaticMarkup(
      <PriceSettingsCard
        modelNames={[]}
        modelPrices={{}}
        onPriceSave={() => undefined}
        onPriceDelete={() => undefined}
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
            cacheRead: 0.3,
            cacheWrite: 3.75,
            multiplier: 1,
          },
        }}
        onPriceSave={() => undefined}
        onPriceDelete={() => undefined}
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

  it('renders OpenAI pricing style with cache read and write prices', () => {
		const html = renderToStaticMarkup(
			<PriceSettingsCard
				modelNames={['gpt-5.6-terra']}
				modelPrices={{
					'gpt-5.6-terra': {
						style: 'openai',
						prompt: 2.5,
						completion: 15,
						cacheRead: 0.25,
						cacheWrite: 3.125,
						multiplier: 1,
					},
				}}
				onPriceSave={() => undefined}
				onPriceDelete={() => undefined}
				loading={false}
			/>,
		);

		expect(html).toContain('OpenAI');
		expect(html).toContain('Cache Read');
		expect(html).toContain('$0.2500/1M');
		expect(html).toContain('Cache Write');
		expect(html).toContain('$3.1250/1M');
	});

	it('shows cache read and write controls for OpenAI create, edit and sync drafts', () => {
		const html = renderToStaticMarkup(
			<PriceSettingsCard
				modelNames={['gpt-5.6-terra']}
				modelPrices={{}}
				onPriceSave={() => undefined}
				onPriceDelete={() => undefined}
				loading={false}
			/>,
		);

		expect(html).toContain('Cache Read');
		expect(html).toContain('Cache Write');
		expect(source).not.toContain("t(pricingStyle === 'claude' ? 'usage_stats.model_price_cache_read' : 'usage_stats.model_price_cache')");
		expect(source).not.toContain("t(editStyle === 'claude' ? 'usage_stats.model_price_cache_read' : 'usage_stats.model_price_cache')");
		expect(source).not.toContain("t(draft.style === 'claude' ? 'usage_stats.model_price_cache_read' : 'usage_stats.model_price_cache')");
		expect(source).not.toContain("pricingStyle === 'claude' && (");
		expect(source).not.toContain("editStyle === 'claude' && (");
		expect(source).not.toContain("draft.style === 'claude' && (");
	});

  it('shows the sync prices action when sync preview is available', () => {
    const html = renderToStaticMarkup(
      <PriceSettingsCard
        modelNames={['gpt-4o']}
        modelPrices={{}}
        onPriceSave={() => undefined}
        onPriceDelete={() => undefined}
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
    const editHandlerEnd = source.indexOf('\n  const handleSaveEdit = async () => {', editHandlerStart);
    const editHandler = source.slice(editHandlerStart, editHandlerEnd);

    expect(editHandlerStart).toBeGreaterThanOrEqual(0);
    expect(editHandler).toContain('setEditModel(model)');
    expect(editHandler).not.toContain('onNotice');
    expect(source).toContain("onNotice?.('success', t('usage_stats.model_price_edit_success'))");
  });

  it('requires confirmation before deleting a saved model price', () => {
    expect(source).toContain('const [deleteModel, setDeleteModel] = useState<string | null>(null);');
    expect(source).toContain('const confirmDeleteModel = async () => {');
    expect(source).toContain("onClick={() => setDeleteModel(model)}");
    expect(source).toContain("title={t('usage_stats.model_price_delete_confirm_title')}");
    expect(source).toContain("t('usage_stats.model_price_delete_confirm_action')");
  });

  it('persists create, edit and delete through single-model callbacks before reporting success', () => {
    const saveHandlerStart = source.indexOf('const handleSavePrice = async () => {');
    const saveHandlerEnd = source.indexOf('\n  const confirmDeleteModel = async () => {', saveHandlerStart);
    const saveHandler = source.slice(saveHandlerStart, saveHandlerEnd);
    const deleteHandlerStart = source.indexOf('const confirmDeleteModel = async () => {');
    const deleteHandlerEnd = source.indexOf('\n  const handleOpenEdit = (model: string) => {', deleteHandlerStart);
    const deleteHandler = source.slice(deleteHandlerStart, deleteHandlerEnd);
    const editHandlerStart = source.indexOf('const handleSaveEdit = async () => {');
    const editHandlerEnd = source.indexOf('\n  const handleModelSelect = (value: string) => {', editHandlerStart);
    const editHandler = source.slice(editHandlerStart, editHandlerEnd);

    expect(source).toContain('onPriceSave: (model: string, price: ModelPrice) => void | Promise<void>;');
    expect(source).toContain('onPriceDelete: (model: string) => void | Promise<void>;');
    expect(source).not.toContain('onPricesChange');
    expect(saveHandler).toContain('await Promise.resolve(onPriceSave(selectedModel, price));');
    expect(saveHandler.indexOf('await Promise.resolve(onPriceSave(selectedModel, price));')).toBeLessThan(saveHandler.indexOf("onNotice?.('success'"));
    expect(editHandler).toContain('await Promise.resolve(onPriceSave(editModel, price));');
    expect(editHandler.indexOf('await Promise.resolve(onPriceSave(editModel, price));')).toBeLessThan(editHandler.indexOf("onNotice?.('success'"));
    expect(deleteHandler).toContain('await Promise.resolve(onPriceDelete(deleteModel));');
    expect(deleteHandler.indexOf('await Promise.resolve(onPriceDelete(deleteModel));')).toBeLessThan(deleteHandler.indexOf("onNotice?.('success'"));
    expect(source).not.toContain('const newPrices = { ...modelPrices');
    expect(source).not.toContain('delete newPrices[deleteModel]');
    expect(saveHandler).toContain('setPriceSaving(true);');
    expect(saveHandler).toContain('setPriceSaving(false);');
    expect(editHandler).toContain('setEditSaving(true);');
    expect(editHandler).toContain('setEditSaving(false);');
    expect(deleteHandler).toContain('setDeleteSaving(true);');
    expect(deleteHandler).toContain('setDeleteSaving(false);');
  });

  it('keeps the create form immutable while a price save is pending', () => {
    const createFormStart = source.indexOf('<div className={styles.priceForm}>');
    const createFormEnd = source.indexOf('\n              <div className={styles.pricesList}>', createFormStart);
    const createForm = source.slice(createFormStart, createFormEnd);

    expect(createFormStart).toBeGreaterThanOrEqual(0);
    expect(countOccurrences(createForm, 'disabled={priceSaving}')).toBeGreaterThanOrEqual(6);
    expect(createForm).toContain('disabled={!selectedModel || priceSaving}');
  });

  it('keeps sync draft pricing controls immutable while sync apply is pending', () => {
    const syncDraftGridStart = source.indexOf('<div className={styles.syncDraftGrid}>');
    const syncDraftGridEnd = source.indexOf('\n                      </div>\n                    </div>\n                  );', syncDraftGridStart);
    const syncDraftGrid = source.slice(syncDraftGridStart, syncDraftGridEnd);

    expect(syncDraftGridStart).toBeGreaterThanOrEqual(0);
    expect(syncDraftGrid).toContain('<Select');
    expect(countOccurrences(syncDraftGrid, 'disabled={syncApplying}')).toBeGreaterThanOrEqual(6);
  });

  it('keeps edit and delete modals locked while persistence is pending', () => {
    const closeEditStart = source.indexOf('const closeEditModal = () => {');
    const closeEditEnd = source.indexOf('\n  const closeDeleteModal = () => {', closeEditStart);
    const closeEdit = source.slice(closeEditStart, closeEditEnd);
    const closeDeleteStart = source.indexOf('const closeDeleteModal = () => {');
    const closeDeleteEnd = source.indexOf('\n  const handleSavePrice = async () => {', closeDeleteStart);
    const closeDelete = source.slice(closeDeleteStart, closeDeleteEnd);
    const editModalStart = source.indexOf('<Modal\n        open={editModel !== null}');
    const editModalEnd = source.indexOf('\n      </Modal>', editModalStart);
    const editModal = source.slice(editModalStart, editModalEnd);
    const deleteModalStart = source.indexOf('<Modal\n        open={deleteModel !== null}');
    const deleteModalEnd = source.indexOf('\n      </Modal>', deleteModalStart);
    const deleteModal = source.slice(deleteModalStart, deleteModalEnd);

    expect(closeEditStart).toBeGreaterThanOrEqual(0);
    expect(closeEdit).toContain('if (!editSaving) {');
    expect(closeEdit).toContain('setEditModel(null);');
    expect(closeDeleteStart).toBeGreaterThanOrEqual(0);
    expect(closeDelete).toContain('if (!deleteSaving) {');
    expect(closeDelete).toContain('setDeleteModel(null);');
    expect(editModalStart).toBeGreaterThanOrEqual(0);
    expect(editModal).toContain('onClose={closeEditModal}');
    expect(editModal).toContain('closeDisabled={editSaving}');
    expect(editModal).toContain('disabled={editSaving}');
    expect(deleteModalStart).toBeGreaterThanOrEqual(0);
    expect(deleteModal).toContain('onClose={closeDeleteModal}');
    expect(deleteModal).toContain('closeDisabled={deleteSaving}');
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

	it('parses OpenAI cache write prices without inferring missing values', () => {
		expect(pricingDraftToModelPrice({
			style: 'openai',
			prompt: '2.5',
			completion: '15',
			cacheRead: '0.25',
			cacheWrite: '3.125',
			multiplier: '1',
		})).toEqual({
			style: 'openai',
			prompt: 2.5,
			completion: 15,
			cacheRead: 0.25,
			cacheWrite: 3.125,
			multiplier: 1,
		});
		expect(pricingDraftToModelPrice({ ...syncDraft('blank-read'), cacheRead: '' })?.cacheRead).toBe(0);
		expect(pricingDraftToModelPrice({ ...syncDraft('blank-write'), cacheWrite: '' })?.cacheWrite).toBe(0);
		expect(pricingDraftToModelPrice({ ...syncDraft('negative-write'), cacheWrite: '-1' })).toBeNull();
		expect(pricingDraftToModelPrice({ ...syncDraft('claude-write'), style: 'claude', cacheWrite: '3.75' })?.cacheWrite).toBe(3.75);
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
      cache_read_price_per_1m: 1.25,
      cache_write_price_per_1m: 0,
    };

    expect(syncMatchToDraft(match).multiplier).toBe('1');
    expect(syncMatchToDraft(match, {
      style: 'openai',
      prompt: 1,
      completion: 2,
      cacheRead: 0.1,
      cacheWrite: 0,
      multiplier: 0,
    }).multiplier).toBe('0');
  });

  it('builds sync save payloads from selected drafts only', () => {
    const selected = syncDraft('gpt-4o');
    const unselected = { ...syncDraft('gpt-4o-mini'), selected: false };

    const result = buildSelectedSyncPrices([selected, unselected]);

    expect(result).toEqual({
      prices: {
        'gpt-4o': {
          style: 'openai',
          prompt: 2.5,
          completion: 10,
          cacheRead: 1.25,
          cacheWrite: 0,
          multiplier: 1,
        },
      },
      invalidModel: null,
      selectedDrafts: [selected],
    });
    expect(result.prices).not.toHaveProperty('gpt-4o-mini');
  });

	it('keeps Models.dev OpenAI cache write through draft and selected-price conversion', () => {
		const match = {
			model: 'gpt-5.6-terra',
			matched_model: 'gpt-5.6-terra',
			match_type: 'index_exact',
			source_provider_id: 'openai',
			source_provider_name: 'OpenAI',
			pricing_style: 'openai' as const,
			prompt_price_per_1m: 2.5,
			completion_price_per_1m: 15,
			cache_read_price_per_1m: 0.25,
			cache_write_price_per_1m: 3.125,
		};

		const draft = syncMatchToDraft(match);
		const result = buildSelectedSyncPrices([draft]);

		expect(draft.cacheWrite).toBe('3.125');
		expect(result.invalidModel).toBeNull();
		expect(result.prices['gpt-5.6-terra']).toMatchObject({
			style: 'openai',
			cacheRead: 0.25,
			cacheWrite: 3.125,
		});
	});

  it('sync fallback saves selected models with single-model callbacks', async () => {
    const calls: Array<{ model: string; price: number }> = [];
    const selectedDrafts = [
      syncDraft('gpt-4o'),
      syncDraft('claude-sonnet'),
    ];
    const prices = {
      'gpt-4o': { ...syncDraftToModelPrice(selectedDrafts[0])!, prompt: 3 },
      'claude-sonnet': { ...syncDraftToModelPrice(selectedDrafts[1])!, prompt: 4 },
      'gpt-4o-mini': { ...syncDraftToModelPrice(syncDraft('gpt-4o-mini'))!, prompt: 5 },
    };

    const result = await saveSyncDraftsWithSingleModelCallback(
      selectedDrafts,
      prices,
      async (model, price) => {
        calls.push({ model, price: price.prompt });
      },
    );

    expect(calls).toEqual([
      { model: 'gpt-4o', price: 3 },
      { model: 'claude-sonnet', price: 4 },
    ]);
    expect(result).toEqual({ successModels: ['gpt-4o', 'claude-sonnet'], failures: [] });
  });
});

describe('buildPricingModelOptions', () => {
  it('keeps configured models visible but disabled', () => {
    const options = buildPricingModelOptions(
      ['priced-zeta', 'unpriced-beta', 'priced-alpha', 'unpriced-alpha'],
      {
        'priced-zeta': { style: 'openai', prompt: 3, completion: 15, cacheRead: 0.3, cacheWrite: 0, multiplier: 1 },
        'priced-alpha': { style: 'openai', prompt: 2, completion: 8, cacheRead: 0.2, cacheWrite: 0, multiplier: 1 },
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
