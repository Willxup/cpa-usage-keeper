import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const styles = readFileSync(new URL('../CredentialModelUsage.module.scss', import.meta.url), 'utf8')
const rangeGroup = styles.match(/\.rangeGroup\s*\{([\s\S]*?)\n\}/)?.[1] ?? ''

describe('CredentialModelUsage range button styles', () => {
  it('does not flash a border or shadow while a range is loading', () => {
    expect(rangeGroup).not.toMatch(/\[aria-busy[^\]]*\][^}]*(?:box-shadow|border|outline)\s*:/)
  })

  it('softly transitions the selection, respects reduced motion, and retains keyboard focus', () => {
    expect(rangeGroup).toMatch(/transition:\s*background-color 160ms ease, color 160ms ease;/)
    expect(rangeGroup).toMatch(/@media\s*\(prefers-reduced-motion:\s*reduce\)\s*\{\s*transition:\s*none;/)
    expect(rangeGroup).toMatch(/&:focus-visible\s*\{\s*outline:/)
    expect(styles).not.toContain('cursor: help')
  })
})
