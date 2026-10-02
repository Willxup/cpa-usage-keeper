import { IconGitBranch } from '@/components/ui/icons'
import { useTranslation } from 'react-i18next'
import { formatCompactTokenValue } from '@/utils/usage'
import type { PricingBasePrices } from '@/lib/types'
import type { PricingBranchDraft } from './pricingBranchDraft'
import styles from './PricingSettings.module.scss'

type PriceKey = keyof PricingBasePrices
const priceKeys: PriceKey[] = ['input', 'output', 'cache_read', 'cache_write']

interface PricingDraftPreviewProps {
  prices: Record<PriceKey, string>
  modelMultiplier: string
  branches: PricingBranchDraft[]
  conditions: Array<{ key: string; value: string; multiplier: string }>
  open: boolean
  onToggle: (open: boolean) => void
  showBranchMatches?: boolean
  branchTimezone?: string
}

// 预览只按模型倍率调整同一草稿的单价；条件倍率仅陈列，不假设某条请求会命中。
function displayedPrice(raw: string, multiplier: string): string {
  if (!raw.trim() || !multiplier.trim()) return '—'
  const amount = Number(raw) * Number(multiplier)
  if (!Number.isFinite(amount) || Number(raw) < 0 || Number(multiplier) < 0) return '—'
  return `$${amount.toLocaleString('en-US', { maximumSignificantDigits: 12 })}`
}

// 分支摘要只展示已选条件，时段标注仅使用调用方传入的部署时区。
export function PricingBranchMatch({ branch, timezone }: { branch: PricingBranchDraft; timezone?: string }) {
  const { t } = useTranslation()
  const context = branch.context.type === 'all' ? t('usage_stats.pricing_settings_context_all')
    : branch.context.type === 'gt' ? `> ${formatCompactTokenValue(Number(branch.context.threshold))} Token`
      : branch.context.type === 'lte' ? `≤ ${formatCompactTokenValue(Number(branch.context.threshold))} Token`
        : `${formatCompactTokenValue(Number(branch.context.min))}–${formatCompactTokenValue(Number(branch.context.max))} Token`
  const period = branch.period.type === 'all' ? t('usage_stats.pricing_settings_period_all')
    : `${branch.period.start}–${branch.period.end}${timezone ? ` · ${timezone}` : ''}`
  const days = t(`usage_stats.pricing_settings_days_${branch.days}`)
  return <div className={styles.branchChips}><span>{context}</span><span>{days}</span><span>{period}</span></div>
}

// 右侧预览与表单共用草稿，折叠状态由模型弹窗持有以便往返分支编辑。
export function PricingDraftPreview({ prices, modelMultiplier, branches, conditions, open, onToggle, showBranchMatches = false, branchTimezone }: PricingDraftPreviewProps) {
  const { t } = useTranslation()
  const plans = [{ id: 'default', name: t('usage_stats.pricing_settings_default_prices'), prices, branch: null as PricingBranchDraft | null },
    ...branches.map((branch) => ({ id: branch.id, name: branch.name || t('usage_stats.pricing_settings_unnamed_branch'), prices: branch.prices, branch }))]

  return <aside className={styles.previewColumn} aria-label={t('usage_stats.pricing_settings_preview')}>
    <details className={styles.previewDetails} data-pricing-preview open={open} onToggle={(event) => onToggle(event.currentTarget.open)}>
      <summary className={styles.conditionSummary}><strong>{t('usage_stats.pricing_settings_preview')}</strong></summary>
      <div className={styles.previewBody}>
        <div className={styles.previewIntro}>
          <span>{t('usage_stats.pricing_settings_price_unit')}</span>
          {modelMultiplier.trim() && Number.isFinite(Number(modelMultiplier)) && Number(modelMultiplier) !== 1
            ? <span>{t('usage_stats.pricing_settings_model_multiplier')} ×{modelMultiplier}</span> : null}
        </div>
        <div aria-live="polite">
          {plans.map((plan) => <section key={plan.id} className={styles.previewPlan} data-preview-plan={plan.id}>
            <h4 className={styles.branchName}>{plan.branch ? <IconGitBranch size={16} aria-hidden="true" /> : null}<span>{plan.name}</span></h4>
            {showBranchMatches && plan.branch ? <PricingBranchMatch branch={plan.branch} timezone={branchTimezone} /> : null}
            <table className={styles.previewTable}>
              <thead><tr><th>{t('usage_stats.pricing_settings_token')}</th><th>{t('usage_stats.pricing_settings_unit_price')}</th></tr></thead>
              <tbody>{priceKeys.map((key) => <tr key={key}>
                <td>{t(`usage_stats.pricing_settings_${key}`)}</td>
                <td><output data-preview-price={key}>{displayedPrice(plan.prices[key], modelMultiplier)}</output></td>
              </tr>)}</tbody>
            </table>
          </section>)}
        </div>
        {conditions.length ? <section className={styles.previewConditions}>
          <h4>{t('usage_stats.pricing_settings_conditions')}</h4>
          {conditions.map((condition, index) => <div key={index} className={styles.previewCondition}>
            <span>{condition.key || '—'} = {condition.value || '—'}</span><strong>×{condition.multiplier || '—'}</strong>
          </div>)}
        </section> : null}
      </div>
    </details>
  </aside>
}
