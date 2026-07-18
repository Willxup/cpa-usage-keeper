import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import styles from './CredentialSections.module.scss'

const TOOLTIP_MAX_WIDTH = 280
const TOOLTIP_ESTIMATED_HEIGHT = 48
const TOOLTIP_OFFSET = 10
const TOOLTIP_VIEWPORT_PADDING = 8

type CredentialExpiryTooltipState = {
  text: string
  x: number
  y: number
  placement: 'above' | 'below'
}

interface CredentialExpiryBadgeProps {
  remainingDaysLabel: string
  expiresAtLabel: string
}

export function CredentialExpiryBadge({ remainingDaysLabel, expiresAtLabel }: CredentialExpiryBadgeProps) {
  const { t } = useTranslation()
  const anchorRef = useRef<HTMLSpanElement | null>(null)
  const hoveredRef = useRef(false)
  const focusedRef = useRef(false)
  const [tooltip, setTooltip] = useState<CredentialExpiryTooltipState | null>(null)
  const tooltipText = t('usage_stats.credentials_expiry_tooltip', { value: expiresAtLabel })

  const positionTooltip = useCallback(() => {
    const anchor = anchorRef.current
    if (!anchor?.isConnected) {
      setTooltip(null)
      return
    }

    // 浮层挂到 body，避免被认证列表卡片裁剪，并在视口边缘自动切换方向。
    const viewportWidth = typeof window === 'undefined' ? 1024 : window.innerWidth
    const viewportHeight = typeof window === 'undefined' ? 768 : window.innerHeight
    const rect = anchor.getBoundingClientRect()
    const tooltipWidth = Math.min(
      TOOLTIP_MAX_WIDTH,
      Math.max(viewportWidth - TOOLTIP_VIEWPORT_PADDING * 2, 0),
    )
    const halfTooltipWidth = tooltipWidth / 2
    const minX = TOOLTIP_VIEWPORT_PADDING + halfTooltipWidth
    const maxX = viewportWidth - TOOLTIP_VIEWPORT_PADDING - halfTooltipWidth
    const anchorX = rect.left + rect.width / 2
    const x = maxX >= minX ? Math.max(minX, Math.min(anchorX, maxX)) : viewportWidth / 2
    const spaceBelow = viewportHeight - rect.bottom - TOOLTIP_OFFSET - TOOLTIP_VIEWPORT_PADDING
    const spaceAbove = rect.top - TOOLTIP_OFFSET - TOOLTIP_VIEWPORT_PADDING
    const placement = spaceBelow >= TOOLTIP_ESTIMATED_HEIGHT || spaceBelow >= spaceAbove ? 'below' : 'above'
    const y = placement === 'above' ? rect.top - TOOLTIP_OFFSET : rect.bottom + TOOLTIP_OFFSET

    setTooltip({ text: tooltipText, x, y, placement })
  }, [tooltipText])

  const syncTooltip = useCallback(() => {
    if (hoveredRef.current || focusedRef.current) {
      positionTooltip()
      return
    }
    setTooltip(null)
  }, [positionTooltip])

  useEffect(() => {
    if (!tooltip) {
      return
    }
    window.addEventListener('resize', syncTooltip)
    window.addEventListener('scroll', syncTooltip, true)
    return () => {
      window.removeEventListener('resize', syncTooltip)
      window.removeEventListener('scroll', syncTooltip, true)
    }
  }, [syncTooltip, tooltip])

  return (
    <>
      <span
        ref={anchorRef}
        className={styles.credentialRemainingDaysBadge}
        tabIndex={0}
        aria-label={`${remainingDaysLabel}; ${tooltipText}`}
        onMouseEnter={() => {
          hoveredRef.current = true
          syncTooltip()
        }}
        onMouseLeave={() => {
          hoveredRef.current = false
          syncTooltip()
        }}
        onFocus={() => {
          focusedRef.current = true
          syncTooltip()
        }}
        onBlur={() => {
          focusedRef.current = false
          syncTooltip()
        }}
      >
        {remainingDaysLabel}
      </span>
      {tooltip && typeof document !== 'undefined'
        ? createPortal(
            <div
              className={styles.credentialExpiryTooltip}
              role="tooltip"
              style={{
                left: tooltip.x,
                top: tooltip.y,
                transform: tooltip.placement === 'above'
                  ? 'translate(-50%, -100%)'
                  : 'translateX(-50%)',
              }}
            >
              {tooltip.text}
            </div>,
            document.body,
          )
        : null}
    </>
  )
}
