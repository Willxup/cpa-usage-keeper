import { useCallback, useEffect, useId, useRef, useState, type Ref } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/Button'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { MainActionButton } from '@/components/ui/MainActionButton'
import { Modal } from '@/components/ui/Modal'
import { Select } from '@/components/ui/Select'
import { IconChartLine, IconGaugeReset, IconRefreshCw, IconSearch, IconSettings, IconShield, IconTrash2 } from '@/components/ui/icons'
import quotaCostIcon from '@/assets/icons/quota-cost.svg'
import quotaTokenIcon from '@/assets/icons/quota-token.svg'
import styles from './CredentialSections.module.scss'
import { formatCredentialTimestamp, type AuthFileCredentialRow, type DisplayQuota } from './credentialViewModels'
import { deleteAuthFiles, fetchClaudeResetGrants, fetchQuotaAutoRefreshSettings, fetchUsageQuotaResetCredits, setAuthFilesDisabled, updateQuotaAutoRefreshSettings, type UsageIdentityPageSort } from '@/lib/api'
import type { ClaudeResetGrantsResponse, QuotaAutoRefreshScheduleUnit, QuotaAutoRefreshSettings, UsageQuotaInspectionResult, UsageQuotaInspectionResultStatus, UsageQuotaInspectionStatusResponse, UsageQuotaResetCreditsResponse, UsageQuotaResetResponse } from '@/lib/types'
import { CredentialAliasEditor } from './CredentialAliasEditor'
import { CredentialHealthPanel } from './CredentialHealthPanel'
import { CredentialSubscriptionBadge } from './CredentialSubscriptionBadge'
import { CredentialKimiSiteBadge, kimiCredentialSite } from './CredentialKimiSiteBadge'
import { CredentialRowShell, CredentialSectionShell, CredentialTableHeader, CredentialsPagination, MetricPill, RequestMetric, TonePercent, cacheReadRateTone, capitalize, credentialToneClassName, formatCredentialNumber, successRateTone } from './CredentialSectionShell'
import { CredentialPriorityEditor } from './CredentialPriorityEditor'
import { ProviderBrandIcon, providerBrandIconKey } from '@/components/ProviderBrandIcon'
import { CredentialStatusToggle } from './CredentialStatusToggle'

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
  top?: number
  bottom?: number
  right: number
  maxHeight: number
}
type CredentialExpiryTooltipTarget = {
  rowKey: string
  anchor: HTMLSpanElement
}
type CredentialExpiryTooltipState = {
  rowKey: string
  x: number
  y: number
  placement: 'above' | 'below'
}

const RESET_CREDITS_LOOKUP_TIMEOUT_MS = 5_000
const RESET_CREDITS_POPOVER_VIEWPORT_MARGIN = 12
const RESET_CREDITS_POPOVER_OFFSET = 8
const RESET_CREDITS_POPOVER_MAX_HEIGHT = 360
// 与后端默认额度处理器一致；品牌图标支持的类型不等于额度接口支持的类型。
const QUOTA_PROVIDER_TYPES = new Set(['antigravity', 'codex', 'gemini-cli', 'claude', 'kimi', 'xai'])
const CREDENTIAL_EXPIRY_TOOLTIP_SAFE_WIDTH = 300
const CREDENTIAL_EXPIRY_TOOLTIP_ESTIMATED_HEIGHT = 48
const CREDENTIAL_EXPIRY_TOOLTIP_OFFSET = 10
const CREDENTIAL_EXPIRY_TOOLTIP_VIEWPORT_PADDING = 8

const QUOTA_ERROR_MESSAGE_MAX_LENGTH = 96
const QUOTA_ERROR_PARSE_MAX_DEPTH = 10
const AUTH_FILE_DISPLAY_MODE_STORAGE_KEY = 'cpa.credentials.authFiles.displayMode'
const ANTIGRAVITY_QUOTA_GROUP_KEYS = new Set([
  'antigravity-gemini-models',
  'antigravity-claude-and-gpt-models',
])
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
const AUTO_REFRESH_SCHEDULE_LIMITS: Record<QuotaAutoRefreshScheduleUnit, number> = {
  minute: 60,
  hour: 24,
  day: 30,
  week: 7,
}
const AUTO_REFRESH_SCHEDULE_UNITS: QuotaAutoRefreshScheduleUnit[] = ['minute', 'hour', 'day', 'week']
const AUTO_REFRESH_WEEKDAYS = [1, 2, 3, 4, 5, 6, 7] as const

interface AuthFileCredentialsSectionProps {
  rows: AuthFileCredentialRow[]
  timeZone?: string
  total: number
  page: number
  totalPages: number
  pageSize: number
  activeOnly: boolean
  sort: UsageIdentityPageSort
  loading: boolean
  quotaRefreshing: boolean
  quotaRefreshError: string
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
  onResetQuotaForAuthIndex: (authIndex: string, grantId?: string, organizationId?: string) => Promise<UsageQuotaResetResponse | void>
  /** 编辑行消失时，焦点留在当前列表的原生筛选控件，不改变页面容器。 */
  editFallbackRef?: Ref<HTMLInputElement>
  onEdit?: (row: AuthFileCredentialRow) => void
  onOpenDetails?: (row: AuthFileCredentialRow) => void
  /** 正在写入上游状态的 Keeper identity id 集合，用于阻止重复点击。 */
  statusPendingIdentityIds?: ReadonlySet<string>
  onToggleStatus?: (identityId: string, authIndex: string, disabled: boolean) => void
  onSavePriority?: (identityId: string, authIndex: string, priority: number) => Promise<void>
  onRefreshInspectionStatus: () => Promise<void>
  onStartInspection: () => Promise<void>
  onAfterInvalidAccountAction?: () => Promise<void>
}

export function AuthFileCredentialsSection({ rows, timeZone, total, page, totalPages, pageSize, activeOnly, sort, loading, quotaRefreshing, quotaRefreshError, quotaInspectionStatus, quotaInspectionLoading, quotaInspectionStarting, quotaInspectionError, onPageChange, onPageSizeChange, onActiveOnlyChange, onSortChange, onRefreshQuota, onRefreshQuotaForAuthIndex, onResetQuotaForAuthIndex, editFallbackRef, onEdit, onOpenDetails, statusPendingIdentityIds, onToggleStatus, onSavePriority, onRefreshInspectionStatus, onStartInspection, onAfterInvalidAccountAction }: AuthFileCredentialsSectionProps) {
  const { t } = useTranslation()
  const [inspectionOpen, setInspectionOpen] = useState(false)
  const [quotaUsageMode, setQuotaUsageMode] = useState<QuotaUsageMode>('current')
  const [displayMode, setDisplayModeState] = useState<AuthFileDisplayMode>(() => readStoredAuthFileDisplayMode())
  const [expiryTooltip, setExpiryTooltip] = useState<CredentialExpiryTooltipState | null>(null)
  const expiryTooltipHoverTargetRef = useRef<CredentialExpiryTooltipTarget | null>(null)
  const expiryTooltipFocusTargetRef = useRef<CredentialExpiryTooltipTarget | null>(null)
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

  const positionExpiryTooltip = useCallback((target: CredentialExpiryTooltipTarget | null) => {
    if (!target?.anchor.isConnected) {
      setExpiryTooltip(null)
      return
    }
    const viewportWidth = typeof window === 'undefined' ? 1024 : window.innerWidth
    const viewportHeight = typeof window === 'undefined' ? 768 : window.innerHeight
    const rect = target.anchor.getBoundingClientRect()
    const tooltipWidth = Math.min(
      CREDENTIAL_EXPIRY_TOOLTIP_SAFE_WIDTH,
      Math.max(viewportWidth - CREDENTIAL_EXPIRY_TOOLTIP_VIEWPORT_PADDING * 2, 0),
    )
    const halfTooltipWidth = tooltipWidth / 2
    const minX = CREDENTIAL_EXPIRY_TOOLTIP_VIEWPORT_PADDING + halfTooltipWidth
    const maxX = viewportWidth - CREDENTIAL_EXPIRY_TOOLTIP_VIEWPORT_PADDING - halfTooltipWidth
    const anchorX = rect.left + rect.width / 2
    const x = maxX >= minX ? Math.max(minX, Math.min(anchorX, maxX)) : viewportWidth / 2
    const spaceBelow = viewportHeight - rect.bottom - CREDENTIAL_EXPIRY_TOOLTIP_OFFSET - CREDENTIAL_EXPIRY_TOOLTIP_VIEWPORT_PADDING
    const spaceAbove = rect.top - CREDENTIAL_EXPIRY_TOOLTIP_OFFSET - CREDENTIAL_EXPIRY_TOOLTIP_VIEWPORT_PADDING
    const placement = spaceBelow >= CREDENTIAL_EXPIRY_TOOLTIP_ESTIMATED_HEIGHT || spaceBelow >= spaceAbove ? 'below' : 'above'
    const y = placement === 'above' ? rect.top - CREDENTIAL_EXPIRY_TOOLTIP_OFFSET : rect.bottom + CREDENTIAL_EXPIRY_TOOLTIP_OFFSET
    setExpiryTooltip({ rowKey: target.rowKey, x, y, placement })
  }, [])

  const syncExpiryTooltip = useCallback(() => {
    if (expiryTooltipHoverTargetRef.current && !expiryTooltipHoverTargetRef.current.anchor.isConnected) {
      expiryTooltipHoverTargetRef.current = null
    }
    if (expiryTooltipFocusTargetRef.current && !expiryTooltipFocusTargetRef.current.anchor.isConnected) {
      expiryTooltipFocusTargetRef.current = null
    }
    positionExpiryTooltip(expiryTooltipHoverTargetRef.current ?? expiryTooltipFocusTargetRef.current)
  }, [positionExpiryTooltip])

  useEffect(() => {
    window.addEventListener('resize', syncExpiryTooltip)
    window.addEventListener('scroll', syncExpiryTooltip, true)
    return () => {
      window.removeEventListener('resize', syncExpiryTooltip)
      window.removeEventListener('scroll', syncExpiryTooltip, true)
    }
  }, [syncExpiryTooltip])

  const activeExpiryRow = expiryTooltip
    ? rows.find((row) => (row.identity.id || row.identity.identity) === expiryTooltip.rowKey)
    : undefined
  const activeExpiryTooltipText = activeExpiryRow?.expiresAtLabel
    ? t('usage_stats.credentials_expiry_tooltip', { value: activeExpiryRow.expiresAtLabel })
    : ''

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
              <input ref={editFallbackRef} type="checkbox" checked={activeOnly} onChange={(event) => onActiveOnlyChange(event.target.checked)} />
              <span className={styles.credentialActiveOnlyTrack} aria-hidden="true">
                <span className={styles.credentialActiveOnlyThumb} />
              </span>
            </label>
            <AuthFileDisplayModeSwitch mode={displayMode} onChange={setDisplayMode} />
          </div>
        )}
        actions={(
          <div className={styles.credentialSectionActionButtons}>
            <MainActionButton
              type="button"
              className={styles.credentialInspectionButton}
              onClick={openInspection}
              aria-pressed={inspectionTone !== 'idle'}
            >
              <IconSearch size={12} />
              <span>{t('usage_stats.credentials_inspection_open')}</span>
              {inspectionTone !== 'idle' && <span className={`${styles.credentialInspectionDot} ${styles[`credentialInspectionDot${capitalize(inspectionTone)}`]}`.trim()} aria-hidden="true" />}
            </MainActionButton>
            <MainActionButton
              type="button"
              onClick={() => void onRefreshQuota()}
              disabled={!canRefresh}
              loading={quotaRefreshing}
            >
              {!quotaRefreshing && <IconRefreshCw size={12} />}
              <span>{quotaRefreshing ? t('usage_stats.credentials_quota_refreshing') : t('usage_stats.credentials_quota_refresh_current_page')}</span>
            </MainActionButton>
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
          cacheReadRateLabel={t('usage_stats.cache_rate')}
          sideLabel={showHealthMode ? t('usage_stats.credentials_column_health') : t('usage_stats.credentials_column_quota')}
        />
      )}
      {rows.map((row) => {
        const rowRefreshing = isRowRefreshing(row)
        const normalizedProvider = row.identity.provider?.trim().toLowerCase() ?? ''
        const quotaProvider = QUOTA_PROVIDER_TYPES.has(normalizedProvider) ? normalizedProvider : row.identity.type?.trim().toLowerCase()
        const isClaude = quotaProvider === 'claude'
        const resetCredits = (isClaude ? row.claudeResetGrants?.availableCount : row.quotaResetCreditsAvailableCount) ?? 0
        const canResetQuota = resetCredits > 0 && !row.identity.is_deleted && !rowRefreshing && !row.quotaResetting
        const rowKey = row.identity.id || row.identity.identity
        const rowExpiryTooltipText = row.expiresAtLabel
          ? t('usage_stats.credentials_expiry_tooltip', { value: row.expiresAtLabel })
          : ''
        return (
          <CredentialRowShell
            key={rowKey}
            icon={(
              // 认证文件 type 直接来自 CPA，可能是不认识的类型；没有品牌图标的类型不给开关，
              // 否则左侧图标槽位会变成一个看不见却可点、可 Tab 的按钮。
              providerBrandIconKey(row.identity.type) ? (
                <CredentialStatusToggle
                  providerType={row.identity.type}
                  displayName={row.displayName}
                  disabled={row.identity.disabled}
                  pending={statusPendingIdentityIds?.has(row.identity.id || row.identity.identity) ?? false}
                  readOnly={row.identity.is_deleted}
                  onToggle={(disabled) => onToggleStatus?.(row.identity.id || row.identity.identity, row.identity.identity, disabled)}
                />
              ) : (
                <ProviderBrandIcon providerType={row.identity.type} size={30} ariaLabel={row.typeLabel} />
              )
            )}
            title={onEdit ? (
              <CredentialAliasEditor
                identityId={row.identity.id}
                displayName={row.displayName}

                disabled={row.identity.is_deleted}
                onOpenDetails={onOpenDetails ? () => onOpenDetails(row) : undefined}
                onEdit={() => onEdit(row)}
              />
            ) : onOpenDetails ? (
              <button
                type="button"
                className={styles.credentialDetailNameButton}
                data-credential-detail-trigger="true"
                onClick={() => onOpenDetails(row)}
              >
                <span className={styles.credentialDetailNameText}>{row.displayName}</span>
                <span className={styles.credentialDetailNameArrow} aria-hidden="true">‹</span>
              </button>
            ) : <span>{row.displayName}</span>}
            subtitle={kimiCredentialSite(row.identity.type) || row.subscriptionBadge || row.remainingDaysLabel || row.priorityLabel || (onSavePriority && !row.identity.is_deleted) ? (
              <span className={styles.credentialIdentityBadges}>
                <CredentialKimiSiteBadge identityType={row.identity.type} />
                {row.subscriptionBadge && <CredentialSubscriptionBadge model={row.subscriptionBadge} />}
                {row.remainingDaysLabel && row.expiresAtLabel
                  ? (
                    <span
                      className={styles.credentialRemainingDaysBadge}
                      tabIndex={0}
                      aria-label={`${row.remainingDaysLabel}; ${rowExpiryTooltipText}`}
                      onMouseEnter={(event) => {
                        expiryTooltipHoverTargetRef.current = { rowKey, anchor: event.currentTarget }
                        syncExpiryTooltip()
                      }}
                      onMouseLeave={(event) => {
                        if (expiryTooltipHoverTargetRef.current?.anchor === event.currentTarget) {
                          expiryTooltipHoverTargetRef.current = null
                        }
                        syncExpiryTooltip()
                      }}
                      onFocus={(event) => {
                        expiryTooltipFocusTargetRef.current = { rowKey, anchor: event.currentTarget }
                        syncExpiryTooltip()
                      }}
                      onBlur={(event) => {
                        if (expiryTooltipFocusTargetRef.current?.anchor === event.currentTarget) {
                          expiryTooltipFocusTargetRef.current = null
                        }
                        syncExpiryTooltip()
                      }}
                    >
                      {row.remainingDaysLabel}
                    </span>
                  )
                  : row.remainingDaysLabel && <span className={styles.credentialRemainingDaysBadge}>{row.remainingDaysLabel}</span>}
                <CredentialPriorityEditor
                  priority={row.identity.priority}
                  displayName={row.displayName}
                  readOnly={row.identity.is_deleted}
                  onSave={onSavePriority ? (priority) => onSavePriority(row.identity.id || row.identity.identity, row.identity.identity, priority) : undefined}
                />
              </span>
            ) : undefined}
            badges={null}
            metricsTitle={row.identity.stats_reset_at ? t('usage_stats.credentials_stats_since', { time: formatCredentialTimestamp(row.identity.stats_reset_at) ?? row.identity.stats_reset_at }) : undefined}
            metrics={(
              <>
                <MetricPill value={<RequestMetric total={row.totalRequests} success={row.successCount} failure={row.failureCount} />} />
                <MetricPill value={<TonePercent value={row.successRate} tone={successRateTone(row.successRate)} />} />
                <MetricPill value={formatCredentialNumber(row.totalTokens)} />
                <MetricPill value={<TonePercent value={row.cacheReadRate} tone={cacheReadRateTone(row.cacheReadRate)} />} />
              </>
            )}
            rowClassName={styles.authFileCredentialRow}
            side={showHealthMode ? (
              <CredentialHealthPanel displayName={row.displayName} health={row.credentialHealth} lastUsedAt={row.identity.last_used_at} statsUpdatedAt={row.identity.stats_updated_at} windowCacheReadRate={row.windowCacheReadRate} />
            ) : (
              <div className={styles.credentialQuotaSideWithAction}>
                <AuthFileQuotaPanel row={row} quotaUsageMode={quotaUsageMode} timeZone={timeZone} />
                <div className={styles.credentialQuotaActionStack}>
                  {/* reset 按钮只在官方缓存给出可用次数时展示；refresh 始终保留在右侧列居中位置。 */}
                  {resetCredits > 0 && (
                    <QuotaResetAction
                      authIndex={row.identity.identity}
                      resetCredits={resetCredits}
                      disabled={!canResetQuota}
                      loading={row.quotaResetting === true}
                      provider={isClaude ? 'claude' : 'codex'}
                      key={`${row.identity.id}:${row.identity.identity}:${isClaude}`}
                      onConfirm={(grantId, organizationId) => isClaude ? onResetQuotaForAuthIndex(row.identity.identity, grantId, organizationId) : onResetQuotaForAuthIndex(row.identity.identity)}
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
      {expiryTooltip && activeExpiryTooltipText && typeof document !== 'undefined'
        ? createPortal(
            <div
              className={styles.credentialExpiryTooltip}
              role="tooltip"
              style={{
                left: expiryTooltip.x,
                top: expiryTooltip.y,
                transform: expiryTooltip.placement === 'above'
                  ? 'translate(-50%, -100%)'
                  : 'translateX(-50%)',
              }}
            >
              {activeExpiryTooltipText}
            </div>,
            document.body,
          )
        : null}
      <QuotaInspectionModal
        open={inspectionOpen}
        status={quotaInspectionStatus}
        loading={quotaInspectionLoading}
        starting={quotaInspectionStarting}
        error={quotaInspectionError}
        onClose={() => setInspectionOpen(false)}
        onStart={onStartInspection}
        onRefreshStatus={onRefreshInspectionStatus}
        onAfterInvalidAccountAction={onAfterInvalidAccountAction}
      />
    </>
  )
}


export function QuotaResetAction({
  authIndex,
  resetCredits,
  disabled,
  loading,
  fetchResetCredits = fetchUsageQuotaResetCredits,
  provider = 'codex',
  fetchClaudeGrants = fetchClaudeResetGrants,
  onConfirm,
}: {
  authIndex: string
  resetCredits: number
  disabled: boolean
  loading: boolean
  fetchResetCredits?: (authIndex: string, signal?: AbortSignal) => Promise<UsageQuotaResetCreditsResponse>
  provider?: 'claude' | 'codex'
  fetchClaudeGrants?: typeof fetchClaudeResetGrants
  onConfirm: (grantId?: string, organizationId?: string) => Promise<UsageQuotaResetResponse | void>
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [popoverPosition, setPopoverPosition] = useState<QuotaResetPopoverPosition | null>(null)
  const [resetCreditsDetails, setResetCreditsDetails] = useState<UsageQuotaResetCreditsResponse | null>(null)
  const [resetCreditsLoading, setResetCreditsLoading] = useState(false)
  const [resetCreditsFailed, setResetCreditsFailed] = useState(false)
  const [claudeDetails, setClaudeDetails] = useState<ClaudeResetGrantsResponse | null>(null)
  const [selectedClaudeGrantId, setSelectedClaudeGrantId] = useState('')
  const [claudeMessage, setClaudeMessage] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const submittingRef = useRef(false)
  const currentActionRef = useRef(`${provider}:${authIndex}`)
  currentActionRef.current = `${provider}:${authIndex}`
  const mountedRef = useRef(true)
  useEffect(() => { mountedRef.current = true; return () => { mountedRef.current = false } }, [])
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
    const viewportHeight = window.innerHeight
    const spaceBelow = Math.max(0, viewportHeight - rect.bottom - RESET_CREDITS_POPOVER_VIEWPORT_MARGIN - RESET_CREDITS_POPOVER_OFFSET)
    const spaceAbove = Math.max(0, rect.top - RESET_CREDITS_POPOVER_VIEWPORT_MARGIN - RESET_CREDITS_POPOVER_OFFSET)
    const maxHeight = provider === 'claude' ? 400 : RESET_CREDITS_POPOVER_MAX_HEIGHT
    const openBelow = spaceBelow >= maxHeight || spaceBelow >= spaceAbove
    const availableHeight = openBelow ? spaceBelow : spaceAbove
    const sharedPosition = {
      right: Math.max(RESET_CREDITS_POPOVER_VIEWPORT_MARGIN, Math.round(window.innerWidth - rect.right)),
      maxHeight: Math.max(0, Math.min(maxHeight, availableHeight)),
    }
    // popover 使用 fixed 并选择空间更充足的一侧，避免被卡片或视口底部裁切。
    setPopoverPosition(openBelow
      ? { ...sharedPosition, top: Math.round(rect.bottom + RESET_CREDITS_POPOVER_OFFSET) }
      : { ...sharedPosition, bottom: Math.round(viewportHeight - rect.top + RESET_CREDITS_POPOVER_OFFSET) })
  }, [provider])

  useEffect(() => {
    if (!open) {
      return
    }
    const refreshPopoverPosition = () => updatePopoverPosition()
    window.addEventListener('resize', refreshPopoverPosition)
    window.addEventListener('scroll', refreshPopoverPosition, true)
    return () => {
      window.removeEventListener('resize', refreshPopoverPosition)
      window.removeEventListener('scroll', refreshPopoverPosition, true)
    }
  }, [open, updatePopoverPosition])

  useEffect(() => {
    if (!open) {
      return
    }
    const controller = new AbortController()
    let active = true
    const timeoutID = window.setTimeout(() => {
      if (!active) {
        return
      }
      active = false
      controller.abort()
      setResetCreditsFailed(true)
      setResetCreditsLoading(false)
    }, provider === 'claude' ? 20_000 : RESET_CREDITS_LOOKUP_TIMEOUT_MS)
    // Claude 确认需要本次查询的 grant 与组织 UUID；Codex 保留明细失败后的缓存回退。
    const lookup = provider === 'claude' ? fetchClaudeGrants(authIndex, controller.signal) : fetchResetCredits(authIndex, controller.signal)
    void lookup
      .then((response) => {
        if (active) {
          if (provider === 'claude') {
            const details = response as ClaudeResetGrantsResponse
            setClaudeDetails(details)
            setSelectedClaudeGrantId(details.selectedGrantId ?? '')
          } else setResetCreditsDetails(response as UsageQuotaResetCreditsResponse)
        }
      })
      .catch(() => {
        if (active) {
          setResetCreditsFailed(true)
        }
      })
      .finally(() => {
        if (!active) {
          return
        }
        active = false
        window.clearTimeout(timeoutID)
        setResetCreditsLoading(false)
      })
    return () => {
      active = false
      window.clearTimeout(timeoutID)
      controller.abort()
    }
  }, [authIndex, fetchResetCredits, fetchClaudeGrants, open, provider])

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
    if (provider !== 'claude') { await onConfirm(); setOpen(false); return }
    if (submittingRef.current || !selectedClaudeGrantId || !claudeDetails?.organizationId) return
    const actionKey = `${provider}:${authIndex}`
    const isCurrent = () => mountedRef.current && currentActionRef.current === actionKey
    submittingRef.current = true
    setSubmitting(true)
    try {
      const result = await onConfirm(selectedClaudeGrantId, claudeDetails.organizationId)
      if (!isCurrent()) return
      if (result?.code === 'reset' || result?.code === 'already_used') setOpen(false)
      else { setClaudeMessage(result?.code ?? 'unknown'); setClaudeDetails(null) }
    } catch {
      if (isCurrent()) { setClaudeMessage('unknown'); setClaudeDetails(null) }
    } finally {
      if (isCurrent()) { submittingRef.current = false; setSubmitting(false) }
    }
  }

  const handleToggleOpen = () => {
    if (open) {
      setOpen(false)
      return
    }
    updatePopoverPosition()
    setResetCreditsDetails(null)
    setResetCreditsFailed(false)
    setResetCreditsLoading(true)
    setClaudeDetails(null)
    setClaudeMessage('')
    setSelectedClaudeGrantId('')
    setOpen(true)
  }

  const resetCreditsCountFromDetails = resetCreditsDetails && resetCreditsDetails.credits.length > 0
    ? resetCreditsDetails.credits.length
    : null
  // 与 CPAMC 保持一致：实时次数未知时先使用有效明细数量，仍未知则回退打开前的缓存次数。
  const displayResetCredits = provider === 'claude' ? claudeDetails?.status?.availableCount ?? resetCredits : resetCreditsDetails?.availableCount ?? resetCreditsCountFromDetails ?? resetCredits
  const resetCreditsExplicitlyUnavailable = resetCreditsDetails !== null
    && resetCreditsDetails.availableCount !== null
    && resetCreditsDetails.availableCount <= 0
  const resetCreditsDetailsIncomplete = resetCreditsDetails !== null && (
    (resetCreditsDetails.availableCount === null && resetCreditsDetails.credits.length === 0)
    || (resetCreditsDetails.availableCount !== null && resetCreditsDetails.availableCount > resetCreditsDetails.credits.length)
  )
  const confirmDisabled = loading || submitting || resetCreditsLoading || (provider === 'claude'
    ? resetCreditsFailed || !claudeDetails?.status?.grants.some((grant) => grant.id === selectedClaudeGrantId) || !claudeDetails.organizationId
    : resetCreditsExplicitlyUnavailable)
  const busy = loading || submitting
  const resetTitle = t(provider === 'claude' ? 'usage_stats.claude_reset_title' : 'usage_stats.credentials_quota_reset_title')

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
          className={`${styles.credentialQuotaResetPopover} ${provider === 'claude' ? styles.credentialQuotaResetClaudePopover : ''}`.trim()}
          role="dialog"
          aria-label={resetTitle}
          style={popoverPosition ?? undefined}
        >
          <p className={styles.credentialQuotaResetTitle}>{resetTitle}</p>
          <p className={styles.credentialQuotaResetMessage}>
            <span className={styles.credentialQuotaResetCountLine}>
              <span className={styles.credentialQuotaResetCount}>{displayResetCredits}</span>
              <span>{t('usage_stats.credentials_quota_reset_message_suffix')}</span>
            </span>
            <span>{t('usage_stats.credentials_quota_reset_message_prompt')}</span>
          </p>
          <div className={styles.credentialQuotaResetExpiry} aria-live="polite">
            <p className={styles.credentialQuotaResetExpiryTitle}>{t(provider === 'claude' ? 'usage_stats.claude_reset_grants_title' : 'usage_stats.credentials_quota_reset_expiry_title')}</p>
            {resetCreditsLoading && (
              <div className={styles.credentialQuotaResetExpiryStatus}>
                <LoadingSpinner size={12} />
                <span>{t(provider === 'claude' ? 'usage_stats.claude_reset_loading' : 'usage_stats.credentials_quota_reset_expiry_loading')}</span>
              </div>
            )}
            {!resetCreditsLoading && resetCreditsDetails && resetCreditsDetails.credits.length > 0 && (
              <div className={styles.credentialQuotaResetExpiryList}>
                {resetCreditsDetails.credits.map((credit, index) => (
                  <div key={credit.id || `${credit.expiresAt}-${index}`} className={styles.credentialQuotaResetExpiryRow}>
                    <span>{t('usage_stats.credentials_quota_reset_expiry_item', { index: index + 1 })}</span>
                    <strong>{formatResetCreditExpiry(credit.expiresAt)}</strong>
                  </div>
                ))}
              </div>
            )}
            {!resetCreditsLoading && provider === 'claude' && (
              <ClaudeResetDetails details={claudeDetails ? { ...claudeDetails, selectedGrantId: selectedClaudeGrantId } : null} failed={resetCreditsFailed} message={claudeMessage} disabled={busy || !!claudeMessage} groupName={`${tooltipId}-grants`} onSelect={(grantId) => { if (!submittingRef.current && !loading) setSelectedClaudeGrantId(grantId) }} />
            )}
            {!resetCreditsLoading && provider !== 'claude' && resetCreditsExplicitlyUnavailable && (
              <p className={styles.credentialQuotaResetExpiryStatus}>{t('usage_stats.credentials_quota_reset_expiry_empty')}</p>
            )}
            {!resetCreditsLoading && provider !== 'claude' && (resetCreditsFailed || resetCreditsDetailsIncomplete) && (
              <p className={`${styles.credentialQuotaResetExpiryStatus} ${styles.credentialQuotaResetExpiryWarning}`.trim()}>
                {t('usage_stats.credentials_quota_reset_expiry_failed')}
              </p>
            )}
          </div>
          <div className={styles.credentialQuotaResetActions}>
            <button type="button" className={styles.credentialQuotaResetCancelButton} onClick={() => setOpen(false)} disabled={busy}>
              {t('common.cancel')}
            </button>
            <button type="button" className={styles.credentialQuotaResetConfirmButton} onClick={() => void handleConfirm()} disabled={confirmDisabled} aria-busy={busy}>
              {busy ? <LoadingSpinner size={12} /> : t('usage_stats.credentials_quota_reset_confirm')}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

function ClaudeResetDetails({ details, failed, message, disabled, groupName, onSelect }: { details: ClaudeResetGrantsResponse | null; failed: boolean; message: string; disabled: boolean; groupName: string; onSelect: (grantId: string) => void }) {
  const { t } = useTranslation()
  const status = details?.status
  const grant = status?.grants.find((item) => item.id === details?.selectedGrantId)
  // 上游范围直接展示原值，保留顺序与重复值。
  const scope = (clears: string[]) => clears.length ? clears.join(' / ') : t('usage_stats.claude_reset_scope_unknown')
  const messages = new Set(['unknown', 'not_limited', 'cooldown', 'ineligible', 'unavailable', 'rate_limited', 'auth_error', 'status_unavailable'])
  let code = message
  if (!code && (failed || !details?.status || !details.organizationId)) code = 'status_unavailable'
  if (!code && !details?.selectedGrantId) code = 'unavailable'
  const empty = !message && !failed && !!status && !!details?.organizationId && status.grants.length === 0
  return (
    <>
      {status && status.grants.length > 0 && <div role="radiogroup" aria-label={t('usage_stats.claude_reset_grants_title')} className={styles.credentialQuotaResetExpiryList}>
      {status.grants.map((item, index) => {
        const selected = item.id === details?.selectedGrantId
        return (
          <label key={item.id} data-claude-grant={item.id} className={`${styles.credentialQuotaResetExpiryRow} ${styles.credentialQuotaResetGrant} ${selected ? styles.credentialQuotaResetGrantSelected : ''}`.trim()}>
            <div className={styles.credentialQuotaResetGrantLine}>
              <span className={styles.credentialQuotaResetGrantChoice}><input type="radio" name={groupName} value={item.id} checked={selected} disabled={disabled} onChange={() => onSelect(item.id)} aria-label={item.label || t('usage_stats.claude_reset_grant', { index: index + 1 })} />{item.label || t('usage_stats.claude_reset_grant', { index: index + 1 })}</span>
              <strong>{t('usage_stats.claude_reset_grant_count', { left: item.resetsLeft })}</strong>
            </div>
            <p className={styles.credentialQuotaResetExpiryStatus}>{scope(item.clears)}</p>
            <div className={styles.credentialQuotaResetGrantLine}>
              <span>{t('usage_stats.claude_reset_expires')}</span>
              <strong>{item.endsAt ? formatResetCreditExpiry(item.endsAt) : t('usage_stats.claude_reset_no_expiry')}</strong>
            </div>
          </label>
        )
      })}
      </div>}
      {grant && <p className={styles.credentialQuotaResetExpiryStatus}>{scope(grant.clears)}</p>}
      {code && <p role="status" className={`${styles.credentialQuotaResetExpiryStatus} ${empty ? '' : styles.credentialQuotaResetExpiryWarning}`.trim()}>{t(`usage_stats.claude_reset_${messages.has(code) ? code : 'unavailable'}`)}</p>}
    </>
  )
}

const RESET_CREDIT_TIME_FORMATTER = new Intl.DateTimeFormat('en-CA', {
  timeZone: 'Asia/Shanghai',
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false,
})

export function formatResetCreditExpiry(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return RESET_CREDIT_TIME_FORMATTER.format(date).replace(',', '')
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

export function isInspectionStartDisabled({ starting, total, running }: { starting: boolean; total: number; running: boolean }): boolean {
  // running 只代表显式巡检轮次；定时刷新和手动刷新共用队列但不禁用手动巡检入口。
  return starting || running || total <= 0
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

type AutoRefreshSettingsFormSetters = {
  setEnabled: (enabled: boolean) => void
  setUnit: (unit: QuotaAutoRefreshScheduleUnit) => void
  setValue: (value: string) => void
}

type AutoRefreshSettingsBuildResult =
  | { settings: QuotaAutoRefreshSettings; errorKey?: never }
  | { settings?: never; errorKey: string }

function applyQuotaAutoRefreshSettingsForm(settings: QuotaAutoRefreshSettings, setters: AutoRefreshSettingsFormSetters) {
  setters.setEnabled(settings.enabled)
  if (settings.schedule) {
    setters.setUnit(settings.schedule.unit)
    setters.setValue(String(settings.schedule.value))
    return
  }
  setters.setUnit('minute')
  setters.setValue('')
}

export function buildQuotaAutoRefreshSettings({
  enabled,
  unit,
  value,
}: {
  enabled: boolean
  unit: QuotaAutoRefreshScheduleUnit
  value: string
}): AutoRefreshSettingsBuildResult {
  if (!enabled) {
    return { settings: { enabled: false, schedule: null } }
  }
  const trimmedValue = value.trim()
  if (trimmedValue === '') {
    return { errorKey: 'usage_stats.credentials_auto_refresh_validation_required' }
  }
  const numericValue = Number(trimmedValue)
  const max = AUTO_REFRESH_SCHEDULE_LIMITS[unit]
  if (!Number.isInteger(numericValue) || numericValue < 1 || numericValue > max) {
    return { errorKey: 'usage_stats.credentials_auto_refresh_validation_range' }
  }
  return {
    settings: {
      enabled: true,
      schedule: {
        unit,
        value: numericValue,
      },
    },
  }
}

export function isAutoRefreshSettingsSaveDisabled({
  loading,
  saving,
  loaded,
}: {
  loading: boolean
  saving: boolean
  loaded: boolean
}): boolean {
  return loading || saving || !loaded
}

export function isAutoRefreshSettingsControlDisabled({
  loading,
  saving,
  loaded,
}: {
  loading: boolean
  saving: boolean
  loaded: boolean
}): boolean {
  return loading || saving || !loaded
}

export function resolveQuotaAutoRefreshSettingsLoadFailure(nextError: unknown, fallbackMessage: string): { settings: QuotaAutoRefreshSettings; error: string; loaded: true } {
  return {
    settings: { enabled: false, schedule: null },
    error: nextError instanceof Error && nextError.message ? nextError.message : fallbackMessage,
    loaded: true,
  }
}

export function isQuotaInspectionCloseDisabled({
	invalidAccountActionOpen,
	invalidAccountSubmitting,
}: {
	invalidAccountActionOpen: boolean
	invalidAccountSubmitting: boolean
}): boolean {
	return invalidAccountActionOpen || invalidAccountSubmitting
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

export function QuotaInspectionModal({
  open,
  status,
  loading,
  starting,
  error,
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
  const [settingsModalOpen, setSettingsModalOpen] = useState(false)
  const [settingsLoading, setSettingsLoading] = useState(false)
  const [settingsSaving, setSettingsSaving] = useState(false)
  const [settingsLoaded, setSettingsLoaded] = useState(false)
  const [settingsError, setSettingsError] = useState('')
  const [settingsEnabled, setSettingsEnabled] = useState(false)
  const [scheduleUnit, setScheduleUnit] = useState<QuotaAutoRefreshScheduleUnit>('minute')
  const [scheduleValue, setScheduleValue] = useState('')
  // total 由后端 Auth Files 身份统计提供，不用页面分页总数替代。
  const total = status?.total ?? 0
  // cached 是已经能解析出最近巡检结果的账号数。
  const cached = status?.cached ?? 0
  // progressTotal 排除 unknown，使进度条只描述实际刷新任务完成度。
  const progressTotal = inspectionProgressTotal(status)
  const progress = formatInspectionProgressPercent(status)
  // startDisabled 只依赖显式巡检状态和可巡检总数，不被定时刷新或普通行刷新状态牵连。
  const startDisabled = isInspectionStartDisabled({
    starting,
    total,
    running: status?.running ?? false,
  })
  const startLabel = (starting || status?.running)
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
  const inspectionCloseDisabled = isQuotaInspectionCloseDisabled({
    invalidAccountActionOpen: invalidAccountAction !== null,
    invalidAccountSubmitting,
  })
  const scheduleMax = AUTO_REFRESH_SCHEDULE_LIMITS[scheduleUnit]

  useEffect(() => {
    if (!settingsModalOpen) {
      setSettingsError('')
      setSettingsLoaded(false)
      return
    }
    const controller = new AbortController()
    setSettingsLoading(true)
    setSettingsLoaded(false)
    setSettingsError('')
    void fetchQuotaAutoRefreshSettings(controller.signal)
      .then((settings) => {
        if (controller.signal.aborted) return
        applyQuotaAutoRefreshSettingsForm(settings, {
          setEnabled: setSettingsEnabled,
          setUnit: setScheduleUnit,
          setValue: setScheduleValue,
        })
        setSettingsLoaded(true)
      })
      .catch((nextError: unknown) => {
        if (controller.signal.aborted) return
        const fallback = resolveQuotaAutoRefreshSettingsLoadFailure(nextError, t('usage_stats.credentials_auto_refresh_load_failed'))
        applyQuotaAutoRefreshSettingsForm(fallback.settings, {
          setEnabled: setSettingsEnabled,
          setUnit: setScheduleUnit,
          setValue: setScheduleValue,
        })
        setSettingsLoaded(fallback.loaded)
        setSettingsError(fallback.error)
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setSettingsLoading(false)
        }
      })
    return () => controller.abort()
  }, [settingsModalOpen, t])

  const handleSaveAutoRefreshSettings = async () => {
    if (isAutoRefreshSettingsSaveDisabled({ loading: settingsLoading, saving: settingsSaving, loaded: settingsLoaded })) {
      return
    }
    const nextSettings = buildQuotaAutoRefreshSettings({
      enabled: settingsEnabled,
      unit: scheduleUnit,
      value: scheduleValue,
    })
    const errorKey = 'errorKey' in nextSettings ? nextSettings.errorKey : ''
    if (errorKey) {
      setSettingsError(t(errorKey, { max: String(scheduleMax) }))
      return
    }
    const settingsToSave = nextSettings.settings
    if (!settingsToSave) {
      return
    }
    setSettingsSaving(true)
    setSettingsError('')
    try {
      const saved = await updateQuotaAutoRefreshSettings(settingsToSave)
      applyQuotaAutoRefreshSettingsForm(saved, {
        setEnabled: setSettingsEnabled,
        setUnit: setScheduleUnit,
        setValue: setScheduleValue,
      })
    } catch (nextError) {
      setSettingsError(nextError instanceof Error ? nextError.message : t('usage_stats.credentials_auto_refresh_save_failed'))
    } finally {
      setSettingsSaving(false)
    }
  }

  return (
    <Modal
      open={open}
      title={t('usage_stats.credentials_inspection_title')}
      onClose={(inspectionCloseDisabled || settingsModalOpen) ? () => undefined : () => {
        setSettingsModalOpen(false)
        onClose()
      }}
      width={820}
      className={styles.credentialInspectionModal}
      closeDisabled={inspectionCloseDisabled || settingsModalOpen}
    >
      <div className={styles.credentialInspectionPanel}>
        <div className={styles.credentialInspectionTitleBar}>
          <button
            type="button"
            className={`${styles.credentialInspectionSettingsButton} ${settingsModalOpen ? styles.credentialInspectionSettingsButtonActive : ''}`.trim()}
            onClick={() => setSettingsModalOpen(true)}
            aria-label={t('usage_stats.credentials_auto_refresh_settings')}
            title={t('usage_stats.credentials_auto_refresh_settings')}
            aria-haspopup="dialog"
          >
            <IconSettings size={15} />
          </button>
        </div>
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
                  <Select
                    value={String(resultPageData.pageSize)}
                    options={INSPECTION_RESULT_PAGE_SIZE_OPTIONS.map((option) => ({ value: String(option), label: String(option) }))}
                    onChange={(next) => handleResultPageSizeChange(Number(next))}
                    ariaLabel={`${t('usage_stats.rows_per_page')}: ${resultPageData.pageSize}`}
                    className={styles.credentialInspectionPageSizeSelect}
                    fullWidth={false}
                  />
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
      <QuotaAutoRefreshSettingsModal
        open={settingsModalOpen}
        enabled={settingsEnabled}
        unit={scheduleUnit}
        value={scheduleValue}
        loading={settingsLoading}
        saving={settingsSaving}
        loaded={settingsLoaded}
        error={settingsError}
        onClose={() => {
          if (!settingsSaving) {
            setSettingsModalOpen(false)
          }
        }}
        onEnabledChange={setSettingsEnabled}
        onUnitChange={(unit) => {
          setScheduleUnit(unit)
          setScheduleValue('')
        }}
        onValueChange={setScheduleValue}
        onSave={handleSaveAutoRefreshSettings}
      />
    </Modal>
  )
}

export function QuotaAutoRefreshSettingsModal({
  open,
  enabled,
  unit,
  value,
  loading,
  saving,
  loaded,
  error,
  onClose,
  onEnabledChange,
  onUnitChange,
  onValueChange,
  onSave,
}: {
  open: boolean
  enabled: boolean
  unit: QuotaAutoRefreshScheduleUnit
  value: string
  loading: boolean
  saving: boolean
  loaded: boolean
  error: string
  onClose: () => void
  onEnabledChange: (enabled: boolean) => void
  onUnitChange: (unit: QuotaAutoRefreshScheduleUnit) => void
  onValueChange: (value: string) => void
  onSave: () => Promise<void>
}) {
  const { t } = useTranslation()
  const scheduleMax = AUTO_REFRESH_SCHEDULE_LIMITS[unit]
  const controlsDisabled = isAutoRefreshSettingsControlDisabled({ loading, saving, loaded })
  const scheduleControlsDisabled = controlsDisabled || !enabled
  const saveDisabled = isAutoRefreshSettingsSaveDisabled({ loading, saving, loaded })
  const scheduleAreaClassName = `${styles.credentialAutoRefreshScheduleArea} ${enabled ? styles.credentialAutoRefreshScheduleAreaActive : ''}`.trim()

  return (
    <Modal
      open={open}
      title={t('usage_stats.credentials_auto_refresh_settings')}
      onClose={saving ? () => undefined : onClose}
      width={620}
      className={styles.credentialAutoRefreshSettingsModal}
      closeDisabled={saving}
    >
      <div className={styles.credentialAutoRefreshSettingsPanel}>
        <div className={styles.credentialAutoRefreshSettingsRow}>
          <label className={`${styles.credentialActiveOnlySwitch} ${controlsDisabled ? styles.credentialActiveOnlySwitchDisabled : ''}`.trim()}>
            <span className={styles.credentialActiveOnlyLabel}>{t('usage_stats.credentials_auto_refresh_enabled')}</span>
            <input type="checkbox" checked={enabled} onChange={(event) => onEnabledChange(event.target.checked)} disabled={controlsDisabled} />
            <span className={styles.credentialActiveOnlyTrack} aria-hidden="true">
              <span className={styles.credentialActiveOnlyThumb} />
            </span>
          </label>
          <button
            type="button"
            className={styles.credentialAutoRefreshSaveButton}
            onClick={() => void onSave()}
            disabled={saveDisabled}
            aria-busy={saving}
            aria-label={saving ? t('usage_stats.credentials_auto_refresh_saving') : t('usage_stats.credentials_auto_refresh_save')}
            title={saving ? t('usage_stats.credentials_auto_refresh_saving') : t('usage_stats.credentials_auto_refresh_save')}
          >
            {saving ? <LoadingSpinner size={12} /> : t('usage_stats.credentials_auto_refresh_save')}
          </button>
        </div>
        <div className={scheduleAreaClassName} aria-hidden={!enabled}>
          <div className={styles.credentialAutoRefreshScheduleGrid}>
            <div className={styles.credentialAutoRefreshUnitSwitcher} role="group" aria-label={t('usage_stats.credentials_auto_refresh_unit')}>
              {AUTO_REFRESH_SCHEDULE_UNITS.map((nextUnit) => (
                <button
                  key={nextUnit}
                  type="button"
                  className={nextUnit === unit ? styles.credentialAutoRefreshUnitButtonActive : undefined}
                  onClick={() => onUnitChange(nextUnit)}
                  aria-pressed={nextUnit === unit}
                  disabled={scheduleControlsDisabled}
                >
                  {t(`usage_stats.credentials_auto_refresh_unit_${nextUnit}`)}
                </button>
              ))}
            </div>
            {unit === 'week' ? (
              <label className={styles.credentialAutoRefreshIntervalField}>
                <span className={styles.credentialAutoRefreshIntervalLabel}>{t('usage_stats.credentials_auto_refresh_weekday')}</span>
                <Select
                  value={value}
                  options={[
                    { value: '', label: t('usage_stats.credentials_auto_refresh_select') },
                    ...AUTO_REFRESH_WEEKDAYS.map((weekday) => ({ value: String(weekday), label: t(`usage_stats.credentials_auto_refresh_weekday_${weekday}`) })),
                  ]}
                  onChange={onValueChange}
                  disabled={scheduleControlsDisabled}
                  ariaLabel={`${t('usage_stats.credentials_auto_refresh_weekday')}: ${t(value ? `usage_stats.credentials_auto_refresh_weekday_${value}` : 'usage_stats.credentials_auto_refresh_select')}`}
                  className={styles.credentialAutoRefreshWeekdaySelect}
                />
              </label>
            ) : (
              <label className={styles.credentialAutoRefreshIntervalField}>
                <span className={styles.credentialAutoRefreshIntervalLabel}>{t('usage_stats.credentials_auto_refresh_value')}</span>
                <input
                  type="number"
                  min={1}
                  max={scheduleMax}
                  step={1}
                  value={value}
                  onChange={(event) => onValueChange(event.target.value)}
                  disabled={scheduleControlsDisabled}
                />
                <span className={styles.credentialAutoRefreshUnitSuffix}>{t(`usage_stats.credentials_auto_refresh_unit_${unit}`)}</span>
              </label>
            )}
          </div>
          <div className={styles.credentialAutoRefreshScheduleTip}>
            {t(`usage_stats.credentials_auto_refresh_tip_${unit}`)}
          </div>
        </div>
        {loading && <div className={styles.credentialEmptyState}>{t('common.loading')}</div>}
        {error && <div className={styles.credentialInlineError} role="alert">{error}</div>}
      </div>
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
          <Button type="button" variant="secondary" appearance="action" onClick={onCancel} disabled={submitting}>
            {t('common.cancel')}
          </Button>
          <Button
            type="button"
            variant={action === 'delete' ? 'danger' : 'primary'}
            appearance="action"
            onClick={onConfirm}
            disabled={submitting || selectedFileNames.length === 0}
            loading={submitting}
            aria-busy={submitting}
          >
            {t('usage_stats.credentials_inspection_invalid_accounts_confirm', { action: actionLabel })}
          </Button>
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
        <ProviderBrandIcon providerType={result.type} size={20} />
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

export function AuthFileQuotaPanel({ row, quotaUsageMode, timeZone }: { row: AuthFileCredentialRow; quotaUsageMode: QuotaUsageMode; timeZone?: string }) {
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
        {/* 只有 canonical Antigravity 组提升为共享标题；其它 provider 继续沿用原始扁平 QuotaBar。 */}
        {authFileQuotaPanelItems(row.displayQuotas).map((item) => item.kind === 'group'
          ? <AntigravityQuotaGroup key={item.renderKey} group={item} quotaUsageMode={quotaUsageMode} timeZone={timeZone} />
          : <QuotaBar key={item.quota.key} quota={item.quota} quotaUsageMode={quotaUsageMode} timeZone={timeZone} tooltipAlignRight={item.tooltipAlignRight} />)}
      </div>
    </div>
  )
}

type AuthFileQuotaPanelItem =
  | { kind: 'quota'; quota: DisplayQuota; tooltipAlignRight: boolean }
  | AntigravityQuotaGroupItem

type AntigravityQuotaGroupItem = {
  kind: 'group'
  renderKey: string
  groupKey: string
  groupLabel: string
  groupDescription?: string
  quotas: DisplayQuota[]
}

function authFileQuotaPanelItems(quotas: DisplayQuota[]): AuthFileQuotaPanelItem[] {
  const items: AuthFileQuotaPanelItem[] = []
  let flatColumn = 0
  for (const quota of quotas) {
    const groupKey = quota.groupKey?.trim() ?? ''
    const groupLabel = quota.groupLabel?.trim() ?? ''
    if (quota.scope !== 'quota_group' || !ANTIGRAVITY_QUOTA_GROUP_KEYS.has(groupKey) || !groupLabel) {
      items.push({ kind: 'quota', quota, tooltipAlignRight: flatColumn === 1 })
      flatColumn = (flatColumn + 1) % 2
      continue
    }
    // 分组块横跨两列；只合并相邻同组行，并让后续扁平行重新从左列开始。
    flatColumn = 0
    const previous = items.at(-1)
    if (previous?.kind === 'group' && previous.groupKey === groupKey) {
      previous.quotas.push(quota)
      if (!previous.groupDescription && quota.groupDescription?.trim()) {
        previous.groupDescription = quota.groupDescription
      }
      continue
    }
    const group: AntigravityQuotaGroupItem = {
      kind: 'group',
      renderKey: `${groupKey}:${quota.key}`,
      groupKey,
      groupLabel,
      groupDescription: quota.groupDescription,
      quotas: [quota],
    }
    items.push(group)
  }
  return items
}

function AntigravityQuotaGroup({ group, quotaUsageMode, timeZone }: { group: AntigravityQuotaGroupItem; quotaUsageMode: QuotaUsageMode; timeZone?: string }) {
  return (
    <div className={styles.credentialQuotaGroupBlock} data-quota-group={group.groupKey}>
      <div className={styles.credentialQuotaGroupHeader}>
        <QuotaGroupLabel label={group.groupLabel} description={group.groupDescription} />
      </div>
      <div className={styles.credentialQuotaGroupBars}>
        {group.quotas.map((quota) => (
          <QuotaBar key={quota.key} quota={quota} quotaUsageMode={quotaUsageMode} timeZone={timeZone} showGroupMetadata={false} />
        ))}
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

export function formatQuotaResetLabel(resetAt: string, timeZone?: string): string {
  const resetTime = new Date(resetAt)
  const resetMs = resetTime.getTime()
  if (!Number.isFinite(resetMs)) {
    return ''
  }
  const normalizedTimeZone = timeZone?.trim()
  if (!normalizedTimeZone || normalizedTimeZone === 'Local') {
    return ''
  }
  try {
    // resetAt 保留 CPA 给出的绝对时刻，只用 Keeper 项目时区生成墙上时间，避免受浏览器时区影响。
    const parts = new Intl.DateTimeFormat('en-CA', {
      timeZone: normalizedTimeZone,
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hourCycle: 'h23',
    }).formatToParts(resetTime)
    const values = Object.fromEntries(parts.map((part) => [part.type, part.value]))
    return `${values.month}/${values.day} ${values.hour}:${values.minute}`
  } catch {
    // Go 的 Local 或浏览器不认识的 IANA 名称无法可靠映射到 Keeper 墙上时间，隐藏标签而不是猜浏览器时区。
    return ''
  }
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

function QuotaBar({ quota, quotaUsageMode, timeZone, showGroupMetadata = true, tooltipAlignRight = false }: { quota: DisplayQuota; quotaUsageMode: QuotaUsageMode; timeZone?: string; showGroupMetadata?: boolean; tooltipAlignRight?: boolean }) {
  const { t } = useTranslation()
  // 条宽使用剩余额度百分比，颜色跟随剩余风险状态从绿到黄到红。
  const percent = quota.barPercent ?? 0
  const width = `${Math.max(0, Math.min(100, percent))}%`
  const percentLabel = quota.barPercent === null ? '' : `${Math.round(quota.barPercent)}%`
  const resetLabel = quota.resetText ? formatQuotaResetLabel(quota.resetText, timeZone) : ''
  const resetDuration = quota.resetText ? formatQuotaResetDuration(quota.resetText) : ''
  const billingUsage = quota.billingUsage
  const windowUsage = billingUsage ? undefined : quotaWindowUsageForMode(quota, quotaUsageMode)

  return (
    <div className={`${styles.credentialQuotaBarBlock} ${tooltipAlignRight ? styles.credentialQuotaBarTooltipRight : ''}`.trim()}>
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
        {showGroupMetadata && quota.scope === 'quota_group' && quota.groupLabel && (
          <QuotaGroupLabel label={quota.groupLabel} description={quota.groupDescription} />
        )}
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
        {resetLabel && <span className={styles.credentialQuotaResetTime}>{resetLabel}</span>}
      </div>
    </div>
  )
}

function QuotaGroupLabel({ label, description }: { label: string; description?: string }) {
  const tooltipId = useId()
  const hasDescription = Boolean(description?.trim())
  return (
    <span
      className={styles.credentialQuotaGroupTooltipTarget}
      tabIndex={hasDescription ? 0 : undefined}
      aria-describedby={hasDescription ? tooltipId : undefined}
    >
      <span className={styles.credentialQuotaGroupLabel}>{label}</span>
      {hasDescription && (
        <span id={tooltipId} className={styles.credentialQuotaGroupTooltip} role="tooltip">
          {description}
        </span>
      )}
    </span>
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
