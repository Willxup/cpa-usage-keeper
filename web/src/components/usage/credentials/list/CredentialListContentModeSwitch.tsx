import { useTranslation } from 'react-i18next'
import { IconShield, IconChartLine } from '@/components/ui/icons'
import type { CredentialListContentMode } from '../credentialListPreferences'
import styles from './CredentialList.module.scss'

export function CredentialListContentModeSwitch({ mode, onChange }: { mode: CredentialListContentMode; onChange: (mode: CredentialListContentMode) => void }) {
  const { t } = useTranslation()

  return (
    <div className={styles.credentialDisplayModeControl}>
      <div className={styles.credentialDisplayModeSwitcher} role="group" aria-label={t('usage_stats.credentials_display_mode_aria')}>
        <span className={`${styles.credentialDisplayModeThumb} ${mode === 'health' ? styles.credentialDisplayModeThumbHealth : ''}`.trim()} aria-hidden="true" />
        <button
          type="button"
          className={mode === 'quota' ? styles.credentialDisplayModeButtonActive : undefined}
          onClick={() => onChange('quota')}
          aria-pressed={mode === 'quota'}
        >
          <IconShield size={12} />
          <span>{t('usage_stats.credentials_display_mode_quota')}</span>
        </button>
        <button
          type="button"
          className={mode === 'health' ? styles.credentialDisplayModeButtonActive : undefined}
          onClick={() => onChange('health')}
          aria-pressed={mode === 'health'}
        >
          <IconChartLine size={12} />
          <span>{t('usage_stats.credentials_display_mode_health')}</span>
        </button>
      </div>
    </div>
  )
}

