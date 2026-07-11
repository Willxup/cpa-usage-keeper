import { useTranslation } from 'react-i18next'
import styles from './CredentialSections.module.scss'
import type { AiProviderCredentialRow } from './credentialViewModels'
import type { UsageIdentityPageSort } from '@/lib/api'
import { CredentialAliasEditor, isCredentialAliasEditorDisabled } from './CredentialAliasEditor'
import { CredentialHealthPanel } from './CredentialHealthPanel'
import { CredentialBadge, CredentialPriorityBadge, CredentialRowShell, CredentialSectionShell, CredentialTableHeader, CredentialsPagination, MetricPill, RequestMetric, TonePercent, cacheReadRateTone, formatCredentialNumber, successRateTone } from './CredentialSectionShell'

interface AiProviderCredentialsSectionProps {
  rows: AiProviderCredentialRow[]
  total: number
  page: number
  totalPages: number
  pageSize: number
  sort: UsageIdentityPageSort
  loading: boolean
  aliasSavingId?: string
  onSaveAlias?: (id: string, alias: string) => Promise<void>
  onPageChange: (page: number) => void
  onPageSizeChange: (pageSize: number) => void
  onSortChange: (sort: UsageIdentityPageSort) => void
}

export function AiProviderCredentialsSection({ rows, total, page, totalPages, pageSize, sort, loading, aliasSavingId, onSaveAlias, onPageChange, onPageSizeChange, onSortChange }: AiProviderCredentialsSectionProps) {
  const { t } = useTranslation()

  return (
    <CredentialSectionShell
      title={t('usage_stats.credentials_ai_providers_title')}
      subtitle={t('usage_stats.credentials_ai_providers_subtitle')}
      countLabel={t('usage_stats.credentials_count', { count: total })}
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
          title={onSaveAlias ? (
            <CredentialAliasEditor
              identityId={row.identity.id}
              displayName={row.displayName}
              alias={row.identity.alias}
              saving={aliasSavingId === row.identity.id}
              disabled={isCredentialAliasEditorDisabled(row.identity.id, row.identity.is_deleted, aliasSavingId)}
              onSaveAlias={onSaveAlias}
            />
          ) : row.displayName}
          subtitle={(
            <span className={styles.credentialIdentityBadges}>
              <CredentialBadge>{row.typeLabel}</CredentialBadge>
              {row.priorityLabel && <CredentialPriorityBadge>{row.priorityLabel}</CredentialPriorityBadge>}
            </span>
          )}
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
