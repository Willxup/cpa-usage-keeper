import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { useCredentialEnterSave } from './useCredentialEnterSave'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { IconCheck, IconX } from '@/components/ui/icons'
import styles from './CredentialSections.module.scss'

interface CredentialPriorityEditorProps {
  priority?: number | null
  displayName: string
  readOnly?: boolean
  openAIShared?: boolean
  onSave?: (priority: number) => Promise<void>
}

interface PopoverPosition { left: number; top: number }

const POPOVER_MARGIN = 8
const POPOVER_GAP = 6

export function parseCredentialPriority(value: string): number | null {
  const trimmed = value.trim()
  if (!/^[+-]?[0-9]+$/.test(trimmed)) return null
  const priority = Number(trimmed)
  return Number.isSafeInteger(priority) ? (priority === 0 ? 0 : priority) : null
}

export function CredentialPriorityEditor({ priority, displayName, readOnly = false, openAIShared = false, onSave }: CredentialPriorityEditorProps) {
  const { t } = useTranslation()
  const currentPriority = typeof priority === 'number' && Number.isFinite(priority) ? priority : 0
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(String(currentPriority))
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [position, setPosition] = useState<PopoverPosition | null>(null)
  const busyRef = useRef(false)
  const triggerRef = useRef<HTMLButtonElement | null>(null)
  const inputRef = useRef<HTMLInputElement | null>(null)
  const popoverRef = useRef<HTMLDivElement | null>(null)
  const restoreFocusRef = useRef(false)
  const focusedOpenRef = useRef(false)
  const popoverId = useId()
  const hintId = useId()
  const noteId = useId()
  const errorId = useId()

  useEffect(() => {
    if (!editing && restoreFocusRef.current) {
      restoreFocusRef.current = false
      triggerRef.current?.focus()
    }
  }, [editing])

  const updatePosition = useCallback(() => {
    const trigger = triggerRef.current
    const popover = popoverRef.current
    if (!trigger || !popover) return
    const anchor = trigger.getBoundingClientRect()
    const popup = popover.getBoundingClientRect()
    const viewportWidth = window.innerWidth
    const viewportHeight = window.innerHeight
    // 与现有凭证浮层一致，固定定位并按实际弹层尺寸贴近标签、夹在视口边缘内。
    const left = Math.max(POPOVER_MARGIN, Math.min(
      anchor.left + anchor.width / 2 - popup.width / 2,
      viewportWidth - POPOVER_MARGIN - popup.width,
    ))
    const below = anchor.bottom + POPOVER_GAP
    const above = anchor.top - POPOVER_GAP - popup.height
    const top = below + popup.height <= viewportHeight - POPOVER_MARGIN || above < POPOVER_MARGIN
      ? Math.max(POPOVER_MARGIN, Math.min(below, viewportHeight - POPOVER_MARGIN - popup.height))
      : above
    setPosition((current) => current?.left === left && current.top === top ? current : { left, top })
  }, [])

  useLayoutEffect(() => {
    if (!editing) return
    updatePosition()
  }, [editing, error, openAIShared, updatePosition])

  useLayoutEffect(() => {
    if (!editing) {
      focusedOpenRef.current = false
      return
    }
    // 浮层首帧隐藏以避免位置闪烁，必须等定位状态生效后再聚焦；错误提示重排不抢焦点。
    if (position && !focusedOpenRef.current && inputRef.current) {
      inputRef.current.focus()
      focusedOpenRef.current = true
    }
  }, [editing, position])

  useEffect(() => {
    if (!editing) return
    const syncPosition = () => updatePosition()
    window.addEventListener('resize', syncPosition)
    window.addEventListener('scroll', syncPosition, true)
    return () => {
      window.removeEventListener('resize', syncPosition)
      window.removeEventListener('scroll', syncPosition, true)
    }
  }, [editing, updatePosition])

  const close = (restoreFocus: boolean) => {
    if (busyRef.current) return
    restoreFocusRef.current = restoreFocus
    setEditing(false)
    setError('')
    setPosition(null)
  }
  const cancel = () => {
    setDraft(String(currentPriority))
    close(true)
  }

  useEffect(() => {
    if (!editing || typeof document === 'undefined') return
    const handlePointerDown = (event: PointerEvent) => {
      if (busyRef.current || !(event.target instanceof Node)) return
      if (triggerRef.current?.contains(event.target) || popoverRef.current?.contains(event.target)) return
      // 点击其它控件时让焦点自然落在被点位置，只有显式取消与键盘关闭才还给标签。
      setEditing(false)
      setError('')
      setPosition(null)
    }
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || busyRef.current) return
      event.preventDefault()
      setDraft(String(currentPriority))
      restoreFocusRef.current = true
      setEditing(false)
      setError('')
      setPosition(null)
    }
    document.addEventListener('pointerdown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [editing, currentPriority])

  const save = async () => {
    if (busyRef.current || !onSave) return
    const parsed = parseCredentialPriority(draft)
    if (parsed === null) {
      setError(t('usage_stats.credentials_priority_invalid'))
      return
    }
    busyRef.current = true
    setSaving(true)
    setError('')
    try {
      await onSave(parsed)
      busyRef.current = false
      close(true)
    } catch {
      // API 层给出全局提示；浮层保留草稿和行内错误以便重试。
      setError(t('usage_stats.credentials_priority_save_failed'))
    } finally {
      busyRef.current = false
      setSaving(false)
    }
  }

  const enterSave = useCredentialEnterSave(save)

  if (!onSave || readOnly) {
    if (priority === null || priority === undefined) return null
    return <span className={styles.credentialPriorityBadge}>P{currentPriority}</span>
  }

  return (
    <>
      <button
        type="button"
        ref={triggerRef}
        className={`${styles.credentialPriorityBadge} ${styles.credentialPriorityEditButton}`}
        onClick={() => {
          if (busyRef.current) return
          if (editing) { close(true); return }
          setDraft(String(currentPriority))
          setError('')
          setPosition(null)
          setEditing(true)
        }}
        aria-label={t('usage_stats.credentials_priority_edit', { name: displayName, priority: currentPriority })}
        title={t('usage_stats.credentials_priority_edit', { name: displayName, priority: currentPriority })}
        aria-haspopup="dialog"
        aria-expanded={editing}
        aria-controls={editing ? popoverId : undefined}
      >P{currentPriority}</button>
      {editing && typeof document !== 'undefined' && createPortal(
        <div
          id={popoverId}
          ref={popoverRef}
          className={styles.credentialPriorityEditor}
          role="dialog"
          aria-label={t('usage_stats.credentials_priority_edit', { name: displayName, priority: currentPriority })}
          aria-busy={saving || undefined}
          style={position ?? { visibility: 'hidden' }}
        >
          <div className={styles.credentialPriorityEditControls}>
            <input
              ref={inputRef}
              className={styles.credentialPriorityInput}
              type="text"
              inputMode="decimal"
              value={draft}
              disabled={saving}
              aria-label={t('usage_stats.credentials_priority_input', { name: displayName })}
              aria-invalid={Boolean(error) || undefined}
              aria-describedby={[hintId, openAIShared ? noteId : '', error ? errorId : ''].filter(Boolean).join(' ')}
              onChange={(event) => { setDraft(event.target.value); setError('') }}
              {...enterSave}
            />
            <button type="button" className={styles.credentialPriorityAction} onClick={() => void save()} disabled={saving}
              aria-label={saving ? t('usage_stats.credentials_priority_saving') : t('usage_stats.credentials_priority_save')}>
              {saving ? <LoadingSpinner size={12} /> : <IconCheck size={13} />}
            </button>
            <button type="button" className={styles.credentialPriorityAction} onClick={cancel} disabled={saving}
              aria-label={t('usage_stats.credentials_priority_cancel')}><IconX size={13} /></button>
          </div>
          <span id={hintId} className={styles.credentialPriorityScope}>{t('usage_stats.credentials_priority_order_hint')}</span>
          {openAIShared && <span id={noteId} className={styles.credentialPriorityScope}>{t('usage_stats.credentials_priority_openai_scope')}</span>}
          {error && <span id={errorId} className={styles.credentialPriorityError} role="alert">{error}</span>}
        </div>,
        document.body,
      )}
    </>
  )
}
