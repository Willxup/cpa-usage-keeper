import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import type { UsageIdentity } from '@/lib/types'
import { AiProviderCredentialsSection } from '../AiProviderCredentialsSection'
import { AuthFileCredentialsSection } from '../AuthFileCredentialsSection'
import { buildAiProviderCredentialRows, buildAuthFileCredentialRows } from '../credentialViewModels'
import { createAiProviderSectionProps, createAuthFileSectionProps } from './credentialSectionFixtures'

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({ t: (key: string) => key }),
}))

const identity = (authType: 1 | 2): UsageIdentity => ({
  id: String(authType), identity: `index-${authType}`, auth_type: authType,
  name: 'Credential', type: authType === 1 ? 'codex' : 'openai', provider: 'provider', is_deleted: false,
} as UsageIdentity)

describe('credential priority entry in both sections', () => {
  it('renders an actionable P0 for active credentials with no priority', () => {
    const onSavePriority = vi.fn(async () => undefined)
    const authRows = buildAuthFileCredentialRows([identity(1)])
    const aiRows = buildAiProviderCredentialRows([identity(2)])
    const authHTML = renderToStaticMarkup(<AuthFileCredentialsSection {...createAuthFileSectionProps({ rows: authRows, total: 1, onSavePriority })} />)
    const aiHTML = renderToStaticMarkup(<AiProviderCredentialsSection {...createAiProviderSectionProps({ rows: aiRows, total: 1, onSavePriority })} />)
    for (const html of [authHTML, aiHTML]) {
      expect(html).toContain('>P0</button>')
      expect(html).toContain('usage_stats.credentials_priority_edit')
    }
  })

  it('does not offer editing for a deleted credential', () => {
    const rows = buildAiProviderCredentialRows([{ ...identity(2), is_deleted: true }])
    const html = renderToStaticMarkup(<AiProviderCredentialsSection {...createAiProviderSectionProps({ rows, total: 1, onSavePriority: async () => undefined })} />)
    expect(html).not.toContain('usage_stats.credentials_priority_edit')
  })
})
