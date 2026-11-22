import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import { CredentialSubscriptionBadge } from '../CredentialSubscriptionBadge'
import { resolveCredentialSubscriptionBadge, type SubscriptionBadgeKind } from '../credentialSubscription'

describe('CredentialSubscriptionBadge', () => {
  it('preserves fallback labels across subscription providers and unknown plans', () => {
    const kinds: SubscriptionBadgeKind[] = [
      'codex-pro20x', 'codex-free',
      'claude-free', 'claude-pro', 'claude-max', 'claude-max5x', 'claude-max20x', 'claude-team',
      'antigravity-free', 'antigravity-pro', 'antigravity-ultra-lite', 'antigravity-ultra',
    ]
    for (const kind of kinds) {
      const html = renderToStaticMarkup(createElement(CredentialSubscriptionBadge, {
        model: { kind, fallbackLabel: kind },
      }))
      expect(html.replace(/<[^>]+>/g, '')).toBe(kind)
    }
  })

  it('hides premium decoration from assistive technology', () => {
    const html = renderToStaticMarkup(createElement(CredentialSubscriptionBadge, {
      model: { kind: 'codex-pro20x', fallbackLabel: 'Pro 20x' },
    }))
    expect(html).toMatch(/credentialPlanBadgeFlow[^>]+aria-hidden="true"/)
    expect(html).toMatch(/credentialPlanBadgeCorona[^>]+aria-hidden="true"/)
  })

  it.each([
    ['business', 'team', 'Business'],
    ['enterprise', 'team', 'Enterprise'],
    ['go', 'plus', 'Go'],
    ['edu', 'plus', 'Edu'],
    ['pro-50x', 'pro-20x', 'Pro 50x'],
    ['pro-10x', 'pro-5x', 'Pro 10x'],
  ])('renders %s with the same animated presentation as %s', async (plan, reference, label) => {
    await i18n.changeLanguage('en')
    const renderPlan = (value: string) => renderToStaticMarkup(createElement(CredentialSubscriptionBadge, {
      model: resolveCredentialSubscriptionBadge({ provider: 'codex', plan: value })!,
    }))
    const html = renderPlan(plan)
    expect(html.replace(/<[^>]+>/g, '')).toBe(label)
    expect(html.replace(/>[^<]*</g, '><')).toBe(renderPlan(reference).replace(/>[^<]*</g, '><'))
    expect(html).toMatch(/credentialPlanBadgeFlow[^>]+aria-hidden="true"/)
    expect(html).toMatch(/credentialPlanBadgeCorona[^>]+aria-hidden="true"/)
  })

  it.each(['claude-max20x', 'codex-pro20x', 'codex-pro50x', 'antigravity-ultra'] as const)(
    'uses the existing purple premium presentation for %s', (kind) => {
      const html = renderToStaticMarkup(createElement(CredentialSubscriptionBadge, {
        model: { kind, fallbackLabel: kind },
      }))
      expect(html).toContain('credentialPlanBadgeEnterprise')
      expect(html).toMatch(/credentialPlanBadgeFlow[^>]+aria-hidden="true"/)
      expect(html).toMatch(/credentialPlanBadgeCorona[^>]+aria-hidden="true"/)
    },
  )

  it.each([
    ['Everyday', 'credentialPlanBadgePlus'],
    ['High', 'credentialPlanBadgePlus'],
    ['Power', 'credentialPlanBadgePro5x'],
    ['Maximum', 'credentialPlanBadgeEnterprise'],
  ])('keeps Muse %s on the specified animated presentation', async (label, className) => {
    await i18n.changeLanguage('en')
    const html = renderToStaticMarkup(createElement(CredentialSubscriptionBadge, {
      model: resolveCredentialSubscriptionBadge({ provider: 'meta', plan: `${label} Usage` })!,
    }))
    expect(html).toContain(className)
    expect(html.replace(/<[^>]+>/g, '')).toBe(label)
    expect(html).toMatch(/credentialPlanBadgeFlow[^>]+aria-hidden="true"/)
    expect(html).toMatch(/credentialPlanBadgeCorona[^>]+aria-hidden="true"/)
  })

  it('resolves known labels through i18n', async () => {
    await i18n.changeLanguage('en')
    const html = renderToStaticMarkup(createElement(CredentialSubscriptionBadge, {
      model: { kind: 'codex-team', labelKey: 'usage_stats.credentials_subscription_codex_team' },
    }))
    expect(html.replace(/<[^>]+>/g, '')).toBe('Team')
  })
})
