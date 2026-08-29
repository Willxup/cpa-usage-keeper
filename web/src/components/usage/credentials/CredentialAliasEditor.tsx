import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { IconCheck, IconPencil, IconX } from '@/components/ui/icons'
import styles from './CredentialSections.module.scss'

interface CredentialAliasEditorProps {
  identityId: string
  displayName: string
  alias?: string | null
  saving: boolean
  disabled?: boolean
  onOpenDetails?: () => void
  onSaveAlias: (id: string, alias: string) => Promise<void>
}

export function isCredentialAliasEditorDisabled(identityId: string, isDeleted?: boolean, aliasSavingId?: string) {
  return Boolean(isDeleted || (aliasSavingId && aliasSavingId !== identityId))
}

export function CredentialAliasEditor({ identityId, displayName, alias, saving, disabled = false, onOpenDetails, onSaveAlias }: CredentialAliasEditorProps) {
  const { t } = useTranslation()
  const [editing, setEditing] = useState(false)
  const [draftAlias, setDraftAlias] = useState(alias ?? '')
  const editButtonRef = useRef<HTMLButtonElement | null>(null)
  const restoreFocusRef = useRef(false)
  const currentAlias = alias ?? ''
  const canEdit = !disabled && identityId.trim() !== ''
  const canSave = !saving && !disabled && draftAlias.trim() !== currentAlias.trim()

  useEffect(() => {
    if (!editing && restoreFocusRef.current) {
      restoreFocusRef.current = false
      editButtonRef.current?.focus()
    }
  }, [editing])

  const finishEditing = () => {
    restoreFocusRef.current = true
    setEditing(false)
  }

  const startEditing = () => {
    if (!canEdit) return
    setDraftAlias(currentAlias)
    setEditing(true)
  }
  const cancelEditing = () => {
    if (saving || disabled) return
    setDraftAlias(currentAlias)
    finishEditing()
  }
  const saveAlias = async () => {
    if (!canSave) return
    try {
      await onSaveAlias(identityId, draftAlias)
      finishEditing()
    } catch {
      // 保存失败时保持编辑态，方便用户直接修正或重试。
    }
  }

  if (editing) {
    return (
      <span className={styles.credentialAliasEditor}>
        <span className={styles.credentialAliasEditLayout}>
          <input
            className={styles.credentialAliasInput}
            value={draftAlias}
            placeholder={t('usage_stats.credentials_alias_placeholder')}
            disabled={saving || disabled}
            maxLength={50}
            onChange={(event) => setDraftAlias(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') {
                event.preventDefault()
                void saveAlias()
              }
              if (event.key === 'Escape') {
                event.preventDefault()
                cancelEditing()
              }
            }}
            aria-label={t('usage_stats.credentials_alias_placeholder')}
            autoFocus
          />
          <span className={styles.credentialAliasEditActions}>
            <button
              type="button"
              className={styles.credentialAliasIconButton}
              onClick={() => void saveAlias()}
              disabled={!canSave}
              title={saving ? t('usage_stats.credentials_alias_saving') : t('usage_stats.credentials_alias_save')}
              aria-label={saving ? t('usage_stats.credentials_alias_saving') : t('usage_stats.credentials_alias_save')}
              aria-busy={saving}
            >
              {saving ? <LoadingSpinner size={12} /> : <IconCheck size={13} />}
            </button>
            <button
              type="button"
              className={styles.credentialAliasIconButton}
              onClick={cancelEditing}
              disabled={saving || disabled}
              title={t('usage_stats.credentials_alias_cancel')}
              aria-label={t('usage_stats.credentials_alias_cancel')}
            >
              <IconX size={13} />
            </button>
          </span>
        </span>
      </span>
    )
  }

  return (
    <span className={styles.credentialAliasEditor}>
      <span className={styles.credentialAliasDisplayLayout}>
        {onOpenDetails ? (
          <button
            type="button"
            className={`${styles.credentialAliasNameSlot} ${styles.credentialDetailNameButton}`}
            data-credential-detail-trigger="true"
            onClick={onOpenDetails}
          >
            <span className={styles.credentialDetailNameText}>{displayName}</span>
            <span className={styles.credentialDetailNameArrow} aria-hidden="true">›</span>
          </button>
        ) : (
          <span className={styles.credentialAliasNameSlot}>{displayName}</span>
        )}
        <span className={styles.credentialAliasActionSlot}>
          {canEdit && (
            <button
              type="button"
              className={styles.credentialAliasEditButton}
              ref={editButtonRef}
              onClick={startEditing}
              title={t('usage_stats.credentials_alias_edit')}
              aria-label={t('usage_stats.credentials_alias_edit')}
            >
              <IconPencil size={12} />
            </button>
          )}
        </span>
      </span>
    </span>
  )
}
