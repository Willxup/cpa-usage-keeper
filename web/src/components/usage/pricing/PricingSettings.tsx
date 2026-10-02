import { forwardRef, useImperativeHandle, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/Button'
import { IconHistory, IconPlus, IconRefreshCw, IconTrash2 } from '@/components/ui/icons'
import { Modal } from '@/components/ui/Modal'
import { useModelPricingData } from '@/components/usage/hooks/useModelPricingData'
import { ApiError } from '@/lib/api'
import type { ModelPricingConfig, PricingBasePrices, PricingRecalculationTask } from '@/lib/types'
import { CompletePricingSyncModal } from './CompletePricingSyncModal'
import { ModelPricingEditor } from './ModelPricingEditor'
import { PricingRecalculationModal } from './PricingRecalculationModal'
import { PricingRuleCounts } from './PricingRuleCounts'
import { usePricingRecalculation } from './usePricingRecalculation'
import styles from './PricingSettings.module.scss'

type PriceKey = keyof PricingBasePrices
const priceKeys: PriceKey[] = ['input', 'output', 'cache_read', 'cache_write']

export interface PricingSettingsProps {
  enabled?: boolean
  onAuthRequired?: () => void
  timezone?: string
  onRecalculationSettled?: (task: PricingRecalculationTask) => void
}

export interface PricingSettingsHandle {
  refresh: () => Promise<void>
}

// 完整模型配置列表负责添加、编辑与删除；写成功后重读配置，不改写历史费用。
export const PricingSettings = forwardRef<PricingSettingsHandle, PricingSettingsProps>(function PricingSettings(
  { enabled = true, onAuthRequired, timezone, onRecalculationSettled }: PricingSettingsProps,
  ref,
) {
  const { t } = useTranslation()
  const pricing = useModelPricingData({ enabled, onAuthRequired })
  const recalculation = usePricingRecalculation({ enabled, onAuthRequired, onSettled: (task) => {
    // 本页观测终态后实际重读当前价格列表；额度的旧本地响应由外层页面一并退休。
    void pricing.loadPricing()
    onRecalculationSettled?.(task)
  } })
  const { loadPricing } = pricing
  const { refreshCurrent } = recalculation
  // 顶部“刷新”沿用页面唯一动作，同时重读价格快照及别页可能启动的当前任务。
  useImperativeHandle(ref, () => ({
    refresh: async () => { await Promise.all([loadPricing(), refreshCurrent()]) },
  }), [loadPricing, refreshCurrent])
  const recalculationRunning = recalculation.task?.status === 'running'
  const pricingLocked = !enabled || recalculationRunning || Boolean(recalculation.connectionError) ||
    (recalculation.loading && !recalculation.task)
  const [editorConfig, setEditorConfig] = useState<ModelPricingConfig | null>(null)
  const [editorOpen, setEditorOpen] = useState(false)
  const [editorKey, setEditorKey] = useState(0)
  const [deleteTarget, setDeleteTarget] = useState<ModelPricingConfig | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState('')
  const [notice, setNotice] = useState('')
  const [syncOpen, setSyncOpen] = useState(false)
  const [recalculationOpen, setRecalculationOpen] = useState(false)

  // 新增入口只列尚未配置的模型；编辑始终保留完整的现有配置。
  const availableModels = useMemo(() => {
    const saved = new Set(pricing.models.map((config) => config.model))
    return pricing.modelOptions.filter((model) => !saved.has(model))
  }, [pricing.modelOptions, pricing.models])

  // 每次打开创建独立草稿，取消不改变服务端配置。
  const openEditor = (config: ModelPricingConfig | null) => {
    setEditorConfig(config)
    setEditorKey((key) => key + 1)
    setNotice('')
    setEditorOpen(true)
  }

  const saveModel = async (config: ModelPricingConfig) => {
    if (pricingLocked) throw new Error(t('usage_stats.pricing_recalculation_busy'))
    // 写入成功与随后列表刷新失败分开呈现，避免把已保存操作误报为失败。
    try {
      const outcome = await pricing.saveModel(config)
      setNotice(outcome.refreshed ? t('usage_stats.pricing_settings_saved') : t('usage_stats.pricing_settings_saved_refresh_failed'))
    } catch (failure) {
      if (failure instanceof ApiError && failure.code === 'pricing_busy') void recalculation.refreshCurrent()
      throw failure
    }
  }

  // 确认后删除当前配置；失败保留确认框，已提交但刷新失败单独告知。
  const deleteModel = async () => {
    if (!deleteTarget || deleting || pricingLocked) return
    setDeleting(true)
    setDeleteError('')
    try {
      const outcome = await pricing.deleteModel(deleteTarget.model)
      setDeleteTarget(null)
      setNotice(outcome.refreshed ? t('usage_stats.pricing_settings_deleted') : t('usage_stats.pricing_settings_saved_refresh_failed'))
    } catch (failure) {
      if (failure instanceof ApiError && failure.code === 'pricing_busy') void recalculation.refreshCurrent()
      setDeleteError(failure instanceof Error ? failure.message : t('usage_stats.pricing_settings_delete_failed'))
    } finally {
      setDeleting(false)
    }
  }

  return <section className={styles.settings} aria-label={t('usage_stats.model_price_settings_title')}>
    <div className={styles.listHeader}>
      <h2>{t('usage_stats.model_price_settings_title')}</h2>
      <div className={styles.headerActions}>
        <Button type="button" variant="secondary" appearance="action" disabled={pricingLocked || pricing.loading || Boolean(pricing.error)}
          onClick={() => { setNotice(''); setSyncOpen(true) }}><IconRefreshCw size={16} />{t('usage_stats.pricing_settings_sync_title')}</Button>
        <Button type="button" variant="secondary" appearance="action" disabled={pricingLocked || pricing.loading || Boolean(pricing.error) || pricing.configRevision === null}
          onClick={() => { setNotice(''); setRecalculationOpen(true) }}><IconHistory size={16} />{t('usage_stats.pricing_recalculation_title')}</Button>
        <Button type="button" appearance="action" disabled={pricingLocked || !availableModels.length || pricing.loading}
          onClick={() => openEditor(null)}><IconPlus size={16} />{t('usage_stats.pricing_settings_add')}</Button>
      </div>
    </div>
    {recalculationRunning && recalculation.task ? <div className={styles.recalculationBanner} role="status">
      <span>{t(`usage_stats.pricing_recalculation_${recalculation.task.status === 'running' ? recalculation.task.stage : recalculation.task.status}`)}
        {recalculation.task.status === 'running' && recalculation.task.stage === 'events' && recalculation.task.total_count && recalculation.task.total_count > 0
          ? ` · ${Math.min(100, Math.round(recalculation.task.processed_count / recalculation.task.total_count * 100))}%` : ''}</span>
      {recalculationRunning ? <Button type="button" variant="secondary" appearance="action" onClick={() => setRecalculationOpen(true)}>
        {t('usage_stats.pricing_recalculation_view_progress')}</Button> : null}
    </div> : null}
    {recalculation.connectionError ? <div className={styles.listError} role="status">
      <span>{t('usage_stats.pricing_recalculation_connection_lost')}</span>
      <Button type="button" variant="secondary" appearance="action" onClick={() => void recalculation.refreshCurrent()}>{t('common.retry')}</Button>
    </div> : null}
    <div className={styles.listLabel}>
      <strong>{t('usage_stats.saved_prices')} · {pricing.models.length}</strong>
      <span>{t('usage_stats.pricing_settings_price_unit')}</span>
    </div>
    {pricing.error ? <div className={styles.listError} role="status">
      <span>{pricing.error}</span>
      <Button type="button" variant="secondary" appearance="action" onClick={() => void pricing.loadPricing()}>{t('common.retry')}</Button>
    </div> : null}
    {pricing.loading && !pricing.models.length ? <p className={styles.empty}>{t('common.loading')}</p>
      : pricing.models.length ? <div className={styles.modelList}>
        {pricing.models.map((config) => <article className={styles.modelCard} key={config.model}>
          <div className={styles.modelName}>
            <strong>{config.model}</strong>
            <span>{t(`usage_stats.model_price_style_${config.pricing_style}`)}</span>
          </div>
          <div className={styles.modelPrices}>
            {priceKeys.map((key) => <div key={key}>
              <span>{t(`usage_stats.pricing_settings_${key}`)}</span>
              <strong>${config.base_prices[key]}</strong>
            </div>)}
          </div>
          <div className={styles.modelMeta}>
            <strong>{t('usage_stats.pricing_settings_model_multiplier')} ×{config.model_multiplier}</strong>
            <PricingRuleCounts branches={config.branches.length} conditions={config.conditional_multipliers.length} />
          </div>
          <div className={styles.modelActions}>
            <Button type="button" variant="secondary" appearance="action" disabled={pricingLocked} onClick={() => openEditor(config)}>{t('common.edit')}</Button>
            <Button type="button" variant="ghost" appearance="action" aria-label={`${t('common.delete')} ${config.model}`}
              disabled={pricingLocked}
              className={styles.iconDelete} onClick={() => { setDeleteError(''); setDeleteTarget(config) }}><IconTrash2 size={16} /></Button>
          </div>
        </article>)}
      </div> : <p className={styles.empty}>{t('usage_stats.model_price_empty')}</p>}

    {notice ? <p className={styles.notice} role="status">{notice}</p> : null}
    {recalculation.task?.status === 'failed' ? <p className={styles.listError} role="alert">
      {recalculation.task.error?.message || t('usage_stats.pricing_recalculation_failed')}
      {' '}{t('usage_stats.pricing_recalculation_partial_kept')}
    </p> : null}

    {editorKey > 0 ? <ModelPricingEditor key={editorKey} open={editorOpen} initialConfig={editorConfig}
      modelOptions={availableModels} timezone={timezone} locked={pricingLocked} onClose={() => setEditorOpen(false)} onSave={saveModel} /> : null}
    <CompletePricingSyncModal open={syncOpen} models={pricing.models} locked={pricingLocked} onClose={() => setSyncOpen(false)}
      onRefreshPricing={pricing.loadPricing} onNotice={setNotice} onAuthRequired={onAuthRequired}
      onPricingBusy={() => void recalculation.refreshCurrent()} />
    <PricingRecalculationModal open={recalculationOpen} models={pricing.models} configRevision={pricing.configRevision}
      task={recalculation.task} starting={recalculation.starting} error={recalculation.error}
      errorCode={recalculation.errorCode} connectionError={recalculation.connectionError}
      onStart={recalculation.start} onRefreshCurrent={recalculation.refreshCurrent} onReloadPricing={pricing.loadPricing}
      onClose={() => setRecalculationOpen(false)} onAuthRequired={onAuthRequired} />
    <Modal open={deleteTarget !== null} title={t('usage_stats.pricing_settings_delete_title')}
      onClose={() => { if (!deleting) setDeleteTarget(null) }} closeDisabled={deleting}
      footer={<div className={styles.deleteFooter}>
        <Button type="button" variant="secondary" appearance="action" disabled={deleting} onClick={() => setDeleteTarget(null)}>{t('common.cancel')}</Button>
        <Button type="button" variant="danger" appearance="action" disabled={pricingLocked} loading={deleting} onClick={() => void deleteModel()}>{t('common.delete')}</Button>
      </div>}>
      {deleteTarget ? <p>{t('usage_stats.pricing_settings_delete_body', { model: deleteTarget.model })}</p> : null}
      {deleteError ? <p className={styles.listError} role="alert">{deleteError}</p> : null}
    </Modal>
  </section>
})
