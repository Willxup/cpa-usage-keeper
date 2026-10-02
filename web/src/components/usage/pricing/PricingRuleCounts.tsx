import { Trans } from 'react-i18next'
import styles from './PricingRuleCounts.module.scss'

// 列表与重算预览共用数量配色；零值不强调，翻译保留完整语序。
export function PricingRuleCounts({ branches, conditions }: { branches: number; conditions: number }) {
  return <span className={styles.counts}>
    <Trans i18nKey="usage_stats.pricing_settings_rule_counts" values={{ branches, conditions }} components={{
      branches: <b className={branches > 0 ? styles.branches : styles.zero} />,
      conditions: <b className={conditions > 0 ? styles.conditions : styles.zero} />,
    }} />
  </span>
}
