import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const styles = readFileSync(new URL('../LoginPage.module.scss', import.meta.url), 'utf8')
const rule = (name: string) => styles.match(new RegExp(`\\.${name}\\s*\\{([\\s\\S]*?)\n\\}`))![1]

describe('LoginPage layout styles', () => {
  it('fills the app main area with a naturally sized login card', () => {
    expect(rule('pageShell')).toContain('flex: 1 1 auto;')
    expect(rule('pageShell')).toContain('min-height: 0;')
    expect(rule('loginCard')).toContain('width: 100%;')
    expect(rule('loginCard')).toContain('min-height: 0;')
    expect(rule('loginCard')).toContain('box-sizing: border-box;')
    expect(rule('form')).toContain('flex: 0 0 auto;')
  })

  it('uses one column on mobile and resets desktop offsets', () => {
    expect(rule('frame')).toMatch(/@include mobile\s*\{[^}]*grid-template-columns:\s*1fr;/)
    for (const selector of ['brandBlock', 'utilityDock']) {
      expect(rule(selector)).toMatch(/@include mobile\s*\{[^}]*transform:\s*none;/)
    }
  })

  it('preserves intentional line breaks in localized login titles', () => {
    expect(rule('title')).toContain('white-space: pre-line;')
  })
})
