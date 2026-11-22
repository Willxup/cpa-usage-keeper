import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const cardStyles = readFileSync(new URL('../cards/CredentialCards.module.scss', import.meta.url), 'utf8')
const sharedStyles = readFileSync(new URL('../CredentialSections.module.scss', import.meta.url), 'utf8')

describe('SIWC status styles', () => {
  it('uses one halo-only breathing animation for all semantic colors', () => {
    expect(cardStyles).toMatch(/\.credentialSIWCDot\s*\{[\s\S]*?width:\s*6px;[\s\S]*?height:\s*6px;[\s\S]*?color:\s*var\(--text-tertiary\)/)
    expect(cardStyles).toMatch(/\.credentialSIWCDot\s*\{[\s\S]*?&::after\s*\{[\s\S]*?animation:\s*credential-siwc-breathe 2\.4s ease-in-out infinite/)
    expect(cardStyles).toMatch(/data-siwc-state='not_enabled'[^\n]*var\(--quota-medium-color\)/)
    expect(cardStyles).toMatch(/data-siwc-state='enabled'[^\n]*var\(--success-color\)/)
    expect(cardStyles).toMatch(/data-siwc-state='invalid'[^\n]*var\(--danger-color\)/)
    const keyframes = cardStyles.slice(cardStyles.indexOf('@keyframes credential-siwc-breathe'), cardStyles.indexOf('@media (prefers-reduced-motion: reduce)', cardStyles.indexOf('@keyframes credential-siwc-breathe')))
    expect(keyframes).toContain('opacity:')
    expect(keyframes).toContain('transform:')
    expect(keyframes).not.toMatch(/width:|height:|margin:|padding:/)
    expect(cardStyles).toMatch(/@media \(prefers-reduced-motion: reduce\)\s*\{\s*\.credentialSIWCDot::after\s*\{\s*animation:\s*none;/)
  })

  it('keeps SIWC left and actions right even when coarse-pointer buttons wrap', () => {
    expect(cardStyles).toMatch(/\.credentialCardFooter\s*\{[^}]*flex-wrap:\s*wrap;/)
    expect(cardStyles).toMatch(/\.credentialSIWCStatus\s*\{[^}]*margin-right:\s*auto;/)
    expect(cardStyles).toMatch(/\.credentialCardActions\s*\{[^}]*justify-content:\s*flex-end;[^}]*margin-left:\s*auto;/)
    expect(sharedStyles).toMatch(/\[data-credential-card\] \.credentialCardActionButton\s*\{[^}]*width:\s*32px;[^}]*height:\s*32px;/)
    expect(sharedStyles).toMatch(/@media \(pointer: coarse\)(?:,\s*\(any-pointer: coarse\))?\s*\{[\s\S]*?\[data-credential-card\] \.credentialCardActionButton,[^}]*min-width:\s*44px;[^}]*min-height:\s*44px;/)
  })
})
