import { useTranslation } from 'react-i18next'
import styles from './CredentialSections.module.scss'
import type { AiProviderCredentialRow } from './credentialViewModels'
import type { UsageIdentityPageSort } from '@/lib/api'
import { CredentialAliasEditor, isCredentialAliasEditorDisabled } from './CredentialAliasEditor'
import { CredentialHealthPanel } from './CredentialHealthPanel'
import { CredentialPriorityBadge, CredentialRowShell, CredentialSectionShell, CredentialTableHeader, CredentialsPagination, MetricPill, RequestMetric, TonePercent, cacheReadRateTone, formatCredentialNumber, successRateTone } from './CredentialSectionShell'
import { ProviderBrandIcon } from '@/components/ProviderBrandIcon'
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
  aliasSavingId?: string
  onSaveAlias?: (id: string, alias: string) => Promise<void>
  onOpenDetails?: (row: AiProviderCredentialRow) => void
  onPageChange: (page: number) => void
  onPageSizeChange: (pageSize: number) => void
  onActiveOnlyChange: (activeOnly: boolean) => void
  onSortChange: (sort: UsageIdentityPageSort) => void
}

export function AiProviderCredentialsSection({ rows, total, page, totalPages, pageSize, activeOnly, sort, loading, aliasSavingId, onSaveAlias, onOpenDetails, onPageChange, onPageSizeChange, onActiveOnlyChange, onSortChange }: AiProviderCredentialsSectionProps) {
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
            <input type="checkbox" checked={activeOnly} onChange={(event) => onActiveOnlyChange(event.target.checked)} />
            <span className={styles.credentialActiveOnlyTrack} aria-hidden="true">
              <span className={styles.credentialActiveOnlyThumb} />
            </span>
          </label>
          <QuestionMarkHelp
            label={t('usage_stats.credentials_ai_providers_active_only_help_label')}
            description={helpText}
            positioning={{
              align: 'center',
              estimatedHeight: 72,
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
          icon={<ProviderBrandIcon providerType={row.identity.type} size={30} ariaLabel={row.typeLabel} />}
          title={onSaveAlias ? (
            <CredentialAliasEditor
              identityId={row.identity.id}
              displayName={row.displayName}
              alias={row.identity.alias}
              saving={aliasSavingId === row.identity.id}
              disabled={isCredentialAliasEditorDisabled(row.identity.id, row.identity.is_deleted, aliasSavingId)}
              onOpenDetails={onOpenDetails ? () => onOpenDetails(row) : undefined}
              onSaveAlias={onSaveAlias}
            />
          ) : onOpenDetails ? (
            <button
              type="button"
              className={styles.credentialDetailNameButton}
            data-credential-detail-trigger="true"
            onClick={() => onOpenDetails(row)}
          >
              <span className={styles.credentialDetailNameText}>{row.displayName}</span>
              <span className={styles.credentialDetailNameArrow} aria-hidden="true">›</span>
            </button>
          ) : row.displayName}
          subtitle={row.priorityLabel ? (
            <span className={styles.credentialIdentityBadges}>
              <CredentialPriorityBadge>{row.priorityLabel}</CredentialPriorityBadge>
            </span>
          ) : undefined}
          badges={null}
          metrics={(
            <>
              <MetricPill value={<RequestMetric total={row.totalRequests} success={row.successCount} failure={row.failureCount} />} />
              <MetricPill value={<TonePercent value={row.successRate} tone={successRateTone(row.successRate)} />} />
              <MetricPill value={formatCredentialNumber(row.totalTokens)} />
              <MetricPill value={<TonePercent value={row.cacheReadRate} tone={cacheReadRateTone(row.cacheReadRate)} />} />
            </>
          )}
          side={<CredentialHealthPanel displayName={row.displayName} health={row.credentialHealth} lastUsedAt={row.lastUsedText} statsUpdatedAt={row.statsUpdatedText} />}
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
