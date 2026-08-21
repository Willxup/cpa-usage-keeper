import { useCallback, useEffect, useMemo, useRef, type UIEvent } from 'react'
import { formatDateTime, useDisplayTimeZone } from '@/utils/timezone'
import { useVirtualizer } from '@tanstack/react-virtual'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/components/ui/EmptyState'
import { IconScrollText } from '@/components/ui/icons'
import { useScrollBoundaryContainment } from '@/hooks/useScrollBoundaryContainment'
import type { UsageEvent } from '@/lib/types'
import { formatDurationMs, formatUsd } from '@/utils/usage'
import styles from './CredentialRequestEventsList.module.scss'

const LOAD_MORE_THRESHOLD_PX = 320
const VIRTUALIZATION_THRESHOLD = 50
const VIRTUAL_ROW_HEIGHT = 52
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
  timestamp: string
  timestampLabel: string
  model: string
  modelAlias: string
  requestType: string
  endpoint: string
  failed: boolean
  inputTokens: string
  outputTokens: string
  totalTokens: string
  latency: string
  ttft: string
  cost: string
}

const toNumber = (value: unknown): number => {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? Math.max(parsed, 0) : 0
}

const formatTimestamp = (timestamp: string): string => formatDateTime(timestamp, {
  dateStyle: undefined, timeStyle: undefined,
  year: 'numeric', month: '2-digit', day: '2-digit',
  hour: '2-digit', minute: '2-digit', second: '2-digit',
}) || timestamp || '-'

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

const buildRow = (event: UsageEvent, index: number): CredentialRequestEventRow => {
  const timestamp = String(event.timestamp ?? '')
  const model = String(event.model ?? '').trim() || '-'
  const modelAliasValue = String(event.model_alias ?? '').trim()
  const endpoint = parseRequestEndpoint(event.endpoint)
  const latencyMs = Number.isFinite(event.latency_ms) ? event.latency_ms : null
  const ttftMs = Number.isFinite(event.ttft_ms) ? event.ttft_ms as number : null
  const costAvailable = event.cost_available === true

  return {
    event,
    id: String(event.id ?? '').trim() || `${timestamp}-${model}-${index}`,
    requestId: String(event.request_id ?? '').trim(),
    timestamp,
    timestampLabel: formatTimestamp(timestamp),
    model,
    modelAlias: modelAliasValue && modelAliasValue !== model ? modelAliasValue : '',
    requestType: endpoint.requestType,
    endpoint: endpoint.endpoint,
    failed: event.failed === true,
    inputTokens: INTEGER_FORMATTER.format(toNumber(event.tokens?.input_tokens)),
    outputTokens: INTEGER_FORMATTER.format(toNumber(event.tokens?.output_tokens)),
    totalTokens: INTEGER_FORMATTER.format(toNumber(event.tokens?.total_tokens)),
    latency: formatDurationMs(latencyMs),
    ttft: ttftMs && ttftMs > 0 ? formatDurationMs(ttftMs) : '-',
    cost: costAvailable ? formatUsd(toNumber(event.cost_usd)) : '-',
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
  const displayTimeZone = useDisplayTimeZone()
  const scrollerRef = useRef<HTMLDivElement | null>(null)
  const rows = useMemo(() => events.map(buildRow), [displayTimeZone, events])
  const virtualizeRows = rows.length > VIRTUALIZATION_THRESHOLD
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

  const tryLoadMore = useCallback((scroller: Pick<HTMLDivElement, 'scrollTop' | 'clientHeight' | 'scrollHeight'>) => {
    if (!autoLoadMore || !hasMore || loading || loadingMore) return
    if (shouldLoadMoreCredentialRequestEvents(scroller)) onLoadMore()
  }, [autoLoadMore, hasMore, loading, loadingMore, onLoadMore])

  const handleScroll = useCallback((event: UIEvent<HTMLDivElement>) => {
    tryLoadMore(event.currentTarget)
  }, [tryLoadMore])

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

  const renderRow = (row: CredentialRequestEventRow, virtualIndex?: number) => {
    const resultLabel = row.failed ? t('usage_stats.failure') : t('usage_stats.success')
    const canOpenLog = Boolean(requestLogAccessEnabled && row.requestId && onRequestLogOpen)
    const logLoading = requestLogLoadingEventId === row.id
    return (
      <tr
        key={row.id}
        ref={virtualIndex === undefined ? undefined : rowVirtualizer.measureElement}
        data-index={virtualIndex}
        aria-rowindex={virtualIndex === undefined ? undefined : virtualIndex + 2}
        data-credential-request-event-id={row.id}
      >
        <td className={styles.timestamp} title={row.timestamp}>{row.timestampLabel}</td>
        <td className={styles.model} title={row.modelAlias || row.model}>
          <strong>{row.model}</strong>
          {row.modelAlias ? <small>{row.modelAlias}</small> : null}
        </td>
        <td className={styles.request} title={row.endpoint}>
          <strong>{row.requestType}</strong>
          <small>{row.endpoint}</small>
        </td>
        <td>
          {canOpenLog ? (
            <button
              type="button"
              className={`${styles.result} ${row.failed ? styles.resultFailed : styles.resultSuccess}`.trim()}
              onClick={() => onRequestLogOpen?.(row.event)}
              aria-label={logLoading
                ? t('usage_stats.request_events_log_loading_aria', { result: resultLabel })
                : t('usage_stats.request_events_log_open_aria', { result: resultLabel })}
              aria-busy={logLoading}
              disabled={logLoading}
            >
              <span>{resultLabel}</span>
              <IconScrollText size={10} aria-hidden="true" />
            </button>
          ) : (
            <span className={`${styles.result} ${row.failed ? styles.resultFailed : styles.resultSuccess}`.trim()}>{resultLabel}</span>
          )}
        </td>
        <td className={styles.tokens}>
          <strong>{row.totalTokens}</strong>
          <small>{t('usage_stats.input_tokens')} {row.inputTokens} · {t('usage_stats.output_tokens')} {row.outputTokens}</small>
        </td>
        <td className={styles.performance}>
          <strong>{row.latency}</strong>
          <small>{t('usage_stats.ttft')} {row.ttft}</small>
        </td>
        <td className={styles.cost}>{row.cost}</td>
      </tr>
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
              <th>{t('usage_stats.request_events_timestamp')}</th>
              <th>{t('usage_stats.model_name')}</th>
              <th>{t('usage_stats.request_type')}</th>
              <th>{t('usage_stats.request_events_result')}</th>
              <th>{t('usage_stats.total_tokens')}</th>
              <th>{t('usage_stats.latency')}</th>
              <th>{t('usage_stats.total_cost')}</th>
            </tr>
          </thead>
          <tbody>
            {virtualizeRows ? (
              <>
                {virtualPaddingTop > 0 ? (
                  <tr className={styles.virtualSpacerRow} style={{ height: `${virtualPaddingTop}px` }} aria-hidden="true" data-credential-request-events-spacer>
                    <td colSpan={7} />
                  </tr>
                ) : null}
                {virtualRows.map((virtualRow) => renderRow(rows[virtualRow.index], virtualRow.index))}
                {virtualPaddingBottom > 0 ? (
                  <tr className={styles.virtualSpacerRow} style={{ height: `${virtualPaddingBottom}px` }} aria-hidden="true" data-credential-request-events-spacer>
                    <td colSpan={7} />
                  </tr>
                ) : null}
              </>
            ) : rows.map((row) => renderRow(row))}
          </tbody>
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
    </div>
  )
}
