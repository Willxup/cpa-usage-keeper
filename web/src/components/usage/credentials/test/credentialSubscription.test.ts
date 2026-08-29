import { describe, expect, it } from 'vitest'
import type { UsageSubscriptionInfo } from '@/lib/types'
import { resolveCredentialSubscriptionBadge } from '../credentialSubscription'

describe('credentialSubscription', () => {
  it.each([
    ['free', 'codex-free', 'usage_stats.credentials_subscription_codex_free'],
    ['plus', 'codex-plus', 'usage_stats.credentials_subscription_codex_plus'],
    ['team', 'codex-team', 'usage_stats.credentials_subscription_codex_team'],
    ['pro-5x', 'codex-pro5x', 'usage_stats.credentials_subscription_codex_pro_5x'],
    ['pro-20x', 'codex-pro20x', 'usage_stats.credentials_subscription_codex_pro_20x'],
    ['enterprise', 'codex-enterprise', 'usage_stats.credentials_subscription_codex_enterprise'],
  ] as const)('maps Codex %s to its namespaced badge', (plan, kind, labelKey) => {
    expect(resolveCredentialSubscriptionBadge({ provider: ' CoDeX ', plan: ` ${plan.toUpperCase()} ` })).toEqual({
      kind,
      labelKey,
    })
  })

  it('keeps unknown Codex plans readable without treating them as known tiers', () => {
    expect(resolveCredentialSubscriptionBadge({ provider: 'codex', plan: ' ChatGPT-Pro-Monthly ' })).toEqual({
      kind: 'codex-unknown',
      fallbackLabel: 'ChatGPT-Pro-Monthly',
    })
  })

  it.each([
    ['free', 'claude-free', 'usage_stats.credentials_subscription_claude_free'],
    ['pro', 'claude-pro', 'usage_stats.credentials_subscription_claude_pro'],
    ['max', 'claude-max', 'usage_stats.credentials_subscription_claude_max'],
    ['team', 'claude-team', 'usage_stats.credentials_subscription_claude_team'],
  ] as const)('maps Claude %s to its namespaced badge', (plan, kind, labelKey) => {
    expect(resolveCredentialSubscriptionBadge({ provider: ' Claude ', plan: ` ${plan.toUpperCase()} ` })).toEqual({
      kind,
      labelKey,
    })
  })

  it('does not guess a badge for unknown Claude plans', () => {
    expect(resolveCredentialSubscriptionBadge({ provider: 'claude', plan: 'enterprise' })).toBeUndefined()
  })

  it.each([
    ['free', 'antigravity-free', 'usage_stats.credentials_subscription_antigravity_free'],
    ['pro', 'antigravity-pro', 'usage_stats.credentials_subscription_antigravity_pro'],
    ['ultra-lite', 'antigravity-ultra-lite', 'usage_stats.credentials_subscription_antigravity_ultra_lite'],
    ['ultra', 'antigravity-ultra', 'usage_stats.credentials_subscription_antigravity_ultra'],
  ] as const)('maps Antigravity %s to its namespaced badge', (plan, kind, labelKey) => {
    expect(resolveCredentialSubscriptionBadge({ provider: ' Antigravity ', plan: ` ${plan.toUpperCase()} ` })).toEqual({
      kind,
      labelKey,
    })
  })

  it('uses tier name, then tier id, then Unknown for unknown Antigravity tiers', () => {
    expect(resolveCredentialSubscriptionBadge({ provider: 'antigravity', plan: 'unknown', tierId: 'future-tier', tierName: ' Future ' })).toEqual({
      kind: 'antigravity-unknown',
      fallbackLabel: 'Future',
    })
    expect(resolveCredentialSubscriptionBadge({ provider: 'antigravity', plan: 'unknown', tierId: ' future-tier ' })).toEqual({
      kind: 'antigravity-unknown',
      fallbackLabel: 'future-tier',
    })
    expect(resolveCredentialSubscriptionBadge({ provider: 'antigravity', plan: 'unknown' })).toEqual({
      kind: 'antigravity-unknown',
      labelKey: 'usage_stats.credentials_subscription_antigravity_unknown',
    })
  })

  it.each(['constructor', 'toString', '__proto__'])('treats inherited object key %s as an unknown plan', (plan) => {
    expect(resolveCredentialSubscriptionBadge({ provider: 'codex', plan })).toEqual({
      kind: 'codex-unknown',
      fallbackLabel: plan,
    })
  })

  it.each([
    undefined,
    { provider: '', plan: 'plus' },
    { provider: 'codex', plan: '' },
    { provider: 'antigravity', plan: 'future' },
  ] as Array<UsageSubscriptionInfo | undefined>)('does not invent badges for missing or unregistered subscriptions', (subscription) => {
    expect(resolveCredentialSubscriptionBadge(subscription)).toBeUndefined()
  })
})
