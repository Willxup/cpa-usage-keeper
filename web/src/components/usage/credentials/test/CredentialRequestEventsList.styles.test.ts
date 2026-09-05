import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const styles = readFileSync(new URL('../CredentialRequestEventsList.module.scss', import.meta.url), 'utf8')

const styleRuleBlock = (selector: string): string => {
  const start = styles.indexOf(`${selector} {`)
  if (start < 0) return ''
  const end = styles.indexOf('\n}', start)
  return end < 0 ? styles.slice(start) : styles.slice(start, end + 2)
}

describe('CredentialRequestEventsList compact table styles', () => {
  it('uses content-driven widths while bounding the API Key and Model columns', () => {
    const tableBlock = styleRuleBlock('.table')
    const apiKeyBlock = styleRuleBlock('.apiKey')
    const modelBlock = styleRuleBlock('.model')

    expect(tableBlock).toContain('width: 100%;')
    expect(tableBlock).not.toContain('table-layout: fixed;')
    expect(tableBlock).not.toContain('min-width: 876px;')
    expect(apiKeyBlock).toContain('max-width: 240px;')
    expect(apiKeyBlock).not.toContain('min-width:')
    expect(modelBlock).toContain('min-width: 110px;')
    expect(modelBlock).toContain('max-width: 240px;')

    for (const selector of ['.timestamp', '.tokens', '.cache', '.performance', '.cost']) {
      expect(styleRuleBlock(selector)).not.toMatch(/(?:^|\n)\s*width:/)
    }
    expect(styles).not.toContain('.request {')
    expect(styles).not.toContain('.resultColumn {')
  })

  it('shows each expanded detail as one label-value row without allowing long metadata to overflow', () => {
    expect(styles).toMatch(/\.detailGroup\s*\{[\s\S]*?box-sizing:\s*border-box;[\s\S]*?overflow:\s*hidden;/)
    expect(styles).toMatch(/\.detailGrid\s*\{[\s\S]*?display:\s*grid;[\s\S]*?grid-template-columns:\s*max-content minmax\(0, 1fr\);[\s\S]*?column-gap:\s*16px;/)
    expect(styles).toMatch(/\.detailItem\s*\{[\s\S]*?display:\s*contents;/)
    expect(styles).toMatch(/\.detailItem\s*\{[\s\S]*?> span\s*\{[\s\S]*?margin:\s*0;[\s\S]*?> strong\s*\{[\s\S]*?max-width:\s*100%;[\s\S]*?text-overflow:\s*ellipsis;/)
  })

  it('distinguishes stacked metadata labels from their values', () => {
    expect(styles).toMatch(/\.subDataLabel\s*\{[\s\S]*?color:\s*var\(--text-secondary\);/)
  })

  it('uses the Request Event Log token and cache metric treatment in credential details', () => {
    expect(styles).toMatch(/\.requestEventsTokenMetricRow\s*,\s*\.requestEventsCacheMetrics\s*\{[\s\S]*?display:\s*flex;/)
    expect(styles).toMatch(/\.requestEventsTokenMetricInput\s*\{[\s\S]*?color:\s*#0284c7;/)
    expect(styles).toMatch(/\.requestEventsTokenMetricOutput\s*\{[\s\S]*?color:\s*#16a34a;/)
    expect(styles).toMatch(/\.requestEventsTokenMetricReasoning\s*\{[\s\S]*?color:\s*#8b5cf6;/)
    expect(styles).toMatch(/\.requestEventsCacheMetricRead\s*\{[\s\S]*?color:\s*#d97706;/)
    expect(styles).toMatch(/\.requestEventsCacheMetricWrite\s*\{[\s\S]*?color:\s*#db2777;/)
    expect(styles).toMatch(/\.requestEventsMetricIconSlot\s*\{[\s\S]*?width:\s*16px;[\s\S]*?height:\s*16px;/)
    expect(styles).toMatch(/\.requestEventsMetricCell\s*\{[\s\S]*?cursor:\s*default;[\s\S]*?&:\s*focus-visible\s*\{[\s\S]*?outline:\s*2px solid var\(--primary-color\);/)
  })

  it('uses the spare client-context row for a two-line User Agent value', () => {
    expect(styles).toMatch(/\.detailUserAgentValue\s*\{[\s\S]*?display:\s*-webkit-box;[\s\S]*?-webkit-line-clamp:\s*2;[\s\S]*?overflow-wrap:\s*anywhere;[\s\S]*?white-space:\s*normal;/)
  })
})
