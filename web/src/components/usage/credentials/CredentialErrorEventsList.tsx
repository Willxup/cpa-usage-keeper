import { useCallback, useEffect, useRef, type UIEvent } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/components/ui/EmptyState'
import { useScrollBoundaryContainment } from '@/hooks/useScrollBoundaryContainment'
import type { ErrorEvent } from '@/lib/types'
import styles from './CredentialErrorEventsList.module.scss'

const LOAD_MORE_THRESHOLD_PX = 320
const VIRTUALIZATION_THRESHOLD = 50
const VIRTUAL_CARD_HEIGHT = 180
const VIRTUAL_CARD_GAP = 12
const VIRTUAL_OVERSCAN = 5
const VIRTUAL_INITIAL_VIEWPORT_HEIGHT = 600

interface CredentialErrorEventsListProps {
  events: ErrorEvent[]
  loading: boolean
  hasMore: boolean
  loadingMore: boolean
  autoLoadMore: boolean
  onLoadMore: () => void
}

const formatErrorTimestamp = (timestamp: string | undefined): string => {
  const value = String(timestamp ?? '')
  const match = value.match(/^(\d{4})-(\d{2})-(\d{2})[T\s](\d{2}):(\d{2}):(\d{2})/)
  if (!match) return value || '-'
  return `${match[1]}/${match[2]}/${match[3]} ${match[4]}:${match[5]}:${match[6]}`
}

export function CredentialErrorEventsList({
  events,
  loading,
  hasMore,
  loadingMore,
  autoLoadMore,
  onLoadMore,
}: CredentialErrorEventsListProps) {
  const { t } = useTranslation()
  const scrollerRef = useRef<HTMLDivElement | null>(null)
  // 首个完整 cursor 页正好是 50 条；此时即启用虚拟化，避免追加第二页后切换渲染模式造成滚动跳位。
  const virtualizeEvents = events.length >= VIRTUALIZATION_THRESHOLD
  // 与 Request Events 共用 TanStack Virtual；Error 卡片高度不固定，因此交给 measureElement 动态校准。
  // eslint-disable-next-line react-hooks/incompatible-library
  const eventVirtualizer = useVirtualizer({
    count: virtualizeEvents ? events.length : 0,
    getScrollElement: () => scrollerRef.current,
    estimateSize: () => VIRTUAL_CARD_HEIGHT,
    gap: VIRTUAL_CARD_GAP,
    overscan: VIRTUAL_OVERSCAN,
    getItemKey: (index) => events[index]?.id ?? index,
    initialRect: { width: 0, height: VIRTUAL_INITIAL_VIEWPORT_HEIGHT },
    useAnimationFrameWithResizeObserver: true,
  })
  const virtualEvents = eventVirtualizer.getVirtualItems()
  useScrollBoundaryContainment(scrollerRef, events.length > 0)

  const tryLoadMore = useCallback((scroller: Pick<HTMLDivElement, 'scrollTop' | 'clientHeight' | 'scrollHeight'>) => {
    if (!autoLoadMore || !hasMore || loading || loadingMore) return
    if (scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight <= LOAD_MORE_THRESHOLD_PX) onLoadMore()
  }, [autoLoadMore, hasMore, loading, loadingMore, onLoadMore])

  const handleScroll = useCallback((event: UIEvent<HTMLDivElement>) => {
    tryLoadMore(event.currentTarget)
  }, [tryLoadMore])

  useEffect(() => {
    const scroller = scrollerRef.current
    if (scroller) tryLoadMore(scroller)
  }, [events.length, tryLoadMore])

  if (loading && events.length === 0) {
    return <div className={styles.state} role="status">{t('common.loading')}</div>
  }

  if (events.length === 0) {
    return (
      <div className={styles.emptyState}>
        <EmptyState
          title={t('usage_stats.credentials_errors_empty_title')}
          description={t('usage_stats.credentials_errors_empty_desc')}
        />
      </div>
    )
  }

  const renderEvent = (event: ErrorEvent, virtualIndex?: number, virtualStart?: number) => {
    return (
      <article
        key={event.id}
        ref={virtualIndex === undefined ? undefined : eventVirtualizer.measureElement}
        data-index={virtualIndex}
        className={`${styles.card} ${virtualIndex === undefined ? '' : styles.virtualCard}`.trim()}
        style={virtualStart === undefined ? undefined : { transform: `translateY(${virtualStart}px)` }}
        data-credential-error-event-id={event.id}
      >
        <header className={styles.header}>
          <time dateTime={event.timestamp} title={event.timestamp}>{formatErrorTimestamp(event.timestamp)}</time>
          <div className={styles.badges}>
            <span className={styles.statusCode}>HTTP {event.status_code}</span>
            {event.code ? <span className={styles.errorCode}>{event.code}</span> : null}
          </div>
        </header>
        <div className={styles.subject}>
          <strong>{event.model || '-'}</strong>
        </div>
        <p className={styles.body}>{event.body_summary || '-'}{event.body_truncated ? '…' : ''}</p>
        {event.credential_retry_after || event.model_retry_after ? (
          <dl className={styles.retryTimes}>
            {event.credential_retry_after ? (
              <>
                <dt>{t('usage_stats.credentials_error_next_retry')}</dt>
                <dd title={event.credential_retry_after}>{formatErrorTimestamp(event.credential_retry_after)}</dd>
              </>
            ) : null}
            {event.model_retry_after ? (
              <>
                <dt>{t('usage_stats.credentials_error_model_next_retry')}</dt>
                <dd title={event.model_retry_after}>{formatErrorTimestamp(event.model_retry_after)}</dd>
              </>
            ) : null}
          </dl>
        ) : null}
      </article>
    )
  }

  return (
    <div
      ref={scrollerRef}
      className={styles.scroller}
      onScroll={handleScroll}
      data-credential-error-events-scroller="true"
      data-virtualized={virtualizeEvents}
      data-loaded-row-count={events.length}
    >
      <div
        className={`${styles.list} ${virtualizeEvents ? styles.virtualList : ''}`.trim()}
        style={virtualizeEvents ? { height: `${eventVirtualizer.getTotalSize()}px` } : undefined}
      >
        {virtualizeEvents
          ? virtualEvents.map((virtualEvent) => renderEvent(events[virtualEvent.index], virtualEvent.index, virtualEvent.start))
          : events.map((event) => renderEvent(event))}
      </div>
      {loadingMore ? <div className={styles.loadState} role="status">{t('common.loading')}</div> : null}
      {!autoLoadMore && hasMore ? (
        <div className={styles.loadState}>
          <button type="button" className={styles.retryButton} onClick={onLoadMore}>{t('common.retry')}</button>
        </div>
      ) : null}
    </div>
  )
}
