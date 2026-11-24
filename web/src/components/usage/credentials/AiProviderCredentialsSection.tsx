import type { Ref } from 'react'
import { useTranslation } from 'react-i18next'
import { SwitchControl } from '@/components/ui/SwitchControl'
import styles from './CredentialSections.module.scss'
import cardStyles from './cards/CredentialCards.module.scss'
import listStyles from './list/CredentialList.module.scss'
import type { AiProviderCredentialRow } from './credentialViewModels'
import type { UsageIdentityPageSort } from '@/lib/api'
import { CredentialSectionShell, CredentialsPagination } from './CredentialSectionShell'
import { AiProviderCardsView } from './cards/AiProviderCardsView'
import { AiProviderListView } from './list/AiProviderListView'
import { CredentialViewModeSwitch } from './CredentialViewModeSwitch'
import type { CredentialViewMode } from './credentialListPreferences'
import { CredentialViewHelp } from './CredentialViewHelp'

interface AiProviderCredentialsSectionProps {
  rows: AiProviderCredentialRow[]
  total: number
  page: number
  totalPages: number
  pageSize: number
  activeOnly: boolean
  sort: UsageIdentityPageSort
  loading: boolean
  viewMode: CredentialViewMode
  onViewModeChange: (mode: CredentialViewMode) => void
  /** 编辑行消失时，焦点留在当前列表的原生筛选控件，不改变页面容器。 */
  editFallbackRef?: Ref<HTMLInputElement>
  onEdit?: (row: AiProviderCredentialRow) => void
  onOpenDetails?: (row: AiProviderCredentialRow) => void
  /** 正在写入上游状态的 Keeper identity id 集合，用于阻止重复点击。 */
  statusPendingIdentityIds?: ReadonlySet<string>
  onToggleStatus?: (identityId: string, authIndex: string, disabled: boolean) => void
  onSavePriority?: (identityId: string, authIndex: string, priority: number) => Promise<void>
  onPageChange: (page: number) => void
  onPageSizeChange: (pageSize: number) => void
  onActiveOnlyChange: (activeOnly: boolean) => void
  onSortChange: (sort: UsageIdentityPageSort) => void
}

export function AiProviderCredentialsSection({ rows, total, page, totalPages, pageSize, activeOnly, sort, loading, viewMode, onViewModeChange, editFallbackRef, onEdit, onOpenDetails, statusPendingIdentityIds, onToggleStatus, onSavePriority, onPageChange, onPageSizeChange, onActiveOnlyChange, onSortChange }: AiProviderCredentialsSectionProps) {
  const { t } = useTranslation()

  return (
    <CredentialSectionShell
      title={t('usage_stats.credentials_ai_providers_title')}
      viewMode={viewMode}
      contentClassName={viewMode === 'cards' ? cardStyles.cardContent : listStyles.listContent}
      titleHelp={(
        <CredentialViewHelp
          mode={viewMode}
          label={t('usage_stats.credentials_ai_providers_view_help_label')}
          cardDescription={t('usage_stats.credentials_ai_providers_subtitle')}
          note={t('usage_stats.credentials_ai_providers_compatibility_help')}
        />
      )}
      titleExtra={(
        <div className={styles.credentialsTitleControls}>
          <div className={styles.credentialFilterGroup}>
            <SwitchControl
              ref={editFallbackRef}
              checked={activeOnly}
              label={t('usage_stats.credentials_ai_providers_active_only')}
              variant="pill"
              onChange={onActiveOnlyChange}
            />
          </div>
          <span className={styles.credentialFilterDivider} aria-hidden="true" />
          <CredentialViewModeSwitch mode={viewMode} onChange={onViewModeChange} />
        </div>
      )}
    >
      {loading && rows.length === 0 && <div className={styles.credentialEmptyState}>{t('common.loading')}</div>}
      {!loading && rows.length === 0 && <div className={styles.credentialEmptyState}>{t('usage_stats.credentials_ai_providers_empty')}</div>}
      {viewMode === 'cards'
        ? <AiProviderCardsView rows={rows} onEdit={onEdit} onOpenDetails={onOpenDetails}
            statusPendingIdentityIds={statusPendingIdentityIds} onToggleStatus={onToggleStatus} onSavePriority={onSavePriority} />
        : <AiProviderListView rows={rows} onEdit={onEdit} onOpenDetails={onOpenDetails}
            statusPendingIdentityIds={statusPendingIdentityIds} onToggleStatus={onToggleStatus} onSavePriority={onSavePriority} />}
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
  )
}
