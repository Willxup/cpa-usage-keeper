import { useState, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Select, type SelectOption } from '@/components/ui/Select';
import { IconCheck } from '@/components/ui/icons';
import type { ModelPrice, PricingStyle } from '@/lib/types';
import styles from '@/pages/UsagePage.module.scss';

const formatDisplayName = (value: string): string => {
  const normalized = value.trim();
  if (!normalized) return '-';
  return normalized;
};

export interface PriceSettingsCardProps {
  modelNames: string[];
  modelPrices: Record<string, ModelPrice>;
  onPricesChange: (prices: Record<string, ModelPrice>) => void;
  onNotice?: (kind: 'success' | 'info' | 'error', message: string) => void;
  loading?: boolean;
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

const parseCachePriceValue = (value: string, style: PricingStyle, prompt: number): number | null => {
  if (value.trim() !== '') return parsePriceValue(value);
  return style === 'openai' ? prompt : 0;
};

const parseCacheCreationPriceValue = (value: string, style: PricingStyle): number | null => {
  if (style !== 'claude') return 0;
  return value.trim() === '' ? 0 : parsePriceValue(value);
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
  onPricesChange,
  onNotice,
  loading = false
}: PriceSettingsCardProps) {
  const { t } = useTranslation();

  // 新增价格表单先暂存输入值，保存成功后再一次性同步到父级配置。
  const [selectedModel, setSelectedModel] = useState('');
  const [pricingStyle, setPricingStyle] = useState<PricingStyle>('openai');
  const [promptPrice, setPromptPrice] = useState('');
  const [completionPrice, setCompletionPrice] = useState('');
  const [cachePrice, setCachePrice] = useState('');
  const [cacheCreationPrice, setCacheCreationPrice] = useState('');

  // 编辑弹窗独立保存草稿值，避免用户取消时污染已保存价格。
  const [editModel, setEditModel] = useState<string | null>(null);
  const [editStyle, setEditStyle] = useState<PricingStyle>('openai');
  const [editPrompt, setEditPrompt] = useState('');
  const [editCompletion, setEditCompletion] = useState('');
  const [editCache, setEditCache] = useState('');
  const [editCacheCreation, setEditCacheCreation] = useState('');

  const handleSavePrice = () => {
    if (!selectedModel) return;
    const prompt = parsePriceValue(promptPrice);
    const completion = parsePriceValue(completionPrice);
    if (prompt === null || completion === null) {
      onNotice?.('error', t('usage_stats.model_price_save_failed'));
      return;
    }
    const cache = parseCachePriceValue(cachePrice, pricingStyle, prompt);
    const cacheCreation = parseCacheCreationPriceValue(cacheCreationPrice, pricingStyle);
    if (cache === null || cacheCreation === null) {
      onNotice?.('error', t('usage_stats.model_price_save_failed'));
      return;
    }
    const newPrices = { ...modelPrices, [selectedModel]: { style: pricingStyle, prompt, completion, cache, cacheCreation } };
    onPricesChange(newPrices);
    onNotice?.('success', t('usage_stats.model_price_save_success'));
    setSelectedModel('');
    setPricingStyle('openai');
    setPromptPrice('');
    setCompletionPrice('');
    setCachePrice('');
    setCacheCreationPrice('');
  };

  const handleDeletePrice = (model: string) => {
    const newPrices = { ...modelPrices };
    delete newPrices[model];
    onPricesChange(newPrices);
    onNotice?.('success', t('usage_stats.model_price_delete_success'));
  };

  const handleOpenEdit = (model: string) => {
    const price = modelPrices[model];
    setEditModel(model);
    setEditStyle(price?.style ?? 'openai');
    setEditPrompt(price?.prompt?.toString() || '');
    setEditCompletion(price?.completion?.toString() || '');
    setEditCache(price?.cache?.toString() || '');
    setEditCacheCreation(price?.cacheCreation?.toString() || '');
    onNotice?.('info', t('usage_stats.model_price_edit_notice', { model: formatDisplayName(model) }));
  };

  const handleSaveEdit = () => {
    if (!editModel) return;
    const prompt = parsePriceValue(editPrompt);
    const completion = parsePriceValue(editCompletion);
    if (prompt === null || completion === null) {
      onNotice?.('error', t('usage_stats.model_price_edit_failed'));
      return;
    }
    const cache = parseCachePriceValue(editCache, editStyle, prompt);
    const cacheCreation = parseCacheCreationPriceValue(editCacheCreation, editStyle);
    if (cache === null || cacheCreation === null) {
      onNotice?.('error', t('usage_stats.model_price_edit_failed'));
      return;
    }
    const newPrices = { ...modelPrices, [editModel]: { style: editStyle, prompt, completion, cache, cacheCreation } };
    onPricesChange(newPrices);
    onNotice?.('success', t('usage_stats.model_price_edit_success'));
    setEditModel(null);
  };

  const handleModelSelect = (value: string) => {
    setSelectedModel(value);
    const price = modelPrices[value];
    if (price) {
      setPricingStyle(price.style);
      setPromptPrice(price.prompt.toString());
      setCompletionPrice(price.completion.toString());
      setCachePrice(price.cache.toString());
      setCacheCreationPrice(price.cacheCreation.toString());
    } else {
      setPricingStyle('openai');
      setPromptPrice('');
      setCompletionPrice('');
      setCachePrice('');
      setCacheCreationPrice('');
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
              <div className={styles.priceForm}>
                <div className={styles.formRow}>
                  <div className={styles.formField}>
                    <label>{t('usage_stats.model_name')}</label>
                    <Select
                      value={selectedModel}
                      options={options}
                      onChange={handleModelSelect}
                      placeholder={t('usage_stats.model_price_select_placeholder')}
                      className={styles.usagePillControl}
                    />
                  </div>
                  <div className={styles.formField}>
                    <label>{t('usage_stats.model_price_style')}</label>
                    <Select
                      value={pricingStyle}
                      options={styleOptions}
                      onChange={(value) => setPricingStyle(value === 'claude' ? 'claude' : 'openai')}
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
                        className={styles.usagePillControl}
                      />
                    </div>
                  )}
                  <Button variant="primary" className={styles.usagePillAction} onClick={handleSavePrice} disabled={!selectedModel}>
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
                          </div>
                        </div>
                        <div className={styles.priceActions}>
                          <Button variant="secondary" size="sm" className={styles.usagePillAction} onClick={() => handleOpenEdit(model)}>
                            {t('common.edit')}
                          </Button>
                          <Button variant="danger" size="sm" className={`${styles.usagePillAction} ${styles.usagePillActionDanger}`} onClick={() => handleDeletePrice(model)}>
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
        onClose={() => setEditModel(null)}
        footer={
          <div className={styles.priceActions}>
            <Button variant="secondary" className={styles.usagePillAction} onClick={() => setEditModel(null)}>
              {t('common.cancel')}
            </Button>
            <Button variant="primary" className={styles.usagePillAction} onClick={handleSaveEdit}>
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
                className={styles.usagePillControl}
              />
            </div>
          )}
        </div>
      </Modal>
    </>
  );
}
