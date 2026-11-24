import { useTranslation } from 'react-i18next'
import { IconShield, IconChartLine } from '@/components/ui/icons'
import type { CredentialListContentMode } from '../credentialListPreferences'
import { IconChoice } from '../CredentialViewModeSwitch'
import controlStyles from '../CredentialViewControls.module.scss'
import styles from './CredentialList.module.scss'

export function CredentialListContentModeSwitch({ mode, onChange }: { mode: CredentialListContentMode; onChange: (mode: CredentialListContentMode) => void }) {
  const { t } = useTranslation()

  return (
    <div className={styles.credentialDisplayModeControl}>
      <div className={controlStyles.segment} role="group" aria-label={t('usage_stats.credentials_display_mode_aria')}>
        <IconChoice label={t('usage_stats.credentials_display_mode_quota')} selected={mode === 'quota'} onClick={() => onChange('quota')}>
          <IconShield size={16} />
        </IconChoice>
        <IconChoice label={t('usage_stats.credentials_display_mode_health')} selected={mode === 'health'} onClick={() => onChange('health')}>
          <IconChartLine size={16} />
        </IconChoice>
      </div>
    </div>
  )
}

