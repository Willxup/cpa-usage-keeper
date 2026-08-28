import { useTranslation } from 'react-i18next'
import { QuestionMarkHelp } from '@/components/ui/QuestionMarkHelp'
import styles from './PriceRulesModal.module.scss'

const PRICE_RULE_HELP_POSITIONING = {
  align: 'start',
  constrainHeight: true,
  estimatedHeight: 144,
  maxWidth: 420,
  offset: 8,
  viewportPadding: 16,
} as const

export function PriceRulesHelp() {
  const { t } = useTranslation()
  const helpText = t('usage_stats.model_price_rules_help')
  const examplesLabel = t('usage_stats.model_price_rules_help_examples')
  const serviceTierExample = t('usage_stats.model_price_rules_help_service_tier')
  const reasoningEffortExample = t('usage_stats.model_price_rules_help_reasoning_effort')

  return (
    <QuestionMarkHelp
      label={t('usage_stats.model_price_rules_help_label')}
      description={[helpText, examplesLabel, serviceTierExample, reasoningEffortExample].join(' ')}
      className={styles.help}
      tooltipClassName={styles.helpTooltip}
      tooltipVisibleClassName={styles.helpTooltipVisible}
      positioning={PRICE_RULE_HELP_POSITIONING}
    >
      <p>{helpText}</p>
      <span className={styles.helpExamplesLabel}>{examplesLabel}</span>
      <span className={styles.helpExamples}>
        <span>{serviceTierExample}</span>
        <span>{reasoningEffortExample}</span>
      </span>
    </QuestionMarkHelp>
  )
}
