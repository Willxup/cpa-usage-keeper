import antigravityIcon from '@/assets/icons/antigravity.svg'
import claudeIcon from '@/assets/icons/claude.svg'
import codexIcon from '@/assets/icons/codex.svg'
import geminiIcon from '@/assets/icons/gemini.svg'
import grokIcon from '@/assets/icons/grok.svg'
import kimiIcon from '@/assets/icons/kimi.svg'
import openaiIcon from '@/assets/icons/openai.svg'
import vertexIcon from '@/assets/icons/vertex.svg'
import styles from './ProviderBrandIcon.module.scss'

export const PROVIDER_BRAND_ICON_KEYS = [
  'antigravity',
  'claude',
  'codex',
  'gemini',
  'kimi',
  'openai',
  'vertex',
  'xai',
] as const

export type ProviderBrandIconKey = typeof PROVIDER_BRAND_ICON_KEYS[number]

export interface ProviderBrandIconProps {
  providerType: string | null | undefined
  size: number | string
  ariaLabel?: string
  className?: string
}

// 仅映射 CPA 能稳定提供 identity 的类型；Gemini CLI 兼容行与 Interactions 复用 Gemini 品牌。
const providerBrandIconKeyByType: Readonly<Record<string, ProviderBrandIconKey>> = {
  antigravity: 'antigravity',
  claude: 'claude',
  codex: 'codex',
  gemini: 'gemini',
  'gemini-cli': 'gemini',
  'gemini-interactions': 'gemini',
  kimi: 'kimi',
  openai: 'openai',
  vertex: 'vertex',
  xai: 'xai',
}

// 品牌资源取自 Lobe Icons；统一复用其 Avatar 圆形容器、背景色与缩放比例。
const providerBrandIconUrlByKey: Readonly<Record<ProviderBrandIconKey, string>> = {
  antigravity: antigravityIcon,
  claude: claudeIcon,
  codex: codexIcon,
  gemini: geminiIcon,
  kimi: kimiIcon,
  openai: openaiIcon,
  vertex: vertexIcon,
  xai: grokIcon,
}

export function providerBrandIconKey(providerType: string | null | undefined): ProviderBrandIconKey | undefined {
  const normalized = providerType?.trim().toLowerCase()
  return normalized ? providerBrandIconKeyByType[normalized] : undefined
}

export function ProviderBrandIcon({ providerType, size, ariaLabel, className }: ProviderBrandIconProps) {
  const iconKey = providerBrandIconKey(providerType)
  if (!iconKey) {
    return null
  }

  const rootClassName = `${styles.providerBrandIcon} ${styles.providerBrandIconAvatar} ${className ?? ''}`.trim()
  const accessibleLabel = ariaLabel?.trim() || undefined

  return (
    <span
      className={rootClassName}
      data-provider-brand-icon={iconKey}
      data-provider-brand-icon-tone="avatar"
      style={{ width: size, height: size }}
      role={accessibleLabel ? 'img' : undefined}
      aria-label={accessibleLabel}
      aria-hidden={accessibleLabel ? undefined : true}
    >
      <img className={styles.providerBrandIconAsset} src={providerBrandIconUrlByKey[iconKey]} alt="" />
    </span>
  )
}
