import type { UsageSubscriptionInfo } from '@/lib/types'

export type SubscriptionBadgeKind =
  | 'codex-free'
  | 'codex-plus'
  | 'codex-team'
  | 'codex-pro5x'
  | 'codex-pro20x'
  | 'codex-enterprise'
  | 'codex-unknown'
  | 'claude-free'
  | 'claude-pro'
  | 'claude-max'
  | 'claude-team'
  | 'antigravity-free'
  | 'antigravity-pro'
  | 'antigravity-ultra-lite'
  | 'antigravity-ultra'
  | 'antigravity-unknown'

export type SubscriptionBadgeModel = {
  kind: SubscriptionBadgeKind
  labelKey?: string
  fallbackLabel?: string
}

const CODEX_PRESENTATIONS = new Map<string, Omit<SubscriptionBadgeModel, 'fallbackLabel'>>([
  ['free', { kind: 'codex-free', labelKey: 'usage_stats.credentials_subscription_codex_free' }],
  ['plus', { kind: 'codex-plus', labelKey: 'usage_stats.credentials_subscription_codex_plus' }],
  ['team', { kind: 'codex-team', labelKey: 'usage_stats.credentials_subscription_codex_team' }],
  ['pro-5x', { kind: 'codex-pro5x', labelKey: 'usage_stats.credentials_subscription_codex_pro_5x' }],
  ['pro-20x', { kind: 'codex-pro20x', labelKey: 'usage_stats.credentials_subscription_codex_pro_20x' }],
  ['enterprise', { kind: 'codex-enterprise', labelKey: 'usage_stats.credentials_subscription_codex_enterprise' }],
])

const CLAUDE_PRESENTATIONS = new Map<string, Omit<SubscriptionBadgeModel, 'fallbackLabel'>>([
  ['free', { kind: 'claude-free', labelKey: 'usage_stats.credentials_subscription_claude_free' }],
  ['pro', { kind: 'claude-pro', labelKey: 'usage_stats.credentials_subscription_claude_pro' }],
  ['max', { kind: 'claude-max', labelKey: 'usage_stats.credentials_subscription_claude_max' }],
  ['team', { kind: 'claude-team', labelKey: 'usage_stats.credentials_subscription_claude_team' }],
])

const ANTIGRAVITY_PRESENTATIONS = new Map<string, Omit<SubscriptionBadgeModel, 'fallbackLabel'>>([
  ['free', { kind: 'antigravity-free', labelKey: 'usage_stats.credentials_subscription_antigravity_free' }],
  ['pro', { kind: 'antigravity-pro', labelKey: 'usage_stats.credentials_subscription_antigravity_pro' }],
  ['ultra-lite', { kind: 'antigravity-ultra-lite', labelKey: 'usage_stats.credentials_subscription_antigravity_ultra_lite' }],
  ['ultra', { kind: 'antigravity-ultra', labelKey: 'usage_stats.credentials_subscription_antigravity_ultra' }],
])

const PRESENTATIONS_BY_PROVIDER = new Map([
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

  if (provider === 'antigravity' && displayPlan.toLowerCase() === 'unknown') {
    const fallbackLabel = subscription?.tierName?.trim() || subscription?.tierId?.trim()
    return fallbackLabel
      ? { kind: 'antigravity-unknown', fallbackLabel }
      : { kind: 'antigravity-unknown', labelKey: 'usage_stats.credentials_subscription_antigravity_unknown' }
  }

  if (provider !== 'codex') {
    return undefined
  }

  return {
    kind: 'codex-unknown',
    fallbackLabel: displayPlan,
  }
}
