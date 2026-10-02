import { Select } from '@/components/ui/Select'
import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/Button'
import type { PricingBasePrices, PricingPriceBranch } from '@/lib/types'
import { validatePricingBranchDraft, type PricingBranchDraft } from './pricingBranchDraft'
import styles from './PricingSettings.module.scss'
import realtimeStyles from '@/pages/UsagePage.module.scss'

type FieldErrors = Record<string, string>
type PriceKey = keyof PricingBasePrices

const priceKeys: PriceKey[] = ['input', 'output', 'cache_read', 'cache_write']

interface ModelPricingBranchEditorProps {
  formId: string
  initialDraft: PricingBranchDraft
  otherBranches: PricingBranchDraft[]
  mode: 'add' | 'edit' | 'copy'
  timezone?: string
  locked?: boolean
  initialErrors?: FieldErrors
  initialConflictBranchIds?: string[]
  onCancel: () => void
  onSave: (draft: PricingBranchDraft, branch: PricingPriceBranch) => void
}

// 组级冲突落到当前可修改的条件控件，仍保留 helper 给出的原字段合同。
function visibleErrorPath(path: string, draft: PricingBranchDraft): string {
  if (path === 'context') return draft.context.type === 'range' ? 'context.min'
    : draft.context.type === 'all' ? 'context.type' : 'context.threshold'
  if (path === 'period') return draft.period.type === 'window' ? 'period.start' : 'period.type'
  return path
}

// 分支仅编辑本地子草稿；返回或取消不会改动模型配置。
export function ModelPricingBranchEditor({ formId, initialDraft, otherBranches, mode, timezone, locked = false, initialErrors = {}, initialConflictBranchIds = [], onCancel, onSave }: ModelPricingBranchEditorProps) {
  const { t } = useTranslation()
  const formRef = useRef<HTMLFormElement | null>(null)
  const focusPath = useRef(Object.keys(initialErrors)[0] ?? '')
  const [draft, setDraft] = useState(initialDraft)
  const [errors, setErrors] = useState<FieldErrors>(initialErrors)
  const [conflictIds, setConflictIds] = useState<string[]>(initialConflictBranchIds)
  const [shakeAttempt, setShakeAttempt] = useState(Object.keys(initialErrors).length ? 1 : 0)

  useEffect(() => {
    if (!focusPath.current) return
    const path = visibleErrorPath(focusPath.current, draft)
    focusPath.current = ''
    const target = [...(formRef.current?.querySelectorAll<HTMLElement>('[data-pricing-field]') ?? [])]
      .find((element) => element.dataset.pricingField === path)
    target?.focus()
  }, [draft, errors, shakeAttempt])

  const edit = (path: string, value: string) => {
    setDraft((current) => {
      if (path === 'name') return { ...current, name: value }
      if (path === 'days') return { ...current, days: value as PricingBranchDraft['days'] }
      const [group, key] = path.split('.') as ['context' | 'period' | 'prices', string]
      return { ...current, [group]: { ...current[group], [key]: value } }
    })
    setErrors((current) => {
      const next = { ...current }
      delete next[path]
      if (path === 'days' || path.startsWith('period.')) delete next['period.end']
      delete next[path.split('.')[0]]
      return next
    })
    setConflictIds([])
  }

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (locked) return
    const checked = validatePricingBranchDraft(draft, otherBranches)
    if (!checked.branch) {
      setErrors(checked.errors)
      setConflictIds(checked.conflictBranchIds)
      setShakeAttempt((attempt) => attempt + 1)
      focusPath.current = Object.keys(checked.errors)[0] ?? ''
      return
    }
    onSave(draft, checked.branch)
  }

  const field = (path: string, label: string, value: string, inputType = 'text') => {
    const error = errors[path] ?? (path.startsWith('context.') ? errors.context : path.startsWith('period.') ? errors.period : undefined)
    const descriptionId = `${formId}-${path.replace(/[^a-z0-9]/gi, '-')}-error`
    return <label key={path} className={`${styles.field} ${error ? styles.invalid : ''}`}
      data-shake={error && shakeAttempt ? shakeAttempt % 2 ? 'odd' : 'even' : undefined}>
      <span>{label}</span>
      {inputType === 'time' ? <div className={styles.clockFields}>
        <Select value={value.split(':')[0] ?? ''} disabled={locked} ariaLabel={`${label} · HH`}
          dataAttributes={{ 'data-pricing-field': path }} ariaInvalid={Boolean(error)} ariaDescribedBy={error ? descriptionId : undefined}
          options={Array.from({ length: 24 }, (_, hour) => { const text = String(hour).padStart(2, '0'); return { value: text, label: text } })}
          onChange={(hour) => edit(path, `${hour}:${value.split(':')[1] || '00'}`)} />
        <span aria-hidden="true">:</span>
        <Select value={value.split(':')[1] ?? ''} disabled={locked} ariaLabel={`${label} · mm`}
          dataAttributes={{ 'data-pricing-minute': path }} ariaInvalid={Boolean(error)} ariaDescribedBy={error ? descriptionId : undefined}
          options={Array.from({ length: 60 }, (_, minute) => { const text = String(minute).padStart(2, '0'); return { value: text, label: text } })}
          onChange={(minute) => edit(path, `${value.split(':')[0] || '00'}:${minute}`)} />
      </div> : <input type={inputType} min={inputType === 'number' ? 0 : undefined}
        step={inputType === 'number' ? (path.startsWith('context.') ? 1 : 'any') : undefined}
        value={value} onChange={(event) => edit(path, event.target.value)}
        disabled={locked} data-pricing-field={path} aria-invalid={Boolean(error)} aria-describedby={error ? descriptionId : undefined} />}
      {error ? <span id={descriptionId} className={styles.screenReaderOnly}>
        {t(`usage_stats.pricing_settings_error_${['required', 'invalid', 'conflict'].includes(error) ? error : 'invalid'}`)}
      </span> : null}
    </label>
  }

  const select = (path: 'context.type' | 'period.type', label: string, value: string, options: Array<[string, string]>) => {
    const error = errors[path] ?? errors[path.split('.')[0]]
    const descriptionId = `${formId}-${path.replace('.', '-')}-error`
    return <label className={`${styles.field} ${error ? styles.invalid : ''}`}
      data-shake={error && shakeAttempt ? shakeAttempt % 2 ? 'odd' : 'even' : undefined}>
      <span>{label}</span>
      <Select value={value} disabled={locked} onChange={(value) => edit(path, value)} dataAttributes={{ 'data-pricing-field': path }}
        ariaLabel={label} ariaInvalid={Boolean(error)} ariaDescribedBy={error ? descriptionId : undefined}
        options={options.map(([value, label]) => ({ value, label }))} />
      {error ? <span id={descriptionId} className={styles.screenReaderOnly}>
        {t(`usage_stats.pricing_settings_error_${error === 'conflict' ? 'conflict' : 'invalid'}`)}
      </span> : null}
    </label>
  }

  const conflictNames = conflictIds.filter((id) => id !== draft.id)
    .map((id) => otherBranches.find((branch) => branch.id === id)?.name ?? id)

  return <section className={styles.branchEditor} data-pricing-branch-editor>
    <div className={styles.branchEditorHeading}>
      <Button type="button" variant="ghost" appearance="action" aria-label={t('usage_stats.pricing_settings_back_to_model')} onClick={onCancel}>←</Button>
      <h3>{t(`usage_stats.pricing_settings_${mode}_branch_title`)}</h3>
    </div>
    <form id={formId} ref={formRef} noValidate onSubmit={submit} className={styles.branchEditorForm}>
      {field('name', t('usage_stats.pricing_settings_branch_name'), draft.name)}
      <div className={styles.branchEditorGrid}>
        <fieldset className={styles.branchEditorSection}>
          <legend>{t('usage_stats.pricing_settings_branch_match')}</legend>
          {select('context.type', t('usage_stats.pricing_settings_branch_context'), draft.context.type, [
            ['all', t('usage_stats.pricing_settings_context_all')],
            ['gt', t('usage_stats.pricing_settings_context_gt')],
            ['lte', t('usage_stats.pricing_settings_context_lte')],
            ['range', t('usage_stats.pricing_settings_context_range')],
          ])}
          {(draft.context.type === 'gt' || draft.context.type === 'lte')
            ? field('context.threshold', t('usage_stats.pricing_settings_branch_threshold'), draft.context.threshold, 'number') : null}
          {draft.context.type === 'range' ? <div className={styles.branchEditorPair}>
            {field('context.min', t('usage_stats.pricing_settings_branch_min'), draft.context.min, 'number')}
            {field('context.max', t('usage_stats.pricing_settings_branch_max'), draft.context.max, 'number')}
          </div> : null}
          <div className={styles.daysField}>
            <span id={`${formId}-days-label`}>{t('usage_stats.pricing_settings_branch_days')}</span>
            <div className={`${realtimeStyles.overviewRealtimeWindowSwitcher} ${styles.daysSwitcher}`}
              role="group" aria-labelledby={`${formId}-days-label`}>
              {(['all', 'weekday', 'weekend'] as const).map((days) => <button key={days} type="button"
                className={`${realtimeStyles.overviewRealtimeWindowButton} ${draft.days === days ? realtimeStyles.overviewRealtimeWindowButtonActive : ''}`.trim()}
                data-pricing-field="days" aria-pressed={draft.days === days} aria-invalid={Boolean(errors.days)}
                disabled={locked} onClick={() => edit('days', days)}>
                {t(`usage_stats.pricing_settings_days_${days}`)}
              </button>)}
            </div>
            <p className={styles.hint}>{t('usage_stats.pricing_settings_days_hint')}</p>
          </div>
          {select('period.type', t('usage_stats.pricing_settings_branch_period', { timezone: timezone ? ` · ${timezone}` : '' }), draft.period.type, [
            ['all', t('usage_stats.pricing_settings_period_all')],
            ['window', t('usage_stats.pricing_settings_period_window')],
          ])}
          {draft.period.type === 'window' ? <div className={styles.branchEditorPair}>
            {field('period.start', t('usage_stats.pricing_settings_branch_start'), draft.period.start, 'time')}
            {field('period.end', t('usage_stats.pricing_settings_branch_end'), draft.period.end, 'time')}
          </div> : null}
          {errors['period.end'] === 'cross_day' ? <p role="alert" className={styles.requestError}>
            {t('usage_stats.pricing_settings_error_cross_day')}
          </p> : null}
        </fieldset>
        <fieldset className={styles.branchEditorSection}>
          <legend>{t('usage_stats.pricing_settings_branch_prices')}</legend>
          <div className={styles.branchPriceGrid}>{priceKeys.map((key) => field(`prices.${key}`, t(`usage_stats.pricing_settings_${key}`), draft.prices[key], 'number'))}</div>
        </fieldset>
      </div>
      <p className={styles.hint}>{t('usage_stats.pricing_settings_branch_hint')}</p>
      {Object.values(errors).includes('default_conflict') ? <p role="alert" className={styles.requestError}>{t('usage_stats.pricing_settings_error_default_conflict')}</p> : null}
      {conflictNames.length ? <p role="alert" className={styles.requestError}>
        {t('usage_stats.pricing_settings_branch_conflict_with', { names: conflictNames.join(' / ') })}
      </p> : null}

    </form>
  </section>
}
