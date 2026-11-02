import { useId } from 'react'
import { useTranslation } from 'react-i18next'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { ProviderBrandIcon } from '@/components/ProviderBrandIcon'
import { IconInfo } from '@/components/ui/icons'
import styles from './CredentialSections.module.scss'

// 只有这六类 AI 供应商能用 excluded-models 的精确 "*" 整条停用，OpenAI 兼容不支持。
const STATUS_TOGGLE_PROVIDER_TYPES = new Set([
  'codex',
  'xai',
  'gemini',
  'gemini-interactions',
  'claude',
  'vertex',
])

export function isCredentialStatusToggleSupported(providerType: string | null | undefined): boolean {
  const normalized = providerType?.trim().toLowerCase() ?? ''
  return STATUS_TOGGLE_PROVIDER_TYPES.has(normalized)
}

export interface CredentialStatusUnsupportedIconProps {
  providerType: string
  displayName: string
  variant?: 'brand' | 'info'
}

/**
 * 不支持整条启停的凭证（如 OpenAI 兼容类型）使用的静态图标。
 * 复用与开关相同的行内 tooltip 机制，让悬停、键盘聚焦和触摸都能读到同一句说明。
 */
export function CredentialStatusUnsupportedIcon({ providerType, displayName, variant = 'brand' }: CredentialStatusUnsupportedIconProps) {
  const { t } = useTranslation()
  const tooltipId = useId()

  return (
    <span className={`${styles.credentialStatusToggleWrap} ${variant === 'info' ? styles.credentialStatusCompactWrap : ''}`.trim()}>
      <span
        className={`${styles.credentialStatusToggleUnsupported} ${variant === 'info' ? styles.credentialStatusInfo : ''}`.trim()}
        data-credential-status-unsupported="true"
        // 可聚焦才能让键盘和触摸用户拿到说明；它不可点击，也不触发任何请求。
        tabIndex={0}
        role="img"
        aria-label={displayName}
        // 说明节点常驻 DOM，聚焦当刻即可被屏幕阅读器计算为可访问描述。
        aria-describedby={tooltipId}
      >
        {variant === 'info' ? <IconInfo width={18} height={18} /> : <ProviderBrandIcon providerType={providerType} size={30} />}
      </span>
      <span id={tooltipId} role="tooltip" data-credential-status-tooltip="true" className={styles.credentialStatusToggleTooltip}>
        {t('usage_stats.credentials_status_unsupported_tooltip')}
      </span>
    </span>
  )
}

export interface CredentialStatusToggleProps {
  providerType: string
  displayName: string
  disabled: boolean
  pending?: boolean
  /** 已删除的历史身份不能再改上游状态。 */
  readOnly?: boolean
  variant?: 'brand' | 'switch'
  onToggle: (disabled: boolean) => void
}

/**
 * 卡片右上角的小开关复用既有请求、防重入和说明；品牌变体保留给原有调用方。
 */
export function CredentialStatusToggle({ providerType, displayName, disabled, pending = false, readOnly = false, variant = 'brand', onToggle }: CredentialStatusToggleProps) {
  const { t } = useTranslation()
  const tooltipId = useId()

  if (readOnly) {
    if (variant === 'switch') {
      return (
        <span className={styles.credentialStatusReadOnly} role="img" aria-label={`${displayName}: ${t(disabled ? 'usage_stats.credentials_detail_disabled' : 'usage_stats.credentials_detail_enabled')}`}>
          <span className={`${styles.credentialStatusSwitchTrack} ${disabled ? styles.credentialStatusSwitchTrackDisabled : ''}`.trim()} aria-hidden="true"><span /></span>
        </span>
      )
    }
    return <ProviderBrandIcon providerType={providerType} size={30} ariaLabel={displayName} />
  }

  // 提示语直接说明点击后的效果；无障碍名称使用凭证 displayName，状态由 aria-pressed 表达。
  const tooltipText = pending
    ? t('usage_stats.credentials_status_updating')
    : t(disabled ? 'usage_stats.credentials_status_enable_tooltip' : 'usage_stats.credentials_status_disable_tooltip')
  const toggleClassName = [
    variant === 'switch' ? styles.credentialStatusSwitch : styles.credentialStatusToggle,
    disabled && variant === 'brand' ? styles.credentialStatusToggleDisabled : '',
    pending ? styles.credentialStatusTogglePending : '',
  ].filter(Boolean).join(' ')

  return (
    <span className={`${styles.credentialStatusToggleWrap} ${variant === 'switch' ? styles.credentialStatusCompactWrap : ''}`.trim()}>
      <button
        type="button"
        className={toggleClassName}
        data-credential-status-toggle="true"
        data-credential-status-disabled={disabled ? 'true' : 'false'}
        // 请求进行中沿用 aria-disabled 而不是原生 disabled：原生禁用会把键盘焦点弹回 body。
        onClick={() => {
          if (pending) {
            return
          }
          onToggle(!disabled)
        }}
        aria-disabled={pending || undefined}
        aria-busy={pending || undefined}
        role={variant === 'switch' ? 'switch' : undefined}
        aria-checked={variant === 'switch' ? !disabled : undefined}
        aria-pressed={variant === 'brand' ? !disabled : undefined}
        aria-label={displayName}
        // 说明节点常驻 DOM，聚焦当刻即可被屏幕阅读器计算为可访问描述。
        aria-describedby={tooltipId}
      >
        {variant === 'switch' ? (
          <span className={`${styles.credentialStatusSwitchTrack} ${disabled ? styles.credentialStatusSwitchTrackDisabled : ''}`.trim()} aria-hidden="true"><span /></span>
        ) : <ProviderBrandIcon providerType={providerType} size={30} />}
        {pending && (
          <span className={styles.credentialStatusToggleSpinner} aria-hidden="true">
            <LoadingSpinner size={14} />
          </span>
        )}
      </button>
      {variant === 'switch' && disabled && <span className={styles.credentialStatusDisabledLabel}>{t('usage_stats.credentials_detail_disabled')}</span>}
      {/* 数据属性用于把开关提示与其它行内 tooltip（如过期时间）区分开。 */}
      <span id={tooltipId} role="tooltip" data-credential-status-tooltip="true" className={styles.credentialStatusToggleTooltip}>
        {tooltipText}
      </span>
    </span>
  )
}
