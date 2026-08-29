import { useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { IconFilterAll } from '@/components/ui/icons'
import { ProviderBrandIcon } from '@/components/ProviderBrandIcon'
import type { UsageIdentityTypeCount } from '@/lib/types'
import styles from './CredentialSections.module.scss'
import { buildCredentialProviderFilterOptions, type CredentialProviderFilterKey, type CredentialProviderFilterScope } from './credentialProviderFilters'

interface CredentialProviderFilterBarProps {
  scope: CredentialProviderFilterScope
  typeCounts: UsageIdentityTypeCount[]
  value: CredentialProviderFilterKey
  onChange: (value: CredentialProviderFilterKey) => void
}

export function CredentialProviderFilterBar({ scope, typeCounts, value, onChange }: CredentialProviderFilterBarProps) {
  const { t } = useTranslation()
  const visibleOptions = useMemo(() => buildCredentialProviderFilterOptions(scope, typeCounts), [scope, typeCounts])

  useEffect(() => {
    if (value !== 'all' && !visibleOptions.some((option) => option.key === value)) {
      onChange('all')
    }
  }, [onChange, value, visibleOptions])

  if (visibleOptions.length === 0) {
    return null
  }

  return (
    <div className={styles.credentialProviderFilterBar} role="toolbar" aria-label={t('usage_stats.credentials_filter_aria_label')}>
      {visibleOptions.map((option) => {
        const selected = value === option.key
        return (
          <button
            key={option.key}
            type="button"
            className={`${styles.credentialProviderFilterButton} ${selected ? styles.credentialProviderFilterButtonActive : ''}`.trim()}
            aria-pressed={selected}
            onClick={() => onChange(option.key)}
          >
            <span className={styles.credentialProviderFilterIconFrame}>
              {option.key === 'all'
                ? <IconFilterAll size={30} />
                : <ProviderBrandIcon providerType={option.knownKey ?? option.key} size="100%" />}
            </span>
            <span className={styles.credentialProviderFilterLabel}>{t(option.labelKey)}</span>
            <span className={styles.credentialProviderFilterCount}>{option.count}</span>
          </button>
        )
      })}
    </div>
  )
}
