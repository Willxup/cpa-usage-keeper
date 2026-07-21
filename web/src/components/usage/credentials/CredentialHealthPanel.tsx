import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import type { UsageCredentialHealth } from '@/lib/types'
import { healthGreenThreshold } from '@/utils/usage/health'
import { IconRefreshCw, IconTimer } from '@/components/ui/icons'
import styles from './CredentialSections.module.scss'

type CredentialHealthBucketState = 'success' | 'warning' | 'failure' | 'empty'
type CredentialHealthSummaryTone = 'healthy' | 'degraded' | 'unhealthy' | 'quiet'

interface CredentialHealthBucket {
  state: CredentialHealthBucketState
  heightPx: number
  startLabel: string
  endLabel: string
  successCount: number
  failureCount: number
  successRateLabel: string
}

interface CredentialHealthSummary {
  detail: string
  label: string
  tone: CredentialHealthSummaryTone
}

interface CredentialHealthPanelProps {
  displayName: string
  health?: UsageCredentialHealth
  lastUsedAt?: string
  statsUpdatedAt?: string
}

const HEALTH_WINDOW_MINUTES = 5 * 60
const HEALTH_BUCKET_MINUTES = 10
const HEALTH_BUCKET_COUNT = HEALTH_WINDOW_MINUTES / HEALTH_BUCKET_MINUTES
const HEALTH_EMPTY_HEIGHT_PX = 5
const HEALTH_REQUEST_HEIGHT_MIN_PX = 10
const HEALTH_REQUEST_HEIGHT_RANGE_PX = 12

const healthCellStateClassName: Record<CredentialHealthBucketState, string> = {
  success: styles.credentialHealthCellSuccess,
  warning: styles.credentialHealthCellWarning,
  failure: styles.credentialHealthCellFailure,
  empty: styles.credentialHealthCellEmpty,
}

const healthMetaToneClassName: Record<CredentialHealthSummaryTone, string> = {
  healthy: styles.credentialHealthMetaHealthy,
  degraded: styles.credentialHealthMetaDegraded,
  unhealthy: styles.credentialHealthMetaUnhealthy,
  quiet: styles.credentialHealthMetaQuiet,
}

export function CredentialHealthPanel({ displayName, health, lastUsedAt, statsUpdatedAt }: CredentialHealthPanelProps) {
  const { t } = useTranslation()
  const buckets = useMemo(() => buildHealthBuckets(health), [health])
  const score = resolveCredentialHealthScore(health, buckets)
  const summary = resolveCredentialHealthSummary(buckets, health, t)
  const lastUsed = formatCredentialHealthDate(lastUsedAt)
  const statsUpdated = formatCredentialHealthDate(statsUpdatedAt)

  return (
    <div className={styles.credentialHealthPanel}>
      <div className={styles.credentialHealthChart}>
        <div className={styles.credentialHealthHeader}>
          <span>{t('usage_stats.credentials_health_last_5h')}</span>
          <strong>{score.toFixed(1)}%</strong>
        </div>
        <div
          className={styles.credentialHealthGrid}
          role="list"
          aria-label={t('usage_stats.credentials_health_grid_aria', { name: displayName })}
        >
          {buckets.map((bucket, index) => {
            const timeRange = `${bucket.startLabel} - ${bucket.endLabel}`
            const status = formatHealthBucketLabel(bucket.state, t)
            return (
              <span
                key={`health-${index}`}
                role="listitem"
                className={`${styles.credentialHealthCell} ${healthCellStateClassName[bucket.state]}`.trim()}
                aria-label={t('usage_stats.credentials_health_bucket_aria', {
                  timeRange,
                  status,
                  successCount: bucket.successCount,
                  failureCount: bucket.failureCount,
                  rate: bucket.successRateLabel,
                })}
                style={{ height: `${bucket.heightPx}px` }}
              >
                <span
                  className={`${styles.credentialHealthTooltip} ${
                    index < 4
                      ? styles.credentialHealthTooltipStart
                      : index > buckets.length - 5
                        ? styles.credentialHealthTooltipEnd
                        : ''
                  }`.trim()}
                  role="tooltip"
                  aria-hidden="true"
                >
                  <span className={styles.credentialHealthTooltipTime}>{timeRange}</span>
                  <span className={styles.credentialHealthTooltipStats}>
                    <span className={styles.credentialHealthTooltipSuccess}>{t('usage_stats.credentials_health_ok')} {bucket.successCount}</span>
                    <span className={styles.credentialHealthTooltipFailure}>{t('usage_stats.credentials_health_fail')} {bucket.failureCount}</span>
                    <span>{bucket.successRateLabel}</span>
                  </span>
                </span>
              </span>
            )
          })}
        </div>
      </div>
      <div className={`${styles.credentialHealthMeta} ${healthMetaToneClassName[summary.tone]}`.trim()}>
        <div className={styles.credentialHealthMetaPrimary}>
          <span className={styles.credentialHealthMetaStatus}>{summary.label}</span>
          <span className={styles.credentialHealthMetaDetail}>{summary.detail}</span>
        </div>
        {(lastUsed || statsUpdated) && (
          <div className={styles.credentialHealthMetaTimes}>
            {lastUsed && (
              <span className={styles.credentialHealthMetaTime} title={t('usage_stats.credentials_last_used')} aria-label={`${t('usage_stats.credentials_last_used')} ${lastUsed}`}>
                <IconTimer size={11} className={styles.credentialHealthMetaTimeIcon} />
                <span>{lastUsed}</span>
              </span>
            )}
            {statsUpdated && (
              <span className={styles.credentialHealthMetaTime} title={t('usage_stats.credentials_stats_updated')} aria-label={`${t('usage_stats.credentials_stats_updated')} ${statsUpdated}`}>
                <IconRefreshCw size={11} className={styles.credentialHealthMetaTimeIcon} />
                <span>{statsUpdated}</span>
              </span>
            )}
          </div>
        )}
      </div>
    </div>
  )
}

type CredentialHealthTranslate = (key: string, params?: Record<string, string | number>) => string

function formatHealthBucketLabel(state: CredentialHealthBucketState, t: CredentialHealthTranslate): string {
  switch (state) {
    case 'success':
      return t('usage_stats.credentials_health_status_success')
    case 'warning':
      return t('usage_stats.credentials_health_status_warning')
    case 'failure':
      return t('usage_stats.credentials_health_status_failure')
    default:
      return t('usage_stats.credentials_health_status_empty')
  }
}

function resolveCredentialHealthSummary(buckets: CredentialHealthBucket[], health: UsageCredentialHealth | undefined, t: CredentialHealthTranslate): CredentialHealthSummary {
  const bucketTotals = buckets.reduce((acc, bucket) => ({
    success: acc.success + bucket.successCount,
    failure: acc.failure + bucket.failureCount,
  }), { success: 0, failure: 0 })
  const successCount = finiteNumber(health?.total_success) ?? bucketTotals.success
  const failureCount = finiteNumber(health?.total_failure) ?? bucketTotals.failure
  const latestFailureBucket = findLatestFailureBucket(buckets)
  const detail = latestFailureBucket
    ? t('usage_stats.credentials_health_failures_5h', {
      count: failureCount,
      timeRange: `${latestFailureBucket.startLabel} - ${latestFailureBucket.endLabel}`,
    })
    : t('usage_stats.credentials_health_no_failures_5h')

  const total = successCount + failureCount
  if (total === 0) {
    return {
      tone: 'quiet',
      label: t('usage_stats.credentials_health_summary_quiet'),
      detail: t('usage_stats.credentials_health_no_requests_5h'),
    }
  }
  if (failureCount > successCount) {
    return {
      tone: 'unhealthy',
      label: t('usage_stats.credentials_health_summary_unhealthy'),
      detail,
    }
  }
  if (successCount / total >= healthGreenThreshold(total)) {
    return {
      tone: 'healthy',
      label: t('usage_stats.credentials_health_summary_healthy'),
      detail,
    }
  }
  return {
    tone: 'degraded',
    label: t('usage_stats.credentials_health_summary_degraded'),
    detail,
  }
}

function findLatestFailureBucket(buckets: CredentialHealthBucket[]): CredentialHealthBucket | undefined {
  for (let index = buckets.length - 1; index >= 0; index -= 1) {
    if (buckets[index].failureCount > 0) {
      return buckets[index]
    }
  }
  return undefined
}

function buildEmptyHealthBuckets(now = new Date()): CredentialHealthBucket[] {
  return Array.from({ length: HEALTH_BUCKET_COUNT }, (_, index): CredentialHealthBucket => {
    const state: CredentialHealthBucketState = 'empty'
    const successCount = 0
    const failureCount = 0
    const successRateLabel = '0.0%'
    const end = new Date(now.getTime() - (HEALTH_BUCKET_COUNT - 1 - index) * HEALTH_BUCKET_MINUTES * 60_000)
    const start = new Date(end.getTime() - HEALTH_BUCKET_MINUTES * 60_000)
    const startLabel = formatHealthTime(start)
    const endLabel = formatHealthTime(end)
    return {
      state,
      heightPx: HEALTH_EMPTY_HEIGHT_PX,
      startLabel,
      endLabel,
      successCount,
      failureCount,
      successRateLabel,
    }
  })
}

function buildHealthBuckets(health: UsageCredentialHealth | undefined): CredentialHealthBucket[] {
  if (!health) {
    return buildEmptyHealthBuckets()
  }
  const bucketSeconds = finitePositiveNumber(health.bucket_seconds) ?? HEALTH_BUCKET_MINUTES * 60
  const windowEnd = parseDate(health.window_end)
  const windowStart = parseDate(health.window_start) ?? (windowEnd ? new Date(windowEnd.getTime() - HEALTH_WINDOW_MINUTES * 60_000) : undefined)
  const countsByStart = new Map<number, UsageCredentialHealth['buckets'][number]>()
  for (const bucket of health.buckets ?? []) {
    const bucketStart = parseDate(bucket.start_time)
    if (bucketStart) {
      countsByStart.set(bucketStart.getTime(), bucket)
    }
  }
  if (!windowStart) {
    return buildBucketsFromAPIList(health.buckets ?? [], bucketSeconds)
  }
  return Array.from({ length: HEALTH_BUCKET_COUNT }, (_, index): CredentialHealthBucket => {
    const start = new Date(windowStart.getTime() + index * bucketSeconds * 1000)
    const end = new Date(start.getTime() + bucketSeconds * 1000)
    const source = countsByStart.get(start.getTime())
    const offsetMinutes = Math.round(index * bucketSeconds / 60)
    const endOffsetMinutes = Math.round((index + 1) * bucketSeconds / 60)
    return credentialHealthBucketFromCounts(start, end, source?.success ?? 0, source?.failure ?? 0, {
      startLabel: source
        ? formatHealthTimeFromAPIValue(source.start_time, start)
        : formatHealthTimeFromAPIBase(health.window_start, offsetMinutes, start),
      endLabel: source
        ? formatHealthTimeFromAPIValue(source.end_time, end)
        : formatHealthTimeFromAPIBase(health.window_start, endOffsetMinutes, end),
    })
  })
}

function resolveCredentialHealthScore(health: UsageCredentialHealth | undefined, buckets: CredentialHealthBucket[]): number {
  const apiScore = finiteNumber(health?.success_rate)
  if (apiScore !== undefined) {
    return Math.max(0, Math.min(100, apiScore))
  }
  const totals = buckets.reduce((acc, bucket) => ({
    success: acc.success + bucket.successCount,
    failure: acc.failure + bucket.failureCount,
  }), { success: 0, failure: 0 })
  const total = totals.success + totals.failure
  if (total === 0) {
    return 0
  }
  return Math.round((totals.success / total) * 1000) / 10
}

function buildBucketsFromAPIList(buckets: UsageCredentialHealth['buckets'], bucketSeconds: number): CredentialHealthBucket[] {
  const normalized = buckets.slice(-HEALTH_BUCKET_COUNT).map((bucket) => {
    const start = parseDate(bucket.start_time) ?? new Date()
    const end = parseDate(bucket.end_time) ?? new Date(start.getTime() + bucketSeconds * 1000)
    return credentialHealthBucketFromCounts(start, end, bucket.success, bucket.failure, {
      startLabel: formatHealthTimeFromAPIValue(bucket.start_time, start),
      endLabel: formatHealthTimeFromAPIValue(bucket.end_time, end),
    })
  })
  if (normalized.length >= HEALTH_BUCKET_COUNT) {
    return normalized
  }
  return [...buildEmptyHealthBuckets().slice(0, HEALTH_BUCKET_COUNT - normalized.length), ...normalized]
}

function credentialHealthBucketFromCounts(
  start: Date,
  end: Date,
  success: number,
  failure: number,
  labels?: { startLabel?: string; endLabel?: string },
): CredentialHealthBucket {
  const successCount = safeCount(success)
  const failureCount = safeCount(failure)
  const total = successCount + failureCount
  const bucketRate = total > 0 ? successCount / total : 0
  const state = credentialHealthBucketState(successCount, failureCount, bucketRate)
  const heightPx = credentialHealthBucketHeight(total, bucketRate)
  const startLabel = labels?.startLabel ?? formatHealthTime(start)
  const endLabel = labels?.endLabel ?? formatHealthTime(end)
  const successRateLabel = `${(bucketRate * 100).toFixed(1)}%`
  return {
    state,
    heightPx,
    startLabel,
    endLabel,
    successCount,
    failureCount,
    successRateLabel,
  }
}

function credentialHealthBucketState(successCount: number, failureCount: number, successRate: number): CredentialHealthBucketState {
  const total = successCount + failureCount
  if (total === 0) {
    return 'empty'
  }
  if (failureCount > successCount) {
    return 'failure'
  }
  if (successRate >= healthGreenThreshold(total)) {
    return 'success'
  }
  return 'warning'
}

function credentialHealthBucketHeight(total: number, successRate: number): number {
  if (total === 0) {
    return HEALTH_EMPTY_HEIGHT_PX
  }
  return Math.round((HEALTH_REQUEST_HEIGHT_MIN_PX + successRate * HEALTH_REQUEST_HEIGHT_RANGE_PX) * 10) / 10
}

function parseDate(value: string | undefined): Date | undefined {
  if (!value) {
    return undefined
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return undefined
  }
  return date
}

function finiteNumber(value: number | undefined): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

function finitePositiveNumber(value: number | undefined): number | undefined {
  const numberValue = finiteNumber(value)
  return numberValue !== undefined && numberValue > 0 ? numberValue : undefined
}

function safeCount(value: number): number {
  return Number.isFinite(value) && value > 0 ? Math.floor(value) : 0
}

function formatHealthTime(date: Date): string {
  const hours = String(date.getHours()).padStart(2, '0')
  const minutes = String(date.getMinutes()).padStart(2, '0')
  return `${hours}:${minutes}`
}

// API 已按项目时区写出时间字符串，这里直接读取 HH:mm，避免浏览器时区二次转换。
function formatHealthTimeFromAPIValue(value: string | undefined, fallback: Date): string {
  const minutes = parseAPIClockMinutes(value)
  return minutes === undefined ? formatHealthTime(fallback) : formatClockMinutes(minutes)
}

function formatHealthTimeFromAPIBase(value: string | undefined, offsetMinutes: number, fallback: Date): string {
  const minutes = parseAPIClockMinutes(value)
  return minutes === undefined ? formatHealthTime(fallback) : formatClockMinutes(minutes + offsetMinutes)
}

function parseAPIClockMinutes(value: string | undefined): number | undefined {
  const match = value?.match(/T(\d{2}):(\d{2})/)
  if (!match) {
    return undefined
  }
  return Number(match[1]) * 60 + Number(match[2])
}

function formatClockMinutes(value: number): string {
  const normalized = ((Math.round(value) % 1440) + 1440) % 1440
  const hours = String(Math.floor(normalized / 60)).padStart(2, '0')
  const minutes = String(normalized % 60).padStart(2, '0')
  return `${hours}:${minutes}`
}

function formatCredentialHealthDate(value: string | undefined): string {
  if (!value) {
    return ''
  }
  const apiDateTime = value.match(/^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/)
  if (apiDateTime) {
    return `${apiDateTime[2]}/${apiDateTime[3]} ${apiDateTime[4]}:${apiDateTime[5]}`
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  const hours = String(date.getHours()).padStart(2, '0')
  const minutes = String(date.getMinutes()).padStart(2, '0')
  return `${month}/${day} ${hours}:${minutes}`
}
