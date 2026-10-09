import { renderToStaticMarkup } from 'react-dom/server'
import { afterEach, describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import { CredentialKimiSiteBadge } from '../CredentialKimiSiteBadge'

describe('CredentialKimiSiteBadge', () => {
  afterEach(async () => { await i18n.changeLanguage('en') })

  it.each([
    ['en', 'China', 'International'],
    ['zh', '国内站', '国际站'],
    ['zh-TW', '國內站', '國際站'],
  ])('renders the site labels in %s', async (language, china, international) => {
    await i18n.changeLanguage(language)
    expect(renderToStaticMarkup(<CredentialKimiSiteBadge identityType="kimi" />)).toContain(`>${china}</span>`)
    expect(renderToStaticMarkup(<CredentialKimiSiteBadge identityType="kimi.com" />)).toContain(`>${china}</span>`)
    expect(renderToStaticMarkup(<CredentialKimiSiteBadge identityType=" Kimi-AI " />)).toContain(`>${international}</span>`)
    expect(renderToStaticMarkup(<CredentialKimiSiteBadge identityType="kimi.ai" />)).toContain(`>${international}</span>`)
  })

  it.each(['claude', 'generic', 'kimi-future', '', null, undefined])('renders nothing for type %s', (identityType) => {
    expect(renderToStaticMarkup(<CredentialKimiSiteBadge identityType={identityType} />)).toBe('')
  })
})
