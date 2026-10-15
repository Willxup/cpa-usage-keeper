import { useTranslation } from 'react-i18next'
import styles from './CredentialSections.module.scss'

export function kimiCredentialSite(identityType: string | null | undefined): 'china' | 'international' | undefined {
  switch (identityType?.trim().toLowerCase()) {
    case 'kimi':
    case 'kimi.com': return 'china'
    case 'kimi-ai':
    case 'kimi.ai': return 'international'
    default: return undefined
  }
}

export function CredentialKimiSiteBadge({ identityType }: { identityType: string | null | undefined }) {
  const { t } = useTranslation()
  const site = kimiCredentialSite(identityType)
  if (!site) return null

  return (
    <span
      className={`${styles.credentialKimiSiteBadge} ${site === 'china' ? styles.credentialKimiSiteBadgeChina : styles.credentialKimiSiteBadgeInternational}`}
      data-kimi-site={site}
    >
      {t(`usage_stats.credentials_kimi_site_${site}`)}
    </span>
  )
}
