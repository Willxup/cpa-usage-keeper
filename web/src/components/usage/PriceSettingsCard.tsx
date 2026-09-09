import { useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Select, type SelectOption } from '@/components/ui/Select';
import { IconCheck } from '@/components/ui/icons';
import { useScrollBoundaryContainment } from '@/hooks/useScrollBoundaryContainment';
import { compareModelNames } from '@/utils/modelSort';
import type { ModelPrice, PricingRule, PricingSaveResult, PricingStyle, PricingSyncSource, PricingSyncPreviewResponse, ReplacePricingRuleInput } from '@/lib/types';
import { PriceRulesModal } from './pricing/PriceRulesModal';
import { PriceSyncPanel } from './pricing/PriceSyncPanel';
import { formatDisplayName, priceToInputValue, pricingDraftToModelPrice, pricingStyleOptions } from './pricing/pricingDrafts';
import styles from '@/pages/UsagePage.module.scss';

export interface PriceSettingsCardProps {
  modelNames: string[];
  modelPrices: Record<string, ModelPrice>;
  onPriceSave: (model: string, price: ModelPrice) => void | Promise<void>;
  onPriceDelete: (model: string) => void | Promise<void>;
  onRulesLoad?: (model: string) => Promise<PricingRule[] | null>;
  onRulesSave?: (model: string, rules: ReplacePricingRuleInput[]) => Promise<PricingRule[] | null>;
  onSyncPricesChange?: (prices: Record<string, ModelPrice>) => Promise<PricingSaveResult>;
  onSyncPreview?: (source: PricingSyncSource, signal?: AbortSignal) => Promise<PricingSyncPreviewResponse>;
  onNotice?: (kind: 'success' | 'info' | 'error', message: string) => void;
  loading?: boolean;
}

const emptyPricingRules = async (): Promise<PricingRule[]> => [];

const notifyPricingPersistenceError = (
  error: unknown,
  fallbackMessage: string,
  onNotice: PriceSettingsCardProps['onNotice'],
) => {
  const message = error instanceof Error ? error.message : '';
  onNotice?.('error', `${fallbackMessage}${message ? `: ${message}` : ''}`);
};

export const buildPricingModelOptions = (
  modelNames: string[],
  modelPrices: Record<string, ModelPrice>,
  placeholder: string,
  configuredLabel = 'Configured',
): SelectOption[] => {
  const configuredModels = new Set(Object.keys(modelPrices));
  const sortedModelNames = [...modelNames]
    .sort((left, right) => {
      const configuredOrder = Number(configuredModels.has(left)) - Number(configuredModels.has(right));
      return configuredOrder || compareModelNames(left, right);
    });

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
  onRulesLoad,
  onRulesSave,
  onSyncPricesChange,
  onSyncPreview,
  onNotice,
  loading = false
}: PriceSettingsCardProps) {
  const { t } = useTranslation();
  const pricesGridRef = useRef<HTMLDivElement | null>(null);

  // 新增价格表单先暂存输入值，保存成功后再合并当前模型的价格。
  const [selectedModel, setSelectedModel] = useState('');
  const [pricingStyle, setPricingStyle] = useState<PricingStyle>('openai');
  const [promptPrice, setPromptPrice] = useState('');
  const [completionPrice, setCompletionPrice] = useState('');
  const [cacheReadPrice, setCacheReadPrice] = useState('');
  const [cacheWritePrice, setCacheWritePrice] = useState('');
  const [priceMultiplier, setPriceMultiplier] = useState('1');
  const [priceSaving, setPriceSaving] = useState(false);

  // 编辑弹窗独立保存草稿值，避免用户取消时污染已保存价格。
  const [editModel, setEditModel] = useState<string | null>(null);
  const [editStyle, setEditStyle] = useState<PricingStyle>('openai');
  const [editPrompt, setEditPrompt] = useState('');
  const [editCompletion, setEditCompletion] = useState('');
  const [editCacheRead, setEditCacheRead] = useState('');
  const [editCacheWrite, setEditCacheWrite] = useState('');
  const [editMultiplier, setEditMultiplier] = useState('1');
  const [editSaving, setEditSaving] = useState(false);
  const [deleteModel, setDeleteModel] = useState<string | null>(null);
  const [deleteSaving, setDeleteSaving] = useState(false);
  const [rulesModel, setRulesModel] = useState<string | null>(null);

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
      cacheRead: cacheReadPrice,
      cacheWrite: cacheWritePrice,
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
      setCacheReadPrice('');
      setCacheWritePrice('');
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
    setEditCacheRead(price?.cacheRead?.toString() || '');
    setEditCacheWrite(price?.cacheWrite?.toString() || '');
    setEditMultiplier(priceToInputValue(price?.multiplier ?? 1));
  };

  const handleSaveEdit = async () => {
    if (!editModel || editSaving) return;
    const price = pricingDraftToModelPrice({
      style: editStyle,
      prompt: editPrompt,
      completion: editCompletion,
      cacheRead: editCacheRead,
      cacheWrite: editCacheWrite,
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
      setCacheReadPrice(price.cacheRead.toString());
      setCacheWritePrice(price.cacheWrite.toString());
      setPriceMultiplier(priceToInputValue(price.multiplier ?? 1));
    } else {
      setPricingStyle('openai');
      setPromptPrice('');
      setCompletionPrice('');
      setCacheReadPrice('');
      setCacheWritePrice('');
      setPriceMultiplier('1');
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
  const sortedModelPrices = useMemo(
    () => Object.entries(modelPrices)
      .sort(([left], [right]) => compareModelNames(left, right)),
    [modelPrices]
  );
  useScrollBoundaryContainment(pricesGridRef, sortedModelPrices.length > 0);
  return (
    <>
      <Card
        title={t('usage_stats.model_price_settings_title')}
        subtitle={t('usage_stats.model_price_settings_subtitle')}
        className={`${styles.detailsFixedCard} ${styles.pricingFixedCard}`}
      >
        <div className={styles.pricingSection}>
          {loading && modelNames.length === 0 && Object.keys(modelPrices).length === 0 ? (
            <div className={styles.hint}>{t('common.loading')}</div>
          ) : (
            <>
              {onSyncPreview && onSyncPricesChange && (
                <PriceSyncPanel modelPrices={modelPrices} onSyncPreview={onSyncPreview}
                  onSyncPricesChange={onSyncPricesChange} onNotice={onNotice} />
              )}
              <div className={styles.priceForm}>
                <div className={styles.formRow}>
                  <div className={`${styles.formField} ${styles.priceFormModelField}`}>
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
                    <label>{t('usage_stats.model_price_cache_read')} ($/1M)</label>
                    <Input
                      type="number"
                      value={cacheReadPrice}
                      onChange={(e) => setCacheReadPrice(e.target.value)}
                      placeholder="0.00"
                      step="0.0001"
                      disabled={priceSaving}
                      className={styles.usagePillControl}
                    />
                  </div>
                  <div className={styles.formField}>
                    <label>{t('usage_stats.model_price_cache_write')} ($/1M)</label>
                    <Input
                      type="number"
                      value={cacheWritePrice}
                      onChange={(e) => setCacheWritePrice(e.target.value)}
                      placeholder="0.00"
                      step="0.0001"
                      disabled={priceSaving}
                      className={styles.usagePillControl}
                    />
                  </div>
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
                  <Button variant="primary" appearance="action" className={styles.priceFormAction} onClick={() => void handleSavePrice()} disabled={!selectedModel || priceSaving} loading={priceSaving}>
                    {t('common.save')}
                  </Button>
                </div>
              </div>

              <div className={styles.pricesList}>
                <h4 className={styles.pricesTitle}>{t('usage_stats.saved_prices')}</h4>
                {sortedModelPrices.length > 0 ? (
                  <div ref={pricesGridRef} className={styles.pricesGrid}>
                    {sortedModelPrices.map(([model, price]) => (
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
                              {t('usage_stats.model_price_cache_read')}: ${price.cacheRead.toFixed(4)}/1M
                            </span>
                            <span>
                              {t('usage_stats.model_price_cache_write')}: ${price.cacheWrite.toFixed(4)}/1M
                            </span>
                            <span>
                              {t('usage_stats.model_price_multiplier')}: {priceToInputValue(price.multiplier ?? 1)}
                            </span>
                          </div>
                        </div>
                        <div className={styles.priceActions}>
                          <Button variant="secondary" size="sm" appearance="action" onClick={() => setRulesModel(model)}>
                            {t('usage_stats.model_price_rules')}
                          </Button>
                          <Button variant="secondary" size="sm" appearance="action" onClick={() => handleOpenEdit(model)}>
                            {t('common.edit')}
                          </Button>
                          <Button variant="danger" size="sm" appearance="action" onClick={() => setDeleteModel(model)}>
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

      <PriceRulesModal
        open={rulesModel !== null}
        model={rulesModel ?? ''}
        onClose={() => setRulesModel(null)}
        loadRules={onRulesLoad ?? emptyPricingRules}
        saveRules={onRulesSave ?? emptyPricingRules}
        onNotice={onNotice}
      />

      {/* 编辑弹窗不作为价格卡片内容参与布局，只负责编辑当前模型价格。 */}
      <Modal
        open={editModel !== null}
        title={formatDisplayName(editModel ?? '')}
        onClose={closeEditModal}
        closeDisabled={editSaving}
        footer={
          <div className={styles.priceActions}>
            <Button variant="secondary" appearance="action" onClick={closeEditModal} disabled={editSaving}>
              {t('common.cancel')}
            </Button>
            <Button variant="primary" appearance="action" onClick={() => void handleSaveEdit()} loading={editSaving}>
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
            <label>{t('usage_stats.model_price_cache_read')} ($/1M)</label>
            <Input
              type="number"
              value={editCacheRead}
              onChange={(e) => setEditCacheRead(e.target.value)}
              placeholder="0.00"
              step="0.0001"
              disabled={editSaving}
              className={styles.usagePillControl}
            />
          </div>
          <div className={styles.formField}>
            <label>{t('usage_stats.model_price_cache_write')} ($/1M)</label>
            <Input
              type="number"
              value={editCacheWrite}
              onChange={(e) => setEditCacheWrite(e.target.value)}
              placeholder="0.00"
              step="0.0001"
              disabled={editSaving}
              className={styles.usagePillControl}
            />
          </div>
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
            <Button variant="secondary" appearance="action" onClick={closeDeleteModal} disabled={deleteSaving}>
              {t('common.cancel')}
            </Button>
            <Button variant="danger" appearance="action" onClick={() => void confirmDeleteModel()} loading={deleteSaving}>
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

    </>
  );
}
