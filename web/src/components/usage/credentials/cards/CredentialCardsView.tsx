import { useTranslation } from 'react-i18next'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { IconRefreshCw } from '@/components/ui/icons'
import { ProviderBrandIcon, providerBrandIconKey } from '@/components/ProviderBrandIcon'
import styles from '../CredentialSections.module.scss'
import { credentialSIWCState, formatCredentialTimestamp } from '../credentialViewModels'
import { credentialBrandOverride } from '../credentialBrand'
import { CredentialSIWCStatus } from '../CredentialSIWCStatus'
import type { CredentialViewProps } from '../credentialViewProps'
import { CredentialAliasEditor, CredentialEditButton } from '../CredentialAliasEditor'
import { CredentialHealthPanel } from '../CredentialHealthPanel'
import { CredentialCardGrid, CredentialCardShell, MetricPill } from './CredentialCardShell'
import { CredentialCardActionTooltip } from './CredentialCardActionTooltip'
import { RequestMetric, TonePercent, cacheReadRateTone, formatCredentialNumber, successRateTone } from '../credentialMetrics'
import { CredentialStatusToggle } from '../CredentialStatusToggle'
import { useCredentialExpiryTooltip } from '../CredentialBadges'
import { QuotaResetAction } from '../quota/QuotaResetAction'
import { CredentialQuotaPanel } from '../quota/CredentialQuotaPanel'
import { credentialResetCapabilities, isRowRefreshing } from '../quota/credentialQuotaCapabilities'

export function CredentialCardsView({ rows, timeZone, quotaUsageMode, onRefreshQuotaForAuthIndex, onResetQuotaForAuthIndex, onEdit, onOpenDetails, statusPendingIdentityIds, onToggleStatus, onSavePriority }: CredentialViewProps) {
  const { t } = useTranslation()
  const { badges, portal } = useCredentialExpiryTooltip(rows)
  return <>
      <CredentialCardGrid>
      {rows.map((row) => {
        const rowRefreshing = isRowRefreshing(row)
        const { isClaude, resetCredits, canResetQuota } = credentialResetCapabilities(row)
        const rowKey = row.identity.id || row.identity.identity
        const siwcState = credentialSIWCState(row.identity)
        const brandType = credentialBrandOverride(row.identity.type)?.iconType ?? row.identity.type
        const openDetails = onOpenDetails ? () => onOpenDetails(row) : undefined
        return (
          <CredentialCardShell
            key={rowKey}
            identityId={rowKey}
            icon={<ProviderBrandIcon providerType={brandType} size={30} />}
            status={(
              // 认证文件沿用品牌类型的现有支持范围，不套用 AI Provider 的能力白名单。
              providerBrandIconKey(brandType) && (
                <CredentialStatusToggle
                  variant="switch"
                  providerType={brandType}
                  displayName={row.displayName}
                  disabled={row.identity.disabled}
                  pending={statusPendingIdentityIds?.has(row.identity.id || row.identity.identity) ?? false}
                  readOnly={row.identity.is_deleted}
                  onToggle={(disabled) => onToggleStatus?.(row.identity.id || row.identity.identity, row.identity.identity, disabled)}
                />
              )
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
            badges={badges(row, onSavePriority)}
            metricsTitle={row.identity.stats_reset_at ? t('usage_stats.credentials_stats_since', { time: formatCredentialTimestamp(row.identity.stats_reset_at) ?? row.identity.stats_reset_at }) : undefined}
            metrics={(
              <>
                <MetricPill label={t('usage_stats.credentials_metric_requests')} value={<RequestMetric total={row.totalRequests} success={row.successCount} failure={row.failureCount} />} />
                <MetricPill label={t('usage_stats.credentials_metric_success')} value={<TonePercent value={row.successRate} tone={successRateTone(row.successRate)} />} />
                <MetricPill label={t('usage_stats.credentials_metric_tokens')} value={formatCredentialNumber(row.totalTokens)} />
                <MetricPill label={t('usage_stats.credentials_metric_cache')} value={<TonePercent value={row.cacheReadRate} tone={cacheReadRateTone(row.cacheReadRate)} />} />
              </>
            )}
            health={<CredentialHealthPanel variant="compact" showCompactMeta displayName={row.displayName} health={row.credentialHealth} lastUsedAt={row.identity.last_used_at} windowCacheReadRate={row.windowCacheReadRate} />}
            quota={<CredentialQuotaPanel variant="compact" row={row} quotaUsageMode={quotaUsageMode} timeZone={timeZone} />}
            footerLeft={siwcState && <CredentialSIWCStatus state={siwcState} />}
            actions={(
              <>
                  {onEdit && <CredentialEditButton identityId={row.identity.id} disabled={row.identity.is_deleted} onEdit={() => onEdit(row)} cardTooltip />}
                  <CredentialCardActionTooltip label={t('usage_stats.credentials_quota_refresh_action')}>
                    {(tooltipId) => <button
                      type="button"
                      className={`${styles.credentialRowRefreshButton} ${styles.credentialCardActionButton} ${rowRefreshing ? styles.credentialRowRefreshButtonLoading : ''}`.trim()}
                      onClick={() => void onRefreshQuotaForAuthIndex(row.identity.identity)}
                      disabled={row.identity.is_deleted || rowRefreshing}
                      aria-label={t('usage_stats.credentials_refresh_single', { name: row.displayName })}
                      aria-describedby={tooltipId}
                      aria-busy={rowRefreshing}
                    >
                      {rowRefreshing ? <LoadingSpinner size={15} /> : <IconRefreshCw size={15} />}
                    </button>}
                  </CredentialCardActionTooltip>
                  {resetCredits > 0 && (
                    <QuotaResetAction
                      authIndex={row.identity.identity}
                      resetCredits={resetCredits}
                      disabled={!canResetQuota}
                      loading={row.quotaResetting === true}
                      provider={isClaude ? 'claude' : 'codex'}
                      key={`${row.identity.id}:${row.identity.identity}:${isClaude}`}
                      onConfirm={(grantId, organizationId) => isClaude ? onResetQuotaForAuthIndex(row.identity.identity, grantId, organizationId) : onResetQuotaForAuthIndex(row.identity.identity)}
                    />
                  )}
              </>
            )}
          />
        )
      })}
      </CredentialCardGrid>
      {portal}
  </>
}
