import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const styles = readFileSync(new URL('../CredentialRequestEventsList.module.scss', import.meta.url), 'utf8')

const styleRuleBlock = (selector: string): string => {
  const start = styles.indexOf(`${selector} {`)
  expect(start).toBeGreaterThanOrEqual(0)
  const end = styles.indexOf('\n}', start)
  expect(end).toBeGreaterThan(start)
  return styles.slice(start, end + 2)
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

  it('shows keyboard focus on token and cache tooltip targets', () => {
    expect(styles).toMatch(/\.requestEventsMetricCell\s*\{[\s\S]*?cursor:\s*default;[\s\S]*?&:\s*focus-visible\s*\{[\s\S]*?outline:\s*2px solid var\(--primary-color\);/)
  })

  it('uses the spare client-context row for a two-line User Agent value', () => {
    expect(styles).toMatch(/\.detailUserAgentValue\s*\{[\s\S]*?display:\s*-webkit-box;[\s\S]*?-webkit-line-clamp:\s*2;[\s\S]*?overflow-wrap:\s*anywhere;[\s\S]*?white-space:\s*normal;/)
  })
})
