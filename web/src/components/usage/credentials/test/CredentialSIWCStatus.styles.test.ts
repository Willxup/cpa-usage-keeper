import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const styles = readFileSync(new URL('../CredentialSections.module.scss', import.meta.url), 'utf8')

describe('SIWC status styles', () => {
  it('uses one halo-only breathing animation for all semantic colors', () => {
    expect(styles).toMatch(/\.credentialSIWCDot\s*\{[\s\S]*?width:\s*6px;[\s\S]*?height:\s*6px;[\s\S]*?color:\s*var\(--text-tertiary\)/)
    expect(styles).toMatch(/\.credentialSIWCDot\s*\{[\s\S]*?&::after\s*\{[\s\S]*?animation:\s*credential-siwc-breathe 2\.4s ease-in-out infinite/)
    expect(styles).toMatch(/data-siwc-state='not_enabled'[^\n]*var\(--quota-medium-color\)/)
    expect(styles).toMatch(/data-siwc-state='enabled'[^\n]*var\(--success-color\)/)
    expect(styles).toMatch(/data-siwc-state='invalid'[^\n]*var\(--danger-color\)/)
    const keyframes = styles.slice(styles.indexOf('@keyframes credential-siwc-breathe'), styles.indexOf('@media (prefers-reduced-motion: reduce)', styles.indexOf('@keyframes credential-siwc-breathe')))
    expect(keyframes).toContain('opacity:')
    expect(keyframes).toContain('transform:')
    expect(keyframes).not.toMatch(/width:|height:|margin:|padding:/)
    expect(styles).toMatch(/@media \(prefers-reduced-motion: reduce\)\s*\{\s*\.credentialSIWCDot::after\s*\{\s*animation:\s*none;/)
  })

  it('wraps the footer while preserving right-aligned action buttons', () => {
    expect(styles).toMatch(/\.credentialCardFooter\s*\{[^}]*flex-wrap:\s*wrap;/)
    expect(styles).toMatch(/\.credentialCardActions\s*\{[^}]*justify-content:\s*flex-end;[^}]*margin-left:\s*auto;/)
    expect(styles).toMatch(/\.credentialCard \.credentialCardActionButton\s*\{[^}]*width:\s*32px;[^}]*height:\s*32px;/)
  })
})
