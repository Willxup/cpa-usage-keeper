import { useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import antigravityIcon from '@/assets/icons/antigravity.svg'
import claudeIcon from '@/assets/icons/claude.svg'
import codexIcon from '@/assets/icons/codex.svg'
import geminiIcon from '@/assets/icons/gemini.svg'
import iflowIcon from '@/assets/icons/iflow.svg'
import { IconFilterAll } from '@/components/ui/icons'
import styles from './CredentialSections.module.scss'
import { CREDENTIAL_PROVIDER_FILTER_OPTIONS, buildCredentialProviderFilterCounts, type CredentialProviderFilterKey, type CredentialProviderRowLike } from './credentialProviderFilters'

interface CredentialProviderFilterBarProps {
  rows: CredentialProviderRowLike[]
  value: CredentialProviderFilterKey
  onChange: (value: CredentialProviderFilterKey) => void
}

const providerIconUrls: Partial<Record<CredentialProviderFilterKey, string>> = {
  antigravity: antigravityIcon,
  claude: claudeIcon,
  codex: codexIcon,
  'gemini-cli': geminiIcon,
  iflow: iflowIcon,
}

export function CredentialProviderFilterBar({ rows, value, onChange }: CredentialProviderFilterBarProps) {
  const { t } = useTranslation()
  const counts = useMemo(() => buildCredentialProviderFilterCounts(rows), [rows])
  const visibleOptions = useMemo(
    () => CREDENTIAL_PROVIDER_FILTER_OPTIONS.filter((option) => counts[option.key] > 0),
    [counts],
  )

  useEffect(() => {
    if (value !== 'all' && counts[value] === 0) {
      onChange('all')
    }
  }, [counts, onChange, value])

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
              <CredentialProviderFilterIcon provider={option.key} />
            </span>
            <span className={styles.credentialProviderFilterLabel}>{t(option.labelKey)}</span>
            <span className={styles.credentialProviderFilterCount}>{counts[option.key]}</span>
          </button>
        )
      })}
    </div>
  )
}

function CredentialProviderFilterIcon({ provider }: { provider: CredentialProviderFilterKey }) {
  if (provider === 'all') {
    return <IconFilterAll size={21} />
  }
  const src = providerIconUrls[provider]
  return src ? <img src={src} alt="" aria-hidden="true" /> : null
}
