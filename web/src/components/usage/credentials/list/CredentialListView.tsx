import { useTranslation } from 'react-i18next'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { IconRefreshCw } from '@/components/ui/icons'
import { ProviderBrandIcon, providerBrandIconKey } from '@/components/ProviderBrandIcon'
import styles from '../CredentialSections.module.scss'
import listStyles from './CredentialList.module.scss'
import { credentialSIWCState, formatCredentialTimestamp } from '../credentialViewModels'
import { credentialBrandOverride } from '../credentialBrand'
import { CredentialSIWCStatus } from '../CredentialSIWCStatus'
import type { CredentialViewProps } from '../credentialViewProps'
import type { CredentialListContentMode } from '../credentialListPreferences'
import { CredentialAliasEditor } from '../CredentialAliasEditor'
import { CredentialHealthPanel } from '../CredentialHealthPanel'
import { CredentialStatusToggle } from '../CredentialStatusToggle'
import { RequestMetric, TonePercent, cacheReadRateTone, formatCredentialNumber, successRateTone } from '../credentialMetrics'
import { useCredentialExpiryTooltip } from '../CredentialBadges'
import { CredentialQuotaPanel } from '../quota/CredentialQuotaPanel'
import { QuotaResetAction } from '../quota/QuotaResetAction'
import { credentialResetCapabilities, isRowRefreshing } from '../quota/credentialQuotaCapabilities'
import { CredentialRowShell, CredentialTableHeader, ListMetric } from './CredentialListShell'

export function CredentialListView({ rows, timeZone, quotaUsageMode, contentMode, onRefreshQuotaForAuthIndex, onResetQuotaForAuthIndex, onEdit, onOpenDetails, statusPendingIdentityIds, onToggleStatus, onSavePriority }: CredentialViewProps & { contentMode: CredentialListContentMode }) {
  const { t } = useTranslation()
  const { badges, portal } = useCredentialExpiryTooltip(rows)
  const showHealthMode = contentMode === 'health'
  return <>
      {rows.length > 0 && (
        <CredentialTableHeader
          rowClassName={listStyles.credentialsRow}
          nameLabel={t('usage_stats.credentials_column_name')}
          totalRequestsLabel={t('usage_stats.total_requests')}
          successRateLabel={t('usage_stats.success_rate')}
          totalTokensLabel={t('usage_stats.total_tokens')}
          cacheReadRateLabel={t('usage_stats.cache_rate')}
          sideLabel={showHealthMode ? t('usage_stats.credentials_column_health') : t('usage_stats.credentials_column_quota')}
        />
      )}
      {rows.map((row) => {
        const rowRefreshing = isRowRefreshing(row)
        const { isClaude, resetCredits, canResetQuota } = credentialResetCapabilities(row)
        const rowKey = row.identity.id || row.identity.identity
        const siwcState = credentialSIWCState(row.identity)
        const brandType = credentialBrandOverride(row.identity.type)?.iconType ?? row.identity.type
        return (
          <CredentialRowShell
            key={rowKey}
            icon={(
              // 凭证 type 直接来自 CPA，可能是不认识的类型；没有品牌图标的类型不给开关，
              // 否则左侧图标槽位会变成一个看不见却可点、可 Tab 的按钮。
              providerBrandIconKey(brandType) ? (
                <CredentialStatusToggle
                  providerType={brandType}
                  displayName={row.displayName}
                  disabled={row.identity.disabled}
                  pending={statusPendingIdentityIds?.has(row.identity.id || row.identity.identity) ?? false}
                  readOnly={row.identity.is_deleted}
                  onToggle={(disabled) => onToggleStatus?.(row.identity.id || row.identity.identity, row.identity.identity, disabled)}
                />
              ) : (
                <ProviderBrandIcon providerType={brandType} size={30} ariaLabel={row.typeLabel} />
              )
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
            ) : <span>{row.displayName}</span>}
            subtitle={badges(row, onSavePriority)}
            status={siwcState && <CredentialSIWCStatus state={siwcState} className={listStyles.credentialListSIWCStatus} />}
            identityId={rowKey}
            metricsTitle={row.identity.stats_reset_at ? t('usage_stats.credentials_stats_since', { time: formatCredentialTimestamp(row.identity.stats_reset_at) ?? row.identity.stats_reset_at }) : undefined}
            metrics={(
              <>
                <ListMetric value={<RequestMetric total={row.totalRequests} success={row.successCount} failure={row.failureCount} />} />
                <ListMetric value={<TonePercent value={row.successRate} tone={successRateTone(row.successRate)} />} />
                <ListMetric value={formatCredentialNumber(row.totalTokens)} />
                <ListMetric value={<TonePercent value={row.cacheReadRate} tone={cacheReadRateTone(row.cacheReadRate)} />} />
              </>
            )}
            rowClassName={listStyles.credentialsRow}
            side={showHealthMode ? (
              <CredentialHealthPanel displayName={row.displayName} health={row.credentialHealth} lastUsedAt={row.identity.last_used_at} statsUpdatedAt={row.identity.stats_updated_at} windowCacheReadRate={row.windowCacheReadRate} />
            ) : (
              <div className={listStyles.credentialQuotaSideWithAction}>
                <CredentialQuotaPanel row={row} quotaUsageMode={quotaUsageMode} timeZone={timeZone} />
                <div className={listStyles.credentialQuotaActionStack}>
                  {/* reset 按钮只在官方缓存给出可用次数时展示；refresh 始终保留在右侧列居中位置。 */}
                  {resetCredits > 0 && (
                    <QuotaResetAction
                      variant="list"
                      authIndex={row.identity.identity}
                      resetCredits={resetCredits}
                      disabled={!canResetQuota}
                      loading={row.quotaResetting === true}
                      provider={isClaude ? 'claude' : 'codex'}
                      key={`${row.identity.id}:${row.identity.identity}:${isClaude}`}
                      onConfirm={(grantId, organizationId) => isClaude ? onResetQuotaForAuthIndex(row.identity.identity, grantId, organizationId) : onResetQuotaForAuthIndex(row.identity.identity)}
                    />
                  )}
                  <button
                    type="button"
                    className={`${styles.credentialRowRefreshButton} ${rowRefreshing ? styles.credentialRowRefreshButtonLoading : ''}`.trim()}
                    onClick={() => void onRefreshQuotaForAuthIndex(row.identity.identity)}
                    disabled={row.identity.is_deleted || rowRefreshing}
                    aria-label={t('usage_stats.credentials_refresh_single', { name: row.displayName })}
                    aria-busy={rowRefreshing}
                  >
                    {rowRefreshing ? <LoadingSpinner size={13} /> : <IconRefreshCw size={13} />}
                  </button>
                </div>
              </div>
            )}
          />
        )
      })}
      {portal}
  </>
}
