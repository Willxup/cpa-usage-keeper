import type { Ref } from 'react'
import { useTranslation } from 'react-i18next'
import styles from './CredentialSections.module.scss'
import { formatCredentialTimestamp, type AiProviderCredentialRow } from './credentialViewModels'
import type { UsageIdentityPageSort } from '@/lib/api'
import { CredentialAliasEditor } from './CredentialAliasEditor'
import { CredentialHealthPanel } from './CredentialHealthPanel'
import { CredentialRowShell, CredentialSectionShell, CredentialTableHeader, CredentialsPagination, MetricPill, RequestMetric, TonePercent, cacheReadRateTone, formatCredentialNumber, successRateTone } from './CredentialSectionShell'
import { CredentialPriorityEditor } from './CredentialPriorityEditor'
import { CredentialStatusToggle, CredentialStatusUnsupportedIcon, isCredentialStatusToggleSupported } from './CredentialStatusToggle'
import { QuestionMarkHelp } from '@/components/ui/QuestionMarkHelp'

interface AiProviderCredentialsSectionProps {
  rows: AiProviderCredentialRow[]
  total: number
  page: number
  totalPages: number
  pageSize: number
  activeOnly: boolean
  sort: UsageIdentityPageSort
  loading: boolean
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

export function AiProviderCredentialsSection({ rows, total, page, totalPages, pageSize, activeOnly, sort, loading, editFallbackRef, onEdit, onOpenDetails, statusPendingIdentityIds, onToggleStatus, onSavePriority, onPageChange, onPageSizeChange, onActiveOnlyChange, onSortChange }: AiProviderCredentialsSectionProps) {
  const { t } = useTranslation()
  const helpText = t('usage_stats.credentials_ai_providers_active_only_help')

  return (
    <CredentialSectionShell
      title={t('usage_stats.credentials_ai_providers_title')}
      subtitle={t('usage_stats.credentials_ai_providers_subtitle')}
      countLabel={t('usage_stats.credentials_count', { count: total })}
      titleExtra={(
        <div className={styles.credentialAuthFileTitleControls}>
          <label className={styles.credentialActiveOnlySwitch}>
            <span className={styles.credentialActiveOnlyLabel}>{t('usage_stats.credentials_ai_providers_active_only')}</span>
            <input ref={editFallbackRef} type="checkbox" checked={activeOnly} onChange={(event) => onActiveOnlyChange(event.target.checked)} />
            <span className={styles.credentialActiveOnlyTrack} aria-hidden="true">
              <span className={styles.credentialActiveOnlyThumb} />
            </span>
          </label>
          <QuestionMarkHelp
            label={t('usage_stats.credentials_ai_providers_active_only_help_label')}
            description={helpText}
            positioning={{
              align: 'center',
              estimatedHeight: 96,
              maxWidth: 280,
              offset: 10,
              viewportPadding: 8,
            }}
          >
            <span>{helpText}</span>
          </QuestionMarkHelp>
        </div>
      )}
    >
      {loading && rows.length === 0 && <div className={styles.credentialEmptyState}>{t('common.loading')}</div>}
      {!loading && rows.length === 0 && <div className={styles.credentialEmptyState}>{t('usage_stats.credentials_ai_providers_empty')}</div>}
      {rows.length > 0 && (
        <CredentialTableHeader
          rowClassName={styles.aiProviderCredentialRow}
          nameLabel={t('usage_stats.credentials_column_name')}
          totalRequestsLabel={t('usage_stats.total_requests')}
          successRateLabel={t('usage_stats.success_rate')}
          totalTokensLabel={t('usage_stats.total_tokens')}
          cacheReadRateLabel={t('usage_stats.cache_rate')}
          sideLabel={t('usage_stats.credentials_column_health')}
        />
      )}
      {rows.map((row) => (
        <CredentialRowShell
          key={row.identity.id || row.identity.identity}
          icon={isCredentialStatusToggleSupported(row.identity.type) ? (
            <CredentialStatusToggle
              providerType={row.identity.type}
              displayName={row.displayName}
              disabled={row.identity.disabled}
              pending={statusPendingIdentityIds?.has(row.identity.id || row.identity.identity) ?? false}
              readOnly={row.identity.is_deleted}
              onToggle={(disabled) => onToggleStatus?.(row.identity.id || row.identity.identity, row.identity.identity, disabled)}
            />
          ) : (
            // OpenAI 兼容类供应商没有对应的整条停用语义，静态图标复用同一套行内提示。
            <CredentialStatusUnsupportedIcon displayName={row.displayName} providerType={row.identity.type} />
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
          ) : row.displayName}
          subtitle={row.priorityLabel || (onSavePriority && !row.identity.is_deleted) ? (
            <span className={styles.credentialIdentityBadges}>
              <CredentialPriorityEditor
                priority={row.identity.priority}
                displayName={row.displayName}
                readOnly={row.identity.is_deleted}
                openAIShared={row.identity.type.trim().toLowerCase() === 'openai'}
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
          side={<CredentialHealthPanel displayName={row.displayName} health={row.credentialHealth} lastUsedAt={row.lastUsedText} statsUpdatedAt={row.statsUpdatedText} windowCacheReadRate={row.windowCacheReadRate} />}
          rowClassName={styles.aiProviderCredentialRow}
        />
      ))}
      <CredentialsPagination
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
