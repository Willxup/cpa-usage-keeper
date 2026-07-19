import { Table, type TableColumnsType } from 'antd'
import { useTranslation } from 'react-i18next'
import styles from './CredentialSections.module.scss'
import type { AiProviderCredentialRow } from './credentialViewModels'
import type { UsageIdentityPageSort } from '@/lib/api'
import { CredentialAliasEditor, isCredentialAliasEditorDisabled } from './CredentialAliasEditor'
import { CredentialHealthPanel } from './CredentialHealthPanel'
import { CredentialPriorityBadge, CredentialSectionShell, CredentialsPagination, MetricPill, RequestMetric, TonePercent, cacheReadRateTone, formatCredentialNumber, successRateTone } from './CredentialSectionShell'

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
  const nameLabel = t('usage_stats.credentials_column_name')
  const providerLabel = t('usage_stats.credentials_column_provider')
  const totalRequestsLabel = t('usage_stats.total_requests')
  const successRateLabel = t('usage_stats.success_rate')
  const totalTokensLabel = t('usage_stats.total_tokens')
  const cacheReadRateLabel = t('usage_stats.cache_rate')
  const healthLabel = t('usage_stats.credentials_column_health')
  const columns: TableColumnsType<AiProviderCredentialRow> = [
    {
      key: 'name',
      title: nameLabel,
      width: 216,
      className: styles.credentialNameTableCell,
      render: (_value, row) => (
        <div className={styles.credentialIdentityBlock}>
          <span className={styles.credentialResponsiveCellLabel}>{nameLabel}</span>
          <div className={styles.credentialNameRow}>
            <span className={styles.credentialDisplayName} title={row.displayName}>
              {onSaveAlias ? (
                <CredentialAliasEditor
                  identityId={row.identity.id}
                  displayName={row.displayName}
                  alias={row.identity.alias}
                  saving={aliasSavingId === row.identity.id}
                  disabled={isCredentialAliasEditorDisabled(row.identity.id, row.identity.is_deleted, aliasSavingId)}
                  onSaveAlias={onSaveAlias}
                />
              ) : row.displayName}
            </span>
          </div>
          {row.priorityLabel && (
            <span className={styles.credentialIdentityText}>
              <span className={styles.credentialIdentityBadges}>
                <CredentialPriorityBadge>{row.priorityLabel}</CredentialPriorityBadge>
              </span>
            </span>
          )}
        </div>
      ),
    },
    {
      key: 'provider',
      title: providerLabel,
      width: 92,
      className: styles.credentialMetricTableCell,
      render: (_value, row) => (
        <div className={styles.credentialTableCellContent}>
          <span className={styles.credentialResponsiveCellLabel}>{providerLabel}</span>
          <span className={styles.credentialProviderValue} title={row.providerLabel}>{row.providerLabel}</span>
        </div>
      ),
    },
    {
      key: 'totalRequests',
      title: totalRequestsLabel,
      width: 176,
      className: styles.credentialMetricTableCell,
      render: (_value, row) => (
        <div className={styles.credentialTableCellContent}>
          <span className={styles.credentialResponsiveCellLabel}>{totalRequestsLabel}</span>
          <MetricPill value={<RequestMetric total={row.totalRequests} success={row.successCount} failure={row.failureCount} />} />
        </div>
      ),
    },
    {
      key: 'successRate',
      title: successRateLabel,
      width: 88,
      className: styles.credentialMetricTableCell,
      render: (_value, row) => (
        <div className={styles.credentialTableCellContent}>
          <span className={styles.credentialResponsiveCellLabel}>{successRateLabel}</span>
          <MetricPill value={<TonePercent value={row.successRate} tone={successRateTone(row.successRate)} />} />
        </div>
      ),
    },
    {
      key: 'totalTokens',
      title: totalTokensLabel,
      width: 92,
      className: styles.credentialMetricTableCell,
      render: (_value, row) => (
        <div className={styles.credentialTableCellContent}>
          <span className={styles.credentialResponsiveCellLabel}>{totalTokensLabel}</span>
          <MetricPill value={formatCredentialNumber(row.totalTokens)} />
        </div>
      ),
    },
    {
      key: 'cacheReadRate',
      title: cacheReadRateLabel,
      width: 84,
      className: styles.credentialMetricTableCell,
      render: (_value, row) => (
        <div className={styles.credentialTableCellContent}>
          <span className={styles.credentialResponsiveCellLabel}>{cacheReadRateLabel}</span>
          <MetricPill value={<TonePercent value={row.cacheReadRate} tone={cacheReadRateTone(row.cacheReadRate)} />} />
        </div>
      ),
    },
    {
      key: 'health',
      title: healthLabel,
      width: 430,
      className: styles.credentialSideTableCell,
      render: (_value, row) => (
        <div className={styles.credentialSidePanel}>
          <span className={styles.credentialResponsiveCellLabel}>{healthLabel}</span>
          <CredentialHealthPanel displayName={row.displayName} health={row.credentialHealth} lastUsedAt={row.lastUsedText} statsUpdatedAt={row.statsUpdatedText} />
        </div>
      ),
    },
  ]

  return (
    <CredentialSectionShell
      title={t('usage_stats.credentials_ai_providers_title')}
      countLabel={t('usage_stats.credentials_count', { count: total })}
    >
      {loading && rows.length === 0 && <div className={styles.credentialEmptyState}>{t('common.loading')}</div>}
      {!loading && rows.length === 0 && <div className={styles.credentialEmptyState}>{t('usage_stats.credentials_ai_providers_empty')}</div>}
      {rows.length > 0 && (
        <Table<AiProviderCredentialRow>
          className={styles.credentialDataTable}
          columns={columns}
          dataSource={rows}
          rowKey={(row) => row.identity.id || row.identity.identity}
          pagination={false}
          size="small"
          scroll={{ x: 1178 }}
        />
      )}
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
