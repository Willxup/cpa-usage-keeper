import { useId, type ReactNode } from 'react'
import styles from '../quota/CredentialQuota.module.scss'

// 卡片动作沿用重置提示的外观，不改变按钮本身的点击或回焦行为。
export function CredentialCardActionTooltip({ label, children }: { label: string; children: (tooltipId: string) => ReactNode }) {
  const tooltipId = useId()
  return (
    <span className={styles.credentialCardActionTooltip}>
      {children(tooltipId)}
      <span id={tooltipId} className={styles.credentialQuotaResetTooltip} role="tooltip">{label}</span>
    </span>
  )
}
