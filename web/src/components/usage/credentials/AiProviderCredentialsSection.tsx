import { useTranslation } from 'react-i18next'
import styles from './CredentialSections.module.scss'
import type { AiProviderCredentialRow } from './credentialViewModels'
import { CredentialBadge, CredentialRowShell, CredentialSectionShell, CredentialsPagination, MetricPill, RequestMetric, TonePercent, cacheRateTone, formatCredentialNumber, successRateTone } from './CredentialSectionShell'

interface AiProviderCredentialsSectionProps {
  rows: AiProviderCredentialRow[]
  total: number
  page: number
  totalPages: number
  loading: boolean
  onPageChange: (page: number) => void
}

export function AiProviderCredentialsSection({ rows, total, page, totalPages, loading, onPageChange }: AiProviderCredentialsSectionProps) {
  const { t } = useTranslation()

  return (
    <CredentialSectionShell
      eyebrow={t('usage_stats.credentials_ai_providers_eyebrow')}
      title={t('usage_stats.credentials_ai_providers_title')}
      subtitle={t('usage_stats.credentials_ai_providers_subtitle')}
      countLabel={t('usage_stats.credentials_count', { count: total })}
    >
      {loading && rows.length === 0 && <div className={styles.credentialEmptyState}>{t('common.loading')}</div>}
      {!loading && rows.length === 0 && <div className={styles.credentialEmptyState}>{t('usage_stats.credentials_ai_providers_empty')}</div>}
      {rows.map((row) => (
        <CredentialRowShell
          key={row.identity.id || row.identity.identity}
          title={row.displayName}
          subtitle={<CredentialBadge>{row.typeLabel}</CredentialBadge>}
          badges={null}
          metrics={(
            <>
              <MetricPill label={t('usage_stats.total_requests')} value={<RequestMetric total={row.totalRequests} success={row.successCount} failure={row.failureCount} />} />
              <MetricPill label={t('usage_stats.success_rate')} value={<TonePercent value={row.successRate} tone={successRateTone(row.successRate)} />} />
              <MetricPill label={t('usage_stats.total_tokens')} value={formatCredentialNumber(row.totalTokens)} />
              <MetricPill label={t('usage_stats.cache_rate')} value={<TonePercent value={row.cacheRate} tone={cacheRateTone(row.cacheRate)} />} />
            </>
          )}
          side={<AiProviderTrafficPanel row={row} />}
        />
      ))}
      <CredentialsPagination page={page} totalPages={totalPages} previousLabel={t('usage_stats.previous_page')} nextLabel={t('usage_stats.next_page')} onPageChange={onPageChange} />
    </CredentialSectionShell>
  )
}

function AiProviderTrafficPanel({ row }: { row: AiProviderCredentialRow }) {
  const { t } = useTranslation()
  const lastUsed = formatDate(row.lastUsedText)
  const statsUpdated = formatDate(row.statsUpdatedText)
  if (!lastUsed && !statsUpdated) {
    return null
  }
  return (
    <div className={styles.credentialTrafficPanel}>
      {lastUsed && <span>{t('usage_stats.credentials_last_used')}</span>}
      {lastUsed && <strong>{lastUsed}</strong>}
      {statsUpdated && <span>{t('usage_stats.credentials_stats_updated')}</span>}
      {statsUpdated && <strong>{statsUpdated}</strong>}
    </div>
  )
}

function formatDate(value: string | undefined): string {
  if (!value) {
    return ''
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  return date.toLocaleString()
}
