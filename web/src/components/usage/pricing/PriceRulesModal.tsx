import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Modal } from '@/components/ui/Modal'
import type { PricingRule, ReplacePricingRuleInput } from '@/lib/types'
import {
  buildPricingRuleSubmission,
  createPricingRuleDraft,
  pricingRulesToDrafts,
  reconcilePricingRuleDuplicateErrors,
  type PricingRuleDraft,
  type PricingRuleDraftError,
} from './ruleDraft'
import styles from './PriceRulesModal.module.scss'
import { PriceRulesHelp } from './PriceRulesHelp'

export interface PriceRulesModalProps {
  open: boolean
  model: string
  onClose: () => void
  loadRules: (model: string) => Promise<PricingRule[] | null>
  saveRules: (model: string, rules: ReplacePricingRuleInput[]) => Promise<PricingRule[] | null>
  onNotice?: (kind: 'success' | 'info' | 'error', message: string) => void
}

const errorMessageKey = (
  field: keyof PricingRuleDraftError,
  error: PricingRuleDraftError[keyof PricingRuleDraftError],
): string | undefined => {
  if (field === 'key' && error === 'required') return 'usage_stats.model_price_rules_key_required'
  if (field === 'key' && error === 'duplicate') return 'usage_stats.model_price_rules_duplicate'
  if (field === 'value' && error === 'required') return 'usage_stats.model_price_rules_value_required'
  if (field === 'multiplier' && error === 'invalid') return 'usage_stats.model_price_rules_multiplier_invalid'
  return undefined
}

export function PriceRulesModal({
  open,
  model,
  onClose,
  loadRules,
  saveRules,
  onNotice,
}: PriceRulesModalProps) {
  const { t } = useTranslation()
  const [drafts, setDrafts] = useState<PricingRuleDraft[]>([createPricingRuleDraft()])
  const [errors, setErrors] = useState<Record<string, PricingRuleDraftError>>({})
  const [loading, setLoading] = useState(false)
  const [loadFailed, setLoadFailed] = useState(false)
  const [saving, setSaving] = useState(false)
  const [requestErrorKey, setRequestErrorKey] = useState('')
  const [requestErrorDetail, setRequestErrorDetail] = useState('')
  const requestIdentityRef = useRef(0)
  const bodyRef = useRef<HTMLDivElement | null>(null)
  const requestErrorRef = useRef<HTMLDivElement | null>(null)
  const focusValidationErrorRef = useRef(false)
  const focusRequestErrorRef = useRef(false)

  useEffect(() => {
    if (!focusValidationErrorRef.current) return
    focusValidationErrorRef.current = false
    bodyRef.current?.querySelector<HTMLInputElement>('[aria-invalid="true"]')?.focus()
  }, [errors])

  useEffect(() => {
    if (!focusRequestErrorRef.current || (!requestErrorKey && !requestErrorDetail)) return
    // 等异步错误渲染完成后再转移焦点，让键盘和读屏用户立即定位失败原因。
    focusRequestErrorRef.current = false
    requestErrorRef.current?.focus()
  }, [requestErrorDetail, requestErrorKey])

  useEffect(() => {
    setErrors((current) => reconcilePricingRuleDuplicateErrors(drafts, current))
  }, [drafts])

  useEffect(() => {
    const identity = ++requestIdentityRef.current
    if (!open || !model) {
      setLoading(false)
      setLoadFailed(false)
      setSaving(false)
      return
    }

    // 切换模型时立即清空旧草稿，迟到响应只能由相同请求身份写回。
    setDrafts([createPricingRuleDraft()])
    setErrors({})
    setRequestErrorKey('')
    setRequestErrorDetail('')
    setLoadFailed(false)
    setSaving(false)
    setLoading(true)
    void loadRules(model)
      .then((rules) => {
        if (requestIdentityRef.current !== identity || rules === null) return
        setDrafts(pricingRulesToDrafts(rules))
      })
      .catch(() => {
        if (requestIdentityRef.current !== identity) return
        setLoadFailed(true)
        setRequestErrorKey('usage_stats.model_price_rules_load_failed')
      })
      .finally(() => {
        if (requestIdentityRef.current === identity) setLoading(false)
      })
  }, [loadRules, model, open])

  const updateDraft = (id: string, patch: Partial<PricingRuleDraft>) => {
    if (saving) return
    setDrafts((current) => current.map((draft) => (
      draft.id === id ? { ...draft, ...patch } : draft
    )))
    setErrors((current) => {
      if (!(id in current)) return current
      const next = { ...current }
      delete next[id]
      return next
    })
  }

  const removeDraft = (id: string) => {
    if (saving) return
    setDrafts((current) => {
      const next = current.filter((draft) => draft.id !== id)
      return next.length > 0 ? next : [createPricingRuleDraft()]
    })
    setErrors((current) => {
      const next = { ...current }
      delete next[id]
      return next
    })
  }

  const closeModal = () => {
    if (!saving) onClose()
  }

  const handleSave = async () => {
    if (!model || loading || loadFailed || saving) return
    const submission = buildPricingRuleSubmission(drafts)
    focusValidationErrorRef.current = submission.rules === null
    setErrors(submission.errors)
    if (submission.rules === null) return

    const identity = ++requestIdentityRef.current
    setSaving(true)
    setRequestErrorKey('')
    setRequestErrorDetail('')
    try {
      const rules = await saveRules(model, submission.rules)
      if (requestIdentityRef.current !== identity || rules === null) return
      setDrafts(pricingRulesToDrafts(rules))
      onNotice?.('success', t('usage_stats.model_price_rules_save_success'))
      onClose()
    } catch (error) {
      if (requestIdentityRef.current !== identity) return
      const fallback = t('usage_stats.model_price_rules_save_failed')
      const message = error instanceof Error && error.message.trim() ? error.message : fallback
      focusRequestErrorRef.current = true
      setRequestErrorDetail(message)
      onNotice?.('error', message)
    } finally {
      if (requestIdentityRef.current === identity) setSaving(false)
    }
  }

  const title = (
    <div className={styles.title}>
      <span className={styles.titleText}>{t('usage_stats.model_price_rules_title', { model })}</span>
      <PriceRulesHelp />
    </div>
  )

  return (
    <Modal
      open={open}
      title={title}
      className={styles.modal}
      onClose={closeModal}
      closeDisabled={saving}
      width={760}
      footer={
        <div className={styles.footer}>
          <Button type="button" variant="secondary" size="sm" appearance="action" onClick={closeModal} disabled={saving}>
            {t('common.cancel')}
          </Button>
          <Button type="button" variant="primary" size="sm" appearance="action" onClick={() => void handleSave()} loading={saving} disabled={loading || loadFailed}>
            {t('common.save')}
          </Button>
        </div>
      }
    >
      <div ref={bodyRef} className={styles.body}>
        {loading ? (
          <div className={styles.state}>{t('usage_stats.model_price_rules_loading')}</div>
        ) : (
          <>
            {(requestErrorKey || requestErrorDetail) && (
              <div
                ref={requestErrorRef}
                className={styles.error}
                role="alert"
                aria-live="assertive"
                tabIndex={-1}
              >
                {requestErrorDetail || t(requestErrorKey)}
              </div>
            )}
            <div className={styles.rules}>
              {drafts.map((draft) => {
                const rowError = errors[draft.id] ?? {}
                const keyError = errorMessageKey('key', rowError.key)
                const valueError = errorMessageKey('value', rowError.value)
                const multiplierError = errorMessageKey('multiplier', rowError.multiplier)
                return (
                  <div key={draft.id} className={styles.ruleRow}>
                    <Input
                      label={t('usage_stats.model_price_rules_key')}
                      className={styles.ruleInput}
                      value={draft.key}
                      onChange={(event) => updateDraft(draft.id, { key: event.target.value })}
                      error={keyError ? t(keyError) : undefined}
                      disabled={saving}
                      data-rule-field="key"
                    />
                    <Input
                      label={t('usage_stats.model_price_rules_value')}
                      className={styles.ruleInput}
                      value={draft.value}
                      onChange={(event) => updateDraft(draft.id, { value: event.target.value })}
                      error={valueError ? t(valueError) : undefined}
                      disabled={saving}
                      data-rule-field="value"
                    />
                    <Input
                      label={t('usage_stats.model_price_rules_multiplier')}
                      className={styles.ruleInput}
                      type="number"
                      min="0"
                      step="any"
                      placeholder="1"
                      value={draft.multiplier}
                      onChange={(event) => updateDraft(draft.id, { multiplier: event.target.value })}
                      error={multiplierError ? t(multiplierError) : undefined}
                      disabled={saving}
                      data-rule-field="multiplier"
                    />
                    <Button
                      type="button"
                      variant="danger"
                      size="sm"
                      appearance="action"
                      className={styles.removeButton}
                      onClick={() => removeDraft(draft.id)}
                      disabled={saving}
                      aria-label={t('usage_stats.model_price_rules_remove')}
                    >
                      {t('usage_stats.model_price_rules_remove')}
                    </Button>
                  </div>
                )
              })}
            </div>
            <div className={styles.actions}>
              <Button
                type="button"
                variant="secondary"
                size="sm"
                appearance="action"
                onClick={() => setDrafts((current) => [...current, createPricingRuleDraft()])}
                disabled={saving}
              >
                {t('usage_stats.model_price_rules_add')}
              </Button>
            </div>
          </>
        )}
      </div>
    </Modal>
  )
}
