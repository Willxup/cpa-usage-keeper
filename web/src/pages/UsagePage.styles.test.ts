import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const readSource = (url: URL) => readFileSync(url, 'utf8').replace(/\r\n/g, '\n')

const globalStyles = readSource(new URL('../styles/global.scss', import.meta.url))
const usagePageStyles = readSource(new URL('./UsagePage.module.scss', import.meta.url))
const usagePageSource = readSource(new URL('./UsagePage.tsx', import.meta.url))
const requestEventsSource = readSource(new URL('../components/usage/RequestEventsDetailsCard.tsx', import.meta.url))
const priceSettingsSource = readSource(new URL('../components/usage/PriceSettingsCard.tsx', import.meta.url))
const chartLineSelectorSource = readSource(new URL('../components/usage/ChartLineSelector.tsx', import.meta.url))
const selectSource = readSource(new URL('../components/ui/Select.tsx', import.meta.url))
const apiIndexSource = readSource(new URL('../components/usage/index.ts', import.meta.url))
const apiClientSource = readSource(new URL('../lib/api.ts', import.meta.url))
const i18nSource = readSource(new URL('../i18n/index.ts', import.meta.url))
const analysisPanelSource = readSource(new URL('../components/usage/analysis/AnalysisPanel.tsx', import.meta.url))
const analysisPanelStyles = readSource(new URL('../components/usage/analysis/AnalysisPanel.module.scss', import.meta.url))
const usageChartSource = readSource(new URL('../components/usage/UsageChart.tsx', import.meta.url))
const tokenBreakdownChartSource = readSource(new URL('../components/usage/TokenBreakdownChart.tsx', import.meta.url))
const costTrendChartSource = readSource(new URL('../components/usage/CostTrendChart.tsx', import.meta.url))
const statCardsSource = readSource(new URL('../components/usage/StatCards.tsx', import.meta.url))

describe('UsagePage toolbar styles', () => {
  it('keeps visible range controls content-sized in narrow layouts', () => {
    expect(usagePageStyles).toMatch(/\.timeRangeGroup\s*\{[\s\S]*?width:\s*fit-content;/)
    expect(usagePageStyles).toMatch(/\.timeRangeSelectControl\s*\{[\s\S]*?flex:\s*0 0 164px;/)
  })

  it('keeps overview stat cards in a two-plus-four desktop grid with a distinct cache-rate color', () => {
    expect(usagePageStyles).toMatch(/\.statCard\s*\{[\s\S]*?grid-column:\s*span 3;/)
    expect(usagePageStyles).toMatch(/\.statCard:nth-child\(-n \+ 2\)\s*\{[\s\S]*?grid-column:\s*span 6;/)
    expect(usagePageStyles).toMatch(/\.statLabel\s*\{[\s\S]*?letter-spacing:\s*0;/)
    expect(statCardsSource).toContain("key: 'requests'")
    expect(statCardsSource).toContain("accent: '#3b82f6'")
    expect(statCardsSource).toContain("key: 'cache-rate'")
    expect(statCardsSource).toContain("accent: '#14b8a6'")
    expect(statCardsSource.match(/accent:\s*'#[0-9a-f]{6}'/g)).toHaveLength(new Set(statCardsSource.match(/accent:\s*'#[0-9a-f]{6}'/g)).size)
  })

  it('keeps refresh controls outside the query filter layout', () => {
    expect(usagePageSource).toContain('{showRangeControls && (\n                  <div className={styles.usageFilterBar}>')
    expect(usagePageSource).toContain('className={styles.usageRefreshSlot}')
    expect(usagePageSource).not.toContain('styles.usageFilterBarCollapsed')
    expect(usagePageStyles).toMatch(/\.usageRefreshSlot\s*\{[\s\S]*?flex:\s*0 0 auto;/)
  })

  it('keeps the API Key filter visible on the Analysis page so Analysis requests can be filtered', () => {
    expect(usagePageSource).not.toContain('shouldShowApiKeyFilter(activeTab)')
    expect(usagePageSource).not.toContain('styles.apiKeyFilterGroupHidden')
    expect(usagePageSource).not.toContain('aria-hidden={!showApiKeyFilter}')
    expect(usagePageStyles).not.toContain('.apiKeyFilterGroupHidden')
  })

  it('uses the new Analysis panel and endpoint instead of the old detail tables', () => {
    expect(usagePageSource).toContain('fetchAnalysis')
    expect(usagePageSource).toContain('<AnalysisPanel')
    expect(usagePageSource).not.toContain('fetchUsageAnalysis')
    expect(usagePageSource).not.toContain('<ApiDetailsCard')
    expect(usagePageSource).not.toContain('<ModelStatsCard')
    expect(apiIndexSource).not.toContain('ApiDetailsCard')
    expect(apiIndexSource).not.toContain('ModelStatsCard')
    expect(apiClientSource).toContain("apiPath('/usage/analysis')")
  })

  it('renames the Analysis tab label and places it before Request Events', () => {
    expect(i18nSource).toContain("tab_analysis: 'Analysis'")
    expect(i18nSource).not.toContain("tab_analysis: 'API & Models'")
    expect(i18nSource).not.toContain("tab_analysis: 'API 与模型'")
    expect(i18nSource).not.toContain("tab_analysis: 'API 與模型'")
    expect(usagePageSource).toContain("const USAGE_TAB_OPTIONS = ['overview', 'analysis', 'events', 'auth-files', 'ai-provider', 'settings'] as const")
  })

  it('keeps Sign out as the rightmost header action after Check Updates', () => {
    expect(usagePageSource).toContain("import { ApiError, fetchAnalysis, fetchCpaApiKeyOptions, fetchCpaApiKeys, fetchStatus, fetchUpdateCheck, fetchUsageEventModelFilterOptions, fetchUsageEventSourceFilterOptions, fetchUsageEvents, logout, markStatusActive, updateCpaApiKeyAlias } from '@/lib/api';")
    expect(usagePageSource.indexOf("t('usage_stats.check_updates')")).toBeLessThan(usagePageSource.indexOf("t('common.logout')"))
    expect(usagePageStyles).toContain('.signOutSwitcher')
    expect(usagePageStyles).toContain('.signOutPill')
  })

  it('keeps mobile tab labels on one line without changing desktop tab sizing', () => {
    const desktopTabPillBlock = usagePageStyles.slice(
      usagePageStyles.indexOf('.tabPill {'),
      usagePageStyles.indexOf('.tabPillActive')
    )

    expect(usagePageStyles).toContain('@include mobile {\n  .tabPill {\n    white-space: nowrap;\n  }\n')
    expect(desktopTabPillBlock).not.toContain('white-space: nowrap;')
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
    expect(apiKeySettingsMobileBlock).toMatch(/\.apiKeyAliasField\s*\{[\s\S]*?:global\(\.form-group\)\s*\{[\s\S]*?width:\s*100%;/)
    expect(apiKeySettingsMobileBlock).toMatch(/\.apiKeyAliasField\s*\{[\s\S]*?:global\(\.form-group\)\s*\{[\s\S]*?min-width:\s*0;/)
    expect(apiKeySettingsMobileBlock).toMatch(/\.apiKeyAliasField\s*\{[\s\S]*?:global\(\.form-group\)\s*\{[\s\S]*?margin-bottom:\s*0;/)
    expect(apiKeySettingsMobileBlock).toMatch(/\.apiKeyAliasInput\s*\{[\s\S]*?max-width:\s*100%;/)
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

  it('keeps the Analysis chart presentation aligned with the redesigned Analysis dashboard', () => {
    expect(analysisPanelSource).toContain("t('usage_stats.analysis_token_usage_title')")
    expect(analysisPanelSource).toContain("t('usage_stats.analysis_token_usage_subtitle')")
    expect(analysisPanelSource).toContain("t('usage_stats.analysis_cost_breakdown_title')")
    expect(analysisPanelSource).toContain("t('usage_stats.analysis_model_efficiency_title')")
    expect(analysisPanelSource).toContain("t('usage_stats.analysis_composition_title')")
    expect(analysisPanelSource).toContain("t('usage_stats.analysis_composition_token_percent')")
    expect(analysisPanelSource).toContain("t('usage_stats.analysis_heatmap_title')")
    expect(analysisPanelSource).toContain("t('usage_stats.analysis_heatmap_subtitle')")
    expect(analysisPanelSource).toContain("t('usage_stats.total_cost')")
    expect(analysisPanelSource).toContain("import { Bar, Doughnut, Scatter } from 'react-chartjs-2'")
    expect(usagePageSource).toContain('LineController')
    expect(usagePageSource).toContain('LogarithmicScale')
    expect(usagePageSource).toContain('ChartJS.register(')
    expect(analysisPanelSource).toContain('<Bar data={chartData} options={chartOptions} plugins={[drawRequestsLineOnTopPlugin]} />')
    expect(analysisPanelSource).toContain("const activeContentKey = `${activeTab?.id ?? 'empty'}:${items.map((item) => item.key).join('|')}`")
    expect(analysisPanelSource).toContain('<Doughnut key={`chart-${activeContentKey}`} data={chartData} options={chartOptions} />')
    expect(analysisPanelSource).toContain('<Scatter data={chartData} options={chartOptions} />')
    expect(analysisPanelSource).toContain("cost: '#14b8a6'")
    expect(analysisPanelSource).toContain('ticks: { color: chartTheme.textSecondary')
    expect(analysisPanelSource).toContain('analysis_cost_per_million_tokens')
    expect(analysisPanelSource).toContain('analysis_blended_rate')
    expect(analysisPanelSource).toContain('styles.costStackFloatingTooltip')
    expect(analysisPanelSource).toContain('onMouseEnter={(event) => showCostTooltip(tooltipLines, event)}')
    expect(analysisPanelSource).toContain('createLinearGradient')
    expect(analysisPanelSource).not.toContain('createRadialGradient')
    expect(analysisPanelSource).toContain('className={styles.costRateMetric}')
    expect(analysisPanelSource).not.toContain('removeCompositionTooltip')
    expect(analysisPanelSource).toContain("yAxisID: 'cost'")
    expect(analysisPanelSource).toContain('buildAnalysisTokenChartOptions')
    expect(analysisPanelSource).toContain('buildCompositionChartData')
    expect(analysisPanelSource).toContain('CostBreakdownCard')
    expect(analysisPanelSource).toContain('ModelEfficiencyCard')
    expect(analysisPanelSource).toContain('CompositionPanel')
    expect(analysisPanelSource).toContain('heatmapTooltip')
    expect(analysisPanelSource).toContain('styles.heatmapModelHeaderCell')
    expect(analysisPanelSource).toContain('styles.heatmapModelLabel')
    expect(analysisPanelSource).toContain('onMouseEnter={(event) => showTooltip([model], event)}')
    expect(analysisPanelSource).toContain('onFocus={(event) => showTooltip([model], event)}')
    expect(analysisPanelSource).not.toContain('styles.efficiencyList')
    expect(analysisPanelSource).not.toContain('styles.efficiencyRow')
    expect(analysisPanelSource).toContain('getHeatmapCellColor(intensity, isDark)')
    expect(analysisPanelSource).toContain('formatUsd')
    expect(analysisPanelSource).not.toContain("analysis_api_key_composition_title")
    expect(analysisPanelSource).not.toContain("analysis_model_composition_title")
    expect(analysisPanelSource).not.toContain("analysis_auth_files_composition_title")
    expect(analysisPanelSource).not.toContain("analysis_ai_provider_composition_title")
    expect(analysisPanelSource).not.toContain("analysis_heatmap_tokens_prefix")
    expect(analysisPanelSource).not.toContain("analysis_heatmap_requests_prefix")
    expect(analysisPanelSource).not.toContain("from 'recharts'")
    expect(analysisPanelStyles).toMatch(/\.insightGrid\s*\{[\s\S]*?grid-template-columns:\s*repeat\(2, minmax\(0, 1fr\)\);/)
    expect(analysisPanelStyles).toMatch(/\.insightGrid\s*\{[\s\S]*?@include mobile\s*\{[\s\S]*?grid-template-columns:\s*1fr;/)
    expect(analysisPanelStyles).toMatch(/\.costRatePanel\s*\{[\s\S]*?grid-template-columns:\s*repeat\(3, minmax\(0, 1fr\)\);/)
    expect(analysisPanelStyles).toMatch(/\.costRatePanel\s*\{[\s\S]*?gap:\s*0;/)
    expect(analysisPanelStyles).toMatch(/\.costRateMetric \+ \.costRateMetric,\s*\.costRateSparkline\s*\{[\s\S]*?border-left:\s*1px solid var\(--border-color\);/)
    expect(analysisPanelStyles).toMatch(/\.costRateSparkline\s*\{[\s\S]*?height:\s*100%;/)
    expect(analysisPanelStyles).toMatch(/\.costRateMetric\s*\{[\s\S]*?justify-content:\s*flex-start;/)
    expect(analysisPanelStyles).toMatch(/\.costStackSegment\s*\{[\s\S]*?background:\s*linear-gradient\(90deg, color-mix\(in srgb, var\(--cost-segment-color\) 72%, var\(--bg-secondary\)\), var\(--cost-segment-color\)\);/)
    expect(analysisPanelStyles).toMatch(/\.costStackFloatingTooltip\s*\{[\s\S]*?position:\s*fixed;/)
    expect(analysisPanelStyles).toMatch(/\.insightGrid\s*\{[\s\S]*?align-items:\s*stretch;/)
    expect(analysisPanelStyles).toMatch(/\.efficiencyChartFrame\s*\{[\s\S]*?height:\s*300px;/)
    expect(analysisPanelStyles).not.toContain('.efficiencyList')
    expect(analysisPanelStyles).not.toContain('.efficiencyRow')
    expect(analysisPanelStyles).toMatch(/\.compositionLayout\s*\{[\s\S]*?grid-template-columns:\s*minmax\(220px, 0\.72fr\) minmax\(0, 1\.28fr\);/)
    expect(analysisPanelStyles).toMatch(/\.compositionLayout\s*\{[\s\S]*?@include mobile\s*\{[\s\S]*?grid-template-columns:\s*1fr;/)
    expect(analysisPanelStyles).toMatch(/\.compositionTabActive\s*\{[\s\S]*?background:\s*color-mix\(in srgb, var\(--bg-primary\) 84%, var\(--bg-secondary\)\);/)
    expect(analysisPanelStyles).not.toMatch(/\.compositionTabActive\s*\{[\s\S]*?#2563eb/)
    expect(analysisPanelStyles).toMatch(/\.heatmapCardLight \.analysisChartSurface\s*\{[\s\S]*?background:\s*color-mix/)
    expect(analysisPanelStyles).toMatch(/\.heatmapCardDark \.analysisChartSurface\s*\{[\s\S]*?background:\s*#100e16;/)
    expect(analysisPanelStyles).toMatch(/\.heatmapCell::before\s*\{[\s\S]*?radial-gradient\(circle at 50% 115%/)
    expect(analysisPanelStyles).toMatch(/\.heatmapCorner,\s*\.heatmapHeaderCell\s*\{[\s\S]*?min-height:\s*48px;/)
    const heatmapRowLabelBlock = [...analysisPanelStyles.matchAll(/\.heatmapRowLabel\s*\{([\s\S]*?)\n\}/g)]
      .map((match) => match[1])
      .find((block) => block.includes('display: flex;')) ?? ''
    expect(heatmapRowLabelBlock).toContain('height: 30px;')
    expect(heatmapRowLabelBlock).toContain('align-self: center;')
    expect(analysisPanelStyles).toMatch(/\.heatmapModelLabel\s*\{[\s\S]*?-webkit-line-clamp:\s*2;/)
    expect(analysisPanelStyles).toMatch(/\.heatmapModelLabel\s*\{[\s\S]*?overflow-wrap:\s*anywhere;/)
    expect(analysisPanelStyles).toMatch(/\.heatmapLegendRamp\s*\{[\s\S]*?linear-gradient\(90deg, #fff7ed, #fed7aa, #fb923c, #ef4444, #7c2d12\)/)
    expect(analysisPanelStyles).toMatch(/\.heatmapCardDark \.heatmapLegendRamp\s*\{[\s\S]*?linear-gradient\(90deg, #1a1118, #4a1f23, #9a3412, #f97316, #fde68a\)/)
    expect(analysisPanelStyles).toMatch(/\.heatmapFloatingTooltip\s*\{[\s\S]*?position:\s*fixed;/)
    expect(analysisPanelStyles).toMatch(/\.heatmapFloatingTooltip\s*\{[\s\S]*?border:\s*1px solid var\(--border-color\);/)
    expect(analysisPanelStyles).toMatch(/\.heatmapFloatingTooltip\s*\{[\s\S]*?background:\s*var\(--bg-primary\);/)
    expect(analysisPanelStyles).toMatch(/\.heatmapFloatingTooltip\s*\{[\s\S]*?color:\s*var\(--text-secondary\);/)
    expect(analysisPanelStyles).toMatch(/\.heatmapTooltipTitle\s*\{[\s\S]*?color:\s*var\(--text-primary\);/)
    expect(analysisPanelStyles).not.toContain('.heatmapCellTooltip')
    expect(analysisPanelStyles).not.toContain('.compositionGrid')
    expect(analysisPanelStyles).not.toContain('.heatmapCellRequestValue')
    expect(analysisPanelStyles).not.toContain('rgb(250, 244, 230)')
  })

  it('widens only the API key dropdown menu without changing the trigger width', () => {
    expect(selectSource).toContain('dropdownMinWidth?: number')
    expect(selectSource).toContain('rect.left - (width - rect.width) / 2')
    expect(usagePageSource).toContain('dropdownMinWidth={180}')
  })

  it('preserves the original desktop toolbar sizing while isolating refresh layout', () => {
    expect(usagePageStyles).toMatch(/\.toolbarActionsRight\s*\{[\s\S]*?align-items:\s*center;/)
    expect(usagePageStyles).toMatch(/\.usageFilterBar\s*\{[\s\S]*?align-items:\s*center;/)
    expect(usagePageStyles).toMatch(/\.usageFilterBar\s*\{[\s\S]*?flex:\s*1 1 auto;/)
    expect(usagePageStyles).toMatch(/\.apiKeySelectControl\s*\{[\s\S]*?width:\s*172px;/)
    expect(usagePageStyles).toMatch(/\.apiKeySelectControl\s*\{[\s\S]*?flex:\s*0 0 172px;/)
    expect(usagePageStyles).toMatch(/\.rangeSelectControl\s*\{[\s\S]*?width:\s*164px;/)
    expect(usagePageStyles).toMatch(/\.rangeSelectControl\s*\{[\s\S]*?flex:\s*0 0 164px;/)
  })

  it('keeps custom range inputs hidden and disabled until the custom range is selected', () => {
    expect(usagePageSource).toContain('styles.customRangeFieldGroupOpen')
    expect(usagePageSource).toContain('aria-hidden={!isCustomRange}')
    expect(usagePageSource).toContain('disabled={!isCustomRange}')
    expect(usagePageSource).not.toContain('{isCustomRange && (')
  })

  it('keeps custom date inputs selectable through the native picker without pointer interception', () => {
    expect(usagePageStyles).toMatch(/\.customRangeInput\s*\{[\s\S]*?user-select:\s*none;/)
    expect(usagePageStyles).toMatch(/\.customRangeInput\s*\{[\s\S]*?-webkit-user-select:\s*none;/)
    expect(usagePageSource).not.toContain('readOnly')
    expect(usagePageSource).not.toContain('onPointerDown={handleCustomDateInputPointerDown}')
    expect(usagePageSource).toContain('className={styles.customRangeInputShell}')
    expect(usagePageSource).toContain('className={styles.customRangeInputDisplay}')
    expect(usagePageSource).toContain('onClick={handleCustomDateInputActivate}')
    expect(usagePageSource).toContain('onFocus={handleCustomDateInputActivate}')
    expect(usagePageSource).toContain('onKeyDown={handleCustomDateInputKeyDown}')
  })

  it('keeps mobile custom date fields inside the toolbar before the refresh action', () => {
    const narrowToolbarStart = usagePageStyles.indexOf('@media (max-width: #{$breakpoint-tablet})')
    const mobileToolbarStart = usagePageStyles.indexOf('@include mobile {\n  .tabPill', narrowToolbarStart)
    const narrowToolbarBlock = usagePageStyles.slice(
      narrowToolbarStart,
      mobileToolbarStart
    )
    const mobileToolbarBlock = usagePageStyles.slice(
      mobileToolbarStart,
      usagePageStyles.indexOf('@media (prefers-reduced-motion: reduce)')
    )

    expect(narrowToolbarBlock).toMatch(/\.usageFilterBar\s*\{[\s\S]*?max-height:\s*none;/)
    expect(narrowToolbarBlock).toMatch(/\.usageFilterBar\s*\{[\s\S]*?overflow:\s*visible;/)
    expect(narrowToolbarBlock).toMatch(/\.timeRangeGroup\s*\{[\s\S]*?width:\s*100%;/)
    expect(narrowToolbarBlock).toMatch(/\.customRangeFieldGroup\s*\{[\s\S]*?width:\s*100%;/)
    expect(narrowToolbarBlock).toMatch(/\.customRangeFieldGroupOpen\s*\{[\s\S]*?max-height:\s*180px;/)
    expect(mobileToolbarBlock).toMatch(/\.usageFilterBar\s*\{[\s\S]*?display:\s*grid;/)
    expect(mobileToolbarBlock).toMatch(/\.usageFilterBar\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\);/)
    expect(mobileToolbarBlock).toMatch(/\.rangeFilterField\s*\{[\s\S]*?grid-template-columns:\s*auto minmax\(0, 1fr\);/)
    expect(mobileToolbarBlock).toMatch(/\.customRangeFieldGroup\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\);/)
    expect(mobileToolbarBlock).toMatch(/\.customRangeField\s*\{[\s\S]*?grid-template-columns:\s*auto minmax\(0, 1fr\);/)
    expect(mobileToolbarBlock).toMatch(/\.customRangeField\s*\{[\s\S]*?min-width:\s*0;/)
    expect(mobileToolbarBlock).toMatch(/\.customRangeField\s*\{[\s\S]*?max-width:\s*100%;/)
    expect(mobileToolbarBlock).toMatch(/\.customRangeInputShell\s*\{[\s\S]*?position:\s*relative;/)
    expect(mobileToolbarBlock).toMatch(/\.customRangeInputShell\s*\{[\s\S]*?overflow:\s*hidden;/)
    expect(mobileToolbarBlock).toMatch(/\.customRangeInputDisplay\s*\{[\s\S]*?display:\s*flex;/)
    expect(mobileToolbarBlock).toMatch(/\.customRangeInput\s*\{[\s\S]*?position:\s*absolute;/)
    expect(mobileToolbarBlock).toMatch(/\.customRangeInput\s*\{[\s\S]*?min-width:\s*0;/)
    expect(mobileToolbarBlock).toMatch(/\.customRangeInput\s*\{[\s\S]*?max-width:\s*100%;/)
    expect(mobileToolbarBlock).toMatch(/\.customRangeInput\s*\{[\s\S]*?display:\s*block;/)
    expect(mobileToolbarBlock).toMatch(/\.customRangeInput\s*\{[\s\S]*?opacity:\s*0;/)
  })

  it('keeps Overview chart period controls hidden because period selection is automatic', () => {
    expect(usageChartSource).not.toContain('className={styles.periodButtons}')
    expect(tokenBreakdownChartSource).not.toContain('className={styles.periodButtons}')
    expect(costTrendChartSource).not.toContain('className={styles.periodButtons}')
  })

  it('places Chart Line Selection and trend cards below Cost Trend on Overview', () => {
    const serviceHealthIndex = usagePageSource.indexOf('<ServiceHealthCard')
    const tokenBreakdownIndex = usagePageSource.indexOf('<TokenBreakdownChart')
    const costTrendIndex = usagePageSource.indexOf('<CostTrendChart')
    const chartLineSelectorIndex = usagePageSource.indexOf('<ChartLineSelector')
    const chartsGridIndex = usagePageSource.indexOf('<div className={styles.chartsGrid}>')

    expect(serviceHealthIndex).toBeGreaterThan(-1)
    expect(tokenBreakdownIndex).toBeGreaterThan(serviceHealthIndex)
    expect(costTrendIndex).toBeGreaterThan(tokenBreakdownIndex)
    expect(chartLineSelectorIndex).toBeGreaterThan(costTrendIndex)
    expect(chartsGridIndex).toBeGreaterThan(chartLineSelectorIndex)
  })

  it('keeps chart line controls aligned with reusable pill controls', () => {
    expect(chartLineSelectorSource).toContain('className={styles.usagePillControl}')
    expect(chartLineSelectorSource).toContain('className={styles.usagePillAction}')
  })

  it('aligns Request Event Log pagination with credential pagination height', () => {
    expect(usagePageStyles).toMatch(/\.requestEventsCard:global\(\.card\)\s*\{[\s\S]*?padding:\s*0;/)
    expect(requestEventsSource).toContain('className={styles.requestEventsCard}')
    expect(usagePageStyles).toMatch(/\.requestEventsPaginationFooter\s*\{[\s\S]*?--usage-pagination-bar-height:\s*51px;/)
    expect(usagePageStyles).toMatch(/\.requestEventsPaginationFooter\s*\{[\s\S]*?height:\s*var\(--usage-pagination-bar-height\);/)
    expect(usagePageStyles).toMatch(/\.requestEventsPaginationFooter\s*\{[\s\S]*?box-sizing:\s*border-box;/)
    expect(usagePageStyles).toMatch(/\.requestEventsPaginationFooter\s*\{[\s\S]*?align-items:\s*center;/)
    expect(usagePageStyles).toMatch(/\.requestEventsPaginationFooter\s*\{[\s\S]*?padding:\s*0 22px;/)
  })

  it('keeps Request Event Log headers visible while the table scrolls', () => {
    expect(usagePageStyles).toMatch(/\.requestEventsTableWrapper\s*\{[\s\S]*?height:\s*clamp\(520px,\s*68vh,\s*760px\);/)
    expect(usagePageStyles).toMatch(/\.requestEventsTableWrapper\s*\{[\s\S]*?overflow:\s*auto;/)
    expect(usagePageStyles).toMatch(/\.requestEventsTableWrapper\s*\{[\s\S]*?thead\s+th\s*\{[\s\S]*?position:\s*sticky;/)
    expect(usagePageStyles).toMatch(/\.requestEventsTableWrapper\s*\{[\s\S]*?thead\s+th\s*\{[\s\S]*?top:\s*0;/)
    expect(usagePageStyles).toMatch(/\.requestEventsTableWrapper\s*\{[\s\S]*?thead\s+th\s*\{[\s\S]*?z-index:\s*2;/)
    expect(usagePageStyles).toMatch(/\.requestEventsTableWrapper\s*\{[\s\S]*?\.table\s*\{[\s\S]*?border-collapse:\s*separate;/)
  })

  it('themes the WebKit scrollbar corner so intersecting scrollbars do not show a white square', () => {
    expect(globalStyles).toMatch(/::-webkit-scrollbar-corner\s*\{[\s\S]*?background:\s*var\(--bg-secondary\);/)
  })

  it('renders Request Event Log with a single outer frame instead of a nested table card', () => {
    const cardBlock = usagePageStyles.slice(
      usagePageStyles.indexOf('.requestEventsCard:global(.card) {'),
      usagePageStyles.indexOf('.requestEventsTitleRow')
    )
    const tableWrapperBlock = usagePageStyles.slice(
      usagePageStyles.indexOf('.requestEventsTableWrapper {'),
      usagePageStyles.indexOf('.requestEventsTimestamp')
    )

    expect(cardBlock).toMatch(/padding:\s*0;/)
    expect(cardBlock).toMatch(/overflow:\s*hidden;/)
    expect(cardBlock).toMatch(/:global\(\.card-header\)\s*\{[\s\S]*?margin-bottom:\s*0;/)
    expect(cardBlock).toMatch(/:global\(\.card-header\)\s*\{[\s\S]*?border-bottom:\s*1px solid var\(--border-color\);/)
    expect(tableWrapperBlock).toMatch(/border:\s*0;/)
    expect(tableWrapperBlock).toMatch(/border-radius:\s*0;/)
    expect(tableWrapperBlock).not.toMatch(/border:\s*1px solid/)
  })

  it('keeps the Request Event Log timestamp column compact', () => {
    expect(usagePageStyles).toMatch(/\.requestEventsTimestamp\s*\{[\s\S]*?width:\s*136px;/)
    expect(usagePageStyles).toMatch(/\.requestEventsTimestamp\s*\{[\s\S]*?min-width:\s*136px;/)
    expect(usagePageStyles).toMatch(/\.requestEventsTimestamp\s*\{[\s\S]*?font-variant-numeric:\s*tabular-nums;/)
  })

  it('keeps the Request Event Log reasoning header on one line without fixing column width', () => {
    expect(usagePageStyles).toMatch(/\.requestEventsReasoningHeader\s*\{[\s\S]*?white-space:\s*nowrap;/)
    expect(usagePageStyles).not.toMatch(/\.requestEventsReasoningHeader\s*\{[^}]*width:/)
    expect(requestEventsSource).toContain('<th className={styles.requestEventsReasoningHeader}>{t(\'usage_stats.reasoning_tokens\')}</th>')
  })

  it('keeps Request Event Log model and endpoint columns compact', () => {
    expect(usagePageStyles).toMatch(/\.modelCell\s*\{[\s\S]*?min-width:\s*110px;/)
    expect(usagePageStyles).toMatch(/\.requestEventsEndpointCell\s*\{[\s\S]*?min-width:\s*100px;/)
  })

  it('provides reusable pill controls for usage subpages', () => {
    expect(usagePageStyles).toMatch(/\.usagePillControl\s*\{[\s\S]*?border-radius:\s*999px;/)
    expect(usagePageStyles).toMatch(/\.usagePillAction\s*\{[\s\S]*?border-radius:\s*999px;/)
    expect(usagePageStyles).toMatch(/\.usagePillActionDanger\s*\{[\s\S]*?color:/)
    expect(usagePageStyles).not.toContain('&:global(.btn-danger):hover:not(:disabled)')
    expect(usagePageStyles).toMatch(/:global\(\.input\)\s*\{[^}]*border-radius:\s*999px;/)
    expect(requestEventsSource).toContain('styles.usagePillControl')
    expect(requestEventsSource).toContain('styles.usagePillAction')
    expect(priceSettingsSource).toContain('styles.usagePillControl')
    expect(priceSettingsSource).toContain('styles.usagePillAction')
    expect(priceSettingsSource).toContain('styles.usagePillActionDanger')
  })
})
