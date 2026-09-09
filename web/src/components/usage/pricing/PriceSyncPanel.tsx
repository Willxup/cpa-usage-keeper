import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Select } from '@/components/ui/Select';
import { IconCircleAlert, IconRefreshCw } from '@/components/ui/icons';
import type { ModelPrice, PricingSaveResult, PricingSyncPreviewResponse, PricingSyncSource } from '@/lib/types';
import { buildSelectedSyncPrices, formatDisplayName, markPricingSyncFailures, notifyPricingSyncFailures,
  notifyPricingSyncUnexpectedError, pricingStyleOptions, PRICING_SYNC_SOURCES, readPricingSyncSource,
  storePricingSyncSource, syncMatchToDraft, type PricingNotice, type PricingSyncDraft } from './pricingDrafts';
import styles from '@/pages/UsagePage.module.scss';

interface PriceSyncPanelProps {
  modelPrices: Record<string, ModelPrice>;
  onSyncPreview: (source: PricingSyncSource, signal?: AbortSignal) => Promise<PricingSyncPreviewResponse>;
  onSyncPricesChange: (prices: Record<string, ModelPrice>) => Promise<PricingSaveResult>;
  onNotice?: PricingNotice;
}

export function PriceSyncPanel({ modelPrices, onSyncPreview, onSyncPricesChange, onNotice }: PriceSyncPanelProps) {
  const { t } = useTranslation();
  const [source, setSource] = useState<PricingSyncSource>(readPricingSyncSource);
  const requestRef = useRef<AbortController | null>(null);
  const sourceName = PRICING_SYNC_SOURCES.find((item) => item.value === source)!.label;
  const styleOptions = useMemo(() => pricingStyleOptions(t), [t]);
  useEffect(() => () => { requestRef.current?.abort(); }, []);
  const [syncOpen, setSyncOpen] = useState(false);
  const [syncLoading, setSyncLoading] = useState(false);
  const [syncApplying, setSyncApplying] = useState(false);
  const [syncPreview, setSyncPreview] = useState<PricingSyncPreviewResponse | null>(null);
  const [syncDrafts, setSyncDrafts] = useState<PricingSyncDraft[]>([]);

  const handleSourceChange = (value: string) => {
    const nextSource = value === 'litellm' ? 'litellm' : 'models-dev';
    // 切换来源立即作废旧请求和草稿，防止晚返回的数据覆盖当前预览。
    requestRef.current?.abort();
    requestRef.current = null;
    setSyncLoading(false);
    setSyncOpen(false);
    setSyncPreview(null);
    setSyncDrafts([]);
    setSource(nextSource);
    storePricingSyncSource(nextSource);
  };

  const handleOpenSyncPreview = async () => {
    if (syncLoading) return;
    const request = new AbortController();
    requestRef.current = request;
    setSyncLoading(true);
    try {
      const preview = await onSyncPreview(source, request.signal);
      if (request.signal.aborted || requestRef.current !== request) return;
      const drafts = (preview.matches ?? []).map((match) => syncMatchToDraft(match, modelPrices[match.model]));
      setSyncPreview({ ...preview, matches: preview.matches ?? [], unmatched_models: preview.unmatched_models ?? [] });
      setSyncDrafts(drafts);
      setSyncOpen(true);
      if (drafts.length === 0) onNotice?.('info', t('usage_stats.model_price_sync_no_matches'));
    } catch (error) {
      if (!request.signal.aborted && requestRef.current === request) {
        notifyPricingSyncUnexpectedError(error, t, onNotice, sourceName);
      }
    } finally {
      if (requestRef.current === request) {
        requestRef.current = null;
        setSyncLoading(false);
      }
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
      const result = await onSyncPricesChange(syncPrices);
      setSyncDrafts((current) => markPricingSyncFailures(current, result));
      if (result.failures.length === 0) {
        onNotice?.('success', t('usage_stats.model_price_sync_apply_success', { count: result.successModels.length }));
        setSyncOpen(false);
        return;
      }
      notifyPricingSyncFailures(result, t, onNotice);
    } catch (error) {
      notifyPricingSyncUnexpectedError(error, t, onNotice, sourceName);
    } finally {
      setSyncApplying(false);
    }
  };

  const selectedSyncCount = syncDrafts.filter((draft) => draft.selected).length;
  return (
    <>
      <div className={styles.pricingToolbar}>
        <div className={styles.pricingToolbarMeta}>
          <span>{t('usage_stats.model_price_sync_source')}:</span>
          <Select value={source} options={PRICING_SYNC_SOURCES} onChange={handleSourceChange}
            ariaLabel={t('usage_stats.model_price_sync_source') + ': ' + sourceName}
            disabled={syncApplying} className={styles.usagePillControl} />
        </div>
        <Button variant="secondary" appearance="action" onClick={() => void handleOpenSyncPreview()} loading={syncLoading}>
          <IconRefreshCw size={14} />
          {t('usage_stats.model_price_sync')}
        </Button>
      </div>
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
              appearance="action"
              onClick={() => setSyncOpen(false)}
              disabled={syncApplying}
            >
              {t('common.cancel')}
            </Button>
            <Button
              variant="primary"
              appearance="action"
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
              {t('usage_stats.model_price_sync_source')}: <a href={syncPreview?.source_url} target="_blank" rel="noreferrer">{syncPreview?.source || sourceName}</a>
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
                  appearance="action"
                  onClick={() => handleSetAllSyncDrafts(true)}
                  disabled={syncApplying}
                >
                  {t('usage_stats.model_price_sync_select_all')}
                </Button>
                <Button
                  variant="secondary"
                  size="sm"
                  appearance="action"
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
                            <label>{t('usage_stats.model_price_cache_read')} ($/1M)</label>
                            <Input
                              type="number"
                              value={draft.cacheRead}
                              onChange={(event) => handleUpdateSyncDraft(index, { cacheRead: event.target.value })}
                              placeholder="0.00"
                              step="0.0001"
                              disabled={syncApplying}
                              className={styles.usagePillControl}
                            />
                          </div>
                          <div className={styles.formField}>
                            <label>{t('usage_stats.model_price_cache_write')} ($/1M)</label>
                            <Input
                              type="number"
                              value={draft.cacheWrite}
                              onChange={(event) => handleUpdateSyncDraft(index, { cacheWrite: event.target.value })}
                              placeholder="0.00"
                              step="0.0001"
                              disabled={syncApplying}
                              className={styles.usagePillControl}
                            />
                          </div>
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
