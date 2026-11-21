import { useTranslation } from 'react-i18next'
import type { UsageCredentialHealth } from '@/lib/types'
import { CredentialHealthPanel, healthMetaToneClassName, useCredentialHealth } from './CredentialHealthPanel'
import { cacheReadRateTone, credentialToneClassName, formatCredentialNumber, formatCredentialPercent } from './CredentialSectionShell'
import styles from './CredentialDetailDrawer.module.scss'
import credentialStyles from './CredentialSections.module.scss'

interface CredentialRecentActivityProps {
  displayName: string
  health?: UsageCredentialHealth
  windowCacheReadRate: number | null
}

export function CredentialRecentActivity({ displayName, health, windowCacheReadRate }: CredentialRecentActivityProps) {
  const { t } = useTranslation()
  const { summary } = useCredentialHealth(health, t)
  const success = validCount(health?.total_success)
  const failure = validCount(health?.total_failure)
  const requests = success !== null && failure !== null ? success + failure : null
  const cacheRate = health ? windowCacheReadRate : null

  const metrics = [
    { key: 'requests', label: t('usage_stats.credentials_metric_requests'), value: requests === null ? '—' : formatCredentialNumber(requests), tone: 'neutral' },
    { key: 'failures', label: t('usage_stats.credentials_health_fail'), value: failure === null ? '—' : formatCredentialNumber(failure), tone: failure !== null && failure > 0 ? 'danger' : 'neutral' },
    { key: 'cache-rate', label: t('usage_stats.credentials_health_cache_short'), value: formatCredentialPercent(cacheRate), tone: cacheReadRateTone(cacheRate) },
  ]

  return <section className={styles.overviewSection} data-credential-overview-section="activity">
    <div className={styles.sectionHeading}>
      <h3>{t('usage_stats.credentials_activity_title')}</h3>
      {health ? <span className={`${styles.activityState} ${healthMetaToneClassName[summary.tone]}`} data-tone={summary.tone} title={summary.detail}><span className={credentialStyles.credentialHealthMetaStatus}><i aria-hidden="true" />{summary.label}</span></span> : null}
      <span className={styles.activityWindow}>{t('usage_stats.credentials_health_last_5h')}</span>
    </div>
    <div className={styles.activityBody}>
      <div className={styles.healthStrip}>
        <CredentialHealthPanel variant="compact" displayName={displayName} health={health} showWindowLabel={false} missingSnapshotAsUnavailable />
      </div>
      <div className={styles.activityTrace}>
        {metrics.map(metric => <div key={metric.key} data-credential-activity-metric={metric.key}><span>{metric.label}</span><strong className={credentialToneClassName('credentialMetricValue', metric.tone)}>{metric.value}</strong></div>)}
      </div>
    </div>
  </section>
}

function validCount(value: number | undefined): number | null {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : null
}
