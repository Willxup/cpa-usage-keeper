import { useCallback, useEffect, useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { Modal } from '@/components/ui/Modal'
import { IconChartLine, IconGaugeReset, IconRefreshCw, IconSearch, IconShield, IconTrash2 } from '@/components/ui/icons'
import quotaCostIcon from '@/assets/icons/quota-cost.svg'
import quotaTokenIcon from '@/assets/icons/quota-token.svg'
import styles from './CredentialSections.module.scss'
import type { AuthFileCredentialRow, DisplayQuota, PlanTypeTone } from './credentialViewModels'
import { deleteAuthFiles, setAuthFilesDisabled, type UsageIdentityPageSort } from '@/lib/api'
import type { UsageQuotaInspectionResult, UsageQuotaInspectionResultStatus, UsageQuotaInspectionStatusResponse } from '@/lib/types'
import { CredentialHealthPanel } from './CredentialHealthPanel'
import { CredentialProviderFilterIcon } from './CredentialProviderFilterBar'
import { CredentialBadge, CredentialPriorityBadge, CredentialRowShell, CredentialSectionShell, CredentialTableHeader, CredentialsPagination, MetricPill, RequestMetric, TonePercent, cacheRateTone, capitalize, credentialToneClassName, formatCredentialNumber, successRateTone } from './CredentialSectionShell'

type Translate = (key: string, options?: Record<string, string>) => string
type InspectionIndicatorTone = 'idle' | 'running' | 'completed'
type InspectionResultStatusFilter = 'normal' | 'limit_reached' | 'unauthorized_401_402' | 'other_failed'
type InspectionResultStatusFilterState = InspectionResultStatusFilter | null
type InspectionStatTone = 'normal' | 'limitReached' | 'unauthorized' | 'failed' | 'unknown'
type QuotaUsageMode = 'current' | 'estimated'
type AuthFileDisplayMode = 'quota' | 'health'
type InvalidInspectionAccountAction = 'disable' | 'delete'
type QuotaErrorDisplay = {
  code?: string
  message: string
  title: string
}
type QuotaErrorDetails = {
  code?: string
  message?: string
}
type QuotaResetPopoverPosition = {
  top: number
  right: number
}

const QUOTA_ERROR_MESSAGE_MAX_LENGTH = 96
const QUOTA_ERROR_PARSE_MAX_DEPTH = 10
const AUTH_FILE_DISPLAY_MODE_STORAGE_KEY = 'cpa.credentials.authFiles.displayMode'
export const INSPECTION_RESULT_PAGE_SIZE_OPTIONS = [10, 20, 50] as const
const DEFAULT_INSPECTION_RESULT_PAGE_SIZE = INSPECTION_RESULT_PAGE_SIZE_OPTIONS[0]
const INSPECTION_SELECTABLE_RESULT_STATUSES = new Set<InspectionResultStatusFilter>([
  'normal',
  'limit_reached',
  'unauthorized_401_402',
  'other_failed',
])
const INVALID_INSPECTION_ACCOUNT_STATUSES = new Set<UsageQuotaInspectionResultStatus>([
  'unauthorized_401',
  'payment_required_402',
])

interface AuthFileCredentialsSectionProps {
  rows: AuthFileCredentialRow[]
  total: number
  page: number
  totalPages: number
  pageSize: number
  activeOnly: boolean
  sort: UsageIdentityPageSort
  loading: boolean
  quotaRefreshing: boolean
  quotaRefreshError: string
  quotaAutoRefreshEnabled: boolean
  quotaInspectionStatus: UsageQuotaInspectionStatusResponse | null
  quotaInspectionLoading: boolean
  quotaInspectionStarting: boolean
  quotaInspectionError: string
  onPageChange: (page: number) => void
  onPageSizeChange: (pageSize: number) => void
  onActiveOnlyChange: (activeOnly: boolean) => void
  onSortChange: (sort: UsageIdentityPageSort) => void
  onRefreshQuota: () => Promise<void>
  onRefreshQuotaForAuthIndex: (authIndex: string) => Promise<void>
  onResetQuotaForAuthIndex: (authIndex: string) => Promise<void>
  onRefreshInspectionStatus: () => Promise<void>
  onStartInspection: () => Promise<void>
  onAfterInvalidAccountAction?: () => Promise<void>
}

export function AuthFileCredentialsSection({ rows, total, page, totalPages, pageSize, activeOnly, sort, loading, quotaRefreshing, quotaRefreshError, quotaAutoRefreshEnabled, quotaInspectionStatus, quotaInspectionLoading, quotaInspectionStarting, quotaInspectionError, onPageChange, onPageSizeChange, onActiveOnlyChange, onSortChange, onRefreshQuota, onRefreshQuotaForAuthIndex, onResetQuotaForAuthIndex, onRefreshInspectionStatus, onStartInspection, onAfterInvalidAccountAction }: AuthFileCredentialsSectionProps) {
  const { t } = useTranslation()
  const [inspectionOpen, setInspectionOpen] = useState(false)
  const [quotaUsageMode, setQuotaUsageMode] = useState<QuotaUsageMode>('current')
  const [displayMode, setDisplayModeState] = useState<AuthFileDisplayMode>(() => readStoredAuthFileDisplayMode())
  const showHealthMode = displayMode === 'health'
  const canRefresh = rows.some((row) => !isRowRefreshing(row) && !row.identity.is_deleted) && !quotaRefreshing
  const inspectionTone = inspectionIndicatorTone(quotaInspectionStatus)
  const openInspection = () => {
    setInspectionOpen(true)
    void onRefreshInspectionStatus()
  }
  const setDisplayMode = (mode: AuthFileDisplayMode) => {
    setDisplayModeState(mode)
    persistAuthFileDisplayMode(mode)
  }

  return (
    <>
      <CredentialSectionShell
        title={t('usage_stats.credentials_auth_files_title')}
        subtitle={t('usage_stats.credentials_auth_files_subtitle')}
        countLabel={t('usage_stats.credentials_count', { count: total })}
        titleExtra={(
          <div className={styles.credentialAuthFileTitleControls}>
            <label className={styles.credentialActiveOnlySwitch}>
              <span className={styles.credentialActiveOnlyLabel}>{t('usage_stats.credentials_auth_files_active_only')}</span>
              <input type="checkbox" checked={activeOnly} onChange={(event) => onActiveOnlyChange(event.target.checked)} />
              <span className={styles.credentialActiveOnlyTrack} aria-hidden="true">
                <span className={styles.credentialActiveOnlyThumb} />
              </span>
            </label>
            <AuthFileDisplayModeSwitch mode={displayMode} onChange={setDisplayMode} />
          </div>
        )}
        actions={(
          <div className={styles.credentialSectionActionButtons}>
            <div className={`${styles.credentialRefreshSwitcher} ${styles.credentialInspectionSwitcher}`.trim()}>
              <button
                type="button"
                className={`${styles.credentialRefreshButton} ${styles.credentialRefreshButtonActive} ${styles.credentialInspectionButton}`.trim()}
                onClick={openInspection}
                aria-pressed={inspectionTone !== 'idle'}
              >
                <span className={styles.credentialRefreshButtonInner}>
                  <IconSearch size={12} />
                  <span>{t('usage_stats.credentials_inspection_open')}</span>
                  {inspectionTone !== 'idle' && <span className={`${styles.credentialInspectionDot} ${styles[`credentialInspectionDot${capitalize(inspectionTone)}`]}`.trim()} aria-hidden="true" />}
                </span>
              </button>
            </div>
            <div className={styles.credentialRefreshSwitcher}>
              <button
                type="button"
                className={`${styles.credentialRefreshButton} ${styles.credentialRefreshButtonActive} ${quotaRefreshing ? styles.credentialRefreshButtonLoading : ''}`.trim()}
                onClick={() => void onRefreshQuota()}
                disabled={!canRefresh}
                aria-busy={quotaRefreshing}
              >
                <span className={styles.credentialRefreshButtonInner}>
                  {quotaRefreshing ? <LoadingSpinner size={12} className={styles.credentialRefreshSpinner} /> : <IconRefreshCw size={12} />}
                  <span>{quotaRefreshing ? t('usage_stats.credentials_quota_refreshing') : t('usage_stats.credentials_quota_refresh_current_page')}</span>
                </span>
              </button>
            </div>
          </div>
        )}
      >
      {/* 批量刷新失败显示在区块顶部，单行任务失败显示在对应限额位置。 */}
      {quotaRefreshError && <div className={styles.credentialInlineError}>{quotaRefreshError}</div>}
      {loading && rows.length === 0 && <div className={styles.credentialEmptyState}>{t('common.loading')}</div>}
      {!loading && rows.length === 0 && <div className={styles.credentialEmptyState}>{t('usage_stats.credentials_auth_files_empty')}</div>}
      {rows.length > 0 && (
        <CredentialTableHeader
          rowClassName={styles.authFileCredentialRow}
          nameLabel={t('usage_stats.credentials_column_name')}
          totalRequestsLabel={t('usage_stats.total_requests')}
          successRateLabel={t('usage_stats.success_rate')}
          totalTokensLabel={t('usage_stats.total_tokens')}
          cacheRateLabel={t('usage_stats.cache_rate')}
          sideLabel={showHealthMode ? t('usage_stats.credentials_column_health') : t('usage_stats.credentials_column_quota')}
        />
      )}
      {rows.map((row) => {
        const rowRefreshing = isRowRefreshing(row)
        const resetCredits = row.quotaResetCreditsAvailableCount ?? 0
        const canResetQuota = resetCredits > 0 && !row.identity.is_deleted && !rowRefreshing && !row.quotaResetting
        return (
          <CredentialRowShell
            key={row.identity.id || row.identity.identity}
            title={row.displayName}
            subtitle={(
              <span className={styles.credentialIdentityBadges}>
                <CredentialBadge>{row.typeLabel}</CredentialBadge>
                {row.planTypeLabel && <CredentialPlanBadge tone={row.planTypeTone}>{row.planTypeLabel}</CredentialPlanBadge>}
                {row.remainingDaysLabel && <span className={styles.credentialRemainingDaysBadge}>{row.remainingDaysLabel}</span>}
                {row.priorityLabel && <CredentialPriorityBadge>{row.priorityLabel}</CredentialPriorityBadge>}
              </span>
            )}
            badges={null}
            metrics={(
              <>
                <MetricPill value={<RequestMetric total={row.totalRequests} success={row.successCount} failure={row.failureCount} />} />
                <MetricPill value={<TonePercent value={row.successRate} tone={successRateTone(row.successRate)} />} />
                <MetricPill value={formatCredentialNumber(row.totalTokens)} />
                <MetricPill value={<TonePercent value={row.cacheRate} tone={cacheRateTone(row.cacheRate)} />} />
              </>
            )}
            rowClassName={styles.authFileCredentialRow}
            side={showHealthMode ? (
              <CredentialHealthPanel displayName={row.displayName} health={row.credentialHealth} lastUsedAt={row.identity.last_used_at} statsUpdatedAt={row.identity.stats_updated_at} />
            ) : (
              <div className={styles.credentialQuotaSideWithAction}>
                <AuthFileQuotaPanel row={row} quotaUsageMode={quotaUsageMode} />
                <div className={styles.credentialQuotaActionStack}>
                  {/* reset 按钮只在官方缓存给出可用次数时展示；refresh 始终保留在右侧列居中位置。 */}
                  {resetCredits > 0 && (
                    <QuotaResetAction
                      resetCredits={resetCredits}
                      disabled={!canResetQuota}
                      loading={row.quotaResetting === true}
                      onConfirm={() => onResetQuotaForAuthIndex(row.identity.identity)}
                    />
                  )}
                  <button
                    type="button"
                    className={`${styles.credentialRowRefreshButton} ${rowRefreshing ? styles.credentialRowRefreshButtonLoading : ''}`.trim()}
                    onClick={() => void onRefreshQuotaForAuthIndex(row.identity.identity)}
                    disabled={row.identity.is_deleted || rowRefreshing}
                    aria-label={t('usage_stats.credentials_refresh_single', { name: row.displayName })}
                    aria-busy={rowRefreshing}
                  >
                    {rowRefreshing ? <LoadingSpinner size={13} /> : <IconRefreshCw size={13} />}
                  </button>
                </div>
              </div>
            )}
          />
        )
      })}
      <CredentialsPagination
        leadingControls={showHealthMode ? undefined : <QuotaUsageModeSwitch label={t('usage_stats.credentials_quota_usage_mode_label')} mode={quotaUsageMode} onChange={setQuotaUsageMode} />}
        page={page}
        total={total}
        totalPages={totalPages}
        pageSize={pageSize}
        sortValue={sort}
        sortLabel={t('usage_stats.credentials_sort_label')}
        sortOptions={[
          { value: 'priority', label: t('usage_stats.credentials_sort_priority') },
          { value: 'total_requests', label: t('usage_stats.credentials_sort_total_requests') },
          { value: 'total_tokens', label: t('usage_stats.credentials_sort_total_tokens') },
          { value: 'last_used_at', label: t('usage_stats.credentials_sort_last_used') },
        ]}
        previousLabel={t('usage_stats.previous_page')}
        nextLabel={t('usage_stats.next_page')}
        rowsPerPageLabel={t('usage_stats.rows_per_page')}
        onPageChange={onPageChange}
        onPageSizeChange={onPageSizeChange}
        onSortChange={(nextSort) => onSortChange(nextSort as UsageIdentityPageSort)}
      />
      </CredentialSectionShell>
      <QuotaInspectionModal
        open={inspectionOpen}
        status={quotaInspectionStatus}
        loading={quotaInspectionLoading}
        starting={quotaInspectionStarting}
        error={quotaInspectionError}
        quotaAutoRefreshEnabled={quotaAutoRefreshEnabled}
        onClose={() => setInspectionOpen(false)}
        onStart={onStartInspection}
        onRefreshStatus={onRefreshInspectionStatus}
        onAfterInvalidAccountAction={onAfterInvalidAccountAction}
      />
    </>
  )
}


function QuotaResetAction({
  resetCredits,
  disabled,
  loading,
  onConfirm,
}: {
  resetCredits: number
  disabled: boolean
  loading: boolean
  onConfirm: () => Promise<void>
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [popoverPosition, setPopoverPosition] = useState<QuotaResetPopoverPosition | null>(null)
  const tooltipId = useId()
  const actionRef = useRef<HTMLDivElement | null>(null)
  const buttonRef = useRef<HTMLButtonElement | null>(null)

  const updatePopoverPosition = useCallback(() => {
    if (typeof window === 'undefined') {
      return
    }
    const button = buttonRef.current
    if (!button) {
      return
    }
    const rect = button.getBoundingClientRect()
    // popover 使用 fixed，避免被卡片 overflow 裁切，同时跟随右侧按钮重新定位。
    setPopoverPosition({
      top: Math.round(rect.bottom + 8),
      right: Math.max(12, Math.round(window.innerWidth - rect.right)),
    })
  }, [])

  useEffect(() => {
    if (!open) {
      return
    }
    updatePopoverPosition()
    const refreshPopoverPosition = () => updatePopoverPosition()
    window.addEventListener('resize', refreshPopoverPosition)
    window.addEventListener('scroll', refreshPopoverPosition, true)
    return () => {
      window.removeEventListener('resize', refreshPopoverPosition)
      window.removeEventListener('scroll', refreshPopoverPosition, true)
    }
  }, [open, updatePopoverPosition])

  useEffect(() => {
    if (!open || typeof document === 'undefined') {
      return
    }
    const handlePointerDown = (event: PointerEvent) => {
      const target = event.target
      if (!(target instanceof Node)) {
        return
      }
      if (actionRef.current?.contains(target)) {
        return
      }
      setOpen(false)
    }
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setOpen(false)
      }
    }
    document.addEventListener('pointerdown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [open])

  const handleConfirm = async () => {
    await onConfirm()
    setOpen(false)
  }

  const handleToggleOpen = () => {
    if (open) {
      setOpen(false)
      return
    }
    updatePopoverPosition()
    setOpen(true)
  }

  return (
    <div ref={actionRef} className={styles.credentialQuotaResetAction}>
      <button
        ref={buttonRef}
        type="button"
        className={`${styles.credentialRowResetButton} ${loading ? styles.credentialRowRefreshButtonLoading : ''}`.trim()}
        onClick={handleToggleOpen}
        disabled={disabled}
        aria-label={t('usage_stats.credentials_quota_reset_button', { count: String(resetCredits) })}
        aria-describedby={open ? undefined : tooltipId}
        aria-busy={loading}
        aria-expanded={open}
        aria-haspopup="dialog"
      >
        {loading ? <LoadingSpinner size={13} /> : <IconGaugeReset size={13} />}
      </button>
      {!open && (
        <span id={tooltipId} className={styles.credentialQuotaResetTooltip} role="tooltip">
          <span className={styles.credentialQuotaResetCount}>{resetCredits}</span>
          <span>{t('usage_stats.credentials_quota_reset_tooltip_suffix')}</span>
        </span>
      )}
      {open && (
        <div
          className={styles.credentialQuotaResetPopover}
          role="dialog"
          aria-label={t('usage_stats.credentials_quota_reset_title')}
          style={popoverPosition ? { top: popoverPosition.top, right: popoverPosition.right } : undefined}
        >
          <p className={styles.credentialQuotaResetTitle}>{t('usage_stats.credentials_quota_reset_title')}</p>
          <p className={styles.credentialQuotaResetMessage}>
            <span className={styles.credentialQuotaResetCountLine}>
              <span className={styles.credentialQuotaResetCount}>{resetCredits}</span>
              <span>{t('usage_stats.credentials_quota_reset_message_suffix')}</span>
            </span>
            <span>{t('usage_stats.credentials_quota_reset_message_prompt')}</span>
          </p>
          <div className={styles.credentialQuotaResetActions}>
            <button type="button" className={styles.credentialQuotaResetCancelButton} onClick={() => setOpen(false)} disabled={loading}>
              {t('common.cancel')}
            </button>
            <button type="button" className={styles.credentialQuotaResetConfirmButton} onClick={() => void handleConfirm()} disabled={loading} aria-busy={loading}>
              {loading ? <LoadingSpinner size={12} /> : t('usage_stats.credentials_quota_reset_confirm')}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

function isRowRefreshing(row: AuthFileCredentialRow): boolean {
  return row.refreshStatus === 'queued' || row.refreshStatus === 'running'
}

export function inspectionProgressTotal(status: Pick<UsageQuotaInspectionStatusResponse, 'total' | 'unknown'> | null): number {
  // 弹框进度条展示的是“巡检刷新任务进度”，不是 Auth Files 总账号覆盖率。
  if (!status) {
    return 0
  }
  // unknown 代表未参与刷新或没有可解析缓存的账号，不参与进度百分比分母。
  return Math.max(0, status.total - status.unknown)
}

export function formatInspectionProgressPercent(status: Pick<UsageQuotaInspectionStatusResponse, 'total' | 'cached' | 'unknown'> | null): number {
  // 初始加载或接口失败时没有状态，进度保持 0，避免前端自行 fallback total。
  if (!status) {
    return 0
  }
  // 分母统一走 inspectionProgressTotal，保证显示文本和进度条使用同一口径。
  const progressTotal = inspectionProgressTotal(status)
  if (progressTotal <= 0) {
    return 0
  }
  // cached 可能因为缓存恢复或并发刷新短暂超过分母，最终百分比要钳制在 0-100。
  return Math.max(0, Math.min(100, Math.round((status.cached / progressTotal) * 100)))
}

export function isInspectionStartDisabled({ quotaAutoRefreshEnabled, starting, total, running }: { quotaAutoRefreshEnabled: boolean; starting: boolean; total: number; running: boolean }): boolean {
  // 自动刷新开启时共用刷新结果，按钮不可手动启动巡检；running 只代表显式巡检轮次。
  return quotaAutoRefreshEnabled || starting || running || total <= 0
}

export function inspectionIndicatorTone(status: Pick<UsageQuotaInspectionStatusResponse, 'running' | 'completed' | 'completed_at'> | null): InspectionIndicatorTone {
  // 黄色点只看显式巡检 running，不响应普通手动刷新/自动刷新。
  if (status?.running) {
    return 'running'
  }
  // 绿色点必须有 completed_at；completed 没有时间时不展示完成态，避免共享缓存误点亮。
  if (status?.completed_at) {
    return 'completed'
  }
  return 'idle'
}

export function isSelectableInspectionStatusFilter(status: unknown): status is InspectionResultStatusFilter {
  return typeof status === 'string' && INSPECTION_SELECTABLE_RESULT_STATUSES.has(status as InspectionResultStatusFilter)
}

export function nextInspectionResultStatusFilter(current: InspectionResultStatusFilterState, next: InspectionResultStatusFilter): InspectionResultStatusFilterState {
  return current === next ? null : next
}

export function buildInspectionResultsPage(results: UsageQuotaInspectionResult[], statusFilter: InspectionResultStatusFilterState, page: number, pageSize: number): { results: UsageQuotaInspectionResult[]; total: number; totalPages: number; page: number; pageSize: number } {
  const safePageSize = INSPECTION_RESULT_PAGE_SIZE_OPTIONS.includes(pageSize as (typeof INSPECTION_RESULT_PAGE_SIZE_OPTIONS)[number])
    ? pageSize
    : DEFAULT_INSPECTION_RESULT_PAGE_SIZE
  const filteredResults = statusFilter ? results.filter((result) => matchesInspectionResultStatusFilter(result.status, statusFilter)) : results
  const total = filteredResults.length
  const totalPages = Math.max(1, Math.ceil(total / safePageSize))
  const safePage = Math.max(1, Math.min(Math.floor(page) || 1, totalPages))
  const start = (safePage - 1) * safePageSize
  return {
    results: filteredResults.slice(start, start + safePageSize),
    total,
    totalPages,
    page: safePage,
    pageSize: safePageSize,
  }
}

function matchesInspectionResultStatusFilter(status: UsageQuotaInspectionResultStatus, filter: InspectionResultStatusFilter): boolean {
  // 摘要卡把 401/402 合并，但结果行仍保留原始状态，方便禁用/删除按行处理。
  if (filter === 'unauthorized_401_402') {
    return status === 'unauthorized_401' || status === 'payment_required_402'
  }
  return status === filter
}

export function buildInvalidInspectionAccountFileNames(results: UsageQuotaInspectionResult[]): string[] {
  const seen = new Set<string>()
  const names: string[] = []
  for (const result of results) {
    if (!INVALID_INSPECTION_ACCOUNT_STATUSES.has(result.status)) {
      continue
    }
    const fileName = (result.file_name ?? '').trim()
    if (!fileName || seen.has(fileName)) {
      continue
    }
    seen.add(fileName)
    names.push(fileName)
  }
  return names
}

export function selectAllInvalidInspectionAccountFileNames(fileNames: string[]): string[] {
  return [...fileNames]
}

export function invertInvalidInspectionAccountFileNames(fileNames: string[], selectedFileNames: string[]): string[] {
  const selected = new Set(selectedFileNames)
  return fileNames.filter((fileName) => !selected.has(fileName))
}

function QuotaInspectionModal({
  open,
  status,
  loading,
  starting,
  error,
  quotaAutoRefreshEnabled,
  onClose,
  onStart,
  onRefreshStatus,
  onAfterInvalidAccountAction,
}: {
  open: boolean
  status: UsageQuotaInspectionStatusResponse | null
  loading: boolean
  starting: boolean
  error: string
  quotaAutoRefreshEnabled: boolean
  onClose: () => void
  onStart: () => Promise<void>
  onRefreshStatus: () => Promise<void>
  onAfterInvalidAccountAction?: () => Promise<void>
}) {
  const { t } = useTranslation()
  const [resultStatusFilter, setResultStatusFilter] = useState<InspectionResultStatusFilterState>(null)
  const [resultPage, setResultPage] = useState(1)
  const [resultPageSize, setResultPageSize] = useState<number>(DEFAULT_INSPECTION_RESULT_PAGE_SIZE)
  const [invalidAccountAction, setInvalidAccountAction] = useState<InvalidInspectionAccountAction | null>(null)
  const [selectedInvalidFileNames, setSelectedInvalidFileNames] = useState<string[]>([])
  const [invalidAccountSubmitting, setInvalidAccountSubmitting] = useState(false)
  const [invalidAccountError, setInvalidAccountError] = useState('')
  // total 由后端 Auth Files 身份统计提供，不用页面分页总数替代。
  const total = status?.total ?? 0
  // cached 是已经能解析出最近巡检结果的账号数。
  const cached = status?.cached ?? 0
  // progressTotal 排除 unknown，使进度条只描述实际刷新任务完成度。
  const progressTotal = inspectionProgressTotal(status)
  const progress = formatInspectionProgressPercent(status)
  // startDisabled 只依赖后端巡检 running 和自动刷新开关，不被普通行刷新状态牵连。
  const startDisabled = isInspectionStartDisabled({
    quotaAutoRefreshEnabled,
    starting,
    total,
    running: status?.running ?? false,
  })
  const startLabel = quotaAutoRefreshEnabled
    ? t('usage_stats.credentials_inspection_auto_enabled')
    : (starting || status?.running)
        ? t('usage_stats.credentials_inspection_running')
        : t('usage_stats.credentials_inspection_start')
  const results = status?.results ?? []
  const invalidFileNames = buildInvalidInspectionAccountFileNames(results)
  const resultPageData = buildInspectionResultsPage(results, resultStatusFilter, resultPage, resultPageSize)
  const handleSelectResultStatus = (nextStatus: InspectionResultStatusFilter) => {
    // 切换状态筛选时回到第一页，避免沿用上一个筛选的高页码导致空页。
    setResultStatusFilter((current) => nextInspectionResultStatusFilter(current, nextStatus))
    setResultPage(1)
  }
  const handleResultPageSizeChange = (nextPageSize: number) => {
    setResultPageSize(nextPageSize)
    setResultPage(1)
  }
  const openInvalidAccountAction = (action: InvalidInspectionAccountAction) => {
    setInvalidAccountAction(action)
    setSelectedInvalidFileNames(invalidFileNames)
    setInvalidAccountError('')
  }
  const selectAllInvalidFileNames = () => {
    setSelectedInvalidFileNames(selectAllInvalidInspectionAccountFileNames(invalidFileNames))
  }
  const invertInvalidFileNames = () => {
    setSelectedInvalidFileNames((current) => invertInvalidInspectionAccountFileNames(invalidFileNames, current))
  }
  const closeInvalidAccountAction = () => {
    if (invalidAccountSubmitting) {
      return
    }
    setInvalidAccountAction(null)
    setSelectedInvalidFileNames([])
    setInvalidAccountError('')
  }
  const toggleInvalidFileName = (fileName: string, checked: boolean) => {
    setSelectedInvalidFileNames((current) => {
      if (checked) {
        return current.includes(fileName) ? current : [...current, fileName]
      }
      return current.filter((name) => name !== fileName)
    })
  }
  const handleConfirmInvalidAccountAction = async () => {
    if (!invalidAccountAction || selectedInvalidFileNames.length === 0) {
      return
    }
    setInvalidAccountSubmitting(true)
    setInvalidAccountError('')
    try {
      if (invalidAccountAction === 'disable') {
        await setAuthFilesDisabled(selectedInvalidFileNames, true)
      } else {
        await deleteAuthFiles(selectedInvalidFileNames)
      }
      await Promise.all([onRefreshStatus(), onAfterInvalidAccountAction?.()])
      setInvalidAccountAction(null)
      setSelectedInvalidFileNames([])
    } catch (nextError) {
      setInvalidAccountError(nextError instanceof Error ? nextError.message : t('usage_stats.credentials_inspection_invalid_accounts_failed'))
    } finally {
      setInvalidAccountSubmitting(false)
    }
  }
  const inspectionCloseDisabled = invalidAccountAction !== null || invalidAccountSubmitting

  return (
    <Modal open={open} title={t('usage_stats.credentials_inspection_title')} onClose={inspectionCloseDisabled ? () => undefined : onClose} width={820} className={styles.credentialInspectionModal} closeDisabled={inspectionCloseDisabled}>
      <div className={styles.credentialInspectionPanel}>
        <div className={styles.credentialInspectionSummary}>
          <div className={styles.credentialInspectionMetric}>
            <span>{t('usage_stats.credentials_inspection_total')}</span>
            <strong>{total}</strong>
          </div>
          <div className={styles.credentialInspectionProgressBlock}>
            <div className={styles.credentialInspectionProgressHeader}>
              <span>{t('usage_stats.credentials_inspection_progress')}</span>
              <strong>{cached} / {progressTotal} ({progress}%)</strong>
            </div>
            <div
              className={styles.credentialInspectionProgressTrack}
              role="progressbar"
              aria-label={t('usage_stats.credentials_inspection_progress_aria', { progress: String(progress) })}
              aria-valuenow={progress}
              aria-valuemin={0}
              aria-valuemax={100}
            >
              <span className={styles.credentialInspectionProgressFill} style={{ width: `${progress}%` }} />
            </div>
            <div className={styles.credentialInspectionCompletedAt}>
              <span>{t('usage_stats.credentials_inspection_completed_at')}</span>
              <strong>{formatInspectionCompletedAt(status?.completed_at) || t('usage_stats.credentials_inspection_not_completed')}</strong>
            </div>
          </div>
          <button
            type="button"
            className={`${styles.credentialActionButton} ${styles.credentialInspectionStartButton}`.trim()}
            onClick={() => void onStart()}
            disabled={startDisabled}
            aria-busy={starting}
          >
            {starting ? <LoadingSpinner size={13} /> : <IconSearch size={13} />}
            <span>{startLabel}</span>
          </button>
        </div>

        {error && <div className={styles.credentialInlineError}>{error}</div>}
        {loading && !status && <div className={styles.credentialEmptyState}>{t('common.loading')}</div>}

        <div className={styles.credentialInspectionStatsGrid}>
          <InspectionStatCard tone="normal" label={t('usage_stats.credentials_inspection_normal')} value={status?.normal ?? 0} total={total} filterStatus="normal" active={resultStatusFilter === 'normal'} onSelect={handleSelectResultStatus} />
          <InspectionStatCard tone="limitReached" label={t('usage_stats.credentials_inspection_limit_reached')} value={status?.limit_reached ?? 0} total={total} filterStatus="limit_reached" active={resultStatusFilter === 'limit_reached'} onSelect={handleSelectResultStatus} />
          <InspectionStatCard tone="unauthorized" label={t('usage_stats.credentials_inspection_401_402')} value={status?.unauthorized_401_402 ?? 0} total={total} filterStatus="unauthorized_401_402" active={resultStatusFilter === 'unauthorized_401_402'} onSelect={handleSelectResultStatus} />
          <InspectionStatCard tone="failed" label={t('usage_stats.credentials_inspection_other_failed')} value={status?.other_failed ?? 0} total={total} filterStatus="other_failed" active={resultStatusFilter === 'other_failed'} onSelect={handleSelectResultStatus} />
          <InspectionStatCard tone="unknown" label={t('usage_stats.credentials_inspection_unknown')} value={status?.unknown ?? 0} total={total} />
        </div>

        <div className={styles.credentialInspectionResultsBlock}>
          <div className={styles.credentialInspectionResultsHeader}>
            <div className={styles.credentialInspectionResultsTitle}>{t('usage_stats.credentials_inspection_recent_results')}</div>
            {results.length > 0 && (
              <div className={styles.credentialInspectionResultControls}>
                <div className={styles.credentialInspectionInvalidActions}>
                  <button
                    type="button"
                    className={styles.credentialInspectionInvalidActionButton}
                    onClick={() => openInvalidAccountAction('disable')}
                    disabled={invalidFileNames.length === 0 || invalidAccountSubmitting}
                  >
                    <IconShield size={13} />
                    <span>{t('usage_stats.credentials_inspection_disable_invalid')}</span>
                  </button>
                  <button
                    type="button"
                    className={`${styles.credentialInspectionInvalidActionButton} ${styles.credentialInspectionInvalidActionButtonDanger}`.trim()}
                    onClick={() => openInvalidAccountAction('delete')}
                    disabled={invalidFileNames.length === 0 || invalidAccountSubmitting}
                  >
                    <IconTrash2 size={13} />
                    <span>{t('usage_stats.credentials_inspection_delete_invalid')}</span>
                  </button>
                </div>
              </div>
            )}
          </div>
          {resultPageData.total === 0 ? (
            <div className={styles.credentialEmptyState}>{t('usage_stats.credentials_inspection_empty_results')}</div>
          ) : (
            <>
              <div className={styles.credentialInspectionResultsTable}>
                {resultPageData.results.map((result) => <InspectionResultRow key={result.auth_index} result={result} />)}
              </div>
              <div className={styles.credentialInspectionResultsFooter}>
                <label className={styles.credentialInspectionPageSizeControl}>
                  <span>{t('usage_stats.rows_per_page')}</span>
                  <select value={resultPageData.pageSize} onChange={(event) => handleResultPageSizeChange(Number(event.target.value))}>
                    {INSPECTION_RESULT_PAGE_SIZE_OPTIONS.map((option) => <option key={option} value={option}>{option}</option>)}
                  </select>
                </label>
                <div className={styles.credentialInspectionPagination}>
                  <button type="button" onClick={() => setResultPage(resultPageData.page - 1)} disabled={resultPageData.page <= 1}>{t('usage_stats.previous_page')}</button>
                  <span>{resultPageData.page} / {resultPageData.totalPages}</span>
                  <button type="button" onClick={() => setResultPage(resultPageData.page + 1)} disabled={resultPageData.page >= resultPageData.totalPages}>{t('usage_stats.next_page')}</button>
                </div>
              </div>
            </>
          )}
        </div>
      </div>
      <InvalidInspectionAccountModal
        open={invalidAccountAction !== null}
        action={invalidAccountAction}
        fileNames={invalidFileNames}
        selectedFileNames={selectedInvalidFileNames}
        submitting={invalidAccountSubmitting}
        error={invalidAccountError}
        onToggleFileName={toggleInvalidFileName}
        onSelectAll={selectAllInvalidFileNames}
        onInvertSelection={invertInvalidFileNames}
        onCancel={closeInvalidAccountAction}
        onConfirm={handleConfirmInvalidAccountAction}
      />
    </Modal>
  )
}

function InvalidInspectionAccountModal({
  open,
  action,
  fileNames,
  selectedFileNames,
  submitting,
  error,
  onToggleFileName,
  onSelectAll,
  onInvertSelection,
  onCancel,
  onConfirm,
}: {
  open: boolean
  action: InvalidInspectionAccountAction | null
  fileNames: string[]
  selectedFileNames: string[]
  submitting: boolean
  error: string
  onToggleFileName: (fileName: string, checked: boolean) => void
  onSelectAll: () => void
  onInvertSelection: () => void
  onCancel: () => void
  onConfirm: () => void
}) {
  const { t } = useTranslation()
  const actionLabel = action === 'delete' ? t('usage_stats.credentials_inspection_delete_action') : t('usage_stats.credentials_inspection_disable_action')
  return (
    <Modal
      open={open}
      title={t('usage_stats.credentials_inspection_invalid_accounts_title', { action: actionLabel })}
      onClose={onCancel}
      width={600}
      className={styles.credentialInvalidAccountModal}
      closeDisabled={submitting}
      footer={(
        <div className={styles.credentialInvalidAccountFooter}>
          <button type="button" className={styles.credentialInvalidAccountCancelButton} onClick={onCancel} disabled={submitting}>
            {t('common.cancel')}
          </button>
          <button
            type="button"
            className={`${styles.credentialInvalidAccountConfirmButton} ${action === 'delete' ? styles.credentialInvalidAccountConfirmButtonDanger : ''}`.trim()}
            onClick={onConfirm}
            disabled={submitting || selectedFileNames.length === 0}
            aria-busy={submitting}
          >
            {submitting && <LoadingSpinner size={13} />}
            <span>{t('usage_stats.credentials_inspection_invalid_accounts_confirm', { action: actionLabel })}</span>
          </button>
        </div>
      )}
    >
      <div className={styles.credentialInvalidAccountPanel}>
        <p>{t(action === 'delete' ? 'usage_stats.credentials_inspection_delete_invalid_confirm' : 'usage_stats.credentials_inspection_disable_invalid_confirm')}</p>
        <div className={styles.credentialInvalidAccountTip}>{t('usage_stats.credentials_inspection_invalid_accounts_sync_tip')}</div>
        {error && <div className={styles.credentialInlineError}>{error}</div>}
        <div className={styles.credentialInvalidAccountToolbar}>
          <span>{selectedFileNames.length} / {fileNames.length}</span>
          <div className={styles.credentialInvalidAccountToolbarActions}>
            <button type="button" onClick={onSelectAll} disabled={submitting || fileNames.length === 0}>
              {t('usage_stats.credentials_inspection_invalid_accounts_select_all')}
            </button>
            <button type="button" onClick={onInvertSelection} disabled={submitting || fileNames.length === 0}>
              {t('usage_stats.credentials_inspection_invalid_accounts_invert_selection')}
            </button>
          </div>
        </div>
        <div className={styles.credentialInvalidAccountList}>
          {fileNames.map((fileName) => (
            <label key={fileName} className={styles.credentialInvalidAccountItem}>
              <input
                type="checkbox"
                checked={selectedFileNames.includes(fileName)}
                onChange={(event) => onToggleFileName(fileName, event.target.checked)}
                disabled={submitting}
              />
              <span>{fileName}</span>
            </label>
          ))}
        </div>
      </div>
    </Modal>
  )
}

function InspectionStatCard({ tone, label, value, total, filterStatus, active = false, onSelect }: { tone: InspectionStatTone; label: string; value: number; total: number; filterStatus?: InspectionResultStatusFilter; active?: boolean; onSelect?: (status: InspectionResultStatusFilter) => void }) {
  const percent = total > 0 ? Math.round((value / total) * 100) : 0
  const content = (
    <>
      <span>{label}</span>
      <strong>{value}</strong>
      <small>{percent}%</small>
    </>
  )
  const cardClassName = `${styles.credentialInspectionStatCard} ${styles[`credentialInspectionStatCard${capitalize(tone)}`]}`.trim()
  if (filterStatus && onSelect && isSelectableInspectionStatusFilter(filterStatus)) {
    return (
      <button
        type="button"
        className={`${cardClassName} ${styles.credentialInspectionStatButton} ${active ? styles.credentialInspectionStatButtonActive : ''}`.trim()}
        onClick={() => onSelect(filterStatus)}
        aria-pressed={active}
      >
        {content}
      </button>
    )
  }
  return (
    <div className={cardClassName}>
      {content}
    </div>
  )
}

function InspectionResultRow({ result }: { result: UsageQuotaInspectionResult }) {
  const { t } = useTranslation()
  return (
    <div className={styles.credentialInspectionResultRow}>
      <span className={styles.credentialInspectionTypeIcon}>
        <CredentialProviderFilterIcon provider={result.type} />
      </span>
      <span className={styles.credentialInspectionIdentity}>
        <strong>{result.name || result.file_name || '-'}</strong>
      </span>
      <span className={`${styles.credentialInspectionStatusPill} ${inspectionResultStatusClassName(result.status)}`.trim()}>
        {t(inspectionResultLabelKey(result.status))}
      </span>
      <span className={styles.credentialInspectionCheckedAt}>{formatInspectionDate(result.refreshed_at)}</span>
    </div>
  )
}

function inspectionResultLabelKey(status: UsageQuotaInspectionResult['status']): string {
  switch (status) {
    case 'normal':
      return 'usage_stats.credentials_inspection_normal'
    case 'limit_reached':
      return 'usage_stats.credentials_inspection_limit_reached'
    case 'unauthorized_401':
      return 'usage_stats.credentials_inspection_401'
    case 'payment_required_402':
      return 'usage_stats.credentials_inspection_402'
    default:
      return 'usage_stats.credentials_inspection_other_failed'
  }
}

function inspectionResultStatusClassName(status: UsageQuotaInspectionResult['status']): string {
  switch (status) {
    case 'normal':
      return styles.credentialInspectionStatusNormal
    case 'limit_reached':
      return styles.credentialInspectionStatusLimitReached
    case 'unauthorized_401':
      return styles.credentialInspectionStatusUnauthorized
    case 'payment_required_402':
      return styles.credentialInspectionStatusPayment
    default:
      return styles.credentialInspectionStatusFailed
  }
}

export function formatInspectionCompletedAt(value: string | undefined): string {
  if (!value) {
    return ''
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  return date.toLocaleString()
}

function formatInspectionDate(value: string | undefined): string {
  return formatInspectionCompletedAt(value)
}

function CredentialPlanBadge({ children, tone = 'neutral' }: { children: string; tone?: PlanTypeTone }) {
  return <span className={`${styles.credentialPlanBadge} ${styles[`credentialPlanBadge${capitalize(tone)}`]}`.trim()}>{children}</span>
}

function QuotaUsageModeSwitch({ label, mode, onChange }: { label: string; mode: QuotaUsageMode; onChange: (mode: QuotaUsageMode) => void }) {
  const { t } = useTranslation()

  return (
    <div className={styles.credentialQuotaModeControl}>
      <span>{label}</span>
      <div className={styles.credentialQuotaModeSwitcher} role="group" aria-label={t('usage_stats.credentials_quota_usage_mode_aria')}>
        <span className={`${styles.credentialQuotaModeThumb} ${mode === 'estimated' ? styles.credentialQuotaModeThumbEstimated : ''}`.trim()} aria-hidden="true" />
        <button
          type="button"
          className={mode === 'current' ? styles.credentialQuotaModeButtonActive : undefined}
          onClick={() => onChange('current')}
          aria-pressed={mode === 'current'}
        >
          {t('usage_stats.credentials_quota_usage_mode_current')}
        </button>
        <button
          type="button"
          className={mode === 'estimated' ? styles.credentialQuotaModeButtonActive : undefined}
          onClick={() => onChange('estimated')}
          aria-pressed={mode === 'estimated'}
        >
          {t('usage_stats.credentials_quota_usage_mode_estimated')}
        </button>
      </div>
    </div>
  )
}

function AuthFileDisplayModeSwitch({ mode, onChange }: { mode: AuthFileDisplayMode; onChange: (mode: AuthFileDisplayMode) => void }) {
  const { t } = useTranslation()

  return (
    <div className={styles.credentialDisplayModeControl}>
      <div className={styles.credentialDisplayModeSwitcher} role="group" aria-label={t('usage_stats.credentials_auth_files_display_mode_aria')}>
        <span className={`${styles.credentialDisplayModeThumb} ${mode === 'health' ? styles.credentialDisplayModeThumbHealth : ''}`.trim()} aria-hidden="true" />
        <button
          type="button"
          className={mode === 'quota' ? styles.credentialDisplayModeButtonActive : undefined}
          onClick={() => onChange('quota')}
          aria-pressed={mode === 'quota'}
        >
          <IconShield size={12} />
          <span>{t('usage_stats.credentials_auth_files_display_mode_quota')}</span>
        </button>
        <button
          type="button"
          className={mode === 'health' ? styles.credentialDisplayModeButtonActive : undefined}
          onClick={() => onChange('health')}
          aria-pressed={mode === 'health'}
        >
          <IconChartLine size={12} />
          <span>{t('usage_stats.credentials_auth_files_display_mode_health')}</span>
        </button>
      </div>
    </div>
  )
}

export function readStoredAuthFileDisplayMode(): AuthFileDisplayMode {
  if (typeof window === 'undefined') {
    return 'quota'
  }
  try {
    const storedMode = window.localStorage?.getItem(AUTH_FILE_DISPLAY_MODE_STORAGE_KEY)
    return isAuthFileDisplayMode(storedMode) ? storedMode : 'quota'
  } catch {
    return 'quota'
  }
}

export function persistAuthFileDisplayMode(mode: AuthFileDisplayMode): void {
  if (typeof window === 'undefined') {
    return
  }
  try {
    window.localStorage?.setItem(AUTH_FILE_DISPLAY_MODE_STORAGE_KEY, mode)
  } catch {
    // localStorage 可能被隐私模式或浏览器策略禁用，忽略后保持本次页面内状态。
  }
}

function isAuthFileDisplayMode(value: string | null | undefined): value is AuthFileDisplayMode {
  return value === 'quota' || value === 'health'
}

export function AuthFileQuotaPanel({ row, quotaUsageMode }: { row: AuthFileCredentialRow; quotaUsageMode: QuotaUsageMode }) {
  const { t } = useTranslation()

  // 限额区域按加载、错误、刷新中、无缓存、可展示数据的顺序降级。
  if (row.quotaLoading) {
    return <div className={styles.credentialQuotaStateSlot}><div className={styles.credentialQuotaState}>{t('usage_stats.credentials_quota_loading')}</div></div>
  }
  if (row.quotaError) {
    const errorDisplay = formatQuotaErrorDisplay(row.quotaError)
    return (
      <div className={styles.credentialQuotaStateSlot}>
        <div className={styles.credentialQuotaErrorSummary} title={errorDisplay.title}>
          {errorDisplay.code && <span className={styles.credentialQuotaErrorCode}>{errorDisplay.code}</span>}
          <span className={styles.credentialQuotaErrorMessage}>{errorDisplay.message}</span>
        </div>
      </div>
    )
  }
  if (row.refreshStatus === 'queued' || row.refreshStatus === 'running') {
    return <div className={styles.credentialQuotaStateSlot}><div className={styles.credentialQuotaRefreshStatus}>{t(`usage_stats.credentials_refresh_status_${row.refreshStatus}`)}</div></div>
  }
  if (row.displayQuotas.length === 0) {
    return <div className={styles.credentialQuotaStateSlot}><div className={styles.credentialQuotaState}>{t('usage_stats.credentials_quota_unavailable')}</div></div>
  }

  return (
    <div className={styles.credentialQuotaPanel}>
      <div className={styles.credentialQuotaBars}>
        {/* 每个可计算进度的 quota 都独占一个稳定块；不可进度化 quota 在 view model 中已过滤。 */}
        {row.displayQuotas.map((quota) => <QuotaBar key={quota.key} quota={quota} quotaUsageMode={quotaUsageMode} />)}
      </div>
    </div>
  )
}

export function formatQuotaErrorDisplay(error: string | undefined): QuotaErrorDisplay {
  const title = (error || '').trim()
  const raw = title || 'Quota refresh failed. Please try again later.'
  const { code, message } = splitHTTPStatus(raw)
  const structured = quotaErrorDetailsFromStructuredValue(message || raw)
  const displayCode = code || structured.code
  const sourceMessage = structured.message || (isStructuredQuotaErrorValue(message || raw) ? '' : (message || raw))
  const readableMessage = readableQuotaErrorMessage(sourceMessage, displayCode ? `HTTP ${displayCode}` : 'Quota refresh failed. Please try again later.')
  return {
    code: displayCode,
    message: readableMessage,
    title: raw,
  }
}

function splitHTTPStatus(value: string): { code?: string; message: string } {
  const trimmed = value.trim()
  const match = trimmed.match(/^HTTP\s+(\d{3})(?=\D|$)(?::|\s+-)?\s*([\s\S]*)$/i) ?? trimmed.match(/^(\d{3})(?=\D|$)(?::|\s+-)?\s*([\s\S]*)$/)
  if (!match) {
    return { message: trimmed }
  }
  return { code: match[1], message: match[2].trim() }
}

function readableQuotaErrorMessage(value: string, fallback: string): string {
  const normalized = (value || fallback).replace(/\s+/g, ' ').trim() || fallback
  return truncateQuotaErrorMessage(normalized)
}

function quotaErrorDetailsFromStructuredValue(value: string, depth = 0): QuotaErrorDetails {
  const trimmed = value.trim()
  if (!trimmed || depth > QUOTA_ERROR_PARSE_MAX_DEPTH || !isStructuredQuotaErrorValue(trimmed)) {
    return {}
  }
  try {
    return quotaErrorDetailsFromParsedValue(JSON.parse(trimmed), depth + 1)
  } catch {
    return {}
  }
}

function quotaErrorDetailsFromParsedValue(value: unknown, depth: number): QuotaErrorDetails {
  if (depth > QUOTA_ERROR_PARSE_MAX_DEPTH) {
    return {}
  }
  if (typeof value === 'string') {
    const trimmed = value.trim()
    if (!trimmed) {
      return {}
    }
    if (isStructuredQuotaErrorValue(trimmed)) {
      const structured = quotaErrorDetailsFromStructuredValue(trimmed, depth + 1)
      if (structured.code || structured.message) {
        return structured
      }
    }
    const httpStatus = splitHTTPStatus(trimmed)
    if (httpStatus.code) {
      return mergeQuotaErrorDetails({ code: httpStatus.code }, quotaErrorDetailsFromStructuredValue(httpStatus.message, depth + 1), { message: httpStatus.message })
    }
    return { message: trimmed }
  }
  if (Array.isArray(value)) {
    return value.reduce<QuotaErrorDetails>((current, item) => mergeQuotaErrorDetails(current, quotaErrorDetailsFromParsedValue(item, depth + 1)), {})
  }
  if (!value || typeof value !== 'object') {
    return {}
  }
  const record = value as Record<string, unknown>
  let details: QuotaErrorDetails = { code: quotaHTTPStatusCodeFromRecord(record) }
  const nestedKeys = ['body', 'body_text', 'bodyText', 'response', 'data', 'payload', 'error', 'errors']
  // provider 错误常带一层通用 message，真实上游错误在 body/error 等字段里，先解析内层响应体。
  for (const key of nestedKeys) {
    if (!isPreferredNestedQuotaErrorValue(key, record[key])) {
      continue
    }
    details = mergeQuotaErrorDetails(details, quotaErrorDetailsFromParsedValue(record[key], depth + 1))
    if (details.message) {
      break
    }
  }
  if (!details.message) {
    for (const key of ['message', 'error_description', 'detail', 'description', 'title', 'reason']) {
      const value = record[key]
      if (typeof value !== 'string') {
        continue
      }
      const nested = quotaErrorDetailsFromParsedValue(value, depth + 1)
      details = mergeQuotaErrorDetails(details, nested.message === value.trim() ? { message: value.trim() } : nested)
      if (details.message) {
        break
      }
    }
  }
  for (const key of nestedKeys) {
    if (record[key] === undefined) {
      continue
    }
    details = mergeQuotaErrorDetails(details, quotaErrorDetailsFromParsedValue(record[key], depth + 1))
    if (details.code && details.message) {
      break
    }
  }
  return details
}

function isPreferredNestedQuotaErrorValue(key: string, value: unknown): boolean {
  if (value === undefined || value === null) {
    return false
  }
  if (typeof value !== 'string') {
    return typeof value === 'object'
  }
  const trimmed = value.trim()
  if (!trimmed) {
    return false
  }
  if (['body', 'body_text', 'bodyText', 'response', 'data', 'payload'].includes(key)) {
    return true
  }
  return isStructuredQuotaErrorValue(trimmed) || Boolean(splitHTTPStatus(trimmed).code)
}

function isStructuredQuotaErrorValue(value: string): boolean {
  const trimmed = value.trim()
  return ['{', '[', '"'].includes(trimmed[0] ?? '')
}

function quotaHTTPStatusCodeFromRecord(record: Record<string, unknown>): string | undefined {
  for (const key of ['http_status_code', 'status_code', 'statusCode', 'status', 'code']) {
    const code = quotaHTTPStatusCode(record[key])
    if (code) {
      return code
    }
  }
  return undefined
}

function quotaHTTPStatusCode(value: unknown): string | undefined {
  if (typeof value === 'number' && Number.isInteger(value) && value >= 100 && value <= 599) {
    return String(value)
  }
  if (typeof value !== 'string') {
    return undefined
  }
  const match = value.trim().match(/^(?:HTTP\s+)?(\d{3})(?:\D|$)/i)
  if (!match) {
    return undefined
  }
  const status = Number(match[1])
  if (status < 100 || status > 599) {
    return undefined
  }
  return match[1]
}

function mergeQuotaErrorDetails(...items: QuotaErrorDetails[]): QuotaErrorDetails {
  return items.reduce<QuotaErrorDetails>((current, item) => ({
    code: current.code || item.code,
    message: current.message || item.message,
  }), {})
}

function truncateQuotaErrorMessage(value: string): string {
  if (value.length <= QUOTA_ERROR_MESSAGE_MAX_LENGTH) {
    return value
  }
  return `${value.slice(0, QUOTA_ERROR_MESSAGE_MAX_LENGTH).trimEnd()}...`
}

export function formatQuotaResetLabel(resetAt: string): string {
  const resetTime = new Date(resetAt)
  const resetMs = resetTime.getTime()
  if (!Number.isFinite(resetMs)) {
    return ''
  }
  const month = String(resetTime.getMonth() + 1).padStart(2, '0')
  const day = String(resetTime.getDate()).padStart(2, '0')
  const hour = String(resetTime.getHours()).padStart(2, '0')
  const minute = String(resetTime.getMinutes()).padStart(2, '0')
  return `${month}/${day} ${hour}:${minute}`
}

export function formatQuotaResetDuration(resetAt: string): string {
  const resetMs = new Date(resetAt).getTime()
  if (!Number.isFinite(resetMs)) {
    return ''
  }
  const remainingMinutes = Math.max(0, Math.ceil((resetMs - Date.now()) / 60_000))
  const days = Math.floor(remainingMinutes / 1_440)
  const hours = Math.floor((remainingMinutes % 1_440) / 60)
  const minutes = remainingMinutes % 60
  return days > 0 ? `${days}d${hours}h${minutes}m` : `${hours}h${minutes}m`
}

export function formatQuotaWindowUsageAriaLabel(t: Translate, windowUsage: NonNullable<DisplayQuota['windowUsage']>): string {
  return t('usage_stats.credentials_quota_window_usage_aria', {
    tokens: windowUsage.tokens,
    cost: windowUsage.cost,
  })
}

export function formatQuotaBillingUsageAriaLabel(t: Translate, billingUsage: NonNullable<DisplayQuota['billingUsage']>): string {
  return t('usage_stats.credentials_quota_billing_usage_aria', {
    used: billingUsage.used ?? '-',
    limit: billingUsage.limit ?? '-',
    remaining: billingUsage.remaining ?? '-',
  })
}

function QuotaBar({ quota, quotaUsageMode }: { quota: DisplayQuota; quotaUsageMode: QuotaUsageMode }) {
  const { t } = useTranslation()
  // 条宽使用剩余额度百分比，颜色跟随剩余风险状态从绿到黄到红。
  const percent = quota.barPercent ?? 0
  const width = `${Math.max(0, Math.min(100, percent))}%`
  const percentLabel = quota.barPercent === null ? '' : `${Math.round(quota.barPercent)}%`
  const resetLabel = quota.resetText ? formatQuotaResetLabel(quota.resetText) : ''
  const resetDuration = quota.resetText ? formatQuotaResetDuration(quota.resetText) : ''
  const billingUsage = quota.billingUsage
  const windowUsage = billingUsage ? undefined : quotaWindowUsageForMode(quota, quotaUsageMode)

  return (
    <div className={styles.credentialQuotaBarBlock}>
      <div className={styles.credentialQuotaBarHeader}>
        <span className={styles.credentialQuotaLabelGroup}>
          <span>{quota.label}</span>
        </span>
        {(resetDuration || percentLabel) && (
          <span className={styles.credentialQuotaValueGroup}>
            {resetDuration && <span className={styles.credentialQuotaResetDuration}>{resetDuration}</span>}
            {percentLabel && <strong>{percentLabel}</strong>}
          </span>
        )}
      </div>
      <div className={styles.credentialQuotaTrack}>
        <span className={`${styles.credentialQuotaFill} ${credentialToneClassName('credentialQuotaFill', quota.status)}`.trim()} style={{ width }} />
      </div>
      <div className={styles.credentialQuotaMeta}>
        {billingUsage && (
          <strong className={styles.credentialQuotaWindowUsage} aria-label={formatQuotaBillingUsageAriaLabel(t, billingUsage)}>
            <span className={styles.credentialQuotaUsageMetric}>
              <img src={quotaCostIcon} alt="" aria-hidden="true" />
              <span>{formatQuotaBillingUsageText(billingUsage)}</span>
            </span>
          </strong>
        )}
        {windowUsage && (
          <strong className={styles.credentialQuotaWindowUsage} aria-label={formatQuotaWindowUsageAriaLabel(t, windowUsage)}>
            <span className={styles.credentialQuotaUsageMetric}>
              <img src={quotaTokenIcon} alt="" aria-hidden="true" />
              <span>{windowUsage.tokens}</span>
            </span>
            <span className={styles.credentialQuotaUsageMetric}>
              <img src={quotaCostIcon} alt="" aria-hidden="true" />
              <span>{windowUsage.cost}</span>
            </span>
          </strong>
        )}
        {resetLabel && <span>{resetLabel}</span>}
      </div>
    </div>
  )
}

function formatQuotaBillingUsageText(billingUsage: NonNullable<DisplayQuota['billingUsage']>): string {
  if (billingUsage.used && billingUsage.limit) {
    return `${billingUsage.used} / ${billingUsage.limit}`
  }
  return billingUsage.used ?? billingUsage.remaining ?? billingUsage.limit ?? ''
}

function quotaWindowUsageForMode(quota: DisplayQuota, mode: QuotaUsageMode): DisplayQuota['windowUsage'] {
  if (mode === 'estimated' && quota.windowUsageEstimate) {
    return quota.windowUsageEstimate
  }
  return quota.windowUsage
}
