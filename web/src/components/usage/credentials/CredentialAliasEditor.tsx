import { useTranslation } from 'react-i18next'
import { IconPencil } from '@/components/ui/icons'
import styles from './CredentialSections.module.scss'

interface CredentialAliasEditorProps {
  identityId: string
  displayName: string
  disabled?: boolean
  onOpenDetails?: () => void
  onEdit: () => void
}

// 名称仍进入详情，原别名铅笔按钮统一打开凭证编辑弹框。
export function CredentialAliasEditor({ identityId, displayName, disabled = false, onOpenDetails, onEdit }: CredentialAliasEditorProps) {
  const { t } = useTranslation()
  const canEdit = !disabled && identityId.trim() !== ''
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
            <span className={styles.credentialDetailNameArrow} aria-hidden="true">‹</span>
          </button>
        ) : (
          <span className={styles.credentialAliasNameSlot}>{displayName}</span>
        )}
        <span className={styles.credentialAliasActionSlot}>
          {canEdit && (
            <button
              type="button"
              className={styles.credentialAliasEditButton}
              onClick={(event) => {
                // 明确记录鼠标/触摸与键盘共同的回焦入口，包括默认不聚焦按钮的浏览器。
                event.currentTarget.focus()
                onEdit()
              }}
              title={t('usage_stats.credentials_edit_title')}
              aria-label={t('usage_stats.credentials_edit_title')}
            >
              <IconPencil size={12} />
            </button>
          )}
        </span>
      </span>
    </span>
  )
}
