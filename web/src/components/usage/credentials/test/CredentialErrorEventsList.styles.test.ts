import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const styles = readFileSync(new URL('../CredentialErrorEventsList.module.scss', import.meta.url), 'utf8')

const scssRule = (selector: string) => {
  const start = styles.indexOf(selector)
  expect(start).toBeGreaterThanOrEqual(0)
  const openingBrace = styles.indexOf('{', start + selector.length)
  expect(openingBrace).toBeGreaterThan(start)

  let depth = 1
  for (let index = openingBrace + 1; index < styles.length; index += 1) {
    if (styles[index] === '{') depth += 1
    if (styles[index] === '}' && --depth === 0) return styles.slice(start, index + 1)
  }
  throw new Error(`Unclosed SCSS rule: ${selector}`)
}

describe('Credential error event list styles', () => {
  it('separates error cards and their body from the drawer surface', () => {
    expect(scssRule('.card')).toContain('border-radius: var(--keeper-card-radius)')
    expect(scssRule('.card')).toContain('background: var(--bg-secondary)')
    expect(scssRule('.body')).toContain('border-radius: var(--keeper-card-radius)')
    expect(scssRule('.body')).toContain('background: var(--bg-tertiary)')
    expect(scssRule('.body')).toContain('border: 1px solid var(--border-color)')
    expect(scssRule('.body')).toContain('color: var(--text-primary)')
    expect(scssRule('.badges .statusCode')).toContain('.badges .errorCode')
    expect(scssRule('.badges .statusCode')).toContain('color: #c94332')
    expect(scssRule(":global([data-theme='dark'])")).toContain('color: var(--failure-badge-text)')
    expect(scssRule('.retryTimes')).toContain('dt {\n    color: var(--text-secondary)')
  })
})
