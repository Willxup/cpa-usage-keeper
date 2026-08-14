import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const readSource = (url: URL) => readFileSync(url, 'utf8').replace(/\r\n/g, '\n')

const globalStyles = readSource(new URL('../../styles/global.scss', import.meta.url))
const componentsStyles = readSource(new URL('../../styles/components.scss', import.meta.url))
const usagePageStyles = readSource(new URL('../UsagePage.module.scss', import.meta.url))
const usagePageSource = readSource(new URL('../UsagePage.tsx', import.meta.url))
const keyOverviewPageStyles = readSource(new URL('../KeyOverviewPage.module.scss', import.meta.url))
const keyOverviewPageSource = readSource(new URL('../KeyOverviewPage.tsx', import.meta.url))
const requestEventsSource = readSource(new URL('../../components/usage/RequestEventsDetailsCard.tsx', import.meta.url))
const requestEventsColumnSettingsSource = readSource(new URL('../../components/usage/RequestEventsColumnSettingsModal.tsx', import.meta.url))
const priceSettingsSource = readSource(new URL('../../components/usage/PriceSettingsCard.tsx', import.meta.url))
const priceRulesSource = readSource(new URL('../../components/usage/pricing/PriceRulesModal.tsx', import.meta.url))
const priceRulesHelpSource = readSource(new URL('../../components/usage/pricing/PriceRulesHelp.tsx', import.meta.url))
const priceRulesStyles = readSource(new URL('../../components/usage/pricing/PriceRulesModal.module.scss', import.meta.url))
const credentialStyles = readSource(new URL('../../components/usage/credentials/CredentialSections.module.scss', import.meta.url))
const selectSource = readSource(new URL('../../components/ui/Select.tsx', import.meta.url))
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
const overviewActivityCardsSource = readSource(new URL('../../components/usage/OverviewActivityCards.tsx', import.meta.url))
const activityHeatmapGridSource = readSource(new URL('../../components/usage/ActivityHeatmapGrid.tsx', import.meta.url))
const serviceHealthCardSource = readSource(new URL('../../components/usage/ServiceHealthCard.tsx', import.meta.url))
const tokenActivityCardSource = readSource(new URL('../../components/usage/TokenActivityCard.tsx', import.meta.url))
const statCardsSource = readSource(new URL('../../components/usage/StatCards.tsx', import.meta.url))
const dailyAverageCardSource = readSource(new URL('../../components/usage/DailyAverageCard.tsx', import.meta.url))
const customRangePanelSource = readSource(new URL('../../components/usage/CustomRangePanel.tsx', import.meta.url))
const timeRangeControlSource = readSource(new URL('../../components/usage/TimeRangeControl.tsx', import.meta.url))
const timeRangeControlStyles = readSource(new URL('../../components/usage/TimeRangeControl.module.scss', import.meta.url))

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
  it('renders every authenticated page header logo at 20px without pill chrome', () => {
    for (const pageStyles of [usagePageStyles, keyOverviewPageStyles]) {
      const logo = styleRuleBlock(pageStyles, '.eyebrow')

      expect(logo).toMatch(/padding:\s*0;/)
      expect(logo).toMatch(/border-radius:\s*0;/)
      expect(logo).toMatch(/border:\s*0;/)
      expect(logo).toMatch(/background:\s*transparent;/)
      expect(logo).toMatch(/font-size:\s*20px;/)
      expect(logo).not.toContain('box-shadow')
    }
  })

  it('routes Settings and Request Event Log headings through the global Card contract', () => {
    expect(sessionSettingsSource).toContain("title={t('usage_stats.session_settings_title')}")
    expect(sessionSettingsSource).toContain("subtitle={t('usage_stats.session_settings_subtitle')}")
    expect(apiKeySettingsSource).toContain("title={t('usage_stats.api_key_settings_title')}")
    expect(apiKeySettingsSource).toContain("subtitle={t('usage_stats.api_key_settings_subtitle')}")
    expect(apiKeySettingsSource).toContain('titleMeta={')
    expect(priceSettingsSource).toContain("title={t('usage_stats.model_price_settings_title')}")
    expect(priceSettingsSource).toContain("subtitle={t('usage_stats.model_price_settings_subtitle')}")
    expect(requestEventsSource).toContain('variant="flush"')
    expect(requestEventsSource).toContain("title={t('usage_stats.request_events_title')}")
    expect(requestEventsSource).toContain("subtitle={t('usage_stats.request_events_subtitle')}")
    expect(requestEventsSource).toContain('titleMeta={')
  })

  it('keeps list internals square while subtitles use regular global weight', () => {
    const subtitleBlock = styleRuleBlock(componentsStyles, '.keeper-card-subtitle')
    const requestEventsCardBlock = styleRuleBlock(usagePageStyles, '.requestEventsCard:global(.card)')

    expect(subtitleBlock).toContain('font-weight: var(--keeper-card-subtitle-weight);')
    expect(requestEventsCardBlock).not.toContain('border-radius: 24px;')
    expect(usagePageStyles).toMatch(/\.statCard\s*\{[\s\S]*?border-radius:\s*var\(--keeper-card-radius\);/)
    expect(usagePageStyles).toMatch(/\.requestEventsTableWrapper\s*\{[\s\S]*?border:\s*0;[\s\S]*?border-radius:\s*0;/)
  })

  it('routes Analysis and Activity cards through the global surface and heading contract', () => {
    const analysisChartSurface = styleRuleBlock(analysisPanelStyles, '\n.analysisChartSurface {')

    expect(analysisPanelSource.match(/keeper-card-surface/g)).toHaveLength(7)
    expect(analysisPanelSource).toContain('keeper-card-title-track')
    expect(analysisPanelSource).toContain('keeper-card-title')
    expect(analysisPanelSource).toContain('keeper-card-subtitle')
    expect(serviceHealthCardSource).toContain('keeper-card-surface')
    expect(serviceHealthCardSource).toContain('keeper-card-title-track')
    expect(serviceHealthCardSource).toContain('keeper-card-title')
    expect(serviceHealthCardSource).toContain('keeper-card-subtitle')
    expect(tokenActivityCardSource).toContain('keeper-card-surface')
    expect(tokenActivityCardSource).toContain('keeper-card-title-track')
    expect(statCardsSource).not.toContain('keeper-card-surface')
    expect(dailyAverageCardSource).not.toContain('keeper-card-surface')
    expect(analysisChartSurface).toContain('border-radius: $radius-lg;')
  })

  it('keeps only the ranking source switch beside Refresh in the shared top toolbar', () => {
    expect(usagePageSource).not.toContain("import { RankingToolbar }")
    expect(usagePageSource).not.toContain('<RankingToolbar')
    expect(usagePageStyles).not.toContain('.rankingToolbarSlot')
    expect(usagePageSource).toContain("import { RankingScopeSwitch }")
    expect(usagePageSource).toContain('<RankingScopeSwitch')
    expect(usagePageSource).toContain('showRankingScopeControl ? styles.rankingScopeTransitionOpen')
    expect(usagePageSource).not.toContain('buildLocalRankingPreviewLeaderboard')
    expect(usagePageSource).not.toContain('RANKING_PREVIEW_ENABLED')
    expect(usagePageSource).toContain("import { MainActionButton } from '@/components/ui/MainActionButton'")
    expect(usagePageSource).toContain('<MainActionButton')
    expect(keyOverviewPageSource).toContain("import { MainActionButton } from '@/components/ui/MainActionButton'")
    expect(keyOverviewPageSource).toContain('<MainActionButton')
  })

  it('patches the local ranking cache by Key ID after a settings alias save', () => {
    const start = usagePageSource.indexOf('const handleSaveApiKeyAlias = useCallback')
    const end = usagePageSource.indexOf('\n  const handleRevokeAuthSession', start)
    expect(start).toBeGreaterThanOrEqual(0)
    expect(end).toBeGreaterThan(start)

    const handler = usagePageSource.slice(start, end)
    expect(handler).toContain('patchLocalRankingProfileCache(updated.id, {')
    expect(handler).toContain('key_alias: updated.keyAlias')
    expect(handler).toContain('display_name: updated.label')
  })

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

  it('lets dashboard page frames consume the mode-specific width cap', () => {
    expect(usagePageStyles).toMatch(/\.pageFrame\s*\{[\s\S]*?width:\s*min\(var\(--keeper-page-max-width, 1245px\), 100%\);/)
    expect(keyOverviewPageStyles).toMatch(/\.pageFrame\s*\{[\s\S]*?width:\s*min\(var\(--keeper-page-max-width, 1245px\), 100%\);/)
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

  it('uses the shared C time-range control on both dashboard surfaces', () => {
    expect(usagePageSource).toContain('<TimeRangeControl')
    expect(keyOverviewPageSource).toContain('<TimeRangeControl')
    expect(usagePageSource).not.toContain('TimeRangeControlPrototype')
    expect(keyOverviewPageSource).not.toContain('TimeRangeControlPrototype')
    expect(usagePageSource).toContain('parseStoredUsageRangeState')
    expect(keyOverviewPageSource).toContain('parseStoredUsageRangeState')
    expect(timeRangeControlSource).toContain('data-time-range-trigger="desktop"')
    expect(timeRangeControlSource).toContain('data-time-range-trigger="mobile"')
  })

  it('threads the tab-effective custom range through Usage and Key Overview queries', () => {
    expect(usagePageSource).toContain('const [timeRangeState, setTimeRangeState]')
    expect(usagePageSource).toContain('const activeCustomRange = useMemo(() => getUsageCustomRangeForTab(')
    expect(usagePageSource).toContain('const usageRangeQuery = useMemo(() => buildUsageRangeQuery({')
    expect(usagePageSource).toContain('customRange={activeCustomRange}')
    expect(usagePageSource).toContain("maxCustomDayRangeDays={activeTab === 'events' ? REQUEST_EVENTS_CUSTOM_DAY_RANGE_MAX_DAYS : undefined}")
    expect(usagePageSource).toContain('onChange={handleTimeRangeChange}')
    expect(usagePageSource).toContain('fetchAnalysis(usageRangeQuery, controller.signal, selectedApiKeyId)')
    expect(usagePageSource).toContain('fetchAnalysisLatency(usageRangeQuery, controller.signal, selectedApiKeyId)')
    expect(usagePageSource).toContain('fetchUsageEvents(usageRangeQuery, controller.signal, {')
    expect(usagePageSource).toContain('exportUsageEvents(usageRangeQuery, format, {')

    expect(keyOverviewPageSource).toContain('const [timeRangeState, setTimeRangeState]')
    expect(keyOverviewPageSource).toContain('const usageRangeQuery = useMemo(() => buildUsageRangeQuery({')
    expect(keyOverviewPageSource).toContain('customRange={customRange}')
    expect(keyOverviewPageSource).toContain('onChange={handleTimeRangeChange}')
    expect(keyOverviewPageSource).toContain('fetchKeyOverview(usageRangeQuery, controller.signal)')
  })

  it('shows a dedicated notice when Usage Events export capacity is full', () => {
    expect(usagePageSource).toContain('error instanceof ApiError && error.status === 429')
    expect(usagePageSource).toContain("t('usage_stats.export_busy')")
    expect(i18nSource.match(/export_busy:/g)).toHaveLength(3)
  })

  it('recovers applied Custom ranges only after a backend bounds conflict', () => {
    expect(usagePageSource).not.toContain('scheduleCustomRangeBoundsRefresh({')
    expect(keyOverviewPageSource).not.toContain('scheduleCustomRangeBoundsRefresh({')
    expect(usagePageSource).toContain('const recoverRangeBoundsConflict = useCallback')
    expect(keyOverviewPageSource).toContain('const recoverRangeBoundsConflict = useCallback')
    expect(usagePageSource).toContain('if (recoverRangeBoundsConflict(error))')
    expect(keyOverviewPageSource).toContain('if (recoverRangeBoundsConflict(nextError))')
  })

  it('keeps the mobile API Key group and select at full available width', () => {
    const mobileToolbarStart = usagePageStyles.indexOf('@include mobile {\n  .tabPill')
    const mobileToolbarBlock = usagePageStyles.slice(mobileToolbarStart, usagePageStyles.indexOf('@media (prefers-reduced-motion: reduce)'))

    expect(mobileToolbarBlock).toMatch(/\.apiKeyFilterGroup\s*\{[\s\S]*?max-width:\s*100%;/)
    expect(mobileToolbarBlock).toMatch(/\.apiKeySelectControl\s*\{[\s\S]*?width:\s*100%;/)
  })

  it('centers the mobile API Key label beside its select', () => {
    const mobileToolbarStart = usagePageStyles.indexOf('@include mobile {\n  .tabPill')
    const mobileToolbarBlock = usagePageStyles.slice(mobileToolbarStart, usagePageStyles.indexOf('@media (prefers-reduced-motion: reduce)'))

    expect(mobileToolbarBlock).toMatch(/\.apiKeyFilterField\s*\{[\s\S]*?align-items:\s*center;/)
    expect(mobileToolbarBlock).not.toMatch(/\.apiKeyFilterField\s*\{[\s\S]*?align-items:\s*stretch;/)
  })

  it('uses a centered mobile modal and a Codex-style layered slider track', () => {
    const mobileSliderStyles = timeRangeControlStyles.slice(timeRangeControlStyles.indexOf('@include mobile {'))

    expect(timeRangeControlStyles).not.toContain('align-self: flex-end')
    expect(timeRangeControlStyles).not.toContain('margin-bottom: -16px')
    expect(timeRangeControlStyles).toMatch(/\.sliderControl\s*\{[\s\S]*?height:\s*48px;/)
    expect(timeRangeControlStyles).toMatch(/\.sliderRail\s*\{[\s\S]*?height:\s*32px;/)
    expect(timeRangeControlStyles).toContain('.sliderDotActive')
    expect(styleRuleBlock(timeRangeControlStyles, '.sliderDot')).toContain('width: 7px;')
    expect(timeRangeControlStyles).toMatch(/\.rangeInput::-webkit-slider-thumb\s*\{[\s\S]*?width:\s*42px;/)
    expect(styleRuleBlock(timeRangeControlStyles, '.sliderFill')).toContain('linear-gradient(180deg')
    expect(styleRuleBlock(timeRangeControlStyles, '.sliderFill')).toContain('linear-gradient(90deg, #244ccf 0%, #4056e8 22%, #8b58f0 48%, #b45df4 63%, #793feb 82%, #5c33dc 100%)')
    expect(styleRuleBlock(timeRangeControlStyles, '.sliderFill')).toContain('inset 0 1px 0 rgba(255, 255, 255, 0.30)')
    expect(mobileSliderStyles).toMatch(/\.sliderControl\s*\{[\s\S]*?height:\s*52px;/)
    expect(mobileSliderStyles).toMatch(/\.sliderRail\s*\{[\s\S]*?height:\s*34px;/)
    expect(mobileSliderStyles).toMatch(/\.rangeInput::-webkit-slider-thumb\s*\{[\s\S]*?width:\s*46px;/)
  })

  it('lets the liquid cover passed divider dots', () => {
    const coveredDot = [...timeRangeControlStyles.matchAll(/\.sliderDotActive\s*\{([\s\S]*?)\n\}/g)]
      .map((match) => match[1])
      .find((block) => block.includes('opacity: 0;')) ?? ''

    expect(coveredDot).toContain('opacity: 0;')
    expect(coveredDot).not.toContain('background: rgba(205, 234, 255, 0.68);')
  })

  it('matches the API Key labeled double-pill shell before opening', () => {
    const desktopShell = styleRuleBlock(timeRangeControlStyles, '.desktopShell')
    const shellLabel = styleRuleBlock(timeRangeControlStyles, '.shellLabel')
    const mobileSliderStyles = timeRangeControlStyles.slice(timeRangeControlStyles.indexOf('@include mobile {'))

    expect(timeRangeControlSource).toContain('data-time-range-shell="desktop"')
    expect(timeRangeControlSource).toContain('data-time-range-shell="mobile"')
    expect(desktopShell).toContain('gap: 8px;')
    expect(desktopShell).toContain('padding: 5px 6px 5px 12px;')
    expect(shellLabel).toContain('font-size: 10px;')
    expect(shellLabel).toContain('font-weight: 700;')
    expect(mobileSliderStyles).toMatch(/\.mobileShell\s*\{[\s\S]*?display:\s*grid;/)
    expect(mobileSliderStyles).toMatch(/\.mobileShell\s*\{[\s\S]*?grid-template-columns:\s*auto minmax\(0, 1fr\);/)
  })

  it('matches the API Key hover and open states on the desktop Range trigger', () => {
    const desktopHover = styleRuleBlock(timeRangeControlStyles, '.desktopTrigger:hover')
    const desktopOpen = styleRuleBlock(timeRangeControlStyles, ".desktopTrigger[aria-expanded='true']")

    expect(desktopHover).toContain('border-color: var(--border-hover);')
    expect(desktopHover).toContain('background: var(--bg-primary);')
    expect(desktopHover).not.toContain('background: var(--bg-tertiary);')
    expect(desktopOpen).toContain('border-color: var(--primary-color);')
    expect(desktopOpen).toContain('background: var(--bg-primary);')
    expect(desktopOpen).toContain('box-shadow: var(--shadow), 0 0 0 3px rgba($primary-color, 0.18);')
    expect(timeRangeControlSource).toContain('<IconChevronDown size={14} className={styles.triggerChevron} />')
    expect(timeRangeControlStyles).toMatch(/\[aria-expanded='true'\][\s\S]*?\.triggerChevron\s*\{[\s\S]*?transform:\s*rotate\(180deg\);/)
  })

  it('keeps all five range modes fully visible with consistent content-aware spacing', () => {
    const desktopTrigger = styleRuleBlock(timeRangeControlStyles, '.desktopTrigger {')
    const modeSelector = styleRuleBlock(timeRangeControlStyles, '.modeSelector')
    const modeButton = styleRuleBlock(timeRangeControlStyles, '.modeButton,')

    expect(desktopTrigger).toContain('width: 192px;')
    expect(modeSelector).toContain('grid-template-columns: repeat(5, max-content);')
    expect(modeSelector).toContain('justify-content: space-between;')
    expect(modeSelector).toContain('gap: 4px;')
    expect(modeButton).toContain('min-width: max-content;')
    expect(modeButton).toContain('width: auto;')
    expect(modeButton).toContain('white-space: nowrap;')
    expect(modeButton).not.toContain('text-overflow: ellipsis;')
    expect(modeButton).not.toContain('overflow: hidden;')
  })

  it('sizes custom actions like model price row actions', () => {
    expect(customRangePanelSource.match(/appearance="action"/g)).toHaveLength(4)
    expect(customRangePanelSource).not.toContain('styles.customRangeAction')
    expect(timeRangeControlStyles).not.toContain('.customRangeAction')
    expect(componentsStyles).toMatch(/\.btn-action\s*\{[\s\S]*?min-height:\s*32px;/)
  })

  it('uses Keeper theme colors for custom day and hour selections', () => {
    const dayRange = styleRuleBlock(timeRangeControlStyles, '.customCalendarDayInRange')
    const selectedDay = styleRuleBlock(timeRangeControlStyles, '.customCalendarDaySelected')
    const selectedDayOverlay = styleRuleBlock(timeRangeControlStyles, '.customCalendarDaySelected::before')
    const rangeRowStart = styleRuleBlock(timeRangeControlStyles, '.customCalendarRangeRowStart')
    const rangeRowEnd = styleRuleBlock(timeRangeControlStyles, '.customCalendarRangeRowEnd')
    const outsideMonth = styleRuleBlock(timeRangeControlStyles, '.customCalendarDayOutsideMonth')
    const outsideMonthLabel = styleRuleBlock(timeRangeControlStyles, '.customCalendarDayOutsideMonth > span')
    const selectedOutsideMonthLabel = styleRuleBlock(timeRangeControlStyles, '.customCalendarDayOutsideMonth.customCalendarDaySelected > span')
    const rangePanel = styleRuleBlock(timeRangeControlStyles, '.rangePanel')
    const darkRangePanel = styleRuleBlock(timeRangeControlStyles, ":global([data-theme='dark']) .rangePanel")
    const selectedHour = [...timeRangeControlStyles.matchAll(/\.customHourOptionActive\s*\{([\s\S]*?)\n\}/g)]
      .map((match) => match[1])
      .find((block) => block.includes('var(--primary-color)')) ?? ''

    expect(rangePanel).toContain('--custom-calendar-selected-bg: var(--primary-active);')
    expect(rangePanel).toContain('--custom-calendar-selected-text: var(--primary-contrast, #fff);')
    expect(dayRange).not.toContain('var(--range-slider-accent)')
    expect(rangePanel).toContain('--custom-calendar-range-bg: color-mix(in srgb, var(--primary-color) 18%, transparent);')
    expect(darkRangePanel).toContain('--custom-calendar-range-bg: color-mix(in srgb, var(--primary-color) 12%, var(--bg-primary));')
    expect(darkRangePanel).toContain('--custom-calendar-selected-text: var(--bg-primary);')
    expect(dayRange).toContain('background: var(--custom-calendar-range-bg);')
    expect(selectedDay).toContain('background: var(--custom-calendar-range-bg);')
    expect(selectedDay).toContain('color: var(--custom-calendar-selected-text);')
    expect(selectedDayOverlay).toContain("content: '';")
    expect(selectedDayOverlay).toContain('background: var(--custom-calendar-selected-bg);')
    expect(rangeRowStart).toContain('border-radius: 9px 0 0 9px;')
    expect(rangeRowEnd).toContain('border-radius: 0 9px 9px 0;')
    expect(timeRangeControlStyles).toMatch(/\.customCalendarRangeRowStart\.customCalendarRangeRowEnd\s*\{[\s\S]*?border-radius:\s*9px;/)
    expect(outsideMonth).toContain('color: var(--text-secondary);')
    expect(outsideMonth).not.toContain('opacity:')
    expect(outsideMonthLabel).toContain('opacity: 0.58;')
    expect(selectedOutsideMonthLabel).toContain('opacity: 1;')
    expect(selectedHour).toContain('var(--primary-color)')
    expect(selectedHour).toContain('var(--bg-primary)')
    expect(selectedHour).not.toContain('#2563eb')
    expect(selectedHour).not.toContain('#38bdf8')
    expect(selectedHour).not.toContain('#67e8f9')
  })

  it('contains hour-list wheel scrolling at its own boundaries', () => {
    const hourList = styleRuleBlock(timeRangeControlStyles, '.customHourList')

    expect(hourList).toContain('position: relative;')
    expect(hourList).toContain('overscroll-behavior-y: contain;')
  })

  it('animates only custom view changes and disables that motion when requested', () => {
    expect(styleRuleBlock(timeRangeControlStyles, '.customSummary,')).toContain('animation: customRangeViewEnter')
    expect(timeRangeControlStyles).toContain('@keyframes customRangeViewEnter')
    expect(timeRangeControlStyles).toMatch(/@media \(prefers-reduced-motion: reduce\)\s*\{[\s\S]*?\.customSummary,[\s\S]*?\.customPicker\s*\{[\s\S]*?animation:\s*none;/)
  })

  it('centers the fixed timer icon with the mobile current-range label', () => {
    const triggerIcon = styleRuleBlock(timeRangeControlStyles, '.triggerIcon')
    const triggerLabel = styleRuleBlock(timeRangeControlStyles, '.triggerLabel')

    expect(timeRangeControlSource).toContain('<IconTimer size={16} className={styles.triggerIcon} />')
    expect(triggerIcon).toContain('display: block;')
    expect(triggerIcon).toContain('width: 16px;')
    expect(triggerIcon).toContain('height: 16px;')
    expect(triggerIcon).toContain('flex: 0 0 auto;')
    expect(triggerLabel).toContain('line-height: 1;')
  })

  it('defines the blue slider accent inside the portalled range panel', () => {
    expect(styleRuleBlock(timeRangeControlStyles, '.rangePanel')).toContain('--range-slider-accent: #3b82f6;')
    expect(styleRuleBlock(timeRangeControlStyles, '.sliderFill')).toContain('var(--range-slider-accent)')
  })

  it('matches the reference video with flowing blue-violet light and independent particles', () => {
    const liquidFill = styleRuleBlock(timeRangeControlStyles, '.sliderFill')
    const particle = styleRuleBlock(timeRangeControlStyles, '.liquidParticle')

    expect(liquidFill).toContain('overflow: hidden;')
    expect(liquidFill).toContain('#244ccf 0%')
    expect(liquidFill).toContain('#b45df4 63%')
    expect(liquidFill).toContain('#5c33dc 100%')
    expect(timeRangeControlStyles).toContain('animation: liquidGlowPrimary 8s ease-in-out infinite alternate;')
    expect(timeRangeControlStyles).toContain('animation: liquidGlowSecondary 11s ease-in-out infinite alternate-reverse;')
    expect(timeRangeControlStyles).toContain('@keyframes liquidGlowPrimary')
    expect(timeRangeControlStyles).toContain('@keyframes liquidGlowSecondary')
    expect(particle).toContain('animation-duration: var(--liquid-particle-duration);')
    expect(particle).toContain('animation-delay: var(--liquid-particle-delay);')
    expect(timeRangeControlStyles).toContain('animation-name: liquidParticleFloatA;')
    expect(timeRangeControlStyles).toContain('animation-name: liquidParticleFloatB;')
    expect(timeRangeControlStyles).toContain('animation-name: liquidParticleFloatC;')
    expect(timeRangeControlStyles).not.toContain('background-size: 10px 8px, 13px 11px, 17px 14px, 23px 17px;')
  })

  it('freezes the liquid and particles for reduced-motion users', () => {
    expect(timeRangeControlStyles).toMatch(/@media \(prefers-reduced-motion: reduce\)\s*\{[\s\S]*?\.sliderFill::before,[\s\S]*?\.sliderFill::after,[\s\S]*?\.liquidParticle\s*\{[\s\S]*?animation:\s*none;/)
    expect(timeRangeControlStyles).toMatch(/@media \(prefers-reduced-motion: reduce\)\s*\{[\s\S]*?\.liquidParticle\s*\{[\s\S]*?opacity:\s*0\.72;/)
  })

  it('keeps overview stat cards in a primary row plus a four-card desktop grid', () => {
    const statCard = styleRuleBlock(usagePageStyles, '\n.statCard {')

    expect(usagePageStyles).toMatch(/\.primaryStatsRow\s*\{[\s\S]*?display:\s*flex;/)
    expect(usagePageStyles).toMatch(/\.secondaryStatsGrid\s*\{[\s\S]*?grid-template-columns:\s*repeat\(4, minmax\(0, 1fr\)\);/)
    expect(usagePageStyles).toMatch(/\.statLabel\s*\{[\s\S]*?letter-spacing:\s*0;/)
    expect(statCardsSource).toContain('const primaryCards = statsCards.slice(0, 2)')
    expect(statCardsSource).toContain('const secondaryCards = statsCards.slice(2)')
    expect(statCardsSource).toContain("key: 'requests'")
    expect(statCardsSource).toContain("accent: '#3b82f6'")
    expect(statCardsSource).toContain("key: 'cache-read-rate'")
    expect(statCardsSource).toContain("accent: '#14b8a6'")
    expect(statCardsSource.match(/accent:\s*'#[0-9a-f]{6}'/g)).toHaveLength(new Set(statCardsSource.match(/accent:\s*'#[0-9a-f]{6}'/g)).size)
    expect(statCard).toContain('border-radius: var(--keeper-card-radius);')
  })

  it('expands Daily Average as the first compact primary card without changing row height', () => {
    const primaryStatsRowStyles = styleRuleBlock(usagePageStyles, '.primaryStatsRow')

    expect(usagePageSource).not.toContain('<DailyAveragePanel')
    expect(keyOverviewPageSource).not.toContain('<DailyAveragePanel')
    expect(usagePageSource).toContain('dailyAverageUsage={dailyAverageCardUsage}')
    expect(usagePageSource).toContain('reserveDailyAverage={reserveDailyAverageCard}')
    expect(keyOverviewPageSource).toContain('dailyAverageUsage={dailyAverageCardUsage}')
    expect(keyOverviewPageSource).toContain('reserveDailyAverage={reserveDailyAverageCard}')
    expect(statCardsSource).toContain('<DailyAverageCard')
    expect(statCardsSource).toContain('styles.primaryStatsRowExpanded')
    expect(dailyAverageCardSource).toContain('buildDailyAverageMetrics')
    expect(usagePageStyles).toMatch(/\.primaryStatsRow\s*\{[\s\S]*?min-height:\s*176px;/)
    expect(primaryStatsRowStyles).not.toMatch(/(?:^|\n)\s*height:\s*176px;/)
    expect(usagePageStyles).toMatch(/\.dailyAverageSlot\s*\{[\s\S]*?flex:\s*0 1 0;[\s\S]*?margin-right:\s*-14px;/)
    expect(usagePageStyles).toMatch(/\.dailyAverageSlot\s*\{[\s\S]*?display:\s*flex;/)
    expect(usagePageStyles).toMatch(/\.primaryStatsRowExpanded\s*\{[\s\S]*?\.dailyAverageSlot\s*\{[\s\S]*?flex-grow:\s*0\.72;[\s\S]*?margin-right:\s*0;/)
    expect(usagePageStyles).toMatch(/\.primaryStatSlot\s*\{[\s\S]*?display:\s*flex;/)
    expect(usagePageStyles).toMatch(/\.dailyAverageCard\s*\{[\s\S]*?height:\s*100%;/)
    expect(dailyAverageCardSource).toContain('styles.dailyAverageMetricCopy')
    expect(usagePageStyles).toMatch(/\.dailyAverageMetrics\s*\{[\s\S]*?grid-template-rows:\s*repeat\(3, minmax\(0, 1fr\)\);/)
    expect(usagePageStyles).toMatch(/\.dailyAverageMetric\s*\{[\s\S]*?grid-template-columns:\s*28px minmax\(0, 1fr\) auto;/)
    expect(usagePageStyles).toMatch(/\.dailyAverageMetric\s*\{[\s\S]*?border-bottom:\s*1px solid/)
    expect(usagePageStyles).toContain('@media (prefers-reduced-motion: reduce)')
  })

  it('adds a small desktop-only side inset to Daily Average content', () => {
    expect(usagePageStyles).toMatch(/\.statCard\.dailyAverageCard\s*\{[\s\S]*?padding-inline:\s*18px;[\s\S]*?@include desktop\s*\{[\s\S]*?padding-inline:\s*22px;/)
  })

  it('places the Daily Average reduced-motion override after its animation rules', () => {
    const slotStylesIndex = usagePageStyles.indexOf('.dailyAverageSlot {', usagePageStyles.indexOf('// Stats Layout'))
    const reducedMotionIndex = usagePageStyles.indexOf('@media (prefers-reduced-motion: reduce)', slotStylesIndex)
    const reducedMotionStyles = usagePageStyles.slice(reducedMotionIndex)

    expect(reducedMotionIndex).toBeGreaterThan(slotStylesIndex)
    expect(reducedMotionStyles).toMatch(/\.dailyAverageSlot\s*\{[\s\S]*?transition:\s*none;[\s\S]*?transform:\s*none;/)
  })

  it('keeps the shared stat-card shadow visible after Daily Average expands', () => {
    expect(dailyAverageCardSource).toContain('className={`${styles.statCard} ${styles.dailyAverageCard}`}')
    expect(usagePageStyles).toMatch(/\.primaryStatsRowExpanded\s*\{\s*\.dailyAverageSlot\s*\{\s*flex-grow:\s*0\.72;\s*margin-right:\s*0;\s*overflow:\s*visible;/)
  })

  it('lets the Daily Average background fade out instead of repainting the lower-right corner', () => {
    const start = usagePageStyles.indexOf('.statCard.dailyAverageCard')
    const end = usagePageStyles.indexOf('\n.dailyAverageCardHeader', start)
    const dailyAverageCardStyles = usagePageStyles.slice(start, end)

    expect(dailyAverageCardStyles).toContain('radial-gradient(90% 120% at 0% 0%')
    expect(dailyAverageCardStyles).not.toContain('radial-gradient(88% 110% at 100% 100%, rgba(245, 158, 11, 0.12)')
  })

  it('keeps the overview accent strip full-width through the top-left curve', () => {
    const statCardStart = usagePageStyles.indexOf('\n.statCard {')
    const statCardEnd = usagePageStyles.indexOf('\n.primaryStatSlot', statCardStart)
    const statCardStyles = usagePageStyles.slice(statCardStart, statCardEnd)
    const accentCornerStyles = styleRuleBlock(statCardStyles, '&::before')

    expect(statCardStyles).toContain('--stat-card-strip-corner-offset: 12px;')
    expect(statCardStyles).toContain('--stat-card-accent-strip: linear-gradient(')
    expect(accentCornerStyles).toContain('inset: -1px;')
    expect(accentCornerStyles).toContain('border-radius: calc(var(--keeper-card-radius) + 1px);')
    expect(accentCornerStyles).toContain('padding: 4px;')
    expect(accentCornerStyles).toContain('background: var(--stat-card-accent-strip);')
    expect(accentCornerStyles).toContain('clip-path: inset(0 calc(100% - 56px) calc(100% - var(--keeper-card-radius)) 0);')
    expect(accentCornerStyles).toContain('-webkit-mask-composite: xor;')
    expect(accentCornerStyles).toContain('mask-composite: exclude;')
  })

  it('keeps the right-side accent taper smooth across short and wide stat cards', () => {
    const statCardStart = usagePageStyles.indexOf('\n.statCard {')
    const statCardEnd = usagePageStyles.indexOf('\n.primaryStatSlot', statCardStart)
    const statCardStyles = usagePageStyles.slice(statCardStart, statCardEnd)
    const accentBodyStyles = styleRuleBlock(statCardStyles, '&::after')

    expect(statCardStyles).toContain('--stat-card-strip-fade-end: calc(100% - clamp(18px, 4%, 32px));')
    expect(accentBodyStyles).toContain('inset: 0 0 auto;')
    expect(accentBodyStyles).toContain('height: 3px;')
    expect(accentBodyStyles).toContain('background: var(--stat-card-accent-strip);')
    expect(accentBodyStyles.match(/linear-gradient\(90deg/g)).toHaveLength(12)
    expect(accentBodyStyles).toContain('linear-gradient(90deg, #000 0%, #000 calc(var(--stat-card-strip-fade-end) - clamp(70px, 12%, 100px)), transparent var(--stat-card-strip-fade-end)) 0 0 / 100% 17% no-repeat')
    expect(accentBodyStyles).toContain('#000 calc(var(--stat-card-strip-fade-end) - clamp(94px, 16%, 126px)), transparent calc(var(--stat-card-strip-fade-end) - clamp(12px, 2%, 18px))) 0 40% / 100% 17% no-repeat')
    expect(accentBodyStyles).toContain('#000 calc(var(--stat-card-strip-fade-end) - clamp(130px, 24%, 180px)), transparent calc(var(--stat-card-strip-fade-end) - clamp(30px, 5%, 42px))) 0 100% / 100% 17% no-repeat')
    expect(accentBodyStyles).toContain('mask-composite: add, add, add, add, add;')
  })

  it('blends the left corner border into the full-width accent strip without a thickness jump', () => {
    const statCardStart = usagePageStyles.indexOf('\n.statCard {')
    const statCardEnd = usagePageStyles.indexOf('\n.primaryStatSlot', statCardStart)
    const statCardStyles = usagePageStyles.slice(statCardStart, statCardEnd)
    const accentBodyStyles = styleRuleBlock(statCardStyles, '&::after')

    expect(accentBodyStyles).toContain('linear-gradient(90deg, transparent 0%, transparent 12px, #000 32px,')
    expect(accentBodyStyles).toContain('linear-gradient(90deg, transparent 0%, transparent 16px, #000 38px,')
    expect(accentBodyStyles).toContain('linear-gradient(90deg, transparent 0%, transparent 28px, #000 56px,')
  })

  it('compresses Daily Average colors forward before fading out like the other stat cards', () => {
    const dailyAverageStart = usagePageStyles.indexOf('.statCard.dailyAverageCard')
    const dailyAverageEnd = usagePageStyles.indexOf('\n.dailyAverageCardHeader', dailyAverageStart)
    const dailyAverageStyles = usagePageStyles.slice(dailyAverageStart, dailyAverageEnd)

    expect(dailyAverageStyles).toContain('--stat-card-accent-strip: linear-gradient(')
    expect(dailyAverageStyles).toContain('transparent 0%')
    expect(dailyAverageStyles).toContain('#3b82f6 var(--stat-card-strip-corner-offset)')
    expect(dailyAverageStyles).toContain('#8b5cf6 28%')
    expect(dailyAverageStyles).toContain('#f59e0b 48%')
    expect(dailyAverageStyles).toContain('transparent var(--stat-card-strip-fade-end)')
    expect(dailyAverageStyles).not.toContain('&::before')
  })

  it('keeps primary overview cards stacked in one column on mobile', () => {
    expect(usagePageStyles).toMatch(/\.primaryStatsRow\s*\{[\s\S]*?@include mobile\s*\{[\s\S]*?flex-direction:\s*column;[\s\S]*?overflow:\s*visible;/)
    expect(usagePageStyles).toMatch(/\.dailyAverageSlot\s*\{[\s\S]*?@include mobile\s*\{[\s\S]*?flex:\s*0 0 auto;[\s\S]*?width:\s*100%;[\s\S]*?max-height:\s*0;[\s\S]*?margin-right:\s*0;[\s\S]*?margin-bottom:\s*-14px;/)
    expect(usagePageStyles).toMatch(/\.primaryStatsRowExpanded\s*\{[\s\S]*?@include mobile\s*\{[\s\S]*?\.dailyAverageSlot\s*\{[\s\S]*?max-height:\s*220px;[\s\S]*?margin-bottom:\s*0;/)
    expect(usagePageStyles).toMatch(/\.primaryStatSlot\s*\{[\s\S]*?@include mobile\s*\{[\s\S]*?flex:\s*0 0 auto;[\s\S]*?width:\s*100%;/)
  })

  it('renders Recent Activity between the stat cards and realtime metrics', () => {
    const realtimeCard = styleRuleBlock(usagePageStyles, '.overviewRealtimeCard')
    const realtimeCompactCard = styleRuleBlock(usagePageStyles, '.overviewRealtimeCardCompact')

    expect(usagePageSource).toContain('<OverviewRealtimePanel')
    expect(keyOverviewPageSource).toContain('<OverviewRealtimePanel')
    expect(usagePageSource).toContain('<RecentActivityPanel')
    expect(keyOverviewPageSource).toContain('<RecentActivityPanel')
    expect(usagePageSource.indexOf('<StatCards')).toBeLessThan(usagePageSource.indexOf('<RecentActivityPanel'))
    expect(usagePageSource.indexOf('<RecentActivityPanel')).toBeLessThan(usagePageSource.indexOf('<OverviewRealtimePanel'))
    expect(keyOverviewPageSource.indexOf('<StatCards')).toBeLessThan(keyOverviewPageSource.indexOf('<RecentActivityPanel'))
    expect(keyOverviewPageSource.indexOf('<RecentActivityPanel')).toBeLessThan(keyOverviewPageSource.indexOf('<OverviewRealtimePanel'))
    expect(usagePageStyles).toMatch(/\.recentActivityTitle\s*\{[\s\S]*?font-size:\s*17px;[\s\S]*?font-weight:\s*800;/)
    expect(usagePageStyles).toMatch(/\.recentActivityWindowSwitcher\s*\{[\s\S]*?border-radius:\s*999px;/)
    expect(usagePageStyles).toMatch(/\.recentActivityGrid\s*\{[\s\S]*?grid-template-columns:\s*repeat\(auto-fit, minmax\(min\(100%, 530px\), 1fr\)\);/)
    expect(overviewActivityCardsSource.indexOf('<TokenActivityCard')).toBeLessThan(overviewActivityCardsSource.indexOf('<ServiceHealthCard'))
    expect(overviewActivityCardsSource).not.toContain('fetchUsageActivity')
    expect(overviewActivityCardsSource).not.toContain('useUsageActivityData')
    expect(activityHeatmapGridSource).toContain('aria-rowcount={ACTIVITY_GRID_ROWS}')
    expect(activityHeatmapGridSource).toContain('aria-colcount={ACTIVITY_GRID_COLUMNS}')
    expect(usagePageStyles).toContain('--token-activity-level-1: #dbeafe;')
    expect(usagePageStyles).toContain('--token-activity-level-2: #93c5fd;')
    expect(usagePageStyles).toContain('--token-activity-level-3: #60a5fa;')
    expect(usagePageStyles).toContain('--token-activity-level-4: #3b82f6;')
    expect(usagePageStyles).toContain('--token-activity-level-5: #1d4ed8;')
    expect(usagePageStyles).toMatch(/:global\(\[data-theme='dark'\]\) \.tokenActivityCard\s*\{[\s\S]*?--token-activity-level-1:\s*#172554;/)
    expect(usagePageStyles).toMatch(/\.overviewRealtimeGrid\s*\{[\s\S]*?grid-template-columns:\s*repeat\(2, minmax\(0, 1fr\)\);/)
    expect(usagePageStyles).toMatch(/\.overviewRealtimeGrid\s*\{[\s\S]*?@include mobile\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\);/)
    expect(usagePageStyles).toMatch(/\.overviewRealtimeCardFull\s*\{[\s\S]*?grid-column:\s*1 \/ -1;/)
    expect(usagePageStyles).toMatch(/\.overviewRealtimeWindowSwitcher\s*\{[\s\S]*?border-radius:\s*999px;/)
    expect(usagePageStyles).toMatch(/\.overviewRealtimeSection\s*\{[\s\S]*?margin-top:\s*12px;/)
    expect(realtimeCard).toContain('padding: var(--keeper-card-padding);')
    expect(realtimeCard).not.toMatch(/(?:background|border|border-radius|box-shadow):/)
    expect(realtimeCompactCard).not.toContain('padding:')
    expect(usagePageStyles).not.toMatch(/\.overviewRealtimeSection\s*\{[\s\S]*?border-top:/)
    expect(usagePageStyles).not.toMatch(/\.overviewRealtimeSection\s*\{[\s\S]*?padding-top:/)
    expect(usagePageSource).toContain("value === '15m' || value === '30m' || value === '60m'")
    expect(keyOverviewPageSource).toContain("value === '15m' || value === '30m' || value === '60m'")
    expect(usagePageSource).not.toContain("value === '5m'")
    expect(keyOverviewPageSource).not.toContain("value === '5m'")
  })

  it('keeps Recent Activity summaries consistent across desktop and mobile', () => {
    const rangeRule = styleRuleBlock(usagePageStyles, '.recentActivityRange')
    const detailsRule = styleRuleBlock(usagePageStyles, '.activitySummaryDetails')
    const healthCountRule = styleRuleBlock(usagePageStyles, '.healthCountRow')
    const tokenValueRule = styleRuleBlock(usagePageStyles, '.tokenActivitySummaryValue')

    expect(rangeRule).not.toMatch(/\b(border|background|border-radius|min-height|padding):/)
    expect(rangeRule).toContain('white-space: nowrap;')
    expect(detailsRule).toContain('font-size: 10px;')
    expect(healthCountRule).toContain('font-size: 10px;')
    expect(tokenValueRule).toContain('color: #3b82f6;')
    expect(serviceHealthCardSource).not.toContain('styles.requestActivityCard')
    expect(typesSource).toMatch(/UsageActivityRequest\s*=\s*UsageRangeRequest\s*\|\s*\{\s*window:\s*UsageActivityWindow\s*\|\s*'today'\s*\|\s*'yesterday'/)
    expect(usagePageStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.activitySummary\s*\{[\s\S]*?justify-items:\s*start;[\s\S]*?text-align:\s*left;/)
    expect(usagePageStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.activitySummaryDetails\s*\{[\s\S]*?justify-content:\s*flex-start;/)
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

  it('crossfades normal filters and ranking scope in one stable slot while Refresh stays fixed', () => {
    expect(usagePageSource).toContain("${!isEmbeddedInCPAMC ? styles.toolbarActionsRightAnimated : ''}")
    expect(usagePageSource).toContain('{(!isEmbeddedInCPAMC || showRangeControls) && (')
    expect(usagePageSource).not.toContain("activeTab !== 'ranking' &&")
    expect(usagePageSource).toContain('showRangeControls ? styles.usageFilterTransitionOpen : \'\'')
    expect(usagePageSource).toContain('inert={!showRangeControls}')
    expect(usagePageSource).toContain('<div className={styles.usageFilterBar}>')
    expect(usagePageSource).not.toContain("key={showRangeControls ? 'open' : 'closed'}")
    expect(usagePageSource).toContain('className={styles.usageRefreshSlot}')
    expect(usagePageSource).toContain('styles.toolbarContextSlotImmediate : styles.toolbarContextSlot')
    expect(usagePageSource).toContain('styles.rankingScopeTransition')
    expect(usagePageStyles).toMatch(/\.toolbarActionsRightAnimated\s*\{[\s\S]*?display:\s*grid;/)
    expect(usagePageStyles).toMatch(/\.toolbarActionsRightAnimated\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\) auto;/)
    expect(usagePageStyles).toMatch(/\.toolbarContextSlot\s*\{[\s\S]*?display:\s*grid;/)
    expect(usagePageStyles).toMatch(/\.usageFilterTransition,\s*\.rankingScopeTransition\s*\{[\s\S]*?grid-area:\s*1 \/ 1;/)
    const contextTransition = styleRuleBlock(usagePageStyles, '.usageFilterTransition,\n.rankingScopeTransition')
    expect(contextTransition).toContain('max-width: 0;')
    expect(contextTransition).toContain('transform: translateX(8px);')
    expect(contextTransition).toContain('max-width 340ms cubic-bezier(0.22, 1, 0.36, 1)')
    expect(contextTransition).toContain('opacity 260ms ease')
    expect(usagePageStyles).toMatch(/\.usageFilterTransitionOpen\s*\{[\s\S]*?max-width:\s*960px;/)
    expect(usagePageStyles).toMatch(/\.usageFilterTransitionOpen\s*\{[\s\S]*?transform:\s*translateX\(0\);/)
    const contextTransitionInner = styleRuleBlock(usagePageStyles, '.usageFilterTransitionInner,\n.rankingScopeTransitionInner')
    expect(contextTransitionInner).toContain('overflow: hidden;')
    expect(contextTransitionInner).toContain('width: max-content;')
    expect(usagePageStyles).toMatch(/\.usageRefreshSlot\s*\{[\s\S]*?flex:\s*0 0 auto;/)
    expect(usagePageStyles).toMatch(/\.rankingScopeTransitionOpen\s*\{[\s\S]*?max-width:\s*260px;/)
    expect(usagePageStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.usageFilterTransition,\s*\.usageFilterTransitionInner,[\s\S]*?\.rankingScopeTransitionInner\s*\{[\s\S]*?width:\s*100%;/)
    expect(usagePageStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.usageFilterTransitionOpen\s*\{[\s\S]*?max-width:\s*100%;/)
  })

  it('collapses the mobile filter height with the historical transition timing', () => {
    const reducedMotionStart = usagePageStyles.indexOf('@media (prefers-reduced-motion: reduce)')
    const mobileStart = usagePageStyles.lastIndexOf('@include mobile {', reducedMotionStart)
    const mobileStyles = usagePageStyles.slice(mobileStart, reducedMotionStart)
    const transitionBlock = mobileStyles.match(/\.toolbarActionsRightAnimated \.usageFilterTransition,\s*\.toolbarActionsRightAnimated \.rankingScopeTransition\s*\{([^}]*)\}/)?.[1] ?? ''
    const openBlock = mobileStyles.match(/\.toolbarActionsRightAnimated \.usageFilterTransitionOpen\s*\{([^}]*)\}/)?.[1] ?? ''

    expect(transitionBlock).toContain('max-height: 0;')
    expect(transitionBlock).toContain('max-height 340ms cubic-bezier(0.22, 1, 0.36, 1)')
    expect(openBlock).toContain('max-height: 280px;')
  })

  it('keeps CPAMC range controls on the immediate toolbar layout path', () => {
    expect(usagePageSource).toContain('isEmbeddedInCPAMC ? styles.usageFilterTransitionImmediate')
    expect(usagePageStyles).toMatch(/\.usageFilterTransitionImmediate\s*\{[\s\S]*?display:\s*contents;/)
    expect(usagePageStyles).toMatch(/\.usageFilterTransitionImmediate\s+\.usageFilterTransitionInner\s*\{[\s\S]*?display:\s*contents;/)
  })

  it('gets Request Events and Settings elevation from the global card surface', () => {
    expect(styleRuleBlock(componentsStyles, '.keeper-card-surface')).toContain('box-shadow: var(--shadow-lg);')
    expect(styleRuleBlock(usagePageStyles, '.requestEventsCard:global(.card)')).not.toContain('box-shadow:')
    expect(usagePageStyles).not.toContain('.settingsSections > :global(.card)')
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
      usagePageSource.indexOf('const refreshActiveTab = useCallback'),
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

  it('loads core and latency Analysis sections through independent endpoints', () => {
    expect(usagePageSource).toContain('fetchAnalysis')
    expect(usagePageSource).toContain('fetchAnalysisLatency')
    expect(usagePageSource).toContain('<AnalysisPanel')
    expect(usagePageSource).toContain('latencyDiagnostics={analysisLatencyData}')
    expect(usagePageSource).toContain('latencyLoading={analysisLatencyLoading}')
    expect(usagePageSource).toContain('latencyError={analysisLatencyError}')
    expect(usagePageSource).not.toContain('fetchUsageAnalysis')
    expect(usagePageSource).not.toContain('<ApiDetailsCard')
    expect(usagePageSource).not.toContain('<ModelStatsCard')
    expect(apiIndexSource).not.toContain('ApiDetailsCard')
    expect(apiIndexSource).not.toContain('ModelStatsCard')
    expect(apiClientSource).toContain("apiPath('/usage/analysis')")
    expect(apiClientSource).toContain("apiPath('/usage/analysis/latency')")
    expect(typesSource).not.toContain('latency_diagnostics: AnalysisLatencyDiagnostics')
  })

  it('keeps Analysis before Ranking and Request Events', () => {
    expect(i18nSource).toContain("tab_analysis: 'Analysis'")
    expect(i18nSource).not.toContain("tab_analysis: 'API & Models'")
    expect(i18nSource).not.toContain("tab_analysis: 'API 与模型'")
    expect(i18nSource).not.toContain("tab_analysis: 'API 與模型'")
    expect(usagePageSource).toContain("const USAGE_TAB_OPTIONS = ['overview', 'analysis', 'ranking', 'events', 'auth-files', 'ai-provider', 'settings'] as const")
  })

  it('keeps Sign out as the rightmost shared main action after Check Updates', () => {
    expect(usagePageSource).toContain('logout')
    expect(usagePageSource).toContain('fetchUpdateCheck')
    expect(usagePageSource.indexOf("t('usage_stats.check_updates')")).toBeLessThan(usagePageSource.indexOf("t('common.logout')"))
    expect(usagePageSource.match(/<MainActionButton/g)).toHaveLength(2)
    expect(keyOverviewPageSource.match(/<MainActionButton/g)).toHaveLength(2)
    expect(usagePageSource).toContain("aria-label={t('common.logout')}")
    expect(keyOverviewPageSource).toContain("aria-label={t('common.logout')}")
    expect(usagePageSource).not.toContain('styles.signOutPill')
    expect(keyOverviewPageSource).not.toContain('styles.logoutPill')
    expect(usagePageStyles).not.toContain('.signOutPill')
    expect(keyOverviewPageStyles).not.toContain('.logoutPill')
  })

  it('uses only a theme-tuned top highlight for the connected shell outline', () => {
    const connectedTabBar = styleRuleBlock(usagePageStyles, '.tabBarConnected')
    const darkConnectedTabBar = styleRuleBlock(usagePageStyles, ":global([data-theme='dark']) .tabBarConnected")

    expect(connectedTabBar).toContain('display: inline-flex;')
    expect(connectedTabBar).toContain('align-items: stretch;')
    expect(connectedTabBar).toContain('width: max-content;')
    expect(connectedTabBar).toContain('max-width: 100%;')
    expect(connectedTabBar).toContain('min-height: 40px;')
    expect(connectedTabBar).toContain('padding: 4px;')
    expect(connectedTabBar).toContain('gap: 0;')
    expect(connectedTabBar).toContain('border: 1px solid transparent;')
    expect(connectedTabBar).toContain('border-radius: 999px;')
    expect(connectedTabBar).toContain('background: color-mix(in srgb, var(--bg-secondary) 78%, transparent);')
    expect(connectedTabBar).toContain('box-shadow: inset 0 1px 0 color-mix(in srgb, var(--text-primary) 12%, transparent);')
    expect(connectedTabBar).toContain('overflow-x: auto;')
    expect(darkConnectedTabBar).toContain('border-color: transparent;')
    expect(darkConnectedTabBar).toContain('box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.06);')
  })

  it('removes per-tab frames while keeping connected tab labels stable at every width', () => {
    const connectedTabPill = styleRuleBlock(usagePageStyles, '.tabBarConnected .tabPill')

    expect(connectedTabPill).toContain('min-height: 32px;')
    expect(connectedTabPill).toContain('padding: 7px 12px;')
    expect(connectedTabPill).toContain('border: 0;')
    expect(connectedTabPill).toContain('border-radius: 999px;')
    expect(connectedTabPill).toContain('background: transparent;')
    expect(connectedTabPill).toContain('font-size: 12px;')
    expect(connectedTabPill).toContain('font-weight: 700;')
    expect(connectedTabPill).toContain('white-space: nowrap;')
    expect(connectedTabPill).not.toContain('transform:')
  })

  it('widens simplified and traditional Chinese tabs without separating the connected segments', () => {
    const connectedTabBar = styleRuleBlock(usagePageStyles, '.tabBarConnected')
    const chineseTabPill = styleRuleBlock(usagePageStyles, '.tabBarConnected:lang(zh) .tabPill')

    expect(usagePageSource).toContain('const { t, i18n } = useTranslation();')
    expect(usagePageSource).toContain('lang={i18n.resolvedLanguage || i18n.language}')
    expect(connectedTabBar).toContain('gap: 0;')
    expect(chineseTabPill).toContain('padding-inline: 16px;')
  })

  it('highlights only the connected active tab with the themed surface and soft shadow', () => {
    const connectedActiveTab = styleRuleBlock(usagePageStyles, '.tabBarConnected .tabPillActive')

    expect(connectedActiveTab).toContain('color: var(--text-primary);')
    expect(connectedActiveTab).toContain('background: var(--bg-primary);')
    expect(connectedActiveTab).toContain('box-shadow: 0 6px 16px color-mix(in srgb, var(--text-primary) 10%, transparent);')
    expect(connectedActiveTab).not.toContain('border-color:')
  })

  it('keeps the connected shell out of CPAMC embed and Key Overview', () => {
    expect(usagePageSource).toContain("${!isEmbeddedInCPAMC ? styles.tabBarConnected : ''}")
    expect(keyOverviewPageSource).not.toContain('tabBarConnected')
    expect(keyOverviewPageStyles).not.toContain('.tabBarConnected')
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
    expect(clientBlock).toContain('border: 1px solid var(--border-color);')
    expect(clientBlock).toContain('background: var(--bg-tertiary);')
    expect(clientBlock).not.toMatch(/text-overflow:\s*ellipsis;/)
    expect(clientBlock).not.toMatch(/white-space:\s*nowrap;/)
    expect(sessionSettingsMobileBlock).toMatch(/\.sessionSettingsItem\s*\{[\s\S]*?grid-template-areas:\s*'summary'\s*'client'\s*'details'\s*'actions';/)
    expect(usagePageStyles).toMatch(/\.sessionSettingsLogoutButton\s*\{[\s\S]*?min-width:\s*92px;/)
  })

  it('keeps both logout confirmation button pairs aligned with Usage dialog actions', () => {
    const sessionLogoutModalStart = sessionSettingsSource.indexOf('<Modal\n          open={Boolean(confirmingSession)}')
    const pageLogoutModalStart = usagePageSource.indexOf('<Modal\n        open={logoutConfirmOpen}')
    const sessionLogoutModal = sessionSettingsSource.slice(
      sessionLogoutModalStart,
      sessionSettingsSource.indexOf('</Modal>', sessionLogoutModalStart),
    )
    const pageLogoutModal = usagePageSource.slice(
      pageLogoutModalStart,
      usagePageSource.indexOf('</Modal>', pageLogoutModalStart),
    )

    for (const modalSource of [sessionLogoutModal, pageLogoutModal]) {
      expect(modalSource.match(/appearance="action"/g)).toHaveLength(2)
      expect(modalSource).not.toContain('styles.usagePillAction')
    }
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

    expect(usagePageStyles).not.toContain('.settingsCompactAction')
    expect(apiKeyButtonsBlock).not.toContain('min-height: 40px;')
    expect(sessionButtonBlock).not.toContain('min-height: 40px;')
    expect(apiKeySettingsSource.match(/appearance="action"/g)).toHaveLength(2)
    expect(sessionSettingsSource.match(/appearance="action"/g)).toHaveLength(3)
  })

  it('contains wheel scrolling at overflowing card boundaries without trapping short lists', () => {
    expect(requestEventsSource).toContain('useScrollBoundaryContainment(requestEventsTableWrapperRef, rows.length > 0);')
    expect(requestEventsSource).toContain('useScrollBoundaryContainment(scrollerRef);')
    expect(apiKeySettingsSource).toContain('useScrollBoundaryContainment(apiKeySettingsBodyRef);')
    expect(sessionSettingsSource).toContain('useScrollBoundaryContainment(sessionSettingsBodyRef);')
    expect(priceSettingsSource).toContain('useScrollBoundaryContainment(pricesGridRef, sortedModelPrices.length > 0);')
    expect(requestEventsSource).toContain('ref={requestEventsTableWrapperRef} className={styles.requestEventsTableWrapper}')
    expect(requestEventsSource).toContain('className={styles.requestEventsLogSectionPanelInner} ref={scrollerRef}')
    expect(apiKeySettingsSource).toContain('ref={apiKeySettingsBodyRef} className={styles.apiKeySettingsBody}')
    expect(sessionSettingsSource).toContain('ref={sessionSettingsBodyRef} className={styles.sessionSettingsBody}')
    expect(priceSettingsSource).toContain('ref={pricesGridRef} className={styles.pricesGrid}')
    expect(usagePageStyles).toMatch(/\.requestEventsTableWrapper\[data-scroll-boundary-contained='true'\],[\s\S]*?\.requestEventsLogSectionPanelInner\[data-scroll-boundary-contained='true'\],[\s\S]*?\.apiKeySettingsBody\[data-scroll-boundary-contained='true'\],[\s\S]*?\.sessionSettingsBody\[data-scroll-boundary-contained='true'\],[\s\S]*?\.pricesGrid\[data-scroll-boundary-contained='true'\]\s*\{[\s\S]*?overscroll-behavior-y:\s*contain;/)
    expect(credentialStyles).not.toContain('data-scroll-boundary-contained')
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
    expect(priceSettingsSource).toContain('className={`${styles.formField} ${styles.priceFormModelField}`}')
    expect(priceSettingsSource).toContain('className={styles.priceFormAction}')
    expect(priceSettingsSource).toContain('appearance="action"')
    expect(styleRuleBlock(usagePageStyles, '.priceFormAction:global(.btn.btn-action)')).toMatch(/min-height:\s*40px;/)
    expect(usagePageStyles).toMatch(/\.priceForm\s*\{[\s\S]*?container-name:\s*model-pricing-form;/)
    expect(usagePageStyles).toMatch(/\.priceForm\s*\{[\s\S]*?container-type:\s*inline-size;/)
    expect(usagePageStyles).toMatch(/\.formRow\s*\{[\s\S]*?display:\s*grid;/)
    expect(usagePageStyles).toMatch(/\.formRow\s*\{[\s\S]*?grid-template-columns:\s*minmax\(180px, 1\.4fr\) minmax\(130px, 0\.85fr\) repeat\(5, minmax\(120px, 1fr\)\) auto;/)
    expect(usagePageStyles).toMatch(/@container model-pricing-form \(max-width:\s*1120px\)\s*\{[\s\S]*?grid-template-columns:\s*repeat\(4, minmax\(0, 1fr\)\);/)
    expect(usagePageStyles).toMatch(/@container model-pricing-form \(max-width:\s*720px\)\s*\{[\s\S]*?grid-template-columns:\s*repeat\(2, minmax\(0, 1fr\)\);[\s\S]*?\.priceFormModelField,[\s\S]*?\.priceFormAction\s*\{[\s\S]*?grid-column:\s*1 \/ -1;/)
    expect(usagePageStyles).toMatch(/@container model-pricing-form \(max-width:\s*480px\)\s*\{[\s\S]*?grid-template-columns:\s*minmax\(0, 1fr\);/)
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
    expect(heatmapRowLabelBlock).toContain('height: 34px;')
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

  it('preserves the API Key sizing while removing the legacy range select and Custom UI', () => {
    const apiKeySelectStart = usagePageSource.indexOf('<Select\n                        value={selectedApiKeyId}')
    const apiKeySelectBlock = usagePageSource.slice(apiKeySelectStart, usagePageSource.indexOf('/>', apiKeySelectStart))

    expect(apiKeySelectBlock).toContain('fullWidth={false}')
    expect(usagePageStyles).toMatch(/\.toolbarActionsRight\s*\{[\s\S]*?align-items:\s*center;/)
    expect(usagePageStyles).toMatch(/\.usageFilterBar\s*\{[\s\S]*?align-items:\s*center;/)
    expect(usagePageStyles).toMatch(/\.usageFilterBar\s*\{[\s\S]*?flex:\s*1 1 auto;/)
    expect(usagePageStyles).toMatch(/\.apiKeySelectControl\s*\{[\s\S]*?width:\s*172px;/)
    expect(usagePageStyles).toMatch(/\.apiKeySelectControl\s*\{[\s\S]*?flex:\s*0 0 172px;/)
    expect(usagePageSource).not.toContain('TIME_RANGE_OPTIONS')
    expect(keyOverviewPageSource).not.toContain('TIME_RANGE_OPTIONS')
    expect(usagePageSource).not.toContain('customTimeRange')
    expect(usagePageStyles).not.toContain('.rangeSelectControl')
    expect(usagePageStyles).not.toContain('.customRange')
    expect(keyOverviewPageStyles).not.toContain('.rangeSelectControl')
  })

  it('passes realtime error state and current data guard to the realtime panel', () => {
    expect(usagePageSource).toContain('error: realtimeError')
    expect(usagePageSource).toContain('const displayRealtimeError = realtimeError')
    expect(usagePageSource).toContain('realtime={currentRealtime ?? undefined}')
    expect(usagePageSource).toContain('error={displayRealtimeError}')
  })

  it('loads both Activity cards through one independent Recent Activity request', () => {
    expect(usagePageSource).toContain('useUsageActivityData({')
    expect(usagePageSource).toContain('useRecentActivityWindow(usageRangeQuery)')
    expect(usagePageSource).toContain('await Promise.all([loadUsage(), loadActivity(), loadRealtime()])')
    expect(usagePageSource).toContain('await Promise.all([loadUsage(), loadActivity({ skipIfInFlight: true }), loadRealtime()])')
    expect(usagePageSource).not.toContain('<ServiceHealthCard')
    expect(usagePageSource).not.toContain('showEyebrow')
  })

  it('aligns Request Event Log pagination with credential pagination height', () => {
    expect(requestEventsSource).toContain('variant="flush"')
    expect(styleRuleBlock(componentsStyles, '.card-flush')).toContain('padding: 0;')
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
    const cardBlock = styleRuleBlock(usagePageStyles, '.requestEventsCard:global(.card)')
    const flushBlock = styleRuleBlock(componentsStyles, '.card-flush')
    const tableWrapperBlock = usagePageStyles.slice(
      usagePageStyles.indexOf('.requestEventsTableWrapper {'),
      usagePageStyles.indexOf('.requestEventsNoWrapCell')
    )

    expect(flushBlock).toMatch(/padding:\s*0;/)
    expect(cardBlock).toMatch(/overflow:\s*hidden;/)
    expect(componentsStyles).toMatch(/\.card-flush\s*>\s*\.card-header\s*\{[\s\S]*?margin-bottom:\s*0;[\s\S]*?border-bottom:\s*1px solid var\(--border-color\);/)
    expect(componentsStyles).toMatch(/@media \(max-width:\s*\$breakpoint-mobile\)[\s\S]*?\.card-flush\s*>\s*\.card-header\s*\{[\s\S]*?padding:\s*18px;/)
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

  it('keeps the Speed Mode tooltip target on the normal arrow cursor', () => {
    const speedModeCellBlock = styleRuleBlock(usagePageStyles, '.requestEventsSpeedModeCell')
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

    const clientMetadataRenderer = requestEventsSource.slice(
      requestEventsSource.indexOf('const renderClientMetadataCell'),
      requestEventsSource.indexOf('\n  const modelOptions'),
    )
    expect(clientMetadataRenderer).toMatch(/<td[\s\S]*styles\.requestEventsNoWrapCell/)
    ;['client_ip', 'x_forwarded_for', 'user_agent'].forEach((columnId) => {
      const block = requestEventColumnDefinitionBlock(columnId)
      expect(block).toMatch(/header:\s*<th[^>]*styles\.requestEventsNoWrapCell/)
      expect(block).toContain('renderClientMetadataCell(')
    })

    ;['api_key', 'source', 'model'].forEach((columnId) => {
      expect(requestEventColumnDefinitionBlock(columnId)).not.toContain('styles.requestEventsNoWrapCell')
    })
  })

  it('provides reusable pill controls and global command actions for usage subpages', () => {
    const actionButton = styleRuleBlock(componentsStyles, '.btn-action')

    expect(usagePageStyles).toMatch(/\.usagePillControl\s*\{[\s\S]*?border-radius:\s*999px;/)
    expect(actionButton).toMatch(/border-radius:\s*999px;/)
    expect(actionButton).not.toMatch(/(?:^|\n)\s*(?:background(?:-color)?|border-color|color):/)
    expect(componentsStyles).toMatch(/&\.btn-danger\s*\{[\s\S]*?background-color:\s*var\(--danger-color\);[\s\S]*?color:\s*#fff;/)
    expect(usagePageStyles).not.toContain('.usagePillAction')
    expect(usagePageStyles).not.toContain('.usagePillActionDanger')
    expect(usagePageStyles).toMatch(/:global\(\.input\)\s*\{[^}]*border-radius:\s*999px;/)
    expect(requestEventsSource).toContain('styles.usagePillControl')
    expect(requestEventsSource).toContain('appearance="action"')
    expect(priceSettingsSource).toContain('styles.usagePillControl')
    expect(priceSettingsSource).not.toContain('styles.usagePillAction')
  })

  it('keeps the Request Event export menu styled and hoverable like the credential inspection control', () => {
    const exportMenuBlock = styleRuleBlock(usagePageStyles, '.requestEventsExportMenu')
    const exportDropdownBlock = usagePageStyles.slice(
      usagePageStyles.indexOf('.requestEventsExportDropdown {'),
      usagePageStyles.indexOf('.requestEventsToolbar {')
    )
    const clearFilterSlotBlock = styleRuleBlock(usagePageStyles, '.requestEventsFilterActionSlot')

    expect(requestEventsSource.match(/<MainActionButton/g)).toHaveLength(2)
    expect(requestEventsSource).not.toContain('styles.requestEventsExportButton')
    expect(requestEventsSource).not.toContain('styles.requestEventsExportButtonInner')
    expect(requestEventsSource).toContain('<IconDownload size={12} aria-hidden="true" />')
    expect(requestEventsSource).toContain('styles.requestEventsFilterActionSlot')
    expect(exportMenuBlock).toMatch(/position:\s*relative;/)
    expect(exportMenuBlock).toMatch(/display:\s*inline-flex;/)
    expect(exportMenuBlock).toMatch(/align-items:\s*center;/)
    expect(usagePageStyles).not.toContain('.requestEventsExportButton:global(.btn)')
    expect(componentsStyles).toMatch(/\.main-action-button-shell\s*\{[\s\S]*?min-height:\s*42px;/)
    expect(componentsStyles).toMatch(/\.btn\.btn-action\.main-action-button\s*\{[\s\S]*?min-height:\s*32px;/)
    expect(exportDropdownBlock).toMatch(/top:\s*calc\(100% \+ 6px\);/)
    expect(clearFilterSlotBlock).toMatch(/display:\s*flex;/)
    expect(clearFilterSlotBlock).toMatch(/align-items:\s*center;/)
    expect(clearFilterSlotBlock).toMatch(/align-self:\s*flex-end;/)
    expect(clearFilterSlotBlock).toMatch(/min-height:\s*40px;/)
    expect(requestEventsSource).not.toContain('styles.requestEventsClearFiltersButton')
  })

  it('matches Request Event header action spacing to Auth Files actions', () => {
    const requestEventActionsBlock = styleRuleBlock(usagePageStyles, '.requestEventsActions')
    const credentialActionsBlock = styleRuleBlock(credentialStyles, '.credentialSectionActionButtons')

    expect(credentialActionsBlock).toContain('gap: 10px;')
    expect(requestEventActionsBlock).toContain('gap: 10px;')
  })

  it('matches the Request Event column visibility switch to Auth Files Enabled only', () => {
    const visibilityStart = usagePageStyles.indexOf('.requestEventsColumnVisibilityControl {')
    const visibilitySwitchBlock = usagePageStyles.slice(
      visibilityStart,
      usagePageStyles.indexOf('@media (prefers-reduced-motion: reduce)', visibilityStart)
    )

    expect(visibilitySwitchBlock).toMatch(/\.requestEventsColumnVisibilityTrack\s*\{[\s\S]*?width:\s*42px;[\s\S]*?height:\s*24px;/)
    expect(visibilitySwitchBlock).toContain('background: linear-gradient(135deg, #2563eb 0%, #38bdf8 58%, #67e8f9 100%);')
    expect(visibilitySwitchBlock).toMatch(/\.requestEventsColumnVisibilityThumb\s*\{[\s\S]*?width:\s*20px;[\s\S]*?height:\s*20px;/)
    expect(visibilitySwitchBlock).toContain('background: linear-gradient(145deg, #fff, color-mix(in srgb, var(--bg-primary) 86%, #dbeafe));')
    expect(visibilitySwitchBlock).toContain('transform: translateX(18px);')
    expect(requestEventsColumnSettingsSource).toContain('styles.requestEventsColumnVisibilityTrack')
    expect(requestEventsColumnSettingsSource).toContain('styles.requestEventsColumnVisibilityThumb')
    expect(requestEventsColumnSettingsSource.match(/appearance="action"/g)).toHaveLength(2)
    expect(usagePageStyles).not.toContain('.requestEventsColumnSettingsAction')
    expect(credentialStyles).toContain('background: linear-gradient(135deg, #2563eb 0%, #38bdf8 58%, #67e8f9 100%);')
  })

  it('disables Request Event column switch transitions for reduced motion', () => {
    expect(usagePageStyles).toMatch(
      /@media \(prefers-reduced-motion: reduce\)\s*\{[\s\S]*?\.requestEventsColumnVisibilityTrack,[\s\S]*?\.requestEventsColumnVisibilityThumb\s*\{[\s\S]*?transition:\s*none;/
    )
  })
})

describe('Pricing rules component boundary', () => {
  it('keeps rule form behavior and responsive styles out of PriceSettingsCard and UsagePage styles', () => {
    expect(priceSettingsSource).toContain('<PriceRulesModal')
    expect(priceSettingsSource).not.toContain('data-rule-field="key"')
    expect(priceRulesSource).toContain('data-rule-field="key"')
    expect(priceRulesSource).toContain('className={styles.modal}')
    expect(priceRulesStyles).toMatch(/\.ruleRow\s*\{[\s\S]*?grid-template-columns:/)
    expect(priceRulesStyles).toMatch(/\.modal\s+:global\(\.modal-header\)\s*\{[\s\S]*?padding-right:/)
	expect(priceRulesHelpSource).toContain('createPortal')
	expect(styleRuleBlock(priceRulesStyles, '.help')).toMatch(/display:\s*inline-flex;/)
	expect(styleRuleBlock(priceRulesStyles, '.helpTooltip')).toMatch(/box-sizing:\s*border-box;/)
	expect(styleRuleBlock(priceRulesStyles, '.helpTooltip')).toMatch(/position:\s*fixed;/)
	expect(styleRuleBlock(priceRulesStyles, '.helpTooltip')).toMatch(/overflow-y:\s*auto;/)
	expect(priceRulesHelpSource).toContain('maxHeight')
	expect(priceRulesHelpSource).toContain("placement === 'above'")
    expect(priceRulesStyles).toMatch(/@media \(max-width:/)
    expect(usagePageStyles).not.toMatch(/\.pricingRules/)
  })

  it('matches the compact model-pricing control sizes and aligns each rule row', () => {
    expect(priceRulesSource.match(/className=\{styles\.ruleInput\}/g)).toHaveLength(3)
    expect(styleRuleBlock(priceRulesStyles, '.ruleInput')).toMatch(/height:\s*40px;/)
    expect(styleRuleBlock(priceRulesStyles, '.ruleInput')).toMatch(/border-radius:\s*999px;/)
    expect(priceRulesStyles).toMatch(/\.ruleRow\s+:global\(\.form-group > label\)\s*\{[\s\S]*?font-size:\s*10px;/)
    expect(styleRuleBlock(priceRulesStyles, '.removeButton')).not.toMatch(/min-height:/)
    expect(styleRuleBlock(priceRulesStyles, '.removeButton')).toMatch(/margin-top:\s*20px;/)
    expect(priceRulesStyles).not.toContain('.actionButton')
    expect(priceRulesSource.match(/appearance="action"/g)).toHaveLength(4)
    expect(priceRulesSource).not.toContain('usageStyles')
  })
})
