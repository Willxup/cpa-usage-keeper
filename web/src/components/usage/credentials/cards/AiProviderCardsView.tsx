import { useTranslation } from 'react-i18next'
import { ProviderBrandIcon } from '@/components/ProviderBrandIcon'
import styles from '../CredentialSections.module.scss'
import { formatCredentialTimestamp } from '../credentialViewModels'
import type { AiProviderViewProps } from '../credentialViewProps'
import { CredentialAliasEditor, CredentialEditButton } from '../CredentialAliasEditor'
import { CredentialHealthPanel } from '../CredentialHealthPanel'
import { CredentialCardGrid, CredentialCardShell, MetricPill } from './CredentialCardShell'
import { RequestMetric, TonePercent, cacheReadRateTone, formatCredentialNumber, successRateTone } from '../credentialMetrics'
import { CredentialPriorityEditor } from '../CredentialPriorityEditor'
import { CredentialStatusToggle, CredentialStatusUnsupportedIcon, isCredentialStatusToggleSupported } from '../CredentialStatusToggle'

export function AiProviderCardsView({ rows, onEdit, onOpenDetails, statusPendingIdentityIds, onToggleStatus, onSavePriority }: AiProviderViewProps) {
  const { t } = useTranslation()
  return (
      <CredentialCardGrid>
      {rows.map((row) => {
        const openDetails = onOpenDetails ? () => onOpenDetails(row) : undefined
        return <CredentialCardShell
          key={row.identity.id || row.identity.identity}
          identityId={row.identity.id || row.identity.identity}
          icon={<ProviderBrandIcon providerType={row.identity.type} size={30} />}
          status={isCredentialStatusToggleSupported(row.identity.type) ? (
            <CredentialStatusToggle
              variant="switch"
              providerType={row.identity.type}
              displayName={row.displayName}
              disabled={row.identity.disabled}
              pending={statusPendingIdentityIds?.has(row.identity.id || row.identity.identity) ?? false}
              readOnly={row.identity.is_deleted}
              onToggle={(disabled) => onToggleStatus?.(row.identity.id || row.identity.identity, row.identity.identity, disabled)}
            />
          ) : (
            // OpenAI 兼容类型的静态说明与展示性品牌图标分开，不提供可执行开关。
            <CredentialStatusUnsupportedIcon variant="info" displayName={row.displayName} providerType={row.identity.type} />
          )}
          title={(
            <CredentialAliasEditor
              identityId={row.identity.id}
              displayName={row.displayName}
              showEditButton={false}
              disabled={row.identity.is_deleted}
              onOpenDetails={openDetails}
            />
          )}
          badges={row.priorityLabel || (onSavePriority && !row.identity.is_deleted) ? (
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
          metricsTitle={row.identity.stats_reset_at ? t('usage_stats.credentials_stats_since', { time: formatCredentialTimestamp(row.identity.stats_reset_at) ?? row.identity.stats_reset_at }) : undefined}
          metrics={(
            <>
              <MetricPill label={t('usage_stats.credentials_metric_requests')} value={<RequestMetric total={row.totalRequests} success={row.successCount} failure={row.failureCount} />} />
              <MetricPill label={t('usage_stats.credentials_metric_success')} value={<TonePercent value={row.successRate} tone={successRateTone(row.successRate)} />} />
              <MetricPill label={t('usage_stats.credentials_metric_tokens')} value={formatCredentialNumber(row.totalTokens)} />
              <MetricPill label={t('usage_stats.credentials_metric_cache')} value={<TonePercent value={row.cacheReadRate} tone={cacheReadRateTone(row.cacheReadRate)} />} />
            </>
          )}
          health={<CredentialHealthPanel variant="compact" showCompactMeta displayName={row.displayName} health={row.credentialHealth} lastUsedAt={row.lastUsedText} windowCacheReadRate={row.windowCacheReadRate} />}
          actions={onEdit && <CredentialEditButton identityId={row.identity.id} disabled={row.identity.is_deleted} onEdit={() => onEdit(row)} cardTooltip />}
        />
      })}
      </CredentialCardGrid>
  )
}
