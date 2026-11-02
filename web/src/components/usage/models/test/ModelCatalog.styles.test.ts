import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const styles = readFileSync(new URL('../ModelCatalog.module.scss', import.meta.url), 'utf8')
const pageStyles = readFileSync(new URL('../../../../pages/UsagePage.module.scss', import.meta.url), 'utf8')
const viewport = styles.slice(styles.indexOf('.listViewport {'), styles.indexOf('.grid {'))
const sessions = pageStyles.slice(pageStyles.indexOf('.sessionSettingsBody {'), pageStyles.indexOf('.sessionSettingsList {'))

describe('model list height', () => {
  it('shares the Session Management scroll limit and shrinks for short lists', () => {
    for (const block of [viewport, sessions]) {
      expect(block).toMatch(/max-height:\s*var\(--settings-list-scroll-height\);/)
      expect(block).toMatch(/flex:\s*0 0 auto;/)
      expect(block).toMatch(/overflow-y:\s*auto;/)
      expect(block).not.toMatch(/(?:^|\n)\s*height:/)
    }
  })

  it('keeps the shared limit across desktop, tablet and mobile', () => {
    expect(styles).not.toContain('--models-list-height')
    const responsiveRules = styles.slice(styles.indexOf('@media'))
    expect(responsiveRules).not.toContain('--settings-list-scroll-height:')
    expect(responsiveRules).not.toMatch(/\.listViewport\s*\{[^}]*\b(?:max-)?height:/)
  })
})
