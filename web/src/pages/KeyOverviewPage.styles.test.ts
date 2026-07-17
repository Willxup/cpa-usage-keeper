import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const source = readFileSync(new URL('./KeyOverviewPage.tsx', import.meta.url), 'utf8')
const styles = readFileSync(new URL('./KeyOverviewPage.module.scss', import.meta.url), 'utf8')

describe('KeyOverviewPage layout', () => {
  it('keeps the viewer page on independent styles while matching the admin overview toolbar structure', () => {
    expect(source).not.toContain('UsagePage.module.scss')
    expect(source).toContain('className={styles.themeSwitcher}')
    expect(source).toContain('className={styles.logoutSwitcher}')
    expect(source).not.toContain('check_updates')
    expect(source.indexOf('className={styles.tabBar}')).toBeLessThan(source.indexOf('className={styles.toolbarActionsRight}'))
    expect(source).toContain('className={styles.timeRangeGroup}')
    expect(source).toContain('className={styles.usageRefreshSlot}')
    expect(source).not.toContain('className={styles.toolbarMetaRow}')
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

  it('removes the Request Health Timeline label instead of toggling it off', () => {
    expect(source).toContain('<ServiceHealthCard usage={usage} loading={overviewDisplayLoading} />')
    expect(source).toContain('<OverviewRealtimePanel')
    expect(source).toContain('KEY_OVERVIEW_REALTIME_VISIBLE_DIMENSIONS')
    expect(source).toContain("visibleDimensions={KEY_OVERVIEW_REALTIME_VISIBLE_DIMENSIONS}")
    expect(source).not.toContain('showEyebrow')
  })

  it('copies the relevant admin toolbar class contracts into its own module', () => {
    expect(styles).toMatch(/\.toolbarRow\s*\{[\s\S]*?flex-direction:\s*column;/)
    expect(styles).toMatch(/\.toolbarActionsRight\s*\{[\s\S]*?justify-content:\s*flex-end;/)
    expect(styles).toMatch(/\.timeRangeGroup\s*\{[\s\S]*?border-radius:\s*9999px;/)
    expect(styles).toMatch(/\.rangeSelectControl\s*\{[\s\S]*?width:\s*164px;/)
    expect(styles).not.toMatch(/\.(toolbarMetaRow|lastRefreshed)\s*\{/)
  })

  it('uses the same soft active tab shadow as the admin usage tabs', () => {
    const activeTabBlock = styles.slice(
      styles.indexOf('.tabPillActive {'),
      styles.indexOf('.toolbarActionsRight')
    )

    expect(activeTabBlock).toMatch(/border-color:\s*rgba\(\$primary-color, 0\.45\);/)
    expect(activeTabBlock).toContain('0 0 0 1px rgba($primary-color, 0.08) inset,')
    expect(activeTabBlock).toContain('0 4px 12px rgba($primary-color, 0.14);')
    expect(activeTabBlock).not.toContain('box-shadow: 0 8px 20px rgba(0, 0, 0, 0.1);')
  })
})
