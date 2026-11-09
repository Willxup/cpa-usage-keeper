import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { compile } from 'sass'
import { describe, expect, it } from 'vitest'

const searchCss = compile(fileURLToPath(new URL('../SearchInput.module.scss', import.meta.url))).css
const catalogStyles = readFileSync(new URL('../../usage/models/ModelCatalog.module.scss', import.meta.url), 'utf8')
const pricingStyles = readFileSync(new URL('../../usage/pricing/PricingSettings.module.scss', import.meta.url), 'utf8')

describe('shared search input styles', () => {
  it('fills the available width at and below 600px without a catalog-only override', () => {
    expect(searchCss).toMatch(/@media\s*\(max-width:\s*600px\)\s*\{\s*\.search\s*\{\s*width:\s*100%;\s*\}/)
    expect(catalogStyles).not.toMatch(/\.search\s*\{/)
  })

  it('retains the desktop capsule dimensions and the pricing unit caption size', () => {
    const desktop = searchCss.match(/(?:^|\n)\.search\s*\{([^}]*)\}/)?.[1] ?? ''
    const input = searchCss.match(/(?:^|\n)\.search input\s*\{([^}]*)\}/)?.[1] ?? ''
    expect(desktop).toMatch(/width:\s*min\(360px,\s*100%\);/)
    expect(desktop).toMatch(/min-height:\s*36px;/)
    expect(desktop).toMatch(/padding:\s*0 12px;/)
    expect(desktop).toMatch(/border-radius:\s*999px;/)
    expect(input).toMatch(/padding:\s*9px 0;/)
    expect(input).toMatch(/font-size:\s*12px;/)
    expect(pricingStyles).toMatch(/\.priceUnit\s*\{[^}]*font-size:\s*11px;/)
  })
})
