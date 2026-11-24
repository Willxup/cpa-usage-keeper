import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import type { CredentialRow } from './credentialViewModels'
import { CredentialKimiSiteBadge, kimiCredentialSite } from './CredentialKimiSiteBadge'
import { CredentialSubscriptionBadge } from './CredentialSubscriptionBadge'
import { CredentialPriorityEditor } from './CredentialPriorityEditor'
import type { CredentialViewProps } from './credentialViewProps'
import styles from './CredentialSections.module.scss'

type CredentialExpiryTooltipTarget = { rowKey: string; anchor: HTMLSpanElement }
type CredentialExpiryTooltipState = { rowKey: string; x: number; y: number; placement: 'above' | 'below' }
const CREDENTIAL_EXPIRY_TOOLTIP_SAFE_WIDTH = 300
const CREDENTIAL_EXPIRY_TOOLTIP_ESTIMATED_HEIGHT = 48
const CREDENTIAL_EXPIRY_TOOLTIP_OFFSET = 10
const CREDENTIAL_EXPIRY_TOOLTIP_VIEWPORT_PADDING = 8

export function useCredentialExpiryTooltip(rows: CredentialRow[]) {
  const { t } = useTranslation()
  const [expiryTooltip, setExpiryTooltip] = useState<CredentialExpiryTooltipState | null>(null)
  const expiryTooltipHoverTargetRef = useRef<CredentialExpiryTooltipTarget | null>(null)
  const expiryTooltipFocusTargetRef = useRef<CredentialExpiryTooltipTarget | null>(null)
  const positionExpiryTooltip = useCallback((target: CredentialExpiryTooltipTarget | null) => {
    if (!target?.anchor.isConnected) {
      setExpiryTooltip(null)
      return
    }
    const viewportWidth = typeof window === 'undefined' ? 1024 : window.innerWidth
    const viewportHeight = typeof window === 'undefined' ? 768 : window.innerHeight
    const rect = target.anchor.getBoundingClientRect()
    const tooltipWidth = Math.min(
      CREDENTIAL_EXPIRY_TOOLTIP_SAFE_WIDTH,
      Math.max(viewportWidth - CREDENTIAL_EXPIRY_TOOLTIP_VIEWPORT_PADDING * 2, 0),
    )
    const halfTooltipWidth = tooltipWidth / 2
    const minX = CREDENTIAL_EXPIRY_TOOLTIP_VIEWPORT_PADDING + halfTooltipWidth
    const maxX = viewportWidth - CREDENTIAL_EXPIRY_TOOLTIP_VIEWPORT_PADDING - halfTooltipWidth
    const anchorX = rect.left + rect.width / 2
    const x = maxX >= minX ? Math.max(minX, Math.min(anchorX, maxX)) : viewportWidth / 2
    const spaceBelow = viewportHeight - rect.bottom - CREDENTIAL_EXPIRY_TOOLTIP_OFFSET - CREDENTIAL_EXPIRY_TOOLTIP_VIEWPORT_PADDING
    const spaceAbove = rect.top - CREDENTIAL_EXPIRY_TOOLTIP_OFFSET - CREDENTIAL_EXPIRY_TOOLTIP_VIEWPORT_PADDING
    const placement = spaceBelow >= CREDENTIAL_EXPIRY_TOOLTIP_ESTIMATED_HEIGHT || spaceBelow >= spaceAbove ? 'below' : 'above'
    const y = placement === 'above' ? rect.top - CREDENTIAL_EXPIRY_TOOLTIP_OFFSET : rect.bottom + CREDENTIAL_EXPIRY_TOOLTIP_OFFSET
    setExpiryTooltip({ rowKey: target.rowKey, x, y, placement })
  }, [])

  const syncExpiryTooltip = useCallback(() => {
    if (expiryTooltipHoverTargetRef.current && !expiryTooltipHoverTargetRef.current.anchor.isConnected) {
      expiryTooltipHoverTargetRef.current = null
    }
    if (expiryTooltipFocusTargetRef.current && !expiryTooltipFocusTargetRef.current.anchor.isConnected) {
      expiryTooltipFocusTargetRef.current = null
    }
    positionExpiryTooltip(expiryTooltipHoverTargetRef.current ?? expiryTooltipFocusTargetRef.current)
  }, [positionExpiryTooltip])

  useEffect(() => {
    window.addEventListener('resize', syncExpiryTooltip)
    window.addEventListener('scroll', syncExpiryTooltip, true)
    return () => {
      window.removeEventListener('resize', syncExpiryTooltip)
      window.removeEventListener('scroll', syncExpiryTooltip, true)
    }
  }, [syncExpiryTooltip])

  const activeExpiryRow = expiryTooltip
    ? rows.find((row) => (row.identity.id || row.identity.identity) === expiryTooltip.rowKey)
    : undefined
  const activeExpiryTooltipText = activeExpiryRow?.expiresAtLabel
    ? t('usage_stats.credentials_expiry_tooltip', { value: activeExpiryRow.expiresAtLabel })
    : ''

  const badges = (row: CredentialRow, onSavePriority: CredentialViewProps['onSavePriority']) => {
    const rowKey = row.identity.id || row.identity.identity
    return (kimiCredentialSite(row.identity.type) || row.subscriptionBadge || row.remainingDaysLabel || row.priorityLabel || (onSavePriority && !row.identity.is_deleted) ? (
              <span className={styles.credentialIdentityBadges}>
                <CredentialKimiSiteBadge identityType={row.identity.type} />
                {row.subscriptionBadge && <CredentialSubscriptionBadge model={row.subscriptionBadge} />}
                {row.remainingDaysLabel && row.expiresAtLabel
                  ? (
                    <span
                      className={styles.credentialRemainingDaysBadge}
                      tabIndex={0}
                      aria-label={`${row.remainingDaysLabel}; ${(row.expiresAtLabel ? t('usage_stats.credentials_expiry_tooltip', { value: row.expiresAtLabel }) : '')}`}
                      onMouseEnter={(event) => {
                        expiryTooltipHoverTargetRef.current = { rowKey, anchor: event.currentTarget }
                        syncExpiryTooltip()
                      }}
                      onMouseLeave={(event) => {
                        if (expiryTooltipHoverTargetRef.current?.anchor === event.currentTarget) {
                          expiryTooltipHoverTargetRef.current = null
                        }
                        syncExpiryTooltip()
                      }}
                      onFocus={(event) => {
                        expiryTooltipFocusTargetRef.current = { rowKey, anchor: event.currentTarget }
                        syncExpiryTooltip()
                      }}
                      onBlur={(event) => {
                        if (expiryTooltipFocusTargetRef.current?.anchor === event.currentTarget) {
                          expiryTooltipFocusTargetRef.current = null
                        }
                        syncExpiryTooltip()
                      }}
                    >
                      {row.remainingDaysLabel}
                    </span>
                  )
                  : row.remainingDaysLabel && <span className={styles.credentialRemainingDaysBadge}>{row.remainingDaysLabel}</span>}
                <CredentialPriorityEditor
                  priority={row.identity.priority}
                  displayName={row.displayName}
                  readOnly={row.identity.is_deleted}
                  onSave={onSavePriority ? (priority) => onSavePriority(row.identity.id || row.identity.identity, row.identity.identity, priority) : undefined}
                />
              </span>
            ) : undefined)
  }
  const portal = (expiryTooltip && activeExpiryTooltipText && typeof document !== 'undefined'
        ? createPortal(
            <div
              className={styles.credentialExpiryTooltip}
              role="tooltip"
              style={{
                left: expiryTooltip.x,
                top: expiryTooltip.y,
                transform: expiryTooltip.placement === 'above'
                  ? 'translate(-50%, -100%)'
                  : 'translateX(-50%)',
              }}
            >
              {activeExpiryTooltipText}
            </div>,
            document.body,
          )
        : null)
  return { badges, portal }
}
