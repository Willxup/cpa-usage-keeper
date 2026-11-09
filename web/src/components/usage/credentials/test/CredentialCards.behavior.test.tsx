// @vitest-environment happy-dom

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import type { UsageQuotaRow } from '@/lib/types'
import { CredentialQuotaPanel, CredentialsSection } from '../CredentialsSection'
import { buildCredentialRows } from '../credentialViewModels'
import { createCredentialSectionProps, createSectionIdentity } from './credentialSectionFixtures'

const quota = (key: string, seconds: number, overrides: Partial<UsageQuotaRow> = {}): UsageQuotaRow => ({
  key,
  label: '5h',
  window: { seconds },
  usedPercent: 25,
  window_usage_tokens: 1_000_000,
  window_usage_cost: 2.5,
  ...overrides,
})

const codexRow = (plan: string, quotas: UsageQuotaRow[]) => {
  const identity = createSectionIdentity({
    id: plan,
    identity: `auth-${plan}`,
    displayName: `Codex ${plan}`,
    subscription: { provider: 'codex', plan },
  })
  return buildCredentialRows([identity], new Map([[identity.identity, { id: identity.identity, quota: quotas }]]))[0]
}

describe('credential compact card data', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true
    await i18n.changeLanguage('en')
    localStorage.clear()
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
  })

  it('renders Plus with two real windows and each Pro with one Weekly, independently of the health strip', async () => {
    const rows = [
      codexRow('plus', [quota('rate_limit.primary_window', 18_000), quota('rate_limit.secondary_window', 604_800)]),
      codexRow('pro-5x', [quota('rate_limit.primary_window', 604_800)]),
      codexRow('pro-20x', [quota('rate_limit.primary_window', 604_800)]),
    ]
    await act(async () => root.render(<CredentialsSection {...createCredentialSectionProps({ rows, total: rows.length })} />))

    const cards = Array.from(container.querySelectorAll<HTMLElement>('[data-credential-card="true"]'))
    expect(cards.map((card) => card.getAttribute('data-credential-identity'))).toEqual(['plus', 'pro-5x', 'pro-20x'])
    for (const [index, card] of cards.entries()) {
      const health = card.querySelector('[data-credential-health-variant="compact"]')!
      const quotaPanel = card.querySelector('[data-credential-quota-variant="compact"]')!
      const windows = Array.from(quotaPanel.querySelectorAll('[data-quota-key]'))
      expect(health.querySelectorAll('[role="listitem"]')).toHaveLength(30)
      expect(health.textContent).toContain('66.7%')
      expect(card.textContent).toContain('97.24%')
      expect(windows).toHaveLength(index === 0 ? 2 : 1)
      expect(windows.map((window) => window.getAttribute('data-quota-key'))).toEqual(rows[index].displayQuotas.map((item) => item.key))
      expect(windows.at(-1)?.textContent).toContain('Weekly')
      if (index === 0) expect(windows[0].textContent).toContain('5h')
      else expect(quotaPanel.textContent).not.toContain('5h')
    }
  })

  it('switches the same icon usage line between Current and Estimated without changing metrics or quota count', async () => {
    const row = codexRow('plus', [quota('rate_limit.primary_window', 18_000)])
    await act(async () => root.render(<CredentialsSection {...createCredentialSectionProps({ rows: [row], total: 1 })} />))

    const card = container.querySelector<HTMLElement>('[data-credential-card="true"]')!
    const quotaPanel = () => card.querySelector<HTMLElement>('[data-credential-quota-variant="compact"]')!
    expect(quotaPanel().textContent).toContain('1.00M')
    expect(quotaPanel().textContent).toContain('$2.50')
    expect(quotaPanel().textContent).not.toContain('4.00M')
    const estimated = Array.from(container.querySelectorAll<HTMLButtonElement>('button')).find((button) => button.textContent === 'Estimated')!
    await act(async () => estimated.click())

    expect(quotaPanel().textContent).toContain('4.00M')
    expect(quotaPanel().textContent).toContain('$10.00')
    expect(quotaPanel().textContent).not.toContain('1.00M')
    expect(quotaPanel().textContent).not.toContain('$2.50')
    expect(quotaPanel().querySelectorAll('[data-quota-key]')).toHaveLength(1)
    expect(card.textContent).toContain('1.23K')
    expect(card.textContent).toContain('97.24%')
    expect(card.textContent).toContain('456.79K')
    expect(card.textContent).toContain('41.50%')
  })

  it('keeps additional and unknown-plan quotas, real zero usage, and missing usage distinct in compact mode', async () => {
    const row = codexRow('unknown-plan', [
      quota('rate_limit.primary_window', 604_800, { window_usage_tokens: 0, window_usage_cost: 0 }),
      quota('additional_rate_limits.Spark.primary_window', 18_000, { label: 'Spark 5h' }),
      quota('code_review_rate_limit.primary_window', 604_800, { label: 'Code Review 5h', window_usage_tokens: undefined, window_usage_cost: 1 }),
    ])
    await act(async () => root.render(<CredentialQuotaPanel row={row} quotaUsageMode="estimated" variant="compact" />))

    const windows = Array.from(container.querySelectorAll('[data-quota-key]'))
    expect(windows.map((window) => window.getAttribute('data-quota-key'))).toEqual(row.displayQuotas.map((item) => item.key))
    expect(windows).toHaveLength(3)
    expect(windows[0].textContent).toContain('$0.00')
    expect(windows[0].querySelector('[aria-label]')?.getAttribute('aria-label')).toContain('0')
    expect(windows[1].textContent).toContain('4.00M')
    expect(windows[2].textContent).toContain('Code Review Weekly')
    expect(windows[2].textContent).not.toContain('$0.00')
    expect(windows[2].querySelector('img')).toBeNull()
  })

  it('keeps the existing Antigravity canonical groups and flat fallback in the same order', async () => {
    const identity = createSectionIdentity({ type: 'antigravity', provider: 'antigravity' })
    const groupQuota = (key: string, label: string, groupKey: string, groupLabel: string) => quota(key, 18_000, {
      label, scope: 'quota_group', groupKey, groupLabel, groupDescription: `${groupLabel} explanation`,
    })
    const rawQuotas = [
      groupQuota('bucket.gemini.5h', '5h', 'antigravity-gemini-models', 'Gemini Models'),
      groupQuota('bucket.gemini.weekly', 'Weekly', 'antigravity-gemini-models', 'Gemini Models'),
      groupQuota('bucket.claude.5h', '5h', 'antigravity-claude-and-gpt-models', 'Claude and GPT models'),
      groupQuota('bucket.other.5h', 'Other 5h', 'other-provider-group', 'Other Provider Group'),
    ]
    const row = buildCredentialRows([identity], new Map([[identity.identity, { id: identity.identity, quota: rawQuotas }]]))[0]
    await act(async () => root.render(<CredentialQuotaPanel row={row} quotaUsageMode="current" variant="compact" />))

    expect(Array.from(container.querySelectorAll('[data-quota-key]'), (window) => window.getAttribute('data-quota-key'))).toEqual(rawQuotas.map((item) => item.key))
    expect(Array.from(container.querySelectorAll('[data-quota-group]'), (group) => group.getAttribute('data-quota-group'))).toEqual([
      'antigravity-gemini-models', 'antigravity-claude-and-gpt-models',
    ])
    expect(container.querySelector('[data-quota-group="antigravity-gemini-models"]')?.querySelectorAll('[data-quota-key]')).toHaveLength(2)
    const flatQuota = container.querySelector('[data-quota-key="bucket.other.5h"]')!
    expect(flatQuota.closest('[data-quota-group]')).toBeNull()
    expect(flatQuota.textContent).toContain('Other Provider Group')
    expect(container.textContent).toContain('Gemini Models explanation')
    expect(container.textContent).toContain('Claude and GPT models explanation')
  })

  it.each([1, 0.44056508, 0])('renders a real Argon Weekly group at fraction %s without the plan description', async (fraction) => {
    const identity = createSectionIdentity({ type: 'antigravity', provider: 'antigravity' })
    const rawQuotas = [
      quota('bucket.antigravity-gemini-models.gemini-weekly', 604_800, {
        label: 'Weekly', scope: 'quota_group', groupKey: 'antigravity-gemini-models', groupLabel: 'Gemini Models', groupDescription: 'Gemini explanation',
        usedPercent: undefined, remainingFraction: 0.5, window_usage_tokens: 200, window_usage_cost: 1,
      }),
      quota('bucket.antigravity-gemini-4-argon.gemini-argon-wtu-weekly', 604_800, {
        label: 'Weekly', scope: 'quota_group', groupKey: 'antigravity-gemini-4-argon', groupLabel: 'Gemini 4 Argon',
        groupDescription: 'For paid Pro plans, Argon quota is separate.', usedPercent: undefined, remainingFraction: fraction,
        resetAt: '2099-10-30T09:33:19Z', window_usage_tokens: 100, window_usage_cost: 0.5,
      }),
      quota('bucket.antigravity-image-models.image-weekly', 604_800, {
        label: 'Image Weekly', scope: 'quota_group', groupKey: 'antigravity-image-models', groupLabel: 'Image Models', groupDescription: 'Image group explanation',
      }),
      quota('bucket.antigravity-other-models.other-weekly', 604_800, {
        label: 'Other Weekly', scope: 'quota_group', groupKey: 'antigravity-other-models', groupLabel: 'Other Models', groupDescription: 'Other group explanation',
      }),
    ]
    const row = buildCredentialRows([identity], new Map([[identity.identity, { id: identity.identity, quota: rawQuotas }]]))[0]
    const render = async (quotaUsageMode: 'current' | 'estimated') => act(async () => root.render(<CredentialQuotaPanel row={row} quotaUsageMode={quotaUsageMode} variant="compact" timeZone="UTC" />))
    await render('current')

    expect(Array.from(container.querySelectorAll('[data-quota-group]'), (group) => group.getAttribute('data-quota-group'))).toEqual([
      'antigravity-gemini-models', 'antigravity-gemini-4-argon',
    ])
    expect(Array.from(container.querySelectorAll('[data-quota-key]'), (bar) => bar.getAttribute('data-quota-key'))).toEqual(rawQuotas.map((item) => item.key))
    const image = container.querySelector<HTMLElement>('[data-quota-key="bucket.antigravity-image-models.image-weekly"]')!
    const other = container.querySelector<HTMLElement>('[data-quota-key="bucket.antigravity-other-models.other-weekly"]')!
    expect(image.closest('[data-quota-group]')).toBeNull()
    expect(other.closest('[data-quota-group]')).toBeNull()
    expect(image.textContent).toContain('Image Models')
    expect(other.textContent).toContain('Other Models')
    expect(other.className).toContain('credentialQuotaBarTooltipRight')
    const argon = container.querySelector<HTMLElement>('[data-quota-group="antigravity-gemini-4-argon"]')!
    const bar = argon.querySelector<HTMLElement>('[data-quota-key]')!
    expect(argon.textContent).toContain('Gemini 4 Argon')
    expect(bar.textContent).toContain('Weekly')
    expect(bar.textContent).toContain(`${Math.round(fraction * 100)}%`)
    expect(bar.className).toContain('credentialQuotaWindowFull')
    expect(bar.querySelector('[data-quota-reset-countdown="true"]')).not.toBeNull()
    expect(argon.querySelector('[role="tooltip"]')).toBeNull()
    expect(container.textContent).not.toContain('For paid Pro plans')
    expect(container.textContent).toContain('Gemini explanation')
    expect(container.textContent).toContain('Image group explanation')
    expect(container.textContent).toContain('Other group explanation')
    const gemini = container.querySelector<HTMLElement>('[data-quota-group="antigravity-gemini-models"]')!
    expect(gemini.textContent).toContain('200')
    expect(gemini.textContent).toContain('$1.00')
    expect(bar.textContent).toContain('100')
    expect(bar.textContent).toContain('$0.50')
    await render('estimated')
    expect(gemini.textContent).toContain('400')
    expect(gemini.textContent).toContain('$2.00')
    if (fraction > 0 && fraction < 1) {
      expect(bar.textContent).toContain('179')
      expect(bar.textContent).toContain('$0.89')
    } else {
      expect(bar.textContent).toContain('100')
      expect(bar.textContent).toContain('$0.50')
    }
  })

  it('keeps the official Argon bar when its local usage pair is unavailable', async () => {
    const identity = createSectionIdentity({ type: 'antigravity', provider: 'antigravity' })
    const row = buildCredentialRows([identity], new Map([[identity.identity, { id: identity.identity, quota: [quota('bucket.antigravity-gemini-4-argon.gemini-argon-wtu-weekly', 604_800, {
      label: 'Weekly', scope: 'quota_group', groupKey: 'antigravity-gemini-4-argon', groupLabel: 'Gemini 4 Argon', usedPercent: undefined, remainingFraction: 0.5,
      window_usage_tokens: undefined, window_usage_cost: undefined,
    })] }]]))[0]
    await act(async () => root.render(<CredentialQuotaPanel row={row} quotaUsageMode="estimated" variant="compact" />))
    const argon = container.querySelector<HTMLElement>('[data-quota-group="antigravity-gemini-4-argon"]')!
    expect(argon.textContent).toContain('Weekly')
    expect(argon.textContent).toContain('50%')
    expect(argon.querySelector('[data-quota-bar="true"]')).not.toBeNull()
    expect(argon.querySelector('img')).toBeNull()
    expect(argon.textContent).not.toContain('$0.00')
  })

  it('does not synthesize Argon for an Antigravity account without the group', async () => {
    const identity = createSectionIdentity({ type: 'antigravity', provider: 'antigravity' })
    const row = buildCredentialRows([identity], new Map([[identity.identity, { id: identity.identity, quota: [quota('bucket.antigravity-gemini-models.gemini-weekly', 604_800, {
      scope: 'quota_group', groupKey: 'antigravity-gemini-models', groupLabel: 'Gemini Models',
    })] }]]))[0]
    await act(async () => root.render(<CredentialQuotaPanel row={row} quotaUsageMode="current" variant="compact" />))
    expect(container.querySelectorAll('[data-quota-group]')).toHaveLength(1)
    expect(container.querySelectorAll('[data-quota-key]')).toHaveLength(1)
    expect(container.textContent).not.toContain('Argon')
  })

  it('preserves canonical grouping without provider or row-key restrictions', async () => {
    const identity = createSectionIdentity({ type: 'codex', provider: 'codex' })
    const row = buildCredentialRows([identity], new Map([[identity.identity, { id: identity.identity, quota: [
      quota('legacy.gemini', 604_800, { scope: 'quota_group', groupKey: 'antigravity-gemini-models', groupLabel: 'Gemini Models' }),
      quota('legacy.claude', 604_800, { scope: 'quota_group', groupKey: 'antigravity-claude-and-gpt-models', groupLabel: 'Claude and GPT models' }),
    ] }]]))[0]
    await act(async () => root.render(<CredentialQuotaPanel row={row} quotaUsageMode="current" variant="compact" />))
    expect(Array.from(container.querySelectorAll('[data-quota-group]'), (group) => group.getAttribute('data-quota-group'))).toEqual([
      'antigravity-gemini-models', 'antigravity-claude-and-gpt-models',
    ])
    expect(container.querySelectorAll('[data-quota-key]')).toHaveLength(2)
    expect(container.textContent).not.toContain('Argon')
  })

  it('refreshes the compact reset tooltip from the current snapshot in the project timezone', async () => {
    const renderReset = async (resetAt: string, timeZone: string | undefined = 'Asia/Shanghai') => {
      const row = codexRow('plus', [quota('rate_limit.primary_window', 18_000, { resetAt })])
      await act(async () => root.render(<CredentialQuotaPanel row={row} quotaUsageMode="current" variant="compact" timeZone={timeZone} />))
    }
    await renderReset('2026-05-12T03:15:00-07:00')
    const countdown = container.querySelector<HTMLButtonElement>('[data-quota-reset-countdown="true"]')!
    expect(countdown.getAttribute('aria-label')).toBe('Resets at 05-12 18:15')
    await act(async () => countdown.focus())
    const tooltip = () => document.getElementById(countdown.getAttribute('aria-describedby')!)
    expect(tooltip()?.textContent).toBe('Resets at 05-12 18:15')

    await renderReset('2026-05-13T03:15:00-07:00')

    expect(container.querySelector('[data-quota-reset-countdown="true"]')).toBe(countdown)
    expect(countdown.getAttribute('aria-label')).toBe('Resets at 05-13 18:15')
    expect(tooltip()?.textContent).toBe('Resets at 05-13 18:15')
    expect(tooltip()?.textContent).not.toContain('05-12')

    await renderReset('2026-05-13T03:15:00-07:00', 'Local')

    expect(container.querySelector('[data-quota-reset-countdown="true"]')).toBeNull()
    expect(container.textContent).not.toContain('05-13 18:15')
  })
})
