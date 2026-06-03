import { useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import antigravityIcon from '@/assets/icons/antigravity.svg'
import claudeIcon from '@/assets/icons/claude.svg'
import codexIcon from '@/assets/icons/codex.svg'
import geminiIcon from '@/assets/icons/gemini.svg'
import iflowIcon from '@/assets/icons/iflow.svg'
import openaiIcon from '@/assets/icons/openai.svg'
import { IconFilterAll } from '@/components/ui/icons'
import type { UsageIdentityTypeCount } from '@/lib/types'
import styles from './CredentialSections.module.scss'
import { buildCredentialProviderFilterOptions, type CredentialProviderFilterKey, type CredentialProviderFilterScope, type KnownCredentialProviderFilterKey } from './credentialProviderFilters'

interface CredentialProviderFilterBarProps {
  scope: CredentialProviderFilterScope
  typeCounts: UsageIdentityTypeCount[]
  value: CredentialProviderFilterKey
  onChange: (value: CredentialProviderFilterKey) => void
}

const providerIconUrls: Partial<Record<KnownCredentialProviderFilterKey, string>> = {
  antigravity: antigravityIcon,
  claude: claudeIcon,
  codex: codexIcon,
  gemini: geminiIcon,
  'gemini-cli': geminiIcon,
  iflow: iflowIcon,
  openai: openaiIcon,
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
              <CredentialProviderFilterIcon provider={option.knownKey ?? option.key} />
            </span>
            <span className={styles.credentialProviderFilterLabel}>{t(option.labelKey)}</span>
            <span className={styles.credentialProviderFilterCount}>{option.count}</span>
          </button>
        )
      })}
    </div>
  )
}

export function CredentialProviderFilterIcon({ provider }: { provider: string }) {
  if (provider === 'all') {
    return <IconFilterAll size={21} />
  }
  const src = providerIconUrls[provider as KnownCredentialProviderFilterKey]
  return src ? <img src={src} alt="" aria-hidden="true" /> : null
}
