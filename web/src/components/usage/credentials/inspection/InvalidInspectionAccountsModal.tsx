import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/Button'
import { Modal } from '@/components/ui/Modal'
import { deleteCredentials, setCredentialsDisabled } from '@/lib/api'
import styles from './QuotaInspection.module.scss'
import shared from '../CredentialSections.module.scss'
import { selectAllInvalidInspectionAccountFileNames, invertInvalidInspectionAccountFileNames } from './quotaInspectionModel'

export type InvalidInspectionAccountSession = { action: 'disable' | 'delete'; fileNamesSnapshot: string[] }
export function InvalidInspectionAccountsModal({ session, onClose, onRefreshStatus, onAfterInvalidAccountAction }: {
  session: InvalidInspectionAccountSession
  onClose: () => void
  onRefreshStatus: () => Promise<void>
  onAfterInvalidAccountAction?: () => Promise<void>
}) {
  const { t } = useTranslation()
  const { action, fileNamesSnapshot: fileNames } = session
  const [selectedFileNames, setSelectedFileNames] = useState(() => [...fileNames])
  const [submitting, setSubmitting] = useState(false)
  const submittingRef = useRef(false)
  const [error, setError] = useState('')
  const onCancel = () => { if (!submittingRef.current) onClose() }
  const onToggleFileName = (fileName: string, checked: boolean) => setSelectedFileNames((current) => checked
    ? current.includes(fileName) ? current : [...current, fileName]
    : current.filter((name) => name !== fileName))
  const onSelectAll = () => setSelectedFileNames(selectAllInvalidInspectionAccountFileNames(fileNames))
  const onInvertSelection = () => setSelectedFileNames((current) => invertInvalidInspectionAccountFileNames(fileNames, current))
  const onConfirm = async () => {
    if (submittingRef.current || selectedFileNames.length === 0) return
    submittingRef.current = true
    setSubmitting(true)
    setError('')
    try {
      if (action === 'disable') await setCredentialsDisabled(selectedFileNames, true)
      else await deleteCredentials(selectedFileNames)
      await Promise.all([onRefreshStatus(), onAfterInvalidAccountAction?.()])
      onClose()
    } catch (nextError) {
      setError(nextError instanceof Error ? nextError.message : t('usage_stats.credentials_inspection_invalid_accounts_failed'))
    } finally {
      submittingRef.current = false
      setSubmitting(false)
    }
  }
  const actionLabel = action === 'delete' ? t('usage_stats.credentials_inspection_delete_action') : t('usage_stats.credentials_inspection_disable_action')
  return (
    <Modal
      open
      title={t('usage_stats.credentials_inspection_invalid_accounts_title', { action: actionLabel })}
      onClose={onCancel}
      width={600}
      className={styles.credentialInvalidAccountModal}
      closeDisabled={submitting}
      footer={(
        <div className={styles.credentialInvalidAccountFooter}>
          <Button type="button" variant="secondary" appearance="action" onClick={onCancel} disabled={submitting}>
            {t('common.cancel')}
          </Button>
          <Button
            type="button"
            variant={action === 'delete' ? 'danger' : 'primary'}
            appearance="action"
            onClick={() => void onConfirm()}
            disabled={submitting || selectedFileNames.length === 0}
            loading={submitting}
            aria-busy={submitting}
          >
            {t('usage_stats.credentials_inspection_invalid_accounts_confirm', { action: actionLabel })}
          </Button>
        </div>
      )}
    >
      <div className={styles.credentialInvalidAccountPanel}>
        <p>{t(action === 'delete' ? 'usage_stats.credentials_inspection_delete_invalid_confirm' : 'usage_stats.credentials_inspection_disable_invalid_confirm')}</p>
        <div className={styles.credentialInvalidAccountTip}>{t('usage_stats.credentials_inspection_invalid_accounts_sync_tip')}</div>
        {error && <div className={shared.credentialInlineError}>{error}</div>}
        <div className={styles.credentialInvalidAccountToolbar}>
          <span>{selectedFileNames.length} / {fileNames.length}</span>
          <div className={styles.credentialInvalidAccountToolbarActions}>
            <button type="button" onClick={onSelectAll} disabled={submitting || fileNames.length === 0}>
              {t('usage_stats.credentials_inspection_invalid_accounts_select_all')}
            </button>
            <button type="button" onClick={onInvertSelection} disabled={submitting || fileNames.length === 0}>
              {t('usage_stats.credentials_inspection_invalid_accounts_invert_selection')}
            </button>
          </div>
        </div>
        <div className={styles.credentialInvalidAccountList}>
          {fileNames.map((fileName) => (
            <label key={fileName} className={styles.credentialInvalidAccountItem}>
              <input
                type="checkbox"
                checked={selectedFileNames.includes(fileName)}
                onChange={(event) => onToggleFileName(fileName, event.target.checked)}
                disabled={submitting}
              />
              <span>{fileName}</span>
            </label>
          ))}
        </div>
      </div>
    </Modal>
  )
}

