import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
  type UIEvent,
} from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/components/ui/EmptyState'
import { PortalTooltip, usePortalTooltip } from '@/components/ui/PortalTooltip'
import { IconChevronDown, IconChevronRight } from '@/components/ui/icons'
import { useScrollBoundaryContainment } from '@/hooks/useScrollBoundaryContainment'
import type { UsageEvent } from '@/lib/types'
import { calculateCacheReadRate, formatDurationMs, formatUsd } from '@/utils/usage'
import { RequestEventResultBadge } from '@/components/usage/RequestEventResultBadge'
import styles from './CredentialRequestEventsList.module.scss'

const LOAD_MORE_THRESHOLD_PX = 320
const VIRTUALIZATION_THRESHOLD = 50
const VIRTUAL_ROW_HEIGHT = 70
const VIRTUAL_OVERSCAN = 8
const VIRTUAL_INITIAL_VIEWPORT_HEIGHT = 600
const INTEGER_FORMATTER = new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 })

interface CredentialRequestEventsListProps {
  events: UsageEvent[]
  loading: boolean
  hasMore: boolean
  loadingMore: boolean
  autoLoadMore: boolean
  onLoadMore: () => void
  requestLogAccessEnabled?: boolean
  onRequestLogOpen?: (event: UsageEvent) => void
  requestLogLoadingEventId?: string | null
}

interface CredentialRequestEventRow {
  event: UsageEvent
  id: string
  requestId: string
  detailsId: string
  timestamp: string
  timestampTimeLabel: string
  timestampDateLabel: string
  apiKey: string
  requestTier: string
  responseTier: string
  model: string
  modelAlias: string
  reasoningEffort: string
  requestType: string
  endpoint: string
  failed: boolean
  inputTokens: string
  outputTokens: string
  reasoningTokens: string
  cacheReadTokens: string
  cacheCreationTokens: string
  cacheReadRate: string
  totalTokens: string
  latency: string
  ttft: string
  speed: string
  cost: string
  pricingStyle: string
  executorType: string
  clientIP: string
  xForwardedFor: string
  userAgent: string
  canExpand: boolean
}

type OverflowTooltipActions = Pick<ReturnType<typeof usePortalTooltip>,
  'showOnMouseEnter' | 'hideOnMouseLeave' | 'showOnFocus' | 'hideOnBlur'>

interface OverflowTooltipTextProps {
  as: 'span' | 'strong' | 'small'
  tooltipText: string
  tooltipActions: OverflowTooltipActions
  children?: ReactNode
  className?: string
  disabled?: boolean
}

function OverflowTooltipText({
  as,
  tooltipText,
  tooltipActions,
  children = tooltipText,
  className,
  disabled = false,
}: OverflowTooltipTextProps) {
  const anchorRef = useRef<HTMLElement | null>(null)
  const overflowingRef = useRef(false)
  const [overflowing, setOverflowing] = useState(false)

  const measureOverflow = useCallback(() => {
    const anchor = anchorRef.current
    if (!anchor) return
    const nextOverflowing = !disabled && (
      anchor.scrollWidth > anchor.clientWidth || anchor.scrollHeight > anchor.clientHeight
    )
    if (overflowingRef.current && !nextOverflowing) {
      tooltipActions.hideOnMouseLeave(anchor)
      tooltipActions.hideOnBlur(anchor)
    }
    overflowingRef.current = nextOverflowing
    setOverflowing((current) => current === nextOverflowing ? current : nextOverflowing)
  }, [disabled, tooltipActions])

  useLayoutEffect(() => {
    const anchor = anchorRef.current
    if (!anchor) return
    const frameId = window.requestAnimationFrame(measureOverflow)
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(measureOverflow)
    observer?.observe(anchor)
    return () => {
      window.cancelAnimationFrame(frameId)
      observer?.disconnect()
      tooltipActions.hideOnMouseLeave(anchor)
      tooltipActions.hideOnBlur(anchor)
    }
  }, [measureOverflow, tooltipActions, tooltipText])

  const interactive = overflowing && !disabled
  const sharedProps = {
    className,
    tabIndex: interactive ? 0 : undefined,
    'aria-label': interactive ? tooltipText : undefined,
    'data-credential-request-overflow-target': 'true',
    onMouseEnter: interactive
      ? (event: React.MouseEvent<HTMLElement>) => tooltipActions.showOnMouseEnter([tooltipText], event.currentTarget)
      : undefined,
    onMouseLeave: interactive
      ? (event: React.MouseEvent<HTMLElement>) => tooltipActions.hideOnMouseLeave(event.currentTarget)
      : undefined,
    onFocus: interactive
      ? (event: React.FocusEvent<HTMLElement>) => tooltipActions.showOnFocus([tooltipText], event.currentTarget)
      : undefined,
    onBlur: interactive
      ? (event: React.FocusEvent<HTMLElement>) => tooltipActions.hideOnBlur(event.currentTarget)
      : undefined,
  }
  if (as === 'strong') return <strong ref={anchorRef} {...sharedProps}>{children}</strong>
  if (as === 'small') return <small ref={anchorRef} {...sharedProps}>{children}</small>
  return <span ref={anchorRef} {...sharedProps}>{children}</span>
}

const SPEED_MODE_LABEL_KEYS: Record<string, string> = {
  auto: 'usage_stats.speed_mode_auto',
  default: 'usage_stats.speed_mode_standard',
  standard: 'usage_stats.speed_mode_standard',
  priority: 'usage_stats.speed_mode_fast',
  fast: 'usage_stats.speed_mode_fast',
  flex: 'usage_stats.speed_mode_flex',
}

const toNumber = (value: unknown): number => {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? Math.max(parsed, 0) : 0
}

const formatTimestamp = (timestamp: string): { time: string; date: string } => {
  const match = timestamp.match(/^(\d{4})-(\d{2})-(\d{2})[T\s](\d{2}):(\d{2}):(\d{2})/)
  if (!match) return { time: timestamp || '-', date: '' }
  return {
    time: `${match[4]}:${match[5]}:${match[6]}`,
    date: `${match[1]}/${match[2]}/${match[3]}`,
  }
}

const formatSpeedMode = (value: unknown, t: (key: string) => string): string => {
  const normalized = String(value ?? '').trim()
  if (!normalized) return '-'
  const labelKey = SPEED_MODE_LABEL_KEYS[normalized.toLowerCase()]
  return labelKey ? t(labelKey) : normalized
}

const formatSpeed = (value: unknown): string => {
  const speed = Number(value)
  return Number.isFinite(speed) && speed > 0 ? `${speed.toFixed(1)} t/s` : '-'
}

const formatCacheRate = (inputTokens: number, cacheReadTokens: number): string => {
  const rate = calculateCacheReadRate({ inputTokens, cacheReadTokens })
  return rate === null ? '-' : `${rate.toFixed(2)}%`
}

const optionalText = (value: unknown): string => String(value ?? '').trim() || '-'

const parseRequestEndpoint = (rawEndpoint: unknown): { requestType: string; endpoint: string } => {
  const raw = String(rawEndpoint ?? '').trim().replace(/\s+/g, ' ')
  if (!raw) return { requestType: '-', endpoint: '-' }
  const [first, ...rest] = raw.split(' ')
  const upperMethod = first.toUpperCase()
  const hasMethod = upperMethod === 'GET' || upperMethod === 'POST'
  const requestType = upperMethod === 'POST' ? 'SSE' : upperMethod === 'GET' ? 'WS' : '-'
  const path = hasMethod ? rest.join(' ').trim() : raw
  const normalizedPath = path.startsWith('/v1/') ? path.slice(3) : path === '/v1' ? '/' : path
  return { requestType, endpoint: normalizedPath || '-' }
}

const buildRow = (
  event: UsageEvent,
  index: number,
  t: (key: string) => string,
): CredentialRequestEventRow => {
  const timestamp = String(event.timestamp ?? '')
  const model = String(event.model ?? '').trim() || '-'
  const modelAliasValue = String(event.model_alias ?? '').trim()
  const endpoint = parseRequestEndpoint(event.endpoint)
  const latencyMs = Number.isFinite(event.latency_ms) ? event.latency_ms : null
  const ttftMs = Number.isFinite(event.ttft_ms) ? event.ttft_ms as number : null
  const costAvailable = event.cost_available === true
  const inputTokens = toNumber(event.tokens?.input_tokens)
  const cacheReadTokens = toNumber(event.tokens?.cache_read_tokens)
  const apiKey = optionalText(event.api_key)
  const requestTier = formatSpeedMode(event.service_tier, t)
  const responseTier = formatSpeedMode(event.response_service_tier, t)
  const executorType = optionalText(event.executor_type)
  const clientIP = optionalText(event.client_ip)
  const xForwardedFor = optionalText(event.x_forwarded_for)
  const userAgent = optionalText(event.user_agent)
  const pricingStyle = event.pricing_style === 'claude'
    ? t('usage_stats.credentials_detail_pricing_style_claude')
    : event.pricing_style === 'openai'
      ? t('usage_stats.credentials_detail_pricing_style_openai')
      : '-'
  const timestampLabels = formatTimestamp(timestamp)

  return {
    event,
    id: String(event.id ?? '').trim() || `${timestamp}-${model}-${index}`,
    requestId: String(event.request_id ?? '').trim(),
    detailsId: `credential-request-event-details-${index}`,
    timestamp,
    timestampTimeLabel: timestampLabels.time,
    timestampDateLabel: timestampLabels.date,
    apiKey,
    requestTier,
    responseTier,
    model,
    modelAlias: modelAliasValue && modelAliasValue !== model ? modelAliasValue : '-',
    reasoningEffort: optionalText(event.reasoning_effort),
    requestType: endpoint.requestType,
    endpoint: endpoint.endpoint,
    failed: event.failed === true,
    inputTokens: INTEGER_FORMATTER.format(inputTokens),
    outputTokens: INTEGER_FORMATTER.format(toNumber(event.tokens?.output_tokens)),
    reasoningTokens: INTEGER_FORMATTER.format(toNumber(event.tokens?.reasoning_tokens)),
    cacheReadTokens: INTEGER_FORMATTER.format(cacheReadTokens),
    cacheCreationTokens: INTEGER_FORMATTER.format(toNumber(event.tokens?.cache_creation_tokens)),
    cacheReadRate: formatCacheRate(inputTokens, cacheReadTokens),
    totalTokens: INTEGER_FORMATTER.format(toNumber(event.tokens?.total_tokens)),
    latency: formatDurationMs(latencyMs),
    ttft: ttftMs && ttftMs > 0 ? formatDurationMs(ttftMs) : '-',
    speed: formatSpeed(event.speed_tps),
    cost: costAvailable ? formatUsd(toNumber(event.cost_usd)) : '-',
    pricingStyle,
    executorType,
    clientIP,
    xForwardedFor,
    userAgent,
    canExpand: [apiKey, requestTier, responseTier, executorType, clientIP, xForwardedFor, userAgent]
      .some((value) => value !== '-'),
  }
}

export const shouldLoadMoreCredentialRequestEvents = ({
  scrollTop,
  clientHeight,
  scrollHeight,
  threshold = LOAD_MORE_THRESHOLD_PX,
}: {
  scrollTop: number
  clientHeight: number
  scrollHeight: number
  threshold?: number
}): boolean => scrollHeight - scrollTop - clientHeight <= threshold

export function CredentialRequestEventsList({
  events,
  loading,
  hasMore,
  loadingMore,
  autoLoadMore,
  onLoadMore,
  requestLogAccessEnabled = false,
  onRequestLogOpen,
  requestLogLoadingEventId = null,
}: CredentialRequestEventsListProps) {
  const { t } = useTranslation()
  const scrollerRef = useRef<HTMLDivElement | null>(null)
  const previousExpandedRowIdRef = useRef<string | null>(null)
  const [expandedRowId, setExpandedRowId] = useState<string | null>(null)
  const {
    tooltip,
    showOnMouseEnter: showTooltipOnMouseEnter,
    hideOnMouseLeave: hideTooltipOnMouseLeave,
    showOnFocus: showTooltipOnFocus,
    hideOnBlur: hideTooltipOnBlur,
    dismiss: dismissTooltip,
  } = usePortalTooltip()
  const tooltipActions = useMemo<OverflowTooltipActions>(() => ({
    showOnMouseEnter: showTooltipOnMouseEnter,
    hideOnMouseLeave: hideTooltipOnMouseLeave,
    showOnFocus: showTooltipOnFocus,
    hideOnBlur: hideTooltipOnBlur,
  }), [
    hideTooltipOnBlur,
    hideTooltipOnMouseLeave,
    showTooltipOnFocus,
    showTooltipOnMouseEnter,
  ])
  const tooltipVisible = tooltip !== null

  useEffect(() => {
    if (!tooltipVisible) return
    const handleTooltipEscape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || !dismissTooltip()) return
      event.preventDefault()
      event.stopPropagation()
    }
    document.addEventListener('keydown', handleTooltipEscape, true)
    return () => document.removeEventListener('keydown', handleTooltipEscape, true)
  }, [dismissTooltip, tooltipVisible])

  const rows = useMemo(() => events.map((event, index) => buildRow(event, index, t)), [events, t])
  const virtualizeRows = rows.length >= VIRTUALIZATION_THRESHOLD
  // TanStack Virtual 依赖内部可变测量状态，不参与 React Compiler 自动记忆化。
  // eslint-disable-next-line react-hooks/incompatible-library
  const rowVirtualizer = useVirtualizer({
    count: virtualizeRows ? rows.length : 0,
    getScrollElement: () => scrollerRef.current,
    estimateSize: () => VIRTUAL_ROW_HEIGHT,
    overscan: VIRTUAL_OVERSCAN,
    getItemKey: (index) => rows[index]?.id ?? index,
    initialRect: { width: 0, height: VIRTUAL_INITIAL_VIEWPORT_HEIGHT },
    useAnimationFrameWithResizeObserver: true,
  })
  const virtualRows = rowVirtualizer.getVirtualItems()
  const virtualPaddingTop = virtualRows.length > 0 ? virtualRows[0].start : 0
  const virtualPaddingBottom = virtualRows.length > 0
    ? Math.max(rowVirtualizer.getTotalSize() - virtualRows[virtualRows.length - 1].end, 0)
    : 0
  useScrollBoundaryContainment(scrollerRef, rows.length > 0)

  useLayoutEffect(() => {
    const previousExpandedRowId = previousExpandedRowIdRef.current
    previousExpandedRowIdRef.current = expandedRowId
    if (!virtualizeRows || previousExpandedRowId === null || previousExpandedRowId === expandedRowId) return
    const previousIndex = rows.findIndex((row) => row.id === previousExpandedRowId)
    if (previousIndex < 0) return
    // 定点恢复上一展开项，让虚拟器同步补偿视口上方的高度变化，避免总高度累积和滚动跳动。
    rowVirtualizer.resizeItem(previousIndex, VIRTUAL_ROW_HEIGHT)
  }, [expandedRowId, rowVirtualizer, rows, virtualizeRows])

  useEffect(() => {
    if (expandedRowId && !rows.some((row) => row.id === expandedRowId)) {
      setExpandedRowId(null)
    }
  }, [expandedRowId, rows])

  const tryLoadMore = useCallback((scroller: Pick<HTMLDivElement, 'scrollTop' | 'clientHeight' | 'scrollHeight'>) => {
    if (!autoLoadMore || !hasMore || loading || loadingMore) return
    if (shouldLoadMoreCredentialRequestEvents(scroller)) onLoadMore()
  }, [autoLoadMore, hasMore, loading, loadingMore, onLoadMore])

  const handleScroll = useCallback((event: UIEvent<HTMLDivElement>) => {
    tryLoadMore(event.currentTarget)
  }, [tryLoadMore])

  const toggleRow = useCallback((row: CredentialRequestEventRow) => {
    if (!row.canExpand) return
    setExpandedRowId((current) => current === row.id ? null : row.id)
  }, [])

  useEffect(() => {
    const scroller = scrollerRef.current
    if (scroller) tryLoadMore(scroller)
  }, [rows.length, tryLoadMore])

  if (loading && rows.length === 0) {
    return <div className={styles.state} role="status">{t('common.loading')}</div>
  }

  if (rows.length === 0) {
    return (
      <div className={styles.emptyState}>
        <EmptyState
          title={t('usage_stats.request_events_empty_title')}
          description={t('usage_stats.request_events_empty_desc')}
        />
      </div>
    )
  }

  const renderOverflowText = (
    as: OverflowTooltipTextProps['as'],
    tooltipText: string,
    children: ReactNode = tooltipText,
    className?: string,
  ) => (
    <OverflowTooltipText
      as={as}
      tooltipText={tooltipText}
      tooltipActions={tooltipActions}
      className={className}
      disabled={tooltipText === '-'}
    >
      {children}
    </OverflowTooltipText>
  )

  const renderLabeledOverflowText = (label: string, value: string) => renderOverflowText(
    'small',
    `${label} ${value}`,
    <>
      <span className={styles.subDataLabel} data-credential-request-sub-label>{label}</span>
      {' '}{value}
    </>,
  )

  const renderRow = (row: CredentialRequestEventRow, virtualIndex?: number) => {
    const canOpenLog = Boolean(requestLogAccessEnabled && row.requestId && onRequestLogOpen)
    const logLoading = requestLogLoadingEventId === row.id
    const expanded = expandedRowId === row.id
    return (
      <tbody
        key={row.id}
        ref={virtualIndex === undefined ? undefined : rowVirtualizer.measureElement}
        data-index={virtualIndex}
        data-credential-request-event-group={row.id}
      >
        <tr
          className={`${styles.eventRow} ${row.canExpand ? styles.expandableRow : ''} ${expanded ? styles.expandedRow : ''}`.trim()}
          data-index={virtualIndex}
          aria-rowindex={virtualIndex === undefined ? undefined : virtualIndex + 2}
          data-credential-request-event-id={row.id}
          onClick={row.canExpand ? () => toggleRow(row) : undefined}
        >
          <td
            className={styles.timestamp}
            data-credential-request-timestamp={row.id}
          >
            {row.canExpand ? (
              <button
                type="button"
                className={styles.rowToggle}
                data-credential-request-event-toggle={row.id}
                aria-expanded={expanded}
                aria-controls={row.detailsId}
                aria-label={t(expanded
                  ? 'usage_stats.credentials_detail_collapse_request'
                  : 'usage_stats.credentials_detail_expand_request')}
                onClick={(event) => {
                  event.stopPropagation()
                  toggleRow(row)
                }}
              >
                {expanded
                  ? <IconChevronDown size={12} aria-hidden="true" />
                  : <IconChevronRight size={12} aria-hidden="true" />}
                <span className={styles.timestampValue}>
                  <strong>{row.timestampTimeLabel}</strong>
                  {row.timestampDateLabel ? <small>{row.timestampDateLabel}</small> : null}
                </span>
              </button>
            ) : (
              <span className={styles.timestampValue}>
                <strong>{row.timestampTimeLabel}</strong>
                {row.timestampDateLabel ? <small>{row.timestampDateLabel}</small> : null}
              </span>
            )}
          </td>
          <td
            className={`${styles.stackedCell} ${styles.model}`.trim()}
            data-credential-request-model={row.id}
          >
            {renderOverflowText('strong', row.model)}
            {renderOverflowText('small', row.modelAlias)}
            {renderLabeledOverflowText(t('usage_stats.reasoning_effort'), row.reasoningEffort)}
          </td>
          <td className={`${styles.stackedCell} ${styles.request}`.trim()}>
            {renderOverflowText('strong', row.requestType)}
            {renderOverflowText('small', row.endpoint)}
          </td>
          <td className={styles.resultColumn}>
            <RequestEventResultBadge
              failed={row.failed}
              loading={logLoading}
              onOpen={canOpenLog ? () => {
                dismissTooltip()
                onRequestLogOpen?.(row.event)
              } : undefined}
              dataAttributes={canOpenLog ? { 'data-credential-request-log': row.id } : undefined}
              size="compact"
            />
          </td>
          <td className={`${styles.stackedCell} ${styles.tokens}`.trim()}>
            {renderOverflowText('strong', row.totalTokens)}
            {renderLabeledOverflowText(t('usage_stats.input_tokens'), row.inputTokens)}
            {renderLabeledOverflowText(
              t('usage_stats.output_tokens'),
              `${row.outputTokens} (${t('usage_stats.reasoning_tokens')} ${row.reasoningTokens})`,
            )}
          </td>
          <td className={`${styles.stackedCell} ${styles.cache}`.trim()}>
            {renderOverflowText('strong', row.cacheReadRate)}
            {renderLabeledOverflowText(t('usage_stats.credentials_detail_cache_read'), row.cacheReadTokens)}
            {renderLabeledOverflowText(t('usage_stats.credentials_detail_cache_write'), row.cacheCreationTokens)}
          </td>
          <td className={`${styles.stackedCell} ${styles.performance}`.trim()}>
            {renderOverflowText('strong', row.latency)}
            {renderLabeledOverflowText(t('usage_stats.ttft'), row.ttft)}
            {renderLabeledOverflowText(t('usage_stats.speed'), row.speed)}
          </td>
          <td className={`${styles.stackedCell} ${styles.cost}`.trim()}>
            {renderOverflowText('strong', row.cost)}
            {renderOverflowText('small', row.pricingStyle)}
          </td>
        </tr>
        {expanded ? (
          <tr
            id={row.detailsId}
            className={styles.detailRow}
            data-credential-request-event-details={row.id}
          >
            <td colSpan={8}>
              <div className={styles.detailLayout}>
                <section className={styles.detailGroup} data-credential-request-detail-group="request">
                  <h4>{t('usage_stats.credentials_detail_request_context')}</h4>
                  <div className={styles.detailGrid}>
                    <div className={styles.detailItem} data-credential-request-detail-item>
                      <span>{t('usage_stats.api_key_filter')}</span>
                      {renderOverflowText('strong', row.apiKey)}
                    </div>
                    <div className={styles.detailItem} data-credential-request-detail-item>
                      <span>{t('usage_stats.credentials_detail_request_tier')}</span>
                      {renderOverflowText('strong', row.requestTier)}
                    </div>
                    <div className={styles.detailItem} data-credential-request-detail-item>
                      <span>{t('usage_stats.credentials_detail_response_tier')}</span>
                      {renderOverflowText('strong', row.responseTier)}
                    </div>
                    <div className={styles.detailItem} data-credential-request-detail-item>
                      <span>{t('usage_stats.credentials_detail_executor')}</span>
                      {renderOverflowText('strong', row.executorType)}
                    </div>
                  </div>
                </section>
                <section className={styles.detailGroup} data-credential-request-detail-group="client">
                  <h4>{t('usage_stats.credentials_detail_client_context')}</h4>
                  <div className={styles.detailGrid}>
                    <div className={styles.detailItem} data-credential-request-detail-item>
                      <span>{t('usage_stats.client_ip')}</span>
                      {renderOverflowText('strong', row.clientIP)}
                    </div>
                    <div className={styles.detailItem} data-credential-request-detail-item>
                      <span>{t('usage_stats.x_forwarded_for')}</span>
                      {renderOverflowText('strong', row.xForwardedFor)}
                    </div>
                    <div className={styles.detailItem} data-credential-request-detail-item>
                      <span>{t('usage_stats.user_agent')}</span>
                      {renderOverflowText('strong', row.userAgent, row.userAgent, styles.detailUserAgentValue)}
                    </div>
                  </div>
                </section>
              </div>
            </td>
          </tr>
        ) : null}
      </tbody>
    )
  }

  return (
    <div className={styles.root} data-credential-request-events-list="true">
      <div
        ref={scrollerRef}
        className={styles.scroller}
        onScroll={handleScroll}
        data-credential-request-events-scroller="true"
        data-virtualized={virtualizeRows}
        data-loaded-row-count={rows.length}
      >
        <table className={styles.table} aria-rowcount={rows.length + 1}>
          <thead>
            <tr>
              <th className={styles.timestamp}>{t('usage_stats.request_events_timestamp')}</th>
              <th className={styles.model}>{t('usage_stats.model_name')}</th>
              <th className={styles.request}>{t('usage_stats.request_type')}</th>
              <th className={styles.resultColumn}>{t('usage_stats.request_events_result')}</th>
              <th className={styles.tokens}>{t('usage_stats.total_tokens')}</th>
              <th className={styles.cache}>{t('usage_stats.credentials_detail_cache_column')}</th>
              <th className={styles.performance}>{t('usage_stats.latency')}</th>
              <th className={styles.cost}>{t('usage_stats.total_cost')}</th>
            </tr>
          </thead>
          {virtualizeRows ? (
            <>
              {virtualPaddingTop > 0 ? (
                <tbody aria-hidden="true">
                  <tr className={styles.virtualSpacerRow} style={{ height: `${virtualPaddingTop}px` }} aria-hidden="true" data-credential-request-events-spacer>
                    <td colSpan={8} />
                  </tr>
                </tbody>
              ) : null}
              {virtualRows.map((virtualRow) => renderRow(rows[virtualRow.index], virtualRow.index))}
              {virtualPaddingBottom > 0 ? (
                <tbody aria-hidden="true">
                  <tr className={styles.virtualSpacerRow} style={{ height: `${virtualPaddingBottom}px` }} aria-hidden="true" data-credential-request-events-spacer>
                    <td colSpan={8} />
                  </tr>
                </tbody>
              ) : null}
            </>
          ) : rows.map((row) => renderRow(row))}
        </table>
        {loadingMore ? <div className={styles.loadState} role="status">{t('common.loading')}</div> : null}
        {!autoLoadMore && hasMore ? (
          <div className={styles.loadState}>
            <button type="button" className={styles.retryButton} onClick={onLoadMore}>
              {t('common.retry')}
            </button>
          </div>
        ) : null}
      </div>
      <PortalTooltip tooltip={tooltip} />
    </div>
  )
}
