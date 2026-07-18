import type { CSSProperties, ReactNode } from 'react'
import styles from './CredentialSections.module.scss'
import { formatCompactNumber } from '@/utils/usage'

export type Translate = (key: string, options?: Record<string, unknown>) => string

type CredentialSectionStyle = CSSProperties

interface CredentialSectionShellProps {
  title: string
  subtitle: string
  countLabel: string
  titleExtra?: ReactNode
  actions?: ReactNode
  style?: CredentialSectionStyle
  children: ReactNode
}

interface CredentialRowShellProps {
  title: ReactNode
  subtitle?: ReactNode
  badges: ReactNode
  metrics: ReactNode
  side: ReactNode
  usage?: ReactNode
  rowClassName?: string
  footer?: ReactNode
}

interface CredentialTableHeaderProps {
  nameLabel: string
  totalRequestsLabel: string
  successRateLabel: string
  totalTokensLabel: string
  cacheReadRateLabel: string
  sideLabel: string
  usageLabel?: string
  rowClassName?: string
}

export function CredentialSectionShell({ title, subtitle, countLabel, titleExtra, actions, style, children }: CredentialSectionShellProps) {
  return (
    <section className={styles.credentialSectionCard} style={style}>
      <div className={styles.credentialSectionHeader}>
        <div className={styles.credentialSectionTitleBlock}>
          <div className={styles.credentialSectionTitleRow}>
            <h3 className={styles.credentialSectionTitle}>{title}</h3>
            <span className={styles.credentialCountBadge}>{countLabel}</span>
            {titleExtra}
          </div>
          <p className={styles.credentialSectionSubtitle}>{subtitle}</p>
        </div>
        {actions && <div className={styles.credentialSectionActions}>{actions}</div>}
      </div>
      <div className={styles.credentialRows}>{children}</div>
    </section>
  )
}

export function CredentialRowShell({ title, subtitle, badges, metrics, side, usage, rowClassName, footer }: CredentialRowShellProps) {
  // 统一行结构：左侧身份信息、中间指标、右侧 side（健康/quota），可选 usage 列让中继用量单独成列。
  // footer 是可选的全宽副行（跨所有列），用于追加展开信息。
  return (
    <article className={`${styles.credentialRow} ${rowClassName ?? ''}`.trim()}>
      <div className={styles.credentialIdentityBlock}>
        <div className={styles.credentialNameRow}>
          <span className={styles.credentialDisplayName}>{title}</span>
          {badges && <div className={styles.credentialBadges}>{badges}</div>}
        </div>
        {subtitle && <span className={styles.credentialIdentityText}>{subtitle}</span>}
      </div>
      <div className={styles.credentialMetricGroup}>{metrics}</div>
      <div className={styles.credentialSidePanel}>{side}</div>
      {usage !== undefined && <div className={styles.credentialUsagePanel}>{usage}</div>}
      {footer && <div className={styles.credentialRowFooter}>{footer}</div>}
    </article>
  )
}

export function CredentialTableHeader({ nameLabel, totalRequestsLabel, successRateLabel, totalTokensLabel, cacheReadRateLabel, sideLabel, usageLabel, rowClassName }: CredentialTableHeaderProps) {
  return (
    <div className={`${styles.credentialTableHeader} ${rowClassName ?? ''}`.trim()}>
      <span className={styles.credentialTableHeaderName}>{nameLabel}</span>
      <div className={styles.credentialMetricHeaderGroup}>
        <span className={styles.credentialMetricHeaderCell}>{totalRequestsLabel}</span>
        <span className={styles.credentialMetricHeaderCell}>{successRateLabel}</span>
        <span className={styles.credentialMetricHeaderCell}>{totalTokensLabel}</span>
        <span className={styles.credentialMetricHeaderCell}>{cacheReadRateLabel}</span>
      </div>
      <span className={styles.credentialTableHeaderSide}>{sideLabel}</span>
      {usageLabel && <span className={styles.credentialTableHeaderUsage}>{usageLabel}</span>}
    </div>
  )
}

export function CredentialBadge({ children, tone = 'neutral' }: { children: ReactNode; tone?: 'neutral' | 'success' | 'warning' | 'danger' }) {
  return <span className={`${styles.credentialBadge} ${styles[`credentialBadge${capitalize(tone)}`]}`.trim()}>{children}</span>
}

export function CredentialPriorityBadge({ children }: { children: ReactNode }) {
  return <span className={styles.credentialPriorityBadge}>{children}</span>
}

export function MetricPill({ value }: { value: ReactNode }) {
  return (
    <span className={styles.credentialMetricValueCell}>{value}</span>
  )
}

export function RequestMetric({ total, success, failure }: { total: number; success: number; failure: number }) {
  return (
    <span className={styles.credentialRequestMetric}>
      <strong>{formatCredentialNumber(total)}</strong>
      <span className={styles.credentialRequestBreakdown}>
        (<span className={styles.credentialMetricValueSuccess}>{formatCredentialNumber(success)}</span>/<span className={styles.credentialMetricValueDanger}>{formatCredentialNumber(failure)}</span>)
      </span>
    </span>
  )
}

export function TonePercent({ value, tone }: { value: number | null; tone: 'success' | 'warning' | 'danger' | 'neutral' }) {
  return <span className={credentialToneClassName('credentialMetricValue', tone)}>{formatCredentialPercent(value)}</span>
}

export function successRateTone(value: number | null): 'success' | 'warning' | 'danger' | 'neutral' {
  if (value === null) {
    return 'neutral'
  }
  if (value >= 95) {
    return 'success'
  }
  if (value >= 80) {
    return 'warning'
  }
  return 'danger'
}

export function cacheReadRateTone(value: number | null): 'success' | 'warning' | 'danger' | 'neutral' {
  if (value === null) {
    return 'neutral'
  }
  if (value >= 50) {
    return 'success'
  }
  if (value >= 20) {
    return 'warning'
  }
  return 'neutral'
}

const CREDENTIAL_PAGE_SIZE_OPTIONS = [5, 10, 20, 50]

export function CredentialsPagination({
  leadingControls,
  page,
  total,
  totalPages,
  pageSize,
  sortValue,
  sortOptions,
  sortLabel,
  previousLabel,
  nextLabel,
  rowsPerPageLabel,
  onPageChange,
  onPageSizeChange,
  onSortChange,
}: {
  leadingControls?: ReactNode
  page: number
  total?: number
  totalPages: number
  pageSize: number
  sortValue?: string
  sortOptions?: Array<{ value: string; label: string }>
  sortLabel?: string
  previousLabel: string
  nextLabel: string
  rowsPerPageLabel: string
  onPageChange: (page: number) => void
  onPageSizeChange: (pageSize: number) => void
  onSortChange?: (sort: string) => void
}) {
  if (total === 0) {
    return null
  }

  return (
    <div className={styles.credentialPagination}>
      <div className={styles.credentialPaginationControls}>
        {leadingControls}
        {sortOptions && sortOptions.length > 0 && sortLabel && onSortChange && (
          <label className={styles.credentialPageSizeControl}>
            <span>{sortLabel}</span>
            <select value={sortValue} onChange={(event) => onSortChange(event.target.value)}>
              {sortOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
            </select>
          </label>
        )}
        <label className={styles.credentialPageSizeControl}>
          <span>{rowsPerPageLabel}</span>
          <select value={pageSize} onChange={(event) => onPageSizeChange(Number(event.target.value))}>
            {CREDENTIAL_PAGE_SIZE_OPTIONS.map((option) => <option key={option} value={option}>{option}</option>)}
          </select>
        </label>
        <button type="button" onClick={() => onPageChange(page - 1)} disabled={page <= 1}>{previousLabel}</button>
        <span className={styles.credentialPaginationPage}>{page} / {totalPages}</span>
        <button type="button" onClick={() => onPageChange(page + 1)} disabled={page >= totalPages}>{nextLabel}</button>
      </div>
    </div>
  )
}

export function formatCredentialNumber(value: number): string {
  return formatCompactNumber(value)
}

export function formatCredentialPercent(value: number | null): string {
  if (value === null) {
    return '—'
  }
  return `${value.toFixed(2)}%`
}

export function credentialToneClassName(prefix: string, tone: string): string {
  return styles[`${prefix}${capitalize(tone)}`] ?? ''
}

export function capitalize(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1)
}

export function formatQuotaResetDuration(resetAt: string, t: Translate): string {
  const resetMs = new Date(resetAt).getTime()
  if (!Number.isFinite(resetMs)) {
    return ''
  }
  const remainingMinutes = Math.max(0, Math.ceil((resetMs - Date.now()) / 60_000))
  const days = Math.floor(remainingMinutes / 1_440)
  const hours = Math.floor((remainingMinutes % 1_440) / 60)
  const minutes = remainingMinutes % 60
  const segments: string[] = []
  if (days > 0) {
    segments.push(t('usage_stats.duration_days_short', { value: String(days).padStart(2, '0') }))
  }
  if (hours > 0) {
    segments.push(t('usage_stats.duration_hours_short', { value: String(hours).padStart(2, '0') }))
  }
  if (minutes > 0) {
    segments.push(t('usage_stats.duration_minutes_short', { value: String(minutes).padStart(2, '0') }))
  }
  return segments.slice(0, 2).join('')
}
