import { Select } from '@/components/ui/Select'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/Button'
import { Modal } from '@/components/ui/Modal'
import { ApiError, applyPricingSync, fetchPricingModels, fetchPricingSync } from '@/lib/api'
import type { ModelPricingConfig, PricingBasePrices, PricingSyncFetchMatch, PricingSyncFetchResponse, PricingSyncSource } from '@/lib/types'
import { readPricingSyncSource, storePricingSyncSource } from './pricingSyncSource'
import styles from './PricingSyncModal.module.scss'

type PriceKey = keyof PricingBasePrices
const priceKeys: PriceKey[] = ['input', 'output', 'cache_read', 'cache_write']

interface SyncDraft {
  match: PricingSyncFetchMatch
  selected: boolean
  prices: Record<PriceKey, string>
}

interface CompletePricingSyncModalProps {
  open: boolean
  models: ModelPricingConfig[]
  locked?: boolean
  onClose: () => void
  onRefreshPricing: () => Promise<boolean>
  onNotice: (message: string) => void
  onAuthRequired?: () => void
  onPricingBusy?: () => void
}

// 来源四价转换为独立可编辑字符串；未填与明确免费保持区分。
const asDraft = (match: PricingSyncFetchMatch): SyncDraft => ({
  match, selected: true,
  prices: {
    input: String(match.base_prices.input), output: String(match.base_prices.output),
    cache_read: String(match.base_prices.cache_read), cache_write: String(match.base_prices.cache_write),
  },
})

// 保存前仅检查必填和有限非负；完整配置及倍率组合由后端权威校验。
const parsePrice = (raw: string): number | null => {
  if (!raw.trim()) return null
  const value = Number(raw)
  return Number.isFinite(value) && value >= 0 ? value : null
}

// 核对当前四价是否与提交目标一致，不把该比较当作历史事务提交回执。
const samePrices = (left: PricingBasePrices, right: PricingBasePrices): boolean =>
  priceKeys.every((key) => left[key] === right[key])

// 新同步只编辑本次四项基础价；来源草稿与应用状态均只存在当前弹窗中。
export function CompletePricingSyncModal({ open, models, locked = false, onClose, onRefreshPricing, onNotice, onAuthRequired, onPricingBusy }: CompletePricingSyncModalProps) {
  const { t } = useTranslation()
  const [source, setSource] = useState<PricingSyncSource>(readPricingSyncSource)
  const [preview, setPreview] = useState<PricingSyncFetchResponse | null>(null)
  const [drafts, setDrafts] = useState<SyncDraft[]>([])
  const [fetching, setFetching] = useState(false)
  const [applying, setApplying] = useState(false)
  const [fetchError, setFetchError] = useState('')
  const [applyError, setApplyError] = useState('')
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
  const fetchRef = useRef<AbortController | null>(null)
  const fieldToFocus = useRef('')
  const bodyRef = useRef<HTMLDivElement | null>(null)

  // 取消本弹窗的价格拉取，并使已关闭或旧来源的响应失效。
  const invalidateFetch = () => {
    fetchRef.current?.abort()
    fetchRef.current = null
    setFetching(false)
  }

  const clearPreview = () => {
    setPreview(null)
    setDrafts([])
    setFetchError('')
    setApplyError('')
    setFieldErrors({})
  }

  useEffect(() => {
    if (!open) {
      invalidateFetch()
      clearPreview()
    }
    return () => {
      fetchRef.current?.abort()
      fetchRef.current = null
    }
  }, [open])

  useEffect(() => {
    if (!fieldToFocus.current) return
    const path = fieldToFocus.current
    fieldToFocus.current = ''
    const field = [...(bodyRef.current?.querySelectorAll<HTMLInputElement>('[data-sync-price]') ?? [])]
      .find((input) => `${input.dataset.syncIndex}.${input.dataset.syncPrice}` === path)
    field?.focus()
  }, [fieldErrors])

  // 提交开始后不以关闭撤销数据库写入；普通关闭丢弃未提交草稿。
  const close = () => {
    if (applying) return
    invalidateFetch()
    clearPreview()
    onClose()
  }

  // 来源变化只保存偏好并清空旧预览，等待用户再次点击拉取。
  const chooseSource = (value: string) => {
    if (applying || locked) return
    const next: PricingSyncSource = value === 'litellm' ? 'litellm' : 'models-dev'
    invalidateFetch()
    clearPreview()
    setSource(next)
    storePricingSyncSource(next)
  }

  // 仅显式点击拉取创建请求；旧来源或已关闭弹窗的迟到响应由身份与 Abort 双重隔离。
  const loadPreview = async () => {
    if (fetching || applying || locked) return
    invalidateFetch()
    clearPreview()
    const controller = new AbortController()
    fetchRef.current = controller
    setFetching(true)
    try {
      const response = await fetchPricingSync(source, controller.signal)
      if (fetchRef.current !== controller || controller.signal.aborted || response.source !== source) return
      setPreview(response)
      setDrafts(response.matches.map(asDraft))
    } catch (failure) {
      if (fetchRef.current !== controller || controller.signal.aborted) return
      if (failure instanceof ApiError && failure.status === 401) onAuthRequired?.()
      setFetchError(failure instanceof Error ? failure.message : t('usage_stats.pricing_settings_sync_fetch_failed'))
    } finally {
      if (fetchRef.current === controller) {
        fetchRef.current = null
        setFetching(false)
      }
    }
  }

  // 修改一个模型的基础价草稿，清除该输入框反馈，不修改已保存配置。
  const editPrice = (index: number, key: PriceKey, value: string) => {
    setDrafts((current) => current.map((draft, row) => row === index
      ? { ...draft, prices: { ...draft.prices, [key]: value } } : draft))
    setFieldErrors((current) => {
      const next = { ...current }
      delete next[`${index}.${key}`]
      return next
    })
    setApplyError('')
  }

  // 整批失败保留全部草稿；响应丢失只读核对当前价格，不重发 POST 或宣称收到提交回执。
  const apply = async () => {
    if (applying || fetching || locked || !preview) return
    const selected = drafts.map((draft, index) => ({ draft, index })).filter(({ draft }) => draft.selected)
    if (!selected.length) return
    const errors: Record<string, string> = {}
    const items = selected.map(({ draft, index }) => {
      const prices = {} as PricingBasePrices
      for (const key of priceKeys) {
        const value = parsePrice(draft.prices[key])
        if (value === null) errors[`${index}.${key}`] = draft.prices[key].trim() ? 'invalid' : 'required'
        else prices[key] = value
      }
      return { model: draft.match.model, base_prices: prices, pricing_style: draft.match.pricing_style }
    })
    if (Object.keys(errors).length) {
      fieldToFocus.current = Object.keys(errors)[0]
      setFieldErrors(errors)
      setApplyError('')
      return
    }
    setApplying(true)
    setApplyError('')
    setFieldErrors({})
    try {
      await applyPricingSync({ source, items })
      const refreshed = await onRefreshPricing()
      onNotice(refreshed ? t('usage_stats.pricing_settings_sync_applied', { count: items.length })
        : t('usage_stats.pricing_settings_saved_refresh_failed'))
      invalidateFetch()
      clearPreview()
      onClose()
    } catch (failure) {
      if (failure instanceof ApiError && failure.status === 401) onAuthRequired?.()
      if (failure instanceof ApiError && failure.code === 'pricing_busy') onPricingBusy?.()
      if (failure instanceof ApiError && failure.status < 500) {
        const nextErrors: Record<string, string> = {}
        failure.fields?.forEach((field) => {
          const match = /^items\[(\d+)\]\.base_prices(?:\.(input|output|cache_read|cache_write))?$/.exec(field.path)
          if (!match) return
          const row = selected[Number(match[1])]?.index
          if (row === undefined) return
          const keys = match[2] ? [match[2] as PriceKey] : priceKeys
          keys.forEach((key) => { nextErrors[`${row}.${key}`] = field.code })
        })
        fieldToFocus.current = Object.keys(nextErrors)[0] ?? ''
        setFieldErrors(nextErrors)
        setApplyError(Object.keys(nextErrors).length ? '' : failure.message)
        return
      }
      try {
        const current = await fetchPricingModels()
        const matches = items.every((item) => {
          const saved = current.models.find((model) => model.model === item.model)
          return saved && samePrices(saved.base_prices, item.base_prices)
        })
        await onRefreshPricing()
        setApplyError(matches ? t('usage_stats.pricing_settings_sync_current_matches')
          : t('usage_stats.pricing_settings_sync_result_unknown'))
      } catch (readFailure) {
        if (readFailure instanceof ApiError && readFailure.status === 401) onAuthRequired?.()
        setApplyError(t('usage_stats.pricing_settings_sync_result_unknown'))
      }
    } finally {
      setApplying(false)
    }
  }

  const selectedCount = drafts.filter((draft) => draft.selected).length
  return <Modal open={open} width={1120} title={t('usage_stats.pricing_settings_sync_title')}
    closeDisabled={applying} onClose={close} footer={<div className={styles.footer}>
      <Button type="button" appearance="action" variant="secondary" disabled={applying} onClick={close}>{t('common.cancel')}</Button>
      <Button type="button" appearance="action" disabled={locked || applying || fetching || !preview || selectedCount === 0}
        loading={applying} onClick={() => void apply()}>{t('usage_stats.pricing_settings_sync_apply')}{preview ? `（${selectedCount}）` : ''}</Button>
    </div>}>
    <div ref={bodyRef} className={styles.body}>
      <div className={styles.toolbar}>
        <label>{t('usage_stats.pricing_settings_sync_source')}
          <Select className={styles.sourceSelect} fullWidth={false} value={source} dataAttributes={{ 'data-sync-source': true }} disabled={locked || applying} onChange={chooseSource}
            ariaLabel={t('usage_stats.pricing_settings_sync_source')}
            options={[{ value: 'models-dev', label: 'Models.dev' }, { value: 'litellm', label: 'LiteLLM' }]} />
        </label>
        <Button type="button" appearance="action" variant="secondary" disabled={locked || fetching || applying}
          onClick={() => void loadPreview()}>{t('usage_stats.pricing_settings_sync_fetch')}</Button>
      </div>
      <p className={styles.note}>{t('usage_stats.pricing_settings_sync_scope')}</p>
      {preview ? <p className={styles.unit}>{t('usage_stats.pricing_settings_price_unit')}</p> : null}
      {fetchError ? <p role="alert" className={styles.error}>{fetchError}</p> : null}
      {fetching ? <p role="status" className={styles.empty}>{t('usage_stats.pricing_settings_sync_fetching')}</p>
        : !preview ? <p role="status" className={styles.empty}>{t('usage_stats.pricing_settings_sync_empty')}</p> : <>
          <div className={styles.selection}>
            <span>{t('usage_stats.pricing_settings_sync_matches', { matched: drafts.length, selected: selectedCount })}</span>
            <div className={styles.actions}>
              <Button type="button" appearance="action" variant="ghost" disabled={locked || applying}
                onClick={() => setDrafts((current) => current.map((draft) => ({ ...draft, selected: true })))}>
                {t('usage_stats.pricing_settings_sync_select_all')}
              </Button>
              <Button type="button" appearance="action" variant="ghost" disabled={locked || applying}
                onClick={() => setDrafts((current) => current.map((draft) => ({ ...draft, selected: false })))}>
                {t('usage_stats.pricing_settings_sync_clear_selection')}
              </Button>
            </div>
          </div>
          {drafts.length ? <div className={styles.draftList}>{drafts.map((draft, index) => {
            const existing = models.find((model) => model.model === draft.match.model)
            const status = !existing ? 'new' : priceKeys.every((key) => parsePrice(draft.prices[key]) === existing.base_prices[key]) ? 'same' : 'changed'
            return <article key={draft.match.model} className={styles.draft} data-sync-model={draft.match.model}>
              <div className={styles.draftHeader}>
                <label className={styles.checkbox}><input type="checkbox" checked={draft.selected} disabled={locked || applying}
                  onChange={(event) => setDrafts((current) => current.map((row, rowIndex) => rowIndex === index
                    ? { ...row, selected: event.target.checked } : row))} />
                  <strong>{draft.match.model}</strong>
                </label>
                <span className={styles.status}>{t(`usage_stats.pricing_settings_sync_${status}`)}</span>
              </div>
              <p className={styles.match}>{t('usage_stats.pricing_settings_sync_match', {
                model: draft.match.matched_model, provider: draft.match.provider,
              })}</p>
              <div className={styles.priceGrid}>{priceKeys.map((key) => {
                const error = fieldErrors[`${index}.${key}`]
                const errorId = `sync-${index}-${key}-error`
                return <label key={key} className={styles.priceField}>
                  <span>{t(`usage_stats.pricing_settings_${key}`)}</span>
                  <input type="number" min={0} step="any" value={draft.prices[key]} disabled={locked || applying}
                    data-sync-index={index} data-sync-price={key} aria-invalid={Boolean(error)}
                    aria-describedby={error ? errorId : undefined}
                    onChange={(event) => editPrice(index, key, event.target.value)} />
                  <small>{t('usage_stats.pricing_settings_sync_current')}: {existing ? `$${existing.base_prices[key]}` : '—'}</small>
                  {error ? <span id={errorId} className={styles.screenReaderOnly}>
                    {t(`usage_stats.pricing_settings_error_${error === 'required' ? 'required' : 'invalid'}`)}
                  </span> : null}
                </label>
              })}</div>
            </article>
          })}</div> : <p className={styles.empty}>{t('usage_stats.pricing_settings_sync_no_matches')}</p>}
          {preview.unmatched_models.length ? <details className={styles.unmatched}>
            <summary>{t('usage_stats.pricing_settings_sync_unmatched', { count: preview.unmatched_models.length })}</summary>
            <p>{preview.unmatched_models.join(' · ')} · {t('usage_stats.pricing_settings_sync_unmatched_hint')}</p>
          </details> : null}
          {applyError ? <p role="alert" className={styles.error}>{applyError}</p> : null}
        </>}
    </div>
  </Modal>
}
