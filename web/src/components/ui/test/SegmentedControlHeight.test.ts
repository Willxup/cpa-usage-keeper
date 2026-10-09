// @vitest-environment happy-dom
import { resolve } from 'node:path'
import { compile } from 'sass'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import languageStyles from '../LanguageSwitcher.module.scss'
import loginStyles from '../../../pages/LoginPage.module.scss'
import usageStyles from '../../../pages/UsagePage.module.scss'

type SegmentedControlFixture = {
  file: string
  hostSourceClass: string
  hostClass: string
  pillSourceClass: string
  pillClass: string
}

const fixtures: SegmentedControlFixture[] = [
  {
    file: 'src/components/ui/LanguageSwitcher.module.scss',
    hostSourceClass: 'languageSwitcher',
    hostClass: languageStyles.languageSwitcher,
    pillSourceClass: 'languagePill',
    pillClass: languageStyles.languagePill,
  },
  {
    file: 'src/pages/LoginPage.module.scss',
    hostSourceClass: 'themeSwitcher',
    hostClass: loginStyles.themeSwitcher,
    pillSourceClass: 'themePill',
    pillClass: loginStyles.themePill,
  },
  {
    file: 'src/pages/UsagePage.module.scss',
    hostSourceClass: 'themeSwitcher',
    hostClass: usageStyles.themeSwitcher,
    pillSourceClass: 'themePill',
    pillClass: usageStyles.themePill,
  },
]

const css = fixtures.map((fixture) => compile(resolve(process.cwd(), fixture.file)).css
  .replace(new RegExp(`\\.${fixture.hostSourceClass}\\b`, 'g'), `.${fixture.hostClass}`)
  .replace(new RegExp(`\\.${fixture.pillSourceClass}\\b`, 'g'), `.${fixture.pillClass}`)).join('\n')

describe('segmented control heights', () => {
  let stylesheet: HTMLStyleElement

  beforeEach(() => {
    document.documentElement.style.setProperty('--border-color', '#e3e1db')
    document.documentElement.style.setProperty('--bg-secondary', '#faf9f5')
    document.documentElement.style.setProperty('--bg-primary', '#f0eee8')
    document.documentElement.style.setProperty('--text-secondary', '#6d6760')
    stylesheet = document.createElement('style')
    stylesheet.textContent = css
    document.head.appendChild(stylesheet)
  })

  afterEach(() => {
    stylesheet.remove()
    document.body.replaceChildren()
    for (const property of ['--border-color', '--bg-secondary', '--bg-primary', '--text-secondary']) {
      document.documentElement.style.removeProperty(property)
    }
  })

  it.each(fixtures)('renders $file at 40px without pill overflow', (fixture) => {
    const host = document.createElement('div')
    host.className = fixture.hostClass
    const pill = document.createElement('button')
    pill.className = fixture.pillClass
    pill.textContent = 'Label'
    host.appendChild(pill)
    document.body.appendChild(host)

    const hostStyle = getComputedStyle(host)
    const pillStyle = getComputedStyle(pill)
    const availablePillHeight = 40
      - Number.parseFloat(hostStyle.paddingTop)
      - Number.parseFloat(hostStyle.paddingBottom)
      - Number.parseFloat(hostStyle.borderTopWidth)
      - Number.parseFloat(hostStyle.borderBottomWidth)
    const requiredPillHeight = Number.parseFloat(pillStyle.lineHeight)
      + Number.parseFloat(pillStyle.paddingTop)
      + Number.parseFloat(pillStyle.paddingBottom)

    expect(hostStyle.height).toBe('40px')
    expect(requiredPillHeight).toBeLessThanOrEqual(availablePillHeight)
  })
})
