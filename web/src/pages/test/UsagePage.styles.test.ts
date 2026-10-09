import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const readSource = (url: URL) => readFileSync(url, 'utf8').replace(/\r\n/g, '\n')

const usagePageStyles = readSource(new URL('../UsagePage.module.scss', import.meta.url))
const keyOverviewPageStyles = readSource(new URL('../../features/key-viewer/KeyViewerShell.module.scss', import.meta.url))
const priceRulesStyles = readSource(new URL('../../components/usage/pricing/PriceRulesModal.module.scss', import.meta.url))
const credentialStyles = readSource(new URL('../../components/usage/credentials/CredentialSections.module.scss', import.meta.url))
const analysisPanelStyles = readSource(new URL('../../components/usage/analysis/AnalysisPanel.module.scss', import.meta.url))
const timeRangeControlStyles = readSource(new URL('../../components/usage/TimeRangeControl.module.scss', import.meta.url))

const styleRuleBlock = (source: string, selector: string) => {
  const start = source.indexOf(selector)
  expect(start).toBeGreaterThanOrEqual(0)
  const open = source.indexOf('{', start)
  expect(open).toBeGreaterThanOrEqual(0)
  const close = source.indexOf('\n}', open)
  expect(close).toBeGreaterThan(open)
  return source.slice(open + 1, close)
}

const cssHexVariable = (rule: string, name: string) => {
  const match = rule.match(new RegExp(`${name}:\\s*(#[0-9a-fA-F]{6});`))
  if (!match) throw new Error(`Missing CSS variable: ${name}`)
  return match[1]
}

const relativeLuminance = (hex: string) => {
  const channels = [1, 3, 5].map((start) => Number.parseInt(hex.slice(start, start + 2), 16) / 255)
  const [red, green, blue] = channels.map((value) => (
    value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4
  ))
  return (0.2126 * red) + (0.7152 * green) + (0.0722 * blue)
}

describe('UsagePage responsive layout and accessibility', () => {
  it('lets dashboard page frames consume the mode-specific width cap', () => {
    for (const source of [usagePageStyles, keyOverviewPageStyles]) {
      expect(styleRuleBlock(source, '.pageFrame')).toContain('width: min(var(--keeper-page-max-width, 1245px), 100%);')
    }
  })

  it('fills the available viewport consistently before the shared footer', () => {
    for (const pageStyles of [usagePageStyles, keyOverviewPageStyles]) {
      const shell = styleRuleBlock(pageStyles, '.pageShell')
      const frame = styleRuleBlock(pageStyles, '.pageFrame')
      const content = styleRuleBlock(pageStyles, '.contentColumn')
      const container = styleRuleBlock(pageStyles, '.container')

      expect(shell).toContain('min-height: 100svh;')
      expect(shell).toContain('display: flex;')
      expect(shell).toContain('flex-direction: column;')
      expect(frame).toContain('flex: 1 1 auto;')
      expect(content).toContain('flex: 1 1 auto;')
      expect(content).toContain('display: flex;')
      expect(content).toContain('flex-direction: column;')
      expect(container).toContain('flex: 1 1 auto;')
      expect(pageStyles).toMatch(/\.container\s*>\s*:last-child\s*\{[\s\S]*?flex:\s*1 0 auto;/)
    }
  })

  it('lets short request, credential, and settings cards reach the common bottom gutter', () => {
    expect(usagePageStyles).toMatch(/\.requestEventsCard:global\(\.card\)\s*\{[\s\S]*?flex:\s*1 0 auto;/)
    expect(usagePageStyles).toMatch(/\.credentialsSections,\s*\n\.settingsSections\s*\{[\s\S]*?flex:\s*1 0 auto;/)
    expect(usagePageStyles).toMatch(/\.credentialsSections\s*>\s*:last-child,\s*\n\.settingsSections\s*>\s*:last-child\s*\{[\s\S]*?flex:\s*1 0 auto;/)
    expect(credentialStyles).toMatch(/\.credentialSectionCard\s*\{[\s\S]*?display:\s*flex;[\s\S]*?flex-direction:\s*column;/)
    expect(credentialStyles).toMatch(/\.credentialEmptyState\s*\{[\s\S]*?flex:\s*1 1 auto;[\s\S]*?align-items:\s*center;[\s\S]*?justify-content:\s*center;/)
  })

  it('pins top notices to the viewport', () => {
    const notice = styleRuleBlock(usagePageStyles, '.updateCheckToast')
    expect(notice).toContain('position: fixed;')
    expect(notice).toContain('z-index: $z-notification;')
  })

  it('keeps the mobile API Key group and select at full available width', () => {
    const mobileToolbarStart = usagePageStyles.indexOf('@include mobile {\n  .tabPill')
    const mobileToolbarBlock = usagePageStyles.slice(mobileToolbarStart, usagePageStyles.indexOf('@media (prefers-reduced-motion: reduce)'))

    expect(mobileToolbarBlock).toMatch(/\.apiKeyFilterGroup\s*\{[\s\S]*?max-width:\s*100%;/)
    expect(mobileToolbarBlock).toMatch(/\.apiKeySelectControl\s*\{[\s\S]*?width:\s*100%;/)
  })

  it('keeps all five range modes fully visible with consistent content-aware spacing', () => {
    const modeSelector = styleRuleBlock(timeRangeControlStyles, '.modeSelector')
    const modeButton = styleRuleBlock(timeRangeControlStyles, '.modeButton,')

    expect(modeSelector).toContain('grid-template-columns: repeat(5, max-content);')
    expect(modeSelector).toContain('justify-content: space-between;')
    expect(modeButton).toContain('min-width: max-content;')
    expect(modeButton).toContain('width: auto;')
    expect(modeButton).toContain('white-space: nowrap;')
    expect(modeButton).not.toContain('text-overflow: ellipsis;')
    expect(modeButton).not.toContain('overflow: hidden;')
  })

  it('contains hour-list wheel scrolling at its own boundaries', () => {
    const hourList = styleRuleBlock(timeRangeControlStyles, '.customHourList')

    expect(hourList).toContain('position: relative;')
    expect(hourList).toContain('overscroll-behavior-y: contain;')
  })

  it('disables custom view motion when requested', () => {
    expect(timeRangeControlStyles).toMatch(/@media \(prefers-reduced-motion: reduce\)\s*\{[\s\S]*?\.customSummary,[\s\S]*?\.customPicker\s*\{[\s\S]*?animation:\s*none;/)
  })

  it('freezes the liquid and particles for reduced-motion users', () => {
    expect(timeRangeControlStyles).toMatch(/@media \(prefers-reduced-motion: reduce\)\s*\{[\s\S]*?\.sliderFill::before,[\s\S]*?\.sliderFill::after,[\s\S]*?\.liquidParticle\s*\{[\s\S]*?animation:\s*none;/)
  })

  it('keeps overview stats in a primary row and a four-card desktop grid', () => {
    expect(styleRuleBlock(usagePageStyles, '.primaryStatsRow')).toContain('display: flex;')
    expect(styleRuleBlock(usagePageStyles, '.secondaryStatsGrid')).toContain('grid-template-columns: repeat(4, minmax(0, 1fr));')
  })

  it('allows Daily Average and its primary row to grow with content', () => {
    const row = styleRuleBlock(usagePageStyles, '.primaryStatsRow')
    expect(row).toContain('min-height: 176px;')
    expect(row).not.toMatch(/(?:^|\n)\s*height:/)
    expect(styleRuleBlock(usagePageStyles, '.dailyAverageSlot')).toContain('display: flex;')
    expect(styleRuleBlock(usagePageStyles, '.primaryStatSlot')).toContain('display: flex;')
    expect(styleRuleBlock(usagePageStyles, '.statCard.dailyAverageCard {')).toContain('height: 100%;')
  })

  it('places the Daily Average reduced-motion override after its animation rules', () => {
    const slotStylesIndex = usagePageStyles.indexOf('.dailyAverageSlot {', usagePageStyles.indexOf('// Stats Layout'))
    const reducedMotionIndex = usagePageStyles.indexOf('@media (prefers-reduced-motion: reduce)', slotStylesIndex)
    const reducedMotionStyles = usagePageStyles.slice(reducedMotionIndex)

    expect(reducedMotionIndex).toBeGreaterThan(slotStylesIndex)
    expect(reducedMotionStyles).toMatch(/\.dailyAverageSlot\s*\{[\s\S]*?transition:\s*none;[\s\S]*?transform:\s*none;/)
  })

  it('keeps primary overview cards stacked in one column on mobile', () => {
    expect(usagePageStyles).toMatch(/\.primaryStatsRow\s*\{[\s\S]*?@include mobile\s*\{[\s\S]*?flex-direction:\s*column;[\s\S]*?overflow:\s*visible;/)
    expect(usagePageStyles).toMatch(/\.dailyAverageSlot\s*\{[\s\S]*?@include mobile\s*\{[\s\S]*?flex:\s*0 0 auto;[\s\S]*?width:\s*100%;[\s\S]*?max-height:\s*0;[\s\S]*?margin-right:\s*0;[\s\S]*?margin-bottom:\s*-14px;/)
    expect(usagePageStyles).toMatch(/\.primaryStatsRowExpanded\s*\{[\s\S]*?@include mobile\s*\{[\s\S]*?\.dailyAverageSlot\s*\{[\s\S]*?max-height:\s*220px;[\s\S]*?margin-bottom:\s*0;/)
    expect(usagePageStyles).toMatch(/\.primaryStatSlot\s*\{[\s\S]*?@include mobile\s*\{[\s\S]*?flex:\s*0 0 auto;[\s\S]*?width:\s*100%;/)
  })

  it('reflows Activity and realtime grids for narrow screens', () => {
    expect(styleRuleBlock(usagePageStyles, '.recentActivityGrid')).toContain('grid-template-columns: repeat(auto-fit, minmax(min(100%, 530px), 1fr));')
    const realtimeGrid = styleRuleBlock(usagePageStyles, '.overviewRealtimeGrid')
    expect(realtimeGrid).toContain('grid-template-columns: repeat(2, minmax(0, 1fr));')
    expect(realtimeGrid).toMatch(/@include mobile\s*\{[^}]*grid-template-columns:\s*minmax\(0, 1fr\);/)
    expect(styleRuleBlock(usagePageStyles, '.overviewRealtimeCardFull')).toContain('grid-column: 1 / -1;')
  })

  it.each(['.tokenActivityCard', ":global([data-theme='dark']) .tokenActivityCard"])(
    'keeps heatmap levels distinguishable in %s', (selector) => {
      const rule = styleRuleBlock(usagePageStyles, selector)
      const luminance = [1, 2, 3, 4, 5].map((level) =>
        relativeLuminance(cssHexVariable(rule, `--token-activity-level-${level}`)))
      for (let index = 1; index < luminance.length; index += 1) {
        expect(luminance[index - 1]).toBeGreaterThan(luminance[index])
      }
    },
  )

  it('keeps inactive toolbar controls inert while Refresh stays outside the collapsing slot', () => {
    expect(styleRuleBlock(usagePageStyles, '.toolbarActionsRightAnimated')).toContain('grid-template-columns: minmax(0, 1fr) auto;')
    expect(styleRuleBlock(usagePageStyles, '.usageFilterTransition,\n.rankingScopeTransition')).toContain('max-width: 0;')
    expect(styleRuleBlock(usagePageStyles, '.usageFilterTransitionInner,\n.rankingScopeTransitionInner')).toContain('overflow: hidden;')
    expect(styleRuleBlock(usagePageStyles, '.usageRefreshSlot')).toContain('flex: 0 0 auto;')
    expect(usagePageStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.usageFilterTransitionOpen\s*\{[^}]*max-width:\s*100%;/)
  })

  it('collapses and expands the mobile filter height', () => {
    const reducedMotionStart = usagePageStyles.indexOf('@media (prefers-reduced-motion: reduce)')
    const mobileStart = usagePageStyles.lastIndexOf('@include mobile {', reducedMotionStart)
    const mobileStyles = usagePageStyles.slice(mobileStart, reducedMotionStart)
    expect(mobileStyles).toMatch(/\.toolbarActionsRightAnimated \.usageFilterTransition,\s*\.toolbarActionsRightAnimated \.rankingScopeTransition\s*\{[^}]*max-height:\s*0;/)
    expect(mobileStyles).toMatch(/\.toolbarActionsRightAnimated \.usageFilterTransitionOpen\s*\{[^}]*max-height:\s*280px;/)
  })

  it('keeps CPAMC range controls on the immediate toolbar layout path', () => {
    expect(usagePageStyles).toMatch(/\.usageFilterTransitionImmediate\s*\{[\s\S]*?display:\s*contents;/)
    expect(usagePageStyles).toMatch(/\.usageFilterTransitionImmediate\s+\.usageFilterTransitionInner\s*\{[\s\S]*?display:\s*contents;/)
  })

  it('keeps navigation scrollable when tab labels exceed the available width', () => {
    const tabs = styleRuleBlock(usagePageStyles, '.tabBarConnected')
    expect(tabs).toContain('max-width: 100%;')
    expect(tabs).toContain('overflow-x: auto;')
  })

  it('keeps connected tab labels on one line', () => {
    expect(styleRuleBlock(usagePageStyles, '.tabBarConnected .tabPill')).toContain('white-space: nowrap;')
  })

  it('lets API Key Settings content scroll inside the card instead of being clipped', () => {
    expect(usagePageStyles).toMatch(/\.apiKeySettingsCard:global\(\.card\)\s*\{[\s\S]*?min-height:\s*auto;/)
    expect(usagePageStyles).toMatch(/\.apiKeySettingsBody\s*\{[\s\S]*?flex:\s*0 0 auto;/)
    expect(usagePageStyles).toMatch(/\.apiKeySettingsBody\s*\{[\s\S]*?height:\s*var\(--settings-list-scroll-height\);/)
    expect(usagePageStyles).toMatch(/\.apiKeySettingsBody\s*\{[\s\S]*?min-height:\s*0;/)
    expect(usagePageStyles).toMatch(/\.apiKeySettingsBody\s*\{[\s\S]*?overflow-y:\s*auto;/)
    expect(usagePageStyles).toMatch(/\.apiKeySettingsBody\s*\{[\s\S]*?padding-right:\s*4px;/)
    const apiKeySettingsMobileBlock = usagePageStyles.slice(
      usagePageStyles.indexOf('@include mobile {\n  .apiKeySettingsCard:global(.card)'),
      usagePageStyles.indexOf('.pricesList')
    )

    expect(apiKeySettingsMobileBlock).toMatch(/\.apiKeySettingsCard:global\(\.card\)\s*\{[\s\S]*?height:\s*auto;/)
    expect(apiKeySettingsMobileBlock).toMatch(/\.apiKeySettingsBody\s*\{[\s\S]*?height:\s*var\(--settings-list-scroll-height\);/)
    expect(apiKeySettingsMobileBlock).toMatch(/\.apiKeySettingsList\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\);/)
    expect(apiKeySettingsMobileBlock).toMatch(/\.apiKeySettingsItem\s*\{[^}]*grid-template-columns:\s*minmax\(0, 1fr\);/)
    expect(apiKeySettingsMobileBlock).toMatch(/\.apiKeySettingsItem\s*\{[^}]*align-items:\s*stretch;/)
    expect(apiKeySettingsMobileBlock).toMatch(/\.apiKeyAliasField\s*\{[\s\S]*?width:\s*100%;/)
    expect(apiKeySettingsMobileBlock).toMatch(/\.apiKeyAliasInput\s*\{[\s\S]*?max-width:\s*100%;/)
  })

  it('reflows API Key settings from two columns to one on tablets', () => {
    expect(styleRuleBlock(usagePageStyles, '.apiKeySettingsList')).toContain('grid-template-columns: repeat(2, minmax(0, 1fr));')
    const tabletStyles = usagePageStyles.slice(usagePageStyles.indexOf('@include tablet {\n  .apiKeySettingsList'))
    expect(tabletStyles).toMatch(/\.apiKeySettingsList\s*\{[^}]*grid-template-columns:\s*minmax\(0, 1fr\);/)
    expect(styleRuleBlock(usagePageStyles, '.apiKeySettingsNameRow')).toContain('grid-template-columns: minmax(0, 1fr) auto;')
  })

  it('keeps session alias editing within the card and visibly focusable', () => {
    expect(styleRuleBlock(usagePageStyles, '.sessionSettingsAliasEditorEditing')).toContain('width: min(236px, 100%);')
    expect(styleRuleBlock(usagePageStyles, '.sessionSettingsAliasEditButton,')).toMatch(/&:focus-visible\s*\{[^}]*outline:\s*2px solid var\(--primary-color\);/)
  })

  it('disables the current-session pulse for reduced motion', () => {
    expect(usagePageStyles).toMatch(/@media \(prefers-reduced-motion: reduce\)\s*\{[\s\S]*?\.sessionSettingsCurrentDot\s*\{[^}]*animation:\s*none;/)
  })

  it('lets Session Management content shrink until it needs to scroll', () => {
    const sessionSettingsBodyBlock = usagePageStyles.slice(
      usagePageStyles.indexOf('.sessionSettingsBody {'),
      usagePageStyles.indexOf('.sessionSettingsList')
    )
    const sessionSettingsMobileBlock = usagePageStyles.slice(
      usagePageStyles.indexOf('@include mobile {\n  .apiKeySettingsCard:global(.card)'),
      usagePageStyles.indexOf('.pricesList')
    )
    const sessionSettingsMobileBodyBlock = sessionSettingsMobileBlock.slice(
      sessionSettingsMobileBlock.indexOf('  .sessionSettingsBody {'),
      sessionSettingsMobileBlock.indexOf('  .sessionSettingsItem {')
    )

    expect(usagePageStyles).toMatch(/\.sessionSettingsCard:global\(\.card\)\s*\{[\s\S]*?min-height:\s*auto;/)
    expect(usagePageStyles).toMatch(/\.sessionSettingsBody\s*\{[\s\S]*?flex:\s*0 0 auto;/)
    expect(sessionSettingsBodyBlock).toMatch(/\n\s{2}max-height:\s*var\(--settings-list-scroll-height\);/)
    expect(sessionSettingsBodyBlock).not.toMatch(/\n\s{2}height:\s*var\(--settings-list-scroll-height\);/)
    expect(usagePageStyles).toMatch(/\.sessionSettingsBody\s*\{[\s\S]*?overflow-y:\s*auto;/)
    expect(usagePageStyles).toMatch(/\.sessionSettingsBody\s*\{[\s\S]*?overflow-x:\s*hidden;/)
    expect(sessionSettingsMobileBodyBlock).toMatch(/\n\s{4}max-height:\s*var\(--settings-list-scroll-height\);/)
    expect(sessionSettingsMobileBodyBlock).not.toMatch(/\n\s{4}height:\s*var\(--settings-list-scroll-height\);/)
  })

  it('uses the full Session Management row for a wrapping User-Agent and adaptive metadata', () => {
    const clientBlock = usagePageStyles.slice(
      usagePageStyles.indexOf('.sessionSettingsClient {'),
      usagePageStyles.indexOf('.sessionSettingsClientLabel {'),
    )
    const sessionSettingsMobileBlock = usagePageStyles.slice(
      usagePageStyles.indexOf('@include mobile {\n  .apiKeySettingsCard:global(.card)'),
      usagePageStyles.indexOf('.pricesList'),
    )

    expect(usagePageStyles).toMatch(/\.sessionSettingsItem\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\) auto;/)
    expect(usagePageStyles).toMatch(/\.sessionSettingsItem\s*\{[\s\S]*?grid-template-areas:\s*'summary actions'\s*'client client'\s*'details details';/)
    expect(usagePageStyles).toMatch(/\.sessionSettingsDetails\s*\{[\s\S]*?grid-template-columns:\s*repeat\(auto-fit, minmax\(220px, 1fr\)\);/)
    expect(usagePageStyles).toMatch(/\.sessionSettingsDetailItem\s*\{[\s\S]*?grid-template-columns:\s*max-content minmax\(0, 1fr\);[\s\S]*?align-items:\s*baseline;/)
    expect(clientBlock).toMatch(/white-space:\s*normal;/)
    expect(clientBlock).toMatch(/overflow-wrap:\s*anywhere;/)
    expect(clientBlock).not.toMatch(/text-overflow:\s*ellipsis;/)
    expect(clientBlock).not.toMatch(/white-space:\s*nowrap;/)
    expect(sessionSettingsMobileBlock).toMatch(/\.sessionSettingsItem\s*\{[\s\S]*?grid-template-areas:\s*'summary'\s*'client'\s*'details'\s*'actions';/)
  })

  it('contains wheel scrolling at overflowing card boundaries without trapping short lists', () => {
    expect(usagePageStyles).toMatch(/\.requestEventsTableWrapper\[data-scroll-boundary-contained='true'\],[\s\S]*?\.requestEventsLogSectionPanelInner\[data-scroll-boundary-contained='true'\],[\s\S]*?\.apiKeySettingsBody\[data-scroll-boundary-contained='true'\],[\s\S]*?\.sessionSettingsBody\[data-scroll-boundary-contained='true'\],[\s\S]*?\.pricesGrid\[data-scroll-boundary-contained='true'\]\s*\{[\s\S]*?overscroll-behavior-y:\s*contain;/)
  })

  it('keeps Model Pricing Settings list viewport aligned with API Key Settings without shrinking it behind the form', () => {
    const settingsSectionsBlock = usagePageStyles.slice(
      usagePageStyles.indexOf('.settingsSections {'),
      usagePageStyles.indexOf('// Pricing Section')
    )
    const pricingBlock = usagePageStyles.slice(
      usagePageStyles.indexOf('.pricingFixedCard {'),
      usagePageStyles.indexOf('.priceForm')
    )
    const apiKeyBodyBlock = usagePageStyles.slice(
      usagePageStyles.indexOf('.apiKeySettingsBody {'),
      usagePageStyles.indexOf('.apiKeySettingsList')
    )
    const apiKeySettingsMobileBlock = usagePageStyles.slice(
      usagePageStyles.indexOf('@include mobile {\n  .apiKeySettingsCard:global(.card)'),
      usagePageStyles.indexOf('.pricesList')
    )
    const pricingGridBlock = usagePageStyles.slice(
      usagePageStyles.indexOf('.pricesGrid {'),
      usagePageStyles.indexOf('.priceItem')
    )

    expect(settingsSectionsBlock).toMatch(/--settings-list-scroll-height:\s*480px;/)
    expect(pricingBlock).toMatch(/\.pricingFixedCard\s*\{[\s\S]*?height:\s*auto;/)
    expect(pricingBlock).not.toMatch(/\.pricingSection\s*\{[\s\S]*?height:\s*480px;/)
    expect(apiKeyBodyBlock).toMatch(/height:\s*var\(--settings-list-scroll-height\);/)
    expect(apiKeySettingsMobileBlock).toMatch(/\.apiKeySettingsBody\s*\{[\s\S]*?height:\s*var\(--settings-list-scroll-height\);/)
    expect(pricingGridBlock).toMatch(/height:\s*var\(--settings-list-scroll-height\);/)
    expect(pricingGridBlock).toMatch(/\.pricesGrid\s*\{[\s\S]*?overflow-y:\s*auto;/)
    expect(pricingGridBlock).toMatch(/\.pricesGrid\s*\{[\s\S]*?overflow-x:\s*hidden;/)
    expect(pricingGridBlock).not.toMatch(/@include mobile\s*\{[\s\S]*?overflow:\s*visible;/)
  })

  it('reflows the model pricing form from four to two to one column based on its container width', () => {
    expect(usagePageStyles).toMatch(/\.priceForm\s*\{[\s\S]*?container-name:\s*model-pricing-form;/)
    expect(usagePageStyles).toMatch(/\.priceForm\s*\{[\s\S]*?container-type:\s*inline-size;/)
    expect(usagePageStyles).toMatch(/\.formRow\s*\{[\s\S]*?display:\s*grid;/)
    expect(usagePageStyles).toMatch(/\.formRow\s*\{[\s\S]*?grid-template-columns:\s*minmax\(180px, 1\.4fr\) minmax\(130px, 0\.85fr\) repeat\(5, minmax\(120px, 1fr\)\) auto;/)
    expect(usagePageStyles).toMatch(/@container model-pricing-form \(max-width:\s*1120px\)\s*\{[\s\S]*?grid-template-columns:\s*repeat\(4, minmax\(0, 1fr\)\);/)
    expect(usagePageStyles).toMatch(/@container model-pricing-form \(max-width:\s*720px\)\s*\{[\s\S]*?grid-template-columns:\s*repeat\(2, minmax\(0, 1fr\)\);[\s\S]*?\.priceFormModelField,[\s\S]*?\.priceFormAction\s*\{[\s\S]*?grid-column:\s*1 \/ -1;/)
    expect(usagePageStyles).toMatch(/@container model-pricing-form \(max-width:\s*480px\)\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\);/)
  })

  it('keeps Analysis tooltips and heatmap labels accessible', () => {
    expect(styleRuleBlock(analysisPanelStyles, '.heatmapCell:focus-visible')).toMatch(/box-shadow:\s*0 0 0 2px/)
    const label = styleRuleBlock(analysisPanelStyles, '.heatmapModelLabel')
    expect(label).toContain('-webkit-line-clamp: 2;')
    expect(label).toContain('overflow-wrap: anywhere;')
    expect(styleRuleBlock(analysisPanelStyles, '.heatmapFloatingTooltip')).toContain('position: fixed;')
    expect(styleRuleBlock(analysisPanelStyles, '.modelEfficiencyFloatingTooltip')).toContain('pointer-events: none;')
  })

  it('keeps Request Event Log headers visible while the table scrolls', () => {
    expect(usagePageStyles).toMatch(/\.requestEventsTableWrapper\s*\{[\s\S]*?height:\s*clamp\(520px,\s*68vh,\s*760px\);/)
    expect(usagePageStyles).toMatch(/\.requestEventsTableWrapper\s*\{[\s\S]*?overflow:\s*auto;/)
    expect(usagePageStyles).toMatch(/\.requestEventsTableWrapper\s*\{[\s\S]*?thead\s+th\s*\{[\s\S]*?position:\s*sticky;/)
    expect(usagePageStyles).toMatch(/\.requestEventsTableWrapper\s*\{[\s\S]*?thead\s+th\s*\{[\s\S]*?top:\s*0;/)
    expect(usagePageStyles).toMatch(/\.requestEventsTableWrapper\s*\{[\s\S]*?thead\s+th\s*\{[\s\S]*?z-index:\s*2;/)
    expect(usagePageStyles).toMatch(/\.requestEventsTableWrapper\s*\{[\s\S]*?\.table\s*\{[\s\S]*?border-collapse:\s*separate;/)
  })

  it('caps Request Event Log long text columns without forcing short aliases wide', () => {
    const apiKeyCellBlock = Array.from(
      usagePageStyles.matchAll(/\.requestEventsAPIKeyCell\s*\{([^}]*)\}/g),
      (match) => match[1],
    ).at(-1)
    const sourceCellBlock = styleRuleBlock(usagePageStyles, '.requestEventsSourceCell {')
    const deletedTagBlock = styleRuleBlock(usagePageStyles, '.requestEventsDeletedTag')

    expect(apiKeyCellBlock).toMatch(/max-width:\s*240px;/)
    expect(sourceCellBlock).toMatch(/max-width:\s*280px;/)
    expect(sourceCellBlock).not.toContain('min-width:')
    expect(deletedTagBlock).toContain('white-space: nowrap;')
    expect(usagePageStyles).toMatch(/\.modelCell\s*\{[\s\S]*?min-width:\s*110px;/)
    expect(usagePageStyles).toMatch(/\.modelCell\s*\{[\s\S]*?max-width:\s*240px;/)
  })

  it('keeps shared Request Event metric styles non-wrapping', () => {
    const noWrapCellBlock = usagePageStyles.slice(
      usagePageStyles.indexOf('.requestEventsNoWrapCell {'),
      usagePageStyles.indexOf('.requestEventsSourceCell')
    )

    expect(noWrapCellBlock).toMatch(/white-space:\s*nowrap;/)
    expect(noWrapCellBlock).toMatch(/font-variant-numeric:\s*tabular-nums;/)
    expect(usagePageStyles).toMatch(/\.requestEventsExecutorCell\s*\{[\s\S]*?white-space:\s*nowrap;/)
  })

  it('disables Request Event column switch transitions for reduced motion', () => {
    expect(usagePageStyles).toMatch(
      /@media \(prefers-reduced-motion: reduce\)\s*\{[\s\S]*?\.requestEventsColumnVisibilityTrack,[\s\S]*?\.requestEventsColumnVisibilityThumb\s*\{[\s\S]*?transition:\s*none;/
    )
  })

  it('keeps pricing help within the viewport and scrollable', () => {
    const tooltip = styleRuleBlock(priceRulesStyles, '.helpTooltip')
    expect(tooltip).toContain('box-sizing: border-box;')
    expect(tooltip).toContain('position: fixed;')
    expect(tooltip).toContain('overflow-y: auto;')
  })
})
