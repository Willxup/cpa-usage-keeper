import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Select, type SelectOption } from '@/components/ui/Select';
import { IconCheck, IconCircleAlert, IconRefreshCw } from '@/components/ui/icons';
import { buildPricingEntryKey, normalizePricingServiceTier, type PricingEntry, type PricingSaveResult, type PricingServiceTier, type PricingStyle, type PricingSyncMatch, type PricingSyncPreviewResponse, type PricingSyncSource } from '@/lib/types';
import styles from '@/pages/UsagePage.module.scss';

const DEFAULT_PRICING_STYLE: PricingStyle = 'openai';
const DEFAULT_PRICING_SERVICE_TIER: PricingServiceTier = 'default';
const DEFAULT_PRICING_SYNC_SOURCE: PricingSyncSource = 'openai_official';

const formatDisplayName = (value: string): string => {
  const normalized = value.trim();
  if (!normalized) return '-';
  return normalized;
};

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

const priceToInputValue = (value: number | undefined): string => (
  typeof value === 'number' && Number.isFinite(value) ? value.toString() : ''
);

const normalizePricingStyle = (style: PricingStyle | string | undefined): PricingStyle => (
  style === 'claude' ? 'claude' : 'openai'
);

const pricingServiceTierOrder: Record<PricingServiceTier, number> = {
  '': 0,
  default: 1,
  priority: 2,
};

const sortPricingEntries = (entries: PricingEntry[]): PricingEntry[] => (
  [...entries].sort((left, right) => {
    const modelOrder = left.model.localeCompare(right.model);
    if (modelOrder !== 0) return modelOrder;
    return pricingServiceTierOrder[left.service_tier] - pricingServiceTierOrder[right.service_tier];
  })
);

const normalizePricingEntry = (entry: PricingEntry): PricingEntry => ({
  model: entry.model.trim(),
  service_tier: normalizePricingServiceTier(entry.service_tier),
  pricing_style: normalizePricingStyle(entry.pricing_style),
  prompt_price_per_1m: entry.prompt_price_per_1m,
  completion_price_per_1m: entry.completion_price_per_1m,
  cache_price_per_1m: entry.cache_price_per_1m,
  cache_creation_price_per_1m: entry.cache_creation_price_per_1m ?? 0,
});

const upsertPricingEntry = (entries: PricingEntry[], nextEntry: PricingEntry): PricingEntry[] => {
  const normalizedEntry = normalizePricingEntry(nextEntry);
  const nextEntries = new Map(entries.map((entry) => {
    const normalized = normalizePricingEntry(entry);
    return [buildPricingEntryKey(normalized), normalized] as const;
  }));
  nextEntries.set(buildPricingEntryKey(normalizedEntry), normalizedEntry);
  return sortPricingEntries(Array.from(nextEntries.values()));
};

const mergePricingEntries = (entries: PricingEntry[], nextEntries: PricingEntry[]): PricingEntry[] => (
  nextEntries.reduce((current, entry) => upsertPricingEntry(current, entry), entries)
);

const removePricingEntryByKey = (entries: PricingEntry[], pricingKey: string): PricingEntry[] => (
  entries.filter((entry) => buildPricingEntryKey(entry) !== pricingKey)
);

const findPricingEntry = (
  entries: PricingEntry[],
  model: string,
  serviceTier: PricingServiceTier,
): PricingEntry | undefined => {
  const pricingKey = buildPricingEntryKey({ model, service_tier: serviceTier });
  return entries.find((entry) => buildPricingEntryKey(entry) === pricingKey);
};

function PriceSettingsTitle({ title, subtitle }: { title: string; subtitle: string }) {
  return (
    <div className={styles.sectionTitleBlock}>
      <h3 className={styles.sectionTitle}>{title}</h3>
      <p className={styles.sectionSubtitle}>{subtitle}</p>
    </div>
  );
}

export interface PriceSettingsCardProps {
  modelNames: string[];
  pricingEntries: PricingEntry[];
  onPricingEntriesChange: (entries: PricingEntry[]) => void | Promise<void>;
  onSyncPricingEntriesChange?: (entries: PricingEntry[]) => Promise<PricingSaveResult>;
  onSyncPreview?: (source: PricingSyncSource) => Promise<PricingSyncPreviewResponse>;
  onNotice?: (kind: 'success' | 'info' | 'error', message: string) => void;
  loading?: boolean;
}

export interface PricingSyncDraft {
  model: string;
  serviceTier: PricingServiceTier;
  pricingKey: string;
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
  saveStatus?: 'failed';
  saveError?: string;
}

const pricingStyleOptions = (t: (key: string) => string): SelectOption[] => [
  { value: 'openai', label: t('usage_stats.model_price_style_openai') },
  { value: 'claude', label: t('usage_stats.model_price_style_claude') },
];

const pricingServiceTierLabel = (serviceTier: PricingServiceTier, t: (key: string) => string): string => {
  if (serviceTier === 'priority') return t('usage_stats.model_price_service_tier_priority');
  if (serviceTier === 'default') return t('usage_stats.model_price_service_tier_default');
  return t('usage_stats.model_price_service_tier_fallback');
};

const pricingServiceTierOptions = (t: (key: string) => string): SelectOption[] => [
  { value: 'default', label: t('usage_stats.model_price_service_tier_default') },
  { value: 'priority', label: t('usage_stats.model_price_service_tier_priority') },
  { value: '', label: t('usage_stats.model_price_service_tier_fallback') },
];

const pricingSyncSourceOptions = (t: (key: string) => string): SelectOption[] => [
  { value: 'openai_official', label: t('usage_stats.model_price_sync_source_openai_official') },
  { value: 'models_dev', label: t('usage_stats.model_price_sync_source_models_dev') },
];

const pricingSyncSourceLabel = (
  source: PricingSyncSource,
  t: (key: string) => string,
): string => (
  source === 'models_dev'
    ? t('usage_stats.model_price_sync_source_models_dev')
    : t('usage_stats.model_price_sync_source_openai_official')
);

const formatPricingEntryLabel = (
  model: string,
  serviceTier: PricingServiceTier,
  t: (key: string) => string,
): string => `${formatDisplayName(model)} · ${pricingServiceTierLabel(serviceTier, t)}`;

const pricingEntryFromForm = (
  model: string,
  serviceTier: PricingServiceTier,
  style: PricingStyle,
  promptValue: string,
  completionValue: string,
  cacheValue: string,
  cacheCreationValue: string,
): PricingEntry | null => {
  const prompt = parsePriceValue(promptValue);
  const completion = parsePriceValue(completionValue);
  if (prompt === null || completion === null) return null;
  const cache = parseCachePriceValue(cacheValue, style, prompt);
  const cacheCreation = parseCacheCreationPriceValue(cacheCreationValue, style);
  if (cache === null || cacheCreation === null) return null;
  return {
    model,
    service_tier: serviceTier,
    pricing_style: style,
    prompt_price_per_1m: prompt,
    completion_price_per_1m: completion,
    cache_price_per_1m: cache,
    cache_creation_price_per_1m: cacheCreation,
  };
};

const syncMatchToDraft = (match: PricingSyncMatch): PricingSyncDraft => {
  const serviceTier = normalizePricingServiceTier(match.service_tier);
  return {
    model: match.model,
    serviceTier,
    pricingKey: buildPricingEntryKey({ model: match.model, service_tier: serviceTier }),
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
  };
};

const syncDraftToPricingEntry = (draft: PricingSyncDraft): PricingEntry | null => (
  pricingEntryFromForm(
    draft.model,
    draft.serviceTier,
    draft.style,
    draft.prompt,
    draft.completion,
    draft.cache,
    draft.cacheCreation,
  )
);

export const markPricingSyncFailures = (
  drafts: PricingSyncDraft[],
  result: PricingSaveResult,
): PricingSyncDraft[] => {
  const failedByKey = new Map(result.failures.map((failure) => [failure.pricing_key, failure.message]));
  const successKeys = new Set(result.success_keys);
  return drafts.map((draft) => {
    const failureMessage = failedByKey.get(draft.pricingKey);
    if (failureMessage !== undefined) {
      return {
        ...draft,
        selected: true,
        saveStatus: 'failed',
        saveError: failureMessage,
      };
    }
    if (successKeys.has(draft.pricingKey)) {
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

export const buildPricingModelOptions = (
  modelNames: string[],
  pricingEntries: PricingEntry[],
  serviceTier: PricingServiceTier,
  placeholder: string,
  configuredLabel = 'Configured',
): SelectOption[] => {
  const configuredKeys = new Set(pricingEntries.map((entry) => buildPricingEntryKey(entry)));
  const sortedModelNames = [...modelNames]
    .sort((left, right) => formatDisplayName(left).localeCompare(formatDisplayName(right)));

  return [
    { value: '', label: placeholder },
    ...sortedModelNames.map((name) => {
      const configured = configuredKeys.has(buildPricingEntryKey({ model: name, service_tier: serviceTier }));
      return {
        value: name,
        label: formatDisplayName(name),
        suffix: configured ? <IconCheck size={12} /> : undefined,
        suffixAriaLabel: configured ? configuredLabel : undefined,
      };
    }),
  ];
};

const applyDraftValues = (
  entry: PricingEntry | undefined,
  setStyle: (value: PricingStyle) => void,
  setPrompt: (value: string) => void,
  setCompletion: (value: string) => void,
  setCache: (value: string) => void,
  setCacheCreation: (value: string) => void,
) => {
  if (!entry) {
    setStyle(DEFAULT_PRICING_STYLE);
    setPrompt('');
    setCompletion('');
    setCache('');
    setCacheCreation('');
    return;
  }
  setStyle(normalizePricingStyle(entry.pricing_style));
  setPrompt(priceToInputValue(entry.prompt_price_per_1m));
  setCompletion(priceToInputValue(entry.completion_price_per_1m));
  setCache(priceToInputValue(entry.cache_price_per_1m));
  setCacheCreation(priceToInputValue(entry.cache_creation_price_per_1m));
};

export function PriceSettingsCard({
  modelNames,
  pricingEntries,
  onPricingEntriesChange,
  onSyncPricingEntriesChange,
  onSyncPreview,
  onNotice,
  loading = false
}: PriceSettingsCardProps) {
  const { t } = useTranslation();

  const [selectedModel, setSelectedModel] = useState('');
  const [selectedServiceTier, setSelectedServiceTier] = useState<PricingServiceTier>(DEFAULT_PRICING_SERVICE_TIER);
  const [pricingStyle, setPricingStyle] = useState<PricingStyle>(DEFAULT_PRICING_STYLE);
  const [promptPrice, setPromptPrice] = useState('');
  const [completionPrice, setCompletionPrice] = useState('');
  const [cachePrice, setCachePrice] = useState('');
  const [cacheCreationPrice, setCacheCreationPrice] = useState('');

  const [editPricingKey, setEditPricingKey] = useState<string | null>(null);
  const [editModel, setEditModel] = useState<string | null>(null);
  const [editServiceTier, setEditServiceTier] = useState<PricingServiceTier>(DEFAULT_PRICING_SERVICE_TIER);
  const [editStyle, setEditStyle] = useState<PricingStyle>(DEFAULT_PRICING_STYLE);
  const [editPrompt, setEditPrompt] = useState('');
  const [editCompletion, setEditCompletion] = useState('');
  const [editCache, setEditCache] = useState('');
  const [editCacheCreation, setEditCacheCreation] = useState('');

  const [syncSource, setSyncSource] = useState<PricingSyncSource>(DEFAULT_PRICING_SYNC_SOURCE);
  const [syncOpen, setSyncOpen] = useState(false);
  const [syncLoading, setSyncLoading] = useState(false);
  const [syncApplying, setSyncApplying] = useState(false);
  const [syncPreview, setSyncPreview] = useState<PricingSyncPreviewResponse | null>(null);
  const [syncDrafts, setSyncDrafts] = useState<PricingSyncDraft[]>([]);

  useEffect(() => {
    if (!selectedModel) {
      applyDraftValues(undefined, setPricingStyle, setPromptPrice, setCompletionPrice, setCachePrice, setCacheCreationPrice);
      return;
    }
    applyDraftValues(
      findPricingEntry(pricingEntries, selectedModel, selectedServiceTier),
      setPricingStyle,
      setPromptPrice,
      setCompletionPrice,
      setCachePrice,
      setCacheCreationPrice,
    );
  }, [pricingEntries, selectedModel, selectedServiceTier]);

  const options = useMemo(
    () => buildPricingModelOptions(
      modelNames,
      pricingEntries,
      selectedServiceTier,
      t('usage_stats.model_price_select_placeholder'),
      t('usage_stats.model_price_configured'),
    ),
    [modelNames, pricingEntries, selectedServiceTier, t]
  );
  const styleOptions = useMemo(() => pricingStyleOptions(t), [t]);
  const serviceTierOptions = useMemo(() => pricingServiceTierOptions(t), [t]);
  const syncSourceOptions = useMemo(() => pricingSyncSourceOptions(t), [t]);
  const selectedSyncCount = useMemo(
    () => syncDrafts.filter((draft) => draft.selected).length,
    [syncDrafts]
  );
  const syncSourceName = useMemo(
    () => pricingSyncSourceLabel(syncSource, t),
    [syncSource, t]
  );
  const sortedPricingEntries = useMemo(
    () => sortPricingEntries(pricingEntries.map(normalizePricingEntry)),
    [pricingEntries]
  );

  const handleSavePrice = async () => {
    if (!selectedModel) return;
    const nextEntry = pricingEntryFromForm(
      selectedModel,
      selectedServiceTier,
      pricingStyle,
      promptPrice,
      completionPrice,
      cachePrice,
      cacheCreationPrice,
    );
    if (!nextEntry) {
      onNotice?.('error', t('usage_stats.model_price_save_failed'));
      return;
    }
    await Promise.resolve(onPricingEntriesChange(upsertPricingEntry(pricingEntries, nextEntry)));
    onNotice?.('success', t('usage_stats.model_price_save_success'));
    setSelectedModel('');
    applyDraftValues(undefined, setPricingStyle, setPromptPrice, setCompletionPrice, setCachePrice, setCacheCreationPrice);
  };

  const handleDeletePrice = async (entry: PricingEntry) => {
    await Promise.resolve(onPricingEntriesChange(removePricingEntryByKey(pricingEntries, buildPricingEntryKey(entry))));
    onNotice?.('success', t('usage_stats.model_price_delete_success'));
  };

  const handleOpenEdit = (entry: PricingEntry) => {
    const normalized = normalizePricingEntry(entry);
    setEditPricingKey(buildPricingEntryKey(normalized));
    setEditModel(normalized.model);
    setEditServiceTier(normalized.service_tier);
    setEditStyle(normalizePricingStyle(normalized.pricing_style));
    setEditPrompt(priceToInputValue(normalized.prompt_price_per_1m));
    setEditCompletion(priceToInputValue(normalized.completion_price_per_1m));
    setEditCache(priceToInputValue(normalized.cache_price_per_1m));
    setEditCacheCreation(priceToInputValue(normalized.cache_creation_price_per_1m));
    onNotice?.('info', t('usage_stats.model_price_edit_notice', { model: formatPricingEntryLabel(normalized.model, normalized.service_tier, t) }));
  };

  const handleSaveEdit = async () => {
    if (!editModel || !editPricingKey) return;
    const updatedEntry = pricingEntryFromForm(
      editModel,
      editServiceTier,
      editStyle,
      editPrompt,
      editCompletion,
      editCache,
      editCacheCreation,
    );
    if (!updatedEntry) {
      onNotice?.('error', t('usage_stats.model_price_edit_failed'));
      return;
    }
    const nextEntries = upsertPricingEntry(
      removePricingEntryByKey(pricingEntries, editPricingKey),
      updatedEntry,
    );
    await Promise.resolve(onPricingEntriesChange(nextEntries));
    onNotice?.('success', t('usage_stats.model_price_edit_success'));
    setEditPricingKey(null);
    setEditModel(null);
  };

  const handleOpenSyncPreview = async () => {
    if (!onSyncPreview || syncLoading) return;
    setSyncLoading(true);
    try {
      const preview = await onSyncPreview(syncSource);
      const drafts = (preview.matches ?? []).map(syncMatchToDraft);
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
    setSyncDrafts((current) => current.map((draft, draftIndex) => {
      if (draftIndex !== index) return draft;
      const nextDraft = { ...draft, ...patch };
      if (patch.serviceTier !== undefined) {
        nextDraft.serviceTier = normalizePricingServiceTier(patch.serviceTier);
      }
      nextDraft.pricingKey = buildPricingEntryKey({
        model: nextDraft.model,
        service_tier: nextDraft.serviceTier,
      });
      return {
        ...nextDraft,
        ...(clearsFailure ? { saveStatus: undefined, saveError: undefined } : {}),
      };
    }));
  };

  const handleSetAllSyncDrafts = (selected: boolean) => {
    setSyncDrafts((current) => current.map((draft) => ({ ...draft, selected })));
  };

  const handleApplySyncDrafts = async () => {
    const selectedDrafts = syncDrafts.filter((draft) => draft.selected);
    if (selectedDrafts.length === 0) {
      onNotice?.('error', t('usage_stats.model_price_sync_none_selected'));
      return;
    }

    const syncEntries: PricingEntry[] = [];
    for (const draft of selectedDrafts) {
      const entry = syncDraftToPricingEntry(draft);
      if (!entry) {
        onNotice?.('error', t('usage_stats.model_price_sync_invalid', { model: formatPricingEntryLabel(draft.model, draft.serviceTier, t) }));
        return;
      }
      syncEntries.push(entry);
    }

    setSyncApplying(true);
    try {
      if (!onSyncPricingEntriesChange) {
        await Promise.resolve(onPricingEntriesChange(mergePricingEntries(pricingEntries, syncEntries)));
        onNotice?.('success', t('usage_stats.model_price_sync_apply_success', { count: selectedDrafts.length }));
        setSyncOpen(false);
        return;
      }

      const result = await onSyncPricingEntriesChange(syncEntries);
      setSyncDrafts((current) => markPricingSyncFailures(current, result));
      if (result.failures.length === 0) {
        onNotice?.('success', t('usage_stats.model_price_sync_apply_success', { count: result.success_keys.length }));
        setSyncOpen(false);
        return;
      }

      onNotice?.(
        result.success_keys.length > 0 ? 'info' : 'error',
        t('usage_stats.model_price_sync_apply_partial', {
          success: result.success_keys.length,
          failed: result.failures.length,
        }),
      );
    } catch (error) {
      notifyPricingSyncUnexpectedError(error, t, onNotice);
    } finally {
      setSyncApplying(false);
    }
  };

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
          {loading && modelNames.length === 0 && pricingEntries.length === 0 ? (
            <div className={styles.hint}>{t('common.loading')}</div>
          ) : (
            <>
              {onSyncPreview && (
                <div className={styles.pricingToolbar}>
                  <div className={styles.pricingToolbarMeta}>
                    <span>{t('usage_stats.model_price_sync_source')}</span>
                    <Select
                      value={syncSource}
                      options={syncSourceOptions}
                      onChange={(value) => setSyncSource(value === 'models_dev' ? 'models_dev' : 'openai_official')}
                      className={styles.usagePillControl}
                    />
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
                      onChange={setSelectedModel}
                      placeholder={t('usage_stats.model_price_select_placeholder')}
                      className={styles.usagePillControl}
                    />
                  </div>
                  <div className={styles.formField}>
                    <label>{t('usage_stats.model_price_service_tier')}</label>
                    <Select
                      value={selectedServiceTier}
                      options={serviceTierOptions}
                      onChange={(value) => setSelectedServiceTier(normalizePricingServiceTier(value))}
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
                  <Button variant="primary" className={styles.usagePillAction} onClick={() => void handleSavePrice()} disabled={!selectedModel}>
                    {t('common.save')}
                  </Button>
                </div>
              </div>

              <div className={styles.pricesList}>
                <h4 className={styles.pricesTitle}>{t('usage_stats.saved_prices')}</h4>
                {sortedPricingEntries.length > 0 ? (
                  <div className={styles.pricesGrid}>
                    {sortedPricingEntries.map((entry) => (
                      <div key={buildPricingEntryKey(entry)} className={styles.priceItem}>
                        <div className={styles.priceInfo}>
                          <span className={styles.priceModel}>{formatDisplayName(entry.model)}</span>
                          <div className={styles.priceMeta}>
                            <span>
                              {t('usage_stats.model_price_service_tier')}: {pricingServiceTierLabel(entry.service_tier, t)}
                            </span>
                            <span>
                              {t('usage_stats.model_price_style')}: {t(entry.pricing_style === 'claude' ? 'usage_stats.model_price_style_claude' : 'usage_stats.model_price_style_openai')}
                            </span>
                            <span>
                              {t('usage_stats.model_price_prompt')}: ${entry.prompt_price_per_1m.toFixed(4)}/1M
                            </span>
                            <span>
                              {t('usage_stats.model_price_completion')}: ${entry.completion_price_per_1m.toFixed(4)}/1M
                            </span>
                            <span>
                              {t(entry.pricing_style === 'claude' ? 'usage_stats.model_price_cache_read' : 'usage_stats.model_price_cache')}: ${entry.cache_price_per_1m.toFixed(4)}/1M
                            </span>
                            {entry.pricing_style === 'claude' && (
                              <span>
                                {t('usage_stats.model_price_cache_write')}: ${entry.cache_creation_price_per_1m.toFixed(4)}/1M
                              </span>
                            )}
                          </div>
                        </div>
                        <div className={styles.priceActions}>
                          <Button variant="secondary" size="sm" className={styles.usagePillAction} onClick={() => handleOpenEdit(entry)}>
                            {t('common.edit')}
                          </Button>
                          <Button variant="danger" size="sm" className={`${styles.usagePillAction} ${styles.usagePillActionDanger}`} onClick={() => void handleDeletePrice(entry)}>
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

      <Modal
        open={editModel !== null}
        title={editModel ? formatPricingEntryLabel(editModel, editServiceTier, t) : ''}
        onClose={() => {
          setEditPricingKey(null);
          setEditModel(null);
        }}
        footer={
          <div className={styles.priceActions}>
            <Button variant="secondary" className={styles.usagePillAction} onClick={() => {
              setEditPricingKey(null);
              setEditModel(null);
            }}>
              {t('common.cancel')}
            </Button>
            <Button variant="primary" className={styles.usagePillAction} onClick={() => void handleSaveEdit()}>
              {t('common.save')}
            </Button>
          </div>
        }
        width={420}
      >
        <div className={styles.editModalBody}>
          <div className={styles.formField}>
            <label>{t('usage_stats.model_price_service_tier')}</label>
            <Select
              value={editServiceTier}
              options={serviceTierOptions}
              onChange={(value) => setEditServiceTier(normalizePricingServiceTier(value))}
              className={styles.usagePillControl}
            />
          </div>
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
              {t('usage_stats.model_price_sync_source')}: {syncPreview?.source || syncSourceName}
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
                  const existing = pricingEntries.some((entry) => buildPricingEntryKey(entry) === draft.pricingKey);
                  const failed = draft.saveStatus === 'failed';
                  const failureLabel = t('usage_stats.model_price_sync_failed_label', {
                    model: formatPricingEntryLabel(draft.model, draft.serviceTier, t),
                  });
                  return (
                    <div
                      key={`${draft.pricingKey}-${draft.matchedModel}`}
                      className={`${styles.syncDraftItem} ${failed ? styles.syncDraftItemFailed : ''}`}
                    >
                      <label className={styles.syncDraftCheck}>
                        <input
                          type="checkbox"
                          checked={draft.selected}
                          disabled={syncApplying}
                          onChange={(event) => handleUpdateSyncDraft(index, { selected: event.target.checked })}
                          aria-label={t('usage_stats.model_price_sync_toggle', { model: formatPricingEntryLabel(draft.model, draft.serviceTier, t) })}
                        />
                      </label>
                      <div className={styles.syncDraftContent}>
                        <div className={styles.syncDraftHeader}>
                          <div className={styles.syncDraftModelBlock}>
                            <span className={styles.priceModel}>{formatDisplayName(draft.model)}</span>
                            <span className={styles.syncDraftMatched}>
                              {t('usage_stats.model_price_service_tier')}: {pricingServiceTierLabel(draft.serviceTier, t)}
                            </span>
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
                            <label>{t('usage_stats.model_price_service_tier')}</label>
                            <Select
                              value={draft.serviceTier}
                              options={serviceTierOptions}
                              onChange={(value) => handleUpdateSyncDraft(index, { serviceTier: normalizePricingServiceTier(value) })}
                              className={styles.usagePillControl}
                            />
                          </div>
                          <div className={styles.formField}>
                            <label>{t('usage_stats.model_price_style')}</label>
                            <Select
                              value={draft.style}
                              options={styleOptions}
                              onChange={(value) => handleUpdateSyncDraft(index, { style: value === 'claude' ? 'claude' : 'openai' })}
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
                                className={styles.usagePillControl}
                              />
                            </div>
                          )}
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
