import { useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Modal } from '@/components/ui/Modal'
import { IconRefreshCw } from '@/components/ui/icons'
import { ProviderBrandIcon } from '@/components/ProviderBrandIcon'
import { RequestEventLogModal } from '@/components/usage/RequestEventLogModal'
import { ApiError, fetchErrorEvents, fetchUsageEvents } from '@/lib/api'
import type { ErrorEvent, UsageEvent, UsageEventRequestLogResponse } from '@/lib/types'
import { AuthFileQuotaPanel } from './AuthFileCredentialsSection'
import { CredentialErrorEventsList } from './CredentialErrorEventsList'
import { CredentialHealthPanel } from './CredentialHealthPanel'
import { CredentialPriorityBadge, cacheReadRateTone, credentialToneClassName, formatCredentialNumber, formatCredentialPercent, successRateTone } from './CredentialSectionShell'
import { CredentialSubscriptionBadge } from './CredentialSubscriptionBadge'
import { CredentialRequestEventsList } from './CredentialRequestEventsList'
import { CodexQuotaHistoryPanel } from './CodexQuotaHistoryPanel'
import type { CredentialDetailSelection } from './credentialViewModels'
import styles from './CredentialDetailDrawer.module.scss'
import credentialStyles from './CredentialSections.module.scss'

const REQUEST_EVENTS_PAGE_SIZE = 50
const ERROR_EVENTS_PAGE_SIZE = 50

type CredentialDetailTab = 'overview' | 'quota-history' | 'requests' | 'errors'

interface CredentialDetailDrawerProps {
  open: boolean
  selection: CredentialDetailSelection | null
  onAuthRequired?: () => void
  requestLogAccessEnabled?: boolean
  onRequestLogOpen?: (event: UsageEvent) => void
  requestLogLoadingEventId?: string | null
  requestLogResponse?: UsageEventRequestLogResponse | null
  requestLogError?: string
  onRequestLogClose?: () => void
  onRequestLogDownload?: (eventId: string) => void
  requestLogDownloading?: boolean
  onClose: () => void
}

export function appendCredentialDetailEvents(
  currentEvents: readonly UsageEvent[],
  incomingEvents: readonly UsageEvent[],
): UsageEvent[] {
  const seen = new Set(currentEvents.map((event) => String(event.id ?? '').trim()).filter(Boolean))
  const merged = [...currentEvents]
  for (const event of incomingEvents) {
    const id = String(event.id ?? '').trim()
    if (id && seen.has(id)) continue
    if (id) seen.add(id)
    merged.push(event)
  }
  return merged
}

function appendCredentialErrorEvents(
  currentEvents: readonly ErrorEvent[],
  incomingEvents: readonly ErrorEvent[],
): ErrorEvent[] {
  const seen = new Set(currentEvents.map((event) => String(event.id ?? '').trim()).filter(Boolean))
  const merged = [...currentEvents]
  for (const event of incomingEvents) {
    const id = String(event.id ?? '').trim()
    if (id && seen.has(id)) continue
    if (id) seen.add(id)
    merged.push(event)
  }
  return merged
}

export function CredentialDetailDrawer({
  open,
  selection,
  onAuthRequired,
  requestLogAccessEnabled = false,
  onRequestLogOpen,
  requestLogLoadingEventId = null,
  requestLogResponse = null,
  requestLogError = '',
  onRequestLogClose,
  onRequestLogDownload,
  requestLogDownloading = false,
  onClose,
}: CredentialDetailDrawerProps) {
  const { t } = useTranslation()
  const overviewTabId = useId()
  const quotaHistoryTabId = useId()
  const requestsTabId = useId()
  const errorsTabId = useId()
  const overviewPanelId = useId()
  const quotaHistoryPanelId = useId()
  const requestsPanelId = useId()
  const errorsPanelId = useId()
  const overviewTabRef = useRef<HTMLButtonElement | null>(null)
  const quotaHistoryTabRef = useRef<HTMLButtonElement | null>(null)
  const requestsTabRef = useRef<HTMLButtonElement | null>(null)
  const errorsTabRef = useRef<HTMLButtonElement | null>(null)
  const [activeTab, setActiveTab] = useState<CredentialDetailTab>('overview')
  const [events, setEvents] = useState<UsageEvent[]>([])
  const [eventsLoading, setEventsLoading] = useState(false)
  const [eventsLoadingMore, setEventsLoadingMore] = useState(false)
  const [eventsAutoLoadMore, setEventsAutoLoadMore] = useState(true)
  const [eventsError, setEventsError] = useState('')
  const [eventsNextCursor, setEventsNextCursor] = useState<string | null>(null)
  const firstPageControllerRef = useRef<AbortController | null>(null)
  const loadMoreControllerRef = useRef<AbortController | null>(null)
  const [errorEvents, setErrorEvents] = useState<ErrorEvent[]>([])
  const [errorEventsLoading, setErrorEventsLoading] = useState(false)
  const [errorEventsLoadingMore, setErrorEventsLoadingMore] = useState(false)
  const [errorEventsAutoLoadMore, setErrorEventsAutoLoadMore] = useState(true)
  const [errorEventsError, setErrorEventsError] = useState('')
  const [errorEventsNextCursor, setErrorEventsNextCursor] = useState<string | null>(null)
  const errorFirstPageControllerRef = useRef<AbortController | null>(null)
  const errorLoadMoreControllerRef = useRef<AbortController | null>(null)

  const row = selection?.row ?? null
  const identity = row?.identity ?? null
  const selectionKey = selection ? `${selection.kind}:${identity?.id || identity?.identity || ''}` : ''
  const sourceFilter = identity?.identity?.trim() ?? ''
  const authTypeFilter = identity?.auth_type
  const identityId = String(identity?.id ?? '').trim()
  const hasCodexQuotaHistory = selection?.kind === 'auth-file' && identity?.type?.trim().toLowerCase() === 'codex'
  const availableTabs = useMemo<CredentialDetailTab[]>(() => hasCodexQuotaHistory
    ? ['overview', 'quota-history', 'requests', 'errors']
    : ['overview', 'requests', 'errors'], [hasCodexQuotaHistory])

  const resetRequestEvents = useCallback(() => {
    firstPageControllerRef.current?.abort()
    loadMoreControllerRef.current?.abort()
    firstPageControllerRef.current = null
    loadMoreControllerRef.current = null
    setEvents([])
    setEventsLoading(false)
    setEventsLoadingMore(false)
    setEventsAutoLoadMore(true)
    setEventsError('')
    setEventsNextCursor(null)
  }, [])

  const resetErrorEvents = useCallback(() => {
    errorFirstPageControllerRef.current?.abort()
    errorLoadMoreControllerRef.current?.abort()
    errorFirstPageControllerRef.current = null
    errorLoadMoreControllerRef.current = null
    setErrorEvents([])
    setErrorEventsLoading(false)
    setErrorEventsLoadingMore(false)
    setErrorEventsAutoLoadMore(true)
    setErrorEventsError('')
    setErrorEventsNextCursor(null)
  }, [])

  // 关闭动画继续使用 Modal 的内容快照；同步清理内部状态，保证下次打开不会提交上一凭证的数据。
  useLayoutEffect(() => {
    if (open) return
    setActiveTab('overview')
    resetRequestEvents()
    resetErrorEvents()
  }, [open, resetErrorEvents, resetRequestEvents])

  useEffect(() => {
    if (!open || !selectionKey) return
    setActiveTab('overview')
    resetRequestEvents()
    resetErrorEvents()
  }, [open, resetErrorEvents, resetRequestEvents, selectionKey])

  useEffect(() => {
    // 同一个身份在同步后可能改变类型；不再是 Codex 时立即退出专属标签，避免展示不属于当前凭证的数据。
    if (activeTab === 'quota-history' && !hasCodexQuotaHistory) setActiveTab('overview')
  }, [activeTab, hasCodexQuotaHistory])

  const loadFirstPage = useCallback(async () => {
    if (!open || activeTab !== 'requests' || !sourceFilter) return
    firstPageControllerRef.current?.abort()
    loadMoreControllerRef.current?.abort()
    loadMoreControllerRef.current = null
    const controller = new AbortController()
    firstPageControllerRef.current = controller
    setEventsLoading(true)
    setEventsLoadingMore(false)
    setEventsAutoLoadMore(true)
    setEventsError('')
    try {
      // 详情列表固定从当前凭证最近的原始事件开始，不继承外层页面的查询条件。
      const response = await fetchUsageEvents(undefined, controller.signal, {
        authType: authTypeFilter,
        pageSize: REQUEST_EVENTS_PAGE_SIZE,
        cursorMode: true,
        source: sourceFilter,
      })
      if (firstPageControllerRef.current !== controller) return
      setEvents(response.events)
      setEventsNextCursor(response.has_more === true ? response.next_cursor?.trim() || null : null)
    } catch (error) {
      if (controller.signal.aborted) return
      setEvents([])
      setEventsNextCursor(null)
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.()
        return
      }
      setEventsError(error instanceof Error ? error.message : t('usage_stats.credentials_detail_requests_load_failed'))
    } finally {
      if (firstPageControllerRef.current === controller) {
        firstPageControllerRef.current = null
        setEventsLoading(false)
      }
    }
  }, [activeTab, authTypeFilter, onAuthRequired, open, sourceFilter, t])

  useEffect(() => {
    if (!open || activeTab !== 'requests') return
    void loadFirstPage()
    return () => {
      firstPageControllerRef.current?.abort()
      firstPageControllerRef.current = null
      loadMoreControllerRef.current?.abort()
      loadMoreControllerRef.current = null
    }
  }, [activeTab, loadFirstPage, open])

  const loadFirstErrorPage = useCallback(async () => {
    if (!open || activeTab !== 'errors' || !identityId) return
    errorFirstPageControllerRef.current?.abort()
    errorLoadMoreControllerRef.current?.abort()
    errorLoadMoreControllerRef.current = null
    const controller = new AbortController()
    errorFirstPageControllerRef.current = controller
    setErrorEventsLoading(true)
    setErrorEventsLoadingMore(false)
    setErrorEventsAutoLoadMore(true)
    setErrorEventsError('')
    try {
      // Errors API 使用 Keeper Identity ID 定位凭证，前端不接触 CPA auth_index 等敏感关联字段。
      const response = await fetchErrorEvents(identityId, controller.signal, undefined, ERROR_EVENTS_PAGE_SIZE)
      if (errorFirstPageControllerRef.current !== controller) return
      setErrorEvents(response.events)
      setErrorEventsNextCursor(response.has_more === true ? response.next_cursor?.trim() || null : null)
    } catch (error) {
      if (controller.signal.aborted) return
      setErrorEvents([])
      setErrorEventsNextCursor(null)
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.()
        return
      }
      setErrorEventsError(error instanceof Error ? error.message : t('usage_stats.credentials_detail_errors_load_failed'))
    } finally {
      if (errorFirstPageControllerRef.current === controller) {
        errorFirstPageControllerRef.current = null
        setErrorEventsLoading(false)
      }
    }
  }, [activeTab, identityId, onAuthRequired, open, t])

  useEffect(() => {
    if (!open || activeTab !== 'errors') return
    void loadFirstErrorPage()
    return () => {
      errorFirstPageControllerRef.current?.abort()
      errorFirstPageControllerRef.current = null
      errorLoadMoreControllerRef.current?.abort()
      errorLoadMoreControllerRef.current = null
    }
  }, [activeTab, loadFirstErrorPage, open])

  useEffect(() => () => {
    firstPageControllerRef.current?.abort()
    loadMoreControllerRef.current?.abort()
    errorFirstPageControllerRef.current?.abort()
    errorLoadMoreControllerRef.current?.abort()
  }, [])

  const activateTab = useCallback((tab: CredentialDetailTab, focus = false) => {
    if (tab !== 'requests') onRequestLogClose?.()
    setActiveTab(tab)
    if (focus) {
      const target = {
        overview: overviewTabRef.current,
        'quota-history': quotaHistoryTabRef.current,
        requests: requestsTabRef.current,
        errors: errorsTabRef.current,
      }[tab]
      target?.focus()
    }
  }, [onRequestLogClose])

  const handleTabKeyDown = useCallback((event: ReactKeyboardEvent<HTMLButtonElement>) => {
    const currentTab: CredentialDetailTab = event.currentTarget === quotaHistoryTabRef.current
      ? 'quota-history'
      : event.currentTarget === requestsTabRef.current
        ? 'requests'
        : event.currentTarget === errorsTabRef.current
          ? 'errors'
          : 'overview'
    const currentIndex = availableTabs.indexOf(currentTab)
    let nextTab: CredentialDetailTab | null = null
    switch (event.key) {
      case 'ArrowLeft':
        nextTab = availableTabs[(currentIndex - 1 + availableTabs.length) % availableTabs.length]
        break
      case 'ArrowRight':
        nextTab = availableTabs[(currentIndex + 1) % availableTabs.length]
        break
      case 'Home':
        nextTab = availableTabs[0]
        break
      case 'End':
        nextTab = availableTabs[availableTabs.length - 1]
        break
      default:
        return
    }
    event.preventDefault()
    activateTab(nextTab, true)
  }, [activateTab, availableTabs])

  const loadMore = useCallback(async () => {
    const cursor = eventsNextCursor?.trim()
    if (!cursor || loadMoreControllerRef.current || eventsLoading || eventsLoadingMore || !sourceFilter) return
    const controller = new AbortController()
    loadMoreControllerRef.current = controller
    setEventsLoadingMore(true)
    setEventsError('')
    try {
      const response = await fetchUsageEvents(undefined, controller.signal, {
        authType: authTypeFilter,
        pageSize: REQUEST_EVENTS_PAGE_SIZE,
        cursorMode: true,
        cursor,
        source: sourceFilter,
      })
      if (loadMoreControllerRef.current !== controller) return
      setEvents((current) => appendCredentialDetailEvents(current, response.events))
      setEventsAutoLoadMore(true)
      setEventsNextCursor(response.has_more === true ? response.next_cursor?.trim() || null : null)
    } catch (error) {
      if (controller.signal.aborted) return
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.()
        return
      }
      setEventsAutoLoadMore(false)
      setEventsError(error instanceof Error ? error.message : t('usage_stats.credentials_detail_requests_load_failed'))
    } finally {
      if (loadMoreControllerRef.current === controller) {
        loadMoreControllerRef.current = null
        setEventsLoadingMore(false)
      }
    }
  }, [authTypeFilter, eventsLoading, eventsLoadingMore, eventsNextCursor, onAuthRequired, sourceFilter, t])

  const loadMoreErrorEvents = useCallback(async () => {
    const cursor = errorEventsNextCursor?.trim()
    if (!cursor || errorLoadMoreControllerRef.current || errorEventsLoading || errorEventsLoadingMore || !identityId) return
    const controller = new AbortController()
    errorLoadMoreControllerRef.current = controller
    setErrorEventsLoadingMore(true)
    setErrorEventsError('')
    try {
      const response = await fetchErrorEvents(identityId, controller.signal, cursor, ERROR_EVENTS_PAGE_SIZE)
      if (errorLoadMoreControllerRef.current !== controller) return
      setErrorEvents((current) => appendCredentialErrorEvents(current, response.events))
      setErrorEventsAutoLoadMore(true)
      setErrorEventsNextCursor(response.has_more === true ? response.next_cursor?.trim() || null : null)
    } catch (error) {
      if (controller.signal.aborted) return
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.()
        return
      }
      // 分页失败保留已展示的数据，并暂停滚动自动重试，等待用户主动点击重试。
      setErrorEventsAutoLoadMore(false)
      setErrorEventsError(error instanceof Error ? error.message : t('usage_stats.credentials_detail_errors_load_failed'))
    } finally {
      if (errorLoadMoreControllerRef.current === controller) {
        errorLoadMoreControllerRef.current = null
        setErrorEventsLoadingMore(false)
      }
    }
  }, [errorEventsLoading, errorEventsLoadingMore, errorEventsNextCursor, identityId, onAuthRequired, t])

  if (!selection || !row || !identity) return null

  const title = (
    <div className={styles.drawerTitle}>
      <ProviderBrandIcon providerType={identity.type} size={36} ariaLabel={row.typeLabel} />
      <div className={styles.drawerTitleText}>
        <strong>{row.displayName}</strong>
        <span data-credential-detail-subtitle>
          {selection.kind === 'auth-file'
            ? identity.file_name?.trim() || '-'
            : `${row.providerLabel} · ${row.typeLabel} · ${row.authTypeLabel}`}
        </span>
      </div>
      <div className={styles.drawerTitleBadges}>
        {selection.kind === 'auth-file' && selection.row.subscriptionBadge
          ? <CredentialSubscriptionBadge model={selection.row.subscriptionBadge} />
          : null}
        {row.priorityLabel ? <CredentialPriorityBadge>{row.priorityLabel}</CredentialPriorityBadge> : null}
        <span className={identity.disabled || identity.is_deleted ? styles.statusDisabled : styles.statusEnabled}>
          {identity.is_deleted
            ? t('usage_stats.deleted')
            : identity.disabled
              ? t('usage_stats.credentials_detail_disabled')
              : t('usage_stats.credentials_detail_enabled')}
        </span>
      </div>
    </div>
  )

  return (
    <>
      <Modal open={open} title={title} variant="drawer" width={920} className={styles.drawer} onClose={onClose}>
        <div className={styles.tabBar} data-credential-detail-tab-bar>
          <div className={styles.tabs} role="tablist" aria-label={t('usage_stats.credentials_detail_tabs')}>
            <button
              ref={overviewTabRef}
              id={overviewTabId}
              type="button"
              role="tab"
              className={styles.tabButton}
              aria-selected={activeTab === 'overview'}
              aria-controls={overviewPanelId}
              tabIndex={activeTab === 'overview' ? 0 : -1}
              data-credential-detail-tab="overview"
              onClick={() => activateTab('overview')}
              onKeyDown={handleTabKeyDown}
            >
              {t('usage_stats.credentials_detail_overview_tab')}
            </button>
            {hasCodexQuotaHistory ? (
              <button
                ref={quotaHistoryTabRef}
                id={quotaHistoryTabId}
                type="button"
                role="tab"
                className={styles.tabButton}
                aria-selected={activeTab === 'quota-history'}
                aria-controls={quotaHistoryPanelId}
                tabIndex={activeTab === 'quota-history' ? 0 : -1}
                data-credential-detail-tab="quota-history"
                onClick={() => activateTab('quota-history')}
                onKeyDown={handleTabKeyDown}
              >
                {t('usage_stats.credentials_quota_history_tab')}
              </button>
            ) : null}
            <button
              ref={requestsTabRef}
              id={requestsTabId}
              type="button"
              role="tab"
              className={styles.tabButton}
              aria-selected={activeTab === 'requests'}
              aria-controls={requestsPanelId}
              tabIndex={activeTab === 'requests' ? 0 : -1}
              data-credential-detail-tab="requests"
              onClick={() => activateTab('requests')}
              onKeyDown={handleTabKeyDown}
            >
              {t('usage_stats.credentials_detail_requests_tab')}
            </button>
            <button
              ref={errorsTabRef}
              id={errorsTabId}
              type="button"
              role="tab"
              className={styles.tabButton}
              aria-selected={activeTab === 'errors'}
              aria-controls={errorsPanelId}
              tabIndex={activeTab === 'errors' ? 0 : -1}
              data-credential-detail-tab="errors"
              onClick={() => activateTab('errors')}
              onKeyDown={handleTabKeyDown}
            >
              {t('usage_stats.credentials_detail_errors_tab')}
            </button>
          </div>
          {((activeTab === 'requests' && eventsError && events.length === 0 && !eventsLoading)
            || (activeTab === 'errors' && errorEventsError && errorEvents.length === 0 && !errorEventsLoading)) ? (
            <button
              type="button"
              className={`${credentialStyles.credentialRowRefreshButton} ${styles.retryButton}`}
              data-credential-detail-retry
              aria-label={t('common.retry')}
              onClick={() => void (activeTab === 'requests' ? loadFirstPage() : loadFirstErrorPage())}
            >
              <IconRefreshCw size={13} />
            </button>
          ) : null}
        </div>

        {activeTab === 'overview' ? (
          <section id={overviewPanelId} role="tabpanel" aria-labelledby={overviewTabId} className={styles.overviewPanel}>
          <div className={styles.summaryGrid}>
            <DetailMetric
              label={t('usage_stats.total_requests')}
              value={formatCredentialNumber(row.totalRequests)}
              detail={(
                <>
                  <span className={credentialStyles.credentialMetricValueSuccess}>{t('usage_stats.success')} {formatCredentialNumber(row.successCount)}</span>
                  {' · '}
                  <span className={credentialStyles.credentialMetricValueDanger}>{t('usage_stats.failure')} {formatCredentialNumber(row.failureCount)}</span>
                </>
              )}
            />
            <DetailMetric valueTone={successRateTone(row.successRate)} label={t('usage_stats.success_rate')} value={formatCredentialPercent(row.successRate)} />
            <DetailMetric label={t('usage_stats.total_tokens')} value={formatCredentialNumber(row.totalTokens)} />
            <DetailMetric valueTone={cacheReadRateTone(row.cacheReadRate)} label={t('usage_stats.cache_rate')} value={formatCredentialPercent(row.cacheReadRate)} />
          </div>
          <div className={`${styles.overviewGrid} ${selection.kind === 'ai-provider' ? styles.overviewGridSingle : ''}`.trim()}>
            <section className={styles.overviewSection}>
              <h3>{t('usage_stats.credentials_detail_identity')}</h3>
              <dl className={styles.identityList}>
                <dt>{t('usage_stats.credentials_detail_provider')}</dt><dd>{row.providerLabel || '-'}</dd>
                <dt>{t('usage_stats.credentials_detail_type')}</dt><dd>{row.typeLabel || '-'}</dd>
                <dt>{t('usage_stats.credentials_detail_auth_type')}</dt><dd>{row.authTypeLabel || '-'}</dd>
                <dt>{t('usage_stats.credentials_detail_priority')}</dt><dd>{row.priorityLabel || '-'}</dd>
              </dl>
            </section>
            {selection.kind === 'auth-file' ? (
              <section className={styles.overviewSection}>
                <h3>{t('usage_stats.credentials_detail_quota')}</h3>
                <AuthFileQuotaPanel row={selection.row} quotaUsageMode="current" />
              </section>
            ) : null}
          </div>
          <section className={`${styles.overviewSection} ${styles.healthSection}`.trim()}>
            <h3>{t('usage_stats.credentials_detail_health')}</h3>
            <CredentialHealthPanel
              displayName={row.displayName}
              health={row.credentialHealth}
              lastUsedAt={identity.last_used_at}
              statsUpdatedAt={identity.stats_updated_at}
              windowCacheReadRate={row.windowCacheReadRate}
            />
          </section>
          </section>
        ) : activeTab === 'quota-history' && hasCodexQuotaHistory ? (
          <section id={quotaHistoryPanelId} role="tabpanel" aria-labelledby={quotaHistoryTabId} className={styles.quotaHistoryPanel}>
            <CodexQuotaHistoryPanel authIndex={sourceFilter} onAuthRequired={onAuthRequired} />
          </section>
        ) : activeTab === 'requests' ? (
          <section id={requestsPanelId} role="tabpanel" aria-labelledby={requestsTabId} className={styles.requestsPanel}>
            {eventsError ? <div className={styles.requestError} role="status">{eventsError}</div> : null}
            {eventsError && events.length === 0 ? null : (
              <CredentialRequestEventsList
                events={events}
                loading={eventsLoading}
                hasMore={Boolean(eventsNextCursor)}
                loadingMore={eventsLoadingMore}
                autoLoadMore={eventsAutoLoadMore}
                onLoadMore={() => void loadMore()}
                requestLogAccessEnabled={requestLogAccessEnabled}
                onRequestLogOpen={onRequestLogOpen}
                requestLogLoadingEventId={requestLogLoadingEventId}
              />
            )}
          </section>
        ) : (
          <section id={errorsPanelId} role="tabpanel" aria-labelledby={errorsTabId} className={styles.errorsPanel}>
            {errorEventsError ? <div className={styles.requestError} role="status">{errorEventsError}</div> : null}
            {errorEventsError && errorEvents.length === 0 ? null : (
              <CredentialErrorEventsList
                events={errorEvents}
                loading={errorEventsLoading}
                hasMore={Boolean(errorEventsNextCursor)}
                loadingMore={errorEventsLoadingMore}
                autoLoadMore={errorEventsAutoLoadMore}
                onLoadMore={() => void loadMoreErrorEvents()}
              />
            )}
          </section>
        )}
      </Modal>
      {open ? (
        <RequestEventLogModal
          loadingEventId={requestLogLoadingEventId}
          response={requestLogResponse}
          error={requestLogError}
          onClose={onRequestLogClose}
          onDownload={onRequestLogDownload}
          downloading={requestLogDownloading}
        />
      ) : null}
    </>
  )
}

type DetailMetricTone = 'success' | 'warning' | 'danger' | 'neutral'

function DetailMetric({ valueTone, label, value, detail }: { valueTone?: DetailMetricTone; label: string; value: string; detail?: ReactNode }) {
  const resolvedValueTone = valueTone ?? 'neutral'

  return (
    <div className={styles.summaryMetric} data-credential-detail-metric-tone={valueTone}>
      <span>{label}</span>
      <strong className={credentialToneClassName('credentialMetricValue', resolvedValueTone)}>{value}</strong>
      {detail ? <small>{detail}</small> : null}
    </div>
  )
}
