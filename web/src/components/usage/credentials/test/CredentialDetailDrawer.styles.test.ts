import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const drawerStyles = readFileSync(new URL('../CredentialDetailDrawer.module.scss', import.meta.url), 'utf8')

describe('CredentialDetailDrawer styles', () => {
  it('keeps the drawer vertical scrollbar present while model usage changes between loading and results', () => {
    const drawer = drawerStyles.match(/\.drawer\s*\{([\s\S]*?)\n\}/)?.[1] ?? ''
    expect(drawer).toMatch(/:global\(\.modal-body\)\s*\{[^}]*overflow-y:\s*scroll\s*;/)
  })

  it('keeps four cumulative cards on desktop and two on mobile', () => {
    expect(drawerStyles).toMatch(/\.summaryGrid\s*\{[^}]*grid-template-columns:\s*repeat\(4,\s*minmax\(0,\s*1fr\)\)/)
    expect(drawerStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.summaryGrid\s*\{\s*grid-template-columns:\s*repeat\(2,\s*minmax\(0,\s*1fr\)\)/)
    expect(drawerStyles).not.toMatch(/grid-template-columns:\s*1fr\s*;/)
  })

  it('stacks native quota windows and native groups without changing the list layout', () => {
    expect(drawerStyles).toMatch(/\.quotaRows \[data-quota-key\]\s*\{[^}]*grid-column:\s*1\s*\/\s*-1;/)
    expect(drawerStyles).toMatch(/\.quotaRows \[data-quota-group\]\s*\{[^}]*> div\s*\{\s*grid-column:\s*1\s*\/\s*-1;/)
  })

  it('aligns flat quota group tooltips inward on desktop and mobile inside the drawer', () => {
    const [desktopStyles, mobileStyles = ''] = drawerStyles.split('@include mobile')
    expect(desktopStyles).toMatch(/> \[role='tooltip'\]\s*\{[^}]*right:\s*0;\s*left:\s*auto;[\s\S]*?&::after\s*\{\s*right:\s*14px;\s*left:\s*auto;\s*\}/)
    expect(mobileStyles).toMatch(/> \[role='tooltip'\]\s*\{[^}]*right:\s*auto;\s*left:\s*0;[\s\S]*?&::after\s*\{\s*right:\s*auto;\s*left:\s*14px;\s*\}/)
  })
})
