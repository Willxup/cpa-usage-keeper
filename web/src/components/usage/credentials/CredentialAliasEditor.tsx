import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { IconPencil } from '@/components/ui/icons'
import styles from './CredentialSections.module.scss'

interface CredentialAliasEditorProps {
  identityId: string
  displayName: ReactNode
  disabled?: boolean
  showEditButton?: boolean
  onOpenDetails?: () => void
  onEdit?: () => void
}

// 卡片标题只负责查看详情，编辑入口可放到独立的 footer 操作区。
export function CredentialAliasEditor({ identityId, displayName, disabled = false, showEditButton = true, onOpenDetails, onEdit }: CredentialAliasEditorProps) {
  return (
    <span className={styles.credentialAliasEditor}>
      <span className={`${styles.credentialAliasDisplayLayout} ${!showEditButton ? styles.credentialAliasTitleOnly : ''}`.trim()}>
        {onOpenDetails ? (
          <button
            type="button"
            className={`${styles.credentialAliasNameSlot} ${styles.credentialDetailNameButton}`}
            data-credential-detail-trigger="true"
            data-credential-detail-entry="title"
            onClick={(event) => {
              event.currentTarget.focus()
              onOpenDetails()
            }}
          >
            <span className={styles.credentialDetailNameText}>{displayName}</span>
            <span className={styles.credentialDetailNameArrow} aria-hidden="true">‹</span>
          </button>
        ) : (
          <span className={styles.credentialAliasNameSlot}>{displayName}</span>
        )}
        {showEditButton && onEdit && <CredentialEditButton identityId={identityId} disabled={disabled} onEdit={onEdit} />}
      </span>
    </span>
  )
}

export function CredentialEditButton({ identityId, disabled = false, onEdit }: Pick<CredentialAliasEditorProps, 'identityId' | 'disabled'> & { onEdit: () => void }) {
  const { t } = useTranslation()
  if (disabled || !identityId.trim()) return null

  return (
    <button
      type="button"
      className={`${styles.credentialAliasEditButton} ${styles.credentialCardActionButton}`}
      onClick={(event) => {
        // 显式记录鼠标/触摸与键盘共同的回焦入口，包括不会自动聚焦按钮的浏览器。
        event.currentTarget.focus()
        onEdit()
      }}
      title={t('usage_stats.credentials_edit_title')}
      aria-label={t('usage_stats.credentials_edit_title')}
    >
      <IconPencil size={15} />
    </button>
  )
}
