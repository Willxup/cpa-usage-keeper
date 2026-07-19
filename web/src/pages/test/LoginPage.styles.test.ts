import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const loginPageSource = readFileSync(new URL('../LoginPage.tsx', import.meta.url), 'utf8').replace(/\r\n/g, '\n')
const loginPageStyles = readFileSync(new URL('../LoginPage.module.scss', import.meta.url), 'utf8').replace(/\r\n/g, '\n')
const shellStyles = readFileSync(new URL('../../components/layout/AppShell.module.scss', import.meta.url), 'utf8').replace(/\r\n/g, '\n')
const themeStyles = readFileSync(new URL('../../styles/themes.scss', import.meta.url), 'utf8').replace(/\r\n/g, '\n')


describe('LoginPage layout styles', () => {
  it('uses a single bounded form column at every viewport size', () => {
    expect(loginPageStyles).toMatch(/\.loginCard\s*\{[\s\S]*?width:\s*min\(440px, 100%\);/)
    expect(loginPageStyles).not.toContain('@include login-split')
    expect(loginPageStyles).toMatch(/\.loginColumn\s*\{[\s\S]*?justify-content:\s*center;/)
  })

  it('keeps the product identity in the shared header and the title out of the content column', () => {
    expect(loginPageSource).toContain('brand: <BrandLink className={styles.brandLink} />')
    expect(loginPageSource).toContain("headerTitle: t('auth.login_title')")
    expect(loginPageSource).not.toContain('<Typography.Title')
    expect(loginPageSource).not.toContain('auth.login_subtitle')
    expect(loginPageSource).not.toContain('auth.console_kicker')
    expect(loginPageSource).not.toContain('auth.console_title')
    expect(loginPageSource).not.toContain('auth.console_hint')
  })

  it('delegates login method and preferences to Ant Design controls', () => {
    expect(loginPageSource).toContain('<Tabs')
    expect(loginPageSource).toContain('<PreferencesDropdown />')
    expect(loginPageSource).not.toContain('<Segmented')
    expect(themeStyles).toMatch(/:root\s*\{[\s\S]*?--text-primary:\s*#192230;/)
    expect(themeStyles).toMatch(/\[data-theme='dark'\]\s*\{[\s\S]*?--text-primary:\s*#f1f5f9;/)
  })

  it('uses standard Ant Design form spacing and middle-sized submission', () => {
    expect(loginPageSource).toContain('htmlType="submit"')
    expect(loginPageSource).toContain('block')
    expect(loginPageSource).toContain('loading={loading}')
    expect(loginPageSource).not.toContain('size="large"')
    expect(loginPageSource).not.toContain('extra={t(\'auth.password_hint\')}')
    expect(loginPageSource).not.toContain('extra={t(\'auth.api_key_hint\')}')
    expect(loginPageStyles).not.toContain(':global(.ant-card-body)')
    expect(loginPageStyles).not.toContain(':global(.ant-form-item)')
  })

  it('lets the shared shell own header geometry while the page only overrides gutter tokens', () => {
    expect(loginPageStyles).toMatch(/\.guestShell\s*\{[\s\S]*?--page-gutter:\s*40px;/)
    expect(loginPageStyles).toMatch(/\.guestShell\s*\{[\s\S]*?--page-header-height:\s*88px;/)
    expect(loginPageStyles).not.toMatch(/\.pageShell\s*\{/)
    expect(loginPageStyles).not.toMatch(/\.frame\s*\{/)
    expect(loginPageStyles).not.toMatch(/\.utilityDock\s*\{/)
    expect(loginPageStyles).not.toMatch(/\.brandBlock\s*\{/)
    expect(shellStyles).toMatch(/\.shellHeaderInner\s*\{[\s\S]*?width:\s*min\(var\(--page-max-width\), 100%\);/)
  })
})
