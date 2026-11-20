import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const drawerStyles = readFileSync(new URL('../CredentialDetailDrawer.module.scss', import.meta.url), 'utf8')

describe('CredentialDetailDrawer styles', () => {
  it('keeps four cumulative cards on desktop and two on mobile', () => {
    expect(drawerStyles).toMatch(/\.summaryGrid\s*\{[^}]*grid-template-columns:\s*repeat\(4,\s*minmax\(0,\s*1fr\)\)/)
    expect(drawerStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.summaryGrid\s*\{\s*grid-template-columns:\s*repeat\(2,\s*minmax\(0,\s*1fr\)\)/)
    expect(drawerStyles).not.toMatch(/grid-template-columns:\s*1fr\s*;/)
  })

  it('stacks native quota windows and native groups without changing the list layout', () => {
    expect(drawerStyles).toMatch(/\.quotaRows \[data-credential-quota-variant\] > div,\s*\.quotaRows \[data-quota-group\] > div:last-child\s*\{\s*grid-template-columns:\s*minmax\(0,\s*1fr\)/)
  })
})
