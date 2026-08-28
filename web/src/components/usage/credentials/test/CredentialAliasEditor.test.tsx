import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { CredentialAliasEditor, isCredentialAliasEditorDisabled } from '../CredentialAliasEditor'

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
        alias="Friendly Auth"
        saving={false}
        onSaveAlias={async () => undefined}
      />,
    )

    expect(html).toContain('Friendly Auth')
    expect(html).toContain('usage_stats.credentials_alias_edit')
  })

  it('keeps the display name and edit action in separate layout slots', () => {
    const html = renderToStaticMarkup(
      <CredentialAliasEditor
        identityId="1"
        displayName="Very Long Credential Name"
        alias="Very Long Credential Name"
        saving={false}
        onSaveAlias={async () => undefined}
      />,
    )

    expect(html).toContain('credentialAliasDisplayLayout')
    expect(html).toContain('credentialAliasNameSlot')
    expect(html).toContain('credentialAliasActionSlot')
    expect(html.indexOf('credentialAliasNameSlot')).toBeLessThan(html.indexOf('credentialAliasActionSlot'))
    expect(html).not.toMatch(/credentialAliasDisplay_[a-z0-9]+/)
  })

  it('disables other rows while an alias save is in flight', () => {
    expect(isCredentialAliasEditorDisabled('1', false, '')).toBe(false)
    expect(isCredentialAliasEditorDisabled('1', false, '1')).toBe(false)
    expect(isCredentialAliasEditorDisabled('1', false, '2')).toBe(true)
    expect(isCredentialAliasEditorDisabled('1', true, '')).toBe(true)
  })

  it('renders the display name as a dedicated detail trigger without nesting the alias edit action', () => {
    const html = renderToStaticMarkup(
      <CredentialAliasEditor
        identityId="1"
        displayName="Friendly Auth"
        alias="Friendly Auth"
        saving={false}
        onOpenDetails={() => undefined}
        onSaveAlias={async () => undefined}
      />,
    )

    expect(html).toContain('data-credential-detail-trigger="true"')
    expect(html).toContain('type="button"')
    expect(html).toContain('credentialDetailNameArrow')
    expect(html.indexOf('data-credential-detail-trigger="true"')).toBeLessThan(html.indexOf('usage_stats.credentials_alias_edit'))
  })
})
