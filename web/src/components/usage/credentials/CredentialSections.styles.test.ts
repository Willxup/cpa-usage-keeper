import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const credentialStyles = readFileSync(new URL('./CredentialSections.module.scss', import.meta.url), 'utf8')
const credentialShellSource = readFileSync(new URL('./CredentialSectionShell.tsx', import.meta.url), 'utf8')
const aiProviderSectionSource = readFileSync(new URL('./AiProviderCredentialsSection.tsx', import.meta.url), 'utf8')
const authFileSectionSource = readFileSync(new URL('./AuthFileCredentialsSection.tsx', import.meta.url), 'utf8')

const cssBlock = (selector: string) => {
  const start = credentialStyles.indexOf(selector)
  expect(start).toBeGreaterThanOrEqual(0)
  const next = credentialStyles.indexOf('\n.', start + selector.length)
  return credentialStyles.slice(start, next === -1 ? undefined : next)
}

describe('Credential section styles', () => {
  it('keeps Auth Files and AI Provider row sizing separate', () => {
    expect(credentialStyles).toMatch(/\.authFileCredentialRow\s*\{[\s\S]*?grid-template-columns:\s*minmax\(170px, 250px\) minmax\(394px, max-content\) minmax\(250px, 1fr\);/)
    expect(credentialStyles).toMatch(/\.authFileCredentialRow\s*\{[\s\S]*?\.credentialIdentityBlock\s*\{[\s\S]*?max-width:\s*250px;/)
    expect(credentialStyles).toMatch(/\.authFileCredentialRow\s*\{[\s\S]*?@include tablet\s*\{[\s\S]*?grid-template-columns:\s*1fr;/)
    expect(credentialStyles).toMatch(/\.authFileCredentialRow\s*\{[\s\S]*?@include mobile\s*\{[\s\S]*?grid-template-columns:\s*1fr;/)
    expect(credentialStyles).toMatch(/\.aiProviderCredentialRow\s*\{[\s\S]*?grid-template-columns:\s*300px minmax\(394px, max-content\) minmax\(250px, 1fr\);/)
    expect(credentialStyles).toMatch(/\.aiProviderCredentialRow\s*\{[\s\S]*?\.credentialIdentityBlock\s*\{[\s\S]*?max-width:\s*300px;/)
    expect(credentialStyles).toMatch(/\.aiProviderCredentialRow\s*\{[\s\S]*?@include tablet\s*\{[\s\S]*?grid-template-columns:\s*1fr;/)
    expect(credentialStyles).toMatch(/\.aiProviderCredentialRow\s*\{[\s\S]*?@include mobile\s*\{[\s\S]*?grid-template-columns:\s*1fr;/)
    expect(credentialShellSource).toContain('rowClassName?: string')
    expect(aiProviderSectionSource).toContain('rowClassName={styles.aiProviderCredentialRow}')
    expect(authFileSectionSource).toContain('rowClassName={styles.authFileCredentialRow}')
    expect(authFileSectionSource).not.toContain('aiProviderCredentialRow')
  })

  it('lets Auth Files quota bars wrap before their blocks overlap', () => {
    expect(credentialStyles).toMatch(/\.credentialRow\s*\{[\s\S]*?column-gap:\s*18px;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaSideWithAction\s*\{[\s\S]*?grid-template-columns:\s*minmax\(350px, 1fr\) 30px;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaSideWithAction\s*\{[\s\S]*?gap:\s*14px;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaBars\s*\{[\s\S]*?grid-template-columns:\s*repeat\(2, minmax\(150px, 1fr\)\);/)
    expect(credentialStyles).toMatch(/\.credentialQuotaBars\s*\{[\s\S]*?gap:\s*14px;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaBarBlock\s*\{[\s\S]*?min-width:\s*150px;/)
    expect(credentialStyles).not.toContain('credentialQuotaChips')
    expect(credentialStyles).not.toContain('credentialQuotaChip')
    expect(credentialStyles).not.toContain('credentialQuotaSidePanel')
    expect(credentialStyles).not.toContain('credentialQuotaRow')
  })

  it('stacks Auth Files quota error status above the message', () => {
    expect(credentialStyles).toMatch(/\.credentialQuotaErrorSummary\s*\{[\s\S]*?flex-direction:\s*column;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaErrorSummary\s*\{[\s\S]*?align-items:\s*flex-start;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaErrorMessage\s*\{[\s\S]*?white-space:\s*normal;/)
    expect(credentialStyles).toMatch(/\.credentialQuotaErrorMessage\s*\{[\s\S]*?line-height:\s*1\.35;/)
  })

  it('keeps Auth Files quota actions inside the mobile card boundary', () => {
    expect(credentialStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.credentialQuotaSideWithAction\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\) auto;/)
    expect(credentialStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.credentialQuotaBars\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\);/)
    expect(credentialStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.credentialQuotaBarBlock\s*\{[\s\S]*?min-width:\s*0;/)
  })

  it('renders compact priority badges without lengthening credential names', () => {
    expect(credentialStyles).toMatch(/\.credentialPriorityBadge\s*\{[\s\S]*?min-width:\s*22px;/)
    expect(credentialShellSource).toContain('CredentialPriorityBadge')
    expect(authFileSectionSource).toContain('row.priorityLabel')
    expect(authFileSectionSource).toMatch(/row\.planTypeLabel[\s\S]*?row\.remainingDaysLabel[\s\S]*?row\.priorityLabel/)
    expect(aiProviderSectionSource).toContain('row.priorityLabel')
  })

  it('keeps Auth Files inspection separate from the quota refresh pill', () => {
    expect(authFileSectionSource).toContain('credentialSectionActionButtons')
    expect(authFileSectionSource).toMatch(/credentialInspectionSwitcher[\s\S]*?credentialInspectionButton[\s\S]*?credentialRefreshSwitcher/)
    expect(authFileSectionSource).toContain('credentialInspectionButton')
    expect(authFileSectionSource).toMatch(/credentialInspectionButton[\s\S]*?credentialRefreshSwitcher/)
    expect(authFileSectionSource).toMatch(/credentialRefreshSwitcher\} \$\{styles\.credentialInspectionSwitcher[\s\S]*?credentialInspectionButton[\s\S]*?<\/div>\s*<div className=\{styles\.credentialRefreshSwitcher\}>[\s\S]*?credentialRefreshButtonLoading/)
    expect(credentialStyles).toMatch(/\.credentialInspectionSwitcher\s*\{[\s\S]*?border-radius:\s*999px;/)
    expect(credentialStyles).toMatch(/\.credentialInspectionSwitcher\s*\{[\s\S]*?background:\s*color-mix\(in srgb, var\(--bg-secondary\) 78%, transparent\);/)
    expect(credentialStyles).toMatch(/\.credentialInspectionProgressTrack\s*\{[\s\S]*?height:\s*8px;/)
    expect(credentialStyles).toMatch(/\.credentialInspectionProgressTrack\s*\{[\s\S]*?background:\s*var\(--bg-tertiary\);/)
    expect(credentialStyles).toMatch(/\.credentialInspectionProgressTrack\s*\{[\s\S]*?border:\s*1px solid var\(--border-color\);/)
    expect(credentialStyles).toMatch(/\.credentialInspectionProgressFill\s*\{[^}]*background:\s*linear-gradient\(90deg, #15803d, #22c55e\);/)
    expect(credentialStyles).not.toMatch(/\.credentialInspectionProgressFill\s*\{[^}]*box-shadow:/)
    expect(credentialStyles).not.toContain('.credentialInspectionProgressFill::after')
    expect(credentialStyles).toMatch(/\.credentialInspectionSummary\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 80px\) minmax\(260px, 1fr\) auto;/)
    expect(credentialStyles).toMatch(/\.credentialInspectionSummary\s*\{[\s\S]*?align-items:\s*start;/)
    expect(credentialStyles).toMatch(/\.credentialInspectionSummary\s*\{[\s\S]*?gap:\s*40px;/)
    expect(credentialStyles).toMatch(/\.credentialInspectionSummary\s*\{[\s\S]*?padding-bottom:\s*34px;/)
    expect(credentialStyles).toMatch(/\.credentialInspectionMetric\s*\{[\s\S]*?align-self:\s*start;/)
    expect(credentialStyles).toMatch(/\.credentialInspectionMetric\s*\{[\s\S]*?align-content:\s*start;/)
    expect(credentialStyles).toMatch(/\.credentialInspectionStartButton\s*\{[\s\S]*?align-self:\s*center;/)
    expect(cssBlock('.credentialInspectionStatCardUnauthorized')).toContain('color: var(--danger-color);')
    expect(cssBlock('.credentialInspectionStatusUnauthorized')).toContain('background: color-mix(in srgb, var(--danger-color) 12%, transparent);')
    expect(cssBlock('.credentialInspectionStatusUnauthorized')).toContain('color: var(--danger-color);')
    expect(authFileSectionSource).toContain('status?.completed_at')
    expect(authFileSectionSource).toContain('role="progressbar"')
    expect(authFileSectionSource).toContain('aria-valuenow={progress}')
    expect(authFileSectionSource).toContain('aria-valuemin={0}')
    expect(authFileSectionSource).toContain('aria-valuemax={100}')
  })

  it('keeps Total Requests fixed and wraps the breakdown only when it overflows', () => {
    expect(credentialStyles).toMatch(/\.credentialMetricGroup\s*\{[\s\S]*?grid-template-columns:\s*109px repeat\(3, 95px\);/)
    expect(credentialStyles).toMatch(/\.credentialRequestMetric\s*\{[\s\S]*?align-items:\s*baseline;/)
    expect(credentialStyles).toMatch(/\.credentialRequestMetric\s*\{[\s\S]*?flex-wrap:\s*wrap;/)
    expect(credentialStyles).toMatch(/\.credentialRequestMetric\s*\{[\s\S]*?white-space:\s*normal;/)
    expect(credentialStyles).toMatch(/\.credentialRequestBreakdown\s*\{[\s\S]*?display:\s*inline-flex;/)
    expect(credentialStyles).toMatch(/\.credentialRequestBreakdown\s*\{[\s\S]*?white-space:\s*nowrap;/)
    expect(credentialStyles).not.toMatch(/\.credentialRequestBreakdown\s*\{[\s\S]*?flex-basis:\s*100%;/)
    expect(credentialStyles).toMatch(/\.credentialRequestBreakdown\s*\{[\s\S]*?line-height:\s*1\.2;/)
  })

  it('uses a fixed centered pagination bar height', () => {
    expect(credentialStyles).toMatch(/\.credentialPagination\s*\{[\s\S]*?--usage-pagination-bar-height:\s*51px;/)
    expect(credentialStyles).toMatch(/\.credentialPagination\s*\{[\s\S]*?height:\s*var\(--usage-pagination-bar-height\);/)
    expect(credentialStyles).toMatch(/\.credentialPagination\s*\{[\s\S]*?box-sizing:\s*border-box;/)
    expect(credentialStyles).toMatch(/\.credentialPagination\s*\{[\s\S]*?align-items:\s*center;/)
    expect(credentialStyles).toMatch(/\.credentialPagination\s*\{[\s\S]*?padding:\s*0 22px;/)
    expect(credentialStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.credentialPagination\s*\{[\s\S]*?overflow-x:\s*auto;/)
    expect(credentialStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.credentialPaginationControls\s*\{[\s\S]*?width:\s*max-content;/)
    expect(credentialStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.credentialPageSizeControl\s*\{[\s\S]*?flex:\s*0 0 auto;/)
  })

  it('keeps plan and remaining-day badges readable in dark mode', () => {
    expect(credentialStyles).toMatch(/\[data-theme='dark'\][\s\S]*\.credentialPlanBadgeTeam[\s\S]*?color:\s*#bbf7d0;/)
    expect(credentialStyles).toMatch(/\[data-theme='dark'\][\s\S]*\.credentialPlanBadgePlus[\s\S]*?color:\s*#bfdbfe;/)
    expect(credentialStyles).toMatch(/\[data-theme='dark'\][\s\S]*\.credentialPlanBadgePro[\s\S]*?color:\s*#fde68a;/)
    expect(credentialStyles).toMatch(/\[data-theme='dark'\][\s\S]*\.credentialRemainingDaysBadge[\s\S]*?color:\s*#bbf7d0;/)
  })
})
