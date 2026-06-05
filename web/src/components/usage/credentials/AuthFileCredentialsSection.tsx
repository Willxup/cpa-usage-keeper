import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { Modal } from '@/components/ui/Modal'
import { IconRefreshCw, IconSearch } from '@/components/ui/icons'
import quotaCostIcon from '@/assets/icons/quota-cost.svg'
import quotaTokenIcon from '@/assets/icons/quota-token.svg'
import styles from './CredentialSections.module.scss'
import type { AuthFileCredentialRow, DisplayQuota, PlanTypeTone } from './credentialViewModels'
import type { UsageIdentityPageSort } from '@/lib/api'
import type { UsageQuotaInspectionResult, UsageQuotaInspectionStatusResponse } from '@/lib/types'
import { CredentialProviderFilterIcon } from './CredentialProviderFilterBar'
import { CredentialBadge, CredentialPriorityBadge, CredentialRowShell, CredentialSectionShell, CredentialsPagination, MetricPill, RequestMetric, TonePercent, cacheRateTone, capitalize, credentialToneClassName, formatCredentialNumber, successRateTone } from './CredentialSectionShell'

type Translate = (key: string, options?: Record<string, string>) => string
type InspectionIndicatorTone = 'idle' | 'running' | 'completed'
type QuotaUsageMode = 'current' | 'estimated'
type QuotaErrorDisplay = {
  code?: string
  message: string
  title: string
}
type QuotaErrorDetails = {
  code?: string
  message?: string
}

const QUOTA_ERROR_MESSAGE_MAX_LENGTH = 96
const QUOTA_ERROR_PARSE_MAX_DEPTH = 10

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
  onRefreshInspectionStatus: () => Promise<void>
  onStartInspection: () => Promise<void>
}

export function AuthFileCredentialsSection({ rows, total, page, totalPages, pageSize, activeOnly, sort, loading, quotaRefreshing, quotaRefreshError, quotaAutoRefreshEnabled, quotaInspectionStatus, quotaInspectionLoading, quotaInspectionStarting, quotaInspectionError, onPageChange, onPageSizeChange, onActiveOnlyChange, onSortChange, onRefreshQuota, onRefreshQuotaForAuthIndex, onRefreshInspectionStatus, onStartInspection }: AuthFileCredentialsSectionProps) {
  const { t } = useTranslation()
  const [inspectionOpen, setInspectionOpen] = useState(false)
  const [quotaUsageMode, setQuotaUsageMode] = useState<QuotaUsageMode>('current')
  const canRefresh = rows.some((row) => !isRowRefreshing(row) && !row.identity.is_deleted) && !quotaRefreshing
  const inspectionTone = inspectionIndicatorTone(quotaInspectionStatus)
  const openInspection = () => {
    setInspectionOpen(true)
    void onRefreshInspectionStatus()
  }

  return (
    <>
      <CredentialSectionShell
        title={t('usage_stats.credentials_auth_files_title')}
        subtitle={t('usage_stats.credentials_auth_files_subtitle')}
        countLabel={t('usage_stats.credentials_count', { count: total })}
        titleExtra={(
          <label className={styles.credentialActiveOnlySwitch}>
            <input type="checkbox" checked={activeOnly} onChange={(event) => onActiveOnlyChange(event.target.checked)} />
            <span>{t('usage_stats.credentials_auth_files_active_only')}</span>
          </label>
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
      {rows.map((row) => {
        const rowRefreshing = isRowRefreshing(row)
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
                {row.totalRequests > 0 && <MetricPill label={t('usage_stats.total_requests')} value={<RequestMetric total={row.totalRequests} success={row.successCount} failure={row.failureCount} />} />}
                {row.successRate !== null && <MetricPill label={t('usage_stats.success_rate')} value={<TonePercent value={row.successRate} tone={successRateTone(row.successRate)} />} />}
                {row.totalTokens > 0 && <MetricPill label={t('usage_stats.total_tokens')} value={formatCredentialNumber(row.totalTokens)} />}
                {row.cacheRate !== null && <MetricPill label={t('usage_stats.cache_rate')} value={<TonePercent value={row.cacheRate} tone={cacheRateTone(row.cacheRate)} />} />}
              </>
            )}
            rowClassName={styles.authFileCredentialRow}
            side={(
              <div className={styles.credentialQuotaSideWithAction}>
                <AuthFileQuotaPanel row={row} quotaUsageMode={quotaUsageMode} />
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
            )}
          />
        )
      })}
      <CredentialsPagination
        leadingControls={<QuotaUsageModeSwitch label={t('usage_stats.credentials_quota_usage_mode_label')} mode={quotaUsageMode} onChange={setQuotaUsageMode} />}
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
      />
    </>
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

function QuotaInspectionModal({
  open,
  status,
  loading,
  starting,
  error,
  quotaAutoRefreshEnabled,
  onClose,
  onStart,
}: {
  open: boolean
  status: UsageQuotaInspectionStatusResponse | null
  loading: boolean
  starting: boolean
  error: string
  quotaAutoRefreshEnabled: boolean
  onClose: () => void
  onStart: () => Promise<void>
}) {
  const { t } = useTranslation()
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

  return (
    <Modal open={open} title={t('usage_stats.credentials_inspection_title')} onClose={onClose} width={820} className={styles.credentialInspectionModal}>
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
          <InspectionStatCard tone="normal" label={t('usage_stats.credentials_inspection_normal')} value={status?.normal ?? 0} total={total} />
          <InspectionStatCard tone="limitReached" label={t('usage_stats.credentials_inspection_limit_reached')} value={status?.limit_reached ?? 0} total={total} />
          <InspectionStatCard tone="unauthorized" label={t('usage_stats.credentials_inspection_401')} value={status?.unauthorized_401 ?? 0} total={total} />
          <InspectionStatCard tone="payment" label={t('usage_stats.credentials_inspection_402')} value={status?.payment_required_402 ?? 0} total={total} />
          <InspectionStatCard tone="failed" label={t('usage_stats.credentials_inspection_other_failed')} value={status?.other_failed ?? 0} total={total} />
          <InspectionStatCard tone="unknown" label={t('usage_stats.credentials_inspection_unknown')} value={status?.unknown ?? 0} total={total} />
        </div>

        <div className={styles.credentialInspectionResultsBlock}>
          <div className={styles.credentialInspectionResultsTitle}>{t('usage_stats.credentials_inspection_recent_results')}</div>
          {results.length === 0 ? (
            <div className={styles.credentialEmptyState}>{t('usage_stats.credentials_inspection_empty_results')}</div>
          ) : (
            <div className={styles.credentialInspectionResultsTable}>
              {results.slice(0, 8).map((result) => <InspectionResultRow key={result.auth_index} result={result} />)}
            </div>
          )}
        </div>
      </div>
    </Modal>
  )
}

function InspectionStatCard({ tone, label, value, total }: { tone: 'normal' | 'limitReached' | 'unauthorized' | 'payment' | 'failed' | 'unknown'; label: string; value: number; total: number }) {
  const percent = total > 0 ? Math.round((value / total) * 100) : 0
  return (
    <div className={`${styles.credentialInspectionStatCard} ${styles[`credentialInspectionStatCard${capitalize(tone)}`]}`.trim()}>
      <span>{label}</span>
      <strong>{value}</strong>
      <small>{percent}%</small>
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

export function AuthFileQuotaPanel({ row, quotaUsageMode }: { row: AuthFileCredentialRow; quotaUsageMode: QuotaUsageMode }) {
  const { t } = useTranslation()

  // 限额区域按加载、错误、刷新中、无缓存、可展示数据的顺序降级。
  if (row.quotaLoading) {
    return <div className={styles.credentialQuotaState}>{t('usage_stats.credentials_quota_loading')}</div>
  }
  if (row.quotaError) {
    const errorDisplay = formatQuotaErrorDisplay(row.quotaError)
    return (
      <div className={styles.credentialQuotaErrorSummary} title={errorDisplay.title}>
        {errorDisplay.code && <span className={styles.credentialQuotaErrorCode}>{errorDisplay.code}</span>}
        <span className={styles.credentialQuotaErrorMessage}>{errorDisplay.message}</span>
      </div>
    )
  }
  if (row.refreshStatus === 'queued' || row.refreshStatus === 'running') {
    return <div className={styles.credentialQuotaRefreshStatus}>{t(`usage_stats.credentials_refresh_status_${row.refreshStatus}`)}</div>
  }
  if (row.displayQuotas.length === 0) {
    return <div className={styles.credentialQuotaState}>{t('usage_stats.credentials_quota_unavailable')}</div>
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
