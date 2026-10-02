import { PortalTooltip, usePortalTooltip } from '@/components/ui/PortalTooltip'
import { Select } from '@/components/ui/Select'
import { useCallback, useEffect, useId, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/Button'
import { IconGitBranch, IconPlus, IconTrash2, IconInfo, IconChevronDown } from '@/components/ui/icons'
import { Modal } from '@/components/ui/Modal'
import { ApiError } from '@/lib/api'
import type { ModelPricingConfig, PricingBasePrices, PricingConditionalMultiplier, PricingPriceBranch, PricingStyle } from '@/lib/types'
import { ModelPricingBranchEditor } from './ModelPricingBranchEditor'
import { PricingBranchMatch, PricingDraftPreview } from './PricingDraftPreview'
import { findPricingBranchConflicts, makePricingBranchDraft, validatePricingBranchDraft, type PricingBranchDraft } from './pricingBranchDraft'
import styles from './PricingSettings.module.scss'

// 标题保留两行；鼠标、键盘与触屏均可查看完整模型名称。
function ModelNameTitle({ name }: { name: string }) {
  const { tooltip, showOnMouseEnter, hideOnMouseLeave, showOnFocus, hideOnBlur, dismiss } = usePortalTooltip()
  return <><button type="button" className={styles.modelTitleName}
    onMouseEnter={(event) => showOnMouseEnter([name], event.currentTarget)}
    onMouseLeave={(event) => hideOnMouseLeave(event.currentTarget)}
    onFocus={(event) => showOnFocus([name], event.currentTarget)}
    onBlur={(event) => hideOnBlur(event.currentTarget)}
    onClick={(event) => showOnFocus([name], event.currentTarget)}
    onKeyDown={(event) => { if (event.key === 'Escape' && dismiss()) event.stopPropagation() }}>
    <span>{name}</span></button><PortalTooltip tooltip={tooltip} /></>
}

type PriceKey = keyof PricingBasePrices
type DraftRule = { id: number; key: string; value: string; multiplier: string }
type FieldErrors = Record<string, string>

interface EditorDraft {
  model: string
  pricingStyle: PricingStyle
  prices: Record<PriceKey, string>
  modelMultiplier: string
  rules: DraftRule[]
  branches: PricingBranchDraft[]
}

type BranchSession = { draft: PricingBranchDraft; originalId: string | null; mode: 'add' | 'edit' | 'copy'; errors?: FieldErrors; conflictIds?: string[] }

export interface ModelPricingEditorProps {
  open: boolean
  initialConfig: ModelPricingConfig | null
  modelOptions: string[]
  timezone?: string
  locked?: boolean
  onClose: () => void
  onSave: (config: ModelPricingConfig) => Promise<unknown>
}

const priceKeys: PriceKey[] = ['input', 'output', 'cache_read', 'cache_write']
let nextBranchSerial = 0

// 分支 id 只需在配置内稳定唯一，局域网 HTTP 页面也能创建草稿。
const newBranchId = (branches: PricingBranchDraft[]): string => {
  let id: string
  do {
    id = `branch-${Date.now().toString(36)}-${(++nextBranchSerial).toString(36)}`
  } while (branches.some((branch) => branch.id === id))
  return id
}

// 将已保存数值转为编辑字符串，空白与合法零价分开；已有配置不被草稿修改。
const makeDraft = (config: ModelPricingConfig | null, options: string[]): EditorDraft => ({
  model: config?.model ?? options[0] ?? '',
  pricingStyle: config?.pricing_style ?? 'openai',
  prices: {
    input: config?.base_prices.input.toString() ?? '',
    output: config?.base_prices.output.toString() ?? '',
    cache_read: config?.base_prices.cache_read.toString() ?? '',
    cache_write: config?.base_prices.cache_write.toString() ?? '',
  },
  modelMultiplier: config?.model_multiplier.toString() ?? '1',
  rules: config?.conditional_multipliers.map((rule, id) => ({
    id, key: rule.key, value: rule.value, multiplier: rule.multiplier.toString(),
  })) ?? [],
  branches: config?.branches.map((branch) => makePricingBranchDraft(branch, {
    input: branch.prices.input.toString(), output: branch.prices.output.toString(),
    cache_read: branch.prices.cache_read.toString(), cache_write: branch.prices.cache_write.toString(),
  }, branch.id)) ?? [],
})

// 表单接受有限非负数，空串不按 Number 的隐式规则转成免费。
const finiteNonNegative = (raw: string): number | null => {
  if (!raw.trim()) return null
  const parsed = Number(raw)
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null
}

// 组错误通常来自价格或倍率组合校验，标出组内数值输入并聚焦第一个。
const editableErrorPaths = (path: string, ruleCount: number): string[] => {
  if (path === 'base_prices') return priceKeys.map((key) => `base_prices.${key}`)
  if (path === 'conditional_multipliers') return Array.from({ length: ruleCount }, (_, index) => `conditional_multipliers[${index}].multiplier`)
  const rule = /^conditional_multipliers\[(\d+)\](?:\.(key|value|multiplier))?$/.exec(path)
  if (rule) return Number(rule[1]) < ruleCount ? [`conditional_multipliers[${rule[1]}].${rule[2] ?? 'multiplier'}`] : []
  if (path === 'model' || path === 'pricing_style' || path === 'model_multiplier' ||
    /^base_prices\.(input|output|cache_read|cache_write)$/.test(path)) return [path]
  return []
}

// 校验本地必填与数值后生成完整保存体；权威字段支持及组合费用校验由后端执行。
function validatedConfig(draft: EditorDraft): { config: ModelPricingConfig | null; errors: FieldErrors } {
  const errors: FieldErrors = {}
  if (!draft.model.trim()) errors.model = 'required'
  const prices = {} as PricingBasePrices
  for (const key of priceKeys) {
    const value = finiteNonNegative(draft.prices[key])
    if (value === null) errors[`base_prices.${key}`] = 'invalid'
    else prices[key] = value
  }
  const multiplier = finiteNonNegative(draft.modelMultiplier)
  if (multiplier === null) errors.model_multiplier = 'invalid'
  const rules: PricingConditionalMultiplier[] = []
  const seen = new Set<string>()
  draft.rules.forEach((rule, index) => {
    const key = rule.key.trim().toLowerCase()
    const value = rule.value.trim()
    const factor = finiteNonNegative(rule.multiplier)
    if (!key) errors[`conditional_multipliers[${index}].key`] = 'required'
    if (!value) errors[`conditional_multipliers[${index}].value`] = 'required'
    if (factor === null) errors[`conditional_multipliers[${index}].multiplier`] = 'invalid'
    const pair = `${key}\u0000${value}`
    if (key && value && seen.has(pair)) errors[`conditional_multipliers[${index}].key`] = 'duplicate'
    seen.add(pair)
    if (key && value && factor !== null) rules.push({ key, value, multiplier: factor })
  })
  if (Object.keys(errors).length || multiplier === null) return { config: null, errors }
  return {
    config: {
      model: draft.model.trim(), pricing_style: draft.pricingStyle, base_prices: prices,
      model_multiplier: multiplier, conditional_multipliers: rules,
      branches: [],
    },
    errors,
  }
}

// 单窗编辑默认价格、模型倍率与条件倍率，提交失败才展示字段错误；取消不保存。
export function ModelPricingEditor({ open, initialConfig, modelOptions, timezone, locked = false, onClose, onSave }: ModelPricingEditorProps) {
  const { t } = useTranslation()
  const formId = useId().replaceAll(':', '')
  const formRef = useRef<HTMLFormElement | null>(null)
  const previewInitialized = useRef(false)
  const conditionsRef = useRef<HTMLDetailsElement | null>(null)
  const errorRef = useRef<HTMLParagraphElement | null>(null)
  const nextRuleId = useRef(initialConfig?.conditional_multipliers.length ?? 0)
  const focusPath = useRef<string | null>(null)
  const [draft, setDraft] = useState(() => makeDraft(initialConfig, modelOptions))
  const initialDraft = useRef(JSON.stringify(draft))
  const dirty = JSON.stringify(draft) !== initialDraft.current
  const [errors, setErrors] = useState<FieldErrors>({})
  const [requestError, setRequestError] = useState('')
  const [saving, setSaving] = useState(false)
  const [shakeAttempt, setShakeAttempt] = useState(0)
  const [branchSession, setBranchSession] = useState<BranchSession | null>(null)
  const [conditionsOpen, setConditionsOpen] = useState(true)
  const [branchesOpen, setBranchesOpen] = useState(false)
  const [previewOpen, setPreviewOpen] = useState(false)

  const attachForm = useCallback((node: HTMLFormElement | null) => {
    formRef.current = node
    if (!node || previewInitialized.current) return
    // Modal 延后挂载内容，因此以实际表单节点首次出现时的宽度初始化。
    const computed = window.getComputedStyle(node)
    const availableWidth = node.clientWidth - (parseFloat(computed.paddingLeft) || 0) - (parseFloat(computed.paddingRight) || 0)
    const wide = availableWidth >= 880
    setBranchesOpen(wide)
    setPreviewOpen(wide)
    previewInitialized.current = true
  }, [])

  useEffect(() => {
    if (!focusPath.current) return
    const path = focusPath.current
    focusPath.current = null
    if (path.startsWith('conditional_multipliers')) {
      conditionsRef.current!.open = true
      setConditionsOpen(true)
    }
    const field = [...(formRef.current?.querySelectorAll<HTMLElement>('[data-pricing-field]') ?? [])]
      .find((element) => element.dataset.pricingField === path)
    const target = field ?? errorRef.current
    target?.focus()
  }, [errors, requestError, shakeAttempt])

  const clearError = (path: string) => {
    setErrors((current) => {
      if (!current[path]) return current
      const next = { ...current }
      delete next[path]
      return next
    })
    setRequestError('')
  }

  const editPrice = (key: PriceKey, value: string) => {
    setDraft((current) => ({ ...current, prices: { ...current.prices, [key]: value } }))
    clearError(`base_prices.${key}`)
  }

  const editRule = (id: number, field: 'key' | 'value' | 'multiplier', value: string) => {
    const index = draft.rules.findIndex((rule) => rule.id === id)
    setDraft((current) => ({
      ...current,
      rules: current.rules.map((rule) => rule.id === id ? { ...rule, [field]: value } : rule),
    }))
    if (index >= 0) clearError(`conditional_multipliers[${index}].${field}`)
  }

  // 新增和复制先创建独立子草稿；原分支直到子保存时才被替换。
  const openBranch = (mode: BranchSession['mode'], branch?: PricingBranchDraft) => {
    const id = mode === 'edit' ? branch!.id : newBranchId(draft.branches)
    const source = branch ? { ...branch, context: { ...branch.context }, period: { ...branch.period }, prices: { ...branch.prices } }
      : makePricingBranchDraft(null, draft.prices, id)
    const branchDraft = { ...source, id, name: mode === 'copy' ? `${source.name} ${t('usage_stats.pricing_settings_copy_suffix')}` : source.name }
    setBranchSession({ draft: branchDraft, originalId: mode === 'edit' ? branch!.id : null, mode })
  }

  const saveBranch = (branchDraft: PricingBranchDraft) => {
    // 子保存只写模型草稿；离开弹窗前不会向后端提交配置。
    setDraft((current) => ({
      ...current,
      branches: branchSession?.originalId
        ? current.branches.map((branch) => branch.id === branchSession.originalId ? branchDraft : branch)
        : [...current.branches, branchDraft],
    }))
    setBranchSession(null)
  }

  // 完整保存发现分支错误时回到对应子编辑，保持其余模型草稿。
  const openInvalidBranch = (index: number, branchErrors: FieldErrors, conflictIds: string[] = []) => {
    const branch = draft.branches[index]
    if (!branch) return
    setBranchSession({ draft: branch, originalId: branch.id, mode: 'edit', errors: branchErrors, conflictIds })
  }

  // 先定位草稿错误，再提交一次完整配置；服务端字段错误映射回控件，网络错误保留草稿。
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (saving || locked) return
    const validated = validatedConfig(draft)
    if (!validated.config) {
      setRequestError('')
      setErrors(validated.errors)
      setShakeAttempt((attempt) => attempt + 1)
      focusPath.current = Object.keys(validated.errors)[0] ?? null
      return
    }
    const parsedBranches: PricingPriceBranch[] = []
    for (const [index, branch] of draft.branches.entries()) {
      const checked = validatePricingBranchDraft(branch, [])
      if (!checked.branch) {
        openInvalidBranch(index, checked.errors, checked.conflictBranchIds)
        return
      }
      parsedBranches.push(checked.branch)
    }
    const conflicts = findPricingBranchConflicts(parsedBranches)
    if (conflicts.length) {
      const conflict = conflicts[0]
      openInvalidBranch(conflict.indices[0], { context: 'conflict', period: 'conflict' }, conflict.branchIds)
      return
    }
    validated.config.branches = parsedBranches
    // 保存只提交完整配置；写入成功后的列表刷新失败由 hook 单独报告。
    setSaving(true)
    setRequestError('')
    try {
      await onSave(validated.config)
      onClose()
    } catch (failure) {
      if (failure instanceof ApiError) {
        const branchField = failure.fields?.find((field) => /^branches\[(\d+)\]/.test(field.path))
        const match = branchField && /^branches\[(\d+)\](?:\.(.+))?$/.exec(branchField.path)
        if (match && Number(match[1]) < draft.branches.length) {
          const path = match[2] ?? 'context'
          const branchErrors = path === 'prices'
            ? Object.fromEntries(priceKeys.map((key) => [`prices.${key}`, branchField.code]))
            : { [path]: branchField.code }
          openInvalidBranch(Number(match[1]), branchErrors, branchField.branch_ids ?? [])
          return
        }
      }
      const fieldErrors: FieldErrors = {}
      let unmapped = false
      if (failure instanceof ApiError) {
        failure.fields?.forEach((field) => {
          const paths = editableErrorPaths(field.path, draft.rules.length)
          if (paths.length) paths.forEach((path) => { fieldErrors[path] = field.code })
          else unmapped = true
        })
      }
      setErrors(fieldErrors)
      setShakeAttempt((attempt) => attempt + 1)
      focusPath.current = Object.keys(fieldErrors)[0] ?? null
      const conflict = failure instanceof ApiError ? failure.fields?.find((field) => field.branch_ids?.length) : null
      const names = conflict?.branch_ids?.map((id) => draft.branches.find((branch) => branch.id === id)?.name ?? id)
      const showSummary = !Object.keys(fieldErrors).length || unmapped || Boolean(names?.length)
      setRequestError(showSummary
        ? `${failure instanceof Error ? failure.message : t('usage_stats.pricing_settings_save_failed')}${names?.length ? ` · ${names.join(' / ')}` : ''}`
        : '')
      if (!Object.keys(fieldErrors).length) focusPath.current = 'request-error'
    } finally {
      setSaving(false)
    }
  }

  const field = (path: string, label: string, value: string, onChange: (value: string) => void, type = 'text') => {
    const error = errors[path]
    const descriptionId = `${formId}-${path.replace(/[^a-z0-9]/gi, '-')}-error`
    return <label key={path} className={`${styles.field} ${error ? styles.invalid : ''}`}
      data-shake={error && shakeAttempt ? shakeAttempt % 2 ? 'odd' : 'even' : undefined}>
      <span>{label}</span>
      <input type={type} min={type === 'number' ? 0 : undefined} step={type === 'number' ? 'any' : undefined}
        value={value} onChange={(event) => onChange(event.target.value)} disabled={saving || locked}
        data-pricing-field={path} aria-invalid={Boolean(error)}
        aria-describedby={error ? descriptionId : undefined} />
      {error ? <span id={descriptionId} className={styles.screenReaderOnly}>
        {t(`usage_stats.pricing_settings_error_${['required', 'invalid', 'duplicate', 'conflict'].includes(error) ? error : 'invalid'}`)}
      </span> : null}
    </label>
  }

  return <Modal open={open} title={<span className={styles.editorTitle}>{initialConfig ? <ModelNameTitle name={initialConfig.model} /> : t('usage_stats.pricing_settings_editor_title')}{dirty ? <span className={styles.dirtyBadge}>{t('usage_stats.pricing_settings_unsaved')}</span> : null}</span>}
    width={1120} className={styles.editorModal} closeDisabled={saving} onClose={onClose}
    footer={branchSession ? <div className={styles.editorFooter}>
      <Button type="button" variant="secondary" appearance="action" onClick={() => setBranchSession(null)}>{t('common.cancel')}</Button>
      <Button type="submit" form={`${formId}-branch`} appearance="action" disabled={locked}>{t('usage_stats.pricing_settings_save_branch')}</Button>
    </div> : <div className={styles.editorFooter}>
      <p className={styles.historyNotice}><IconInfo size={16} />{t('usage_stats.pricing_settings_history_notice')}</p>
        <div className={styles.footerActions}>
        <Button type="button" variant="secondary" appearance="action" disabled={saving} onClick={onClose}>{t('common.cancel')}</Button>
        <Button type="submit" form={formId} appearance="action" disabled={locked} loading={saving}>{t('common.save')}</Button>
        </div>
      </div>}>
    {branchSession ? <ModelPricingBranchEditor formId={`${formId}-branch`} key={branchSession.draft.id} initialDraft={branchSession.draft}
      otherBranches={draft.branches.filter((branch) => branch.id !== branchSession.originalId)}
      mode={branchSession.mode} timezone={timezone} locked={locked} initialErrors={branchSession.errors}
      initialConflictBranchIds={branchSession.conflictIds}
      onCancel={() => setBranchSession(null)} onSave={(branchDraft) => saveBranch(branchDraft)} />
      : <form id={formId} ref={attachForm} className={styles.editorBody} noValidate onSubmit={(event) => void submit(event)}>
      <div className={styles.settingsLayout}>
      <div className={styles.configColumn}>
      <div className={styles.modelHeading}>
      {!initialConfig ? <label className={`${styles.modelField} ${errors.model ? styles.invalid : ''}`}
        data-shake={errors.model && shakeAttempt ? shakeAttempt % 2 ? 'odd' : 'even' : undefined}>
        <span>{t('usage_stats.pricing_settings_model')}</span>
        <Select value={draft.model} disabled={saving || locked} dataAttributes={{ 'data-pricing-field': 'model' }} ariaInvalid={Boolean(errors.model)}
          ariaLabel={t('usage_stats.pricing_settings_model')} ariaDescribedBy={errors.model ? `${formId}-model-error` : undefined}
          options={modelOptions.map((model) => ({ value: model, label: model }))}
          onChange={(model) => {
            setDraft((current) => ({ ...current, model }))
            setErrors({}); setRequestError(''); setShakeAttempt(0); focusPath.current = null
          }} />
        {errors.model ? <span id={`${formId}-model-error`} className={styles.screenReaderOnly}>
          {t(`usage_stats.pricing_settings_error_${errors.model === 'required' ? 'required' : 'invalid'}`)}
        </span> : null}
      </label> : <h3>{t('usage_stats.pricing_settings_default_prices')}</h3>}
        <label className={`${styles.styleField} ${errors.pricing_style ? styles.invalid : ''}`}>{t('usage_stats.model_price_style')}
          <Select value={draft.pricingStyle} disabled={saving || locked} dataAttributes={{ 'data-pricing-field': 'pricing_style' }} ariaInvalid={Boolean(errors.pricing_style)}
            ariaLabel={t('usage_stats.model_price_style')} ariaDescribedBy={errors.pricing_style ? `${formId}-pricing-style-error` : undefined}
            options={['openai', 'claude'].map((value) => ({ value, label: t(`usage_stats.model_price_style_${value}`) }))}
            onChange={(value) => { setDraft((current) => ({ ...current, pricingStyle: value as PricingStyle })); clearError('pricing_style') }} />
          {errors.pricing_style ? <span id={`${formId}-pricing-style-error`} className={styles.screenReaderOnly}>
            {t('usage_stats.pricing_settings_error_invalid')}
          </span> : null}
        </label>
      </div>
      {!initialConfig ? <div className={styles.sectionHeading}><h3>{t('usage_stats.pricing_settings_default_prices')}</h3></div> : null}
      <div className={styles.priceGrid}>
        {priceKeys.map((key) => field(`base_prices.${key}`, t(`usage_stats.pricing_settings_${key}`), draft.prices[key], (value) => editPrice(key, value), 'number'))}
      </div>
      <div className={styles.multiplierRow}>
        {field('model_multiplier', t('usage_stats.pricing_settings_model_multiplier'), draft.modelMultiplier,
          (value) => { setDraft((current) => ({ ...current, modelMultiplier: value })); clearError('model_multiplier') }, 'number')}
        <span>{t('usage_stats.pricing_settings_multiplier_hint')}</span>
      </div>

      <details className={styles.branchSection} data-pricing-branches open={branchesOpen}
        onToggle={(event) => setBranchesOpen(event.currentTarget.open)}>
        <summary className={styles.conditionSummary}>
          <strong>{t('usage_stats.pricing_settings_branches')}</strong><span>{draft.branches.length}</span><IconChevronDown size={16} />
        </summary>
        <div className={styles.branchBody}>
          {draft.branches.length ? draft.branches.map((branch) => <article key={branch.id} className={styles.branchCard} data-pricing-branch-id={branch.id}>
            <div className={styles.branchIdentity}>
              <strong className={styles.branchName}><IconGitBranch size={16} aria-hidden="true" /><span>{branch.name || t('usage_stats.pricing_settings_unnamed_branch')}</span></strong>
              <PricingBranchMatch branch={branch} timezone={timezone} />
            </div>
            <div className={styles.branchRates}>{priceKeys.map((key) => <span key={key}>
              {t(`usage_stats.pricing_settings_${key}`)}<strong>${branch.prices[key] || '—'}</strong>
            </span>)}</div>
            <div className={styles.branchActions}>
              <Button type="button" variant="secondary" appearance="action" disabled={saving || locked} data-branch-action="edit"
                aria-label={t('usage_stats.pricing_settings_edit_branch_named', { name: branch.name })}
                onClick={() => openBranch('edit', branch)}>{t('common.edit')}</Button>
              <Button type="button" variant="secondary" appearance="action" disabled={saving || locked} data-branch-action="copy"
                aria-label={t('usage_stats.pricing_settings_copy_branch_named', { name: branch.name })}
                onClick={() => openBranch('copy', branch)}>{t('usage_stats.pricing_settings_copy_branch')}</Button>
              <Button type="button" variant="ghost" appearance="action" disabled={saving || locked} data-branch-action="delete" className={styles.iconDelete}
                aria-label={t('usage_stats.pricing_settings_delete_branch_named', { name: branch.name })}
                onClick={() => { setDraft((current) => ({ ...current, branches: current.branches.filter((item) => item.id !== branch.id) })); setRequestError('') }}>
                <IconTrash2 size={16} />
              </Button>
            </div>
          </article>) : null}
          <div className={styles.conditionFooter}>
            <p className={styles.hint}>{t('usage_stats.pricing_settings_branches_hint')}</p>
            <Button type="button" appearance="action" disabled={saving || locked}
              onClick={() => openBranch('add')}><IconPlus size={16} />{t('usage_stats.pricing_settings_add_branch')}</Button>
          </div>
        </div>
      </details>

      <details ref={conditionsRef} className={styles.conditionSection} data-pricing-conditions open={conditionsOpen}
        onToggle={(event) => setConditionsOpen(event.currentTarget.open)}>
        <summary className={styles.conditionSummary}>
          <strong>{t('usage_stats.pricing_settings_conditions')}</strong>
          <span>{draft.rules.length}</span><IconChevronDown size={16} />
        </summary>
        <div className={styles.conditionBody}>
        {draft.rules.map((rule, index) => <div className={styles.conditionRow} key={rule.id}>
          {field(`conditional_multipliers[${index}].key`, t('usage_stats.model_price_rules_key'), rule.key,
            (value) => editRule(rule.id, 'key', value))}
          {field(`conditional_multipliers[${index}].value`, t('usage_stats.model_price_rules_value'), rule.value,
            (value) => editRule(rule.id, 'value', value))}
          {field(`conditional_multipliers[${index}].multiplier`, t('usage_stats.model_price_rules_multiplier'), rule.multiplier,
            (value) => editRule(rule.id, 'multiplier', value), 'number')}
          <Button type="button" variant="ghost" appearance="action" className={styles.iconDelete} disabled={saving || locked} aria-label={t('usage_stats.pricing_settings_delete_condition')}
            onClick={() => { setDraft((current) => ({ ...current, rules: current.rules.filter((item) => item.id !== rule.id) })); setErrors({}) }}>
            <IconTrash2 size={16} />
          </Button>
        </div>)}
        <div className={styles.conditionFooter}>
        <p className={styles.hint}>{t('usage_stats.pricing_settings_conditions_hint')}</p>
        <Button type="button" appearance="action" disabled={saving || locked}
          onClick={() => setDraft((current) => ({ ...current, rules: [...current.rules, { id: nextRuleId.current++, key: '', value: '', multiplier: '1' }] }))}>
          <IconPlus size={16} />{t('usage_stats.pricing_settings_add_condition')}
        </Button>
        </div>
        </div>
      </details>


      </div>
      <PricingDraftPreview prices={draft.prices} modelMultiplier={draft.modelMultiplier} branches={draft.branches}
        conditions={draft.rules} open={previewOpen} onToggle={setPreviewOpen} showBranchMatches branchTimezone={timezone} />
      </div>
      {requestError ? <p ref={errorRef} tabIndex={-1} className={styles.requestError} role="alert" data-pricing-field="request-error">{requestError}</p> : null}

    </form>
    }
  </Modal>
}
