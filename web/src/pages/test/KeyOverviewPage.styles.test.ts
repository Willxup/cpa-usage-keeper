import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const source = readFileSync(new URL('../KeyOverviewPage.tsx', import.meta.url), 'utf8')
const styles = readFileSync(new URL('../../features/key-viewer/KeyViewerShell.module.scss', import.meta.url), 'utf8')
const shellSource = readFileSync(new URL('../../features/key-viewer/KeyViewerShell.tsx', import.meta.url), 'utf8')

describe('KeyOverviewPage layout', () => {
  it('keeps viewer data and time storage separate while sharing dashboard controls', () => {
    expect(source).not.toContain('UsagePage.module.scss')
    expect(source).toContain('<KeyViewerShell')
    expect(shellSource).toContain('<DashboardHeader identity={identityLabel}')
    expect(shellSource).toContain('<DashboardToolbar')
    expect(source).toContain('<TimeRangeControl')
    expect(source).toContain('loadKeyViewerTimeRange')
    expect(source).toContain('onRefresh={() => void handleManualRefresh()}')
    expect(source).not.toContain('check_updates')
  })

  it('does not reload overview data just because language changes', () => {
    expect(source).not.toContain('}, [onAuthRequired, recoverRangeBoundsConflict, t, usageRangeQuery, usageRangeQueryKey]);')
    expect(source).not.toContain('}, [onAuthRequired, realtimeWindow, t]);')
    expect(source).toContain('}, [onAuthRequired, recoverRangeBoundsConflict, usageRangeQuery, usageRangeQueryKey]);')
    expect(source).toContain('}, [onAuthRequired, realtimeWindow]);')
  })

  it('loads overview, Activity, and realtime data through separate requests', () => {
    expect(source).toContain('fetchKeyOverviewRealtime')
    expect(source).toContain('overviewRequestControllerRef')
    expect(source).toContain('realtimeRequestControllerRef')
    expect(source).toContain('const overview = await fetchKeyOverview(')
    expect(source).toContain('const nextRealtime = await fetchKeyOverviewRealtime({')
    expect(source).toContain('useUsageActivityData({')
    expect(source).toContain('useRecentActivityWindow(usageRangeQuery)')
    expect(source).toContain('await Promise.all([loadOverview(options), loadActivity(options)])')
  })

  it('auto-refreshes the active viewer page', () => {
    expect(source).toContain('KEY_OVERVIEW_AUTO_REFRESH_INTERVAL_MS')
    expect(source).toContain('scheduleKeyOverviewAutoRefresh')
    expect(source).toContain('refreshKeyOverview')
    expect(source).toContain('refreshOverview: () => refreshKeyOverview({ skipIfInFlight: true })')
    expect(source).toContain('onRefreshError: handleAutoRefreshError')
    expect(source).toContain('intervalMs: KEY_OVERVIEW_AUTO_REFRESH_INTERVAL_MS')
  })

  it('disables manual refresh only while its own request is in flight', () => {
    expect(source).toContain('const refreshDisabled = manualRefreshLoading')
    expect(source).not.toContain('manualRefreshLoading || loading || realtimeLoading')
  })

  it('keeps existing realtime data visible during background refreshes', () => {
    expect(source).not.toContain('setRealtime(null)')
    expect(source).toContain('realtime?.window === realtimeWindow ? realtime : undefined')
  })

  it('renders both Activity cards through Recent Activity before realtime metrics', () => {
    expect(source).toContain('<RecentActivityPanel')
    expect(source.indexOf('<RecentActivityPanel')).toBeLessThan(source.indexOf('<OverviewRealtimePanel'))
    expect(source).not.toContain('<ServiceHealthCard')
    expect(source).toContain('<OverviewRealtimePanel')
    expect(source).toContain('KEY_OVERVIEW_REALTIME_VISIBLE_DIMENSIONS')
    expect(source).toContain("visibleDimensions={KEY_OVERVIEW_REALTIME_VISIBLE_DIMENSIONS}")
    expect(source).not.toContain('showEyebrow')
  })

  it('keeps viewer shell styles focused on page layout and loading', () => {
    expect(styles).toContain('.loadingOverlay')
    expect(styles).toContain('.pageFrame')
    expect(styles).not.toContain('.tabBarConnected')
    expect(styles).not.toContain('.themeSwitcher')
    expect(styles).not.toContain('.rangeSelectControl')
  })
})
