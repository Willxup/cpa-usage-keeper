import { useEffect, useId, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { IconInfo } from '@/components/ui/icons'
import { PortalTooltip, usePortalTooltip } from '@/components/ui/PortalTooltip'
import quotaCostIcon from '@/assets/icons/quota-cost.svg'
import quotaTokenIcon from '@/assets/icons/quota-token.svg'
import styles from './CredentialQuota.module.scss'
import { capitalize } from '../credentialMetrics'
import type { CredentialRow, DisplayQuota } from '../credentialViewModels'
import { ANTIGRAVITY_ARGON_GROUP_KEY, credentialQuotaPanelItems, formatQuotaErrorDisplay, formatQuotaResetDuration, formatQuotaResetLabel, formatQuotaWindowUsageAriaLabel, formatQuotaBillingUsageAriaLabel, formatQuotaBillingUsageText, quotaWindowUsageForMode, type AntigravityQuotaGroupItem } from './quotaPresentation'

export type QuotaUsageMode = 'current' | 'estimated'

export function CredentialQuotaPanel({ row, quotaUsageMode, timeZone, variant = 'full', tooltipEnabled = true }: { row: CredentialRow; quotaUsageMode: QuotaUsageMode; timeZone?: string; variant?: 'full' | 'compact'; tooltipEnabled?: boolean }) {
  const { t } = useTranslation()
  const compact = variant === 'compact'

  // 限额区域按加载、错误、刷新中、无缓存、可展示数据的顺序降级。
  if (row.quotaLoading) {
    return <div className={styles.credentialQuotaStateSlot}><div className={styles.credentialQuotaState}>{t('usage_stats.credentials_quota_loading')}</div></div>
  }
  if (row.quotaError) {
    const errorDisplay = formatQuotaErrorDisplay(row.quotaError)
    return (
      <div className={styles.credentialQuotaStateSlot}>
        <div className={styles.credentialQuotaErrorSummary} title={errorDisplay.title}>
          {errorDisplay.code && <span className={styles.credentialQuotaErrorCode}>{errorDisplay.code}</span>}
          <span className={styles.credentialQuotaErrorMessage}>{errorDisplay.message}</span>
        </div>
      </div>
    )
  }
  if (row.refreshStatus === 'queued' || row.refreshStatus === 'running') {
    return <div className={styles.credentialQuotaStateSlot}><div className={styles.credentialQuotaRefreshStatus}>{t(`usage_stats.credentials_refresh_status_${row.refreshStatus}`)}</div></div>
  }
  if (row.displayQuotas.length === 0) {
    return <div className={styles.credentialQuotaStateSlot}><div className={styles.credentialQuotaState}>{t('usage_stats.credentials_quota_unavailable')}</div></div>
  }

  const items = credentialQuotaPanelItems(row.displayQuotas)
  return (
    <div className={`${styles.credentialQuotaPanel} ${compact ? styles.credentialQuotaCompact : ''}`.trim()} data-credential-quota-variant={variant}>
      <div className={styles.credentialQuotaBars}>
        {/* 只有 canonical Antigravity 组提升为共享标题；其它 provider 继续沿用原始扁平 QuotaBar。 */}
        {items.map((item, index) => item.kind === 'group'
          ? <AntigravityQuotaGroup key={item.renderKey} compact={compact} tooltipEnabled={tooltipEnabled} group={item} quotaUsageMode={quotaUsageMode} timeZone={timeZone} />
          : <QuotaBar key={item.quota.key} tooltipEnabled={tooltipEnabled} fullWidth={compact && !item.tooltipAlignRight && items[index + 1]?.kind !== 'quota'} quota={item.quota} quotaUsageMode={quotaUsageMode} timeZone={timeZone} tooltipAlignRight={item.tooltipAlignRight} />)}
      </div>
    </div>
  )
}

function AntigravityQuotaGroup({ group, quotaUsageMode, timeZone, compact = false, tooltipEnabled = true }: { group: AntigravityQuotaGroupItem; quotaUsageMode: QuotaUsageMode; timeZone?: string; compact?: boolean; tooltipEnabled?: boolean }) {
  return (
    <div className={styles.credentialQuotaGroupBlock} data-quota-group={group.groupKey}>
      <div className={styles.credentialQuotaGroupHeader}>
        <QuotaGroupLabel label={group.groupLabel} description={group.groupKey === ANTIGRAVITY_ARGON_GROUP_KEY ? undefined : group.groupDescription} />
      </div>
      <div className={styles.credentialQuotaGroupBars}>
        {group.quotas.map((quota, index) => (
          <QuotaBar key={quota.key} tooltipEnabled={tooltipEnabled} fullWidth={compact && group.quotas.length % 2 === 1 && index === group.quotas.length - 1} quota={quota} quotaUsageMode={quotaUsageMode} timeZone={timeZone} showGroupMetadata={false} />
        ))}
      </div>
    </div>
  )
}

function QuotaBar({ quota, quotaUsageMode, timeZone, showGroupMetadata = true, tooltipAlignRight = false, tooltipEnabled = true, fullWidth = false }: { quota: DisplayQuota; quotaUsageMode: QuotaUsageMode; timeZone?: string; showGroupMetadata?: boolean; tooltipAlignRight?: boolean; tooltipEnabled?: boolean; fullWidth?: boolean }) {
  const { t } = useTranslation()
  // 条宽使用剩余额度百分比，颜色跟随剩余风险状态从绿到黄到红。
  const percent = quota.barPercent ?? 0
  const width = `${Math.max(0, Math.min(100, percent))}%`
  const percentLabel = quota.barPercent === null ? '' : `${Math.round(quota.barPercent)}%`
  const resetLabel = quota.resetText ? formatQuotaResetLabel(quota.resetText, timeZone) : ''
  const resetDuration = quota.resetText ? formatQuotaResetDuration(quota.resetText) : ''
  const billingUsage = quota.billingUsage
  const windowUsage = billingUsage ? undefined : quotaWindowUsageForMode(quota, quotaUsageMode)

  return (
    <div className={`${styles.credentialQuotaBarBlock} ${tooltipAlignRight ? styles.credentialQuotaBarTooltipRight : ''} ${fullWidth ? styles.credentialQuotaWindowFull : ''}`.trim()} data-quota-key={quota.key}>
      <div className={styles.credentialQuotaBarHeader}>
        <span className={`${styles.credentialQuotaLabelGroup} ${quota.usageBreakdown?.length ? styles.credentialQuotaLabelWithInfo : ''}`.trim()}>
          <span>{quota.label}</span>
          {Boolean(quota.usageBreakdown?.length) && <QuotaUsageBreakdownInfo quota={quota} enabled={tooltipEnabled} />}
        </span>
        {(resetDuration || percentLabel) && (
          <span className={styles.credentialQuotaValueGroup}>
            {resetDuration && (resetLabel
              ? <QuotaResetCountdown duration={resetDuration} resetLabel={resetLabel} enabled={tooltipEnabled} />
              : <span className={styles.credentialQuotaResetDuration}>{resetDuration}</span>)}
            {percentLabel && <strong>{percentLabel}</strong>}
          </span>
        )}
      </div>
      {!(quota.usageBreakdown?.length && quota.barPercent === null) && <div className={styles.credentialQuotaTrack} data-quota-bar="true">
        <span className={`${styles.credentialQuotaFill} ${styles[`credentialQuotaFill${capitalize(quota.status)}`]}`.trim()} style={{ width }} />
      </div>}
      <div className={styles.credentialQuotaMeta}>
        {showGroupMetadata && quota.scope === 'quota_group' && quota.groupLabel && (
          <QuotaGroupLabel label={quota.groupLabel} description={quota.groupDescription} />
        )}
        {billingUsage && (
          <strong className={styles.credentialQuotaWindowUsage} aria-label={formatQuotaBillingUsageAriaLabel(t, billingUsage)}>
            <span className={styles.credentialQuotaUsageMetric}>
              <img src={quotaCostIcon} alt="" aria-hidden="true" />
              <span>{formatQuotaBillingUsageText(billingUsage)}</span>
            </span>
          </strong>
        )}
        {windowUsage && (
          <strong className={styles.credentialQuotaWindowUsage} data-quota-window-usage aria-label={formatQuotaWindowUsageAriaLabel(t, windowUsage)}>
            <span className={styles.credentialQuotaUsageMetric} data-quota-metric="tokens">
              <img src={quotaTokenIcon} alt="" aria-hidden="true" />
              <span>{windowUsage.tokens}</span>
            </span>
            <span className={styles.credentialQuotaUsageMetric} data-quota-metric="cost">
              <img src={quotaCostIcon} alt="" aria-hidden="true" />
              <span>{windowUsage.cost}</span>
            </span>
          </strong>
        )}
      </div>
    </div>
  )
}

function QuotaResetCountdown({ duration, resetLabel, enabled = true }: { duration: string; resetLabel: string; enabled?: boolean }) {
  const { t } = useTranslation()
  const tooltipId = useId()
  const anchorRef = useRef<HTMLButtonElement>(null)
  const { tooltip, showOnMouseEnter, hideOnMouseLeave, showOnFocus, hideOnBlur, dismiss } = usePortalTooltip({ trackAnchor: true })
  const label = t('usage_stats.credentials_quota_resets_at', { time: resetLabel })
  const lines = [label]

  useEffect(() => {
    if (!enabled) dismiss()
  }, [enabled, dismiss])

  useEffect(() => {
    if (!enabled || !tooltip) return
    const closeOutside = (event: PointerEvent) => {
      if (!anchorRef.current?.contains(event.target as Node)) dismiss()
    }
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && dismiss()) {
        event.preventDefault()
        event.stopPropagation()
      }
    }
    document.addEventListener('pointerdown', closeOutside)
    document.addEventListener('keydown', closeOnEscape, true)
    return () => {
      document.removeEventListener('pointerdown', closeOutside)
      document.removeEventListener('keydown', closeOnEscape, true)
    }
  }, [enabled, tooltip, dismiss])

  return <>
    <button
      ref={anchorRef}
      type="button"
      className={`${styles.credentialQuotaResetDuration} ${styles.credentialQuotaCountdown}`}
      data-quota-reset-countdown="true"
      aria-label={label}
      aria-describedby={enabled && tooltip ? tooltipId : undefined}
      onMouseEnter={(event) => { if (enabled) showOnMouseEnter(lines, event.currentTarget) }}
      onMouseLeave={(event) => hideOnMouseLeave(event.currentTarget)}
      onFocus={(event) => { if (enabled) showOnFocus(lines, event.currentTarget) }}
      onBlur={(event) => hideOnBlur(event.currentTarget)}
      onClick={(event) => { if (enabled) showOnFocus(lines, event.currentTarget) }}
    >{duration}</button>
    {/* 文本始终使用当前额度快照；悬停期间刷新不会留下旧重置时间。 */}
    <PortalTooltip tooltip={enabled && tooltip ? { ...tooltip, lines } : null} id={tooltipId} />
  </>
}

function QuotaUsageBreakdownInfo({ quota, enabled = true }: { quota: DisplayQuota; enabled?: boolean }) {
  const { t } = useTranslation()
  const tooltipId = useId()
  const anchorRef = useRef<HTMLButtonElement>(null)
  const { tooltip, showOnMouseEnter, hideOnMouseLeave, showOnFocus, hideOnBlur, dismiss } = usePortalTooltip()
  const formatUsed = (value: number | null | undefined) => typeof value === 'number' && Number.isFinite(value)
    ? `${Math.round(Math.max(0, Math.min(100, value)) * 100) / 100}%`
    : t('usage_stats.credentials_quota_usage_unknown')
  const lines = [
    t('usage_stats.credentials_quota_shared_usage'),
    `${t('usage_stats.credentials_quota_total_used')}: ${formatUsed(quota.percent)}`,
    ...(quota.usageBreakdown ?? []).map((item) => `${item.product}: ${formatUsed(item.usedPercent)}`),
    t('usage_stats.credentials_quota_shared_usage_note'),
  ]

  useEffect(() => {
    if (!enabled) dismiss()
  }, [enabled, dismiss])

  useEffect(() => {
    if (!enabled || !tooltip) return
    const closeOutside = (event: PointerEvent) => {
      if (!anchorRef.current?.contains(event.target as Node)) dismiss()
    }
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && dismiss()) {
        event.preventDefault()
        event.stopPropagation()
      }
    }
    document.addEventListener('pointerdown', closeOutside)
    document.addEventListener('keydown', closeOnEscape, true)
    return () => {
      document.removeEventListener('pointerdown', closeOutside)
      document.removeEventListener('keydown', closeOnEscape, true)
    }
  }, [enabled, tooltip, dismiss])

  return <>
    <button
      ref={anchorRef}
      type="button"
      className={styles.credentialQuotaInfoButton}
      aria-label={t('usage_stats.credentials_quota_shared_usage')}
      aria-describedby={enabled && tooltip ? tooltipId : undefined}
      onMouseEnter={(event) => { if (enabled) showOnMouseEnter(lines, event.currentTarget) }}
      onMouseLeave={(event) => hideOnMouseLeave(event.currentTarget)}
      onFocus={(event) => { if (enabled) showOnFocus(lines, event.currentTarget) }}
      onBlur={(event) => hideOnBlur(event.currentTarget)}
      onClick={(event) => { if (enabled) showOnFocus(lines, event.currentTarget) }}
    >
      <IconInfo width="1em" height="1em" />
    </button>
    {/* 保留浮层位置，文本始终来自当前缓存快照，避免后台刷新后仍显示旧用量。 */}
    <PortalTooltip tooltip={enabled && tooltip ? { ...tooltip, lines } : null} id={tooltipId} />
  </>
}

function QuotaGroupLabel({ label, description }: { label: string; description?: string }) {
  const tooltipId = useId()
  const hasDescription = Boolean(description?.trim())
  return (
    <span
      className={styles.credentialQuotaGroupTooltipTarget}
      tabIndex={hasDescription ? 0 : undefined}
      aria-describedby={hasDescription ? tooltipId : undefined}
    >
      <span className={styles.credentialQuotaGroupLabel}>{label}</span>
      {hasDescription && (
        <span id={tooltipId} className={styles.credentialQuotaGroupTooltip} role="tooltip">
          {description}
        </span>
      )}
    </span>
  )
}

