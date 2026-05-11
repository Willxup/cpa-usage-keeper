import type { ReactNode } from 'react'
import styles from './CredentialSections.module.scss'
import { formatCompactNumber } from '@/utils/usage'

interface CredentialSectionShellProps {
  eyebrow: string
  title: string
  subtitle: string
  countLabel: string
  actions?: ReactNode
  children: ReactNode
}

interface CredentialRowShellProps {
  title: string
  subtitle?: ReactNode
  badges: ReactNode
  metrics: ReactNode
  side: ReactNode
}

export function CredentialSectionShell({ eyebrow, title, subtitle, countLabel, actions, children }: CredentialSectionShellProps) {
  return (
    <section className={styles.credentialSectionCard}>
      <div className={styles.credentialSectionHeader}>
        <div className={styles.credentialSectionTitleBlock}>
          <span className={styles.credentialSectionEyebrow}>{eyebrow}</span>
          <div className={styles.credentialSectionTitleRow}>
            <h3 className={styles.credentialSectionTitle}>{title}</h3>
            <span className={styles.credentialCountBadge}>{countLabel}</span>
          </div>
          <p className={styles.credentialSectionSubtitle}>{subtitle}</p>
        </div>
        {actions && <div className={styles.credentialSectionActions}>{actions}</div>}
      </div>
      <div className={styles.credentialRows}>{children}</div>
    </section>
  )
}

export function CredentialRowShell({ title, subtitle, badges, metrics, side }: CredentialRowShellProps) {
  // 统一三段式行结构：左侧身份信息、中间指标、右侧 quota/状态区域。
  return (
    <article className={styles.credentialRow}>
      <div className={styles.credentialIdentityBlock}>
        <div className={styles.credentialNameRow}>
          <span className={styles.credentialDisplayName}>{title}</span>
          {badges && <div className={styles.credentialBadges}>{badges}</div>}
        </div>
        {subtitle && <span className={styles.credentialIdentityText}>{subtitle}</span>}
      </div>
      <div className={styles.credentialMetricGroup}>{metrics}</div>
      <div className={styles.credentialSidePanel}>{side}</div>
    </article>
  )
}

export function CredentialBadge({ children, tone = 'neutral' }: { children: ReactNode; tone?: 'neutral' | 'success' | 'warning' | 'danger' }) {
  return <span className={`${styles.credentialBadge} ${styles[`credentialBadge${capitalize(tone)}`]}`.trim()}>{children}</span>
}

export function MetricPill({ label, value }: { label: string; value: ReactNode }) {
  return (
    <span className={styles.credentialMetricPill}>
      <span className={styles.credentialMetricLabel}>{label}</span>
      <span className={styles.credentialMetricValue}>{value}</span>
    </span>
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

export function cacheRateTone(value: number | null): 'success' | 'warning' | 'danger' | 'neutral' {
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

export function CredentialsPagination({ page, totalPages, previousLabel, nextLabel, onPageChange }: { page: number; totalPages: number; previousLabel: string; nextLabel: string; onPageChange: (page: number) => void }) {
  if (totalPages <= 1) {
    return null
  }

  return (
    <div className={styles.credentialPagination}>
      <button type="button" onClick={() => onPageChange(page - 1)} disabled={page <= 1}>{previousLabel}</button>
      <span>{page} / {totalPages}</span>
      <button type="button" onClick={() => onPageChange(page + 1)} disabled={page >= totalPages}>{nextLabel}</button>
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
  return `${Math.round(value)}%`
}

export function credentialToneClassName(prefix: string, tone: string): string {
  return styles[`${prefix}${capitalize(tone)}`] ?? ''
}

export function capitalize(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1)
}
