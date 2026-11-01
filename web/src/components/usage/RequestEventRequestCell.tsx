import { useEffect, useRef, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import type { usePortalTooltip } from '@/components/ui/PortalTooltip'
import { IconStreamOutput } from '@/components/ui/IconStreamOutput'
import type { UsageEvent } from '@/lib/types'
import styles from './RequestEventRequestCell.module.scss'

type TooltipActions = Pick<ReturnType<typeof usePortalTooltip>,
  'showOnMouseEnter' | 'hideOnMouseLeave' | 'showOnFocus' | 'hideOnBlur'>

export function RequestEventRequestCell({
  event,
  requestType,
  className,
  tooltipActions,
  showReasoningEffort = false,
  children,
}: {
  event: UsageEvent
  requestType: string
  className: string
  tooltipActions: TooltipActions
  showReasoningEffort?: boolean
  children: ReactNode
}) {
  const { t } = useTranslation()
  const cellRef = useRef<HTMLTableCellElement | null>(null)
  const { hideOnMouseLeave, hideOnBlur } = tooltipActions

  useEffect(() => {
    const cell = cellRef.current
    return () => {
      if (!cell) return
      // 虚拟行卸载时清理浮层，避免继续显示已移除请求的信息。
      hideOnMouseLeave(cell)
      hideOnBlur(cell)
    }
  }, [hideOnMouseLeave, hideOnBlur])

  const line = (label: string, value: string) => t('usage_stats.request_events_metric_tooltip_line', {
    label: t(label),
    value,
  })
  // 浮层使用原始端点，保留 HTTP 方法和归一化前的完整路径。
  const responseModeKey = event.stream === true
    ? 'usage_stats.request_events_streaming'
    : event.stream === false
      ? 'usage_stats.request_events_non_streaming'
      : 'usage_stats.request_events_stream_unknown'
  const tooltipLines = [
    line('usage_stats.request_events_request', requestType),
    line('usage_stats.request_endpoint', event.endpoint?.trim() || '-'),
    line('usage_stats.request_events_response_mode', t(responseModeKey)),
  ]
  if (showReasoningEffort && event.reasoning_effort?.trim()) {
    tooltipLines.push(line('usage_stats.reasoning_effort', event.reasoning_effort.trim()))
  }

  return (
    <td
      ref={cellRef}
      className={className}
      tabIndex={0}
      aria-label={tooltipLines.join('; ')}
      onMouseEnter={(e) => tooltipActions.showOnMouseEnter(tooltipLines, e.currentTarget)}
      onMouseLeave={(e) => hideOnMouseLeave(e.currentTarget)}
      onFocus={(e) => tooltipActions.showOnFocus(tooltipLines, e.currentTarget)}
      onBlur={(e) => hideOnBlur(e.currentTarget)}
    >
      {children}
    </td>
  )
}

export function RequestEventRequestType({ requestType, stream, iconSize = 14 }: {
  requestType: string
  stream: UsageEvent['stream']
  iconSize?: number
}) {
  return (
    <span className={styles.requestType}>
      {requestType}
      {stream === true ? <IconStreamOutput size={iconSize} className={styles.streamIcon} aria-hidden="true" /> : null}
    </span>
  )
}
