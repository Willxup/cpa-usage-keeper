import { Select } from '@/components/ui/Select'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/Button'
import { Modal } from '@/components/ui/Modal'
import { ApiError, fetchPricingRecalculationOptions } from '@/lib/api'
import type { ModelPricingConfig, PricingRecalculationOptions, PricingRecalculationTask, StartPricingRecalculationResponse } from '@/lib/types'
import { IconInfo, IconChevronDown } from '@/components/ui/icons'
import { buildPricingRecalculationHours } from './pricingRecalculationHours'
import { PricingRuleCounts } from './PricingRuleCounts'
import styles from './PricingRecalculationModal.module.scss'

interface PricingRecalculationModalProps {
  open: boolean
  models: ModelPricingConfig[]
  configRevision: number | null
  task: PricingRecalculationTask | null
  starting: boolean
  error: string
  errorCode: string
  connectionError: string
  onStart: (startAt: string, configRevision: number) => Promise<StartPricingRecalculationResponse | null>
  onRefreshCurrent: () => Promise<PricingRecalculationTask | null>
  onReloadPricing: () => Promise<boolean>
  onClose: () => void
  onAuthRequired?: () => void
}

const stages = ['preparing', 'events', 'updating_stats', 'finalizing'] as const

// 范围按服务端 RFC3339 的本地钟点显示；仅跨 DST 偏移时标出两端偏移以消除歧义。
function formatTaskTime(value: string, showOffset: boolean): string {
  const match = /^(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2}):\d{2}(?:\.\d+)?(Z|[+-]\d{2}:\d{2})$/.exec(value)
  return match ? `${match[1]} ${match[2]}${showOffset ? ` UTC${match[3] === 'Z' ? '+00:00' : match[3]}` : ''}` : value
}

function taskRangeHasDifferentOffsets(start: string, end: string): boolean {
  const offset = (value: string) => /(Z|[+-]\d{2}:\d{2})$/.exec(value)?.[1].replace('Z', '+00:00')
  return offset(start) !== offset(end)
}

// 重算确认只概览已保存基础价与规则数量，不把倍率乘入单价，也不展开编辑器内容。
function SavedModelPricingPreview({ model }: { model: ModelPricingConfig }) {
  const { t } = useTranslation()
  const keys = ['input', 'output', 'cache_read', 'cache_write'] as const
  return <article className={styles.model}>
    <strong>{model.model}</strong>
    <div className={styles.modelPrices}>
      {keys.map((key) => <span key={key}>{t(`usage_stats.pricing_settings_${key}`)}: ${model.base_prices[key]}</span>)}
      <span>{t('usage_stats.pricing_settings_model_multiplier')}: ×{model.model_multiplier}</span>
      <PricingRuleCounts branches={model.branches.length} conditions={model.conditional_multipliers.length} />
    </div>
  </article>
}

// 服务端选项只决定合法 instant；显示和提交均使用 helper 生成的部署时区小时及偏移。
export function PricingRecalculationModal({
  open, models, configRevision, task, starting, error, errorCode, connectionError,
  onStart, onRefreshCurrent, onReloadPricing, onClose, onAuthRequired,
}: PricingRecalculationModalProps) {
  const { t } = useTranslation()
  const [options, setOptions] = useState<PricingRecalculationOptions | null>(null)
  const [optionsLoading, setOptionsLoading] = useState(false)
  const [optionsError, setOptionsError] = useState('')
  const [reloading, setReloading] = useState(false)
  const [requiresReconfirm, setRequiresReconfirm] = useState(false)
  const [selectedDate, setSelectedDate] = useState('')
  const [selectedHour, setSelectedHour] = useState('')
  const [showProgress, setShowProgress] = useState(false)
  const [startAttempted, setStartAttempted] = useState(false)
  const optionsRequestRef = useRef<AbortController | null>(null)
  const busyCheckedRef = useRef(false)
  const hadTaskRef = useRef(false)

  const hours = useMemo(() => options ? buildPricingRecalculationHours(options) : [], [options])
  const dates = useMemo(() => [...new Set(hours.map((hour) => hour.date))], [hours])
  const hoursOnDate = useMemo(() => hours.filter((hour) => hour.date === selectedDate), [hours, selectedDate])
  const currentTask = showProgress ? task : null
  const running = task?.status === 'running'
  const mismatch = Boolean(options && configRevision !== options.config_revision)
  const canStart = Boolean(!starting && !optionsLoading && !reloading && !requiresReconfirm && !mismatch &&
    !optionsError && options && configRevision !== null && selectedHour && !running)

  // 每次打开才读取合法小时；关闭作废旧请求，避免旧范围覆盖再次打开的确认草稿。
  const loadOptions = useCallback(async () => {
    optionsRequestRef.current?.abort()
    const controller = new AbortController()
    optionsRequestRef.current = controller
    setOptionsLoading(true)
    setOptionsError('')
    try {
      const next = await fetchPricingRecalculationOptions(controller.signal)
      if (optionsRequestRef.current !== controller || controller.signal.aborted) return
      const nextHours = buildPricingRecalculationHours(next)
      const latest = nextHours.at(-1)
      setOptions(next)
      setSelectedDate(latest?.date ?? '')
      setSelectedHour(latest?.value ?? '')
    } catch (failure) {
      if (optionsRequestRef.current !== controller || controller.signal.aborted) return
      if (failure instanceof ApiError && failure.status === 401) onAuthRequired?.()
      setOptionsError(failure instanceof Error ? failure.message : t('usage_stats.pricing_recalculation_options_failed'))
      setOptions(null)
    } finally {
      if (optionsRequestRef.current === controller) {
        optionsRequestRef.current = null
        setOptionsLoading(false)
      }
    }
  }, [onAuthRequired, t])

  useEffect(() => {
    if (!open) {
      optionsRequestRef.current?.abort()
      optionsRequestRef.current = null
      setShowProgress(false)
      setStartAttempted(false)
      setRequiresReconfirm(false)
      busyCheckedRef.current = false
      hadTaskRef.current = false
      return
    }
    if (task?.status === 'running') {
      setShowProgress(true)
    } else {
      setShowProgress(false)
      void loadOptions()
    }
    return () => {
      optionsRequestRef.current?.abort()
      optionsRequestRef.current = null
    }
    // 打开动作是确认快照的边界；任务轮询变化不能重置已选日期或重复拉取选项。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  useEffect(() => {
    if (open && task?.status === 'running') setShowProgress(true)
  }, [open, task?.status])

  // 重启后 current 可能由运行任务变为 null；重新取得合法小时才能开始新任务。
  useEffect(() => {
    if (!open) return
    if (task) {
      hadTaskRef.current = true
      return
    }
    if (hadTaskRef.current) {
      hadTaskRef.current = false
      setShowProgress(false)
      void loadOptions()
    }
  }, [loadOptions, open, task])

  // 服务端忙表示可能是另一页面启动；只查唯一 current，不重发 POST。
  useEffect(() => {
    if (!open || !startAttempted || errorCode !== 'pricing_busy' || busyCheckedRef.current) return
    busyCheckedRef.current = true
    void onRefreshCurrent().then((current) => {
      if (current) setShowProgress(true)
    })
  }, [errorCode, onRefreshCurrent, open, startAttempted])

  // 修订冲突后让用户重读完整模型列表及合法小时，再重新确认；绝不自动重试写入。
  useEffect(() => {
    if (open && startAttempted && errorCode === 'pricing_changed') setRequiresReconfirm(true)
  }, [errorCode, open, startAttempted])

  const reloadConfirmation = async () => {
    if (reloading) return
    setReloading(true)
    const refreshed = await onReloadPricing()
    if (refreshed) {
      await loadOptions()
      setRequiresReconfirm(false)
      setStartAttempted(false)
    } else {
      setOptionsError(t('usage_stats.pricing_recalculation_options_failed'))
    }
    setReloading(false)
  }

  const start = async () => {
    if (!canStart || !options) return
    busyCheckedRef.current = false
    setStartAttempted(true)
    const result = await onStart(selectedHour, options.config_revision)
    if (result) setShowProgress(true)
  }

  const progress = currentTask?.stage === 'events' && currentTask.total_count !== null && currentTask.total_count > 0
    ? Math.max(0, Math.min(100, Math.round(currentTask.processed_count / currentTask.total_count * 100))) : null
  const showRangeOffsets = currentTask ? taskRangeHasDifferentOffsets(currentTask.start_at, currentTask.end_at) : false

  return <Modal open={open} width={720} title={t('usage_stats.pricing_recalculation_title')} onClose={onClose}
    closeDisabled={starting || running} className={`${styles.modal} ${starting || running ? styles.runningModal : ''}`} footer={running ? undefined : currentTask ? <div className={styles.footer}><Button type="button" variant="secondary" appearance="action" data-recalculation-close
          onClick={onClose}>{t('usage_stats.pricing_recalculation_return')}</Button></div> : <div className={styles.footer}>
          <Button type="button" variant="secondary" appearance="action" disabled={starting} onClick={onClose}>{t('common.cancel')}</Button>
          <Button type="button" appearance="action" data-recalculation-start disabled={!canStart} loading={starting}
            onClick={() => void start()}>{t('usage_stats.pricing_recalculation_start')}</Button>
        </div>}>
    <div className={styles.body}>
      {currentTask ? <>
        <div className={styles.progressTitle}>
          <h3 role="status">{t(`usage_stats.pricing_recalculation_${currentTask.status === 'running' ? currentTask.stage : currentTask.status}`)}</h3>
          {progress !== null ? <strong role="progressbar" aria-label={t('usage_stats.pricing_recalculation_events_progress')}
            aria-valuemin={0} aria-valuemax={100} aria-valuenow={progress}>{progress}%</strong> : null}
        </div>
        <p className={styles.muted}>{t('usage_stats.pricing_recalculation_all_models')} · {formatTaskTime(currentTask.start_at, showRangeOffsets)} → {formatTaskTime(currentTask.end_at, showRangeOffsets)}</p>
        <ol className={styles.stages}>{stages.map((stage, index) => {
          // 仅依据后端阶段标记完成，不将事件百分比当成整个任务进度。
          const currentIndex = stages.indexOf(currentTask.stage)
          const state = currentTask.status === 'completed' || index < currentIndex ? 'done'
            : index === currentIndex ? currentTask.status === 'failed' ? 'failed' : 'active' : 'pending'
          return <li key={stage} data-state={state} aria-current={state === 'active' || state === 'failed' ? 'step' : undefined}>
            <span className={styles.stageIcon} aria-hidden="true">
              {state === 'done' ? <svg viewBox="0 0 24 24"><path d="m5 12 4 4 10-10" /></svg>
                : state === 'failed' ? <svg viewBox="0 0 24 24"><path d="m7 7 10 10M7 17 17 7" /></svg>
                : state === 'active' ? <span className={styles.stageSpinner} /> : <span className={styles.stageDot} />}
            </span>
            <span>{t(`usage_stats.pricing_recalculation_${stage}`)}</span>
            {index < stages.length - 1 ? <span className={styles.stageConnector} aria-hidden="true" /> : null}
          </li>
        })}</ol>
        <p className={styles.muted}>{currentTask.total_count !== null && currentTask.total_count > 0
          ? t('usage_stats.pricing_recalculation_processed_total', { processed: currentTask.processed_count, total: currentTask.total_count })
          : t('usage_stats.pricing_recalculation_processed', { processed: currentTask.processed_count })}</p>
        {currentTask.status === 'running' ? <p className={styles.impact}><IconInfo size={16} />{t('usage_stats.pricing_recalculation_impact')}</p> : null}
        {currentTask.status === 'completed' ? <p role="status" className={styles.success}>{t('usage_stats.pricing_recalculation_done')}</p> : null}
        {currentTask.status === 'failed' ? <p role="alert" className={styles.failure}>
          {currentTask.error?.message || t('usage_stats.pricing_recalculation_failed')}
          {' '}{t('usage_stats.pricing_recalculation_partial_kept')}</p> : null}
        {connectionError ? <div role="status" className={styles.connection}>
          <span>{t('usage_stats.pricing_recalculation_connection_lost')}</span>
          <Button type="button" variant="secondary" appearance="action" onClick={() => void onRefreshCurrent()}>{t('common.retry')}</Button>
        </div> : null}

      </> : <>
        <p className={styles.intro}>{t('usage_stats.pricing_recalculation_intro')}</p>
        <div className={styles.range}>
          <div className={styles.startFields}>
            <label>{t('usage_stats.pricing_recalculation_start_date')}
              <Select dataAttributes={{ 'data-recalculation-date': true }} value={optionsLoading ? '' : selectedDate} placeholder={optionsLoading ? t('common.loading') : undefined} disabled={!dates.length || optionsLoading || reloading || starting}
                ariaLabel={t('usage_stats.pricing_recalculation_start_date')}
                options={dates.map((date) => ({ value: date, label: date }))}
                onChange={(date) => {
                  setSelectedDate(date)
                  setSelectedHour(hours.filter((hour) => hour.date === date).at(-1)?.value ?? '')
                }} />
            </label>
            <label>{t('usage_stats.pricing_recalculation_start_hour')}
              <Select dataAttributes={{ 'data-recalculation-hour': true }} value={optionsLoading ? '' : selectedHour} placeholder={optionsLoading ? t('common.loading') : undefined} disabled={!hoursOnDate.length || optionsLoading || reloading || starting}
                ariaLabel={t('usage_stats.pricing_recalculation_start_hour')}
                options={hoursOnDate.map((hour) => ({ value: hour.value, label: hour.hourLabel }))}
                onChange={setSelectedHour} />
            </label>
          </div>
          <span className={styles.rangeArrow} aria-hidden="true">→</span>
          <div className={styles.rangeEnd}><strong>{t('usage_stats.pricing_recalculation_until_now')}</strong></div>
          <p className={styles.rangeHint}>{t('usage_stats.pricing_recalculation_recent_days', { days: options?.max_days ?? 30 })}</p>
        </div>
        <p className={styles.impact}><IconInfo size={16} />{t('usage_stats.pricing_recalculation_impact')}</p>
        <details className={styles.savedPricing}>
          <summary>{t('usage_stats.pricing_recalculation_saved_pricing')} <span>{t('usage_stats.pricing_recalculation_model_count', { count: models.length })}</span><IconChevronDown size={16} /></summary>
          <div className={styles.modelList}>
            <p className={styles.muted}>{t('usage_stats.pricing_settings_price_unit')}</p>
            {models.map((model) => <SavedModelPricingPreview key={model.model} model={model} />)}
          </div>
        </details>
        {options && !hours.length ? <p role="status" className={styles.muted}>{t('usage_stats.pricing_recalculation_no_data')}</p> : null}
        {optionsError ? <p role="alert" className={styles.failure}>{optionsError}</p> : null}
        {mismatch || requiresReconfirm ? <div role="alert" className={styles.failure}>
          <span>{t('usage_stats.pricing_recalculation_config_changed')}</span>
          <Button type="button" variant="secondary" appearance="action" data-recalculation-reload disabled={reloading}
            onClick={() => void reloadConfirmation()}>{t('usage_stats.pricing_recalculation_reload')}</Button>
        </div> : null}
        {startAttempted && error && errorCode !== 'pricing_changed' && errorCode !== 'pricing_busy' ? <p role="alert" className={styles.failure}>{error}</p> : null}
        {startAttempted && errorCode === 'pricing_busy' && !task ? <p role="status" className={styles.failure}>{t('usage_stats.pricing_recalculation_busy')}</p> : null}

      </>}
    </div>
  </Modal>
}
