import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { compile } from 'sass'
import { describe, expect, it } from 'vitest'

const quotaHistoryStylesUrl = new URL('../CodexQuotaHistoryPanel.module.scss', import.meta.url)
const quotaHistoryStyles = readFileSync(quotaHistoryStylesUrl, 'utf8')
const compiledQuotaHistoryStyles = compile(fileURLToPath(quotaHistoryStylesUrl), { style: 'expanded' }).css

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
  it('uses the Keeper card contract for the two top-level sections', () => {
    expect(quotaHistoryStyles).toMatch(/\.card,\s*\.historySection\s*\{[\s\S]*?border-radius:\s*var\(--keeper-card-radius\);/)
    expect(quotaHistoryStyles).toMatch(/\.card,\s*\.historySection\s*\{[\s\S]*?box-shadow:\s*var\(--shadow-lg\);/)
    expect(quotaHistoryStyles).toMatch(/\.cycleCard\s*\{[\s\S]*?border-radius:\s*9px;/)
  })

  it('keeps the window selector in the existing pill-shaped segmented-control language', () => {
    expect(quotaHistoryStyles).toMatch(/\.windowSwitcher\s*\{[\s\S]*?border-radius:\s*999px;/)
    expect(quotaHistoryStyles).toMatch(/\.segmentButton\s*\{[\s\S]*?border-radius:\s*999px;/)
    expect(quotaHistoryStyles).toMatch(/&\[aria-pressed='true'\]\s*\{[\s\S]*?background:\s*var\(--bg-primary\);[\s\S]*?box-shadow:\s*0 6px 14px rgba\(0, 0, 0, 0\.08\);/)
    expect(quotaHistoryStyles).toMatch(/&:hover:not\(:disabled\)\s*\{[\s\S]*?color:\s*var\(--text-primary\);/)
    expect(quotaHistoryStyles).toMatch(/&:focus-visible\s*\{[\s\S]*?outline:\s*2px solid var\(--primary-color\);[\s\S]*?outline-offset:\s*2px;/)
  })

  it('centers the combined chart legend below the graph', () => {
    expect(quotaHistoryStyles).toMatch(/\.chartLegend\s*\{[\s\S]*?justify-content:\s*center;/)
    expect(quotaHistoryStyles).toMatch(/\.remainingLine\s*\{[\s\S]*?border-top:\s*1\.5px solid var\(--quota-remaining-line-color, rgba\(120, 113, 108, 0\.68\)\);/)
    expect(quotaHistoryStyles).toMatch(/\.costLine\s*\{[\s\S]*?border-top:\s*2px dashed var\(--quota-cost-line-color, #ff5a40\);/)
  })

  it('matches the Analysis header hint and keeps chart facts available to screen readers', () => {
    expect(quotaHistoryStyles).toMatch(/\.costHeaderHint\s*\{[\s\S]*?text-align:\s*right;/)
    expect(quotaHistoryStyles).toMatch(/\.screenReaderOnly\s*\{[\s\S]*?clip-path:\s*inset\(50%\);/)
  })

  it('wraps current-cycle ranges as complete content blocks at every width', () => {
    expect(quotaHistoryStyles).toMatch(/\.currentCycleMeta\s*\{[\s\S]*?display:\s*flex;[\s\S]*?flex-wrap:\s*wrap;/)
    expect(quotaHistoryStyles).toMatch(/\.currentCycleRange,\s*\.currentObservedRange\s*\{[\s\S]*?min-width:\s*0;[\s\S]*?flex:\s*0 1 auto;/)
    expect(quotaHistoryStyles).not.toMatch(/@include mobile\s*\{[\s\S]*?\.currentCycleRange,\s*\.currentObservedRange/)
  })

  it('switches chart summaries directly from three columns to top-labeled rows at one card width', () => {
    const summaryStyles = quotaHistoryStyles.slice(
      quotaHistoryStyles.indexOf('.chartSummary {'),
      quotaHistoryStyles.indexOf('.chartSummaryRow'),
    )
    const summaryRowStyles = quotaHistoryStyles.slice(
      quotaHistoryStyles.indexOf('.chartSummaryRow'),
      quotaHistoryStyles.indexOf('.chartSummaryMetric'),
    )
    const narrowSummaryStyles = quotaHistoryStyles.slice(
      quotaHistoryStyles.indexOf('@container quota-history-card'),
      quotaHistoryStyles.indexOf('.screenReaderOnly'),
    )
    expect(quotaHistoryStyles).toMatch(/\.card\s*\{[\s\S]*?container:\s*quota-history-card \/ inline-size;/)
    expect(summaryStyles).toContain('display: grid')
    expect(summaryStyles).toContain('grid-template-columns: repeat(3, minmax(0, 1fr))')
    expect(summaryStyles).toContain('border-top: 1px solid var(--border-color)')
    expect(summaryStyles).not.toContain('620px')
    expect(summaryRowStyles).toMatch(/\.chartSummaryRow\s*\{[\s\S]*?display:\s*grid;[\s\S]*?grid-template-rows:\s*max-content max-content;/)
    expect(summaryRowStyles).toMatch(/& \+ &\s*\{[\s\S]*?border-left:\s*1px solid var\(--border-color\);/)
    expect(summaryRowStyles).toMatch(/dd\s*\{[\s\S]*?display:\s*grid;[\s\S]*?grid-template-columns:\s*repeat\(3, max-content\);[\s\S]*?justify-content:\s*center;/)
    expect(quotaHistoryStyles).toMatch(/\.chartSummaryMetric\s*\{[\s\S]*?display:\s*grid;[\s\S]*?grid-template-columns:\s*14px max-content;/)
    expect(narrowSummaryStyles).toMatch(/@container quota-history-card \(max-width:\s*640px\)/)
    expect(narrowSummaryStyles).toMatch(/\.chartSummary\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\);/)
    expect(narrowSummaryStyles).toMatch(/\.chartSummaryRow\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\);[\s\S]*?grid-template-rows:\s*max-content max-content;/)
    expect(narrowSummaryStyles).toMatch(/dt\s*\{[\s\S]*?text-align:\s*left;/)
    expect(narrowSummaryStyles).toMatch(/dd\s*\{[\s\S]*?grid-template-columns:\s*repeat\(3, max-content\);[\s\S]*?justify-content:\s*start;/)
    expect(compiledQuotaHistoryStyles).toMatch(/@container quota-history-card \(max-width: 640px\) \{[\s\S]*?\.chartSummary > \.chartSummaryRow \+ \.chartSummaryRow \{\s*border-left: 0;/)
    expect(compiledQuotaHistoryStyles).not.toContain('.chartSummary > .chartSummaryRow + .chartSummary > .chartSummaryRow')
    expect(narrowSummaryStyles).not.toMatch(/@container quota-history-card \(max-width:\s*560px\)/)
  })

  it('collapses current and completed summaries into the same direct narrow layout', () => {
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
    expect(narrowCycleSummaryStyles).toMatch(/\.currentCycleSummary,\s*\.completedCycleSummary\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\);/)
    expect(narrowCycleSummaryStyles).toMatch(/\.chartSummaryRow:nth-child\(n\)\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\);[\s\S]*?grid-template-rows:\s*max-content max-content;/)
    expect(narrowCycleSummaryStyles).toMatch(/\.chartSummaryRow:nth-child\(n\)\s*\{[\s\S]*?margin-top:\s*0;[\s\S]*?border-top:\s*0;[\s\S]*?border-left:\s*0;/)
    expect(narrowCycleSummaryStyles).toMatch(/dt\s*\{[\s\S]*?text-align:\s*left;/)
    expect(narrowCycleSummaryStyles).toMatch(/dd\s*\{[\s\S]*?grid-template-columns:\s*repeat\(3, max-content\);[\s\S]*?justify-content:\s*start;/)
    expect(narrowCycleSummaryStyles).not.toMatch(/@container quota-cycle-card \(max-width:\s*560px\)/)
  })

  it('keeps transition details four-column until the cycle card narrows, then two-column at every viewport', () => {
    const transitionGrid = scssRule('.transitionHeader,')
    const compactTransitionGrid = scssRule('@container quota-cycle-card (max-width: 720px)')
    const tabletStyles = scssRule('@include tablet')
    const mobileStyles = scssRule('@include mobile')

    expect(transitionGrid).toContain('grid-template-columns: minmax(126px, 0.8fr) minmax(170px, 1.2fr) minmax(125px, 0.9fr) minmax(145px, 1fr);')
    expect(compactTransitionGrid).toMatch(/\.transitionHeader\s*\{[\s\S]*?display:\s*none;/)
    expect(compactTransitionGrid).toMatch(/\.transitionRow\s*\{[\s\S]*?grid-template-columns:\s*repeat\(2, minmax\(0, 1fr\)\);/)
    expect(tabletStyles).not.toContain('.transitionHeader')
    expect(tabletStyles).not.toContain('.transitionRow')
    expect(mobileStyles).not.toContain('.transitionRow')
    expect(quotaHistoryStyles).not.toMatch(/\.transitionRow\s*\{\s*grid-template-columns:\s*1fr;/)
  })
})
