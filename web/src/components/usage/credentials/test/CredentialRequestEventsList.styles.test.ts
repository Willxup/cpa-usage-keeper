import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const styles = readFileSync(new URL('../CredentialRequestEventsList.module.scss', import.meta.url), 'utf8')

describe('CredentialRequestEventsList compact table styles', () => {
  it('fits the eight-column table inside the default credential drawer before falling back to horizontal scrolling', () => {
    expect(styles).toContain('table-layout: fixed;')
    expect(styles).toContain('min-width: 876px;')
    expect(styles).toContain('box-sizing: border-box;')
    expect(styles).toContain('padding: 10px 8px;')
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

  it('uses the spare client-context row for a two-line User Agent value', () => {
    expect(styles).toMatch(/\.detailUserAgentValue\s*\{[\s\S]*?display:\s*-webkit-box;[\s\S]*?-webkit-line-clamp:\s*2;[\s\S]*?overflow-wrap:\s*anywhere;[\s\S]*?white-space:\s*normal;/)
  })
})
