import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { AuthFileCredentialsSection } from '../AuthFileCredentialsSection'
import { createAuthFileSectionProps } from './credentialSectionFixtures'
import type { AuthFileCredentialRow } from '../credentialViewModels'

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({
    t: (key: string, params?: Record<string, string>) => `${key}:${params?.tokens ?? ''}:${params?.cost ?? ''}`,
  }),
}))

const createRow = (overrides: Partial<AuthFileCredentialRow['identity']> = {}): AuthFileCredentialRow => ({
  identity: { id: '1', identity: 'auth-1', type: 'codex', is_deleted: false, ...overrides },
  displayName: 'Auth File Row',
  maskedIdentity: 'auth-1',
  providerLabel: 'Codex',
  typeLabel: 'codex',
  authTypeLabel: 'oauth',
  priorityLabel: 'P1',
  totalRequests: 1,
  successCount: 1,
  failureCount: 0,
  successRate: 100,
  totalTokens: 10,
  cacheReadRate: null,
  windowCacheReadRate: null,
  quota: [],
  quotaLoading: false,
  displayQuotas: [],
} as AuthFileCredentialRow)

const renderSection = (rows: AuthFileCredentialRow[]) =>
  renderToStaticMarkup(createElement(AuthFileCredentialsSection, createAuthFileSectionProps({ rows, total: rows.length })))

describe('AuthFileCredentialsSection status toggle gating', () => {
  it('renders the enable/disable toggle for credential types Keeper knows', () => {
    const html = renderSection([createRow({ type: 'codex' })])

    expect(html).toContain('data-credential-status-toggle="true"')
    expect(html).toContain('aria-pressed="true"')
  })

  it('omits the toggle for unknown auth file types instead of rendering an invisible button', () => {
    // 认证文件 type 直接来自 CPA，插件可以注册任意 provider 标识；没有品牌图标的类型不能给出开关。
    const html = renderSection([createRow({ type: 'iflow' })])

    expect(html).not.toContain('data-credential-status-toggle="true"')
    // 回退到改动前的静态图标槽位，未映射类型不渲染任何图标，也不产生可聚焦元素。
    expect(html).not.toContain('data-provider-brand-icon')
    expect(html).not.toContain('tabindex="0"')
  })

  it('gates the toggle by brand icon mapping rather than by the single-credential whitelist', () => {
    // kimi、antigravity、gemini-cli、openai 都是 CPA 认证文件里的真实类型：有品牌图标，但不在整条停用的白名单里。
    // 只有这些类型能把「按品牌图标判定」与「按白名单判定」两种实现区分开。
    for (const type of ['kimi', 'antigravity', 'gemini-cli', 'openai']) {
      expect(renderSection([createRow({ type })])).toContain('data-credential-status-toggle="true"')
    }
  })

  it('reflects the upstream disabled state through aria-pressed', () => {
    expect(renderSection([createRow({ type: 'codex', disabled: false })])).toContain('aria-pressed="true"')
    expect(renderSection([createRow({ type: 'codex', disabled: true })])).toContain('aria-pressed="false"')
  })
})
