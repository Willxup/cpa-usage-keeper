import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const usagePageStyles = readFileSync(new URL('./UsagePage.module.scss', import.meta.url), 'utf8')
const usagePageSource = readFileSync(new URL('./UsagePage.tsx', import.meta.url), 'utf8')
const requestEventsSource = readFileSync(new URL('../components/usage/RequestEventsDetailsCard.tsx', import.meta.url), 'utf8')
const priceSettingsSource = readFileSync(new URL('../components/usage/PriceSettingsCard.tsx', import.meta.url), 'utf8')
const chartLineSelectorSource = readFileSync(new URL('../components/usage/ChartLineSelector.tsx', import.meta.url), 'utf8')

describe('UsagePage toolbar styles', () => {
  it('keeps visible range controls content-sized in narrow layouts', () => {
    expect(usagePageStyles).toMatch(/\.timeRangeGroup\s*\{[\s\S]*?width:\s*fit-content;/)
    expect(usagePageStyles).toMatch(/\.timeRangeSelectControl\s*\{[\s\S]*?flex:\s*0 0 164px;/)
  })

  it('only renders custom range inputs when the custom range is selected', () => {
    expect(usagePageSource).toContain('{isCustomRange && (')
    expect(usagePageSource).not.toContain('aria-hidden={!isCustomRange}')
  })

  it('keeps custom date inputs selectable through the native picker without pointer interception', () => {
    expect(usagePageStyles).toMatch(/\.customRangeInput\s*\{[\s\S]*?user-select:\s*none;/)
    expect(usagePageStyles).toMatch(/\.customRangeInput\s*\{[\s\S]*?-webkit-user-select:\s*none;/)
    expect(usagePageSource).not.toContain('readOnly')
    expect(usagePageSource).not.toContain('onPointerDown={handleCustomDateInputPointerDown}')
    expect(usagePageSource).toContain('onClick={handleCustomDateInputActivate}')
    expect(usagePageSource).toContain('onFocus={handleCustomDateInputActivate}')
    expect(usagePageSource).toContain('onKeyDown={handleCustomDateInputKeyDown}')
  })

  it('keeps chart line selects aligned with reusable pill controls', () => {
    expect(chartLineSelectorSource).toContain('className={styles.usagePillControl}')
  })

  it('aligns Request Event Log pagination with credential pagination height', () => {
    expect(usagePageStyles).toMatch(/\.requestEventsCard:global\(\.card\)\s*\{[\s\S]*?padding-bottom:\s*0;/)
    expect(requestEventsSource).toContain('className={styles.requestEventsCard}')
    expect(usagePageStyles).toMatch(/\.requestEventsPaginationFooter\s*\{[\s\S]*?--usage-pagination-bar-height:\s*51px;/)
    expect(usagePageStyles).toMatch(/\.requestEventsPaginationFooter\s*\{[\s\S]*?height:\s*var\(--usage-pagination-bar-height\);/)
    expect(usagePageStyles).toMatch(/\.requestEventsPaginationFooter\s*\{[\s\S]*?box-sizing:\s*border-box;/)
    expect(usagePageStyles).toMatch(/\.requestEventsPaginationFooter\s*\{[\s\S]*?align-items:\s*center;/)
    expect(usagePageStyles).toMatch(/\.requestEventsPaginationFooter\s*\{[\s\S]*?padding:\s*0 #\{\$spacing-lg\};/)
  })

  it('keeps Request Event Log headers visible while the table scrolls', () => {
    expect(usagePageStyles).toMatch(/\.requestEventsTableWrapper\s*\{[\s\S]*?height:\s*clamp\(400px,\s*60vh,\s*600px\);/)
    expect(usagePageStyles).toMatch(/\.requestEventsTableWrapper\s*\{[\s\S]*?overflow:\s*auto;/)
    expect(usagePageStyles).toMatch(/\.requestEventsTableWrapper\s*\{[\s\S]*?thead\s+th\s*\{[\s\S]*?position:\s*sticky;/)
    expect(usagePageStyles).toMatch(/\.requestEventsTableWrapper\s*\{[\s\S]*?thead\s+th\s*\{[\s\S]*?top:\s*0;/)
    expect(usagePageStyles).toMatch(/\.requestEventsTableWrapper\s*\{[\s\S]*?thead\s+th\s*\{[\s\S]*?z-index:\s*2;/)
    expect(usagePageStyles).toMatch(/\.requestEventsTableWrapper\s*\{[\s\S]*?\.table\s*\{[\s\S]*?border-collapse:\s*separate;/)
  })

  it('keeps the Request Event Log timestamp column compact', () => {
    expect(usagePageStyles).toMatch(/\.requestEventsTimestamp\s*\{[\s\S]*?width:\s*136px;/)
    expect(usagePageStyles).toMatch(/\.requestEventsTimestamp\s*\{[\s\S]*?min-width:\s*136px;/)
    expect(usagePageStyles).toMatch(/\.requestEventsTimestamp\s*\{[\s\S]*?font-variant-numeric:\s*tabular-nums;/)
  })

  it('keeps the Request Event Log reasoning header on one line without fixing column width', () => {
    expect(usagePageStyles).toMatch(/\.requestEventsReasoningHeader\s*\{[\s\S]*?white-space:\s*nowrap;/)
    expect(usagePageStyles).not.toMatch(/\.requestEventsReasoningHeader\s*\{[^}]*width:/)
    expect(requestEventsSource).toContain('<th className={styles.requestEventsReasoningHeader}>{t(\'usage_stats.reasoning_tokens\')}</th>')
  })

  it('provides reusable pill controls for usage subpages', () => {
    expect(usagePageStyles).toMatch(/\.usagePillControl\s*\{[\s\S]*?border-radius:\s*999px;/)
    expect(usagePageStyles).toMatch(/\.usagePillAction\s*\{[\s\S]*?border-radius:\s*999px;/)
    expect(usagePageStyles).toMatch(/\.usagePillActionDanger\s*\{[\s\S]*?color:/)
    expect(usagePageStyles).not.toContain('&:global(.btn-danger):hover:not(:disabled)')
    expect(usagePageStyles).toMatch(/:global\(\.input\)\s*\{[^}]*border-radius:\s*999px;/)
    expect(requestEventsSource).toContain('styles.usagePillControl')
    expect(requestEventsSource).toContain('styles.usagePillAction')
    expect(priceSettingsSource).toContain('styles.usagePillControl')
    expect(priceSettingsSource).toContain('styles.usagePillAction')
    expect(priceSettingsSource).toContain('styles.usagePillActionDanger')
  })
})
