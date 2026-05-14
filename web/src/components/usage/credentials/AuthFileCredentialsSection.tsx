import { useTranslation } from 'react-i18next'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { IconRefreshCw } from '@/components/ui/icons'
import styles from './CredentialSections.module.scss'
import type { AuthFileCredentialRow, DisplayQuota, PlanTypeTone } from './credentialViewModels'
import { CredentialBadge, CredentialRowShell, CredentialSectionShell, CredentialsPagination, MetricPill, RequestMetric, TonePercent, cacheRateTone, capitalize, credentialToneClassName, formatCredentialNumber, successRateTone } from './CredentialSectionShell'

interface AuthFileCredentialsSectionProps {
  rows: AuthFileCredentialRow[]
  total: number
  page: number
  totalPages: number
  pageSize: number
  loading: boolean
  quotaRefreshing: boolean
  quotaRefreshError: string
  onPageChange: (page: number) => void
  onPageSizeChange: (pageSize: number) => void
  onRefreshQuota: () => Promise<void>
  onRefreshQuotaForAuthIndex: (authIndex: string) => Promise<void>
}

export function AuthFileCredentialsSection({ rows, total, page, totalPages, pageSize, loading, quotaRefreshing, quotaRefreshError, onPageChange, onPageSizeChange, onRefreshQuota, onRefreshQuotaForAuthIndex }: AuthFileCredentialsSectionProps) {
  const { t } = useTranslation()
  const canRefresh = rows.some((row) => !isRowRefreshing(row) && !row.identity.is_deleted) && !quotaRefreshing

  return (
    <CredentialSectionShell
      eyebrow={t('usage_stats.credentials_auth_files_eyebrow')}
      title={t('usage_stats.credentials_auth_files_title')}
      subtitle={t('usage_stats.credentials_auth_files_subtitle')}
      countLabel={t('usage_stats.credentials_count', { count: total })}
      actions={(
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
            side={(
              <div className={styles.credentialQuotaSideWithAction}>
                <AuthFileQuotaPanel row={row} />
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
        page={page}
        totalPages={totalPages}
        pageSize={pageSize}
        previousLabel={t('usage_stats.previous_page')}
        nextLabel={t('usage_stats.next_page')}
        rowsPerPageLabel={t('usage_stats.rows_per_page')}
        onPageChange={onPageChange}
        onPageSizeChange={onPageSizeChange}
      />
    </CredentialSectionShell>
  )
}

function isRowRefreshing(row: AuthFileCredentialRow): boolean {
  return row.refreshStatus === 'queued' || row.refreshStatus === 'running'
}

function CredentialPlanBadge({ children, tone = 'neutral' }: { children: string; tone?: PlanTypeTone }) {
  return <span className={`${styles.credentialPlanBadge} ${styles[`credentialPlanBadge${capitalize(tone)}`]}`.trim()}>{children}</span>
}

function AuthFileQuotaPanel({ row }: { row: AuthFileCredentialRow }) {
  const { t } = useTranslation()

  // 限额区域按加载、错误、刷新中、无缓存、可展示数据的顺序降级。
  if (row.quotaLoading) {
    return <div className={styles.credentialQuotaState}>{t('usage_stats.credentials_quota_loading')}</div>
  }
  if (row.quotaError) {
    return <div className={styles.credentialQuotaStateError}>{row.quotaError}</div>
  }
  if (row.refreshStatus === 'queued' || row.refreshStatus === 'running') {
    return <div className={styles.credentialQuotaRefreshStatus}>{t(`usage_stats.credentials_refresh_status_${row.refreshStatus}`)}</div>
  }
  if (!row.primaryQuota && !row.secondaryQuota && row.extraQuota.length === 0) {
    return <div className={styles.credentialQuotaState}>{t('usage_stats.credentials_quota_unavailable')}</div>
  }

  return (
    <div className={styles.credentialQuotaPanel}>
      <div className={styles.credentialQuotaBars}>
        {/* 主/次窗口固定优先展示，额外窗口放到下方 chips，避免宽度被无限撑开。 */}
        {row.primaryQuota && <QuotaBar quota={row.primaryQuota} />}
        {row.secondaryQuota && <QuotaBar quota={row.secondaryQuota} />}
      </div>
      {row.extraQuota.length > 0 && (
        <div className={styles.credentialQuotaChips}>
          {row.extraQuota.map((quota) => (
            <span key={quota.key} className={styles.credentialQuotaChip}>
              <span>{quota.label}</span>
              {quota.remaining !== undefined && <strong>{formatCredentialNumber(quota.remaining)}</strong>}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}

export function formatQuotaResetLabel(resetAt: string): string {
  const resetTime = new Date(resetAt)
  const resetMs = resetTime.getTime()
  if (!Number.isFinite(resetMs)) {
    return ''
  }
  const remainingMinutes = Math.max(0, Math.ceil((resetMs - Date.now()) / 60_000))
  const days = Math.floor(remainingMinutes / 1_440)
  const hours = Math.floor((remainingMinutes % 1_440) / 60)
  const minutes = remainingMinutes % 60
  const month = String(resetTime.getMonth() + 1).padStart(2, '0')
  const day = String(resetTime.getDate()).padStart(2, '0')
  const hour = String(resetTime.getHours()).padStart(2, '0')
  const minute = String(resetTime.getMinutes()).padStart(2, '0')
  const duration = days > 0 ? `${days}d${hours}h${minutes}m` : `${hours}h${minutes}m`
  return `${duration} (${month}/${day} ${hour}:${minute})`
}

function QuotaBar({ quota }: { quota: DisplayQuota }) {
  // 条宽使用剩余额度百分比，颜色跟随剩余风险状态从绿到黄到红。
  const { t } = useTranslation()
  const percent = quota.barPercent ?? 0
  const width = `${Math.max(0, Math.min(100, percent))}%`
  const percentLabel = quota.barPercent === null ? '' : t('usage_stats.credentials_quota_percent_remaining', { percent: `${Math.round(quota.barPercent)}%` })
  const resetLabel = quota.resetText ? formatQuotaResetLabel(quota.resetText) : ''

  return (
    <div className={styles.credentialQuotaBarBlock}>
      <div className={styles.credentialQuotaBarHeader}>
        <span className={styles.credentialQuotaLabelGroup}>
          <span>{quota.label}</span>
        </span>
        {percentLabel && (
          <span className={styles.credentialQuotaValueGroup}>
            <strong>{percentLabel}</strong>
          </span>
        )}
      </div>
      <div className={styles.credentialQuotaTrack}>
        <span className={`${styles.credentialQuotaFill} ${credentialToneClassName('credentialQuotaFill', quota.status)}`.trim()} style={{ width }} />
      </div>
      <div className={styles.credentialQuotaMeta}>
        {resetLabel && <span>{resetLabel}</span>}
      </div>
    </div>
  )
}
