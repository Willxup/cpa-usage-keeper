import { useCallback, useEffect, useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { IconGaugeReset } from '@/components/ui/icons'
import { fetchClaudeResetGrants, fetchUsageQuotaResetCredits } from '@/lib/api'
import type { ClaudeResetGrantsResponse, UsageQuotaResetCreditsResponse, UsageQuotaResetResponse } from '@/lib/types'
import styles from './CredentialQuota.module.scss'
import shared from '../CredentialSections.module.scss'

type QuotaResetPopoverPosition = { top?: number; bottom?: number; right: number; maxHeight: number }
const RESET_CREDITS_LOOKUP_TIMEOUT_MS = 5_000
const RESET_CREDITS_POPOVER_VIEWPORT_MARGIN = 12
const RESET_CREDITS_POPOVER_OFFSET = 8
const RESET_CREDITS_POPOVER_MAX_HEIGHT = 360

export function QuotaResetAction({
  authIndex,
  resetCredits,
  disabled,
  loading,
  fetchResetCredits = fetchUsageQuotaResetCredits,
  provider = 'codex',
  variant = 'card',
  fetchClaudeGrants = fetchClaudeResetGrants,
  onConfirm,
}: {
  authIndex: string
  resetCredits: number
  disabled: boolean
  loading: boolean
  fetchResetCredits?: (authIndex: string, signal?: AbortSignal) => Promise<UsageQuotaResetCreditsResponse>
  provider?: 'claude' | 'codex'
  variant?: 'card' | 'list'
  fetchClaudeGrants?: typeof fetchClaudeResetGrants
  onConfirm: (grantId?: string, organizationId?: string) => Promise<UsageQuotaResetResponse | void>
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [popoverPosition, setPopoverPosition] = useState<QuotaResetPopoverPosition | null>(null)
  const [resetCreditsDetails, setResetCreditsDetails] = useState<UsageQuotaResetCreditsResponse | null>(null)
  const [resetCreditsLoading, setResetCreditsLoading] = useState(false)
  const [resetCreditsFailed, setResetCreditsFailed] = useState(false)
  const [claudeDetails, setClaudeDetails] = useState<ClaudeResetGrantsResponse | null>(null)
  const [selectedClaudeGrantId, setSelectedClaudeGrantId] = useState('')
  const [claudeMessage, setClaudeMessage] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const submittingRef = useRef(false)
  const currentActionRef = useRef(`${provider}:${authIndex}`)
  currentActionRef.current = `${provider}:${authIndex}`
  const mountedRef = useRef(true)
  useEffect(() => { mountedRef.current = true; return () => { mountedRef.current = false } }, [])
  const tooltipId = useId()
  const actionRef = useRef<HTMLDivElement | null>(null)
  const buttonRef = useRef<HTMLButtonElement | null>(null)

  const updatePopoverPosition = useCallback(() => {
    if (typeof window === 'undefined') {
      return
    }
    const button = buttonRef.current
    if (!button) {
      return
    }
    const rect = button.getBoundingClientRect()
    const viewportHeight = window.innerHeight
    const spaceBelow = Math.max(0, viewportHeight - rect.bottom - RESET_CREDITS_POPOVER_VIEWPORT_MARGIN - RESET_CREDITS_POPOVER_OFFSET)
    const spaceAbove = Math.max(0, rect.top - RESET_CREDITS_POPOVER_VIEWPORT_MARGIN - RESET_CREDITS_POPOVER_OFFSET)
    const maxHeight = provider === 'claude' ? 400 : RESET_CREDITS_POPOVER_MAX_HEIGHT
    const openBelow = spaceBelow >= maxHeight || spaceBelow >= spaceAbove
    const availableHeight = openBelow ? spaceBelow : spaceAbove
    const sharedPosition = {
      right: Math.max(RESET_CREDITS_POPOVER_VIEWPORT_MARGIN, Math.round(window.innerWidth - rect.right)),
      maxHeight: Math.max(0, Math.min(maxHeight, availableHeight)),
    }
    // popover 使用 fixed 并选择空间更充足的一侧，避免被卡片或视口底部裁切。
    setPopoverPosition(openBelow
      ? { ...sharedPosition, top: Math.round(rect.bottom + RESET_CREDITS_POPOVER_OFFSET) }
      : { ...sharedPosition, bottom: Math.round(viewportHeight - rect.top + RESET_CREDITS_POPOVER_OFFSET) })
  }, [provider])

  useEffect(() => {
    if (!open) {
      return
    }
    const refreshPopoverPosition = () => updatePopoverPosition()
    window.addEventListener('resize', refreshPopoverPosition)
    window.addEventListener('scroll', refreshPopoverPosition, true)
    return () => {
      window.removeEventListener('resize', refreshPopoverPosition)
      window.removeEventListener('scroll', refreshPopoverPosition, true)
    }
  }, [open, updatePopoverPosition])

  useEffect(() => {
    if (!open) {
      return
    }
    const controller = new AbortController()
    let active = true
    const timeoutID = window.setTimeout(() => {
      if (!active) {
        return
      }
      active = false
      controller.abort()
      setResetCreditsFailed(true)
      setResetCreditsLoading(false)
    }, provider === 'claude' ? 20_000 : RESET_CREDITS_LOOKUP_TIMEOUT_MS)
    // Claude 确认需要本次查询的 grant 与组织 UUID；Codex 保留明细失败后的缓存回退。
    const lookup = provider === 'claude' ? fetchClaudeGrants(authIndex, controller.signal) : fetchResetCredits(authIndex, controller.signal)
    void lookup
      .then((response) => {
        if (active) {
          if (provider === 'claude') {
            const details = response as ClaudeResetGrantsResponse
            setClaudeDetails(details)
            setSelectedClaudeGrantId(details.selectedGrantId ?? '')
          } else setResetCreditsDetails(response as UsageQuotaResetCreditsResponse)
        }
      })
      .catch(() => {
        if (active) {
          setResetCreditsFailed(true)
        }
      })
      .finally(() => {
        if (!active) {
          return
        }
        active = false
        window.clearTimeout(timeoutID)
        setResetCreditsLoading(false)
      })
    return () => {
      active = false
      window.clearTimeout(timeoutID)
      controller.abort()
    }
  }, [authIndex, fetchResetCredits, fetchClaudeGrants, open, provider])

  useEffect(() => {
    if (!open || typeof document === 'undefined') {
      return
    }
    const handlePointerDown = (event: PointerEvent) => {
      const target = event.target
      if (!(target instanceof Node)) {
        return
      }
      if (actionRef.current?.contains(target)) {
        return
      }
      setOpen(false)
    }
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setOpen(false)
      }
    }
    document.addEventListener('pointerdown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [open])

  const handleConfirm = async () => {
    if (provider !== 'claude') { await onConfirm(); setOpen(false); return }
    if (submittingRef.current || !selectedClaudeGrantId || !claudeDetails?.organizationId) return
    const actionKey = `${provider}:${authIndex}`
    const isCurrent = () => mountedRef.current && currentActionRef.current === actionKey
    submittingRef.current = true
    setSubmitting(true)
    try {
      const result = await onConfirm(selectedClaudeGrantId, claudeDetails.organizationId)
      if (!isCurrent()) return
      if (result?.code === 'reset' || result?.code === 'already_used') setOpen(false)
      else { setClaudeMessage(result?.code ?? 'unknown'); setClaudeDetails(null) }
    } catch {
      if (isCurrent()) { setClaudeMessage('unknown'); setClaudeDetails(null) }
    } finally {
      if (isCurrent()) { submittingRef.current = false; setSubmitting(false) }
    }
  }

  const handleToggleOpen = () => {
    if (open) {
      setOpen(false)
      return
    }
    updatePopoverPosition()
    setResetCreditsDetails(null)
    setResetCreditsFailed(false)
    setResetCreditsLoading(true)
    setClaudeDetails(null)
    setClaudeMessage('')
    setSelectedClaudeGrantId('')
    setOpen(true)
  }

  const resetCreditsCountFromDetails = resetCreditsDetails && resetCreditsDetails.credits.length > 0
    ? resetCreditsDetails.credits.length
    : null
  // 与 CPAMC 保持一致：实时次数未知时先使用有效明细数量，仍未知则回退打开前的缓存次数。
  const displayResetCredits = provider === 'claude' ? claudeDetails?.status?.availableCount ?? resetCredits : resetCreditsDetails?.availableCount ?? resetCreditsCountFromDetails ?? resetCredits
  const resetCreditsExplicitlyUnavailable = resetCreditsDetails !== null
    && resetCreditsDetails.availableCount !== null
    && resetCreditsDetails.availableCount <= 0
  const resetCreditsDetailsIncomplete = resetCreditsDetails !== null && (
    (resetCreditsDetails.availableCount === null && resetCreditsDetails.credits.length === 0)
    || (resetCreditsDetails.availableCount !== null && resetCreditsDetails.availableCount > resetCreditsDetails.credits.length)
  )
  const confirmDisabled = loading || submitting || resetCreditsLoading || (provider === 'claude'
    ? resetCreditsFailed || !claudeDetails?.status?.grants.some((grant) => grant.id === selectedClaudeGrantId) || !claudeDetails.organizationId
    : resetCreditsExplicitlyUnavailable)
  const busy = loading || submitting
  const resetTitle = t(provider === 'claude' ? 'usage_stats.claude_reset_title' : 'usage_stats.credentials_quota_reset_title')

  return (
    <div ref={actionRef} className={styles.credentialQuotaResetAction}>
      <button
        ref={buttonRef}
        type="button"
        className={`${shared.credentialRowResetButton} ${variant === 'card' ? shared.credentialCardActionButton : ''} ${loading ? shared.credentialRowRefreshButtonLoading : ''}`.trim()}
        onClick={handleToggleOpen}
        disabled={disabled}
        aria-label={t('usage_stats.credentials_quota_reset_button', { count: String(resetCredits) })}
        aria-describedby={open ? undefined : tooltipId}
        aria-busy={loading}
        aria-expanded={open}
        aria-haspopup="dialog"
      >
        {loading ? <LoadingSpinner size={variant === 'card' ? 15 : 13} /> : <IconGaugeReset size={variant === 'card' ? 15 : 13} />}
      </button>
      {!open && (
        <span id={tooltipId} className={styles.credentialQuotaResetTooltip} role="tooltip">
          <span className={styles.credentialQuotaResetCount}>{resetCredits}</span>
          <span>{t('usage_stats.credentials_quota_reset_tooltip_suffix')}</span>
        </span>
      )}
      {open && (
        <div
          className={`${styles.credentialQuotaResetPopover} ${provider === 'claude' ? styles.credentialQuotaResetClaudePopover : ''}`.trim()}
          role="dialog"
          aria-label={resetTitle}
          style={popoverPosition ?? undefined}
        >
          <p className={styles.credentialQuotaResetTitle}>{resetTitle}</p>
          <p className={styles.credentialQuotaResetMessage}>
            <span className={styles.credentialQuotaResetCountLine}>
              <span className={styles.credentialQuotaResetCount}>{displayResetCredits}</span>
              <span>{t('usage_stats.credentials_quota_reset_message_suffix')}</span>
            </span>
            <span>{t('usage_stats.credentials_quota_reset_message_prompt')}</span>
          </p>
          <div className={styles.credentialQuotaResetExpiry} aria-live="polite">
            <p className={styles.credentialQuotaResetExpiryTitle}>{t(provider === 'claude' ? 'usage_stats.claude_reset_grants_title' : 'usage_stats.credentials_quota_reset_expiry_title')}</p>
            {resetCreditsLoading && (
              <div className={styles.credentialQuotaResetExpiryStatus}>
                <LoadingSpinner size={12} />
                <span>{t(provider === 'claude' ? 'usage_stats.claude_reset_loading' : 'usage_stats.credentials_quota_reset_expiry_loading')}</span>
              </div>
            )}
            {!resetCreditsLoading && resetCreditsDetails && resetCreditsDetails.credits.length > 0 && (
              <div className={styles.credentialQuotaResetExpiryList}>
                {resetCreditsDetails.credits.map((credit, index) => (
                  <div key={credit.id || `${credit.expiresAt}-${index}`} className={styles.credentialQuotaResetExpiryRow}>
                    <span>{t('usage_stats.credentials_quota_reset_expiry_item', { index: index + 1 })}</span>
                    <strong>{formatResetCreditExpiry(credit.expiresAt)}</strong>
                  </div>
                ))}
              </div>
            )}
            {!resetCreditsLoading && provider === 'claude' && (
              <ClaudeResetDetails details={claudeDetails ? { ...claudeDetails, selectedGrantId: selectedClaudeGrantId } : null} failed={resetCreditsFailed} message={claudeMessage} disabled={busy || !!claudeMessage} groupName={`${tooltipId}-grants`} onSelect={(grantId) => { if (!submittingRef.current && !loading) setSelectedClaudeGrantId(grantId) }} />
            )}
            {!resetCreditsLoading && provider !== 'claude' && resetCreditsExplicitlyUnavailable && (
              <p className={styles.credentialQuotaResetExpiryStatus}>{t('usage_stats.credentials_quota_reset_expiry_empty')}</p>
            )}
            {!resetCreditsLoading && provider !== 'claude' && (resetCreditsFailed || resetCreditsDetailsIncomplete) && (
              <p className={`${styles.credentialQuotaResetExpiryStatus} ${styles.credentialQuotaResetExpiryWarning}`.trim()}>
                {t('usage_stats.credentials_quota_reset_expiry_failed')}
              </p>
            )}
          </div>
          <div className={styles.credentialQuotaResetActions}>
            <button type="button" className={styles.credentialQuotaResetCancelButton} onClick={() => setOpen(false)} disabled={busy}>
              {t('common.cancel')}
            </button>
            <button type="button" className={styles.credentialQuotaResetConfirmButton} onClick={() => void handleConfirm()} disabled={confirmDisabled} aria-busy={busy}>
              {busy ? <LoadingSpinner size={12} /> : t('usage_stats.credentials_quota_reset_confirm')}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

function ClaudeResetDetails({ details, failed, message, disabled, groupName, onSelect }: { details: ClaudeResetGrantsResponse | null; failed: boolean; message: string; disabled: boolean; groupName: string; onSelect: (grantId: string) => void }) {
  const { t } = useTranslation()
  const status = details?.status
  const grant = status?.grants.find((item) => item.id === details?.selectedGrantId)
  // 上游范围直接展示原值，保留顺序与重复值。
  const scope = (clears: string[]) => clears.length ? clears.join(' / ') : t('usage_stats.claude_reset_scope_unknown')
  const messages = new Set(['unknown', 'not_limited', 'cooldown', 'ineligible', 'unavailable', 'rate_limited', 'auth_error', 'status_unavailable'])
  let code = message
  if (!code && (failed || !details?.status || !details.organizationId)) code = 'status_unavailable'
  if (!code && !details?.selectedGrantId) code = 'unavailable'
  const empty = !message && !failed && !!status && !!details?.organizationId && status.grants.length === 0
  return (
    <>
      {status && status.grants.length > 0 && <div role="radiogroup" aria-label={t('usage_stats.claude_reset_grants_title')} className={styles.credentialQuotaResetExpiryList}>
      {status.grants.map((item, index) => {
        const selected = item.id === details?.selectedGrantId
        return (
          <label key={item.id} data-claude-grant={item.id} className={`${styles.credentialQuotaResetExpiryRow} ${styles.credentialQuotaResetGrant} ${selected ? styles.credentialQuotaResetGrantSelected : ''}`.trim()}>
            <div className={styles.credentialQuotaResetGrantLine}>
              <span className={styles.credentialQuotaResetGrantChoice}><input type="radio" name={groupName} value={item.id} checked={selected} disabled={disabled} onChange={() => onSelect(item.id)} aria-label={item.label || t('usage_stats.claude_reset_grant', { index: index + 1 })} />{item.label || t('usage_stats.claude_reset_grant', { index: index + 1 })}</span>
              <strong>{t('usage_stats.claude_reset_grant_count', { left: item.resetsLeft })}</strong>
            </div>
            <p className={styles.credentialQuotaResetExpiryStatus}>{scope(item.clears)}</p>
            <div className={styles.credentialQuotaResetGrantLine}>
              <span>{t('usage_stats.claude_reset_expires')}</span>
              <strong>{item.endsAt ? formatResetCreditExpiry(item.endsAt) : t('usage_stats.claude_reset_no_expiry')}</strong>
            </div>
          </label>
        )
      })}
      </div>}
      {grant && <p className={styles.credentialQuotaResetExpiryStatus}>{scope(grant.clears)}</p>}
      {code && <p role="status" className={`${styles.credentialQuotaResetExpiryStatus} ${empty ? '' : styles.credentialQuotaResetExpiryWarning}`.trim()}>{t(`usage_stats.claude_reset_${messages.has(code) ? code : 'unavailable'}`)}</p>}
    </>
  )
}

const RESET_CREDIT_TIME_FORMATTER = new Intl.DateTimeFormat('en-CA', {
  timeZone: 'Asia/Shanghai',
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false,
})

export function formatResetCreditExpiry(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return RESET_CREDIT_TIME_FORMATTER.format(date).replace(',', '')
}

