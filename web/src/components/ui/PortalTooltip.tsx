import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import styles from './PortalTooltip.module.scss'

const TOOLTIP_MAX_WIDTH = 280
const TOOLTIP_ESTIMATED_HEIGHT = 72
const TOOLTIP_OFFSET = 10
const TOOLTIP_VIEWPORT_PADDING = 8

type TooltipPlacementInput = {
  anchorTop: number
  anchorBottom: number
  viewportHeight: number
  tooltipHeight: number
}

type TooltipPlacement = {
  placement: 'above' | 'below'
  y: number
}

const resolveTooltipPlacement = ({
  anchorTop,
  anchorBottom,
  viewportHeight,
  tooltipHeight,
}: TooltipPlacementInput): TooltipPlacement => {
  const spaceBelow = viewportHeight - anchorBottom - TOOLTIP_OFFSET - TOOLTIP_VIEWPORT_PADDING
  const spaceAbove = anchorTop - TOOLTIP_OFFSET - TOOLTIP_VIEWPORT_PADDING
  const placement = spaceBelow >= tooltipHeight || spaceBelow >= spaceAbove ? 'below' : 'above'
  return {
    placement,
    y: placement === 'above' ? anchorTop - TOOLTIP_OFFSET : anchorBottom + TOOLTIP_OFFSET,
  }
}

export type PortalTooltipState = {
  lines: string[]
  x: number
  y: number
  placement: 'above' | 'below'
  anchorTop?: number
  anchorBottom?: number
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
    const { placement, y } = resolveTooltipPlacement({
      anchorTop: rect.top,
      anchorBottom: rect.bottom,
      viewportHeight,
      tooltipHeight: TOOLTIP_ESTIMATED_HEIGHT,
    })

    setTooltip({ lines: target.lines, x, y, placement, anchorTop: rect.top, anchorBottom: rect.bottom })
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
  const tooltipRef = useRef<HTMLDivElement | null>(null)
  const [measuredPosition, setMeasuredPosition] = useState<{
    tooltip: PortalTooltipState
    x: number
    placement: 'above' | 'below'
    y: number
  } | null>(null)

  useLayoutEffect(() => {
    if (!tooltip || tooltip.anchorTop === undefined || tooltip.anchorBottom === undefined) {
      return
    }

    const renderedTooltip = tooltipRef.current
    if (!renderedTooltip) return
    const measuredHeight = renderedTooltip.getBoundingClientRect().height
    if (!Number.isFinite(measuredHeight) || measuredHeight <= 0) return

    const viewportHeight = typeof window === 'undefined' ? 768 : window.innerHeight
    const nextPosition = resolveTooltipPlacement({
      anchorTop: tooltip.anchorTop,
      anchorBottom: tooltip.anchorBottom,
      viewportHeight,
      tooltipHeight: measuredHeight,
    })
    setMeasuredPosition((current) => (
      current?.tooltip === tooltip
      && current.x === tooltip.x
      && current.placement === nextPosition.placement
      && current.y === nextPosition.y
        ? current
        : { tooltip, x: tooltip.x, ...nextPosition }
    ))
  }, [tooltip])

  if (!tooltip || typeof document === 'undefined') return null

  const position = measuredPosition?.tooltip === tooltip ? measuredPosition : tooltip

  return createPortal(
    <div
      ref={tooltipRef}
      className={styles.tooltip}
      role="tooltip"
      style={{
        left: position.x,
        top: position.y,
        transform: position.placement === 'above'
          ? 'translate(-50%, -100%)'
          : 'translateX(-50%)',
      }}
    >
      {tooltip.lines.map((line, index) => <span key={`${index}-${line}`}>{line}</span>)}
    </div>,
    document.body,
  )
}
