import { useTranslation } from 'react-i18next'
import { QuestionMarkHelp } from '@/components/ui/QuestionMarkHelp'
import type { CredentialViewMode } from './credentialListPreferences'
import styles from './CredentialSections.module.scss'

export function CredentialViewHelp({ mode, label, cardDescription, note }: {
  mode: CredentialViewMode
  label: string
  cardDescription: string
  note?: string
}) {
  const { t } = useTranslation()
  const description = mode === 'cards' ? cardDescription : t('usage_stats.credentials_list_help')

  return (
    <QuestionMarkHelp
      label={label}
      description={note ? `${description} ${note}` : description}
      positioning={{
        align: 'center',
        estimatedHeight: 144,
        maxWidth: 280,
        offset: 10,
        viewportPadding: 8,
      }}
    >
      <span>{description}</span>
      {note && <span className={styles.credentialViewHelpNote}>{note}</span>}
    </QuestionMarkHelp>
  )
}
