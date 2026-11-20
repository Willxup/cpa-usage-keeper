import type { UsageSubscriptionInfo } from '@/lib/types'

export type SubscriptionBadgeKind =
  | 'codex-free'
  | 'codex-plus'
  | 'codex-team'
  | 'codex-pro10x'
  | 'codex-pro20x'
  | 'codex-pro50x'
  | 'codex-business'
  | 'codex-go'
  | 'codex-edu'
  | 'codex-enterprise'
  | 'claude-free'
  | 'claude-pro'
  | 'claude-max'
  | 'claude-max5x'
  | 'claude-max20x'
  | 'claude-team'
  | 'antigravity-free'
  | 'antigravity-pro'
  | 'antigravity-ultra-lite'
  | 'antigravity-ultra'

export type SubscriptionBadgeModel = {
  kind: SubscriptionBadgeKind
  labelKey?: string
  fallbackLabel?: string
}

const CODEX_PRESENTATIONS = new Map<string, Omit<SubscriptionBadgeModel, 'fallbackLabel'>>([
  ['free', { kind: 'codex-free', labelKey: 'usage_stats.credentials_subscription_codex_free' }],
  ['plus', { kind: 'codex-plus', labelKey: 'usage_stats.credentials_subscription_codex_plus' }],
  ['team', { kind: 'codex-team', labelKey: 'usage_stats.credentials_subscription_codex_team' }],
  ['pro-5x', { kind: 'codex-pro10x', labelKey: 'usage_stats.credentials_subscription_codex_pro_10x' }],
  ['pro-10x', { kind: 'codex-pro10x', labelKey: 'usage_stats.credentials_subscription_codex_pro_10x' }],
  ['pro-50x', { kind: 'codex-pro50x', labelKey: 'usage_stats.credentials_subscription_codex_pro_50x' }],
  ['business', { kind: 'codex-business', labelKey: 'usage_stats.credentials_subscription_codex_business' }],
  ['go', { kind: 'codex-go', labelKey: 'usage_stats.credentials_subscription_codex_go' }],
  ['edu', { kind: 'codex-edu', labelKey: 'usage_stats.credentials_subscription_codex_edu' }],
  ['pro-20x', { kind: 'codex-pro20x', labelKey: 'usage_stats.credentials_subscription_codex_pro_20x' }],
  ['enterprise', { kind: 'codex-enterprise', labelKey: 'usage_stats.credentials_subscription_codex_enterprise' }],
])

const CLAUDE_PRESENTATIONS = new Map<string, Omit<SubscriptionBadgeModel, 'fallbackLabel'>>([
  ['free', { kind: 'claude-free', labelKey: 'usage_stats.credentials_subscription_claude_free' }],
  ['pro', { kind: 'claude-pro', labelKey: 'usage_stats.credentials_subscription_claude_pro' }],
  ['max', { kind: 'claude-max', labelKey: 'usage_stats.credentials_subscription_claude_max' }],
  ['max-5x', { kind: 'claude-max5x', labelKey: 'usage_stats.credentials_subscription_claude_max_5x' }],
  ['max-20x', { kind: 'claude-max20x', labelKey: 'usage_stats.credentials_subscription_claude_max_20x' }],
  ['team', { kind: 'claude-team', labelKey: 'usage_stats.credentials_subscription_claude_team' }],
])

const ANTIGRAVITY_PRESENTATIONS = new Map<string, Omit<SubscriptionBadgeModel, 'fallbackLabel'>>([
  ['free', { kind: 'antigravity-free', labelKey: 'usage_stats.credentials_subscription_antigravity_free' }],
  ['pro', { kind: 'antigravity-pro', labelKey: 'usage_stats.credentials_subscription_antigravity_pro' }],
  ['ultra-lite', { kind: 'antigravity-ultra-lite', labelKey: 'usage_stats.credentials_subscription_antigravity_ultra_lite' }],
  ['ultra', { kind: 'antigravity-ultra', labelKey: 'usage_stats.credentials_subscription_antigravity_ultra' }],
])

// Muse 复用已有动效：Codex Pro 10x 仍是原金色，Pro 20x 使用原 Enterprise 淡紫色。
const META_PRESENTATIONS = new Map<string, Omit<SubscriptionBadgeModel, 'fallbackLabel'>>([
  ['everyday usage', { kind: 'codex-plus', labelKey: 'usage_stats.credentials_subscription_meta_everyday' }],
  ['high usage', { kind: 'codex-plus', labelKey: 'usage_stats.credentials_subscription_meta_high' }],
  ['power usage', { kind: 'codex-pro10x', labelKey: 'usage_stats.credentials_subscription_meta_power' }],
  ['maximum usage', { kind: 'codex-pro20x', labelKey: 'usage_stats.credentials_subscription_meta_maximum' }],
])

const PRESENTATIONS_BY_PROVIDER = new Map([
  ['meta', META_PRESENTATIONS],
  ['codex', CODEX_PRESENTATIONS],
  ['claude', CLAUDE_PRESENTATIONS],
  ['antigravity', ANTIGRAVITY_PRESENTATIONS],
])

export function resolveCredentialSubscriptionBadge(subscription?: UsageSubscriptionInfo): SubscriptionBadgeModel | undefined {
  const provider = subscription?.provider.trim().toLowerCase()
  const displayPlan = subscription?.plan.trim()
  if (!provider || !displayPlan) {
    return undefined
  }

  const known = PRESENTATIONS_BY_PROVIDER.get(provider)?.get(displayPlan.toLowerCase())
  if (known) {
    return known
  }

  return undefined
}
