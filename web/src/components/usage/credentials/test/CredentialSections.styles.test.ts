import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const credentialStyles = readFileSync(new URL('../CredentialSections.module.scss', import.meta.url), 'utf8')

const cssBlock = (selector: string) => {
  const start = credentialStyles.indexOf(selector)
  expect(start).toBeGreaterThanOrEqual(0)
  const next = credentialStyles.indexOf('\n.', start + selector.length)
  return credentialStyles.slice(start, next === -1 ? undefined : next)
}

const scssRule = (source: string, selector: string, occurrence = 0) => {
  let start = -selector.length
  for (let index = 0; index <= occurrence; index += 1) {
    start = source.indexOf(selector, start + selector.length)
    expect(start).toBeGreaterThanOrEqual(0)
  }

  const openingBrace = source.indexOf('{', start + selector.length)
  expect(openingBrace).toBeGreaterThan(start)
  let depth = 1
  for (let index = openingBrace + 1; index < source.length; index += 1) {
    if (source[index] === '{') {
      depth += 1
    } else if (source[index] === '}' && --depth === 0) {
      return source.slice(start, index + 1)
    }
  }
  throw new Error(`Unclosed SCSS rule: ${selector}`)
}

describe('Credential section layout and accessibility', () => {
  it('wraps the section heading and actions on mobile', () => {
    expect(credentialStyles).toMatch(/\.credentialSectionHeader\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\) auto;/)
    expect(credentialStyles).toMatch(/@include mobile\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\);[\s\S]*?\.credentialSectionTitleRow\s*\{[\s\S]*?flex-wrap:\s*wrap;/)
    expect(cssBlock('.credentialSectionTitleBlock')).toContain('min-width: 0;')
  })

  it('stacks both credential row types on narrow screens', () => {
    expect(credentialStyles).toMatch(/\.authFileCredentialRow\s*\{[\s\S]*?@include tablet\s*\{[\s\S]*?grid-template-columns:\s*1fr;/)
    expect(credentialStyles).toMatch(/\.authFileCredentialRow\s*\{[\s\S]*?@include mobile\s*\{[\s\S]*?grid-template-columns:\s*1fr;/)
    expect(credentialStyles).toMatch(/\.aiProviderCredentialRow\s*\{[\s\S]*?@include tablet\s*\{[\s\S]*?grid-template-columns:\s*1fr;/)
    expect(credentialStyles).toMatch(/\.aiProviderCredentialRow\s*\{[\s\S]*?@include mobile\s*\{[\s\S]*?grid-template-columns:\s*1fr;/)
  })

  it('bounds quota bars and reveals wrapped group tooltips on hover or keyboard focus', () => {
    expect(credentialStyles).toMatch(/\.credentialQuotaSideWithAction\s*\{[\s\S]*?grid-template-columns:\s*minmax\(350px, 1fr\) 30px;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaBars\s*\{[\s\S]*?grid-template-columns:\s*repeat\(2, minmax\(150px, 1fr\)\);/)
    expect(credentialStyles).toMatch(/\.credentialQuotaBarBlock\s*\{[\s\S]*?min-width:\s*150px;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaGroupBlock\s*\{[\s\S]*?grid-column:\s*1 \/ -1;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaGroupBars\s*\{[\s\S]*?grid-template-columns:\s*repeat\(2, minmax\(150px, 1fr\)\);/)
    expect(credentialStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.credentialQuotaGroupBars\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\);/)
    expect(credentialStyles).toMatch(/\.credentialQuotaGroupTooltip\s*\{[\s\S]*?opacity:\s*0;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaGroupTooltip\s*\{[\s\S]*?max-width:\s*min\(240px, 70vw\);/)
    expect(credentialStyles).toMatch(/\.credentialQuotaGroupTooltip\s*\{[\s\S]*?overflow-wrap:\s*anywhere;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaGroupTooltipTarget \.credentialQuotaGroupTooltip\s*\{[\s\S]*?white-space:\s*normal;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaGroupTooltipTarget:hover \.credentialQuotaGroupTooltip[\s\S]*?opacity:\s*1;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaGroupTooltipTarget:focus-visible \.credentialQuotaGroupTooltip[\s\S]*?opacity:\s*1;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaBarTooltipRight \.credentialQuotaGroupTooltip\s*\{[\s\S]*?right:\s*0;[\s\S]*?left:\s*auto;/)
    expect(credentialStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.credentialHealthPanel\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\);/)
  })

  it('reveals health bucket tooltips on hover', () => {
    expect(credentialStyles).toMatch(/\.credentialHealthCell\s*\{[\s\S]*?position:\s*relative;/)
    expect(credentialStyles).toMatch(/\.credentialHealthTooltip\s*\{[\s\S]*?position:\s*absolute;/)
    expect(credentialStyles).toMatch(/\.credentialHealthTooltip\s*\{[\s\S]*?opacity:\s*0;/)
    expect(credentialStyles).toMatch(/\.credentialHealthCell:hover \.credentialHealthTooltip[\s\S]*?opacity:\s*1;/)
    expect(credentialStyles).not.toMatch(/\.credentialHealthCell:focus-visible/)
  })

  it('keeps shared credential table headers out of narrow row stacking', () => {
    expect(credentialStyles).toMatch(/@include tablet\s*\{[\s\S]*?\.credentialTableHeader\.authFileCredentialRow[\s\S]*?display:\s*none;/)
    expect(credentialStyles).toMatch(/@include tablet\s*\{[\s\S]*?\.credentialTableHeader\.aiProviderCredentialRow[\s\S]*?display:\s*none;/)
  })

  it('keeps Auth Files quota actions inside the mobile card boundary', () => {
    expect(credentialStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.credentialQuotaSideWithAction\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\) auto;/)
    expect(credentialStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.credentialQuotaBars\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\);/)
    expect(credentialStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.credentialQuotaBarBlock\s*\{[\s\S]*?min-width:\s*0;/)
  })

  it('keeps expiry tooltips above scrolling content without intercepting pointers', () => {
    expect(credentialStyles).toMatch(/\.credentialExpiryTooltip\s*\{[\s\S]*?position:\s*fixed;/)
    expect(credentialStyles).toMatch(/\.credentialExpiryTooltip\s*\{[\s\S]*?pointer-events:\s*none;/)
    expect(credentialStyles).toMatch(/\.credentialExpiryTooltip\s*\{[\s\S]*?white-space:\s*nowrap;/)
    expect(cssBlock('.credentialExpiryTooltip')).not.toContain('overflow-wrap:')
  })

  it('allows long alias names to wrap beside their edit action', () => {
    expect(credentialStyles).toMatch(/\.credentialAliasDisplayLayout\s*\{[\s\S]*?display:\s*grid;/)
    expect(credentialStyles).toMatch(/\.credentialAliasDisplayLayout\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\) 28px;/)
    expect(credentialStyles).toMatch(/\.credentialAliasNameSlot\s*\{[\s\S]*?min-width:\s*0;/)
    expect(credentialStyles).toMatch(/\.credentialAliasNameSlot\s*\{[\s\S]*?overflow-wrap:\s*anywhere;/)
  })

  it('bounds and scrolls the inspection results table', () => {
    expect(credentialStyles).toMatch(/\.credentialInspectionResultsTable\s*\{[\s\S]*?max-height:\s*min\(52vh, 520px\);/)
    expect(credentialStyles).toMatch(/\.credentialInspectionResultsTable\s*\{[\s\S]*?overflow-y:\s*auto;/)
  })

  it('keeps Auth Files quota reset popovers visible outside the section card', () => {
    expect(credentialStyles).toMatch(/\.credentialSectionCard\s*\{[\s\S]*?overflow:\s*hidden;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaResetAction\s*\{[\s\S]*?position:\s*relative;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaResetPopover\s*\{[\s\S]*?position:\s*fixed;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaResetPopover\s*\{[\s\S]*?max-height:\s*calc\(100vh - 24px\);/)
    expect(credentialStyles).toMatch(/\.credentialQuotaResetPopover\s*\{[\s\S]*?overflow-y:\s*auto;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaResetExpiryList\s*\{[\s\S]*?max-height:\s*min\(40vh, 220px\);/)
    expect(credentialStyles).toMatch(/\.credentialQuotaResetExpiryList\s*\{[\s\S]*?overflow-y:\s*auto;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaResetActions\s*\{[\s\S]*?flex-shrink:\s*0;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaResetTooltip\s*\{[\s\S]*?position:\s*absolute;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaResetAction:hover \.credentialQuotaResetTooltip[\s\S]*?opacity:\s*1;/)
    expect(credentialStyles).not.toContain('.credentialQuotaResetAction:focus-within .credentialQuotaResetTooltip')
    expect(cssBlock('.credentialSectionCard')).not.toContain('overflow: visible;')
  })

  it('wraps credential names and request metrics within their cells', () => {
    expect(cssBlock('.credentialQuotaErrorMessage')).toContain('white-space: normal;')
    expect(credentialStyles).toMatch(/\.credentialDisplayName\s*\{[\s\S]*?white-space:\s*normal;/)
    expect(credentialStyles).toMatch(/\.credentialDisplayName\s*\{[\s\S]*?overflow-wrap:\s*anywhere;/)
    expect(credentialStyles).toMatch(/\.credentialMetricValueCell\s*\{[\s\S]*?white-space:\s*normal;/)
    expect(credentialStyles).toMatch(/\.credentialRequestMetric\s*\{[\s\S]*?align-items:\s*baseline;/)
    expect(credentialStyles).toMatch(/\.credentialRequestMetric\s*\{[\s\S]*?flex-wrap:\s*wrap;/)
    expect(credentialStyles).toMatch(/\.credentialRequestMetric\s*\{[\s\S]*?white-space:\s*normal;/)
    expect(credentialStyles).toMatch(/\.credentialRequestBreakdown\s*\{[\s\S]*?display:\s*inline-flex;/)
    expect(credentialStyles).toMatch(/\.credentialRequestBreakdown\s*\{[\s\S]*?white-space:\s*nowrap;/)
  })

  it('uses theme-aware failure colors for quota errors', () => {
    expect(scssRule(credentialStyles, '.credentialQuotaStateError', 1)).toContain('color: var(--failure-badge-text)')
    expect(scssRule(credentialStyles, '.credentialInlineError', 1)).toContain('color: var(--failure-badge-text)')
    expect(scssRule(credentialStyles, '.credentialQuotaErrorSummary')).toContain('color: var(--failure-badge-text)')
    const code = scssRule(credentialStyles, '.credentialQuotaErrorCode')
    expect(code).toContain('border: 1px solid var(--failure-badge-border)')
    expect(code).toContain('background: var(--failure-badge-bg)')
    expect(code).toContain('color: var(--failure-badge-text)')
  })

  it('scrolls pagination controls on narrow screens and sizes sorting by its content', () => {
    expect(credentialStyles).toMatch(/@include tablet\s*\{[\s\S]*?\.credentialPagination\s*\{[\s\S]*?overflow-x:\s*auto;/)
    expect(credentialStyles).toMatch(/@include tablet\s*\{[\s\S]*?\.credentialPaginationControls\s*\{[\s\S]*?width:\s*max-content;/)
    expect(credentialStyles).toMatch(/@include tablet\s*\{[\s\S]*?\.credentialQuotaModeControl[\s\S]*?\.credentialPageSizeControl\s*\{[\s\S]*?flex:\s*0 0 auto;/)
    expect(credentialStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.credentialPagination\s*\{[\s\S]*?overflow-x:\s*auto;/)
    expect(credentialStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.credentialPaginationControls\s*\{[\s\S]*?width:\s*max-content;/)
    expect(credentialStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.credentialQuotaModeControl[\s\S]*?\.credentialPageSizeControl\s*\{[\s\S]*?flex:\s*0 0 auto;/)
    expect(credentialStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.credentialPageSizeControl\s*\{[\s\S]*?flex:\s*0 0 auto;/)
    expect(scssRule(credentialStyles, '.credentialPaginationSortControl')).toContain('display: inline-grid')
    expect(scssRule(credentialStyles, '.credentialPaginationSortSizer', 1)).toContain('visibility: hidden')
    expect(scssRule(credentialStyles, '.credentialPaginationSortSizer', 1)).toContain('white-space: nowrap')
    expect(scssRule(credentialStyles, '.credentialPaginationSortSelect', 1)).toContain('width: 100%')
  })

  it('collapses disabled schedules and keeps their form inside the viewport', () => {
    expect(credentialStyles).toMatch(/\.credentialAutoRefreshSettingsModal\s*\{[\s\S]*?max-width:\s*min\(620px, calc\(100vw - 28px\)\);/)
    expect(cssBlock('.credentialAutoRefreshScheduleArea')).toContain('grid-template-rows: 0fr;')
    expect(cssBlock('.credentialAutoRefreshScheduleAreaActive')).toContain('grid-template-rows: 1fr;')
    expect(cssBlock('.credentialAutoRefreshIntervalField')).toContain('max-width: 100%;')
  })

  it('disables detail name and arrow motion for reduced-motion users', () => {
    const reducedMotion = scssRule(credentialStyles, '@media (prefers-reduced-motion: reduce)')
    expect(reducedMotion).toContain('.credentialDetailNameText')
    expect(reducedMotion).toContain('.authFileCredentialRow:hover .credentialDetailNameArrow')
    expect(reducedMotion).toContain('animation: none')
  })

  it('keeps subscription animations limited to compositor transforms', () => {
    const keyframes = [...credentialStyles.matchAll(/@keyframes\s+(credentialPlanBadge\w+)/g)].map((match) => match[1])
    expect(keyframes.length).toBeGreaterThan(0)
    for (const keyframe of keyframes) {
      const declarations = [...scssRule(credentialStyles, `@keyframes ${keyframe}`).matchAll(/^\s*([\w-]+)\s*:/gm)].map((match) => match[1])
      expect(new Set(declarations), keyframe).toEqual(new Set(['transform']))
    }
    expect(credentialStyles).not.toContain('background-position:')
    expect(credentialStyles).not.toContain('will-change:')
  })

  it('disables badge motion for reduced-motion and slow-update devices', () => {
    expect(credentialStyles).toMatch(/prefers-reduced-motion:\s*reduce\),\s*\(update:\s*slow\)[\s\S]*?\.credentialPlanBadgeFlow[\s\S]*?\.credentialPlanBadgeCorona[\s\S]*?animation:\s*none/)
    expect(credentialStyles).toMatch(/prefers-reduced-motion:\s*reduce\),\s*\(update:\s*slow\)[\s\S]*?\.credentialPlanBadgeEnterprise::before[\s\S]*?animation:\s*none/)
    expect(credentialStyles).not.toMatch(/prefers-reduced-motion:[\s\S]*?\.credentialPlanBadgeFree::before/)
  })
})
