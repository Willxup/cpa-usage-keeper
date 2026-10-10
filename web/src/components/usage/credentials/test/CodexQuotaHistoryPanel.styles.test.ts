import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const quotaHistoryStylesUrl = new URL('../CodexQuotaHistoryPanel.module.scss', import.meta.url)
const quotaHistoryStyles = readFileSync(quotaHistoryStylesUrl, 'utf8')

const scssRule = (selector: string) => {
  const start = quotaHistoryStyles.indexOf(selector)
  expect(start).toBeGreaterThanOrEqual(0)
  const openingBrace = quotaHistoryStyles.indexOf('{', start + selector.length)
  expect(openingBrace).toBeGreaterThan(start)
  let depth = 1
  for (let index = openingBrace + 1; index < quotaHistoryStyles.length; index += 1) {
    if (quotaHistoryStyles[index] === '{') depth += 1
    if (quotaHistoryStyles[index] === '}' && --depth === 0) return quotaHistoryStyles.slice(start, index + 1)
  }
  throw new Error(`Unclosed SCSS rule: ${selector}`)
}

describe('Codex quota history styles', () => {

  it('gives the window selector a visible keyboard focus indicator', () => {
    expect(quotaHistoryStyles).toMatch(/&:focus-visible\s*\{[\s\S]*?outline:\s*2px solid var\(--primary-color\);[\s\S]*?outline-offset:\s*2px;/)
  })

  it('keeps chart facts available to screen readers', () => {
    expect(quotaHistoryStyles).toMatch(/\.screenReaderOnly\s*\{[\s\S]*?clip-path:\s*inset\(50%\);/)
  })

  it('wraps selected-cycle ranges as complete content blocks at every width', () => {
    expect(quotaHistoryStyles).toMatch(/\.chartMeta\s*\{[\s\S]*?display:\s*flex;[\s\S]*?flex-wrap:\s*wrap;/)
    expect(quotaHistoryStyles).toMatch(/\.chartCycleRange,\s*\.chartObservedRange\s*\{[\s\S]*?min-width:\s*0;[\s\S]*?flex:\s*0 1 auto;/)
    expect(quotaHistoryStyles).not.toMatch(/@include mobile\s*\{[\s\S]*?\.chartCycleRange,\s*\.chartObservedRange/)
  })

  it('collapses current and completed summaries into the same shared-column narrow layout', () => {
    const cycleSummaryStyles = quotaHistoryStyles.slice(
      quotaHistoryStyles.indexOf('.cycleSummary {'),
      quotaHistoryStyles.indexOf('@container quota-cycle-card'),
    )
    const narrowCycleSummaryStyles = quotaHistoryStyles.slice(
      quotaHistoryStyles.indexOf('@container quota-cycle-card'),
      quotaHistoryStyles.indexOf('.currentStatus,'),
    )
    expect(quotaHistoryStyles).toMatch(/\.cycleCard\s*\{[\s\S]*?container:\s*quota-cycle-card \/ inline-size;/)
    expect(cycleSummaryStyles).toMatch(/>\s*\.chartSummaryRow\s*\{[\s\S]*?border-left:\s*0;/)
    expect(cycleSummaryStyles).toMatch(/\.completedCycleSummary\s*\{[\s\S]*?grid-template-columns:\s*repeat\(2, minmax\(0, 1fr\)\);/)
    expect(cycleSummaryStyles).toMatch(/\.completedCycleSummary\s*\{[\s\S]*?\.chartSummaryRow:nth-child\(n \+ 3\)\s*\{[\s\S]*?border-top:\s*1px solid var\(--border-color\);/)
    expect(cycleSummaryStyles).toMatch(/\.currentCycleSummary\s*\{[\s\S]*?grid-template-columns:\s*repeat\(3, minmax\(0, 1fr\)\);/)
    expect(cycleSummaryStyles).toMatch(/dd\s*\{[\s\S]*?grid-template-columns:\s*repeat\(3, max-content\);[\s\S]*?justify-content:\s*center;/)
    expect(cycleSummaryStyles).toMatch(/\.chartSummaryMetric\s*\{[\s\S]*?grid-template-columns:\s*14px max-content;/)
    expect(narrowCycleSummaryStyles).toMatch(/@container quota-cycle-card \(max-width:\s*640px\)/)
    expect(narrowCycleSummaryStyles).toMatch(/\.currentCycleSummary,\s*\.completedCycleSummary\s*\{[\s\S]*?grid-template-columns:\s*repeat\(3, minmax\(0, max-content\)\);/)
    expect(narrowCycleSummaryStyles).toMatch(/\.chartSummaryRow:nth-child\(n\)\s*\{[\s\S]*?grid-column:\s*1 \/ -1;[\s\S]*?grid-template-columns:\s*subgrid;[\s\S]*?grid-template-rows:\s*max-content max-content;/)
    expect(narrowCycleSummaryStyles).toMatch(/\.chartSummaryRow:nth-child\(n\)\s*\{[\s\S]*?margin-top:\s*0;[\s\S]*?border-top:\s*0;[\s\S]*?border-left:\s*0;/)
    expect(narrowCycleSummaryStyles).toMatch(/dt\s*\{[\s\S]*?text-align:\s*center;/)
    expect(narrowCycleSummaryStyles).toMatch(/dd\s*\{[\s\S]*?grid-column:\s*1 \/ -1;[\s\S]*?grid-template-columns:\s*subgrid;[\s\S]*?column-gap:\s*inherit;/)
    expect(narrowCycleSummaryStyles).not.toMatch(/@container quota-cycle-card \(max-width:\s*560px\)/)
  })

  it('keeps transition details four-column until the cycle card narrows, then two-column at every viewport', () => {
    const transitionGrid = scssRule('.transitionHeader,')
    const compactTransitionGrid = scssRule('@container quota-cycle-card (max-width: 720px)')
    const mobileStyles = scssRule('@include mobile')

    expect(transitionGrid).toContain('grid-template-columns: minmax(126px, 0.8fr) minmax(170px, 1.2fr) minmax(125px, 0.9fr) minmax(145px, 1fr);')
    expect(compactTransitionGrid).toMatch(/\.transitionHeader\s*\{[\s\S]*?display:\s*none;/)
    expect(compactTransitionGrid).toMatch(/\.transitionRow\s*\{[\s\S]*?grid-template-columns:\s*repeat\(2, minmax\(0, 1fr\)\);/)
    expect(mobileStyles).not.toContain('.transitionRow')
    expect(quotaHistoryStyles).not.toMatch(/\.transitionRow\s*\{\s*grid-template-columns:\s*1fr;/)
  })
})
