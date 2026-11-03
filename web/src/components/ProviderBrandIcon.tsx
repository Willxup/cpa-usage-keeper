import antigravityIcon from '@/assets/icons/antigravity.svg'
import claudeIcon from '@/assets/icons/claude.svg'
import codexIcon from '@/assets/icons/codex.svg'
import deepseekIcon from '@/assets/icons/deepseek.svg'
import devinIcon from '@/assets/icons/devin.svg'
import geminiIcon from '@/assets/icons/gemini.svg'
import grokIcon from '@/assets/icons/grok.svg'
import kimiIcon from '@/assets/icons/kimi.svg'
import metaIcon from '@/assets/icons/meta.svg'
import museIcon from '@/assets/icons/muse.svg'
import openaiIcon from '@/assets/icons/openai.svg'
import qwenIcon from '@/assets/icons/qwen.svg'
import vertexIcon from '@/assets/icons/vertex.svg'
import zaiIcon from '@/assets/icons/zai.svg'
import styles from './ProviderBrandIcon.module.scss'

export const PROVIDER_BRAND_ICON_KEYS = [
  'antigravity',
  'claude',
  'codex',
  'deepseek',
  'devin',
  'gemini',
  'kimi',
  'meta',
  'muse',
  'openai',
  'qwen',
  'vertex',
  'xai',
  'zai',
] as const

export type ProviderBrandIconKey = typeof PROVIDER_BRAND_ICON_KEYS[number]

export interface ProviderBrandIconProps {
  providerType: string | null | undefined
  size: number | string
  ariaLabel?: string
  className?: string
}

// 映射明确的供应商品牌及别名；模型页先解析模型家族，再与其他页面共用这些图标。
const providerBrandIconKeyByType: Readonly<Record<string, ProviderBrandIconKey>> = {
  antigravity: 'antigravity',
  claude: 'claude',
  codex: 'codex',
  deepseek: 'deepseek',
  devin: 'devin',
  gemini: 'gemini',
  'gemini-cli': 'gemini',
  'gemini-interactions': 'gemini',
  kimi: 'kimi',
  'kimi-ai': 'kimi',
  'kimi.ai': 'kimi',
  'kimi.com': 'kimi',
  meta: 'meta',
  muse: 'muse',
  openai: 'openai',
  qwen: 'qwen',
  vertex: 'vertex',
  xai: 'xai',
  zai: 'zai',
  'z.ai': 'zai',
  'z-ai': 'zai',
  zhipu: 'zai',
  glm: 'zai',
  chatglm: 'zai',
}

// 品牌资源取自 Lobe Icons；统一复用其 Avatar 圆形容器、背景色与缩放比例。
const providerBrandIconUrlByKey: Readonly<Record<ProviderBrandIconKey, string>> = {
  antigravity: antigravityIcon,
  claude: claudeIcon,
  codex: codexIcon,
  deepseek: deepseekIcon,
  devin: devinIcon,
  gemini: geminiIcon,
  kimi: kimiIcon,
  meta: metaIcon,
  muse: museIcon,
  openai: openaiIcon,
  qwen: qwenIcon,
  vertex: vertexIcon,
  xai: grokIcon,
  zai: zaiIcon,
}

export function providerBrandIconKey(providerType: string | null | undefined): ProviderBrandIconKey | undefined {
  const normalized = providerType?.trim().toLowerCase()
  return normalized && Object.hasOwn(providerBrandIconKeyByType, normalized) ? providerBrandIconKeyByType[normalized] : undefined
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
