import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const loginPageStyles = readFileSync(new URL('../LoginPage.module.scss', import.meta.url), 'utf8').replace(/\r\n/g, '\n')
const themeStyles = readFileSync(new URL('../../styles/themes.scss', import.meta.url), 'utf8').replace(/\r\n/g, '\n')
const pageShellRules = loginPageStyles.match(/\.pageShell\s*\{[\s\S]*?\n\}/g) ?? []
const pageShellStyles = pageShellRules[0] ?? ''
const pageShellBackground = pageShellStyles.match(/background:\s*([\s\S]*?);/)?.[1] ?? ''

describe('LoginPage layout styles', () => {
  it('ends the login surface on the base theme background without an edge glow', () => {
    expect(pageShellRules).toHaveLength(1)
    expect(pageShellBackground.match(/radial-gradient\(/g)).toHaveLength(1)
    expect(pageShellBackground).toContain('radial-gradient(900px 480px at 12% 0%')
    expect(pageShellBackground.trim()).toMatch(/var\(--bg-secondary\)$/)
    expect(pageShellStyles).not.toMatch(/\bbackground-image\s*:/)
  })

  it('gives the desktop login columns enough room without relying on shared app zoom', () => {
    expect(loginPageStyles).toMatch(/\.frame\s*\{[\s\S]*?width:\s*min\(1180px, 100%\);/)
    expect(loginPageStyles).toMatch(/\.frame\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1\.15fr\) minmax\(360px, 440px\);/)
    expect(loginPageStyles).toMatch(/\.frame\s*\{[\s\S]*?gap:\s*64px;/)
  })

  it('keeps the compact single-column layout on mobile', () => {
    expect(loginPageStyles).toMatch(/@include mobile\s*\{[\s\S]*?grid-template-columns:\s*1fr;[\s\S]*?gap:\s*18px;/)
  })

  it('preserves intentional line breaks in localized login titles', () => {
    expect(loginPageStyles).toMatch(/\.title\s*\{[\s\S]*?white-space:\s*pre-line;/)
  })

  it('keeps the login card width stable when localized copy changes', () => {
    expect(loginPageStyles).toMatch(/\.loginCard\s*\{[\s\S]*?width:\s*100%;/)
    expect(loginPageStyles).toMatch(/\.loginCard\s*\{[\s\S]*?min-height:\s*440px;/)
    expect(loginPageStyles).toMatch(/\.loginCard\s*\{[\s\S]*?box-sizing:\s*border-box;/)
    expect(loginPageStyles).toMatch(/\.loginCard\s*\{[\s\S]*?display:\s*flex;/)
    expect(loginPageStyles).toMatch(/\.loginCard\s*\{[\s\S]*?flex-direction:\s*column;/)
    expect(loginPageStyles).toMatch(/\.loginCard\s*\{[\s\S]*?@include mobile\s*\{[\s\S]*?min-height:\s*0;/)
  })

  it('makes the active login method visible in dark mode surfaces', () => {
    expect(loginPageStyles).toMatch(/\.tabActive\s*\{[\s\S]*?border:\s*1px solid color-mix\(in srgb, var\(--primary-color\) 48%, var\(--border-color\)\);/)
    expect(loginPageStyles).toMatch(/\.tabActive\s*\{[\s\S]*?background:\s*color-mix\(in srgb, var\(--primary-color\) 18%, var\(--bg-primary\)\);/)
    expect(loginPageStyles).toMatch(/\.tabActive\s*\{[\s\S]*?box-shadow:\s*0 10px 24px rgba\(0, 0, 0, 0\.16\), inset 0 0 0 1px color-mix\(in srgb, var\(--text-primary\) 18%, transparent\);/)
    expect(themeStyles).toMatch(/:root\s*\{[\s\S]*?--text-primary:\s*#2d2a26;/)
    expect(themeStyles).toMatch(/\[data-theme='white'\]\s*\{[\s\S]*?--text-primary:\s*#2d2a26;/)
    expect(themeStyles).toMatch(/\[data-theme='dark'\]\s*\{[\s\S]*?--text-primary:\s*#f6f4f1;/)
  })

  it('keeps the submit action anchored inside the minimum-height login card', () => {
    expect(loginPageStyles).toMatch(/\.form\s*\{[\s\S]*?flex:\s*1 1 auto;/)
    expect(loginPageStyles).toMatch(/\.form\s*\{[\s\S]*?:global\(\.btn\)\s*\{[\s\S]*?margin-top:\s*auto;/)
  })
})
