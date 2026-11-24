import { useState, type Ref } from 'react'
import { useTranslation } from 'react-i18next'
import { MainActionButton } from '@/components/ui/MainActionButton'
import { SwitchControl } from '@/components/ui/SwitchControl'
import { IconRefreshCw, IconSearch } from '@/components/ui/icons'
import styles from './CredentialSections.module.scss'
import cardStyles from './cards/CredentialCards.module.scss'
import listStyles from './list/CredentialList.module.scss'
import type { CredentialRow } from './credentialViewModels'
import type { UsageIdentityPageSort } from '@/lib/api'
import type { UsageQuotaInspectionStatusResponse, UsageQuotaResetResponse } from '@/lib/types'
import { CredentialSectionShell, CredentialsPagination } from './CredentialSectionShell'
import { capitalize } from './credentialMetrics'
import { CredentialCardsView } from './cards/CredentialCardsView'
import { CredentialListView } from './list/CredentialListView'
import { CredentialListContentModeSwitch } from './list/CredentialListContentModeSwitch'
import { CredentialViewModeSwitch, QuotaUsageModeSwitch } from './CredentialViewModeSwitch'
import { CredentialViewHelp } from './CredentialViewHelp'
import { persistCredentialListContentMode, readCredentialListContentMode, type CredentialListContentMode, type CredentialViewMode } from './credentialListPreferences'
import type { QuotaUsageMode } from './quota/CredentialQuotaPanel'
import { isRowRefreshing } from './quota/credentialQuotaCapabilities'
import { QuotaInspectionModal } from './inspection/QuotaInspectionModal'
import { inspectionIndicatorTone } from './inspection/quotaInspectionModel'

interface CredentialsSectionProps {
  rows: CredentialRow[]
  timeZone?: string
  total: number
  page: number
  totalPages: number
  pageSize: number
  activeOnly: boolean
  sort: UsageIdentityPageSort
  loading: boolean
  viewMode: CredentialViewMode
  onViewModeChange: (mode: CredentialViewMode) => void
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
  onEdit?: (row: CredentialRow) => void
  onOpenDetails?: (row: CredentialRow) => void
  /** 正在写入上游状态的 Keeper identity id 集合，用于阻止重复点击。 */
  statusPendingIdentityIds?: ReadonlySet<string>
  onToggleStatus?: (identityId: string, authIndex: string, disabled: boolean) => void
  onSavePriority?: (identityId: string, authIndex: string, priority: number) => Promise<void>
  onRefreshInspectionStatus: () => Promise<void>
  onStartInspection: () => Promise<void>
  onAfterInvalidAccountAction?: () => Promise<void>
}

export function CredentialsSection({ rows, timeZone, total, page, totalPages, pageSize, activeOnly, sort, loading, viewMode, onViewModeChange, quotaRefreshing, quotaRefreshError, quotaInspectionStatus, quotaInspectionLoading, quotaInspectionStarting, quotaInspectionError, onPageChange, onPageSizeChange, onActiveOnlyChange, onSortChange, onRefreshQuota, onRefreshQuotaForAuthIndex, onResetQuotaForAuthIndex, editFallbackRef, onEdit, onOpenDetails, statusPendingIdentityIds, onToggleStatus, onSavePriority, onRefreshInspectionStatus, onStartInspection, onAfterInvalidAccountAction }: CredentialsSectionProps) {
  const { t } = useTranslation()
  const [inspectionOpen, setInspectionOpen] = useState(false)
  const [quotaUsageMode, setQuotaUsageMode] = useState<QuotaUsageMode>('current')
  const [listContentMode, setListContentModeState] = useState(readCredentialListContentMode)
  const setListContentMode = (mode: CredentialListContentMode) => {
    setListContentModeState(mode)
    persistCredentialListContentMode(mode)
  }
  const canRefresh = rows.some((row) => !isRowRefreshing(row) && !row.identity.is_deleted) && !quotaRefreshing
  const inspectionTone = inspectionIndicatorTone(quotaInspectionStatus)
  const openInspection = () => {
    setInspectionOpen(true)
    void onRefreshInspectionStatus()
  }


  return (
    <>
      <CredentialSectionShell
        title={t('usage_stats.credentials_title')}
        viewMode={viewMode}
        contentClassName={viewMode === 'cards' ? cardStyles.cardContent : listStyles.listContent}
        titleHelp={(
          <CredentialViewHelp
            mode={viewMode}
            label={t('usage_stats.credentials_view_help_label')}
            cardDescription={t('usage_stats.credentials_subtitle')}
          />
        )}
        titleExtra={(
          <div className={styles.credentialsTitleControls}>
            <div className={styles.credentialFilterGroup}>
              <SwitchControl
                ref={editFallbackRef}
                checked={activeOnly}
                label={t('usage_stats.credentials_active_only')}
                variant="pill"
                onChange={onActiveOnlyChange}
              />
            </div>
            <span className={styles.credentialFilterDivider} aria-hidden="true" />
            <CredentialViewModeSwitch mode={viewMode} onChange={onViewModeChange} />
            {viewMode === 'list' && <>
              <span className={styles.credentialControlDivider} aria-hidden="true" />
              <CredentialListContentModeSwitch mode={listContentMode} onChange={setListContentMode} />
            </>}
            {total !== 0 && (viewMode === 'cards' || listContentMode === 'quota') && <>
              <span className={styles.credentialControlDivider} aria-hidden="true" />
              <QuotaUsageModeSwitch mode={quotaUsageMode} onChange={setQuotaUsageMode} />
            </>}
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
      {!loading && rows.length === 0 && <div className={styles.credentialEmptyState}>{t('usage_stats.credentials_empty')}</div>}
      {viewMode === 'cards'
        ? <CredentialCardsView rows={rows} timeZone={timeZone} quotaUsageMode={quotaUsageMode}
            onRefreshQuotaForAuthIndex={onRefreshQuotaForAuthIndex} onResetQuotaForAuthIndex={onResetQuotaForAuthIndex}
            onEdit={onEdit} onOpenDetails={onOpenDetails} statusPendingIdentityIds={statusPendingIdentityIds}
            onToggleStatus={onToggleStatus} onSavePriority={onSavePriority} />
        : <CredentialListView rows={rows} timeZone={timeZone} quotaUsageMode={quotaUsageMode} contentMode={listContentMode}
            onRefreshQuotaForAuthIndex={onRefreshQuotaForAuthIndex} onResetQuotaForAuthIndex={onResetQuotaForAuthIndex}
            onEdit={onEdit} onOpenDetails={onOpenDetails} statusPendingIdentityIds={statusPendingIdentityIds}
            onToggleStatus={onToggleStatus} onSavePriority={onSavePriority} />}
      <CredentialsPagination
        className={viewMode === 'cards' ? cardStyles.cardPagination : undefined}
        controlsClassName={viewMode === 'cards' ? cardStyles.cardPaginationControls : undefined}
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
        onClose={() => setInspectionOpen(false)}
        onStart={onStartInspection}
        onRefreshStatus={onRefreshInspectionStatus}
        onAfterInvalidAccountAction={onAfterInvalidAccountAction}
      />
    </>
  )
}
