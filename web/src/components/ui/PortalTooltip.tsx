import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import styles from './PortalTooltip.module.scss'

const TOOLTIP_MAX_WIDTH = 280
const TOOLTIP_ESTIMATED_HEIGHT = 72
const TOOLTIP_OFFSET = 10
const TOOLTIP_VIEWPORT_PADDING = 8

export type PortalTooltipState = {
  lines: string[]
  x: number
  y: number
  placement: 'above' | 'below'
}

type PortalTooltipTarget = {
  lines: string[]
  anchor: HTMLElement
}

export function usePortalTooltip() {
  const [tooltip, setTooltip] = useState<PortalTooltipState | null>(null)
  const hoverTargetRef = useRef<PortalTooltipTarget | null>(null)
  const focusTargetRef = useRef<PortalTooltipTarget | null>(null)

  const positionTooltip = useCallback((target: PortalTooltipTarget | null) => {
    if (!target?.anchor.isConnected) {
      setTooltip(null)
      return
    }

    // 浮层挂到 body 后不受滚动容器裁剪，并围绕当前 hover/focus 锚点限制在视口内。
    const viewportWidth = typeof window === 'undefined' ? 1024 : window.innerWidth
    const viewportHeight = typeof window === 'undefined' ? 768 : window.innerHeight
    const rect = target.anchor.getBoundingClientRect()
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

    setTooltip({ lines: target.lines, x, y, placement })
  }, [])

  const syncTooltip = useCallback(() => {
    if (hoverTargetRef.current && !hoverTargetRef.current.anchor.isConnected) {
      hoverTargetRef.current = null
    }
    if (focusTargetRef.current && !focusTargetRef.current.anchor.isConnected) {
      focusTargetRef.current = null
    }
    positionTooltip(hoverTargetRef.current ?? focusTargetRef.current)
  }, [positionTooltip])

  const showOnMouseEnter = useCallback((lines: string[], anchor: HTMLElement) => {
    hoverTargetRef.current = { lines, anchor }
    syncTooltip()
  }, [syncTooltip])

  const hideOnMouseLeave = useCallback((anchor: HTMLElement) => {
    if (hoverTargetRef.current?.anchor === anchor) {
      hoverTargetRef.current = null
    }
    syncTooltip()
  }, [syncTooltip])

  const showOnFocus = useCallback((lines: string[], anchor: HTMLElement) => {
    focusTargetRef.current = { lines, anchor }
    syncTooltip()
  }, [syncTooltip])

  const hideOnBlur = useCallback((anchor: HTMLElement) => {
    if (focusTargetRef.current?.anchor === anchor) {
      focusTargetRef.current = null
    }
    syncTooltip()
  }, [syncTooltip])

  const dismiss = useCallback((): boolean => {
    const dismissed = hoverTargetRef.current !== null || focusTargetRef.current !== null
    hoverTargetRef.current = null
    focusTargetRef.current = null
    setTooltip(null)
    return dismissed
  }, [])

  useEffect(() => {
    const repositionTooltip = () => {
      if (hoverTargetRef.current || focusTargetRef.current) {
        syncTooltip()
      }
    }
    window.addEventListener('resize', repositionTooltip)
    window.addEventListener('scroll', repositionTooltip, true)
    return () => {
      window.removeEventListener('resize', repositionTooltip)
      window.removeEventListener('scroll', repositionTooltip, true)
    }
  }, [syncTooltip])

  return {
    tooltip,
    showOnMouseEnter,
    hideOnMouseLeave,
    showOnFocus,
    hideOnBlur,
    dismiss,
  }
}

export function PortalTooltip({ tooltip }: { tooltip: PortalTooltipState | null }) {
  if (!tooltip || typeof document === 'undefined') return null

  return createPortal(
    <div
      className={styles.tooltip}
      role="tooltip"
      style={{
        left: tooltip.x,
        top: tooltip.y,
        transform: tooltip.placement === 'above'
          ? 'translate(-50%, -100%)'
          : 'translateX(-50%)',
      }}
    >
      {tooltip.lines.map((line, index) => <span key={`${index}-${line}`}>{line}</span>)}
    </div>,
    document.body,
  )
}
