import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const drawerStyles = readFileSync(new URL('../CredentialDetailDrawer.module.scss', import.meta.url), 'utf8')

describe('CredentialDetailDrawer styles', () => {
  it('stacks identity and quota cards in the mobile Overview layout', () => {
    expect(drawerStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.overviewGrid\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\);/)
  })
})
