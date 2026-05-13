import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const credentialStyles = readFileSync(new URL('./CredentialSections.module.scss', import.meta.url), 'utf8')
const credentialShellSource = readFileSync(new URL('./CredentialSectionShell.tsx', import.meta.url), 'utf8')

describe('Credential section styles', () => {
  it('keeps Auth Files and AI Provider identity columns at the requested width', () => {
    expect(credentialStyles).toMatch(/\.credentialRow\s*\{[\s\S]*?grid-template-columns:\s*minmax\(170px, 250px\) minmax\(394px, max-content\) minmax\(250px, 1fr\);/)
    expect(credentialStyles).toMatch(/\.credentialIdentityBlock\s*\{[\s\S]*?max-width:\s*250px;/)
    expect(credentialShellSource).toContain('<article className={styles.credentialRow}>')
  })

  it('balances the Auth Files quota spacing without fixing the quota bar width', () => {
    expect(credentialStyles).toMatch(/\.credentialRow\s*\{[\s\S]*?column-gap:\s*18px;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaSideWithAction\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\) 30px;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaSideWithAction\s*\{[\s\S]*?gap:\s*14px;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaBars\s*\{[\s\S]*?gap:\s*14px;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaBarBlock\s*\{[\s\S]*?min-width:\s*195px;/)
    expect(credentialStyles).not.toContain('credentialQuotaSidePanel')
    expect(credentialStyles).not.toContain('credentialQuotaRow')
  })

  it('keeps Total Requests success and failure counts horizontally aligned', () => {
    expect(credentialStyles).toMatch(/\.credentialRequestMetric\s*\{[\s\S]*?align-items:\s*center;/)
    expect(credentialStyles).toMatch(/\.credentialRequestBreakdown\s*\{[\s\S]*?display:\s*inline-flex;/)
    expect(credentialStyles).toMatch(/\.credentialRequestBreakdown\s*\{[\s\S]*?align-items:\s*center;/)
    expect(credentialStyles).toMatch(/\.credentialRequestBreakdown\s*\{[\s\S]*?line-height:\s*1;/)
  })

  it('uses a fixed centered pagination bar height', () => {
    expect(credentialStyles).toMatch(/\.credentialPagination\s*\{[\s\S]*?--usage-pagination-bar-height:\s*51px;/)
    expect(credentialStyles).toMatch(/\.credentialPagination\s*\{[\s\S]*?height:\s*var\(--usage-pagination-bar-height\);/)
    expect(credentialStyles).toMatch(/\.credentialPagination\s*\{[\s\S]*?box-sizing:\s*border-box;/)
    expect(credentialStyles).toMatch(/\.credentialPagination\s*\{[\s\S]*?align-items:\s*center;/)
    expect(credentialStyles).toMatch(/\.credentialPagination\s*\{[\s\S]*?padding:\s*0 22px;/)
  })

  it('keeps plan and remaining-day badges readable in dark mode', () => {
    expect(credentialStyles).toMatch(/\[data-theme='dark'\][\s\S]*\.credentialPlanBadgeTeam[\s\S]*?color:\s*#bbf7d0;/)
    expect(credentialStyles).toMatch(/\[data-theme='dark'\][\s\S]*\.credentialPlanBadgePlus[\s\S]*?color:\s*#bfdbfe;/)
    expect(credentialStyles).toMatch(/\[data-theme='dark'\][\s\S]*\.credentialPlanBadgePro[\s\S]*?color:\s*#fde68a;/)
    expect(credentialStyles).toMatch(/\[data-theme='dark'\][\s\S]*\.credentialRemainingDaysBadge[\s\S]*?color:\s*#bbf7d0;/)
  })
})
