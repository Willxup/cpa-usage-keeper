import { useState, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Select, type SelectOption } from '@/components/ui/Select';
import { IconCheck } from '@/components/ui/icons';
import type { ModelPrice } from '@/utils/usage';
import type { PricingModelOption } from '@/lib/types';
import styles from '@/pages/UsagePage.module.scss';

const formatDisplayName = (value: string): string => {
  const normalized = value.trim();
  if (!normalized) return '-';
  return normalized;
};

export interface PriceSettingsCardProps {
  modelNames: string[];
  modelOptions?: PricingModelOption[];
  modelPrices: Record<string, ModelPrice>;
  onPricesChange: (prices: Record<string, ModelPrice>) => void;
  loading?: boolean;
}

function PriceSettingsTitle({ title, subtitle, eyebrow }: { title: string; subtitle: string; eyebrow: string }) {
  return (
    <div className={styles.sectionTitleBlock}>
      <span className={styles.sectionEyebrow}>{eyebrow}</span>
      <h3 className={styles.sectionTitle}>{title}</h3>
      <p className={styles.sectionSubtitle}>{subtitle}</p>
    </div>
  );
}

const parsePriceValue = (value: string): number | null => {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null;
};

export const buildPricingModelOptions = (
  modelOptions: PricingModelOption[],
  selectedSource: string,
  modelPrices: Record<string, ModelPrice>,
  placeholder: string,
  configuredSuffix: React.ReactNode,
  configuredLabel: string,
): SelectOption[] => {
  const configuredModels = new Set(Object.keys(modelPrices));
  const source = selectedSource.trim();
  const seenModels = new Set<string>();
  const sortedModelOptions = modelOptions
    .filter((option) => option.source.trim() === source && option.model.trim() !== '')
    .filter((option) => {
      const model = option.model.trim();
      if (seenModels.has(model)) return false;
      seenModels.add(model);
      return true;
    })
    .sort((left, right) => {
      const leftConfigured = configuredModels.has(left.value);
      const rightConfigured = configuredModels.has(right.value);
      if (leftConfigured !== rightConfigured) return leftConfigured ? 1 : -1;
      return formatDisplayName(left.model).localeCompare(formatDisplayName(right.model));
    });

  return [
    { value: '', label: placeholder },
    ...sortedModelOptions.map((option) => ({
      value: option.model,
      label: formatDisplayName(option.model),
      suffix: configuredModels.has(option.value) ? configuredSuffix : undefined,
      suffixAriaLabel: configuredModels.has(option.value) ? configuredLabel : undefined,
    })),
  ];
};

export const buildPricingProviderOptions = (
  modelOptions: PricingModelOption[],
  defaultLabel: string,
): SelectOption[] => {
  const sources = Array.from(new Set(modelOptions.map((option) => option.source.trim()).filter(Boolean)));
  sources.sort((left, right) => formatDisplayName(left).localeCompare(formatDisplayName(right)));
  return [
    { value: '', label: defaultLabel },
    ...sources.map((source) => ({ value: source, label: formatDisplayName(source) })),
  ];
};

export const normalizePricingModelOptions = (
  modelNames: string[],
  modelOptions?: PricingModelOption[],
): PricingModelOption[] => {
  const candidates = modelOptions && modelOptions.length > 0
    ? modelOptions
    : modelNames.map((model) => ({ value: model, source: '', model }));
  const normalized: PricingModelOption[] = [];
  const seen = new Set<string>();
  for (const option of candidates) {
    const value = option.value.trim();
    const source = option.source.trim();
    const model = option.model.trim();
    if (!value || !model) continue;
    const key = `${source}\x00${model}\x00${value}`;
    if (seen.has(key)) continue;
    seen.add(key);
    normalized.push({ value, source, model });
  }
  return normalized;
};

export function PriceSettingsCard({
  modelNames,
  modelOptions,
  modelPrices,
  onPricesChange,
  loading = false
}: PriceSettingsCardProps) {
  const { t } = useTranslation();

  // 新增价格表单先暂存输入值，保存成功后再一次性同步到父级配置。
  const [selectedSource, setSelectedSource] = useState('');
  const [selectedModel, setSelectedModel] = useState('');
  const [promptPrice, setPromptPrice] = useState('');
  const [completionPrice, setCompletionPrice] = useState('');
  const [cachePrice, setCachePrice] = useState('');

  // 编辑弹窗独立保存草稿值，避免用户取消时污染已保存价格。
  const [editModel, setEditModel] = useState<string | null>(null);
  const [editPrompt, setEditPrompt] = useState('');
  const [editCompletion, setEditCompletion] = useState('');
  const [editCache, setEditCache] = useState('');

  const normalizedModelOptions = useMemo(
    () => normalizePricingModelOptions(modelNames, modelOptions),
    [modelNames, modelOptions]
  );

  const selectedPricingOption = useMemo(
    () => normalizedModelOptions.find((option) => option.source === selectedSource && option.model === selectedModel),
    [normalizedModelOptions, selectedModel, selectedSource]
  );

  const clearDraftPrices = () => {
    setPromptPrice('');
    setCompletionPrice('');
    setCachePrice('');
  };

  const handleSavePrice = () => {
    if (!selectedPricingOption) return;
    const prompt = parsePriceValue(promptPrice);
    const completion = parsePriceValue(completionPrice);
    const cache = cachePrice.trim() === '' ? prompt : parsePriceValue(cachePrice);
    if (prompt === null || completion === null || cache === null) return;
    const newPrices = { ...modelPrices, [selectedPricingOption.value]: { prompt, completion, cache } };
    onPricesChange(newPrices);
    setSelectedSource('');
    setSelectedModel('');
    clearDraftPrices();
  };

  const handleDeletePrice = (model: string) => {
    const newPrices = { ...modelPrices };
    delete newPrices[model];
    onPricesChange(newPrices);
  };

  const handleOpenEdit = (model: string) => {
    const price = modelPrices[model];
    setEditModel(model);
    setEditPrompt(price?.prompt?.toString() || '');
    setEditCompletion(price?.completion?.toString() || '');
    setEditCache(price?.cache?.toString() || '');
  };

  const handleSaveEdit = () => {
    if (!editModel) return;
    const prompt = parsePriceValue(editPrompt);
    const completion = parsePriceValue(editCompletion);
    const cache = editCache.trim() === '' ? prompt : parsePriceValue(editCache);
    if (prompt === null || completion === null || cache === null) return;
    const newPrices = { ...modelPrices, [editModel]: { prompt, completion, cache } };
    onPricesChange(newPrices);
    setEditModel(null);
  };

  const handleSourceSelect = (value: string) => {
    setSelectedSource(value);
    setSelectedModel('');
    clearDraftPrices();
  };

  const handleModelSelect = (model: string) => {
    setSelectedModel(model);
    const option = normalizedModelOptions.find((item) => item.source === selectedSource && item.model === model);
    const price = option ? modelPrices[option.value] : undefined;
    if (price) {
      setPromptPrice(price.prompt.toString());
      setCompletionPrice(price.completion.toString());
      setCachePrice(price.cache.toString());
    } else {
      clearDraftPrices();
    }
  };

  const providerOptions = useMemo(
    () => buildPricingProviderOptions(
      normalizedModelOptions,
      t('usage_stats.model_price_provider_default')
    ),
    [normalizedModelOptions, t]
  );

  const modelSelectOptions = useMemo(
    () => buildPricingModelOptions(
      normalizedModelOptions,
      selectedSource,
      modelPrices,
      t('usage_stats.model_price_select_placeholder'),
      <IconCheck size={10} />,
      t('usage_stats.model_price_configured')
    ),
    [modelPrices, normalizedModelOptions, selectedSource, t]
  );

  return (
    <Card
      title={
        <PriceSettingsTitle
          eyebrow={t('usage_stats.model_price_settings_eyebrow')}
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
                <div className={`${styles.formField} ${styles.priceSourceField}`.trim()}>
                  <label>{t('usage_stats.model_price_provider')}</label>
                  <Select
                    value={selectedSource}
                    options={providerOptions}
                    onChange={handleSourceSelect}
                    placeholder={t('usage_stats.model_price_provider_default')}
                    className={styles.usagePillControl}
                    dropdownMinWidth={320}
                  />
                </div>
                <div className={`${styles.formField} ${styles.priceModelField}`.trim()}>
                  <label>{t('usage_stats.model_name')}</label>
                  <Select
                    value={selectedModel}
                    options={modelSelectOptions}
                    onChange={handleModelSelect}
                    placeholder={t('usage_stats.model_price_select_placeholder')}
                    className={styles.usagePillControl}
                    dropdownMinWidth={360}
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
                  <label>{t('usage_stats.model_price_cache')} ($/1M)</label>
                  <Input
                    type="number"
                    value={cachePrice}
                    onChange={(e) => setCachePrice(e.target.value)}
                    placeholder="0.00"
                    step="0.0001"
                    className={styles.usagePillControl}
                  />
                </div>
                <Button variant="primary" className={styles.usagePillAction} onClick={handleSavePrice} disabled={!selectedPricingOption}>
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
                            {t('usage_stats.model_price_prompt')}: ${price.prompt.toFixed(4)}/1M
                          </span>
                          <span>
                            {t('usage_stats.model_price_completion')}: ${price.completion.toFixed(4)}/1M
                          </span>
                          <span>
                            {t('usage_stats.model_price_cache')}: ${price.cache.toFixed(4)}/1M
                          </span>
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

      {/* 编辑弹窗沿用同一套价格输入和操作按钮样式。 */}
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
            <label>{t('usage_stats.model_price_cache')} ($/1M)</label>
            <Input
              type="number"
              value={editCache}
              onChange={(e) => setEditCache(e.target.value)}
              placeholder="0.00"
              step="0.0001"
              className={styles.usagePillControl}
            />
          </div>
        </div>
      </Modal>
    </Card>
  );
}
