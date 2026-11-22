import { useTranslation } from 'react-i18next'
import { QuestionMarkHelp } from '@/components/ui/QuestionMarkHelp'
import type { CredentialViewMode } from './credentialListPreferences'

export function CredentialViewHelp({ mode, label, cardDescription }: {
  mode: CredentialViewMode
  label: string
  cardDescription: string
}) {
  const { t } = useTranslation()
  const description = mode === 'cards' ? cardDescription : t('usage_stats.credentials_list_help')

  return (
    <QuestionMarkHelp
      label={label}
      description={description}
      positioning={{
        align: 'center',
        estimatedHeight: 144,
        maxWidth: 280,
        offset: 10,
        viewportPadding: 8,
      }}
    >
      <span>{description}</span>
    </QuestionMarkHelp>
  )
}
