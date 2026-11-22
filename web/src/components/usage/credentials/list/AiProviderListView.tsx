import { useTranslation } from 'react-i18next'
import styles from '../CredentialSections.module.scss'
import listStyles from './CredentialList.module.scss'
import { formatCredentialTimestamp } from '../credentialViewModels'
import type { AiProviderViewProps } from '../credentialViewProps'
import { CredentialAliasEditor } from '../CredentialAliasEditor'
import { CredentialHealthPanel } from '../CredentialHealthPanel'
import { RequestMetric, TonePercent, cacheReadRateTone, formatCredentialNumber, successRateTone } from '../credentialMetrics'
import { CredentialPriorityEditor } from '../CredentialPriorityEditor'
import { CredentialStatusToggle, CredentialStatusUnsupportedIcon, isCredentialStatusToggleSupported } from '../CredentialStatusToggle'
import { CredentialRowShell, CredentialTableHeader, ListMetric } from './CredentialListShell'

export function AiProviderListView({ rows, onEdit, onOpenDetails, statusPendingIdentityIds, onToggleStatus, onSavePriority }: AiProviderViewProps) {
  const { t } = useTranslation()
  return <>
      {rows.length > 0 && (
        <CredentialTableHeader
          rowClassName={listStyles.aiProviderCredentialRow}
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
          identityId={row.identity.id || row.identity.identity}
          metricsTitle={row.identity.stats_reset_at ? t('usage_stats.credentials_stats_since', { time: formatCredentialTimestamp(row.identity.stats_reset_at) ?? row.identity.stats_reset_at }) : undefined}
          metrics={(
            <>
              <ListMetric value={<RequestMetric total={row.totalRequests} success={row.successCount} failure={row.failureCount} />} />
              <ListMetric value={<TonePercent value={row.successRate} tone={successRateTone(row.successRate)} />} />
              <ListMetric value={formatCredentialNumber(row.totalTokens)} />
              <ListMetric value={<TonePercent value={row.cacheReadRate} tone={cacheReadRateTone(row.cacheReadRate)} />} />
            </>
          )}
          side={<CredentialHealthPanel displayName={row.displayName} health={row.credentialHealth} lastUsedAt={row.lastUsedText} statsUpdatedAt={row.statsUpdatedText} windowCacheReadRate={row.windowCacheReadRate} />}
          rowClassName={listStyles.aiProviderCredentialRow}
        />
      ))}
  </>
}
