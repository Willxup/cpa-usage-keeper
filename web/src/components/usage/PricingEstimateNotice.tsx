import { useTranslation } from 'react-i18next'
import styles from './PricingEstimateNotice.module.scss'

export function PricingEstimateNotice() {
  const { t } = useTranslation()
  return <p className={styles.notice}>{t('usage_stats.pricing_estimate_notice')}</p>
}
