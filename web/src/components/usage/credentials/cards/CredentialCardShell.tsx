import type { ReactNode } from 'react'
import styles from './CredentialCards.module.scss'
import shared from '../CredentialSections.module.scss'

interface CredentialCardShellProps {
  identityId: string
  icon?: ReactNode
  title: ReactNode
  badges?: ReactNode
  status?: ReactNode
  metrics: ReactNode
  metricsTitle?: string
  health: ReactNode
  quota?: ReactNode
  footerLeft?: ReactNode
  actions?: ReactNode
}

export function CredentialCardGrid({ children }: { children: ReactNode }) {
  return <div className={styles.credentialCardGrid}>{children}</div>
}

export function CredentialCardShell({ identityId, icon, title, badges, status, metrics, metricsTitle, health, quota, footerLeft, actions }: CredentialCardShellProps) {
  // 两类身份共用卡片骨架；详情入口与编辑等操作独立，卡片留白不绑定点击。
  return (
    <article className={styles.credentialCard} data-credential-card="true" data-credential-identity={identityId}>
      <div className={styles.credentialCardHeader}>
        <span className={styles.credentialCardBrand} aria-hidden="true">{icon}</span>
        <div className={styles.credentialCardIdentity}>
          <h4 className={styles.credentialCardTitle}>{title}</h4>
          {badges && <div className={shared.credentialIdentityBadges}>{badges}</div>}
        </div>
        {status && <div className={styles.credentialCardStatus}>{status}</div>}
      </div>
      <div className={styles.credentialCardMetrics} title={metricsTitle} role="group" aria-label={metricsTitle}>{metrics}</div>
      {health}
      {quota && <div className={styles.credentialCardQuota}>{quota}</div>}
      <div className={styles.credentialCardFooter}>
        {footerLeft}
        <div className={styles.credentialCardActions}>
          {actions}
        </div>
      </div>
    </article>
  )
}

export function MetricPill({ label, value }: { label?: string; value: ReactNode }) {
  return (
    <span className={styles.credentialCardMetric}>
      {label && <span className={styles.credentialCardMetricLabel}>{label}</span>}
      <span className={shared.credentialMetricValueCell}>{value}</span>
    </span>
  )
}

