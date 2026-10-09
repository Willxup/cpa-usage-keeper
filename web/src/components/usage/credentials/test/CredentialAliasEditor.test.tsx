import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { CredentialAliasEditor } from '../CredentialAliasEditor'

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({
    t: (key: string) => key,
  }),
}))

describe('CredentialAliasEditor', () => {
  it('renders the current display name with an edit alias action', () => {
    const html = renderToStaticMarkup(
      <CredentialAliasEditor
        identityId="1"
        displayName="Friendly Auth"
        onEdit={async () => undefined}
      />,
    )

    expect(html).toContain('Friendly Auth')
    expect(html).toContain('usage_stats.credentials_edit_title')
  })

  it('hides editing for a deleted credential', () => {
    const html = renderToStaticMarkup(<CredentialAliasEditor identityId="1" displayName="Deleted" disabled onEdit={() => undefined} />)
    expect(html).not.toContain('usage_stats.credentials_edit_title')
  })

  it('renders the display name as a dedicated detail trigger without nesting the alias edit action', () => {
    const html = renderToStaticMarkup(
      <CredentialAliasEditor
        identityId="1"
        displayName="Friendly Auth"
        onOpenDetails={() => undefined}
        onEdit={async () => undefined}
      />,
    )

    expect(html).toContain('data-credential-detail-trigger="true"')
    expect(html).toContain('type="button"')
    expect(html.indexOf('data-credential-detail-trigger="true"')).toBeLessThan(html.indexOf('usage_stats.credentials_edit_title'))
  })
})
