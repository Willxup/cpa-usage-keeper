import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const source = readFileSync(new URL('./KeyOverviewPage.tsx', import.meta.url), 'utf8')
const styles = readFileSync(new URL('./KeyOverviewPage.module.scss', import.meta.url), 'utf8')
const shellStyles = readFileSync(new URL('../components/layout/AppShell.module.scss', import.meta.url), 'utf8')

describe('KeyOverviewPage layout', () => {
  it('delegates the restricted viewer chrome to the shared AppShell', () => {
    expect(source).not.toContain('UsagePage.module.scss')
    expect(source).toContain('<AppShell')
    expect(source).toContain("variant={embedded ? 'embed' : 'viewer'}")
    expect(source).toContain('sticky="desktop"')
    expect(source).toContain("nav={{ mode: 'none' }}")
    expect(source).not.toContain('<Layout')
    expect(source).not.toContain('<Sider')
    expect(source).not.toContain('<PageHeader')
    expect(source).not.toContain('<PageContent')
    expect(source).not.toContain('<PageTitle')
    expect(source).not.toContain('<Menu')
    expect(source).not.toContain('<Drawer')
    expect(source).not.toContain('check_updates')
    expect(source).toContain('<PreferencesDropdown />')
    expect(source).toContain('<LogoutOutlined />')
    expect(source).toContain('<ReloadOutlined />')
    expect(source).not.toContain('<Segmented')
  })

  it('declares viewer header slots with the API Key identity and utility controls', () => {
    expect(source).toContain("headerTitle: t('usage_stats.tab_overview')")
    expect(source).toContain("headerSubtitle: 'API Key Viewer'")
    expect(source).toContain('headerUtility: (')
    expect(source).toContain('className={styles.identityTag}')
    expect(source).toContain('brand: <BrandLink className={styles.brandLink} />')
  })

  it('renders exactly one page heading through the shared shell header', () => {
    expect(source.match(/<PageTitle/g)).toBeNull()
    expect(source).not.toContain('role="tablist"')
    expect(source).not.toContain('styles.tabPill')
    expect(styles).not.toContain('.tabPill')
    expect(source).not.toContain('contentTitle')
    expect(source).toContain("headerTitle: t('usage_stats.tab_overview')")
  })

  it('keeps the stable overview panels in the shared content slot', () => {
    expect(source).not.toContain('<DailyAveragePanel')
    expect(source).toContain('<StatCards')
    expect(source).toContain('dailyAverageUsage={dailyAverageUsage}')
    expect(source).toContain('<ServiceHealthCard usage={usage} loading={overviewDisplayLoading} />')
    expect(source).toContain('<OverviewRealtimePanel')
    expect(source).toContain('KEY_OVERVIEW_REALTIME_VISIBLE_DIMENSIONS')
    expect(source).toContain("visibleDimensions={KEY_OVERVIEW_REALTIME_VISIBLE_DIMENSIONS}")
    expect(source).not.toContain('showEyebrow')
  })

  it('does not reload overview data just because language changes', () => {
    expect(source).not.toContain('}, [onAuthRequired, t, timeRange]);')
    expect(source).not.toContain('}, [onAuthRequired, realtimeWindow, t, timeRange]);')
    expect(source).toContain('}, [onAuthRequired, timeRange]);')
    expect(source).toContain('}, [onAuthRequired, realtimeWindow]);')
  })

  it('loads overview and realtime data through separate parallel requests', () => {
    expect(source).toContain('fetchKeyOverviewRealtime')
    expect(source).toContain('overviewRequestControllerRef')
    expect(source).toContain('realtimeRequestControllerRef')
    expect(source).toContain('const overview = await fetchKeyOverview(')
    expect(source).toContain('const nextRealtime = await fetchKeyOverviewRealtime({')
    expect(source).toContain('await Promise.all([loadOverview(options), loadRealtime(options)])')
  })

  it('auto-refreshes the viewer overview and realtime data together', () => {
    expect(source).toContain('KEY_OVERVIEW_AUTO_REFRESH_INTERVAL_MS')
    expect(source).toContain('scheduleKeyOverviewAutoRefresh')
    expect(source).toContain('refreshKeyOverview')
    expect(source).toContain('refreshOverview: () => refreshKeyOverview({ skipIfInFlight: true })')
    expect(source).toContain('onRefreshError: handleAutoRefreshError')
    expect(source).toContain('intervalMs: KEY_OVERVIEW_AUTO_REFRESH_INTERVAL_MS')
  })

  it('keeps manual refresh available while background loads are in flight', () => {
    expect(source).toContain('const refreshDisabled = manualRefreshLoading || refreshThrottled')
    expect(source).not.toContain('manualRefreshLoading || loading || realtimeLoading || refreshThrottled')
  })

  it('keeps existing realtime data visible during background refreshes', () => {
    expect(source).not.toContain('setRealtime(null)')
    expect(source).toContain('realtime?.window === realtimeWindow ? realtime : undefined')
  })

  it('keeps the viewer toolbar and wide gutter tokens while the shell owns header geometry', () => {
    expect(source).toContain('<PageToolbar')
    expect(source).toContain('className={styles.keyOverviewLeading}')
    expect(source).not.toContain("style={{ width: '100%' }}")
    expect(styles).toMatch(/\.rangeSelectControl\s*\{[\s\S]*?flex:\s*0 0 160px;[\s\S]*?width:\s*160px;[\s\S]*?min-width:\s*160px;/)
    expect(styles).toMatch(/@include mobile\s*\{[\s\S]*?\.rangeSelectControl\s*\{[\s\S]*?flex:\s*1 1 auto;[\s\S]*?min-width:\s*0;/)
    expect(styles).toMatch(/\.viewerShell\s*\{[\s\S]*?--page-gutter:\s*40px;/)
    expect(styles).toMatch(/\.viewerShell\s*\{[\s\S]*?--page-header-height:\s*88px;/)
    expect(styles).not.toMatch(/\.(toolbarMetaRow|lastRefreshed)\s*\{/)
    expect(styles).not.toMatch(/\.pageShell\s*\{/)
    expect(styles).not.toMatch(/\.headerLayout\s*\{/)
    expect(styles).not.toMatch(/\.headerDivider\s*\{/)
    expect(styles).not.toMatch(/\.viewerLabel\s*\{/)
    expect(styles).not.toMatch(/\.headerActions\s*\{/)
    expect(styles).not.toMatch(/\.header\s*\{/)
    expect(shellStyles).toMatch(/\.shellHeaderStickyAlways,[\s\S]*?\.shellHeaderStickyDesktop\s*\{[\s\S]*?position:\s*sticky;/)
    expect(shellStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.shellHeaderStickyDesktop\s*\{[\s\S]*?position:\s*static;/)
  })
})
