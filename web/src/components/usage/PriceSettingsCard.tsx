import { useState, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Select, type SelectOption } from '@/components/ui/Select';
import { IconCheck, IconCircleAlert, IconRefreshCw } from '@/components/ui/icons';
import type { ModelPrice, PricingSaveResult, PricingStyle, PricingSyncMatch, PricingSyncPreviewResponse } from '@/lib/types';
import styles from '@/pages/UsagePage.module.scss';

const formatDisplayName = (value: string): string => {
  const normalized = value.trim();
  if (!normalized) return '-';
  return normalized;
};

export interface PriceSettingsCardProps {
  modelNames: string[];
  modelPrices: Record<string, ModelPrice>;
  onPriceSave: (model: string, price: ModelPrice) => void | Promise<void>;
  onPriceDelete: (model: string) => void | Promise<void>;
  onSyncPricesChange?: (prices: Record<string, ModelPrice>) => Promise<PricingSaveResult>;
  onSyncPreview?: () => Promise<PricingSyncPreviewResponse>;
  onNotice?: (kind: 'success' | 'info' | 'error', message: string) => void;
  loading?: boolean;
}

export interface PricingSyncDraft {
  model: string;
  matchedModel: string;
  matchType: string;
  sourceProviderId: string;
  sourceProviderName: string;
  selected: boolean;
  style: PricingStyle;
  prompt: string;
  completion: string;
  cache: string;
  cacheCreation: string;
  multiplier: string;
  saveStatus?: 'failed';
  saveError?: string;
}

export interface PricingDraftInput {
  style: PricingStyle;
  prompt: string;
  completion: string;
  cache: string;
  cacheCreation: string;
  multiplier: string;
}

function PriceSettingsTitle({ title, subtitle }: { title: string; subtitle: string }) {
  return (
    <div className={styles.sectionTitleBlock}>
      <h3 className={styles.sectionTitle}>{title}</h3>
      <p className={styles.sectionSubtitle}>{subtitle}</p>
    </div>
  );
}

const parsePriceValue = (value: string): number | null => {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null;
};

const parseMultiplierValue = (value: string): number | null => {
  if (value.trim() === '') return 1;
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null;
};

const parseCachePriceValue = (value: string, style: PricingStyle, prompt: number): number | null => {
  if (value.trim() !== '') return parsePriceValue(value);
  return style === 'openai' ? prompt : 0;
};

const parseCacheCreationPriceValue = (value: string, style: PricingStyle): number | null => {
  if (style !== 'claude') return 0;
  return value.trim() === '' ? 0 : parsePriceValue(value);
};

const priceToInputValue = (value: number | undefined): string => (
  typeof value === 'number' && Number.isFinite(value) ? value.toString() : ''
);

const normalizePricingStyle = (style: PricingStyle | string | undefined): PricingStyle => (
  style === 'claude' ? 'claude' : 'openai'
);

export const syncMatchToDraft = (match: PricingSyncMatch, existingPrice?: ModelPrice): PricingSyncDraft => ({
  model: match.model,
  matchedModel: match.matched_model,
  matchType: match.match_type,
  sourceProviderId: match.source_provider_id,
  sourceProviderName: match.source_provider_name,
  selected: true,
  style: normalizePricingStyle(match.pricing_style),
  prompt: priceToInputValue(match.prompt_price_per_1m),
  completion: priceToInputValue(match.completion_price_per_1m),
  cache: priceToInputValue(match.cache_price_per_1m),
  cacheCreation: priceToInputValue(match.cache_creation_price_per_1m),
  multiplier: priceToInputValue(existingPrice?.multiplier ?? 1),
});

export const pricingDraftToModelPrice = (draft: PricingDraftInput): ModelPrice | null => {
  const prompt = parsePriceValue(draft.prompt);
  const completion = parsePriceValue(draft.completion);
  if (prompt === null || completion === null) return null;
  const cache = parseCachePriceValue(draft.cache, draft.style, prompt);
  const cacheCreation = parseCacheCreationPriceValue(draft.cacheCreation, draft.style);
  const multiplier = parseMultiplierValue(draft.multiplier);
  if (cache === null || cacheCreation === null || multiplier === null) return null;
  return {
    style: draft.style,
    prompt,
    completion,
    cache,
    cacheCreation,
    multiplier,
  };
};

export const syncDraftToModelPrice = (draft: PricingSyncDraft): ModelPrice | null => (
  pricingDraftToModelPrice(draft)
);

export const markPricingSyncFailures = (
  drafts: PricingSyncDraft[],
  result: PricingSaveResult,
): PricingSyncDraft[] => {
  const failedByModel = new Map(result.failures.map((failure) => [failure.model, failure.message]));
  const successModels = new Set(result.successModels);
  return drafts.map((draft) => {
    const failureMessage = failedByModel.get(draft.model);
    if (failureMessage !== undefined) {
      return {
        ...draft,
        selected: true,
        saveStatus: 'failed',
        saveError: failureMessage,
      };
    }
    if (successModels.has(draft.model)) {
      return {
        ...draft,
        selected: false,
        saveStatus: undefined,
        saveError: undefined,
      };
    }
    return {
      ...draft,
      saveStatus: undefined,
      saveError: undefined,
    };
  });
};

export const notifyPricingSyncUnexpectedError = (
  error: unknown,
  t: (key: string) => string,
  onNotice: PriceSettingsCardProps['onNotice'],
) => {
  const message = error instanceof Error ? error.message : '';
  onNotice?.(
    'error',
    `${t('usage_stats.model_price_sync_failed')}${message ? `: ${message}` : ''}`,
  );
};

export interface SelectedSyncPrices {
  selectedDrafts: PricingSyncDraft[];
  prices: Record<string, ModelPrice>;
  invalidModel: string | null;
}

export const buildSelectedSyncPrices = (drafts: PricingSyncDraft[]): SelectedSyncPrices => {
  const selectedDrafts = drafts.filter((draft) => draft.selected);
  const prices: Record<string, ModelPrice> = {};
  for (const draft of selectedDrafts) {
    const price = syncDraftToModelPrice(draft);
    if (!price) {
      return { selectedDrafts, prices: {}, invalidModel: draft.model };
    }
    prices[draft.model] = price;
  }
  return { selectedDrafts, prices, invalidModel: null };
};

export const saveSyncDraftsWithSingleModelCallback = async (
  selectedDrafts: PricingSyncDraft[],
  prices: Record<string, ModelPrice>,
  onPriceSave: PriceSettingsCardProps['onPriceSave'],
): Promise<PricingSaveResult> => {
  const settled = await Promise.all(selectedDrafts.map(async (draft) => {
    try {
      await Promise.resolve(onPriceSave(draft.model, prices[draft.model]));
      return { model: draft.model, ok: true as const };
    } catch (error) {
      return {
        model: draft.model,
        ok: false as const,
        message: error instanceof Error ? error.message : String(error),
        error,
      };
    }
  }));

  return settled.reduce<PricingSaveResult>((result, item) => {
    if (item.ok) {
      result.successModels.push(item.model);
    } else {
      result.failures.push({ model: item.model, message: item.message, error: item.error });
    }
    return result;
  }, { successModels: [], failures: [] });
};

const notifyPricingPersistenceError = (
  error: unknown,
  fallbackMessage: string,
  onNotice: PriceSettingsCardProps['onNotice'],
) => {
  const message = error instanceof Error ? error.message : '';
  onNotice?.('error', `${fallbackMessage}${message ? `: ${message}` : ''}`);
};

const pricingStyleOptions = (t: (key: string) => string): SelectOption[] => [
  { value: 'openai', label: t('usage_stats.model_price_style_openai') },
  { value: 'claude', label: t('usage_stats.model_price_style_claude') },
];

export const buildPricingModelOptions = (
  modelNames: string[],
  modelPrices: Record<string, ModelPrice>,
  placeholder: string,
  configuredLabel = 'Configured',
): SelectOption[] => {
  const configuredModels = new Set(Object.keys(modelPrices));
  const sortedModelNames = [...modelNames]
    .sort((left, right) => formatDisplayName(left).localeCompare(formatDisplayName(right)));

  return [
    { value: '', label: placeholder },
    ...sortedModelNames.map((name) => {
      const configured = configuredModels.has(name);
      return {
        value: name,
        label: formatDisplayName(name),
        disabled: configured || undefined,
        suffix: configured ? <IconCheck size={12} /> : undefined,
        suffixAriaLabel: configured ? configuredLabel : undefined,
      };
    }),
  ];
};

export function PriceSettingsCard({
  modelNames,
  modelPrices,
  onPriceSave,
  onPriceDelete,
  onSyncPricesChange,
  onSyncPreview,
  onNotice,
  loading = false
}: PriceSettingsCardProps) {
  const { t } = useTranslation();

  // 新增价格表单先暂存输入值，保存成功后再合并当前模型的价格。
  const [selectedModel, setSelectedModel] = useState('');
  const [pricingStyle, setPricingStyle] = useState<PricingStyle>('openai');
  const [promptPrice, setPromptPrice] = useState('');
  const [completionPrice, setCompletionPrice] = useState('');
  const [cachePrice, setCachePrice] = useState('');
  const [cacheCreationPrice, setCacheCreationPrice] = useState('');
  const [priceMultiplier, setPriceMultiplier] = useState('1');
  const [priceSaving, setPriceSaving] = useState(false);

  // 编辑弹窗独立保存草稿值，避免用户取消时污染已保存价格。
  const [editModel, setEditModel] = useState<string | null>(null);
  const [editStyle, setEditStyle] = useState<PricingStyle>('openai');
  const [editPrompt, setEditPrompt] = useState('');
  const [editCompletion, setEditCompletion] = useState('');
  const [editCache, setEditCache] = useState('');
  const [editCacheCreation, setEditCacheCreation] = useState('');
  const [editMultiplier, setEditMultiplier] = useState('1');
  const [editSaving, setEditSaving] = useState(false);
  const [deleteModel, setDeleteModel] = useState<string | null>(null);
  const [deleteSaving, setDeleteSaving] = useState(false);

  const [syncOpen, setSyncOpen] = useState(false);
  const [syncLoading, setSyncLoading] = useState(false);
  const [syncApplying, setSyncApplying] = useState(false);
  const [syncPreview, setSyncPreview] = useState<PricingSyncPreviewResponse | null>(null);
  const [syncDrafts, setSyncDrafts] = useState<PricingSyncDraft[]>([]);

  const closeEditModal = () => {
    if (!editSaving) {
      setEditModel(null);
    }
  };

  const closeDeleteModal = () => {
    if (!deleteSaving) {
      setDeleteModel(null);
    }
  };

  const handleSavePrice = async () => {
    if (!selectedModel || priceSaving) return;
    const price = pricingDraftToModelPrice({
      style: pricingStyle,
      prompt: promptPrice,
      completion: completionPrice,
      cache: cachePrice,
      cacheCreation: cacheCreationPrice,
      multiplier: priceMultiplier,
    });
    if (!price) {
      onNotice?.('error', t('usage_stats.model_price_save_failed'));
      return;
    }
    setPriceSaving(true);
    try {
      await Promise.resolve(onPriceSave(selectedModel, price));
      onNotice?.('success', t('usage_stats.model_price_save_success'));
      setSelectedModel('');
      setPricingStyle('openai');
      setPromptPrice('');
      setCompletionPrice('');
      setCachePrice('');
      setCacheCreationPrice('');
      setPriceMultiplier('1');
    } catch (error) {
      notifyPricingPersistenceError(error, t('usage_stats.model_price_save_failed'), onNotice);
    } finally {
      setPriceSaving(false);
    }
  };

  const confirmDeleteModel = async () => {
    if (!deleteModel || deleteSaving) return;
    setDeleteSaving(true);
    try {
      await Promise.resolve(onPriceDelete(deleteModel));
      onNotice?.('success', t('usage_stats.model_price_delete_success'));
      setDeleteModel(null);
    } catch (error) {
      notifyPricingPersistenceError(error, t('usage_stats.model_price_delete_failed'), onNotice);
    } finally {
      setDeleteSaving(false);
    }
  };

  const handleOpenEdit = (model: string) => {
    const price = modelPrices[model];
    setEditModel(model);
    setEditStyle(price?.style ?? 'openai');
    setEditPrompt(price?.prompt?.toString() || '');
    setEditCompletion(price?.completion?.toString() || '');
    setEditCache(price?.cache?.toString() || '');
    setEditCacheCreation(price?.cacheCreation?.toString() || '');
    setEditMultiplier(priceToInputValue(price?.multiplier ?? 1));
  };

  const handleSaveEdit = async () => {
    if (!editModel || editSaving) return;
    const price = pricingDraftToModelPrice({
      style: editStyle,
      prompt: editPrompt,
      completion: editCompletion,
      cache: editCache,
      cacheCreation: editCacheCreation,
      multiplier: editMultiplier,
    });
    if (!price) {
      onNotice?.('error', t('usage_stats.model_price_edit_failed'));
      return;
    }
    setEditSaving(true);
    try {
      await Promise.resolve(onPriceSave(editModel, price));
      onNotice?.('success', t('usage_stats.model_price_edit_success'));
      setEditModel(null);
    } catch (error) {
      notifyPricingPersistenceError(error, t('usage_stats.model_price_edit_failed'), onNotice);
    } finally {
      setEditSaving(false);
    }
  };

  const handleModelSelect = (value: string) => {
    if (priceSaving) return;
    setSelectedModel(value);
    const price = modelPrices[value];
    if (price) {
      setPricingStyle(price.style);
      setPromptPrice(price.prompt.toString());
      setCompletionPrice(price.completion.toString());
      setCachePrice(price.cache.toString());
      setCacheCreationPrice(price.cacheCreation.toString());
      setPriceMultiplier(priceToInputValue(price.multiplier ?? 1));
    } else {
      setPricingStyle('openai');
      setPromptPrice('');
      setCompletionPrice('');
      setCachePrice('');
      setCacheCreationPrice('');
      setPriceMultiplier('1');
    }
  };

  const handleOpenSyncPreview = async () => {
    if (!onSyncPreview || syncLoading) return;
    setSyncLoading(true);
    try {
      const preview = await onSyncPreview();
      const drafts = (preview.matches ?? []).map((match) => syncMatchToDraft(match, modelPrices[match.model]));
      setSyncPreview({
        ...preview,
        matches: preview.matches ?? [],
        unmatched_models: preview.unmatched_models ?? [],
      });
      setSyncDrafts(drafts);
      setSyncOpen(true);
      if (drafts.length === 0) {
        onNotice?.('info', t('usage_stats.model_price_sync_no_matches'));
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : '';
      onNotice?.('error', `${t('usage_stats.model_price_sync_failed')}${message ? `: ${message}` : ''}`);
    } finally {
      setSyncLoading(false);
    }
  };

  const handleUpdateSyncDraft = (index: number, patch: Partial<PricingSyncDraft>) => {
    const clearsFailure = Object.keys(patch).some((key) => key !== 'selected');
    setSyncDrafts((current) => current.map((draft, draftIndex) => (
      draftIndex === index
        ? {
          ...draft,
          ...patch,
          ...(clearsFailure ? { saveStatus: undefined, saveError: undefined } : {}),
        }
        : draft
    )));
  };

  const handleSetAllSyncDrafts = (selected: boolean) => {
    setSyncDrafts((current) => current.map((draft) => ({ ...draft, selected })));
  };

  const handleApplySyncDrafts = async () => {
    const { selectedDrafts, prices: syncPrices, invalidModel } = buildSelectedSyncPrices(syncDrafts);
    if (selectedDrafts.length === 0) {
      onNotice?.('error', t('usage_stats.model_price_sync_none_selected'));
      return;
    }
    if (invalidModel !== null) {
      onNotice?.('error', t('usage_stats.model_price_sync_invalid', { model: formatDisplayName(invalidModel) }));
      return;
    }

    setSyncApplying(true);
    try {
      if (!onSyncPricesChange) {
        const result = await saveSyncDraftsWithSingleModelCallback(selectedDrafts, syncPrices, onPriceSave);
        setSyncDrafts((current) => markPricingSyncFailures(current, result));
        if (result.failures.length === 0) {
          onNotice?.('success', t('usage_stats.model_price_sync_apply_success', { count: result.successModels.length }));
          setSyncOpen(false);
          return;
        }

        onNotice?.(
          result.successModels.length > 0 ? 'info' : 'error',
          t('usage_stats.model_price_sync_apply_partial', {
            success: result.successModels.length,
            failed: result.failures.length,
          }),
        );
        return;
      }

      const result = await onSyncPricesChange(syncPrices);
      setSyncDrafts((current) => markPricingSyncFailures(current, result));
      if (result.failures.length === 0) {
        onNotice?.('success', t('usage_stats.model_price_sync_apply_success', { count: result.successModels.length }));
        setSyncOpen(false);
        return;
      }

      onNotice?.(
        result.successModels.length > 0 ? 'info' : 'error',
        t('usage_stats.model_price_sync_apply_partial', {
          success: result.successModels.length,
          failed: result.failures.length,
        }),
      );
    } catch (error) {
      notifyPricingSyncUnexpectedError(error, t, onNotice);
    } finally {
      setSyncApplying(false);
    }
  };

  const options = useMemo(
    () => buildPricingModelOptions(
      modelNames,
      modelPrices,
      t('usage_stats.model_price_select_placeholder'),
      t('usage_stats.model_price_configured'),
    ),
    [modelNames, modelPrices, t]
  );
  const styleOptions = useMemo(() => pricingStyleOptions(t), [t]);
  const selectedSyncCount = useMemo(
    () => syncDrafts.filter((draft) => draft.selected).length,
    [syncDrafts]
  );

  return (
    <>
      <Card
        title={
          <PriceSettingsTitle
            title={t('usage_stats.model_price_settings_title')}
            subtitle={t('usage_stats.model_price_settings_subtitle')}
          />
        }
        className={`${styles.detailsFixedCard} ${styles.pricingFixedCard}`}
      >
        <div className={styles.pricingSection}>
          {loading && modelNames.length === 0 && Object.keys(modelPrices).length === 0 ? (
            <div className={styles.hint}>{t('common.loading')}</div>
          ) : (
            <>
              {onSyncPreview && (
                <div className={styles.pricingToolbar}>
                  <div className={styles.pricingToolbarMeta}>
                    <span>{t('usage_stats.model_price_sync_source')}: Models.dev</span>
                  </div>
                  <Button
                    variant="secondary"
                    className={styles.usagePillAction}
                    onClick={() => void handleOpenSyncPreview()}
                    loading={syncLoading}
                  >
                    <IconRefreshCw size={14} />
                    {t('usage_stats.model_price_sync')}
                  </Button>
                </div>
              )}
              <div className={styles.priceForm}>
                <div className={styles.formRow}>
                  <div className={styles.formField}>
                    <label>{t('usage_stats.model_name')}</label>
                    <Select
                      value={selectedModel}
                      options={options}
                      onChange={handleModelSelect}
                      placeholder={t('usage_stats.model_price_select_placeholder')}
                      disabled={priceSaving}
                      className={styles.usagePillControl}
                    />
                  </div>
                  <div className={styles.formField}>
                    <label>{t('usage_stats.model_price_style')}</label>
                    <Select
                      value={pricingStyle}
                      options={styleOptions}
                      onChange={(value) => setPricingStyle(value === 'claude' ? 'claude' : 'openai')}
                      disabled={priceSaving}
                      className={styles.usagePillControl}
                    />
                  </div>
                  <div className={styles.formField}>
                    <label>{t('usage_stats.model_price_prompt')} ($/1M)</label>
                    <Input
                      type="number"
                      value={promptPrice}
                      onChange={(e) => setPromptPrice(e.target.value)}
                      placeholder="0.00"
                      step="0.0001"
                      disabled={priceSaving}
                      className={styles.usagePillControl}
                    />
                  </div>
                  <div className={styles.formField}>
                    <label>{t('usage_stats.model_price_completion')} ($/1M)</label>
                    <Input
                      type="number"
                      value={completionPrice}
                      onChange={(e) => setCompletionPrice(e.target.value)}
                      placeholder="0.00"
                      step="0.0001"
                      disabled={priceSaving}
                      className={styles.usagePillControl}
                    />
                  </div>
                  <div className={styles.formField}>
                    <label>{t(pricingStyle === 'claude' ? 'usage_stats.model_price_cache_read' : 'usage_stats.model_price_cache')} ($/1M)</label>
                    <Input
                      type="number"
                      value={cachePrice}
                      onChange={(e) => setCachePrice(e.target.value)}
                      placeholder="0.00"
                      step="0.0001"
                      disabled={priceSaving}
                      className={styles.usagePillControl}
                    />
                  </div>
                  {pricingStyle === 'claude' && (
                    <div className={styles.formField}>
                      <label>{t('usage_stats.model_price_cache_write')} ($/1M)</label>
                      <Input
                        type="number"
                        value={cacheCreationPrice}
                        onChange={(e) => setCacheCreationPrice(e.target.value)}
                        placeholder="0.00"
                        step="0.0001"
                        disabled={priceSaving}
                        className={styles.usagePillControl}
                      />
                    </div>
                  )}
                  <div className={styles.formField}>
                    <label>{t('usage_stats.model_price_multiplier')}</label>
                    <Input
                      type="number"
                      value={priceMultiplier}
                      onChange={(e) => setPriceMultiplier(e.target.value)}
                      placeholder="1"
                      step="0.0001"
                      min="0"
                      disabled={priceSaving}
                      className={styles.usagePillControl}
                    />
                  </div>
                  <Button variant="primary" className={styles.usagePillAction} onClick={() => void handleSavePrice()} disabled={!selectedModel || priceSaving} loading={priceSaving}>
                    {t('common.save')}
                  </Button>
                </div>
              </div>

              <div className={styles.pricesList}>
                <h4 className={styles.pricesTitle}>{t('usage_stats.saved_prices')}</h4>
                {Object.keys(modelPrices).length > 0 ? (
                  <div className={styles.pricesGrid}>
                    {Object.entries(modelPrices).map(([model, price]) => (
                      <div key={model} className={styles.priceItem}>
                        <div className={styles.priceInfo}>
                          <span className={styles.priceModel}>{formatDisplayName(model)}</span>
                          <div className={styles.priceMeta}>
                            <span>
                              {t('usage_stats.model_price_style')}: {t(price.style === 'claude' ? 'usage_stats.model_price_style_claude' : 'usage_stats.model_price_style_openai')}
                            </span>
                            <span>
                              {t('usage_stats.model_price_prompt')}: ${price.prompt.toFixed(4)}/1M
                            </span>
                            <span>
                              {t('usage_stats.model_price_completion')}: ${price.completion.toFixed(4)}/1M
                            </span>
                            <span>
                              {t(price.style === 'claude' ? 'usage_stats.model_price_cache_read' : 'usage_stats.model_price_cache')}: ${price.cache.toFixed(4)}/1M
                            </span>
                            {price.style === 'claude' && (
                              <span>
                                {t('usage_stats.model_price_cache_write')}: ${price.cacheCreation.toFixed(4)}/1M
                              </span>
                            )}
                            <span>
                              {t('usage_stats.model_price_multiplier')}: {priceToInputValue(price.multiplier ?? 1)}
                            </span>
                          </div>
                        </div>
                        <div className={styles.priceActions}>
                          <Button variant="secondary" size="sm" className={styles.usagePillAction} onClick={() => handleOpenEdit(model)}>
                            {t('common.edit')}
                          </Button>
                          <Button variant="danger" size="sm" className={`${styles.usagePillAction} ${styles.usagePillActionDanger}`} onClick={() => setDeleteModel(model)}>
                            {t('common.delete')}
                          </Button>
                        </div>
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className={styles.hint}>{t('usage_stats.model_price_empty')}</div>
                )}
              </div>
            </>
          )}
        </div>
      </Card>

      {/* 编辑弹窗不作为价格卡片内容参与布局，只负责编辑当前模型价格。 */}
      <Modal
        open={editModel !== null}
        title={formatDisplayName(editModel ?? '')}
        onClose={closeEditModal}
        closeDisabled={editSaving}
        footer={
          <div className={styles.priceActions}>
            <Button variant="secondary" className={styles.usagePillAction} onClick={closeEditModal} disabled={editSaving}>
              {t('common.cancel')}
            </Button>
            <Button variant="primary" className={styles.usagePillAction} onClick={() => void handleSaveEdit()} loading={editSaving}>
              {t('common.save')}
            </Button>
          </div>
        }
        width={420}
      >
        <div className={styles.editModalBody}>
          <div className={styles.formField}>
            <label>{t('usage_stats.model_price_style')}</label>
            <Select
              value={editStyle}
              options={styleOptions}
              onChange={(value) => setEditStyle(value === 'claude' ? 'claude' : 'openai')}
              disabled={editSaving}
              className={styles.usagePillControl}
            />
          </div>
          <div className={styles.formField}>
            <label>{t('usage_stats.model_price_prompt')} ($/1M)</label>
            <Input
              type="number"
              value={editPrompt}
              onChange={(e) => setEditPrompt(e.target.value)}
              placeholder="0.00"
              step="0.0001"
              disabled={editSaving}
              className={styles.usagePillControl}
            />
          </div>
          <div className={styles.formField}>
            <label>{t('usage_stats.model_price_completion')} ($/1M)</label>
            <Input
              type="number"
              value={editCompletion}
              onChange={(e) => setEditCompletion(e.target.value)}
              placeholder="0.00"
              step="0.0001"
              disabled={editSaving}
              className={styles.usagePillControl}
            />
          </div>
          <div className={styles.formField}>
            <label>{t(editStyle === 'claude' ? 'usage_stats.model_price_cache_read' : 'usage_stats.model_price_cache')} ($/1M)</label>
            <Input
              type="number"
              value={editCache}
              onChange={(e) => setEditCache(e.target.value)}
              placeholder="0.00"
              step="0.0001"
              disabled={editSaving}
              className={styles.usagePillControl}
            />
          </div>
          {editStyle === 'claude' && (
            <div className={styles.formField}>
              <label>{t('usage_stats.model_price_cache_write')} ($/1M)</label>
              <Input
                type="number"
                value={editCacheCreation}
                onChange={(e) => setEditCacheCreation(e.target.value)}
                placeholder="0.00"
                step="0.0001"
                disabled={editSaving}
                className={styles.usagePillControl}
              />
            </div>
          )}
          <div className={styles.formField}>
            <label>{t('usage_stats.model_price_multiplier')}</label>
            <Input
              type="number"
              value={editMultiplier}
              onChange={(e) => setEditMultiplier(e.target.value)}
              placeholder="1"
              step="0.0001"
              min="0"
              disabled={editSaving}
              className={styles.usagePillControl}
            />
          </div>
        </div>
      </Modal>

      <Modal
        open={deleteModel !== null}
        title={t('usage_stats.model_price_delete_confirm_title')}
        onClose={closeDeleteModal}
        closeDisabled={deleteSaving}
        footer={
          <div className={styles.priceActions}>
            <Button variant="secondary" className={styles.usagePillAction} onClick={closeDeleteModal} disabled={deleteSaving}>
              {t('common.cancel')}
            </Button>
            <Button variant="danger" className={`${styles.usagePillAction} ${styles.usagePillActionDanger}`} onClick={() => void confirmDeleteModel()} loading={deleteSaving}>
              {t('usage_stats.model_price_delete_confirm_action')}
            </Button>
          </div>
        }
        width={420}
      >
        <p className={styles.modelPriceDeleteConfirmText}>
          {t('usage_stats.model_price_delete_confirm_body', { model: formatDisplayName(deleteModel ?? '') })}
        </p>
      </Modal>

      <Modal
        open={syncOpen}
        title={t('usage_stats.model_price_sync_title')}
        onClose={() => {
          if (!syncApplying) {
            setSyncOpen(false);
          }
        }}
        closeDisabled={syncApplying}
        footer={
          <div className={styles.priceActions}>
            <Button
              variant="secondary"
              className={styles.usagePillAction}
              onClick={() => setSyncOpen(false)}
              disabled={syncApplying}
            >
              {t('common.cancel')}
            </Button>
            <Button
              variant="primary"
              className={styles.usagePillAction}
              onClick={() => void handleApplySyncDrafts()}
              loading={syncApplying}
              disabled={selectedSyncCount === 0}
            >
              {t('usage_stats.model_price_sync_update_selected', { count: selectedSyncCount })}
            </Button>
          </div>
        }
        width={940}
      >
        <div className={styles.syncModalBody}>
          <div className={styles.syncSummaryRow}>
            <span>
              {t('usage_stats.model_price_sync_source')}: {syncPreview?.source || 'Models.dev'}
            </span>
            <span>
              {t('usage_stats.model_price_sync_matched')}: {syncDrafts.length}
            </span>
            <span>
              {t('usage_stats.model_price_sync_unmatched')}: {syncPreview?.unmatched_models?.length ?? 0}
            </span>
          </div>

          {syncDrafts.length > 0 ? (
            <>
              <div className={styles.syncBatchActions}>
                <Button
                  variant="secondary"
                  size="sm"
                  className={styles.usagePillAction}
                  onClick={() => handleSetAllSyncDrafts(true)}
                  disabled={syncApplying}
                >
                  {t('usage_stats.model_price_sync_select_all')}
                </Button>
                <Button
                  variant="secondary"
                  size="sm"
                  className={styles.usagePillAction}
                  onClick={() => handleSetAllSyncDrafts(false)}
                  disabled={syncApplying}
                >
                  {t('usage_stats.model_price_sync_select_none')}
                </Button>
              </div>

              <div className={styles.syncDraftList}>
                {syncDrafts.map((draft, index) => {
                  const existing = Boolean(modelPrices[draft.model]);
                  const failed = draft.saveStatus === 'failed';
                  const failureLabel = t('usage_stats.model_price_sync_failed_label', { model: formatDisplayName(draft.model) });
                  return (
                    <div
                      key={`${draft.model}-${draft.matchedModel}`}
                      className={`${styles.syncDraftItem} ${failed ? styles.syncDraftItemFailed : ''}`}
                    >
                      <label className={styles.syncDraftCheck}>
                        <input
                          type="checkbox"
                          checked={draft.selected}
                          disabled={syncApplying}
                          onChange={(event) => handleUpdateSyncDraft(index, { selected: event.target.checked })}
                          aria-label={t('usage_stats.model_price_sync_toggle', { model: formatDisplayName(draft.model) })}
                        />
                      </label>
                      <div className={styles.syncDraftContent}>
                        <div className={styles.syncDraftHeader}>
                          <div className={styles.syncDraftModelBlock}>
                            <span className={styles.priceModel}>{formatDisplayName(draft.model)}</span>
                            <span className={styles.syncDraftMatched}>
                              {t('usage_stats.model_price_sync_matched_model', { model: formatDisplayName(draft.matchedModel) })}
                            </span>
                            <span className={styles.syncDraftMatched}>
                              {t('usage_stats.model_price_sync_provider', {
                                provider: formatDisplayName(draft.sourceProviderName || draft.sourceProviderId),
                                id: formatDisplayName(draft.sourceProviderId),
                              })}
                            </span>
                          </div>
                          <div className={styles.syncDraftBadges}>
                            {failed && (
                              <span
                                className={styles.syncDraftFailureIcon}
                                role="img"
                                aria-label={failureLabel}
                                title={draft.saveError || failureLabel}
                              >
                                <IconCircleAlert size={13} />
                              </span>
                            )}
                            <span>{draft.matchType}</span>
                            {existing && <span>{t('usage_stats.model_price_sync_existing')}</span>}
                          </div>
                        </div>
                        <div className={styles.syncDraftGrid}>
                          <div className={styles.formField}>
                            <label>{t('usage_stats.model_price_style')}</label>
                            <Select
                              value={draft.style}
                              options={styleOptions}
                              onChange={(value) => handleUpdateSyncDraft(index, { style: value === 'claude' ? 'claude' : 'openai' })}
                              disabled={syncApplying}
                              className={styles.usagePillControl}
                            />
                          </div>
                          <div className={styles.formField}>
                            <label>{t('usage_stats.model_price_prompt')} ($/1M)</label>
                            <Input
                              type="number"
                              value={draft.prompt}
                              onChange={(event) => handleUpdateSyncDraft(index, { prompt: event.target.value })}
                              placeholder="0.00"
                              step="0.0001"
                              disabled={syncApplying}
                              className={styles.usagePillControl}
                            />
                          </div>
                          <div className={styles.formField}>
                            <label>{t('usage_stats.model_price_completion')} ($/1M)</label>
                            <Input
                              type="number"
                              value={draft.completion}
                              onChange={(event) => handleUpdateSyncDraft(index, { completion: event.target.value })}
                              placeholder="0.00"
                              step="0.0001"
                              disabled={syncApplying}
                              className={styles.usagePillControl}
                            />
                          </div>
                          <div className={styles.formField}>
                            <label>{t(draft.style === 'claude' ? 'usage_stats.model_price_cache_read' : 'usage_stats.model_price_cache')} ($/1M)</label>
                            <Input
                              type="number"
                              value={draft.cache}
                              onChange={(event) => handleUpdateSyncDraft(index, { cache: event.target.value })}
                              placeholder="0.00"
                              step="0.0001"
                              disabled={syncApplying}
                              className={styles.usagePillControl}
                            />
                          </div>
                          {draft.style === 'claude' && (
                            <div className={styles.formField}>
                              <label>{t('usage_stats.model_price_cache_write')} ($/1M)</label>
                              <Input
                                type="number"
                                value={draft.cacheCreation}
                                onChange={(event) => handleUpdateSyncDraft(index, { cacheCreation: event.target.value })}
                                placeholder="0.00"
                                step="0.0001"
                                disabled={syncApplying}
                                className={styles.usagePillControl}
                              />
                            </div>
                          )}
                          <div className={styles.formField}>
                            <label>{t('usage_stats.model_price_multiplier')}</label>
                            <Input
                              type="number"
                              value={draft.multiplier}
                              onChange={(event) => handleUpdateSyncDraft(index, { multiplier: event.target.value })}
                              placeholder="1"
                              step="0.0001"
                              min="0"
                              disabled={syncApplying}
                              className={styles.usagePillControl}
                            />
                          </div>
                        </div>
                      </div>
                    </div>
                  );
                })}
              </div>
            </>
          ) : (
            <div className={styles.hint}>{t('usage_stats.model_price_sync_no_matches')}</div>
          )}

          {(syncPreview?.unmatched_models?.length ?? 0) > 0 && (
            <details className={styles.syncUnmatched}>
              <summary>
                {t('usage_stats.model_price_sync_unmatched')}: {syncPreview?.unmatched_models.length}
              </summary>
              <div className={styles.syncUnmatchedList}>
                {syncPreview?.unmatched_models.map((model) => (
                  <span key={model}>{formatDisplayName(model)}</span>
                ))}
              </div>
            </details>
          )}
        </div>
      </Modal>
    </>
  );
}
