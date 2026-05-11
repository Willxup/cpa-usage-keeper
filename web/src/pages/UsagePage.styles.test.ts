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

  it('keeps chart line selects aligned with reusable pill controls', () => {
    expect(chartLineSelectorSource).toContain('className={styles.usagePillControl}')
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
