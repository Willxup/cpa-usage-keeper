import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/Button'
import { ApiError, fetchUsageIdentityActivity } from '@/lib/api'
import type { UsageCredentialHealth, UsageIdentityActivityEvent, UsageIdentityActivityResponse } from '@/lib/types'
import { CredentialHealthPanel } from './CredentialHealthPanel'
import { formatCredentialTimestamp } from './credentialViewModels'
import styles from './CredentialDetailDrawer.module.scss'

interface CredentialRecentActivityProps {
  identityId: string
  displayName: string
  disabled: boolean
  health?: UsageCredentialHealth
  enabled: boolean
  onAuthRequired?: () => void
}

type ActivityResult = { key: string; data: UsageIdentityActivityResponse | null; error: boolean }

export function CredentialRecentActivity({ identityId, displayName, disabled, health, enabled, onAuthRequired }: CredentialRecentActivityProps) {
  const { t } = useTranslation()
  const [attempt, setAttempt] = useState(0)
  const [result, setResult] = useState<ActivityResult | null>(null)
  const onAuthRequiredRef = useRef(onAuthRequired)
  const key = JSON.stringify([identityId, attempt])
  useEffect(() => { onAuthRequiredRef.current = onAuthRequired }, [onAuthRequired])
  useEffect(() => {
    if (!enabled) return
    const controller = new AbortController()
    void fetchUsageIdentityActivity(identityId, controller.signal).then(data => {
      if (!controller.signal.aborted) setResult({ key, data, error: false })
    }).catch((error: unknown) => {
      if (controller.signal.aborted) return
      if (error instanceof ApiError && error.status === 401) onAuthRequiredRef.current?.()
      setResult({ key, data: null, error: true })
    })
    return () => controller.abort()
  }, [identityId, key, enabled])
  const current = result?.key === key ? result : null
  const activity = current?.data
  const latest = activity?.latest
  const state = disabled ? 'disabled' : !latest ? 'empty' : !latest.failed ? 'success' : latest.status_code === 429 ? 'limited' : 'failure'
  const tone = state === 'success' ? 'success' : state === 'limited' ? 'warning' : state === 'failure' ? 'danger' : 'quiet'
  const trace = [
    { key: 'success', label: t('usage_stats.credentials_activity_last_success'), event: activity?.last_success },
    { key: 'failure', label: t('usage_stats.credentials_activity_last_failure'), event: activity?.last_failure },
  ].sort((a, b) => Date.parse(a.event?.timestamp ?? '') - Date.parse(b.event?.timestamp ?? '') || 0)

  return <section className={styles.overviewSection} data-credential-overview-section="activity">
    <div className={styles.sectionHeading}>
      <h3>{t('usage_stats.credentials_activity_title')}</h3>
      {activity ? <span className={styles.activityState} data-tone={tone}><i aria-hidden="true" />{t(`usage_stats.credentials_activity_${state}`)}</span> : null}
      {latest ? <time className={styles.latestTime} dateTime={latest.timestamp}>{formatCredentialTimestamp(latest.timestamp) ?? latest.timestamp}</time> : null}
    </div>
    {!current ? <div className={styles.activityMessage} role="status">{t('common.loading')}</div>
      : current.error ? <div className={styles.activityMessage} role="status">
        <span>{t('usage_stats.credentials_activity_load_failed')}</span>
        <Button type="button" variant="secondary" size="sm" onClick={() => setAttempt(value => value + 1)}>{t('common.retry')}</Button>
      </div> : latest && activity ? <div className={styles.activityTrace} aria-label={t('usage_stats.credentials_activity_trace')}>
        {trace.map(fact => <div key={fact.key}><span>{fact.label}</span><ActivityTime event={fact.event} />{fact.event?.status_code && fact.event.status_code >= 400 ? <span className={styles.statusCode} data-tone={fact.event.status_code === 429 ? 'warning' : 'danger'}>HTTP {fact.event.status_code}</span> : null}</div>)}
        <div><span>{t(latest.failed ? 'usage_stats.credentials_activity_failure_streak' : 'usage_stats.credentials_activity_success_streak')}</span><strong>{t(activity.streak_at_least ? 'usage_stats.credentials_activity_at_least' : 'usage_stats.credentials_activity_count', { count: activity.streak })}</strong></div>
      </div> : null}
    <div className={styles.signalRow}>
      <span className={styles.signalLabel}>{t('usage_stats.credentials_activity_health')}</span>
      <div className={styles.healthStrip}><CredentialHealthPanel variant="compact" displayName={displayName} health={health} /></div>
    </div>
  </section>
}

function ActivityTime({ event }: { event: UsageIdentityActivityEvent | null | undefined }) {
  if (!event) return <strong>—</strong>
  const fullTime = formatCredentialTimestamp(event.timestamp) ?? event.timestamp
  const clock = event.timestamp.match(/T(\d{2}:\d{2}:\d{2})/)?.[1] ?? fullTime
  return <time dateTime={event.timestamp} title={fullTime}><strong>{clock}</strong></time>
}
