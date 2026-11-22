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
  quota: [], displayQuotas: [], quotaLoading: false,
}

// 编译实际列表、健康组件和样式；单独的全尺寸面板模拟详情，紧凑面板模拟卡片。
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
      const row = ${JSON.stringify(row)}
      const mode = document.documentElement.dataset.mode
      const component = mode === 'credentials'
        ? h(CredentialListView, { rows: [row], contentMode: 'health', quotaUsageMode: 'tokens', onRefreshQuotaForAuthIndex: async () => {}, onResetQuotaForAuthIndex: async () => {} })
        : mode === 'provider' ? h(AiProviderListView, { rows: [row] })
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
    assert.deepEqual(errors, [], `${engine}: unexpected browser errors`)
  } finally {
    await browser.close()
  }
}
