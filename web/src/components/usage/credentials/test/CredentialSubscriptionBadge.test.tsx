import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import { CredentialSubscriptionBadge } from '../CredentialSubscriptionBadge'
import type { SubscriptionBadgeKind } from '../credentialSubscription'

describe('CredentialSubscriptionBadge', () => {
  it('preserves fallback labels across subscription providers and unknown plans', () => {
    const kinds: SubscriptionBadgeKind[] = [
      'codex-pro20x', 'codex-free', 'codex-unknown',
      'claude-free', 'claude-pro', 'claude-max', 'claude-max5x', 'claude-max20x', 'claude-team',
      'antigravity-free', 'antigravity-pro', 'antigravity-ultra-lite', 'antigravity-ultra', 'antigravity-unknown',
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

  it('resolves known labels through i18n', async () => {
    await i18n.changeLanguage('en')
    const html = renderToStaticMarkup(createElement(CredentialSubscriptionBadge, {
      model: { kind: 'codex-team', labelKey: 'usage_stats.credentials_subscription_codex_team' },
    }))
    expect(html.replace(/<[^>]+>/g, '')).toBe('Team')
  })
})
