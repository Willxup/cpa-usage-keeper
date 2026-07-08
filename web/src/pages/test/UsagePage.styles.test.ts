import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const readSource = (url: URL) => readFileSync(url, 'utf8').replace(/\r\n/g, '\n')

const globalStyles = readSource(new URL('../../styles/global.scss', import.meta.url))
const usagePageStyles = readSource(new URL('../UsagePage.module.scss', import.meta.url))
const usagePageSource = readSource(new URL('../UsagePage.tsx', import.meta.url))
const keyOverviewPageStyles = readSource(new URL('../KeyOverviewPage.module.scss', import.meta.url))
const keyOverviewPageSource = readSource(new URL('../KeyOverviewPage.tsx', import.meta.url))
const requestEventsSource = readSource(new URL('../../components/usage/RequestEventsDetailsCard.tsx', import.meta.url))
const priceSettingsSource = readSource(new URL('../../components/usage/PriceSettingsCard.tsx', import.meta.url))
const credentialStyles = readSource(new URL('../../components/usage/credentials/CredentialSections.module.scss', import.meta.url))
const selectSource = readSource(new URL('../../components/ui/Select.tsx', import.meta.url))
const apiIndexSource = readSource(new URL('../../components/usage/index.ts', import.meta.url))
const apiClientSource = readSource(new URL('../../lib/api.ts', import.meta.url))
const i18nSource = readSource(new URL('../../i18n/index.ts', import.meta.url))
const apiKeySettingsSource = readSource(new URL('../../components/usage/ApiKeySettingsCard.tsx', import.meta.url))
const sessionSettingsSource = readSource(new URL('../../components/usage/SessionSettingsCard.tsx', import.meta.url))
const analysisPanelSource = readSource(new URL('../../components/usage/analysis/AnalysisPanel.tsx', import.meta.url))
const analysisPanelStyles = readSource(new URL('../../components/usage/analysis/AnalysisPanel.module.scss', import.meta.url))
const overviewRealtimePanelSource = readSource(new URL('../../components/usage/OverviewRealtimePanel.tsx', import.meta.url))
const statCardsSource = readSource(new URL('../../components/usage/StatCards.tsx', import.meta.url))
const dailyAveragePanelSource = readSource(new URL('../../components/usage/DailyAveragePanel.tsx', import.meta.url))

const requestEventColumnDefinitionBlock = (columnId: string) => {
  const start = requestEventsSource.indexOf(`id: '${columnId}',`)
  expect(start).toBeGreaterThanOrEqual(0)
  const next = requestEventsSource.indexOf('\n      {', start + 1)
  const end = next === -1 ? requestEventsSource.indexOf('\n    ];', start) : next
  return requestEventsSource.slice(start, end)
}

const usagePageEffectBlock = (needle: string) => {
  const needleIndex = usagePageSource.indexOf(needle)
  expect(needleIndex).toBeGreaterThanOrEqual(0)
  const start = usagePageSource.lastIndexOf('  useEffect(() => {', needleIndex)
  expect(start).toBeGreaterThanOrEqual(0)
  const end = usagePageSource.indexOf('\n  }, [', start)
  expect(end).toBeGreaterThan(start)
  const close = usagePageSource.indexOf(');', end)
  expect(close).toBeGreaterThan(end)
  return usagePageSource.slice(start, close + 2)
}

const styleRuleBlock = (source: string, selector: string) => {
  const start = source.indexOf(selector)
  expect(start).toBeGreaterThanOrEqual(0)
  const open = source.indexOf('{', start)
  expect(open).toBeGreaterThanOrEqual(0)
  const close = source.indexOf('\n}', open)
  expect(close).toBeGreaterThan(open)
  return source.slice(open + 1, close)
}

describe('UsagePage toolbar styles', () => {
  it('lets dashboard page frames consume the mode-specific width cap', () => {
    expect(usagePageStyles).toMatch(/\.pageFrame\s*\{[\s\S]*?width:\s*min\(var\(--keeper-page-max-width, 1245px\), 100%\);/)
    expect(keyOverviewPageStyles).toMatch(/\.pageFrame\s*\{[\s\S]*?width:\s*min\(var\(--keeper-page-max-width, 1245px\), 100%\);/)
  })

  it('uses shell density variables for dashboard spacing without root zoom', () => {
    expect(usagePageStyles).toMatch(/\.pageShell\s*\{[\s\S]*?padding:\s*var\(--keeper-page-padding-top, 28px\) var\(--keeper-page-padding-x, 20px\) var\(--keeper-page-padding-bottom, 48px\);/)
    expect(keyOverviewPageStyles).toMatch(/\.pageShell\s*\{[\s\S]*?padding:\s*var\(--keeper-page-padding-top, 28px\) var\(--keeper-page-padding-x, 20px\) var\(--keeper-page-padding-bottom, 48px\);/)
    expect(usagePageStyles).toMatch(/\.pageFrame\s*\{[\s\S]*?gap:\s*var\(--keeper-page-frame-gap, 18px\);/)
    expect(keyOverviewPageStyles).toMatch(/\.pageFrame\s*\{[\s\S]*?gap:\s*var\(--keeper-page-frame-gap, 18px\);/)
    expect(usagePageStyles).toMatch(/\.topBar\s*\{[\s\S]*?padding:\s*var\(--keeper-top-bar-padding-y, 18px\) var\(--keeper-top-bar-padding-x, 20px\);/)
    expect(keyOverviewPageStyles).toMatch(/\.topBar\s*\{[\s\S]*?padding:\s*var\(--keeper-top-bar-padding-y, 18px\) var\(--keeper-top-bar-padding-x, 20px\);/)
    expect(usagePageStyles).toMatch(/\.eyebrow\s*\{[\s\S]*?min-height:\s*var\(--keeper-toolbar-control-height, 42px\);/)
    expect(keyOverviewPageStyles).toMatch(/\.eyebrow\s*\{[\s\S]*?min-height:\s*var\(--keeper-toolbar-control-height, 42px\);/)
  })

  it('pins top notices to the viewport instead of the scrolled page body', () => {
    const noticeBlock = usagePageStyles.match(/\.updateCheckToast\s*\{[\s\S]*?\n\}/)?.[0] ?? ''

    expect(noticeBlock).toContain('position: fixed;')
    expect(noticeBlock).toContain('z-index: $z-notification;')
    expect(noticeBlock).not.toContain('position: absolute;')
  })

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

  it('places the Daily Average panel above stat cards with animated responsive styling', () => {
    const usageDailyAverageIndex = usagePageSource.indexOf('<DailyAveragePanel usage={dailyAveragePanelUsage} loading={overviewDisplayLoading} reserveVisible={reserveDailyAveragePanel} />')
    const keyDailyAverageIndex = keyOverviewPageSource.indexOf('<DailyAveragePanel usage={dailyAveragePanelUsage} loading={overviewDisplayLoading} reserveVisible={reserveDailyAveragePanel} />')
    expect(usageDailyAverageIndex).toBeGreaterThanOrEqual(0)
    expect(keyDailyAverageIndex).toBeGreaterThanOrEqual(0)
    expect(usageDailyAverageIndex).toBeLessThan(usagePageSource.indexOf('<StatCards'))
    expect(keyDailyAverageIndex).toBeLessThan(keyOverviewPageSource.indexOf('<StatCards'))
    expect(dailyAveragePanelSource).toContain('buildDailyAverageMetrics')
    expect(dailyAveragePanelSource).not.toContain('dailyAverageIdentityIcon')
    expect(usagePageStyles).toMatch(/\.dailyAveragePanel\s*\{[\s\S]*?transition:[\s\S]*?opacity/)
    expect(usagePageStyles).toMatch(/\.dailyAveragePanelEntering\s*\{[\s\S]*?transform:\s*translateY\(-6px\);/)
    expect(usagePageStyles).toMatch(/\.dailyAveragePanelVisible\s*\{[\s\S]*?opacity:\s*1;/)
    expect(usagePageStyles).toMatch(/\.dailyAverageMetrics\s*\{[\s\S]*?grid-template-columns:\s*repeat\(3, minmax\(0, 1fr\)\);/)
    expect(usagePageStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.dailyAverageMetrics\s*\{[\s\S]*?grid-template-columns:\s*repeat\(2, minmax\(0, 1fr\)\);/)
    expect(usagePageStyles).toMatch(/\.dailyAverageMetricCost\s*\{[\s\S]*?grid-column:\s*1 \/ -1;/)
    expect(usagePageStyles).toContain('@media (prefers-reduced-motion: reduce)')
  })

  it('renders the realtime overview panel below Request Health Timeline with the planned responsive grid', () => {
    expect(usagePageSource).toContain('<OverviewRealtimePanel')
    expect(keyOverviewPageSource).toContain('<OverviewRealtimePanel')
    expect(usagePageSource.indexOf('<ServiceHealthCard usage={usage} loading={overviewDisplayLoading} />')).toBeLessThan(usagePageSource.indexOf('<OverviewRealtimePanel'))
    expect(usagePageStyles).toMatch(/\.overviewRealtimeGrid\s*\{[\s\S]*?grid-template-columns:\s*repeat\(2, minmax\(0, 1fr\)\);/)
    expect(usagePageStyles).toMatch(/\.overviewRealtimeGrid\s*\{[\s\S]*?@include mobile\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\);/)
    expect(usagePageStyles).toMatch(/\.overviewRealtimeCardFull\s*\{[\s\S]*?grid-column:\s*1 \/ -1;/)
    expect(usagePageStyles).toMatch(/\.overviewRealtimeWindowSwitcher\s*\{[\s\S]*?border-radius:\s*999px;/)
    expect(usagePageStyles).toMatch(/\.overviewRealtimeSection\s*\{[\s\S]*?margin-top:\s*12px;/)
    expect(usagePageStyles).not.toMatch(/\.overviewRealtimeSection\s*\{[\s\S]*?border-top:/)
    expect(usagePageStyles).not.toMatch(/\.overviewRealtimeSection\s*\{[\s\S]*?padding-top:/)
    expect(usagePageSource).toContain("value === '15m' || value === '30m' || value === '60m'")
    expect(keyOverviewPageSource).toContain("value === '15m' || value === '30m' || value === '60m'")
    expect(usagePageSource).not.toContain("value === '5m'")
    expect(keyOverviewPageSource).not.toContain("value === '5m'")
  })

  it('keeps realtime overview empty and metadata states explicit without stale legend styles', () => {
    expect(overviewRealtimePanelSource).toContain('overview_realtime_rolling_metric_hint')
    expect(overviewRealtimePanelSource).toContain('overview_realtime_ttft_empty')
    expect(overviewRealtimePanelSource).toContain('overview_realtime_latency_empty')
    expect(overviewRealtimePanelSource).toContain('overview_realtime_cache_empty')
    expect(overviewRealtimePanelSource).toContain('overviewRealtimeUsageMetaPill')
    expect(usagePageStyles).toContain('.overviewRealtimeEmptyOverlay')
    expect(usagePageStyles).toContain('.overviewRealtimeUsageMetaPill')
    expect(usagePageStyles).not.toContain('.overviewRealtimeLegend')
    expect(i18nSource).not.toContain('overview_realtime_response_level')
    expect(i18nSource).not.toContain('overview_realtime_ttft_p95')
    expect(i18nSource).not.toContain('overview_realtime_latency_p95')
  })

  it('keeps refresh controls outside the query filter layout', () => {
    expect(usagePageSource).toContain('{showRangeControls && (\n                  <div className={styles.usageFilterBar}>')
    expect(usagePageSource).toContain('className={styles.usageRefreshSlot}')
    expect(usagePageSource).not.toContain('styles.usageFilterBarCollapsed')
    expect(usagePageStyles).toMatch(/\.usageRefreshSlot\s*\{[\s\S]*?flex:\s*0 0 auto;/)
  })

  it('does not reload Request Events filter options for table query changes', () => {
    const filterOptionsEffect = usagePageEffectBlock('void loadEventFilterOptions();')
    const eventsEffect = usagePageEffectBlock('void loadEvents();')

    expect(filterOptionsEffect).toContain('void loadEventFilterOptions();')
    expect(filterOptionsEffect).not.toContain('void loadEvents();')
    expect(filterOptionsEffect).toContain('}, [activeTab, loadEventFilterOptions]);')
    expect(eventsEffect).toContain('void loadEvents();')
    expect(eventsEffect).not.toContain('loadEventFilterOptions')
    expect(eventsEffect).toContain('}, [activeTab, loadEvents]);')
  })

  it('removes stale header control styles after the Overview chart cleanup', () => {
    expect(usagePageStyles).not.toContain('.syncSwitcher')
    expect(usagePageStyles).not.toContain('.syncPill')
    expect(usagePageStyles).not.toContain('.refreshButton')
    expect(usagePageStyles).not.toContain('.pageTitle')
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
    expect(usagePageSource).toContain('logout')
    expect(usagePageSource).toContain('fetchUpdateCheck')
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

  it('reserves the Session Management action column so current rows keep timestamps aligned', () => {
    expect(usagePageStyles).toMatch(/\.sessionSettingsItem\s*\{[\s\S]*?grid-template-columns:\s*minmax\(160px, 0\.8fr\) minmax\(220px, 1\.2fr\) minmax\(92px, auto\);/)
    expect(usagePageStyles).toMatch(/\.sessionSettingsLogoutButton\s*\{[\s\S]*?min-width:\s*92px;/)
  })

  it('keeps Session and API Key Settings row actions compact like Model Pricing actions', () => {
    const apiKeyButtonsBlock = usagePageStyles.slice(
      usagePageStyles.indexOf('.apiKeySettingsCopyButton,'),
      usagePageStyles.indexOf('.sessionSettingsCard:global(.card)')
    )
    const sessionButtonBlock = usagePageStyles.slice(
      usagePageStyles.indexOf('.sessionSettingsLogoutButton {'),
      usagePageStyles.indexOf('.sessionSettingsConfirmText')
    )

    expect(usagePageStyles).toMatch(/\.settingsCompactAction\s*\{[\s\S]*?min-height:\s*32px;/)
    expect(usagePageStyles).toMatch(/\.settingsCompactAction\s*\{[\s\S]*?padding:\s*7px 12px;/)
    expect(apiKeyButtonsBlock).not.toContain('min-height: 40px;')
    expect(sessionButtonBlock).not.toContain('min-height: 40px;')
    expect(apiKeySettingsSource).toContain('styles.settingsCompactAction')
    expect(sessionSettingsSource).toContain('styles.settingsCompactAction')
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
    expect(analysisPanelSource).toContain("import '@/lib/chartjs'")
    expect(overviewRealtimePanelSource).toContain("import '@/lib/chartjs'")
    expect(analysisPanelSource).toContain("import { Bar, Doughnut, Scatter } from 'react-chartjs-2'")
    expect(usagePageSource).not.toContain('ChartJS.register(')
    expect(usagePageSource).not.toContain("from 'chart.js'")
    expect(analysisPanelSource).toContain('<Bar data={chartData} options={chartOptions} plugins={[drawRequestsLineOnTopPlugin, drawTokenAverageLinePlugin]} />')
    expect(analysisPanelSource).toContain("id: 'analysis-token-average-line'")
    expect(analysisPanelSource).toContain("const activeContentKey = `${activeTab?.id ?? 'empty'}:${items.map((item) => item.key).join('|')}`")
    expect(analysisPanelSource).toContain('<Doughnut key={`chart-${activeContentKey}`} data={chartData} options={chartOptions} />')
    expect(analysisPanelSource).toContain('hoverOffset: COMPOSITION_DONUT_HOVER_OFFSET')
    expect(analysisPanelSource).toContain("position: 'analysisCompositionCursor'")
    expect(analysisPanelSource).toContain('analysisCompositionCursor')
    expect(analysisPanelSource).toContain('<Scatter data={chartData} options={chartOptions} plugins={[modelEfficiencyTooltipPointerPlugin]} />')
    expect(analysisPanelSource).toContain("id: 'analysis-model-efficiency-tooltip-pointer'")
    expect(analysisPanelSource).toContain("cost: '#14b8a6'")
    expect(analysisPanelSource).toContain('ticks: { color: chartTheme.textSecondary')
    expect(analysisPanelSource).toContain('analysis_cost_per_million_tokens')
    expect(analysisPanelSource).toContain('analysis_blended_rate')
    expect(analysisPanelSource).toContain('styles.costStackFloatingTooltip')
    expect(analysisPanelSource).toContain('onMouseEnter={(event) => showCostTooltip(tooltipLines, event)}')
    expect(analysisPanelSource).toContain('createLinearGradient')
    expect(analysisPanelSource).not.toContain('createRadialGradient')
    expect(analysisPanelSource).toContain('className={styles.costRateMetric}')
    expect(analysisPanelSource).toContain("yAxisID: 'cost'")
    expect(analysisPanelSource).toContain('buildAnalysisTokenChartOptions')
    expect(analysisPanelSource).toContain('buildCompositionChartData')
    expect(analysisPanelSource).toContain('className={styles.donutCanvasBox}')
    expect(analysisPanelSource).toContain('className={styles.compositionUsageList}')
    expect(analysisPanelSource).toContain('className={styles.compositionUsageMetaPill}')
    expect(analysisPanelSource).not.toContain('className={styles.compositionTable}')
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
    const compositionLayoutBlock = styleRuleBlock(analysisPanelStyles, '.compositionLayout')
    expect(compositionLayoutBlock).toContain('min-height: 340px;')
    expect(analysisPanelStyles).toMatch(/\.compositionLayout\s*\{[\s\S]*?@include mobile\s*\{[\s\S]*?grid-template-columns:\s*1fr;/)
    expect(analysisPanelStyles).toMatch(/\.compositionUsageItem\s*\{[\s\S]*?border-bottom:\s*1px solid var\(--border-color\);/)
    expect(analysisPanelStyles).toMatch(/\.compositionUsageTrack\s*\{[\s\S]*?height:\s*5px;/)
    expect(analysisPanelStyles).toMatch(/\.compositionUsageBar\s*\{[\s\S]*?background:\s*linear-gradient\(90deg, color-mix\(in srgb, var\(--composition-bar-color\) 70%, var\(--bg-secondary\)\), var\(--composition-bar-color\)\);/)
    expect(analysisPanelStyles).toMatch(/\.compositionUsageMetaPill\s*\{[\s\S]*?border-radius:\s*999px;/)
    const compositionUsageListBlock = styleRuleBlock(analysisPanelStyles, '.compositionUsageList')
    expect(compositionUsageListBlock).toContain('justify-content: center;')
    expect(compositionUsageListBlock).toContain('min-height: 340px;')
    const donutChartFrameBlock = styleRuleBlock(analysisPanelStyles, '.donutChartFrame')
    expect(donutChartFrameBlock).toContain('align-self: center;')
    expect(donutChartFrameBlock).toContain('display: flex;')
    expect(donutChartFrameBlock).toContain('align-items: center;')
    expect(donutChartFrameBlock).toContain('justify-content: center;')
    expect(donutChartFrameBlock).toContain('min-height: 340px;')
    expect(donutChartFrameBlock).toMatch(/@include mobile\s*\{[\s\S]*?min-height:\s*0;/)
    expect(donutChartFrameBlock).not.toContain('height: 260px;')
    const donutCanvasBoxBlock = styleRuleBlock(analysisPanelStyles, '.donutCanvasBox')
    expect(donutCanvasBoxBlock).toContain('position: relative;')
    expect(donutCanvasBoxBlock).toContain('width: min(100%, 340px);')
    expect(donutCanvasBoxBlock).toContain('height: auto;')
    expect(donutCanvasBoxBlock).toContain('aspect-ratio: 1;')
    expect(donutCanvasBoxBlock).toContain('flex: 0 1 340px;')
    expect(donutCanvasBoxBlock).toContain('max-width: 100%;')
    expect(donutCanvasBoxBlock).toMatch(/@include mobile\s*\{[\s\S]*?width:\s*min\(100%, 260px\);/)
    expect(donutCanvasBoxBlock).toMatch(/@include mobile\s*\{[\s\S]*?height:\s*auto;/)
    const compositionUsageMetaPillBlock = styleRuleBlock(analysisPanelStyles, '.compositionUsageMetaPill')
    expect(compositionUsageMetaPillBlock).toContain('max-width: 100%;')
    expect(compositionUsageMetaPillBlock).toContain('min-width: 0;')
    expect(compositionUsageMetaPillBlock).toContain('flex-wrap: wrap;')
    expect(analysisPanelStyles).toMatch(/\.modelEfficiencyFloatingTooltip\s*\{[\s\S]*?pointer-events:\s*none;/)
    expect(analysisPanelStyles).toMatch(/\.compositionTabActive\s*\{[\s\S]*?background:\s*color-mix\(in srgb, var\(--bg-primary\) 84%, var\(--bg-secondary\)\);/)
    expect(analysisPanelStyles).not.toMatch(/\.compositionTabActive\s*\{[\s\S]*?#2563eb/)
    expect(analysisPanelStyles).toMatch(/\.heatmapCardLight \.analysisChartSurface\s*\{[\s\S]*?background:\s*color-mix/)
    expect(analysisPanelStyles).toMatch(/\.heatmapCardDark \.analysisChartSurface\s*\{[\s\S]*?background:\s*var\(--bg-secondary\);/)
    expect(analysisPanelStyles).toMatch(/\.heatmapCardDark\s*\{[\s\S]*?\.heatmapCorner,\s*\.heatmapHeaderCell\s*\{[\s\S]*?background:\s*color-mix\(in srgb, var\(--bg-tertiary\) 72%, var\(--bg-primary\)\);/)
    expect(analysisPanelStyles).not.toContain('#100e16')
    expect(analysisPanelStyles).not.toContain('#17131d')
    expect(analysisPanelStyles).not.toContain('.heatmapCell::before')
    const heatmapCellBlock = [...analysisPanelStyles.matchAll(/\.heatmapCell\s*\{([\s\S]*?)\n\}/g)]
      .map((match) => match[1])
      .find((block) => block.includes('font-variant-numeric: tabular-nums;')) ?? ''
    expect(heatmapCellBlock).toContain('box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.10);')
    expect(heatmapCellBlock).not.toContain('inset 0 -10px 18px')
    const heatmapCellFocusBlock = [...analysisPanelStyles.matchAll(/\.heatmapCell:focus-visible\s*\{([\s\S]*?)\n\}/g)]
      .map((match) => match[1])[0] ?? ''
    expect(heatmapCellFocusBlock).toContain('box-shadow: 0 0 0 2px color-mix(in srgb, var(--heatmap-focus-color, #d86a4a) 70%, transparent), inset 0 0 0 1px rgba(255, 255, 255, 0.12);')
    expect(analysisPanelStyles).not.toContain('--heatmap-flame-alpha')
    expect(analysisPanelStyles).not.toContain('radial-gradient(circle at 50% 115%')
    expect(analysisPanelStyles).toMatch(/\.heatmapCorner,\s*\.heatmapHeaderCell\s*\{[\s\S]*?min-height:\s*48px;/)
    const heatmapRowLabelBlock = [...analysisPanelStyles.matchAll(/\.heatmapRowLabel\s*\{([\s\S]*?)\n\}/g)]
      .map((match) => match[1])
      .find((block) => block.includes('display: flex;')) ?? ''
    expect(heatmapRowLabelBlock).toContain('height: 30px;')
    expect(heatmapRowLabelBlock).toContain('align-self: center;')
    expect(analysisPanelStyles).toMatch(/\.heatmapModelLabel\s*\{[\s\S]*?-webkit-line-clamp:\s*2;/)
    expect(analysisPanelStyles).toMatch(/\.heatmapModelLabel\s*\{[\s\S]*?overflow-wrap:\s*anywhere;/)
    expect(analysisPanelStyles).toMatch(/\.heatmapLegendRamp\s*\{[\s\S]*?linear-gradient\(90deg, #fff7ed, #fed7aa, #fb923c, #ef4444\)/)
    expect(analysisPanelStyles).toMatch(/\.heatmapCardDark \.heatmapLegendRamp\s*\{[\s\S]*?linear-gradient\(90deg, #3a2430, #7a2f3b, #ef4444\)/)
    expect(analysisPanelStyles).not.toContain('#1a1118')
    expect(analysisPanelStyles).not.toContain('#4a1f23')
    expect(analysisPanelStyles).not.toContain('#7c2d12')
    expect(analysisPanelStyles).not.toContain('#fde68a')
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

  it('passes realtime error state and current data guard to the realtime panel', () => {
    expect(usagePageSource).toContain('error: realtimeError')
    expect(usagePageSource).toContain('const displayRealtimeError = realtimeError')
    expect(usagePageSource).toContain('realtime={currentRealtime ?? undefined}')
    expect(usagePageSource).toContain('error={displayRealtimeError}')
  })

  it('removes the Overview Request Health Timeline label instead of toggling it off', () => {
    expect(usagePageSource).toContain('<ServiceHealthCard usage={usage} loading={overviewDisplayLoading} />')
    expect(usagePageSource).not.toContain('showEyebrow')
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
      usagePageStyles.indexOf('.requestEventsNoWrapCell')
    )

    expect(cardBlock).toMatch(/padding:\s*0;/)
    expect(cardBlock).toMatch(/overflow:\s*hidden;/)
    expect(cardBlock).toMatch(/:global\(\.card-header\)\s*\{[\s\S]*?margin-bottom:\s*0;/)
    expect(cardBlock).toMatch(/:global\(\.card-header\)\s*\{[\s\S]*?border-bottom:\s*1px solid var\(--border-color\);/)
    expect(tableWrapperBlock).toMatch(/border:\s*0;/)
    expect(tableWrapperBlock).toMatch(/border-radius:\s*0;/)
    expect(tableWrapperBlock).not.toMatch(/border:\s*1px solid/)
  })

  it('keeps Request Event Log adaptive columns free of legacy column styles', () => {
    expect(usagePageStyles).not.toContain('.requestEventsTimestamp')
    expect(usagePageStyles).not.toContain('.requestEventsReasoningHeader')
    expect(usagePageStyles).not.toContain('.requestEventsEndpointCell')
    expect(usagePageStyles).not.toContain('.durationCell')
    expect(requestEventsSource).not.toContain('styles.requestEventsTimestamp')
    expect(requestEventsSource).not.toContain('styles.requestEventsReasoningHeader')
    expect(requestEventsSource).not.toContain('styles.requestEventsEndpointCell')
    expect(requestEventsSource).not.toContain('styles.durationCell')
  })

  it('uses the shared adaptive style for the Request Event Log reasoning column', () => {
    expect(usagePageStyles).not.toContain('.requestEventsReasoningHeader')
    expect(requestEventColumnDefinitionBlock('reasoning_tokens')).toContain('styles.requestEventsNoWrapCell')
  })

  it('keeps Request Event Log long text columns controlled', () => {
    expect(usagePageStyles).toMatch(/\.requestEventsAPIKeyCell\s*\{[\s\S]*?min-width:\s*135px;/)
    expect(usagePageStyles).toMatch(/\.requestEventsAPIKeyCell\s*\{[\s\S]*?max-width:\s*240px;/)
    expect(usagePageStyles).toMatch(/\.requestEventsSourceCell\s*\{[\s\S]*?min-width:\s*165px;/)
    expect(usagePageStyles).toMatch(/\.modelCell\s*\{[\s\S]*?min-width:\s*110px;/)
    expect(usagePageStyles).toMatch(/\.modelCell\s*\{[\s\S]*?max-width:\s*240px;/)
    expect(usagePageStyles).not.toContain('.requestEventsAuthIndex')
    expect(usagePageStyles).not.toContain('.requestEventsEndpointCell')
  })

  it('keeps Request Event Log non-text columns adaptive and non-wrapping', () => {
    const adaptiveColumnIds = [
      'timestamp',
      'reasoning_effort',
      'service_tier',
      'result',
      'request_type',
      'endpoint',
      'ttft',
      'latency',
      'speed',
      'input_tokens',
      'output_tokens',
      'reasoning_tokens',
      'cached_tokens',
      'cache_rate',
      'total_tokens',
      'total_cost',
    ]
    const noWrapCellBlock = usagePageStyles.slice(
      usagePageStyles.indexOf('.requestEventsNoWrapCell {'),
      usagePageStyles.indexOf('.requestEventsSourceCell')
    )

    expect(noWrapCellBlock).toMatch(/white-space:\s*nowrap;/)
    expect(noWrapCellBlock).toMatch(/font-variant-numeric:\s*tabular-nums;/)
    expect(usagePageStyles).not.toContain('.requestEventsSpeedCell')

    adaptiveColumnIds.forEach((columnId) => {
      const block = requestEventColumnDefinitionBlock(columnId)
      expect(block).toMatch(/header:\s*<th[^>]*styles\.requestEventsNoWrapCell/)
      expect(block).toMatch(/renderCell:[\s\S]*<td[^>]*styles\.requestEventsNoWrapCell/)
    })

    ;['api_key', 'source', 'model'].forEach((columnId) => {
      expect(requestEventColumnDefinitionBlock(columnId)).not.toContain('styles.requestEventsNoWrapCell')
    })
  })

  it('provides reusable pill controls for usage subpages', () => {
    expect(usagePageStyles).toMatch(/\.usagePillControl\s*\{[\s\S]*?border-radius:\s*999px;/)
    expect(usagePageStyles).toMatch(/\.usagePillAction\s*\{[\s\S]*?border-radius:\s*999px;/)
    expect(usagePageStyles).toMatch(/\.usagePillAction\s*\{[\s\S]*?font-size:\s*12px;/)
    expect(usagePageStyles).toMatch(/\.usagePillAction:global\(\.btn\.btn-sm\)\s*\{[\s\S]*?min-height:\s*32px;[\s\S]*?padding:\s*7px 12px;[\s\S]*?font-size:\s*12px;/)
    expect(usagePageStyles).toMatch(/\.usagePillActionDanger\s*\{[\s\S]*?color:/)
    expect(usagePageStyles).not.toContain('&:global(.btn-danger):hover:not(:disabled)')
    expect(usagePageStyles).toMatch(/:global\(\.input\)\s*\{[^}]*border-radius:\s*999px;/)
    expect(requestEventsSource).toContain('styles.usagePillControl')
    expect(requestEventsSource).toContain('styles.usagePillAction')
    expect(priceSettingsSource).toContain('styles.usagePillControl')
    expect(priceSettingsSource).toContain('styles.usagePillAction')
    expect(priceSettingsSource).toContain('styles.usagePillActionDanger')
  })

  it('keeps the Request Event export menu styled and hoverable like the credential inspection control', () => {
    const exportMenuBlock = usagePageStyles.slice(
      usagePageStyles.indexOf('.requestEventsExportMenu {'),
      usagePageStyles.indexOf('.requestEventsExportButton:global(.btn) {')
    )
    const exportButtonBlock = usagePageStyles.slice(
      usagePageStyles.indexOf('.requestEventsExportButton:global(.btn) {'),
      usagePageStyles.indexOf('.requestEventsExportButtonInner {')
    )
    const exportDropdownBlock = usagePageStyles.slice(
      usagePageStyles.indexOf('.requestEventsExportDropdown {'),
      usagePageStyles.indexOf('.requestEventsToolbar {')
    )
    const clearFilterSlotBlock = styleRuleBlock(usagePageStyles, '.requestEventsFilterActionSlot')
    const clearFilterButtonBlock = styleRuleBlock(usagePageStyles, '.requestEventsClearFiltersButton:global(.btn)')
    const credentialRefreshActiveBlock = credentialStyles.slice(
      credentialStyles.indexOf('.credentialRefreshButtonActive,'),
      credentialStyles.indexOf('.credentialRefreshButtonInner {')
    )

    expect(requestEventsSource).toContain('styles.requestEventsExportButton')
    expect(requestEventsSource).toContain('styles.requestEventsExportButtonInner')
    expect(requestEventsSource).toContain('<IconDownload size={12} aria-hidden="true" />')
    expect(requestEventsSource).toContain('styles.requestEventsFilterActionSlot')
    expect(exportMenuBlock).toMatch(/min-height:\s*42px;/)
    expect(exportMenuBlock).toMatch(/padding:\s*4px;/)
    expect(exportMenuBlock).toMatch(/align-items:\s*center;/)
    expect(exportMenuBlock).not.toMatch(/padding-bottom:\s*6px;/)
    expect(exportMenuBlock).not.toMatch(/margin-bottom:\s*-6px;/)
    expect(exportMenuBlock).toContain('&::after')
    expect(exportMenuBlock).toMatch(/border-radius:\s*999px;/)
    expect(exportButtonBlock).toMatch(/border:\s*0;/)
    expect(exportButtonBlock).toMatch(/min-height:\s*32px;/)
    expect(exportButtonBlock).toMatch(/padding:\s*7px 12px;/)
    expect(exportButtonBlock).toMatch(/\.requestEventsExportButton:global\(\.btn\.btn-sm\)\s*\{[\s\S]*?min-height:\s*32px;[\s\S]*?padding:\s*7px 12px;[\s\S]*?font-size:\s*12px;/)
    expect(credentialRefreshActiveBlock).toMatch(/background:\s*var\(--bg-primary\);/)
    expect(exportButtonBlock).toMatch(/background:\s*var\(--bg-primary\);/)
    expect(exportButtonBlock).toMatch(/&:global\(\.btn-secondary\),[\s\S]*?&:global\(\.btn-secondary\):hover:not\(:disabled\),[\s\S]*?&:global\(\.btn-secondary\)\[aria-expanded='true'\]\s*\{[\s\S]*?background:\s*var\(--bg-primary\);[\s\S]*?background-color:\s*var\(--bg-primary\);/)
    expect(exportButtonBlock).toMatch(/font-size:\s*12px;/)
    expect(exportButtonBlock).toMatch(/box-shadow:\s*0 8px 20px rgba\(0,\s*0,\s*0,\s*0\.1\);/)
    expect(exportDropdownBlock).toMatch(/top:\s*calc\(100% \+ 6px\);/)
    expect(clearFilterSlotBlock).toMatch(/display:\s*flex;/)
    expect(clearFilterSlotBlock).toMatch(/align-items:\s*center;/)
    expect(clearFilterSlotBlock).toMatch(/align-self:\s*flex-end;/)
    expect(clearFilterSlotBlock).toMatch(/min-height:\s*40px;/)
    expect(clearFilterButtonBlock).toMatch(/min-height:\s*32px;/)
    expect(clearFilterButtonBlock).not.toContain('margin-bottom')
    expect(usagePageStyles).toMatch(/\.requestEventsClearFiltersButton:global\(\.btn\.btn-sm\)\s*\{[\s\S]*?min-height:\s*32px;[\s\S]*?padding:\s*7px 12px;[\s\S]*?font-size:\s*12px;/)
  })
})
