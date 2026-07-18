import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { RelayBalance } from '@/lib/types'
import styles from './CredentialSections.module.scss'
import type { AiProviderCredentialRow } from './credentialViewModels'
import { buildRelayDisplayQuotas, type DisplayQuota } from './credentialViewModels'
import { credentialToneClassName, formatQuotaResetDuration } from './CredentialSectionShell'

// AiProviderUsagePanel 在凭证行 side（Health 下方）展示中转商用量。
// 用量窗口以带列名的紧凑表格展示（窗口/用量/进度/重置），含 used/limit 数量与剩余百分比、重置倒计时。
// 平台标识做成可点击徽标，点击弹出菜单切换平台覆盖（替代旧的下拉框）。

const PLATFORM_OPTIONS: Array<{ value: string; labelKey: string }> = [
  { value: 'auto', labelKey: 'usage_stats.relay_platform_auto' },
  { value: 'glm', labelKey: 'usage_stats.relay_platform_glm' },
  { value: 'minimax', labelKey: 'usage_stats.relay_platform_minimax' },
  { value: 'kimi', labelKey: 'usage_stats.relay_platform_kimi' },
  { value: 'deepseek', labelKey: 'usage_stats.relay_platform_deepseek' },
  { value: 'none', labelKey: 'usage_stats.relay_platform_none' },
]

interface AiProviderUsagePanelProps {
  row: AiProviderCredentialRow
  onSetPlatform: (identityId: string, platform: string) => void
}

export function AiProviderUsagePanel({ row, onSetPlatform }: AiProviderUsagePanelProps) {
  const { t } = useTranslation()
  const item = row.relayUsageResult
  const result = item?.result
  const platform = row.relayPlatform
  const quotas = useMemo(() => (result ? buildRelayDisplayQuotas(result) : []), [result])
  const balance = result?.balance

  return (
    <div className={styles.relayUsagePanel}>
      <div className={styles.relayUsageHeader}>
        <PlatformSwitcher platform={platform} onSelect={(value) => onSetPlatform(row.identity.id, value)} />
      </div>

      {result?.error && (
        <div className={styles.credentialQuotaErrorSummary} title={result.error}>
          <span className={styles.credentialQuotaErrorMessage}>{result.error}</span>
        </div>
      )}

      {balance && <RelayBalanceBlock balance={balance} />}

      {quotas.length > 0 && (
        <div className={styles.relayUsageTable}>
          {quotas.map((quota) => (
            <RelayQuotaCells key={quota.key} quota={quota} />
          ))}
        </div>
      )}

      {!result && (
        <div className={styles.relayUsageState}>{t('usage_stats.relay_usage_unsupported')}</div>
      )}
    </div>
  )
}

// PlatformSwitcher 把平台标识做成可点击徽标，点击弹出菜单覆盖平台（auto = 按域名自动识别）。
function PlatformSwitcher({ platform, onSelect }: { platform?: string; onSelect: (value: string) => void }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement | null>(null)
  useEffect(() => {
    if (!open) return
    const handleOutsideClick = (event: MouseEvent) => {
      if (ref.current && !ref.current.contains(event.target as Node)) setOpen(false)
    }
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', handleOutsideClick)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('mousedown', handleOutsideClick)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [open])
  const currentLabel = platform && platform !== 'none'
    ? t(`usage_stats.relay_platform_${platform}`)
    : t('usage_stats.relay_platform_unknown')
  return (
    <div className={styles.relayPlatformSwitcher} ref={ref}>
      <button
        type="button"
        className={styles.relayPlatformBadge}
        onClick={() => setOpen((prev) => !prev)}
        aria-haspopup="menu"
        aria-expanded={open}
      >
        {currentLabel}
        <span className={styles.relayPlatformBadgeCaret} aria-hidden="true">▾</span>
      </button>
      {open && (
        <div className={styles.relayPlatformMenu} role="menu">
          {PLATFORM_OPTIONS.map((option) => (
            <button
              key={option.value}
              type="button"
              role="menuitemradio"
              aria-checked={platform === option.value}
              className={platform === option.value ? styles.relayPlatformMenuItemActive : undefined}
              onClick={() => {
                onSelect(option.value)
                setOpen(false)
              }}
            >
              {t(option.labelKey)}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

// RelayQuotaCells 把一个窗口的 4 个单元格作为 Fragment 直接放进共享 grid，
// 多个窗口的进度条列宽由同一个 grid 统一分配，天然对齐。
function RelayQuotaCells({ quota }: { quota: DisplayQuota }) {
  const { t } = useTranslation()
  const percent = quota.percent
  const width = `${Math.max(0, Math.min(100, quota.barPercent ?? 0))}%`
  const rawPercent = quota.percent !== null && quota.percent !== undefined
    ? (quota.percentKind === 'used' ? quota.percent : 100 - quota.percent)
    : null
  const percentLabel = rawPercent === null ? '—' : `${Math.round(rawPercent)}%`
  const usageLabel = formatUsedLimit(quota)
  const resetLabel = quota.resetText ? formatQuotaResetDuration(quota.resetText, t) : '—'
  const fillClassName = `${styles.credentialQuotaFill} ${credentialToneClassName('credentialQuotaFill', quota.status)}`.trim()
  // 窗口名优先取 i18n；未配置的 key 回退到后端给的原 label（多为英文）。
  const labelText = quota.translationKey
    ? t(`usage_stats.relay_window_${quota.translationKey}`)
    : quota.label
  return (
    <Fragment>
      <span className={styles.relayUsageCellLabel}>{labelText}</span>
      <span className={styles.relayUsageCellUsage}>{usageLabel}</span>
      <span className={styles.relayUsageCellBar}>
        <span
          className={styles.credentialQuotaTrack}
          role="progressbar"
          aria-label={quota.label}
          aria-valuenow={quota.barPercent ?? undefined}
          aria-valuemin={0}
          aria-valuemax={100}
        >
          <span className={fillClassName} style={{ width }} />
        </span>
        <span className={styles.relayUsageCellPercent}>{percentLabel}</span>
      </span>
      <span className={styles.relayUsageCellReset}>{resetLabel}</span>
    </Fragment>
  )
}

function RelayBalanceBlock({ balance }: { balance: RelayBalance }) {
  const { t } = useTranslation()
  const currency = balance.currency || 'CNY'
  const details: Array<{ key: string; label: string; value: string }> = []
  if (balance.granted && balance.granted > 0) {
    details.push({ key: 'granted', label: t('usage_stats.relay_usage_balance_granted'), value: formatRelayAmount(balance.granted, currency) })
  }
  if (balance.toppedUp && balance.toppedUp > 0) {
    details.push({ key: 'toppedUp', label: t('usage_stats.relay_usage_balance_topped_up'), value: formatRelayAmount(balance.toppedUp, currency) })
  }
  return (
    <div className={styles.relayBalanceBlock}>
      <div className={styles.relayBalanceHead}>
        <span>{t('usage_stats.relay_usage_balance')}</span>
        <strong>{formatRelayAmount(balance.available, currency)}</strong>
      </div>
      {details.length > 0 && (
        <div className={styles.relayBalanceDetails}>
          {details.map((detail) => (
            <span key={detail.key} className={styles.relayBalanceDetailItem}>
              <span>{detail.label}</span>
              <strong>{detail.value}</strong>
            </span>
          ))}
        </div>
      )}
    </div>
  )
}

function formatUsedLimit(quota: DisplayQuota): string {
  if (quota.used != null && quota.limit != null && quota.limit > 0) {
    return `${formatCompactQuantity(quota.used)}/${formatCompactQuantity(quota.limit)}`
  }
  // GLM 的 5h/weekly 只回 percentage、不回绝对值（used=0,limit=0），此时显示 — 而非误导性的 0。
  if (quota.used != null && quota.used > 0) return formatCompactQuantity(quota.used)
  if (quota.remaining != null && quota.remaining > 0) return formatCompactQuantity(quota.remaining)
  return '—'
}

// formatCompactQuantity 用 Intl 紧凑记数法：120000 -> "120K"，1234567 -> "1.2M"。
const compactQuantityFormatter = new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 1 })
function formatCompactQuantity(value: number): string {
  return Number.isFinite(value) ? compactQuantityFormatter.format(value) : '0'
}

const relayAmountFormatters = new Map<string, Intl.NumberFormat>()
function getRelayAmountFormatter(currency: string): Intl.NumberFormat {
  let formatter = relayAmountFormatters.get(currency)
  if (!formatter) {
    formatter = new Intl.NumberFormat('zh-CN', { style: 'currency', currency, minimumFractionDigits: 2, maximumFractionDigits: 2 })
    relayAmountFormatters.set(currency, formatter)
  }
  return formatter
}

function formatRelayAmount(value: number, currency: string): string {
  const amount = Number.isFinite(value) ? value : 0
  try {
    return getRelayAmountFormatter(currency).format(amount)
  } catch {
    return `${amount.toFixed(2)} ${currency}`
  }
}
