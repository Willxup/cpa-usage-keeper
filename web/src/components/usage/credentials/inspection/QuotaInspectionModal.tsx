import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { Modal } from '@/components/ui/Modal'
import { Select } from '@/components/ui/Select'
import { IconSearch, IconSettings, IconShield, IconTrash2 } from '@/components/ui/icons'
import { ProviderBrandIcon } from '@/components/ProviderBrandIcon'
import type { UsageQuotaInspectionResult, UsageQuotaInspectionStatusResponse } from '@/lib/types'
import styles from './QuotaInspection.module.scss'
import shared from '../CredentialSections.module.scss'
import { capitalize } from '../credentialMetrics'
import { credentialBrandOverride } from '../credentialBrand'
import { QuotaAutoRefreshSettingsModal } from './QuotaAutoRefreshSettingsModal'
import { InvalidInspectionAccountsModal, type InvalidInspectionAccountSession } from './InvalidInspectionAccountsModal'
import { INSPECTION_RESULT_PAGE_SIZE_OPTIONS, DEFAULT_INSPECTION_RESULT_PAGE_SIZE, inspectionProgressTotal, formatInspectionProgressPercent, isInspectionStartDisabled, isSelectableInspectionStatusFilter, nextInspectionResultStatusFilter, buildInspectionResultsPage, buildInvalidInspectionAccountFileNames, isQuotaInspectionCloseDisabled, formatInspectionCompletedAt, type InspectionResultStatusFilter, type InspectionResultStatusFilterState, type InspectionStatTone } from './quotaInspectionModel'

const INSPECTION_MODAL_FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

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
  const [invalidSession, setInvalidSession] = useState<InvalidInspectionAccountSession | null>(null)
  const [settingsModalOpen, setSettingsModalOpen] = useState(false)
  const panelRef = useRef<HTMLDivElement | null>(null)
  const disableInvalidButtonRef = useRef<HTMLButtonElement | null>(null)
  const deleteInvalidButtonRef = useRef<HTMLButtonElement | null>(null)
  const restoreInvalidActionFocusRef = useRef<InvalidInspectionAccountSession['action'] | null>(null)
  // total 由后端 Credentials 身份统计提供，不用页面分页总数替代。
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
  const openInvalidAccountAction = (action: InvalidInspectionAccountSession['action']) => {
    setInvalidSession({ action, fileNamesSnapshot: [...invalidFileNames] })
  }
  const closeInvalidAccountAction = (action: InvalidInspectionAccountSession['action']) => {
    restoreInvalidActionFocusRef.current = action
    setInvalidSession(null)
  }
  const inspectionCloseDisabled = isQuotaInspectionCloseDisabled({ invalidAccountActionOpen: invalidSession !== null, settingsModalOpen })

  useEffect(() => {
    if (!open) {
      restoreInvalidActionFocusRef.current = null
      return
    }
    if (invalidSession !== null) return
    const closedAction = restoreInvalidActionFocusRef.current
    if (!closedAction) return
    restoreInvalidActionFocusRef.current = null

    // 子弹窗直接卸载会跳过 Modal 关闭回焦；待父弹窗按钮恢复可用状态后优先回到原触发按钮，
    // 若所有无效账号已被处理导致原按钮禁用，则兜底聚焦父巡检弹窗内可用控件，避免焦点落回 body。
    const triggerButton = closedAction === 'delete'
      ? deleteInvalidButtonRef.current
      : disableInvalidButtonRef.current
    if (triggerButton && triggerButton.isConnected && !triggerButton.disabled) {
      triggerButton.focus()
      return
    }
    const parentDialog = panelRef.current?.closest<HTMLElement>('.modal[role="dialog"][aria-modal="true"]')
    if (!parentDialog) return
    const fallbackTarget = Array.from(
      parentDialog.querySelectorAll<HTMLElement>(INSPECTION_MODAL_FOCUSABLE_SELECTOR),
    ).find((element) => !element.hasAttribute('disabled') && element.tabIndex !== -1)
    ;(fallbackTarget ?? parentDialog).focus()
  }, [invalidSession, open])

  return (
    <Modal
      open={open}
      title={t('usage_stats.credentials_inspection_title')}
      onClose={inspectionCloseDisabled ? () => undefined : () => {
        setSettingsModalOpen(false)
        onClose()
      }}
      width={820}
      className={styles.credentialInspectionModal}
      closeDisabled={inspectionCloseDisabled}
    >
      <div ref={panelRef} className={styles.credentialInspectionPanel}>
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
            className={`${shared.credentialActionButton} ${styles.credentialInspectionStartButton}`.trim()}
            onClick={() => void onStart()}
            disabled={startDisabled}
            aria-busy={starting}
          >
            {starting ? <LoadingSpinner size={13} /> : <IconSearch size={13} />}
            <span>{startLabel}</span>
          </button>
        </div>

        {error && <div className={shared.credentialInlineError}>{error}</div>}
        {loading && !status && <div className={shared.credentialEmptyState}>{t('common.loading')}</div>}

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
                    ref={disableInvalidButtonRef}
                    type="button"
                    className={styles.credentialInspectionInvalidActionButton}
                    onClick={() => openInvalidAccountAction('disable')}
                    disabled={invalidFileNames.length === 0 || invalidSession !== null}
                  >
                    <IconShield size={13} />
                    <span>{t('usage_stats.credentials_inspection_disable_invalid')}</span>
                  </button>
                  <button
                    ref={deleteInvalidButtonRef}
                    type="button"
                    className={`${styles.credentialInspectionInvalidActionButton} ${styles.credentialInspectionInvalidActionButtonDanger}`.trim()}
                    onClick={() => openInvalidAccountAction('delete')}
                    disabled={invalidFileNames.length === 0 || invalidSession !== null}
                  >
                    <IconTrash2 size={13} />
                    <span>{t('usage_stats.credentials_inspection_delete_invalid')}</span>
                  </button>
                </div>
              </div>
            )}
          </div>
          {resultPageData.total === 0 ? (
            <div className={shared.credentialEmptyState}>{t('usage_stats.credentials_inspection_empty_results')}</div>
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
      {invalidSession && (
        <InvalidInspectionAccountsModal
          session={invalidSession}
          onClose={() => closeInvalidAccountAction(invalidSession.action)}
          onRefreshStatus={onRefreshStatus}
          onAfterInvalidAccountAction={onAfterInvalidAccountAction}
        />
      )}
      <QuotaAutoRefreshSettingsModal open={settingsModalOpen} onClose={() => setSettingsModalOpen(false)} />
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
        <ProviderBrandIcon providerType={credentialBrandOverride(result.type)?.iconType ?? result.type} size={20} />
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

function formatInspectionDate(value: string | undefined): string {
  return formatInspectionCompletedAt(value)
}

