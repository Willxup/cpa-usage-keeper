// 列表真实组件的容器几何回归；browser-test run 执行，不启动服务。
import assert from 'node:assert/strict'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { build } from 'vite'

const webRoot = fileURLToPath(new URL('../../../../../', import.meta.url))
const entry = 'virtual:credential-list-health-layout'
const id = `\0${entry}`
const health = {
  window_seconds: 18000, bucket_seconds: 600,
  window_start: '2026-07-16T10:00:00+08:00', window_end: '2026-07-16T15:00:00+08:00',
  total_success: 9, total_failure: 1, success_rate: 90, input_tokens: 100, cache_read_tokens: 40,
  buckets: [{ start_time: '2026-07-16T10:00:00+08:00', end_time: '2026-07-16T10:10:00+08:00', success: 9, failure: 1, rate: 0.9 }],
}
const row = {
  identity: { id: 'credential-1', identity: 'auth-1', type: 'codex', provider: 'codex', is_deleted: false, disabled: false, last_used_at: '2026-07-16T14:58:00+08:00', stats_updated_at: '2026-07-16T15:00:00+08:00' },
  displayName: 'Test credential', typeLabel: 'Codex', totalRequests: 10, successCount: 9, failureCount: 1,
  successRate: 90, totalTokens: 100, cacheReadRate: 40, windowCacheReadRate: 40, credentialHealth: health,
  lastUsedText: '2026-07-16T14:58:00+08:00', statsUpdatedText: '2026-07-16T15:00:00+08:00',
  quota: [],
  displayQuotas: [
    { key: 'five_hour', label: '5h Window', percent: 35, barPercent: 65, percentKind: 'remaining', resetText: '2099-07-16T18:00:00+08:00', windowUsage: { tokens: '1.2M', cost: '$4.50' }, status: 'ok' },
    { key: 'weekly', label: '7d Window', percent: 55, barPercent: 45, percentKind: 'remaining', resetText: '2099-07-22T18:00:00+08:00', windowUsage: { tokens: '8.4M', cost: '$32.10' }, status: 'warning' },
  ],
  quotaLoading: false,
}

const antigravityRow = {
  ...row,
  identity: { ...row.identity, id: 'credential-2', identity: 'auth-2', type: 'antigravity', provider: 'antigravity' },
  displayName: 'Antigravity credential', typeLabel: 'Antigravity',
  displayQuotas: [
    { key: 'ag_pro', label: 'Gemini Pro', scope: 'quota_group', groupKey: 'antigravity-gemini-models', groupLabel: 'Gemini Models', percent: 25, barPercent: 75, percentKind: 'remaining', resetText: '2099-07-16T18:00:00+08:00', status: 'ok' },
    { key: 'ag_flash', label: 'Gemini Flash', scope: 'quota_group', groupKey: 'antigravity-gemini-models', groupLabel: 'Gemini Models', percent: 40, barPercent: 60, percentKind: 'remaining', resetText: '2099-07-16T18:00:00+08:00', status: 'ok' },
  ],
}

// 编译实际列表、健康与限额组件和样式；单独的全尺寸面板模拟详情，紧凑面板模拟卡片。
const bundle = await build({
  root: webRoot,
  configFile: path.join(webRoot, 'vite.config.js'),
  configLoader: 'runner',
  cacheDir: path.join(tmpdir(), 'credential-list-health-vite-cache'),
  logLevel: 'error',
  plugins: [{
    name: 'credential-list-health-layout-fixture',
    resolveId: (source) => source === entry ? id : null,
    load: (source) => source === id ? `
      import { createElement as h } from 'react'
      import { createRoot } from 'react-dom/client'
      import i18n from '@/i18n'
      import '@/styles/global.scss'
      import { CredentialListView } from '@/components/usage/credentials/list/CredentialListView'
      import { AiProviderListView } from '@/components/usage/credentials/list/AiProviderListView'
      import { CredentialHealthPanel } from '@/components/usage/credentials/CredentialHealthPanel'
      import { CredentialQuotaPanel } from '@/components/usage/credentials/quota/CredentialQuotaPanel'
      const row = ${JSON.stringify(row)}
      const antigravityRow = ${JSON.stringify(antigravityRow)}
      const mode = document.documentElement.dataset.mode
      const component = mode === 'credentials'
        ? h(CredentialListView, { rows: [row], contentMode: 'health', quotaUsageMode: 'current', onRefreshQuotaForAuthIndex: async () => {}, onResetQuotaForAuthIndex: async () => {} })
        : mode === 'credentials-quota'
        ? h(CredentialListView, { rows: [row, antigravityRow], contentMode: 'quota', quotaUsageMode: 'current', timeZone: 'Asia/Shanghai', onRefreshQuotaForAuthIndex: async () => {}, onResetQuotaForAuthIndex: async () => {} })
        : mode === 'provider' ? h(AiProviderListView, { rows: [row] })
        : mode === 'quota-full' || mode === 'quota-compact'
        ? h(CredentialQuotaPanel, { row, quotaUsageMode: 'current', timeZone: 'Asia/Shanghai', variant: mode === 'quota-compact' ? 'compact' : 'full' })
        : h(CredentialHealthPanel, { displayName: row.displayName, health: row.credentialHealth, variant: mode, lastUsedAt: row.lastUsedText, statsUpdatedAt: row.statsUpdatedText, windowCacheReadRate: 40, showCompactMeta: true })
      i18n.changeLanguage('en').then(() => createRoot(document.getElementById('root')).render(component))
    ` : null,
  }],
  build: { write: false, minify: false, cssMinify: false, rollupOptions: { input: entry, output: { format: 'iife', name: 'CredentialListHealthFixture' } } },
})
const script = bundle.output.find((item) => item.type === 'chunk')?.code
assert.ok(script, 'fixture bundle must include the components')

const { launchBrowser } = await import(process.env.BROWSER_TEST_RUNTIME)
for (const engine of ['chromium', 'webkit']) {
  const browser = await launchBrowser(engine)
  try {
    const page = await browser.newPage()
    const errors = []
    page.on('pageerror', (error) => errors.push(error.message))
    await page.route('http://credential-list.test/', (route) => route.fulfill({ status: 200, body: '<!doctype html>' }))
    await page.goto('http://credential-list.test/')
    for (const mode of ['credentials', 'provider']) {
      for (const { viewport, width, stacked } of [
        { viewport: 1440, width: 380, stacked: true },
        { viewport: 1440, width: 460, stacked: true },
        { viewport: 1440, width: 464, stacked: false },
        { viewport: 1440, width: 480, stacked: false },
        { viewport: 1440, width: 500, stacked: false },
        { viewport: 1440, width: 650, stacked: false },
        { viewport: 320, width: 320, stacked: true },
        { viewport: 390, width: 390, stacked: true },
      ]) {
        await page.setViewportSize({ width: viewport, height: 800 })
        await page.setContent(`<!doctype html><html data-mode="${mode}"><body><section id="root" style="width:${width}px;container:credential-section / inline-size"></section></body></html>`)
        await page.addScriptTag({ content: script })
        const panel = page.locator('[data-credential-list-row] [data-credential-health-variant="full"]')
        await panel.waitFor()
        assert.equal(await panel.locator('[role="listitem"]').count(), 30)
        const result = await panel.evaluate((el) => {
          const root = document.getElementById('root')
          const chart = el.querySelector('[class*="credentialHealthChart_"]')
          const meta = el.querySelector('[class*="credentialHealthMeta_"]')
          const r = (node) => { const b = node.getBoundingClientRect(); return { left: b.left, right: b.right, top: b.top, bottom: b.bottom } }
          return { scroll: root.scrollWidth, width: root.clientWidth, panel: r(el), chart: r(chart), meta: r(meta), gap: getComputedStyle(el).columnGap, text: el.textContent, times: meta.querySelectorAll('[class*="credentialHealthMetaTime_"]').length }
        })
        const label = `${engine} ${mode} viewport=${viewport} width=${width}`
        assert.ok(result.scroll <= result.width + 1, `${label}: section overflow ${result.scroll}/${result.width}`)
        assert.ok(result.meta.left >= result.panel.left - 0.5 && result.meta.right <= result.panel.right + 0.5, `${label}: metadata outside panel`)
        assert.equal(result.meta.top >= result.chart.bottom - 0.5, stacked, `${label}: wrong panel arrangement`)
        if (width === 464 && viewport === 1440) assert.equal(result.gap, '20px', `${label}: minimum gap at the threshold`)
        if (width === 480 && viewport === 1440) assert.equal(result.gap, '36px', `${label}: preserve two columns with a fitting gap`)
        if (width === 500 && viewport === 1440) assert.equal(result.gap, '40px', `${label}: preserve the normal gap`)
        assert.match(result.text, /90\.0%/)
        assert.match(result.text, /40\.00%/)
        assert.equal(result.times, 2, `${label}: expected both timestamps`)
        console.log(`PASS ${label}: scroll=${result.scroll}, panel=${Math.round(result.panel.right - result.panel.left)}, stacked=${stacked}`)
      }
    }
    for (const { viewport, width, expectedColumns } of [
      { viewport: 1080, width: 1038, expectedColumns: 1 },
      { viewport: 1120, width: 1078, expectedColumns: 1 },
      { viewport: 1150, width: 1108, expectedColumns: 1 },
      { viewport: 1200, width: 1158, expectedColumns: 1 },
      { viewport: 1440, width: 1243, expectedColumns: 2 },
      { viewport: 1440, width: 380, expectedColumns: 1 },
    ]) {
      await page.setViewportSize({ width: viewport, height: 800 })
      await page.setContent(`<!doctype html><html data-mode="credentials-quota"><body><section id="root" style="width:${width}px;container:credential-section / inline-size"></section></body></html>`)
      await page.addScriptTag({ content: script })
      const panels = page.locator('[data-credential-list-row] [data-credential-quota-variant="full"]')
      await panels.nth(1).waitFor()
      const result = await page.evaluate(() => {
        const root = document.getElementById('root')
        const rect = (node) => { const b = node.getBoundingClientRect(); return { left: b.left, right: b.right, top: b.top, bottom: b.bottom, width: b.width } }
        const rows = [...document.querySelectorAll('[data-credential-list-row]')].map((rowEl) => {
          const sidePanel = rowEl.querySelector('[class*="credentialSidePanel_"]')
          const panel = sidePanel.querySelector('[data-credential-quota-variant="full"]')
          const grid = panel.querySelector('[class*="credentialQuotaGroupBars_"]') ?? panel.querySelector('[class*="credentialQuotaBars_"]')
          const bars = [...panel.querySelectorAll('[data-quota-key]')]
          const refreshButton = sidePanel.querySelector('button[aria-label*="Refresh"]')
          return {
            rowScroll: rowEl.scrollWidth,
            rowClient: rowEl.clientWidth,
            side: rect(sidePanel),
            panel: rect(panel),
            refresh: rect(refreshButton),
            bars: bars.map(rect),
            columns: getComputedStyle(grid).gridTemplateColumns.split(' ').length,
          }
        })
        return { rootScroll: root.scrollWidth, rootClient: root.clientWidth, rows }
      })
      const label = `${engine} credentials-quota viewport=${viewport} width=${width}`
      assert.ok(result.rootScroll <= result.rootClient + 1, `${label}: section overflow ${result.rootScroll}/${result.rootClient}`)
      assert.equal(result.rows.length, 2, `${label}: expected flat and grouped quota rows`)
      for (const item of result.rows) {
        assert.ok(item.rowScroll <= item.rowClient + 1, `${label}: row overflow ${item.rowScroll}/${item.rowClient}`)
        assert.ok(item.panel.left >= item.side.left - 0.5 && item.panel.right <= item.side.right + 0.5, `${label}: quota panel outside side panel`)
        assert.ok(item.refresh.left >= item.panel.right - 0.5 && item.refresh.right <= item.side.right + 0.5, `${label}: refresh button overlaps or overflows side panel`)
        assert.equal(item.bars.length, 2, `${label}: expected two quota bars`)
        assert.equal(item.columns, expectedColumns, `${label}: expected ${expectedColumns} quota column(s)`)
        if (expectedColumns === 2) {
          assert.ok(item.bars[1].left >= item.bars[0].right - 0.5, `${label}: 1440px desktop quota bars must stay side-by-side`)
        } else {
          assert.ok(item.bars[1].top >= item.bars[0].bottom - 0.5, `${label}: narrow quota bars must stack vertically`)
        }
      }
      console.log(`PASS ${label}: scroll=${result.rootScroll}/${result.rootClient}, side=${Math.round(result.rows[0].side.width)}, columns=${result.rows.map((item) => item.columns).join('/')}`)
    }
    for (const mode of ['full', 'compact']) {
      await page.setViewportSize({ width: 1440, height: 800 })
      await page.setContent(`<!doctype html><html data-mode="${mode}"><body><div id="root" style="width:300px"></div></body></html>`)
      await page.addScriptTag({ content: script })
      const panel = page.locator(`[data-credential-health-variant="${mode}"]`)
      await panel.waitFor()
      const layout = await panel.evaluate((el) => ({ display: getComputedStyle(el).display, columns: getComputedStyle(el).gridTemplateColumns.split(' ').length }))
      assert.equal(layout.display, mode === 'compact' ? 'flex' : 'grid')
      if (mode === 'full') assert.equal(layout.columns, 2, `${engine}: standalone full panel must retain desktop layout`)
      assert.equal(await panel.locator('[role="listitem"]').count(), 30)
      console.log(`PASS ${engine} standalone ${mode}: ${layout.display}, columns=${layout.columns}`)
    }
    for (const mode of ['quota-full', 'quota-compact']) {
      await page.setViewportSize({ width: 1440, height: 800 })
      const containerStyle = mode === 'quota-compact' ? 'width:340px;container:credential-card / inline-size' : 'width:340px'
      await page.setContent(`<!doctype html><html data-mode="${mode}"><body><div id="root" style="${containerStyle}"></div></body></html>`)
      await page.addScriptTag({ content: script })
      const variant = mode === 'quota-compact' ? 'compact' : 'full'
      const panel = page.locator(`[data-credential-quota-variant="${variant}"]`)
      await panel.waitFor()
      const columns = await panel.evaluate((el) => getComputedStyle(el.querySelector('[class*="credentialQuotaBars_"]')).gridTemplateColumns.split(' ').length)
      assert.equal(columns, 2, `${engine}: standalone ${mode} outside credential-list-side must retain 2 columns`)
      console.log(`PASS ${engine} standalone ${mode}: columns=${columns}`)
    }
    assert.deepEqual(errors, [], `${engine}: unexpected browser errors`)
  } finally {
    await browser.close()
  }
}
