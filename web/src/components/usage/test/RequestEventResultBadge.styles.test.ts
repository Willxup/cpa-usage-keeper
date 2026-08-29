import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const styles = readFileSync(new URL('../RequestEventResultBadge.module.scss', import.meta.url), 'utf8')

describe('RequestEventResultBadge sizes', () => {
  it('keeps the default Request Events badge size', () => {
    expect(styles).toMatch(/\.requestEventsResultSuccess,[\s\S]*?\.requestEventsResultFailed\s*\{[\s\S]*?width:\s*60px;[\s\S]*?font-size:\s*11px;/)
  })

  it('provides the credential request list with its previous compact footprint', () => {
    expect(styles).toMatch(/\.requestEventsResultCompact\s*\{[\s\S]*?width:\s*auto;[\s\S]*?min-height:\s*22px;[\s\S]*?padding:\s*3px 7px;[\s\S]*?font-size:\s*9px;[\s\S]*?font-weight:\s*800;/)
  })
})
