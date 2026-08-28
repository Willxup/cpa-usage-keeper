import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type CSSProperties,
  type HTMLAttributes,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from 'react'
import { createPortal } from 'react-dom'
import { QuestionMarkHelpButton } from './QuestionMarkHelpButton'
import styles from './QuestionMarkHelp.module.scss'

const POINTER_TRANSFER_GRACE_MS = 120

type DataAttributes = {
  [key: `data-${string}`]: string | number | boolean | undefined
}

type HelpButtonProps = Omit<
  ButtonHTMLAttributes<HTMLButtonElement>,
  | 'aria-controls'
  | 'aria-describedby'
  | 'aria-expanded'
  | 'aria-label'
  | 'children'
  | 'className'
  | 'onBlur'
  | 'onClick'
  | 'onFocus'
  | 'onPointerDown'
  | 'type'
> & DataAttributes

type HelpTooltipProps = Omit<
  HTMLAttributes<HTMLSpanElement>,
  | 'aria-hidden'
  | 'children'
  | 'className'
  | 'id'
  | 'onPointerEnter'
  | 'onPointerLeave'
  | 'role'
  | 'style'
> & DataAttributes

interface QuestionMarkHelpPositioning {
  align?: 'center' | 'start'
  constrainHeight?: boolean
  estimatedHeight?: number
  maxWidth?: number
  offset?: number
  viewportPadding?: number
}

interface QuestionMarkHelpProps {
  label: string
  description: string
  children: ReactNode
  className?: string
  buttonClassName?: string
  tooltipClassName?: string
  tooltipVisibleClassName?: string
  portal?: boolean
  positioning?: QuestionMarkHelpPositioning
  buttonProps?: HelpButtonProps
  tooltipProps?: HelpTooltipProps
}

interface TooltipPosition {
  left: number
  top: number
  width?: number
  maxHeight?: number
  placement: 'above' | 'below'
}

const useIsomorphicLayoutEffect = typeof window === 'undefined' ? useEffect : useLayoutEffect

export function QuestionMarkHelp({
  label,
  description,
  children,
  className,
  buttonClassName,
  tooltipClassName = styles.tooltip,
  tooltipVisibleClassName = styles.tooltipVisible,
  portal = true,
  positioning,
  buttonProps,
  tooltipProps,
}: QuestionMarkHelpProps) {
  const descriptionId = useId()
  const tooltipId = useId()
  const rootRef = useRef<HTMLSpanElement | null>(null)
  const buttonRef = useRef<HTMLButtonElement | null>(null)
  const tooltipRef = useRef<HTMLSpanElement | null>(null)
  const hoverCloseTimerRef = useRef<number | null>(null)
  const [hovered, setHovered] = useState(false)
  const [focused, setFocused] = useState(false)
  const [touchOpen, setTouchOpen] = useState(false)
  const [position, setPosition] = useState<TooltipPosition>({
    left: positioning?.viewportPadding ?? 16,
    top: positioning?.viewportPadding ?? 16,
    placement: 'below',
  })
  const visible = hovered || focused || touchOpen

  const cancelScheduledHoverClose = useCallback(() => {
    if (hoverCloseTimerRef.current === null) return
    window.clearTimeout(hoverCloseTimerRef.current)
    hoverCloseTimerRef.current = null
  }, [])

  const openForPointer = useCallback((event: ReactPointerEvent) => {
    if (event.pointerType === 'touch') return
    cancelScheduledHoverClose()
    setHovered(true)
  }, [cancelScheduledHoverClose])

  const schedulePointerClose = useCallback((event: ReactPointerEvent) => {
    if (event.pointerType === 'touch') return
    cancelScheduledHoverClose()
    hoverCloseTimerRef.current = window.setTimeout(() => {
      hoverCloseTimerRef.current = null
      setHovered(false)
    }, POINTER_TRANSFER_GRACE_MS)
  }, [cancelScheduledHoverClose])

  const closeTouchState = useCallback(() => {
    setTouchOpen(false)
    setFocused(false)
    buttonRef.current?.blur()
  }, [])

  const updatePosition = useCallback(() => {
    if (!portal) return
    const button = buttonRef.current
    if (!button?.isConnected) return

    const viewportPadding = positioning?.viewportPadding ?? 16
    const offset = positioning?.offset ?? 8
    const estimatedHeight = positioning?.estimatedHeight ?? 144
    const maxWidth = positioning?.maxWidth ?? 420
    const align = positioning?.align ?? 'start'
    const viewportWidth = window.innerWidth
    const viewportHeight = window.innerHeight
    const rect = button.getBoundingClientRect()
    const width = Math.min(maxWidth, Math.max(0, viewportWidth - viewportPadding * 2))
    const spaceBelow = Math.max(0, viewportHeight - rect.bottom - offset - viewportPadding)
    const spaceAbove = Math.max(0, rect.top - offset - viewportPadding)
    const placement = spaceBelow >= estimatedHeight || spaceBelow >= spaceAbove ? 'below' : 'above'

    let left: number
    if (align === 'center') {
      const halfWidth = width / 2
      const minX = viewportPadding + halfWidth
      const maxX = viewportWidth - viewportPadding - halfWidth
      const anchorX = rect.left + rect.width / 2
      left = maxX >= minX ? Math.max(minX, Math.min(anchorX, maxX)) : viewportWidth / 2
    } else {
      const maxLeft = Math.max(viewportPadding, viewportWidth - viewportPadding - width)
      left = Math.max(viewportPadding, Math.min(rect.left, maxLeft))
    }

    setPosition({
      left,
      top: placement === 'above' ? rect.top - offset : rect.bottom + offset,
      width: align === 'start' ? width : undefined,
      maxHeight: positioning?.constrainHeight ? (placement === 'above' ? spaceAbove : spaceBelow) : undefined,
      placement,
    })
  }, [portal, positioning])

  useIsomorphicLayoutEffect(() => {
    if (visible) updatePosition()
  }, [updatePosition, visible])

  useEffect(() => {
    if (!portal || !visible) return
    window.addEventListener('resize', updatePosition)
    window.addEventListener('scroll', updatePosition, true)
    return () => {
      window.removeEventListener('resize', updatePosition)
      window.removeEventListener('scroll', updatePosition, true)
    }
  }, [portal, updatePosition, visible])

  useEffect(() => () => cancelScheduledHoverClose(), [cancelScheduledHoverClose])

  useEffect(() => {
    if (!touchOpen) return
    const closeOnOutsidePointer = (event: PointerEvent) => {
      const target = event.target
      if (!(target instanceof Node)) return
      if (rootRef.current?.contains(target) || tooltipRef.current?.contains(target)) return
      closeTouchState()
    }
    document.addEventListener('pointerdown', closeOnOutsidePointer)
    return () => document.removeEventListener('pointerdown', closeOnOutsidePointer)
  }, [closeTouchState, touchOpen])

  const tooltipStyle: CSSProperties | undefined = portal ? {
    position: 'fixed',
    left: position.left,
    top: position.top,
    width: position.width,
    maxHeight: position.maxHeight,
    transform: position.placement === 'above'
      ? positioning?.align === 'center' ? 'translate(-50%, -100%)' : 'translateY(-100%)'
      : positioning?.align === 'center' ? 'translateX(-50%)' : 'translateY(0)',
  } : undefined

  const tooltip = (
    <span
      {...tooltipProps}
      ref={tooltipRef}
      id={tooltipId}
      role="tooltip"
      aria-hidden={!visible}
      className={`${tooltipClassName} ${visible ? tooltipVisibleClassName : ''}`.trim()}
      style={tooltipStyle}
      onPointerEnter={openForPointer}
      onPointerLeave={schedulePointerClose}
    >
      {children}
    </span>
  )

  return (
    <span
      ref={rootRef}
      className={`${styles.root} ${className ?? ''}`.trim()}
      onPointerEnter={openForPointer}
      onPointerLeave={schedulePointerClose}
    >
      <QuestionMarkHelpButton
        {...buttonProps}
        ref={buttonRef}
        className={buttonClassName}
        aria-label={label}
        aria-describedby={descriptionId}
        aria-controls={tooltipId}
        aria-expanded={visible}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        onPointerDown={(event) => {
          if (event.pointerType !== 'touch') return
          event.preventDefault()
          if (touchOpen) {
            closeTouchState()
            return
          }
          setTouchOpen(true)
        }}
      />
      <span id={descriptionId} className={styles.screenReaderOnly}>{description}</span>
      {!portal || typeof document === 'undefined' ? tooltip : createPortal(tooltip, document.body)}
    </span>
  )
}
