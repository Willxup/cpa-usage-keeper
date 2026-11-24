import { describe, expect, it } from 'vitest'
import type { UsageSubscriptionInfo } from '@/lib/types'
import { resolveCredentialSubscriptionBadge } from '../credentialSubscription'

describe('credentialSubscription', () => {
  it.each([
    ['free', 'codex-free', 'usage_stats.credentials_subscription_codex_free'],
    ['plus', 'codex-plus', 'usage_stats.credentials_subscription_codex_plus'],
    ['team', 'codex-team', 'usage_stats.credentials_subscription_codex_team'],
    ['pro-5x', 'codex-pro10x', 'usage_stats.credentials_subscription_codex_pro_10x'],
    ['pro-10x', 'codex-pro10x', 'usage_stats.credentials_subscription_codex_pro_10x'],
    ['pro-50x', 'codex-pro50x', 'usage_stats.credentials_subscription_codex_pro_50x'],
    ['business', 'codex-business', 'usage_stats.credentials_subscription_codex_business'],
    ['go', 'codex-go', 'usage_stats.credentials_subscription_codex_go'],
    ['edu', 'codex-edu', 'usage_stats.credentials_subscription_codex_edu'],
    ['pro-20x', 'codex-pro20x', 'usage_stats.credentials_subscription_codex_pro_20x'],
    ['enterprise', 'codex-enterprise', 'usage_stats.credentials_subscription_codex_enterprise'],
  ] as const)('maps Codex %s to its namespaced badge', (plan, kind, labelKey) => {
    expect(resolveCredentialSubscriptionBadge({ provider: ' CoDeX ', plan: ` ${plan.toUpperCase()} ` })).toEqual({
      kind,
      labelKey,
    })
  })

  it.each(['codex', 'claude', 'antigravity'])('hides unknown %s plans', (provider) => {
    expect(resolveCredentialSubscriptionBadge({ provider, plan: ' Future-Plan ' })).toBeUndefined()
  })

  it.each([
    ['Everyday Usage', 'codex-plus', 'everyday'],
    ['High Usage', 'codex-plus', 'high'],
    ['Power Usage', 'codex-pro10x', 'power'],
    ['Maximum Usage', 'codex-pro20x', 'maximum'],
  ] as const)('maps Meta %s directly to the shared badge presentation', (plan, kind, label) => {
    expect(resolveCredentialSubscriptionBadge({ provider: ' META ', plan: ` ${plan.toUpperCase()} ` })).toEqual({
      kind,
      labelKey: `usage_stats.credentials_subscription_meta_${label}`,
    })
  })

  it.each(['1234567890123456', 'Super Power Usage', 'high', 'unknown', ''])('hides unrecognized Meta plan %s', (plan) => {
    expect(resolveCredentialSubscriptionBadge({ provider: 'meta', plan })).toBeUndefined()
  })

  it.each([
    ['free', 'claude-free', 'usage_stats.credentials_subscription_claude_free'],
    ['pro', 'claude-pro', 'usage_stats.credentials_subscription_claude_pro'],
    ['max', 'claude-max', 'usage_stats.credentials_subscription_claude_max'],
    ['max-5x', 'claude-max5x', 'usage_stats.credentials_subscription_claude_max_5x'],
    ['max-20x', 'claude-max20x', 'usage_stats.credentials_subscription_claude_max_20x'],
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

  it.each([
    { tierId: 'future-tier', tierName: 'Future' },
    { tierId: 'future-tier' },
    {},
  ])('hides unknown Antigravity tiers even when metadata is available: %j', (metadata) => {
    expect(resolveCredentialSubscriptionBadge({ provider: 'antigravity', plan: 'unknown', ...metadata })).toBeUndefined()
  })

  it.each(['constructor', 'toString', '__proto__'])('treats inherited object key %s as an unknown plan', (plan) => {
    expect(resolveCredentialSubscriptionBadge({ provider: 'codex', plan })).toBeUndefined()
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
