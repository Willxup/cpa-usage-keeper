import type { ReactNode } from 'react'
import styles from './CredentialList.module.scss'
import shared from '../CredentialSections.module.scss'

export function CredentialRowShell({ icon, title, subtitle, status, metrics, metricsTitle, side, rowClassName, identityId }: {
  icon: ReactNode; title: ReactNode; subtitle?: ReactNode; status?: ReactNode; metrics: ReactNode; metricsTitle?: string;
  side: ReactNode; rowClassName: string; identityId: string
}) {
  return <article className={`${styles.credentialRow} ${rowClassName}`} data-credential-list-row="true" data-credential-identity={identityId}>
    <div className={styles.credentialIdentityBlock}>{icon}<div className={styles.credentialIdentityContent}>
      <div className={styles.credentialNameRow}><div className={styles.credentialNameMain}>
        <span className={styles.credentialDisplayName}>{title}</span>
      </div></div>
      {subtitle && <span className={styles.credentialIdentityText}>{subtitle}</span>}
      {status && <div className={styles.credentialIdentityMeta}>{status}</div>}
    </div></div>
    <div className={styles.credentialMetricGroup} title={metricsTitle}>{metrics}</div>
    <div className={styles.credentialSidePanel}>{side}</div>
  </article>
}
export function CredentialTableHeader({ nameLabel, totalRequestsLabel, successRateLabel, totalTokensLabel, cacheReadRateLabel, sideLabel, rowClassName }: {
  nameLabel: string; totalRequestsLabel: string; successRateLabel: string; totalTokensLabel: string;
  cacheReadRateLabel: string; sideLabel: string; rowClassName: string
}) {
  return <div className={`${styles.credentialTableHeader} ${rowClassName}`}>
    <span className={styles.credentialTableHeaderName}>{nameLabel}</span>
    <div className={styles.credentialMetricHeaderGroup}>
      {[totalRequestsLabel, successRateLabel, totalTokensLabel, cacheReadRateLabel].map((label, index) => <span className={styles.credentialMetricHeaderCell} key={index}>{label}</span>)}
    </div>
    <span className={styles.credentialTableHeaderSide}>{sideLabel}</span>
  </div>
}
export function ListMetric({ value }: { value: ReactNode }) {
  return <span className={shared.credentialMetricValueCell}>{value}</span>
}
