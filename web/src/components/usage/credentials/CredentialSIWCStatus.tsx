import { useTranslation } from 'react-i18next'
import type { CredentialSIWCState } from './credentialViewModels'
import styles from './CredentialSIWCStatus.module.scss'

export function CredentialSIWCStatus({ state, className }: { state: CredentialSIWCState; className?: string }) {
  const { t } = useTranslation()
  return (
    <span className={`${styles.credentialSIWCStatus} ${className ?? ''}`.trim()} data-siwc-state={state}>
      <span className={styles.credentialSIWCDot} aria-hidden="true" />
      <span>SIWC {t(`usage_stats.credentials_siwc_${state}`)}</span>
    </span>
  )
}
