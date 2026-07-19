import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const readSource = (url: URL) => readFileSync(url, 'utf8').replace(/\r\n/g, '\n')

const globalStyles = readSource(new URL('../../styles/global.scss', import.meta.url))
const usagePageStyles = readSource(new URL('../UsagePage.module.scss', import.meta.url))
const usageOverviewStyles = readSource(new URL('../../components/usage/UsageOverview.module.scss', import.meta.url))
const requestEventsStyles = readSource(new URL('../../components/usage/RequestEventsDetailsCard.module.scss', import.meta.url))
const usageSettingsStyles = readSource(new URL('../../components/usage/UsageSettings.module.scss', import.meta.url))
const usagePageSource = readSource(new URL('../UsagePage.tsx', import.meta.url))
const usageFilterBarSource = readSource(new URL('../../components/usage/UsageFilterBar.tsx', import.meta.url))
const usageFilterBarStyles = readSource(new URL('../../components/usage/UsageFilterBar.module.scss', import.meta.url))
const keyOverviewPageStyles = readSource(new URL('../KeyOverviewPage.module.scss', import.meta.url))
const keyOverviewPageSource = readSource(new URL('../KeyOverviewPage.tsx', import.meta.url))
const pageLayoutStyles = readSource(new URL('../../components/layout/PageLayout.module.scss', import.meta.url))
const themeStyles = readSource(new URL('../../styles/themes.scss', import.meta.url))
const requestEventsSource = readSource(new URL('../../components/usage/RequestEventsDetailsCard.tsx', import.meta.url))
const priceSettingsSource = readSource(new URL('../../components/usage/PriceSettingsCard.tsx', import.meta.url))
const apiIndexSource = readSource(new URL('../../components/usage/index.ts', import.meta.url))
const apiClientSource = readSource(new URL('../../lib/api.ts', import.meta.url))
const i18nSource = readSource(new URL('../../i18n/index.ts', import.meta.url))
const typesSource = readSource(new URL('../../lib/types.ts', import.meta.url))
const pricingDataSource = readSource(new URL('../../components/usage/hooks/usePricingData.ts', import.meta.url))
const overviewRealtimeDataSource = readSource(new URL('../../components/usage/hooks/useOverviewRealtimeData.ts', import.meta.url))
const apiKeySettingsSource = readSource(new URL('../../components/usage/ApiKeySettingsCard.tsx', import.meta.url))
const sessionSettingsSource = readSource(new URL('../../components/usage/SessionSettingsCard.tsx', import.meta.url))
const analysisPanelSource = readSource(new URL('../../components/usage/analysis/AnalysisPanel.tsx', import.meta.url))
const analysisPanelStyles = readSource(new URL('../../components/usage/analysis/AnalysisPanel.module.scss', import.meta.url))
const overviewRealtimePanelSource = readSource(new URL('../../components/usage/OverviewRealtimePanel.tsx', import.meta.url))
const statCardsSource = readSource(new URL('../../components/usage/StatCards.tsx', import.meta.url))
const serviceHealthSource = readSource(new URL('../../components/usage/ServiceHealthCard.tsx', import.meta.url))
const credentialSectionShellSource = readSource(new URL('../../components/usage/credentials/CredentialSectionShell.tsx', import.meta.url))
const sidebarUtilityActionsSource = readSource(new URL('../../components/layout/SidebarUtilityActions.tsx', import.meta.url))
const sidebarUtilityActionsStyles = readSource(new URL('../../components/layout/SidebarUtilityActions.module.scss', import.meta.url))

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

describe('Usage component style ownership', () => {
  const reusableUsageComponents = [
    ['StatCards', statCardsSource],
    ['ServiceHealthCard', serviceHealthSource],
    ['OverviewRealtimePanel', overviewRealtimePanelSource],
    ['RequestEventsDetailsCard', requestEventsSource],
    ['SessionSettingsCard', sessionSettingsSource],
    ['ApiKeySettingsCard', apiKeySettingsSource],
    ['PriceSettingsCard', priceSettingsSource],
    ['UsageFilterBar', usageFilterBarSource],
  ] as const

  it.each(reusableUsageComponents)('%s owns its styles outside UsagePage', (_name, source) => {
    expect(source).not.toContain("@/pages/UsagePage.module.scss")
  })

  it('keeps feature selectors out of the page-owned stylesheet', () => {
    expect(usagePageStyles).not.toMatch(/\.(dailyAveragePanel|statsPanel|requestEventsCard|overviewRealtimeSection|healthCard|pricingFixedCard|apiKeySettingsCard|sessionSettingsCard)\b/)
  })

  it('uses shared semantic section headings without changing chart internals', () => {
    ;[
      requestEventsSource,
      sessionSettingsSource,
      apiKeySettingsSource,
      priceSettingsSource,
      serviceHealthSource,
      overviewRealtimePanelSource,
      credentialSectionShellSource,
      analysisPanelSource,
    ].forEach((source) => {
      expect(source).toContain('SectionHeader')
      expect(source).toContain('headingLevel={2}')
    })
    expect(overviewRealtimePanelSource).toContain('headingLevel={3}')
    expect(priceSettingsSource).toContain('<h3 id="saved-model-prices-title"')
    expect(priceSettingsSource).not.toContain('<h4 id="saved-model-prices-title"')
  })
})

describe('UsagePage toolbar styles', () => {
  it('removes obsolete Last Updated presentation and API plumbing', () => {
    expect(usagePageSource).not.toContain('lastSyncAt')
    expect(usagePageSource).not.toContain('status?.last_run_at')
    expect(usagePageSource).not.toContain("t('usage_stats.last_updated')")
    expect(usagePageSource).not.toContain('analysisLastRefreshedAt')
    expect(usagePageSource).not.toContain('setAnalysisLastRefreshedAt')
    expect(usagePageStyles).not.toMatch(/\.lastRefreshed\s*\{/)
    expect(keyOverviewPageSource).not.toContain('lastRefreshedAt')
    expect(keyOverviewPageSource).not.toContain('setLastRefreshedAt')
    expect(keyOverviewPageSource).not.toContain("t('usage_stats.last_updated')")
    expect(keyOverviewPageStyles).not.toMatch(/\.(toolbarMetaRow|lastRefreshed)\s*\{/)
    expect(pricingDataSource).not.toContain('lastRefreshedAt')
    expect(pricingDataSource).not.toContain('setLastRefreshedAt')
    expect(overviewRealtimeDataSource).not.toContain('lastRefreshedAt')
    expect(overviewRealtimeDataSource).not.toContain('lastRefreshedAtTs')
    expect(typesSource).not.toContain('last_run_at?: string')
    expect(i18nSource).not.toMatch(/\blast_updated:/)
  })

  it('shares one professional wide content canvas across both dashboards', () => {
    expect(usagePageSource).toContain('<AppShell')
    expect(keyOverviewPageSource).toContain('<AppShell')
    expect(pageLayoutStyles).toContain('width: min(var(--page-max-width), 100%);')
    expect(themeStyles).toContain('--page-max-width: 1440px;')
    expect(usagePageStyles).not.toMatch(/\.container\s*\{/)
    expect(keyOverviewPageStyles).not.toMatch(/\.container\s*\{/)
  })

  it('delegates dashboard header geometry to the shared shell and keeps the wide gutter tokens', () => {
    expect(usagePageSource).toContain("variant={embedded ? 'embed' : 'authenticated'}")
    expect(keyOverviewPageSource).toContain("variant={embedded ? 'embed' : 'viewer'}")
    expect(pageLayoutStyles).toContain('padding: 12px var(--page-gutter) !important;')
    expect(pageLayoutStyles).toContain('padding: var(--page-gutter);')
    expect(pageLayoutStyles).toContain('gap: var(--page-stack);')
    expect(themeStyles).toContain('--page-gutter: 24px;')
    expect(themeStyles).toContain('--page-gutter-mobile: 12px;')
    expect(themeStyles).toContain('--page-stack: 20px;')
    expect(usagePageStyles).toMatch(/\.usageShell\s*\{[\s\S]*?--page-gutter:\s*40px;/)
    expect(usagePageStyles).toMatch(/\.usageShell\s*\{[\s\S]*?--page-header-height:\s*88px;/)
    expect(usagePageStyles).not.toContain('.appHeader')
    expect(keyOverviewPageStyles).not.toMatch(/\.header\s*\{/)
  })

  it('renders dismissible top notices with Ant Design Alert instead of a custom toast layer', () => {
    expect(usagePageSource).toContain('<Alert')
    expect(usagePageSource).toContain('className={styles.topNotice}')
    expect(usagePageSource).toContain('showIcon')
    expect(usagePageSource).toContain('closable')
    expect(usagePageStyles).toMatch(/\.topNotice\s*\{[\s\S]*?width:\s*100%;/)
    expect(usagePageStyles).not.toContain('.updateCheckToast')
  })

  it('keeps shared scope control sizing encapsulated without targeting Ant Design internals', () => {
    expect(usageFilterBarStyles).not.toContain('.apiKeyField')
    expect(usageFilterBarStyles).not.toContain('.rangeField')
    expect(usageFilterBarStyles).not.toContain('.customRangeField')
    expect(usageFilterBarStyles).not.toContain('.ant-form-item')
    expect(usageFilterBarStyles).not.toContain('.ant-select')
    expect(usageFilterBarStyles).not.toContain('.ant-picker')
    expect(usageFilterBarStyles).toContain('--usage-filter-control-height: 36px;')
    expect(usageFilterBarStyles).toMatch(/\.apiKeyControl,[\s\S]*?\.rangeControl,[\s\S]*?\.autoRefreshControl\s*\{[\s\S]*?height:\s*var\(--usage-filter-control-height\);/)
    expect(usageFilterBarStyles).toMatch(/\.timeRangeTrigger\s*\{[\s\S]*?padding-block:\s*calc\(\(var\(--usage-filter-control-height\) - var\(--usage-filter-control-line-height\)\) \/ 2 - 1px\);/)
  })

  it('keeps every compact header control on the same 12px spacing rhythm', () => {
    expect(usageFilterBarSource).toContain('const compactItemStyle = compact ? { marginInlineEnd: 0 } : undefined;')
    expect(usageFilterBarSource.match(/style=\{compactItemStyle\}/g)).toHaveLength(3)
    expect(usageFilterBarSource).toContain('size={isVertical ? 12 : [12, 12]}')
    expect(themeStyles).toContain('--toolbar-gap: 12px;')
  })

  it('renders overview metrics as one Ant Design statistic surface instead of a card wall', () => {
    expect(statCardsSource).toContain("import { Col, Row, Statistic } from 'antd';")
    expect(statCardsSource).toContain('<section className={styles.statsPanel}')
    expect(statCardsSource).toContain('<Statistic')
    expect(statCardsSource).not.toContain('className={styles.statCard}')
    expect(usageOverviewStyles).toMatch(/\.statsPanel\s*\{[\s\S]*?border:\s*1px solid var\(--border-structural\);/)
    expect(usageOverviewStyles).toMatch(/\.statItem\s*\{[\s\S]*?min-height:\s*144px;/)
    expect(usageOverviewStyles).not.toContain('.statCard')
    expect(usageOverviewStyles).not.toContain('.statTrend')
    expect(statCardsSource).not.toContain('Tiny.Line')
    expect(statCardsSource).toContain("import { getChartTheme } from '@/lib/chartTheme';")
    expect(statCardsSource).toContain('const accents = getChartTheme(isDark).series;')
    expect(statCardsSource).toContain('accent: accents.blue.stroke')
    expect(statCardsSource).toContain('accent: accents.teal.stroke')
    expect(usageOverviewStyles).toMatch(/\.statItem\s*\{[\s\S]*?&::before\s*\{[\s\S]*?background:\s*var\(--accent-border\);/)
    expect(usageOverviewStyles).toMatch(/\.statIcon\s*\{[\s\S]*?background:\s*var\(--accent-soft\);/)
    expect(statCardsSource).not.toMatch(/accent:\s*'#[0-9a-f]{6}'/)
  })

  it('keeps daily averages inside their matching stat tiles without inserting a range-dependent panel', () => {
    expect(usagePageSource).not.toContain('<DailyAveragePanel')
    expect(keyOverviewPageSource).not.toContain('<DailyAveragePanel')
    expect(apiIndexSource).not.toContain('DailyAveragePanel')
    expect(usagePageSource).toContain('dailyAverageUsage={dailyAverageUsage}')
    expect(usagePageSource).toContain('showDailyAverages={showDailyAverages}')
    expect(keyOverviewPageSource).toContain('dailyAverageUsage={dailyAverageUsage}')
    expect(keyOverviewPageSource).toContain('showDailyAverages={showDailyAverages}')
    expect(statCardsSource).toContain("t('usage_stats.avg_requests')")
    expect(statCardsSource).toContain("t('usage_stats.avg_tokens')")
    expect(statCardsSource).toContain("t('usage_stats.avg_cost')")
    expect(statCardsSource).toContain('<div className={styles.statContextRow}>{card.context}</div>')
    expect(usageOverviewStyles).not.toContain('.dailyAveragePanel')
  })

  it('opens the model-pricing settings tab from the pricing coverage notice', () => {
    expect(usagePageSource).toContain("setSettingsTab('model-pricing')")
    expect(usagePageSource).toContain("setActiveTab('settings')")
    expect(usagePageSource).toContain('onConfigure={openModelPricingSettings}')
    expect(usagePageSource).toContain('onConfigurePricing={openModelPricingSettings}')
    expect(usagePageSource).toContain('activeKey={settingsTab}')
  })

  it('gives every stat tile the same metadata and context structure', () => {
    expect(usageOverviewStyles).toMatch(/\.statColumn\s*\{[\s\S]*?display:\s*flex;/)
    expect(usageOverviewStyles).toMatch(/\.statItem\s*\{[\s\S]*?flex:\s*1 1 auto;[\s\S]*?width:\s*100%;/)
    expect(usageOverviewStyles).toMatch(/\.statMetaRow\s*\{[\s\S]*?min-height:\s*38px;/)
    expect(usageOverviewStyles).toMatch(/\.statContextRow\s*\{[\s\S]*?min-height:\s*var\(--type-caption-line-height\);/)
    expect(statCardsSource).not.toContain('sparkline')
  })

  it('renders the realtime overview panel below Request Health Timeline with the planned responsive grid', () => {
    expect(usagePageSource).toContain('<OverviewRealtimePanel')
    expect(keyOverviewPageSource).toContain('<OverviewRealtimePanel')
    expect(usagePageSource.indexOf('<ServiceHealthCard usage={usage} loading={overviewDisplayLoading} />')).toBeLessThan(usagePageSource.indexOf('<OverviewRealtimePanel'))
    expect(usageOverviewStyles).toMatch(/\.overviewRealtimeGrid\s*\{[\s\S]*?grid-template-columns:\s*repeat\(2, minmax\(0, 1fr\)\);/)
    expect(usageOverviewStyles).toMatch(/\.overviewRealtimeGrid\s*\{[\s\S]*?@include mobile\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\);/)
    expect(usageOverviewStyles).toMatch(/\.overviewRealtimeCardFull\s*\{[\s\S]*?grid-column:\s*1 \/ -1;/)
    expect(usageOverviewStyles).toMatch(/\.overviewRealtimeWindowSwitcher\s*\{[\s\S]*?border-radius:\s*999px;/)
    expect(usageOverviewStyles).toMatch(/\.overviewRealtimeSection\s*\{[\s\S]*?gap:\s*var\(--section-stack\);[\s\S]*?margin-top:\s*0;/)
    expect(usageOverviewStyles).not.toMatch(/\.overviewRealtimeSection\s*\{[\s\S]*?border-top:/)
    expect(usageOverviewStyles).not.toMatch(/\.overviewRealtimeSection\s*\{[\s\S]*?padding-top:/)
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
    expect(usageOverviewStyles).toContain('.overviewRealtimeEmptyOverlay')
    expect(usageOverviewStyles).toContain('.overviewRealtimeUsageMetaPill')
    expect(usageOverviewStyles).not.toContain('.overviewRealtimeLegend')
    expect(i18nSource).not.toContain('overview_realtime_response_level')
    expect(i18nSource).not.toContain('overview_realtime_ttft_p95')
    expect(i18nSource).not.toContain('overview_realtime_latency_p95')
  })

  it('renders compact shared scope controls inline in the header', () => {
    expect(usagePageSource).toContain('UsageFilterBar,')
    expect(usagePageSource).toContain("from '@/components/usage';")
    expect(usagePageSource).not.toContain('<Popover')
    expect(usagePageSource).toContain('<Drawer')
    expect(usagePageSource).toContain('scopeFiltersOpen')
    expect(usagePageSource).toContain('scopeFilterContent')
    expect(usagePageSource).toContain('scopeFilterControls')
    expect(usagePageSource).not.toContain('scopeSummary')
    expect(usagePageSource).not.toContain('<Collapse')
    expect(usagePageSource).toContain('onClick={() => setScopeFiltersOpen(true)}')
    expect(usagePageSource).toContain('shellHeaderUtility')
    expect(usagePageSource).toContain('headerUtility: shellHeaderUtility')
    expect(usagePageSource).not.toContain('type="primary"')
    expect(usagePageStyles).not.toContain('.headerStack')
    expect(usagePageStyles).not.toContain('.scopeFilterPopover')
    expect(usagePageStyles).toContain('.headerScopeControls')
    expect(usageFilterBarSource).toContain('styles.usageFilterBar')
    expect(usageFilterBarSource).toContain('layout={layout}')
    expect(usageFilterBarSource).toContain("label={compact ? undefined : t('usage_stats.api_key_filter')}")
    expect(usageFilterBarSource).toContain("label={compact ? undefined : t('usage_stats.range_filter')}")
    expect(usageFilterBarSource.indexOf("label={compact ? undefined : t('usage_stats.auto_refresh')}")).toBeLessThan(
      usageFilterBarSource.indexOf("label={compact ? undefined : t('usage_stats.api_key_filter')}")
    )
    expect(usageFilterBarSource.indexOf("label={compact ? undefined : t('usage_stats.api_key_filter')}")).toBeLessThan(
      usageFilterBarSource.indexOf("label={compact ? undefined : t('usage_stats.range_filter')}")
    )
    expect(usageFilterBarSource).toContain('<DatePicker.RangePicker')
    expect(usageFilterBarSource).toContain('<Popover')
    expect(usageFilterBarSource).toContain("t('usage_stats.range_quick_ranges')")
    expect(usageFilterBarSource).toContain("t('usage_stats.range_absolute')")
    expect(usageFilterBarSource).not.toContain('isCustomRange &&')
    expect(usageFilterBarSource).not.toContain('type="date"')
    expect(usageFilterBarStyles).not.toContain('.ant-form-item')
    expect(usageFilterBarStyles).toMatch(/\.apiKeyControl\s*\{[\s\S]*?width:\s*220px;/)
  })

  it('moves the overview refresh cadence into the shared scope controls and drops the manual overview refresh', () => {
    expect(usagePageSource).not.toContain('useHeaderRefresh')
    expect(usagePageSource).not.toContain('manualRefreshLoading')
    expect(usagePageSource).not.toContain('<PageToolbar')
    expect(usagePageSource).toContain('showAutoRefresh: showAutoRefreshControl')
    expect(usagePageSource).toContain('autoRefreshOptions: overviewAutoRefreshOptions')
    expect(usagePageSource).toContain('onRefresh={() => void handleEventsRefresh()}')
    expect(usagePageSource).toContain('refreshing={eventsLoading}')
    expect(requestEventsSource).toContain('onRefresh?: () => void')
    expect(requestEventsSource).toContain('<ReloadOutlined />')
  })

  it('hoists the Analysis refresh action into the shell header for the analysis tab only', () => {
    expect(usagePageSource).toContain("headerActions: activeTab === 'analysis' ? (")
    expect(usagePageSource).toContain('className={styles.analysisRefreshControl}')
    expect(usagePageSource).toContain('loading={analysisLoading}')
    expect(usagePageSource).toContain('onClick={() => void loadAnalysis()}')
    expect(usagePageSource).toContain("{t('usage_stats.refresh')}")
    const analysisRefreshControl = styleRuleBlock(usagePageStyles, '.analysisRefreshControl:global(.ant-btn)')
    expect(analysisRefreshControl).toContain('height: 36px;')
    expect(analysisRefreshControl).toContain('padding-block: 6px;')
    expect(analysisRefreshControl).toContain('line-height: 22px;')
    const analysisHeaderAction = usagePageSource.slice(
      usagePageSource.indexOf("headerActions: activeTab === 'analysis'"),
      usagePageSource.indexOf('headerUtility: shellHeaderUtility'),
    )
    expect(analysisHeaderAction).not.toContain('size="small"')
    expect(usagePageSource).not.toContain('refreshing={analysisLoading}')
    expect(analysisPanelSource).not.toContain('onRefresh')
    expect(analysisPanelSource).not.toContain('refreshing')
    expect(analysisPanelSource).not.toContain("t('usage_stats.tab_analysis')")
    expect(analysisPanelSource).not.toContain('ReloadOutlined')
    expect(analysisPanelSource).toContain('AnalysisCardHeader')
  })

  it('uses restrained Ant Design boundaries for Request Events and Settings', () => {
    expect(styleRuleBlock(requestEventsStyles, '.requestEventsCard:global(.ant-card)')).toContain('box-shadow: none;')
    expect(usagePageSource).toContain('<Tabs')
    expect(usagePageSource).toContain('className={styles.settingsTabs}')
    expect(usagePageSource).toContain('destroyOnHidden={false}')
    expect(usagePageSource.match(/forceRender: true/g)).toHaveLength(3)
    expect(usagePageStyles).toMatch(/\.settingsTabs\s*\{[\s\S]*?:global\(\.ant-tabs-tabpane > \.ant-card\)\s*\{[\s\S]*?box-shadow:\s*none;/)
    expect(usagePageStyles).not.toContain('.settingsSections')
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

  it('uses an authenticated native request log download URL instead of fetching a blob into memory', () => {
    expect(apiClientSource).toContain('createUsageEventRequestLogDownloadURL')
    expect(apiClientSource).toContain('/request-log/download-token')
    expect(apiClientSource).not.toContain('downloadUsageEventRequestLog')
    expect(apiClientSource).not.toContain('getUsageEventRequestLogDownloadURL')
    expect(usagePageSource).toContain('triggerBrowserURLDownload')
    expect(usagePageSource).toContain('createDownloadURL = createUsageEventRequestLogDownloadURL')
    expect(usagePageSource).toContain('const downloadURL = await createDownloadURL(normalizedEventId)')
    expect(usagePageSource).not.toContain('downloadUsageEventRequestLog(normalizedEventId)')
    const downloadHandler = usagePageSource.slice(
      usagePageSource.indexOf('const handleRequestLogDownload = useCallback'),
      usagePageSource.indexOf('const handleOverviewRefresh = useCallback'),
    )
    expect(downloadHandler).not.toContain("showTopNotice('success'")
    expect(downloadHandler).toContain("showTopNotice('error'")
    expect(downloadHandler).not.toContain('handleRequestLogClose()')
  })

  it('cancels request log work when UsagePage unmounts', () => {
    const cleanupStart = usagePageSource.indexOf('useEffect(() => () => {\n    requestLogDownloadGenerationRef.current += 1;')
    expect(cleanupStart).toBeGreaterThanOrEqual(0)
    const cleanupEnd = usagePageSource.indexOf('\n  }, []);', cleanupStart)
    expect(cleanupEnd).toBeGreaterThan(cleanupStart)
    const cleanupEffect = usagePageSource.slice(cleanupStart, cleanupEnd)

    expect(cleanupEffect).toContain('requestLogControllerRef.current?.abort();')
    expect(cleanupEffect).toContain('requestLogControllerRef.current = null;')
    expect(cleanupEffect).not.toContain('setRequestLog')
  })

  it('delegates the authenticated application shell to AppShell', () => {
    expect(usagePageSource).toContain('<AppShell')
    expect(usagePageSource).toContain("variant={embedded ? 'embed' : 'authenticated'}")
    expect(usagePageSource).toContain('<Menu')
    expect(usagePageSource).toContain('<Drawer')
    expect(usagePageSource).not.toContain('<Sider')
    expect(usagePageSource).not.toContain('<Layout')
    expect(usagePageSource).not.toContain('<PageHeader')
    expect(usagePageSource).not.toContain('<PageContent')
    expect(usagePageSource).not.toContain('<PageTitle')
    expect(usagePageStyles).toMatch(/\.usageShell\s*\{[\s\S]*?width:\s*min\(var\(--app-shell-max-width\), 100%\);[\s\S]*?margin-inline:\s*auto;/)
    expect(usagePageStyles).not.toMatch(/\.appShell\s*\{/)
    expect(usagePageStyles).not.toMatch(/\.appSider\s*\{/)
    expect(usagePageStyles).not.toMatch(/\.appMain\s*\{/)
    expect(usagePageStyles).not.toContain('margin-inline-start: 232px;')
    expect(usagePageStyles).not.toContain('.syncSwitcher')
    expect(usagePageStyles).not.toContain('.syncPill')
    expect(usagePageStyles).not.toContain('.refreshButton')
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

  it('keeps utility actions in the sidebar footer instead of the page header', () => {
    expect(usagePageSource).toContain('<SidebarUtilityActions')
    expect(usagePageSource.match(/<SidebarUtilityActions/g)).toHaveLength(1)
    expect(usagePageSource).toContain('sidebarUtility: sidebarUtilityActions')
    expect(usagePageSource).toContain('styles.mobileNavigationActions')
    expect(usagePageSource).not.toContain('<PreferencesDropdown />')
    expect(usagePageSource).not.toContain('styles.headerActions')
    expect(usagePageStyles).toContain('.mobileNavigationMenu')
    expect(usagePageStyles).toContain('.mobileNavigationActions')
  })

  it('keeps the Ant Design utility action semantics in the sidebar action component', () => {
    expect(sidebarUtilityActionsSource.indexOf("t('usage_stats.check_updates')")).toBeLessThan(sidebarUtilityActionsSource.indexOf("t('common.logout')"))
    expect(sidebarUtilityActionsSource).toContain('icon={<LogoutOutlined />}')
    expect(sidebarUtilityActionsSource).toContain('danger')
    expect(sidebarUtilityActionsSource).toContain('aria-pressed={hasNewVersion}')
    expect(sidebarUtilityActionsStyles).toContain('.utilityButton')
    expect(sidebarUtilityActionsSource).toContain('type="text"')
    expect(sidebarUtilityActionsStyles).toContain('.preferencesActions')
    expect(sidebarUtilityActionsSource).toContain('section="language"')
    expect(sidebarUtilityActionsSource).toContain('section="appearance"')
  })

  it('delegates desktop and mobile navigation labels to Ant Design Menu', () => {
    expect(usagePageSource).toContain('<Menu')
    expect(usagePageSource).toContain('items={navigationItems}')
    expect(usagePageSource).toContain('mode="inline"')
    expect(usagePageStyles).not.toContain('.tabPill')
    expect(usagePageStyles).not.toContain('.tabPillActive')
  })

  it('renders API Key Settings as an Ant Design table with standard controls', () => {
    expect(apiKeySettingsSource).toContain('Table,')
    expect(apiKeySettingsSource).toContain('Input,')
    expect(apiKeySettingsSource).toContain('<Table<CpaApiKeySettingsItem>')
    expect(apiKeySettingsSource).toContain('pagination={false}')
    expect(apiKeySettingsSource).toContain('rowKey={getApiKeySettingsRowKey}')
    expect(apiKeySettingsSource).toContain("type=\"primary\"")
    expect(apiKeySettingsSource).toContain('copyApiKeyToClipboard(item.apiKey)')
    expect(usageSettingsStyles).toMatch(/\.apiKeySettingsTable:global\(\.ant-table-wrapper\)\s*\{[\s\S]*?:global\(\.ant-table-tbody > tr > td\)/)
    expect(usageSettingsStyles).not.toContain('.apiKeySettingsItem')
    expect(usagePageStyles).not.toContain('.apiKeyAliasInput')
  })

  it('renders Session Management as an Ant Design table with controlled confirmation', () => {
    expect(sessionSettingsSource).toContain('Table,')
    expect(sessionSettingsSource).toContain('Popconfirm,')
    expect(sessionSettingsSource).toContain('<Table<AuthManagedSessionItem>')
    expect(sessionSettingsSource).toContain('pagination={false}')
    expect(sessionSettingsSource).toContain('rowKey={getSessionRowKey}')
    expect(sessionSettingsSource).toContain('open={isOpen}')
    expect(sessionSettingsSource).toContain('loading: isRevoking')
    expect(sessionSettingsSource).toContain('if (session.current)')
    expect(usageSettingsStyles).toMatch(/\.sessionSettingsTable:global\(\.ant-table-wrapper\)\s*\{[\s\S]*?:global\(\.ant-table-tbody > tr > td\)/)
    expect(usageSettingsStyles).not.toContain('.sessionSettingsItem')
    expect(usageSettingsStyles).not.toContain('.sessionSettingsLogoutButton')
  })

  it('removes legacy compact action styling from migrated Settings tables', () => {
    expect(apiKeySettingsSource).not.toContain('styles.settingsCompactAction')
    expect(sessionSettingsSource).not.toContain('styles.settingsCompactAction')
    expect(usageSettingsStyles).not.toContain('.settingsCompactAction')
  })

  it('delegates the Model Pricing viewport to an Ant Design table', () => {
    const pricingBlock = usageSettingsStyles.slice(
      usageSettingsStyles.indexOf('.pricingFixedCard:global(.ant-card) {'),
      usageSettingsStyles.indexOf('.priceForm')
    )

    expect(pricingBlock).toMatch(/\.pricingFixedCard:global\(\.ant-card\)\s*\{[\s\S]*?height:\s*auto;/)
    expect(priceSettingsSource).toContain('<Table<PricingTableRow>')
    expect(priceSettingsSource).toContain('pagination={false}')
    expect(priceSettingsSource).toContain('scroll={{ x: 1080, y: 480 }}')
    expect(usageSettingsStyles).toMatch(/\.pricingTable:global\(\.ant-table-wrapper\)\s*\{[\s\S]*?:global\(\.ant-table-tbody > tr > td\)/)
    expect(usageSettingsStyles).not.toContain('.pricesGrid')
    expect(usageSettingsStyles).not.toContain('.priceItem')
    expect(usageSettingsStyles).not.toContain('.apiKeySettingsBody')
  })

  it('uses an Ant Design Form that reflows from four to two to one column', () => {
    expect(priceSettingsSource).toContain('<Form layout="vertical" className={styles.priceForm}>')
    expect(priceSettingsSource).toContain('<Form.Item')
    expect(priceSettingsSource).toContain('className={styles.priceFormModelField}')
    expect(priceSettingsSource).toContain('className={styles.priceFormAction}')
    expect(usageSettingsStyles).toMatch(/\.priceForm\s*\{[\s\S]*?container-name:\s*model-pricing-form;/)
    expect(usageSettingsStyles).toMatch(/\.priceForm\s*\{[\s\S]*?container-type:\s*inline-size;/)
    expect(usageSettingsStyles).toMatch(/\.formRow\s*\{[\s\S]*?display:\s*grid;/)
    expect(usageSettingsStyles).toMatch(/\.formRow\s*\{[\s\S]*?:global\(\.ant-form-item\)\s*\{[\s\S]*?margin-bottom:\s*0;/)
    expect(usageSettingsStyles).toMatch(/\.formRow\s*\{[\s\S]*?grid-template-columns:\s*minmax\(240px, 1\.4fr\) minmax\(130px, 0\.85fr\) repeat\(5, minmax\(120px, 1fr\)\) auto;/)
    expect(priceSettingsSource).toContain('const MODEL_PRICE_FILTER_POPUP_WIDTH = 360;')
    expect(priceSettingsSource).toContain('popupMatchSelectWidth={MODEL_PRICE_FILTER_POPUP_WIDTH}')
    expect(usageSettingsStyles).toMatch(/@container model-pricing-form \(max-width:\s*1120px\)\s*\{[\s\S]*?grid-template-columns:\s*repeat\(4, minmax\(0, 1fr\)\);/)
    expect(usageSettingsStyles).toMatch(/@container model-pricing-form \(max-width:\s*720px\)\s*\{[\s\S]*?grid-template-columns:\s*repeat\(2, minmax\(0, 1fr\)\);[\s\S]*?\.priceFormModelField,[\s\S]*?\.priceFormAction\s*\{[\s\S]*?grid-column:\s*1 \/ -1;/)
    expect(usageSettingsStyles).toMatch(/@container model-pricing-form \(max-width:\s*480px\)\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\);/)
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
    expect(analysisPanelSource).not.toContain("import '@/lib/chartjs'")
    expect(overviewRealtimePanelSource).not.toContain("import '@/lib/chartjs'")
    expect(overviewRealtimePanelSource).toContain("import { Base, Line, type LineConfig } from '@ant-design/charts'")
    expect(overviewRealtimePanelSource).toContain('<Line {...tokenChartConfig} />')
    expect(overviewRealtimePanelSource).toContain('<Base {...ttftDistributionConfig} />')
    expect(analysisPanelSource).toContain("import { Base, Line, Pie, type LineConfig } from '@ant-design/charts'")
    expect(analysisPanelSource).not.toContain("from 'react-chartjs-2'")
    expect(analysisPanelSource).not.toContain("from 'chart.js'")
    expect(usagePageSource).not.toContain('ChartJS.register(')
    expect(usagePageSource).not.toContain("from 'chart.js'")
    expect(analysisPanelSource).toContain('<Base {...tokenConfig} />')
    expect(analysisPanelSource).toContain('<Line {...requestConfig} />')
    expect(analysisPanelSource).toContain('<Line {...costConfig} />')
    expect(analysisPanelSource).toContain("type: 'lineY'")
    expect(analysisPanelSource).toContain("const activeContentKey = activeTab?.id ?? 'empty';")
    expect(analysisPanelSource).toContain('<Pie key={`chart-${activeContentKey}`} {...chartConfig} />')
    expect(analysisPanelSource).toContain('innerRadius: 0.58')
    expect(analysisPanelSource).toContain('COMPOSITION_DONUT_HOVER_OFFSET / 5')
    expect(analysisPanelSource).toContain("type: 'lineX'")
    expect(analysisPanelSource).toContain('<Base {...chart.config} />')
    expect(analysisPanelSource).toContain('getStableEntityColor')
    expect(analysisPanelSource).toContain('labelFill: chartTheme.textSecondary')
    expect(analysisPanelSource).toContain('analysis_cost_per_million_tokens')
    expect(analysisPanelSource).toContain('analysis_blended_rate')
    expect(analysisPanelSource).toContain('styles.costStackFloatingTooltip')
    expect(analysisPanelSource).toContain('onMouseEnter={(event) => showCostTooltip(segment.tooltipLines, event)}')
    expect(analysisPanelSource).not.toContain('createLinearGradient')
    expect(analysisPanelSource).not.toContain('createRadialGradient')
    expect(analysisPanelSource).toContain('className={styles.costRateMetric}')
    expect(analysisPanelSource).not.toContain('yAxisID')
    expect(analysisPanelSource).toContain('buildTokenStackConfig')
    expect(analysisPanelSource).toContain('buildCompositionConfig')
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
    expect(analysisPanelSource).toContain('onMouseEnter={(event) => onShowTooltip(header.tooltipLines, event)}')
    expect(analysisPanelSource).toContain('onFocus={(event) => onShowTooltip(header.tooltipLines, event)}')
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
    expect(analysisPanelStyles).toMatch(/\.costRateMetric \+ \.costRateMetric\s*\{[\s\S]*?border-left:\s*1px solid var\(--border-color\);/)
    expect(analysisPanelSource).not.toContain('costRateSparkline')
    expect(analysisPanelStyles).not.toContain('.costRateSparkline')
    expect(analysisPanelStyles).toMatch(/\.costRateMetric\s*\{[\s\S]*?justify-content:\s*flex-start;/)
    const costMetricGridBlock = styleRuleBlock(analysisPanelStyles, '.costMetricGrid')
    expect(costMetricGridBlock).toContain('grid-template-columns: repeat(4, minmax(0, 1fr));')
    expect(costMetricGridBlock).toMatch(/@include tablet\s*\{[\s\S]*?grid-template-columns:\s*repeat\(2, minmax\(0, 1fr\)\);/)
    expect(costMetricGridBlock).toMatch(/@include mobile\s*\{[\s\S]*?grid-template-columns:\s*1fr;/)
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
    expect(analysisPanelStyles).not.toContain('.modelEfficiencyFloatingTooltip')
    expect(analysisPanelStyles).toMatch(/\.chartDataTable\s*\{[\s\S]*?clip:\s*rect\(0, 0, 0, 0\);/)
    expect(analysisPanelStyles).toMatch(/\.tokenSmallMultiples\s*\{[\s\S]*?grid-template-columns:\s*repeat\(2, minmax\(0, 1fr\)\);/)
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

  it('keeps the API key trigger close to masked-key content while preserving a readable popup', () => {
    expect(usagePageSource).toContain("import { Alert, Button as AntButton, Drawer, Menu, Modal, Tabs } from 'antd';")
    expect(sidebarUtilityActionsSource).toContain('<PreferencesDropdown')
    expect(usagePageSource).not.toContain('<Segmented')
    expect(usageFilterBarSource).toContain('<Select')
    expect(usageFilterBarSource).toContain('const COMPACT_API_KEY_POPUP_WIDTH = 220;')
    expect(usageFilterBarSource).toContain('popupMatchSelectWidth={compact ? COMPACT_API_KEY_POPUP_WIDTH : undefined}')
    expect(usageFilterBarStyles).toMatch(/\.apiKeyControl\s*\{[\s\S]*?flex:\s*0 0 220px;/)
    expect(usageFilterBarStyles).toMatch(/\.apiKeyControl\s*\{[\s\S]*?min-width:\s*220px;/)
    expect(usageFilterBarStyles).toMatch(/\.rangeControl\s*\{[\s\S]*?min-width:\s*188px;/)
    expect(usageFilterBarStyles).toMatch(/\.autoRefreshControl\s*\{[\s\S]*?min-width:\s*150px;/)
    expect(usageFilterBarStyles).toMatch(/\.verticalControl\s*\{[\s\S]*?width:\s*100%;[\s\S]*?min-width:\s*0;/)
    expect(usageFilterBarSource).toContain('isVertical ? styles.verticalControl')
    expect(usageFilterBarSource).not.toContain('dropdownMinWidth={180}')
    expect(usageFilterBarSource).not.toContain("@/components/ui/Select")
  })

  it('uses a wrapper-only shared filter layout without custom Select skinning', () => {
    expect(usageFilterBarStyles).toMatch(/\.usageFilterBar\s*\{[\s\S]*?width:\s*100%;/)
    expect(usageFilterBarStyles).not.toContain(':global(button)')
    expect(usageFilterBarStyles).not.toContain('.ant-form-item')
    expect(usagePageStyles).not.toContain('.toolbarActionsRight')
  })

  it('keeps quick and custom ranges inside one stable Grafana-style popover', () => {
    expect(usageFilterBarSource).toContain('<Popover')
    expect(usageFilterBarSource).toContain('<DatePicker.RangePicker')
    expect(usageFilterBarSource).toContain('minDate={minimumDate}')
    expect(usageFilterBarSource).toContain('maxDate={maximumDate}')
    expect(usageFilterBarSource).toContain('presetTimeRangeOptions.map')
    expect(usageFilterBarSource).toContain('handleApplyCustomRange')
    expect(usageFilterBarSource).not.toContain('{isCustomRange && (')
  })

  it('delegates custom date interaction and focus treatment to Ant Design', () => {
    expect(usageFilterBarSource).toContain("import { Button, DatePicker, Form, Popover, Select, Space } from 'antd';")
    expect(usageFilterBarSource).toContain('format={DATE_FORMAT}')
    expect(usageFilterBarSource).toContain('onChange={handleDraftCustomRangeChange}')
    expect(usageFilterBarSource).not.toContain('type="date"')
    expect(usageFilterBarSource).not.toContain('showPicker')
    expect(usageFilterBarStyles).not.toContain('.ant-picker')
  })

  it('keeps the shared filter wrapper full width on mobile without restyling controls', () => {
    const mobileToolbarStart = usageFilterBarStyles.indexOf('@include mobile {')
    const mobileToolbarBlock = usageFilterBarStyles.slice(mobileToolbarStart)

    expect(mobileToolbarBlock).toMatch(/\.usageFilterBar\s*\{[\s\S]*?width:\s*100%;/)
    expect(usageFilterBarStyles).not.toContain('.ant-form-item')
    expect(usageFilterBarStyles).not.toContain('.ant-space')
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

  it('gives Ant Design Request Event pagination a stable responsive footer', () => {
    expect(requestEventsStyles).toMatch(/\.requestEventsCard:global\(\.ant-card\)\s*\{[\s\S]*?:global\(\.ant-card-body\)\s*\{[\s\S]*?padding:\s*0;/)
    expect(requestEventsSource).toContain('className={styles.requestEventsCard}')
    expect(requestEventsSource).toContain('<Pagination')
    expect(requestEventsSource).toContain('showSizeChanger')
    expect(requestEventsStyles).toMatch(/\.requestEventsPaginationFooter\s*\{[\s\S]*?min-height:\s*58px;/)
    expect(requestEventsStyles).toMatch(/\.requestEventsPaginationFooter\s*\{[\s\S]*?box-sizing:\s*border-box;/)
    expect(requestEventsStyles).toMatch(/\.requestEventsPaginationFooter\s*\{[\s\S]*?align-items:\s*center;/)
    expect(requestEventsStyles).toMatch(/\.requestEventsPaginationFooter\s*\{[\s\S]*?padding:\s*10px var\(--request-events-inset\);/)
  })

  it('delegates Request Event Log scrolling and sticky headers to Ant Design Table', () => {
    expect(requestEventsSource).toContain('Table,')
    expect(requestEventsSource).toContain('type TableColumnsType,')
    expect(requestEventsSource).toContain("} from 'antd';")
    expect(requestEventsSource).toContain('<Table<RequestEventRow>')
    expect(requestEventsSource).toContain("pagination={false}")
    expect(requestEventsSource).toContain("scroll={{ x: 'max-content', y: 'clamp(520px, 68vh, 760px)' }}")
    expect(requestEventsStyles).toMatch(/\.requestEventsTable\s*\{[\s\S]*?:global\(\.ant-table-body\)\s*\{[\s\S]*?scrollbar-gutter:\s*stable;/)
    expect(requestEventsStyles).not.toMatch(/\.requestEventsTableWrapper\s*\{[\s\S]*?height:\s*clamp\(520px,\s*68vh,\s*760px\);/)
  })

  it('does not retain a global WebKit scrollbar visual contract', () => {
    expect(globalStyles).not.toContain('::-webkit-scrollbar-corner')
    expect(globalStyles).not.toContain('::-webkit-scrollbar-thumb')
  })

  it('renders Request Event Log with a single outer frame instead of a nested table card', () => {
    const cardBlock = requestEventsStyles.slice(
      requestEventsStyles.indexOf('.requestEventsCard:global(.ant-card) {'),
      requestEventsStyles.indexOf('.requestEventsCountBadge')
    )
    const tableWrapperBlock = requestEventsStyles.slice(
      requestEventsStyles.indexOf('.requestEventsTableWrapper {'),
      requestEventsStyles.indexOf('.requestEventsNoWrapCell')
    )

    expect(cardBlock).toMatch(/overflow:\s*hidden;/)
    expect(cardBlock).toMatch(/box-shadow:\s*none;/)
    expect(cardBlock).toMatch(/:global\(\.ant-card-body\)\s*\{[\s\S]*?padding:\s*0;/)
    expect(tableWrapperBlock).toMatch(/border:\s*0;/)
    expect(tableWrapperBlock).toMatch(/border-radius:\s*0;/)
    expect(tableWrapperBlock).not.toMatch(/border:\s*1px solid/)
  })

  it('keeps Request Event Log adaptive columns free of legacy column styles', () => {
    expect(requestEventsStyles).not.toContain('.requestEventsTimestamp')
    expect(requestEventsStyles).not.toContain('.requestEventsReasoningHeader')
    expect(requestEventsStyles).not.toContain('.requestEventsEndpointCell')
    expect(requestEventsStyles).not.toContain('.durationCell')
    expect(requestEventsSource).not.toContain('styles.requestEventsTimestamp')
    expect(requestEventsSource).not.toContain('styles.requestEventsReasoningHeader')
    expect(requestEventsSource).not.toContain('styles.requestEventsEndpointCell')
    expect(requestEventsSource).not.toContain('styles.durationCell')
  })

  it('uses the shared adaptive style for the Request Event Log reasoning column', () => {
    expect(requestEventsStyles).not.toContain('.requestEventsReasoningHeader')
    expect(requestEventColumnDefinitionBlock('reasoning_tokens')).toContain('styles.requestEventsNoWrapCell')
  })

  it('keeps Request Event Log long text columns controlled', () => {
    expect(requestEventsStyles).toMatch(/\.requestEventsAPIKeyCell\s*\{[\s\S]*?min-width:\s*135px;/)
    expect(requestEventsStyles).toMatch(/\.requestEventsAPIKeyCell\s*\{[\s\S]*?max-width:\s*240px;/)
    expect(requestEventsStyles).toMatch(/\.requestEventsSourceCell\s*\{[\s\S]*?min-width:\s*165px;/)
    expect(requestEventsStyles).toMatch(/\.modelCell\s*\{[\s\S]*?min-width:\s*110px;/)
    expect(requestEventsStyles).toMatch(/\.modelCell\s*\{[\s\S]*?max-width:\s*240px;/)
    expect(requestEventsStyles).not.toContain('.requestEventsAuthIndex')
    expect(requestEventsStyles).not.toContain('.requestEventsEndpointCell')
  })

  it('keeps the Speed Mode tooltip target on the normal arrow cursor', () => {
    const speedModeCellBlock = styleRuleBlock(requestEventsStyles, '.requestEventsSpeedModeCell')
    expect(speedModeCellBlock).toContain('cursor: default;')
    expect(speedModeCellBlock).not.toContain('cursor: help;')
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
      'cache_read_tokens',
      'cache_creation_tokens',
      'cache_read_rate',
      'total_tokens',
      'total_cost',
    ]
    const noWrapCellBlock = requestEventsStyles.slice(
      requestEventsStyles.indexOf('.requestEventsNoWrapCell {'),
      requestEventsStyles.indexOf('.requestEventsSourceCell')
    )

    expect(noWrapCellBlock).toMatch(/white-space:\s*nowrap;/)
    expect(noWrapCellBlock).toMatch(/font-variant-numeric:\s*tabular-nums;/)
    expect(requestEventsStyles).not.toContain('.requestEventsSpeedCell')

    adaptiveColumnIds.forEach((columnId) => {
      const block = requestEventColumnDefinitionBlock(columnId)
      expect(block).toMatch(/className:[^\n]*styles\.requestEventsNoWrapCell/)
      expect(block).toContain('render:')
    })

    ;['api_key', 'source', 'model'].forEach((columnId) => {
      expect(requestEventColumnDefinitionBlock(columnId)).not.toContain('styles.requestEventsNoWrapCell')
    })
  })

  it('uses Ant Design controls for migrated request-event and pricing surfaces', () => {
    expect(requestEventsSource).not.toContain('styles.usagePillControl')
    expect(requestEventsSource).toContain("<Form layout=\"inline\"")
    expect(requestEventsSource).toContain('<AntSelect')
    expect(requestEventsSource).toContain('<AntButton')

    expect(priceSettingsSource).not.toContain('styles.usagePillControl')
    expect(priceSettingsSource).not.toContain('styles.usagePillAction')
    expect(priceSettingsSource).not.toContain('styles.usagePillActionDanger')
    expect(priceSettingsSource).toContain('<Form layout="vertical" className={styles.priceForm}>')
    expect(priceSettingsSource).toContain('<Table<PricingTableRow>')
    expect(priceSettingsSource).toContain('<Select')
    expect(priceSettingsSource).toContain('<Input')
    expect(priceSettingsSource).toContain('<Button')
    expect(priceSettingsSource).toContain('<Checkbox')
  })

  it('uses Ant Design controls for Request Event filters, export, columns, and pagination', () => {
    expect(requestEventsSource).toContain('Dropdown,')
    expect(requestEventsSource).toContain('Form,')
    expect(requestEventsSource).toContain('Pagination,')
    expect(requestEventsSource).toContain('Select as AntSelect,')
    expect(requestEventsSource).toContain('<DownloadOutlined />')
    expect(requestEventsSource).toContain('<TableOutlined />')
    expect(requestEventsSource).toContain('<Checkbox')
    expect(requestEventsSource).toContain('<Pagination')
    expect(requestEventsSource).toContain('showSizeChanger')
    expect(requestEventsSource).toContain('<Form layout="inline"')
    expect(requestEventsStyles).toMatch(/\.requestEventsFiltersForm\s*\{[\s\S]*?width:\s*100%;/)
    expect(requestEventsStyles).not.toContain('.ant-form-item')
    expect(requestEventsSource).toContain('const REQUEST_EVENT_ENTITY_FILTER_POPUP_WIDTH = 360;')
    expect(requestEventsSource).toContain('className={styles.requestEventsEntityFilter}')
    expect(requestEventsSource).toContain('popupMatchSelectWidth={REQUEST_EVENT_ENTITY_FILTER_POPUP_WIDTH}')
    expect(requestEventsSource).toContain('className={styles.requestEventsResultFilter}')
    expect(requestEventsStyles).toMatch(/\.requestEventsEntityFilter\s*\{[\s\S]*?min-width:\s*clamp\(220px, 24vw, 320px\);/)
    expect(requestEventsStyles).toMatch(/\.requestEventsResultFilter\s*\{[\s\S]*?min-width:\s*140px;/)
    expect(requestEventsStyles).toMatch(/\.requestEventsPaginationControls\s*\{[\s\S]*?justify-content:\s*flex-end;/)
    expect(requestEventsStyles).not.toContain('.requestEventsExportMenu')
    expect(requestEventsStyles).not.toContain('.requestEventsExportDropdown')
    expect(requestEventsStyles).not.toContain('.requestEventsColumnDropdown')
    expect(requestEventsStyles).not.toContain('.requestEventsPagerButton')
    expect(requestEventsStyles).not.toContain('.requestEventsPageSizeControl')
  })
})
