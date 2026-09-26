import { useLayoutEffect, useRef, useState, type RefObject } from 'react'
import { useTranslation } from 'react-i18next'
import { useCredentialEnterSave } from './useCredentialEnterSave'
import { Modal } from '@/components/ui/Modal'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { ApiError } from '@/lib/api'
import { parseCredentialPriority } from './CredentialPriorityEditor'
import { isCredentialStatusToggleSupported } from './CredentialStatusToggle'
import type { CredentialDetailSelection, CredentialEditChange } from './credentialViewModels'
import styles from './CredentialSections.module.scss'

interface CredentialEditModalProps {
  selection: CredentialDetailSelection
  onClose: () => void
  onSaveField: (change: CredentialEditChange) => Promise<void>
  onSaved: () => void
  fallbackFocusRef?: RefObject<HTMLElement | null>
}

// 弹框挂在页面层，保存导致列表重新排序或过滤掉当前行时仍保留草稿。
export function CredentialEditModal({ selection, onClose, onSaveField, onSaved, fallbackFocusRef }: CredentialEditModalProps) {
  const { t } = useTranslation()
  useLayoutEffect(() => {
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const fallback = fallbackFocusRef?.current
    // 页面会直接卸载编辑弹框；在公共 Modal 转移焦点之前记住入口，并在卸载时恢复。
    return () => {
      const target = opener?.isConnected ? opener : fallback
      target?.focus()
    }
  }, [fallbackFocusRef])
  const { identity, displayName } = selection.row
  const [saved, setSaved] = useState({ alias: identity.alias ?? '', priority: identity.priority ?? 0, disabled: identity.disabled ?? false })
  const [alias, setAlias] = useState(saved.alias)
  const [priority, setPriority] = useState(String(saved.priority))
  const [enabled, setEnabled] = useState(!saved.disabled)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [partial, setPartial] = useState(false)
  const busyRef = useRef(false)
  const statusSupported = selection.kind === 'auth-file' || isCredentialStatusToggleSupported(identity.type)
  const readOnly = Boolean(identity.is_deleted)
  const parsedPriority = parseCredentialPriority(priority)
  const dirty = alias.trim() !== saved.alias.trim() || parsedPriority !== saved.priority || (statusSupported && !enabled !== saved.disabled)

  const save = async () => {
    if (busyRef.current || readOnly || !dirty) return
    if (parsedPriority === null) { setError(t('usage_stats.credentials_priority_invalid')); return }
    const changes: CredentialEditChange[] = []
    if (alias.trim() !== saved.alias.trim()) changes.push({ field: 'alias', value: alias.trim() })
    if (parsedPriority !== saved.priority) changes.push({ field: 'priority', value: parsedPriority })
    if (statusSupported && !enabled !== saved.disabled) changes.push({ field: 'disabled', value: !enabled })
    busyRef.current = true
    setSaving(true)
    setError('')
    let completed = 0
    try {
      // 各字段使用现有独立接口，逐项记住成功结果；失败后只重试未保存部分。
      for (const change of changes) {
        await onSaveField(change)
        setSaved((current) => ({ ...current, [change.field]: change.value }))
        completed += 1
      }
      onSaved()
    } catch (cause) {
      setPartial((current) => current || completed > 0)
      const key = cause instanceof ApiError && cause.status === 404
        ? 'usage_stats.credentials_priority_stale_target'
        : cause instanceof ApiError && cause.status === 409
          ? selection.kind === 'auth-file' ? 'usage_stats.credentials_priority_conflict_auth_file' : 'usage_stats.credentials_edit_unsupported'
          : 'usage_stats.credentials_edit_failed'
      setError(t(key))
    } finally {
      busyRef.current = false
      setSaving(false)
    }
  }

  const enterSave = useCredentialEnterSave(save)

  return (
    <Modal open title={t('usage_stats.credentials_edit_title')} width={600} closeDisabled={saving}
      onClose={() => { if (!busyRef.current) onClose() }}
      footer={<>
        <Button variant="secondary" disabled={saving} onClick={onClose}>{t('common.cancel')}</Button>
        <Button loading={saving} disabled={!dirty || readOnly} onClick={() => void save()}>{t('common.save')}</Button>
      </>}>
      <form {...enterSave} className={styles.credentialEditForm} onSubmit={(event) => { event.preventDefault(); void save() }}>
        <p className={styles.credentialEditName}>{displayName}</p>
        <Input label={t('usage_stats.credentials_edit_alias')} value={alias} maxLength={50} disabled={saving || readOnly}
          placeholder={t('usage_stats.credentials_alias_placeholder')}
          hint={t('usage_stats.credentials_edit_alias_hint')} onChange={(event) => setAlias(event.target.value)} />
        <div className={styles.credentialEditStatusRow}>
          <span>{t('usage_stats.credentials_edit_status')}</span>
          <label className={`${styles.credentialActiveOnlySwitch} ${!statusSupported || saving || readOnly ? styles.credentialActiveOnlySwitchDisabled : ''}`}>
            <span className={styles.credentialActiveOnlyLabel}>{t(enabled ? 'usage_stats.credentials_edit_enabled' : 'usage_stats.credentials_edit_disabled')}</span>
            <input type="checkbox" checked={enabled} disabled={!statusSupported || saving || readOnly}
              aria-label={t('usage_stats.credentials_edit_status')} onChange={(event) => setEnabled(event.target.checked)} />
            <span className={styles.credentialActiveOnlyTrack} aria-hidden="true"><span className={styles.credentialActiveOnlyThumb} /></span>
          </label>
        </div>
        {!statusSupported && <p className="hint">{t('usage_stats.credentials_edit_unsupported')}</p>}
        <Input label={t('usage_stats.credentials_edit_priority')} type="text" value={priority} disabled={saving || readOnly}
          hint={t('usage_stats.credentials_priority_order_hint')} onChange={(event) => setPriority(event.target.value)} />
        {selection.kind === 'ai-provider' && identity.type.trim().toLowerCase() === 'openai' &&
          <p className="hint">{t('usage_stats.credentials_priority_openai_scope')}</p>}
        {partial && <p className={styles.credentialEditPartial} role="status">{t('usage_stats.credentials_edit_partial')}</p>}
        {error && <p className="error-box" role="alert">{error}</p>}
      </form>
    </Modal>
  )
}
